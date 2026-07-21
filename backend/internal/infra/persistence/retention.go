package persistence

import (
	"context"
	"time"

	"github.com/keepbuild/seewxapkg/internal/domain/task"
)

type inMemoryRetentionCleaner interface {
	deleteUpdatedBefore(before time.Time) int
}

// StartRetentionJanitor bounds task metadata retained by the development-only
// in-memory repository. File-backed records are cleaned by the storage janitor.
func StartRetentionJanitor(ctx context.Context, repo task.Repository, retainHours int) {
	cleaner, ok := repo.(inMemoryRetentionCleaner)
	if !ok || retainHours <= 0 {
		return
	}
	retention := time.Duration(retainHours) * time.Hour
	cleanup := func() {
		cleaner.deleteUpdatedBefore(time.Now().Add(-retention))
	}
	cleanup()
	ticker := time.NewTicker(time.Hour)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				cleanup()
			case <-ctx.Done():
				return
			}
		}
	}()
}
