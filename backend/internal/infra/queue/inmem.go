package queue

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

type JobQueue interface {
	Enqueue(ctx context.Context, taskID string) error
	StartWorkers(ctx context.Context, workers int, handler func(context.Context, string) error)
	Wait()
}

type InMemoryQueue struct {
	ch           chan memoryJob
	maxRetries   int
	retryBackoff time.Duration
	workers      sync.WaitGroup
}

type memoryJob struct {
	taskID  string
	retries int
}

func NewInMemoryQueue(buffer int) *InMemoryQueue {
	if buffer <= 0 {
		buffer = 64
	}
	return &InMemoryQueue{
		ch:           make(chan memoryJob, buffer),
		maxRetries:   3,
		retryBackoff: 200 * time.Millisecond,
	}
}

func (q *InMemoryQueue) Enqueue(ctx context.Context, taskID string) error {
	if taskID == "" {
		return fmt.Errorf("empty task id")
	}
	select {
	case q.ch <- memoryJob{taskID: taskID}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *InMemoryQueue) StartWorkers(ctx context.Context, workers int, handler func(context.Context, string) error) {
	if workers <= 0 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		q.workers.Add(1)
		go func() {
			defer q.workers.Done()
			for {
				select {
				case job := <-q.ch:
					if ctx.Err() != nil {
						return
					}
					q.handleMemoryJob(ctx, job, handler)
				case <-ctx.Done():
					return
				}
			}
		}()
	}
}

func (q *InMemoryQueue) Wait() {
	q.workers.Wait()
}

func (q *InMemoryQueue) handleMemoryJob(ctx context.Context, job memoryJob, handler func(context.Context, string) error) {
	for {
		err := invokeHandler(handler, ctx, job.taskID)
		if err == nil || ctx.Err() != nil {
			return
		}
		job.retries++
		if job.retries >= q.maxRetries {
			log.Printf("[Queue] in-memory job failed after %d attempts: %v", job.retries, err)
			return
		}
		timer := time.NewTimer(time.Duration(job.retries) * q.retryBackoff)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}
	}
}
