package events

import (
	"errors"
	"sync"
	"time"

	"github.com/keepbuild/seewxapkg/internal/domain/task"
)

var ErrStreamNotFound = errors.New("event stream not found")

const maxStreamHistory = 256

// Streams are an in-process acceleration for SSE. The persisted task repository
// remains authoritative, so an idle stream can be discarded safely: a later
// subscriber falls back to repository polling in the HTTP handler.
const defaultStreamIdleTTL = 10 * time.Minute

type stream struct {
	history     []task.TaskEvent
	subscribers map[chan task.TaskEvent]struct{}
	expiresAt   time.Time
	expiryTimer *time.Timer
}

type Broker struct {
	mu      sync.RWMutex
	streams map[string]*stream
	idleTTL time.Duration
}

func NewBroker() *Broker {
	return newBrokerWithIdleTTL(defaultStreamIdleTTL)
}

func newBrokerWithIdleTTL(idleTTL time.Duration) *Broker {
	return &Broker{
		streams: make(map[string]*stream),
		idleTTL: idleTTL,
	}
}

func (b *Broker) Create(taskID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if current, ok := b.streams[taskID]; ok {
		b.refreshExpiryLocked(taskID, current)
		return
	}
	current := &stream{subscribers: make(map[chan task.TaskEvent]struct{})}
	b.streams[taskID] = current
	b.refreshExpiryLocked(taskID, current)
}

func (b *Broker) Publish(taskID string, event task.TaskEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	terminal := event.Type == "complete" || event.Type == "partial" || event.Type == "error"
	s, ok := b.streams[taskID]
	if !ok {
		// A standalone worker has no in-process SSE subscribers. Requiring Create
		// prevents that process from retaining per-task histories or identifiers.
		return
	}
	s.history = append(s.history, event)
	if len(s.history) > maxStreamHistory {
		s.history = append([]task.TaskEvent(nil), s.history[len(s.history)-maxStreamHistory:]...)
	}
	for ch := range s.subscribers {
		select {
		case ch <- event:
		default:
			if terminal {
				// A slow subscriber may lose intermediate progress, but must still
				// receive the terminal outcome before the stream closes.
				select {
				case <-ch:
				default:
				}
				ch <- event
			}
		}
	}
	if terminal {
		// Current subscribers already received the terminal outcome. Reconnects use
		// the authoritative repository, so release the task ID and history now.
		b.removeStreamLocked(taskID, s)
		return
	}
	b.refreshExpiryLocked(taskID, s)
}

func (b *Broker) Subscribe(taskID string) (<-chan task.TaskEvent, []task.TaskEvent, func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	s, ok := b.streams[taskID]
	if !ok {
		return nil, nil, nil, ErrStreamNotFound
	}

	ch := make(chan task.TaskEvent, 64)
	history := append([]task.TaskEvent(nil), s.history...)
	s.subscribers[ch] = struct{}{}
	b.refreshExpiryLocked(taskID, s)
	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if current, exists := b.streams[taskID]; exists {
			if _, subscribed := current.subscribers[ch]; subscribed {
				delete(current.subscribers, ch)
				close(ch)
				b.refreshExpiryLocked(taskID, current)
			}
		}
	}
	return ch, history, cancel, nil
}

// CloseAndRemove closes every active subscriber and releases all history and
// task identifiers retained by the stream. It is idempotent and safe to race
// with Publish, Subscribe, and subscription cancellation.
func (b *Broker) CloseAndRemove(taskID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.streams[taskID]
	if !ok {
		return
	}
	b.removeStreamLocked(taskID, s)
}

func (b *Broker) removeStreamLocked(taskID string, expected *stream) {
	current, ok := b.streams[taskID]
	if !ok || current != expected {
		return
	}
	delete(b.streams, taskID)
	if current.expiryTimer != nil {
		current.expiryTimer.Stop()
		current.expiryTimer = nil
	}
	current.history = nil
	for ch := range current.subscribers {
		close(ch)
	}
	current.subscribers = nil
}

func (b *Broker) refreshExpiryLocked(taskID string, current *stream) {
	if b.idleTTL <= 0 {
		return
	}
	current.expiresAt = time.Now().Add(b.idleTTL)
	if current.expiryTimer == nil {
		current.expiryTimer = time.AfterFunc(b.idleTTL, func() {
			b.expireIdleStream(taskID, current)
		})
		return
	}
	current.expiryTimer.Reset(b.idleTTL)
}

func (b *Broker) expireIdleStream(taskID string, expected *stream) {
	b.mu.Lock()
	defer b.mu.Unlock()

	current, ok := b.streams[taskID]
	if !ok || current != expected {
		return
	}
	if remaining := time.Until(current.expiresAt); remaining > 0 {
		current.expiryTimer.Reset(remaining)
		return
	}
	if len(current.subscribers) > 0 {
		b.refreshExpiryLocked(taskID, current)
		return
	}
	b.removeStreamLocked(taskID, current)
}
