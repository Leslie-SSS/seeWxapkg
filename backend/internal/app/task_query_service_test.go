package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/keepbuild/seewxapkg/internal/config"
	"github.com/keepbuild/seewxapkg/internal/domain/task"
	"github.com/keepbuild/seewxapkg/internal/infra/persistence"
)

func TestGetNamedReportAllowsOnlyKnownReports(t *testing.T) {
	tempDir := t.TempDir()
	repo := persistence.NewMemoryTaskRepo()
	current := &task.Task{ID: "task-1", ArtifactSummary: &task.ArtifactSummary{FileCount: 1}}
	if err := repo.Create(context.Background(), current); err != nil {
		t.Fatalf("create task: %v", err)
	}
	reportsDir := filepath.Join(tempDir, current.ID, "result", "reports")
	if err := os.MkdirAll(reportsDir, 0755); err != nil {
		t.Fatalf("mkdir reports: %v", err)
	}
	want := []byte(`{"ok":true}`)
	if err := os.WriteFile(filepath.Join(reportsDir, "js-recovery-report.json"), want, 0644); err != nil {
		t.Fatalf("write report: %v", err)
	}

	service := NewTaskQueryService(&config.Config{TempDir: tempDir}, repo)
	got, err := service.GetNamedReport(context.Background(), current.ID, "js-recovery-report")
	if err != nil || string(got) != string(want) {
		t.Fatalf("unexpected allowed report: got=%q err=%v", got, err)
	}

	secretPath := filepath.Join(tempDir, "secret.json")
	if err := os.WriteFile(secretPath, []byte("secret"), 0644); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	for _, name := range []string{"../../secret", "../recovery-report", "unknown", "" + string([]byte{0})} {
		if _, err := service.GetNamedReport(context.Background(), current.ID, name); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected %q to be rejected, got %v", name, err)
		}
	}
}

func TestResolvePathsRejectInvalidTaskID(t *testing.T) {
	service := NewTaskQueryService(&config.Config{TempDir: t.TempDir(), OutputDir: t.TempDir()}, persistence.NewMemoryTaskRepo())
	for _, id := range []string{"", "../task", "..", "nested/task"} {
		if path := service.ResolveZipPath(id); path != "" {
			t.Fatalf("expected invalid task id %q to be rejected, got %q", id, path)
		}
	}
}

func TestResolveReadyZipPathRequiresPersistedTerminalReadyTask(t *testing.T) {
	repo := persistence.NewMemoryTaskRepo()
	service := NewTaskQueryService(&config.Config{OutputDir: t.TempDir()}, repo)
	for _, current := range []*task.Task{
		{ID: "processing", Status: task.TaskPackaging, ArtifactSummary: &task.ArtifactSummary{DownloadReady: true}},
		{ID: "failed", Status: task.TaskFailed, ArtifactSummary: &task.ArtifactSummary{DownloadReady: true}},
		{ID: "not-ready", Status: task.TaskCompleted, ArtifactSummary: &task.ArtifactSummary{DownloadReady: false}},
	} {
		if err := repo.Create(context.Background(), current); err != nil {
			t.Fatal(err)
		}
		if path, err := service.ResolveReadyZipPath(context.Background(), current.ID); err == nil || path != "" {
			t.Fatalf("task %#v unexpectedly resolved download %q", current, path)
		}
	}

	ready := &task.Task{ID: "ready", Status: task.TaskPartial, ArtifactSummary: &task.ArtifactSummary{DownloadReady: true}}
	if err := repo.Create(context.Background(), ready); err != nil {
		t.Fatal(err)
	}
	path, err := service.ResolveReadyZipPath(context.Background(), ready.ID)
	if err != nil || filepath.Base(path) != "ready.zip" {
		t.Fatalf("ready task did not resolve its archive: path=%q err=%v", path, err)
	}
}

func TestReportReadsSanitizeLegacyStoredPaths(t *testing.T) {
	tempDir := t.TempDir()
	repo := persistence.NewMemoryTaskRepo()
	current := &task.Task{ID: "legacy-task", ArtifactSummary: &task.ArtifactSummary{FileCount: 1}}
	if err := repo.Create(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	reportsDir := filepath.Join(tempDir, current.ID, "result", "reports")
	if err := os.MkdirAll(reportsDir, 0755); err != nil {
		t.Fatal(err)
	}
	legacyRecovery := []byte(`{"taskId":"legacy-task","status":"partial","snapshotScope":"live-task","diagnostics":[{"code":"legacy","severity":"warn","message":"open /app/tasks/input.wxapkg","file":"/data/tasks/legacy-task/result/src/app.js"}],"artifacts":{"fileCount":1,"files":[{"path":"/workspace/result/src/app.js","kind":"js","source":"legacy"}],"downloadReady":true},"packaging":{"status":"ready","downloadReady":true}}`)
	if err := os.WriteFile(filepath.Join(reportsDir, "recovery-report.json"), legacyRecovery, 0644); err != nil {
		t.Fatal(err)
	}
	legacyNamed := []byte(`{"nested":{"input":"C:\\Users\\person\\private\\app.js","output":"/srv/output/result.zip"}}`)
	if err := os.WriteFile(filepath.Join(reportsDir, "format-report.json"), legacyNamed, 0644); err != nil {
		t.Fatal(err)
	}

	service := NewTaskQueryService(&config.Config{TempDir: tempDir}, repo)
	recovery, err := service.GetReport(context.Background(), current.ID)
	if err != nil {
		t.Fatal(err)
	}
	recoveryJSON, err := json.Marshal(recovery)
	if err != nil {
		t.Fatal(err)
	}
	namedJSON, err := service.GetNamedReport(context.Background(), current.ID, "format-report")
	if err != nil {
		t.Fatal(err)
	}
	for _, publicJSON := range [][]byte{recoveryJSON, namedJSON} {
		for _, secret := range []string{"/app/", "/data/", "/workspace/", "/srv/", `C:\\Users`} {
			if strings.Contains(string(publicJSON), secret) {
				t.Fatalf("legacy report exposed %q: %s", secret, publicJSON)
			}
		}
	}
}

func TestGetNamedReportAllowsPublishedReportLinks(t *testing.T) {
	tempDir := t.TempDir()
	repo := persistence.NewMemoryTaskRepo()
	current := &task.Task{ID: "task-1", ArtifactSummary: &task.ArtifactSummary{FileCount: 1}}
	if err := repo.Create(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	reportsDir := filepath.Join(tempDir, current.ID, "result", "reports")
	if err := os.MkdirAll(reportsDir, 0755); err != nil {
		t.Fatal(err)
	}

	service := NewTaskQueryService(&config.Config{TempDir: tempDir}, repo)
	for _, name := range []string{"format-report", "zip-manifest", "package-profile"} {
		want := []byte(`{"name":"` + name + `"}`)
		if err := os.WriteFile(filepath.Join(reportsDir, name+".json"), want, 0644); err != nil {
			t.Fatal(err)
		}
		got, err := service.GetNamedReport(context.Background(), current.ID, name)
		if err != nil {
			t.Fatalf("GetNamedReport(%q): %v", name, err)
		}
		var gotValue, wantValue interface{}
		if json.Unmarshal(got, &gotValue) != nil || json.Unmarshal(want, &wantValue) != nil || !reflect.DeepEqual(gotValue, wantValue) {
			t.Fatalf("GetNamedReport(%q) = %q, want JSON %q", name, got, want)
		}
	}
}
