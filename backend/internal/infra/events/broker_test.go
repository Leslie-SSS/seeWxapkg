package events

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/keepbuild/seewxapkg/internal/domain/task"
)

func TestBrokerTerminalEventIsDeliveredAndStreamIsRemoved(t *testing.T) {
	b := NewBroker()
	b.Create("task-1")
	eventStream, _, cancel, err := b.Subscribe("task-1")
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}
	defer cancel()

	b.Publish("task-1", task.TaskEvent{Type: "complete", TaskID: "task-1"})
	event, ok := <-eventStream
	if !ok || event.Type != "complete" {
		t.Fatalf("expected terminal event before close, got %+v ok=%v", event, ok)
	}
	if _, ok := <-eventStream; ok {
		t.Fatal("expected stream to be closed")
	}
	assertStreamNotFound(t, b, "task-1")
}

func TestBrokerCloseAndRemoveDropsHistoryAndLatePublish(t *testing.T) {
	b := NewBroker()
	b.Create("task-remove")
	eventStream, _, cancel, err := b.Subscribe("task-remove")
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}
	defer cancel()
	b.Publish("task-remove", task.TaskEvent{Type: "progress", Percent: 50})

	b.CloseAndRemove("task-remove")
	if event, ok := <-eventStream; !ok || event.Percent != 50 {
		t.Fatalf("expected buffered progress before close, got %+v ok=%v", event, ok)
	}
	if _, ok := <-eventStream; ok {
		t.Fatal("expected removed subscriber stream to close")
	}
	assertStreamNotFound(t, b, "task-remove")

	// A late worker publish must not recreate a stream that the API has already
	// reclaimed after observing the authoritative terminal task state.
	b.Publish("task-remove", task.TaskEvent{Type: "complete", Percent: 100})
	assertStreamNotFound(t, b, "task-remove")
}

func TestBrokerPublishWithoutCreateDoesNotRetainWorkerHistory(t *testing.T) {
	b := NewBroker()
	b.Publish("worker-only-task", task.TaskEvent{Type: "progress", Percent: 10})
	b.Publish("worker-only-task", task.TaskEvent{Type: "complete", Percent: 100})
	assertStreamNotFound(t, b, "worker-only-task")
}

func TestBrokerPublishCancelAndRemoveAreRaceSafe(t *testing.T) {
	b := NewBroker()
	for iteration := 0; iteration < 100; iteration++ {
		taskID := fmt.Sprintf("task-%d", iteration)
		b.Create(taskID)
		cancels := make([]func(), 0, 16)
		for i := 0; i < 16; i++ {
			_, _, cancel, err := b.Subscribe(taskID)
			if err != nil {
				t.Fatalf("Subscribe returned error: %v", err)
			}
			cancels = append(cancels, cancel)
		}

		var wg sync.WaitGroup
		for i, cancel := range cancels {
			wg.Add(2)
			go func(cancel func()) {
				defer wg.Done()
				cancel()
			}(cancel)
			go func(index int) {
				defer wg.Done()
				b.Publish(taskID, task.TaskEvent{Type: "progress", Percent: index})
			}(i)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.CloseAndRemove(taskID)
		}()
		wg.Wait()
		assertStreamNotFound(t, b, taskID)
	}
}

func TestBrokerRetainsBoundedHistoryBeforeTerminalRemoval(t *testing.T) {
	b := NewBroker()
	b.Create("task-slow")
	slowStream, _, slowCancel, err := b.Subscribe("task-slow")
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}
	defer slowCancel()
	for i := 0; i < maxStreamHistory+50; i++ {
		b.Publish("task-slow", task.TaskEvent{Type: "progress", Percent: i})
	}
	probeStream, history, probeCancel, err := b.Subscribe("task-slow")
	if err != nil {
		t.Fatalf("Subscribe before completion returned error: %v", err)
	}
	defer probeCancel()
	if len(history) != maxStreamHistory {
		t.Fatalf("expected bounded history of %d, got %d", maxStreamHistory, len(history))
	}

	b.Publish("task-slow", task.TaskEvent{Type: "complete", Percent: 100})
	for event := range slowStream {
		if event.Type == "complete" {
			break
		}
	}
	event, ok := <-probeStream
	if !ok || event.Type != "complete" {
		t.Fatalf("probe subscriber missed terminal event: %+v ok=%v", event, ok)
	}
	if _, ok := <-probeStream; ok {
		t.Fatal("probe stream should close after terminal delivery")
	}
	assertStreamNotFound(t, b, "task-slow")
}

func TestBrokerIdleTTLRemovesOnlyUnsubscribedStreams(t *testing.T) {
	const idleTTL = 20 * time.Millisecond
	b := newBrokerWithIdleTTL(idleTTL)
	b.Create("idle-task")
	waitForCondition(t, time.Second, func() bool { return !brokerHasStream(b, "idle-task") })

	b.Create("active-task")
	eventStream, _, cancel, err := b.Subscribe("active-task")
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}
	for deadline := time.Now().Add(4 * idleTTL); time.Now().Before(deadline); {
		if !brokerHasStream(b, "active-task") {
			t.Fatal("idle TTL removed a stream with an active subscriber")
		}
		time.Sleep(idleTTL / 2)
	}
	select {
	case _, ok := <-eventStream:
		if !ok {
			t.Fatal("active subscriber was closed by idle TTL")
		}
	default:
	}

	cancel()
	waitForCondition(t, time.Second, func() bool { return !brokerHasStream(b, "active-task") })
}

func assertStreamNotFound(t *testing.T, b *Broker, taskID string) {
	t.Helper()
	stream, _, cancel, err := b.Subscribe(taskID)
	if cancel != nil {
		cancel()
	}
	if stream != nil || !errors.Is(err, ErrStreamNotFound) {
		t.Fatalf("stream %q still retained: stream=%v err=%v", taskID, stream != nil, err)
	}
}

func brokerHasStream(b *Broker, taskID string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.streams[taskID]
	return ok
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition was not met before timeout")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
