package persistence

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/keepbuild/seewxapkg/internal/domain/task"
)

var ErrTaskNotFound = errors.New("task not found")

type memoryTaskRepo struct {
	mu    sync.RWMutex
	tasks map[string]*task.Task
}

func NewMemoryTaskRepo() task.Repository {
	return &memoryTaskRepo{
		tasks: make(map[string]*task.Task),
	}
}

func (r *memoryTaskRepo) Create(_ context.Context, t *task.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks[t.ID] = t.Clone()
	return nil
}

func (r *memoryTaskRepo) Update(_ context.Context, t *task.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks[t.ID] = t.Clone()
	return nil
}

func (r *memoryTaskRepo) Get(_ context.Context, id string) (*task.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	current, ok := r.tasks[id]
	if !ok {
		return nil, ErrTaskNotFound
	}
	return current.Clone(), nil
}

func (r *memoryTaskRepo) deleteUpdatedBefore(before time.Time) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for id, current := range r.tasks {
		updatedAt := current.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = current.CreatedAt
		}
		if !updatedAt.IsZero() && updatedAt.Before(before) {
			delete(r.tasks, id)
			removed++
		}
	}
	return removed
}
