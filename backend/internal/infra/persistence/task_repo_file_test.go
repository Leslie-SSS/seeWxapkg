package persistence

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/keepbuild/seewxapkg/internal/domain/task"
)

func newTestFileTaskRepo(t *testing.T, baseDir string) task.Repository {
	t.Helper()
	repo, err := NewFileTaskRepo(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestFileTaskRepoGetSeesExternalUpdates(t *testing.T) {
	baseDir := t.TempDir()
	repoA := newTestFileTaskRepo(t, baseDir)
	repoB := newTestFileTaskRepo(t, baseDir)

	now := time.Now()
	current := &task.Task{
		ID:        "task-1",
		Status:    task.TaskQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := repoA.Create(context.Background(), current); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	loaded, err := repoB.Get(context.Background(), current.ID)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if loaded.Status != task.TaskQueued {
		t.Fatalf("expected queued, got %s", loaded.Status)
	}

	current.Status = task.TaskCompleted
	current.UpdatedAt = time.Now()
	if err := repoA.Update(context.Background(), current); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	reloaded, err := repoB.Get(context.Background(), current.ID)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if reloaded.Status != task.TaskCompleted {
		t.Fatalf("expected completed after external update, got %s", reloaded.Status)
	}
}

func TestFileTaskRepoRejectsInvalidTaskID(t *testing.T) {
	repo := newTestFileTaskRepo(t, t.TempDir())
	for _, id := range []string{"", "../escape", "nested/task", ".."} {
		if err := repo.Create(context.Background(), &task.Task{ID: id}); err == nil {
			t.Fatalf("expected task id %q to be rejected", id)
		}
		if _, err := repo.Get(context.Background(), id); err == nil {
			t.Fatalf("expected Get to reject task id %q", id)
		}
	}
}

func TestFileTaskRepoScrubsLegacySensitiveFieldsOnStartup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}
	baseDir := t.TempDir()
	path := filepath.Join(baseDir, "11111111-1111-4111-8111-111111111111.json")
	legacy := []byte(`{
  "id": "11111111-1111-4111-8111-111111111111",
  "status": "completed",
  "requestedOptions": {
    "appId": "wx0123456789abcdef",
    "originalFileName": "private.wxapkg",
    "originalFileSize": 123,
    "beautify": true
  },
	"stages": [{"metrics": {
		"fileCount": 2,
		"zipPath": "/data/output/private.zip",
		"reportPath": "/data/tasks/private/report.json",
		"workspacePath": "/data/tasks/private"
	}}],
  "nested": {"APP_ID": "another-secret"}
}`)
	if err := os.WriteFile(path, legacy, 0644); err != nil {
		t.Fatal(err)
	}
	taskRoot := filepath.Join(filepath.Dir(baseDir), "11111111-1111-4111-8111-111111111111")
	for _, privatePath := range []string{
		filepath.Join(taskRoot, "input", "input.wxapkg"),
		filepath.Join(taskRoot, "fallback", "input.wxapkg"),
		filepath.Join(taskRoot, "result", "raw", "app.js"),
		filepath.Join(taskRoot, "result", "src", "app.js"),
	} {
		if err := os.MkdirAll(filepath.Dir(privatePath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(privatePath, []byte("private"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewFileTaskRepo(baseDir); err != nil {
		t.Fatal(err)
	}
	cleaned, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"wx0123456789abcdef", "another-secret", "private.wxapkg", "appId", "APP_ID",
		"originalFileName", "originalFileSize", "zipPath", "reportPath", "workspacePath",
		"/data/output/private.zip", "/data/tasks/private",
	} {
		if strings.Contains(string(cleaned), forbidden) {
			t.Fatalf("legacy sensitive value or key remained after startup scrub: %q", forbidden)
		}
	}
	if !strings.Contains(string(cleaned), `"beautify": true`) {
		t.Fatal("non-sensitive task state was removed")
	}
	if !strings.Contains(string(cleaned), `"fileCount": 2`) {
		t.Fatal("non-sensitive stage metric was removed")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("scrubbed task state mode = %o, want 600", got)
	}
	for _, removed := range []string{
		filepath.Join(taskRoot, "input"),
		filepath.Join(taskRoot, "fallback"),
		filepath.Join(taskRoot, "result", "raw"),
	} {
		if _, err := os.Stat(removed); !os.IsNotExist(err) {
			t.Fatalf("legacy private duplicate was retained: %s: %v", removed, err)
		}
	}
	artifact := filepath.Join(taskRoot, "result", "src", "app.js")
	artifactInfo, err := os.Stat(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if got := artifactInfo.Mode().Perm(); got != 0600 {
		t.Fatalf("legacy artifact mode = %o, want 600", got)
	}
}

func TestFileTaskRepoAtomicConcurrentUpdates(t *testing.T) {
	baseDir := t.TempDir()
	repoA := newTestFileTaskRepo(t, baseDir)
	repoB := newTestFileTaskRepo(t, baseDir)
	repoReader := newTestFileTaskRepo(t, baseDir)
	ctx := context.Background()
	initial := &task.Task{ID: "task-atomic", Status: task.TaskQueued, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := repoA.Create(ctx, initial); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	var writers sync.WaitGroup
	errorsCh := make(chan error, 3)
	for _, repo := range []task.Repository{repoA, repoB} {
		writers.Add(1)
		go func(repo task.Repository) {
			defer writers.Done()
			for i := 0; i < 100; i++ {
				current := initial.Clone()
				current.Progress = i
				current.UpdatedAt = time.Now()
				if err := repo.Update(ctx, current); err != nil {
					errorsCh <- err
					return
				}
			}
		}(repo)
	}
	done := make(chan struct{})
	go func() {
		writers.Wait()
		close(done)
	}()

	for {
		select {
		case err := <-errorsCh:
			t.Fatalf("concurrent update failed: %v", err)
		case <-done:
			select {
			case err := <-errorsCh:
				t.Fatalf("concurrent update failed: %v", err)
			default:
			}
			entries, err := filepath.Glob(filepath.Join(baseDir, ".task-*.tmp"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("temporary files were not cleaned: %v err=%v", entries, err)
			}
			if _, err := os.Stat(filepath.Join(baseDir, initial.ID+".json")); err != nil {
				t.Fatalf("final task file missing: %v", err)
			}
			return
		default:
			if _, err := repoReader.Get(ctx, initial.ID); err != nil {
				t.Fatalf("reader observed a partial update: %v", err)
			}
		}
	}
}
