package queue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestInMemoryQueueRetriesHandlerPanic(t *testing.T) {
	q := NewInMemoryQueue(4)
	q.retryBackoff = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var attempts atomic.Int32
	done := make(chan struct{}, 1)
	q.StartWorkers(ctx, 1, func(_ context.Context, taskID string) error {
		attempt := attempts.Add(1)
		if taskID != "task-panic" {
			t.Errorf("unexpected task id: %s", taskID)
		}
		if attempt == 1 {
			panic("boom")
		}
		done <- struct{}{}
		return nil
	})
	if err := q.Enqueue(ctx, "task-panic"); err != nil {
		t.Fatalf("Enqueue returned error: %v", err)
	}
	select {
	case <-done:
		if attempts.Load() != 2 {
			t.Fatalf("expected one retry, got %d attempts", attempts.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for retry")
	}
}

func TestInMemoryQueueRejectsEmptyTaskID(t *testing.T) {
	if err := NewInMemoryQueue(1).Enqueue(context.Background(), ""); err == nil {
		t.Fatal("expected empty task id error")
	}
}

func TestInMemoryQueueWaitsForWorkerExit(t *testing.T) {
	q := NewInMemoryQueue(1)
	ctx, cancel := context.WithCancel(context.Background())
	q.StartWorkers(ctx, 1, func(context.Context, string) error { return nil })
	cancel()
	done := make(chan struct{})
	go func() {
		q.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Wait did not observe worker shutdown")
	}
}
