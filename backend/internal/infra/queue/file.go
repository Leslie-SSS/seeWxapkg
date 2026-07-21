package queue

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type FileQueue struct {
	queueDir          string
	dlqDir            string
	pollInterval      time.Duration
	maxRetries        int
	retryBackoff      time.Duration
	visibilityTimeout time.Duration
	reclaimMu         sync.Mutex
	nextReclaim       time.Time
	workers           sync.WaitGroup
}

type fileQueueJob struct {
	TaskID      string    `json:"taskId"`
	Retries     int       `json:"retries"`
	AvailableAt time.Time `json:"availableAt,omitempty"`
	LastError   string    `json:"lastError,omitempty"`
}

func NewFileQueue(rootDir string) (*FileQueue, error) {
	queueDir := filepath.Join(rootDir, "pending")
	dlqDir := filepath.Join(rootDir, "dlq")
	for _, directory := range []string{rootDir, queueDir, dlqDir} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return nil, err
		}
		if err := os.Chmod(directory, 0700); err != nil {
			return nil, err
		}
	}
	return &FileQueue{
		queueDir:          queueDir,
		dlqDir:            dlqDir,
		pollInterval:      200 * time.Millisecond,
		maxRetries:        3,
		retryBackoff:      2 * time.Second,
		visibilityTimeout: 30 * time.Minute,
	}, nil
}

func (q *FileQueue) Enqueue(ctx context.Context, taskID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return q.writeJob(q.queueDir, fileQueueJob{TaskID: taskID})
}

func (q *FileQueue) StartWorkers(ctx context.Context, workers int, handler func(context.Context, string) error) {
	if workers <= 0 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		consumerID := fmt.Sprintf("worker-%d", i+1)
		q.workers.Add(1)
		go q.runWorker(ctx, consumerID, handler)
	}
}

func (q *FileQueue) runWorker(ctx context.Context, consumerID string, handler func(context.Context, string) error) {
	defer q.workers.Done()
	ticker := time.NewTicker(q.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			q.reclaimStaleJobs()
			claimedPath, job, ok := q.claimNextJob(consumerID)
			if !ok {
				continue
			}
			q.handleJob(ctx, claimedPath, job, handler)
		}
	}
}

func (q *FileQueue) Wait() {
	q.workers.Wait()
}

func (q *FileQueue) handleJob(ctx context.Context, claimedPath string, job fileQueueJob, handler func(context.Context, string) error) {
	stopHeartbeat := q.startLeaseHeartbeat(ctx, claimedPath)
	err := invokeHandler(handler, ctx, job.TaskID)
	stopHeartbeat()
	if err == nil {
		if removeErr := os.Remove(claimedPath); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("[Queue] remove completed file job failed (%T)", removeErr)
		}
		return
	}
	if ctx.Err() != nil {
		// Keep the claimed job for visibility-timeout recovery after shutdown.
		return
	}

	job.Retries++
	job.LastError = "task processing failed"
	targetDir := q.queueDir
	if job.Retries >= q.maxRetries {
		targetDir = q.dlqDir
	} else {
		job.AvailableAt = time.Now().Add(time.Duration(job.Retries) * q.retryBackoff)
	}
	if writeErr := q.writeJob(targetDir, job); writeErr != nil {
		log.Printf("[Queue] persist failed job failed (%T)", writeErr)
		return
	}
	if removeErr := os.Remove(claimedPath); removeErr != nil && !os.IsNotExist(removeErr) {
		log.Printf("[Queue] remove claimed job failed (%T)", removeErr)
	}
}

func (q *FileQueue) startLeaseHeartbeat(ctx context.Context, claimedPath string) func() {
	if q.visibilityTimeout <= 0 {
		return func() {}
	}
	interval := q.visibilityTimeout / 3
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	if interval > time.Minute {
		interval = time.Minute
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				now := time.Now()
				if err := os.Chtimes(claimedPath, now, now); err != nil && !os.IsNotExist(err) {
					log.Printf("[Queue] refresh active job lease failed (%T)", err)
				}
			case <-stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

func (q *FileQueue) claimNextJob(consumerID string) (string, fileQueueJob, bool) {
	entries, err := os.ReadDir(q.queueDir)
	if err != nil || len(entries) == 0 {
		return "", fileQueueJob{}, false
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".job") {
			continue
		}
		sourcePath := filepath.Join(q.queueDir, entry.Name())
		claimedPath := sourcePath + "." + consumerID + ".working"
		if err := os.Rename(sourcePath, claimedPath); err != nil {
			continue
		}
		if err := os.Chtimes(claimedPath, now, now); err != nil {
			q.moveInvalidJob(claimedPath)
			continue
		}

		job, err := q.readJob(claimedPath)
		if err != nil {
			q.moveInvalidJob(claimedPath)
			continue
		}
		if !job.AvailableAt.IsZero() && job.AvailableAt.After(now) {
			_ = os.Rename(claimedPath, sourcePath)
			continue
		}
		return claimedPath, job, true
	}

	return "", fileQueueJob{}, false
}

func (q *FileQueue) reclaimStaleJobs() {
	if q.visibilityTimeout <= 0 {
		return
	}
	now := time.Now()
	q.reclaimMu.Lock()
	if now.Before(q.nextReclaim) {
		q.reclaimMu.Unlock()
		return
	}
	reclaimInterval := q.visibilityTimeout / 2
	if reclaimInterval > time.Minute {
		reclaimInterval = time.Minute
	}
	if reclaimInterval < q.pollInterval {
		reclaimInterval = q.pollInterval
	}
	q.nextReclaim = now.Add(reclaimInterval)
	q.reclaimMu.Unlock()

	entries, err := os.ReadDir(q.queueDir)
	if err != nil {
		return
	}
	staleBefore := now.Add(-q.visibilityTimeout)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".working") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(staleBefore) {
			continue
		}
		source := filepath.Join(q.queueDir, entry.Name())
		job, err := q.readJob(source)
		if err != nil {
			q.moveInvalidJob(source)
			continue
		}
		name := fmt.Sprintf("%020d-recovered-%s.job", time.Now().UnixNano(), taskIDToken(job.TaskID))
		if err := os.Rename(source, filepath.Join(q.queueDir, name)); err != nil && !os.IsNotExist(err) {
			log.Printf("[Queue] reclaim stale job failed (%T)", err)
		}
	}
}

func (q *FileQueue) moveInvalidJob(path string) {
	if err := os.MkdirAll(q.dlqDir, 0700); err != nil {
		log.Printf("[Queue] create DLQ directory failed (%T)", err)
		return
	}
	if err := os.Chmod(q.dlqDir, 0700); err != nil {
		log.Printf("[Queue] secure DLQ directory failed (%T)", err)
		return
	}
	target := filepath.Join(q.dlqDir, filepath.Base(path)+".invalid")
	if err := os.Rename(path, target); err != nil && !os.IsNotExist(err) {
		log.Printf("[Queue] move invalid job to DLQ failed (%T)", err)
	}
}

func (q *FileQueue) writeJob(dir string, job fileQueueJob) error {
	if job.TaskID == "" {
		return fmt.Errorf("empty task id")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}

	data, err := json.Marshal(job)
	if err != nil {
		return err
	}

	baseName := fmt.Sprintf("%020d-%s.job", time.Now().UnixNano(), taskIDToken(job.TaskID))
	finalPath := filepath.Join(dir, baseName)
	tmp, err := os.CreateTemp(dir, ".queue-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return err
	}
	if err := syncQueueDirectory(dir); err != nil {
		return err
	}
	committed = true
	return nil
}

func (q *FileQueue) readJob(path string) (fileQueueJob, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fileQueueJob{}, err
	}
	var job fileQueueJob
	if err := json.Unmarshal(data, &job); err != nil {
		return fileQueueJob{}, err
	}
	return job, nil
}

func taskIDToken(taskID string) string {
	digest := sha256.Sum256([]byte(taskID))
	return fmt.Sprintf("%x", digest[:8])
}

func syncQueueDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
