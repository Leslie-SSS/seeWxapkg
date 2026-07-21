package persistence

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/keepbuild/seewxapkg/internal/domain/task"
)

func TestMemoryTaskRepoDeletesExpiredMetadata(t *testing.T) {
	repo := NewMemoryTaskRepo().(*memoryTaskRepo)
	now := time.Now()
	for _, current := range []*task.Task{
		{ID: "expired", CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now.Add(-48 * time.Hour)},
		{ID: "active", CreatedAt: now, UpdatedAt: now},
	} {
		if err := repo.Create(context.Background(), current); err != nil {
			t.Fatal(err)
		}
	}
	if removed := repo.deleteUpdatedBefore(now.Add(-24 * time.Hour)); removed != 1 {
		t.Fatalf("removed %d tasks, want 1", removed)
	}
	if _, err := repo.Get(context.Background(), "expired"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expired metadata remains: %v", err)
	}
	if _, err := repo.Get(context.Background(), "active"); err != nil {
		t.Fatalf("active metadata was removed: %v", err)
	}
}
