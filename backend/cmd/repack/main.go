package main

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/keepbuild/seewxapkg/internal/domain/task"
	"github.com/keepbuild/seewxapkg/internal/infra/persistence"
	"github.com/keepbuild/seewxapkg/internal/infra/storage"
	"github.com/keepbuild/seewxapkg/internal/report"
)

func main() {
	tempDir := envOrDefault("TEMP_DIR", "/data/tasks")
	outputDir := envOrDefault("OUTPUT_DIR", "/data/output")
	stateDir := filepath.Join(tempDir, "task-state")
	stateFiles, err := filepath.Glob(filepath.Join(stateDir, "*.json"))
	if err != nil {
		fatalf("scan task state: %v", err)
	}
	sort.Strings(stateFiles)
	repo, err := persistence.NewFileTaskRepo(stateDir)
	if err != nil {
		fatalf("initialize task state: %v", err)
	}
	migrated := 0
	skipped := 0
	for index, stateFile := range stateFiles {
		taskID := strings.TrimSuffix(filepath.Base(stateFile), filepath.Ext(stateFile))
		changed, err := migrateTaskArchive(context.Background(), repo, tempDir, outputDir, taskID)
		if err != nil {
			fatalf("migrate task record %d failed (%T)", index+1, err)
		}
		if changed {
			migrated++
		} else {
			skipped++
		}
	}
	fmt.Printf("src-only archive migration complete: migrated=%d skipped=%d\n", migrated, skipped)
}

func migrateTaskArchive(ctx context.Context, repo task.Repository, tempDir, outputDir, taskID string) (bool, error) {
	current, err := repo.Get(ctx, taskID)
	if err != nil {
		return false, err
	}
	if current.ArtifactSummary == nil || !current.ArtifactSummary.DownloadReady || (current.Status != task.TaskCompleted && current.Status != task.TaskPartial) {
		return false, nil
	}

	zipPath := filepath.Join(outputDir, taskID+".zip")
	archiveEntries, srcOnly := readSrcOnlyEntries(zipPath)
	repacked := false
	if !srcOnly {
		sourceDir := filepath.Join(tempDir, taskID, "result", "src")
		archiveEntries, err = storage.ZipDirWithPrefixEntries(sourceDir, zipPath, "src")
		if err != nil {
			return false, fmt.Errorf("repack source tree: %w", err)
		}
		repacked = true
	}
	manifest, err := report.BuildZipManifest(taskID, archiveEntries, "src")
	if err != nil {
		return false, err
	}
	reportsDir := filepath.Join(tempDir, taskID, "result", "reports")
	if err := report.WriteZipManifest(filepath.Join(reportsDir, "zip-manifest.json"), manifest); err != nil {
		return false, fmt.Errorf("write zip manifest: %w", err)
	}
	archiveInfo, err := os.Stat(zipPath)
	if err != nil {
		return false, err
	}
	metadataChanged := current.ArtifactSummary.ArchiveSize != archiveInfo.Size()
	current.ArtifactSummary.ArchiveSize = archiveInfo.Size()
	current.ArtifactSummary.DownloadReady = true
	for index := range current.StageResults {
		if current.StageResults[index].Stage != string(task.TaskPackaging) {
			continue
		}
		if current.StageResults[index].Metrics == nil {
			current.StageResults[index].Metrics = make(map[string]interface{})
		}
		current.StageResults[index].Metrics["archiveSize"] = archiveInfo.Size()
		current.StageResults[index].Metrics["archiveRoot"] = "src/"
		current.StageResults[index].Metrics["zipManifest"] = "report?name=zip-manifest"
	}
	if err := report.WriteRecoveryReport(filepath.Join(reportsDir, "recovery-report.json"), report.BuildRecoveryReport(current)); err != nil {
		return false, fmt.Errorf("write recovery report: %w", err)
	}
	if err := repo.Update(ctx, current); err != nil {
		return false, fmt.Errorf("persist migrated task: %w", err)
	}
	return repacked || metadataChanged, nil
}

func readSrcOnlyEntries(zipPath string) ([]string, bool) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, false
	}
	defer reader.Close()
	if len(reader.File) == 0 {
		return nil, false
	}
	entries := make([]string, 0, len(reader.File))
	seen := make(map[string]struct{}, len(reader.File))
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() || !entry.Mode().IsRegular() || storage.ValidateZipEntryPath(entry.Name) != nil || !strings.HasPrefix(entry.Name, "src/") {
			return nil, false
		}
		if _, duplicate := seen[entry.Name]; duplicate {
			return nil, false
		}
		seen[entry.Name] = struct{}{}
		entries = append(entries, entry.Name)
	}
	sort.Strings(entries)
	return entries, true
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
