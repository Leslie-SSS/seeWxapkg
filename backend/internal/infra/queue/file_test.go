package queue

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestFileQueue(t *testing.T) *FileQueue {
	t.Helper()
	queue, err := NewFileQueue(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return queue
}

func TestFileQueueProcessesEnqueuedTask(t *testing.T) {
	q := newTestFileQueue(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan string, 1)
	q.StartWorkers(ctx, 1, func(_ context.Context, taskID string) error {
		done <- taskID
		return nil
	})

	if err := q.Enqueue(ctx, "task-123"); err != nil {
		t.Fatalf("Enqueue returned error: %v", err)
	}

	select {
	case taskID := <-done:
		if taskID != "task-123" {
			t.Fatalf("expected task-123, got %s", taskID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for file queue worker")
	}
}

func TestFileQueueRetriesAndSendsToDLQ(t *testing.T) {
	q := newTestFileQueue(t)
	q.pollInterval = 50 * time.Millisecond
	q.retryBackoff = 20 * time.Millisecond
	q.maxRetries = 2

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var attempts atomic.Int32
	q.StartWorkers(ctx, 1, func(_ context.Context, taskID string) error {
		attempts.Add(1)
		if taskID != "task-dlq" {
			t.Fatalf("unexpected task id: %s", taskID)
		}
		return errors.New("boom")
	})

	if err := q.Enqueue(ctx, "task-dlq"); err != nil {
		t.Fatalf("Enqueue returned error: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(q.dlqDir)
		if err != nil {
			t.Fatalf("ReadDir returned error: %v", err)
		}
		if len(entries) > 0 {
			if attempts.Load() < 2 {
				t.Fatalf("expected retries before DLQ, got %d attempts", attempts.Load())
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("expected job in DLQ after retries, attempts=%d", attempts.Load())
}

func TestFileQueueRecoversHandlerPanicIntoDLQ(t *testing.T) {
	q := newTestFileQueue(t)
	q.pollInterval = 10 * time.Millisecond
	q.retryBackoff = time.Millisecond
	q.maxRetries = 2

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts atomic.Int32
	q.StartWorkers(ctx, 1, func(_ context.Context, _ string) error {
		attempts.Add(1)
		panic("malformed package")
	})
	if err := q.Enqueue(ctx, "task-panic"); err != nil {
		t.Fatalf("Enqueue returned error: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(q.dlqDir)
		if err == nil && len(entries) == 1 {
			if attempts.Load() != 2 {
				t.Fatalf("expected two attempts, got %d", attempts.Load())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected panicking job in DLQ")
}

func TestFileQueueReclaimsStaleWorkingJob(t *testing.T) {
	q := newTestFileQueue(t)
	q.pollInterval = 10 * time.Millisecond
	q.visibilityTimeout = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := q.Enqueue(ctx, "task-stale"); err != nil {
		t.Fatalf("Enqueue returned error: %v", err)
	}
	claimedPath, _, ok := q.claimNextJob("crashed-worker")
	if !ok {
		t.Fatal("expected initial job claim")
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(claimedPath, old, old); err != nil {
		t.Fatalf("age claimed job: %v", err)
	}

	done := make(chan struct{}, 1)
	q.StartWorkers(ctx, 1, func(_ context.Context, taskID string) error {
		if taskID != "task-stale" {
			t.Errorf("unexpected task id: %s", taskID)
		}
		done <- struct{}{}
		return nil
	})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stale working job was not reclaimed")
	}
	if _, err := os.Stat(claimedPath); !os.IsNotExist(err) {
		t.Fatalf("stale claim was not removed: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		working, err := filepath.Glob(filepath.Join(q.queueDir, "*.working"))
		if err != nil {
			t.Fatalf("glob working files: %v", err)
		}
		if len(working) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("unexpected working files: %v", working)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestFileQueueHeartbeatsLongRunningJob(t *testing.T) {
	q := newTestFileQueue(t)
	q.pollInterval = 5 * time.Millisecond
	q.visibilityTimeout = 60 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var attempts atomic.Int32
	done := make(chan struct{}, 1)
	q.StartWorkers(ctx, 2, func(_ context.Context, taskID string) error {
		if taskID != "task-long-running" {
			t.Errorf("unexpected task id: %s", taskID)
		}
		attempts.Add(1)
		time.Sleep(220 * time.Millisecond)
		done <- struct{}{}
		return nil
	})
	if err := q.Enqueue(ctx, "task-long-running"); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("long-running job did not finish")
	}
	time.Sleep(120 * time.Millisecond)
	if got := attempts.Load(); got != 1 {
		t.Fatalf("long-running job was reclaimed while its lease was healthy: attempts=%d", got)
	}
}

func TestFileQueueRepeatedReclaimsKeepFilenameBounded(t *testing.T) {
	q := newTestFileQueue(t)
	q.visibilityTimeout = time.Millisecond
	ctx := context.Background()
	if err := q.Enqueue(ctx, strings.Repeat("sensitive-task-id", 100)); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 20; attempt++ {
		claimedPath, _, ok := q.claimNextJob("crashed-worker")
		if !ok {
			t.Fatalf("attempt %d: expected a claimable job", attempt)
		}
		old := time.Now().Add(-time.Minute)
		if err := os.Chtimes(claimedPath, old, old); err != nil {
			t.Fatal(err)
		}
		q.nextReclaim = time.Time{}
		q.reclaimStaleJobs()
		entries, err := os.ReadDir(q.queueDir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("attempt %d: entries=%v err=%v", attempt, entries, err)
		}
		if len(entries[0].Name()) > 80 {
			t.Fatalf("reclaimed filename grew unexpectedly: %s", entries[0].Name())
		}
		if strings.Contains(entries[0].Name(), "sensitive-task-id") {
			t.Fatalf("queue filename exposed the task ID: %s", entries[0].Name())
		}
	}
}
