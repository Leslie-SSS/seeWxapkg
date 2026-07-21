package queue

import (
	"fmt"
	"path/filepath"

	"github.com/keepbuild/seewxapkg/internal/config"
)

func NewJobQueue(cfg *config.Config) (JobQueue, error) {
	switch cfg.QueueDriver {
	case "file":
		return NewFileQueue(filepath.Join(cfg.TempDir, "queue"))
	case "inmem":
		return NewInMemoryQueue(cfg.MaxConcurrentTasks * 4), nil
	default:
		return nil, fmt.Errorf("unsupported queue driver %q", cfg.QueueDriver)
	}
}
