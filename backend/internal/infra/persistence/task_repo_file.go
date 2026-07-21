package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/keepbuild/seewxapkg/internal/domain/task"
)

type fileTaskRepo struct {
	baseDir string
	mu      sync.Mutex
}

func NewFileTaskRepo(baseDir string) (task.Repository, error) {
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(baseDir, 0700); err != nil {
		return nil, err
	}
	if err := scrubLegacySensitiveTaskState(baseDir); err != nil {
		return nil, fmt.Errorf("sanitize legacy task state record (%T)", err)
	}
	return &fileTaskRepo{baseDir: baseDir}, nil
}

func scrubLegacySensitiveTaskState(baseDir string) error {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic-link task state is not allowed")
		}
		path := filepath.Join(baseDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var document interface{}
		if err := json.Unmarshal(data, &document); err != nil {
			return err
		}
		if removeLegacySensitiveFields(document) {
			cleaned, err := json.MarshalIndent(document, "", "  ")
			if err != nil {
				return err
			}
			if err := writeFileAtomic(path, cleaned, 0600); err != nil {
				return err
			}
		} else {
			if err := os.Chmod(path, 0600); err != nil {
				return err
			}
		}
		if isTerminalLegacyTaskState(document) {
			id := strings.TrimSuffix(entry.Name(), ".json")
			if parsed, err := uuid.Parse(id); err == nil && parsed.String() == id {
				if err := cleanAndSecureTerminalTaskTree(filepath.Dir(baseDir), id); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func isTerminalLegacyTaskState(value interface{}) bool {
	document, ok := value.(map[string]interface{})
	if !ok {
		return false
	}
	status, _ := document["status"].(string)
	switch status {
	case string(task.TaskCompleted), string(task.TaskPartial), string(task.TaskFailed):
		return true
	default:
		return false
	}
}

func cleanAndSecureTerminalTaskTree(tempRoot, taskID string) error {
	taskRoot := filepath.Join(tempRoot, taskID)
	info, err := os.Lstat(taskRoot)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("terminal task workspace is not a private directory")
	}
	for _, target := range []string{filepath.Join(taskRoot, "input"), filepath.Join(taskRoot, "fallback")} {
		if err := removePrivateTree(target); err != nil {
			return err
		}
	}
	resultDir := filepath.Join(taskRoot, "result")
	if resultInfo, err := os.Lstat(resultDir); err == nil {
		if resultInfo.Mode()&os.ModeSymlink != 0 || !resultInfo.IsDir() {
			return fmt.Errorf("terminal result workspace is not a private directory")
		}
		if err := removePrivateTree(filepath.Join(resultDir, "raw")); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := filepath.WalkDir(taskRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link in terminal task workspace")
		}
		if entry.IsDir() {
			return os.Chmod(path, 0700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular terminal task artifact")
		}
		return os.Chmod(path, 0600)
	}); err != nil {
		return err
	}
	return syncRepoDirectory(taskRoot)
}

func removePrivateTree(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(path)
	}
	return os.RemoveAll(path)
}

func syncRepoDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func removeLegacySensitiveFields(value interface{}) bool {
	changed := false
	switch current := value.(type) {
	case map[string]interface{}:
		for key, child := range current {
			normalized := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key))
			switch normalized {
			case "appid", "filename", "originalfilename", "filesize", "originalfilesize",
				"artifactspath", "diagnosticspath", "fallbackdir", "inputpath", "manifestpath",
				"outputpath", "reportpath", "reportsdir", "rootdir", "sourcedir", "tempdir",
				"workspace", "workspacepath", "zippath":
				delete(current, key)
				changed = true
				continue
			}
			changed = removeLegacySensitiveFields(child) || changed
		}
	case []interface{}:
		for _, child := range current {
			changed = removeLegacySensitiveFields(child) || changed
		}
	}
	return changed
}

func (r *fileTaskRepo) Create(ctx context.Context, t *task.Task) error {
	return r.write(ctx, t)
}

func (r *fileTaskRepo) Update(ctx context.Context, t *task.Task) error {
	return r.write(ctx, t)
}

func (r *fileTaskRepo) Get(ctx context.Context, id string) (*task.Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := r.taskPath(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}

	var current task.Task
	if err := json.Unmarshal(data, &current); err != nil {
		return nil, err
	}

	return current.Clone(), nil
}

func (r *fileTaskRepo) write(ctx context.Context, t *task.Task) error {
	if t == nil {
		return fmt.Errorf("task is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	path, err := r.taskPath(t.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(r.baseDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(r.baseDir, 0700); err != nil {
		return err
	}
	if err := writeFileAtomic(path, data, 0600); err != nil {
		return err
	}
	return nil
}

func (r *fileTaskRepo) taskPath(id string) (string, error) {
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id || filepath.VolumeName(id) != "" || strings.IndexByte(id, 0) >= 0 {
		return "", fmt.Errorf("invalid task id")
	}
	return filepath.Join(r.baseDir, id+".json"), nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) (retErr error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".task-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		if retErr != nil {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpPath)
	}()

	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}

	// Sync the directory entry so a successful update survives a host crash.
	dirHandle, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer dirHandle.Close()
	return dirHandle.Sync()
}
