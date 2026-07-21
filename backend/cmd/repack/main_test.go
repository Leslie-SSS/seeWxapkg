package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/keepbuild/seewxapkg/internal/domain/task"
	"github.com/keepbuild/seewxapkg/internal/infra/persistence"
	"github.com/keepbuild/seewxapkg/internal/infra/storage"
)

func newTestFileRepo(t *testing.T, path string) task.Repository {
	t.Helper()
	repo, err := persistence.NewFileTaskRepo(path)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestMigrateTaskArchiveRepackagesLegacyLayoutAndUpdatesMetadata(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := t.TempDir()
	taskID := "task-1"
	resultDir := filepath.Join(tempDir, taskID, "result")
	for _, relative := range []string{"src/pages/index.js", "raw/app-service.js", "reports/legacy.json"} {
		path := filepath.Join(resultDir, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(relative), 0644); err != nil {
			t.Fatal(err)
		}
	}
	zipPath := filepath.Join(outputDir, taskID+".zip")
	if _, err := storage.ZipDirWithPrefixEntries(resultDir, zipPath, "legacy"); err != nil {
		t.Fatal(err)
	}
	repo := newTestFileRepo(t, filepath.Join(tempDir, "task-state"))
	current := &task.Task{
		ID:     taskID,
		Status: task.TaskCompleted,
		ArtifactSummary: &task.ArtifactSummary{
			ArchiveSize:   1,
			DownloadReady: true,
		},
		StageResults: []task.StageResult{{
			Stage:   string(task.TaskPackaging),
			Metrics: map[string]interface{}{"archiveSize": float64(1)},
		}},
	}
	if err := repo.Create(context.Background(), current); err != nil {
		t.Fatal(err)
	}

	changed, err := migrateTaskArchive(context.Background(), repo, tempDir, outputDir, taskID)
	if err != nil || !changed {
		t.Fatalf("migration failed: changed=%v err=%v", changed, err)
	}
	entries, ok := readSrcOnlyEntries(zipPath)
	if !ok || len(entries) != 1 || entries[0] != "src/pages/index.js" {
		t.Fatalf("unexpected migrated entries: %#v ok=%v", entries, ok)
	}
	updated, err := repo.Get(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ArtifactSummary.ArchiveSize != info.Size() || !updated.ArtifactSummary.DownloadReady {
		t.Fatalf("archive metadata was not synchronized: %#v size=%d", updated.ArtifactSummary, info.Size())
	}

	manifestData, err := os.ReadFile(filepath.Join(resultDir, "reports", "zip-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil || len(manifest.Files) != 1 || manifest.Files[0] != entries[0] {
		t.Fatalf("manifest does not match migrated ZIP: %#v err=%v", manifest, err)
	}

	recoveryData, err := os.ReadFile(filepath.Join(resultDir, "reports", "recovery-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var recovery struct {
		SnapshotScope string `json:"snapshotScope"`
		Packaging     struct {
			ArchiveSize int64 `json:"archiveSize"`
		} `json:"packaging"`
	}
	if err := json.Unmarshal(recoveryData, &recovery); err != nil || recovery.SnapshotScope != "live-task" || recovery.Packaging.ArchiveSize != info.Size() {
		t.Fatalf("recovery report was not synchronized: %#v err=%v", recovery, err)
	}
}

func TestMigrateTaskArchiveSkipsNonDownloadableTask(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := t.TempDir()
	repo := newTestFileRepo(t, filepath.Join(tempDir, "task-state"))
	current := &task.Task{ID: "failed", Status: task.TaskFailed, ArtifactSummary: &task.ArtifactSummary{DownloadReady: true}}
	if err := repo.Create(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	if changed, err := migrateTaskArchive(context.Background(), repo, tempDir, outputDir, current.ID); err != nil || changed {
		t.Fatalf("failed task should be skipped: changed=%v err=%v", changed, err)
	}
}

func TestMigrateTaskArchiveEmptySourcePreservesPublishedState(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := t.TempDir()
	taskID := "task-empty-source"
	resultDir := filepath.Join(tempDir, taskID, "result")
	if err := os.MkdirAll(filepath.Join(resultDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	reportsDir := filepath.Join(resultDir, "reports")
	if err := os.MkdirAll(reportsDir, 0755); err != nil {
		t.Fatal(err)
	}

	zipPath := filepath.Join(outputDir, taskID+".zip")
	wantZip := []byte("previous-published-archive")
	if err := os.WriteFile(zipPath, wantZip, 0644); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(reportsDir, "zip-manifest.json")
	wantManifest := []byte(`{"taskId":"task-empty-source","files":["legacy/app.js"]}`)
	if err := os.WriteFile(manifestPath, wantManifest, 0644); err != nil {
		t.Fatal(err)
	}

	repo := newTestFileRepo(t, filepath.Join(tempDir, "task-state"))
	current := &task.Task{
		ID:     taskID,
		Status: task.TaskCompleted,
		ArtifactSummary: &task.ArtifactSummary{
			ArchiveSize:   int64(len(wantZip)),
			DownloadReady: true,
		},
		StageResults: []task.StageResult{{
			Stage:   string(task.TaskPackaging),
			Metrics: map[string]interface{}{"archiveSize": int64(len(wantZip))},
		}},
	}
	if err := repo.Create(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	wantTask, err := repo.Get(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}

	changed, err := migrateTaskArchive(context.Background(), repo, tempDir, outputDir, taskID)
	if err == nil || changed {
		t.Fatalf("empty source migration should fail safely: changed=%v err=%v", changed, err)
	}
	gotZip, readErr := os.ReadFile(zipPath)
	if readErr != nil || !bytes.Equal(gotZip, wantZip) {
		t.Fatalf("published ZIP changed after rejected migration: got=%q err=%v", gotZip, readErr)
	}
	gotManifest, readErr := os.ReadFile(manifestPath)
	if readErr != nil || !bytes.Equal(gotManifest, wantManifest) {
		t.Fatalf("manifest changed after rejected migration: got=%q err=%v", gotManifest, readErr)
	}
	gotTask, err := repo.Get(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotTask, wantTask) {
		t.Fatalf("task metadata changed after rejected migration:\nwant: %#v\n got: %#v", wantTask, gotTask)
	}
}

func TestReadSrcOnlyEntriesRejectsUnsafeArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unsafe.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create(`src/..\..\escape.js`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("bad")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if entries, ok := readSrcOnlyEntries(path); ok || entries != nil {
		t.Fatalf("unsafe archive accepted: %#v", entries)
	}
}
