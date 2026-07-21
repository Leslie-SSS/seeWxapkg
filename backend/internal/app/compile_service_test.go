package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keepbuild/seewxapkg/internal/config"
	"github.com/keepbuild/seewxapkg/internal/domain/task"
	"github.com/keepbuild/seewxapkg/internal/infra/events"
	"github.com/keepbuild/seewxapkg/internal/infra/persistence"
	"github.com/keepbuild/seewxapkg/internal/infra/storage"
	"github.com/keepbuild/seewxapkg/internal/pipeline/verify"
)

func TestDetermineFinalStatusCompleted(t *testing.T) {
	status, code, _ := determineFinalStatus(
		true,
		&verify.ManifestVerifyResult{Success: true, PageCount: 1},
		&verify.ArtifactVerifyResult{Success: true, VerifierPassed: true, TotalPages: 1, PageTriplets: 1},
		false,
	)
	if status != task.TaskCompleted || code != "" {
		t.Fatalf("expected completed, got %s code=%s", status, code)
	}
}

func TestDetermineFinalStatusPartialWhenDecompileHasGap(t *testing.T) {
	status, _, _ := determineFinalStatus(
		true,
		&verify.ManifestVerifyResult{Success: true, PageCount: 1},
		&verify.ArtifactVerifyResult{Success: true, VerifierPassed: true, TotalPages: 1, PageTriplets: 1},
		true,
	)
	if status != task.TaskPartial {
		t.Fatalf("expected partial, got %s", status)
	}
}

func TestDetermineFinalStatusFailedOnCriticalArtifacts(t *testing.T) {
	status, code, _ := determineFinalStatus(
		true,
		&verify.ManifestVerifyResult{Success: true, PageCount: 1},
		&verify.ArtifactVerifyResult{Success: false, CriticalFailure: true},
		false,
	)
	if status != task.TaskFailed || code == "" {
		t.Fatalf("expected failed with code, got %s code=%s", status, code)
	}
}

func TestDetermineFinalStatusPartialWhenManifestHasMissingPageArtifacts(t *testing.T) {
	status, code, _ := determineFinalStatus(
		true,
		&verify.ManifestVerifyResult{Success: false, PageCount: 2, MissingPages: []string{"pages/missing"}},
		&verify.ArtifactVerifyResult{Success: false, TotalPages: 2},
		true,
	)
	if status != task.TaskPartial || code != "" {
		t.Fatalf("expected truthful partial delivery, got %s code=%s", status, code)
	}
}

func TestDetermineFinalStatusPartialOnWXMLQualityFailureWithoutDeepRecovery(t *testing.T) {
	status, code, message := determineFinalStatus(
		false,
		&verify.ManifestVerifyResult{Success: true, PageCount: 1},
		&verify.ArtifactVerifyResult{Success: false, WXMLQualityPassed: false, WXMLQualityIssueFiles: 1, TotalPages: 1},
		false,
	)
	if status != task.TaskPartial || code != "" {
		t.Fatalf("WXML quality failure must be reported truthfully, got %s code=%s", status, code)
	}
	if strings.Contains(message, "深度恢复") || !strings.Contains(message, "需检查") {
		t.Fatalf("shallow quality failure message is misleading: %q", message)
	}
}

func TestDetermineFinalStatusAllowsShallowExtractionWithoutSourceTriplets(t *testing.T) {
	status, code, _ := determineFinalStatus(
		false,
		&verify.ManifestVerifyResult{Success: true, PageCount: 1},
		&verify.ArtifactVerifyResult{Success: false, WXMLQualityPassed: true, TotalPages: 1},
		false,
	)
	if status != task.TaskCompleted || code != "" {
		t.Fatalf("shallow extraction should not promise source triplets, got %s code=%s", status, code)
	}
}

func TestDetermineFinalStatusFailsWhenNoManifestPagesRecovered(t *testing.T) {
	status, code, _ := determineFinalStatus(
		true,
		&verify.ManifestVerifyResult{Success: false, PageCount: 0},
		&verify.ArtifactVerifyResult{},
		true,
	)
	if status != task.TaskFailed || code != "manifest_incomplete" {
		t.Fatalf("expected manifest failure, got %s code=%s", status, code)
	}
}

func TestRunTaskPersistsFailedStateAfterPanic(t *testing.T) {
	repo := persistence.NewMemoryTaskRepo()
	now := time.Now()
	queued := &task.Task{ID: "panic-task", Status: task.TaskQueued, CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(context.Background(), queued); err != nil {
		t.Fatal(err)
	}
	service := NewCompileService(&config.Config{
		TempDir:   t.TempDir(),
		OutputDir: t.TempDir(),
	}, repo, nil, nil)

	if err := service.RunTask(context.Background(), queued.ID); err == nil {
		t.Fatal("expected recovered panic to return an error")
	}
	stored, err := repo.Get(context.Background(), queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != task.TaskFailed {
		t.Fatalf("status = %s, want failed", stored.Status)
	}
	if stored.ErrorCode == nil || *stored.ErrorCode != "internal_panic" {
		t.Fatalf("errorCode = %v, want internal_panic", stored.ErrorCode)
	}
}

func TestCollectArtifactFilesPreservesHigherConfidenceSource(t *testing.T) {
	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "app.json"), []byte(`{"pages":["index"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	files := collectArtifactFiles(sourceDir, []task.ArtifactFile{
		{Path: "src/app.json", Kind: "json", Source: "manifest"},
		{Path: "src/app.json", Kind: "json", Source: "fallback"},
	})
	if len(files) != 1 || files[0].Source != "manifest" {
		t.Fatalf("unexpected provenance: %#v", files)
	}
}

func TestFinalizeFailureKeepsRawCauseInternallyAndPublishesSafeEvent(t *testing.T) {
	repo := persistence.NewMemoryTaskRepo()
	broker := events.NewBroker()
	now := time.Now()
	current := &task.Task{
		ID:        "failure-path-task",
		Status:    task.TaskQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := repo.Create(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	service := NewCompileService(&config.Config{
		TempDir:   t.TempDir(),
		OutputDir: t.TempDir(),
	}, repo, broker, nil)
	dirs, err := storage.EnsureTaskDirs(service.cfg.TempDir, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveAppIDSecret(dirs, "wx0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storage.InputFilePath(dirs), []byte("private package"), 0600); err != nil {
		t.Fatal(err)
	}
	broker.Create(current.ID)
	eventStream, _, cancel, err := broker.Subscribe(current.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	rawMessage := "读取 /data/tasks/failure-path-task/input.wxapkg 失败"
	rawCause := errors.New("write /data/output/failure-path-task.zip; inspect /Users/person/private/app.js")

	err = service.finalizeTask(context.Background(), current, task.TaskFailed, "test_failed", rawMessage, rawCause)
	if !errors.Is(err, rawCause) {
		t.Fatalf("finalizeTask error = %v, want original cause", err)
	}
	for _, secret := range []string{"/data/output", "/Users"} {
		if !strings.Contains(err.Error(), secret) {
			t.Fatalf("internal error lost raw cause %q: %v", secret, err)
		}
	}

	stored, err := repo.Get(context.Background(), current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ErrorMessage == nil || stored.CurrentMessage != *stored.ErrorMessage {
		t.Fatalf("persisted task did not retain only the safe failure message: %#v", stored)
	}
	for _, secret := range []string{"/data/tasks", "/data/output", "/Users"} {
		if strings.Contains(stored.CurrentMessage, secret) || strings.Contains(*stored.ErrorMessage, secret) {
			t.Fatalf("persisted terminal state exposed %q: %#v", secret, stored)
		}
	}
	if _, err := os.Stat(storage.AppIDSecretPath(dirs)); !os.IsNotExist(err) {
		t.Fatalf("terminal task retained AppID secret: %v", err)
	}
	if _, err := os.Stat(dirs.InputDir); !os.IsNotExist(err) {
		t.Fatalf("terminal task retained uploaded input: %v", err)
	}

	event, ok := <-eventStream
	if !ok || event.Type != "error" {
		t.Fatalf("terminal event was not delivered before stream removal: %#v ok=%v", event, ok)
	}
	if _, ok := <-eventStream; ok {
		t.Fatal("terminal event stream remained open")
	}
	for _, value := range []string{event.Message, event.Error} {
		for _, secret := range []string{"/data/tasks", "/data/output", "/Users"} {
			if strings.Contains(value, secret) {
				t.Fatalf("public event exposed %q: %#v", secret, event)
			}
		}
	}
	if _, _, _, err := broker.Subscribe(current.ID); !errors.Is(err, events.ErrStreamNotFound) {
		t.Fatalf("terminal stream was retained after delivery: %v", err)
	}
}

func TestFinalizeDeletesAppIDSecretEvenWhenRequestContextIsCanceled(t *testing.T) {
	tempDir := t.TempDir()
	repo, err := persistence.NewFileTaskRepo(filepath.Join(tempDir, "task-state"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	current := &task.Task{ID: "canceled-finalize-task", Status: task.TaskQueued, CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	service := NewCompileService(&config.Config{TempDir: tempDir, OutputDir: t.TempDir()}, repo, events.NewBroker(), nil)
	dirs, err := storage.EnsureTaskDirs(tempDir, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	const appID = "wx0123456789abcdef"
	if err := storage.SaveAppIDSecret(dirs, appID); err != nil {
		t.Fatal(err)
	}
	stateData, err := os.ReadFile(filepath.Join(tempDir, "task-state", current.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stateData), appID) {
		t.Fatal("task state persisted the AppID secret")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.finalizeTask(canceled, current, task.TaskFailed, "canceled", "任务取消", context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("finalizeTask error = %v, want context canceled", err)
	}
	if _, err := os.Stat(storage.AppIDSecretPath(dirs)); !os.IsNotExist(err) {
		t.Fatalf("canceled finalization retained AppID secret: %v", err)
	}
}
