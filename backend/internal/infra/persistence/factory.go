package persistence

import (
	"fmt"
	"path/filepath"

	"github.com/keepbuild/seewxapkg/internal/config"
	"github.com/keepbuild/seewxapkg/internal/domain/task"
)

func NewTaskRepository(cfg *config.Config) (task.Repository, error) {
	switch cfg.TaskRepoDriver {
	case "memory":
		return NewMemoryTaskRepo(), nil
	case "file":
		return NewFileTaskRepo(filepath.Join(cfg.TempDir, "task-state"))
	default:
		return nil, fmt.Errorf("unsupported task repository driver %q", cfg.TaskRepoDriver)
	}
}
