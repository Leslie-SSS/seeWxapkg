package storage

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestTaskStorageUsesPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}
	dirs, err := EnsureTaskDirs(t.TempDir(), "task-private")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dirs.RootDir, dirs.InputDir, dirs.SourceDir, dirs.ReportsDir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0700 {
			t.Fatalf("task directory %s mode = %o, want 700", path, got)
		}
	}

	reportPath := filepath.Join(dirs.ReportsDir, "privacy.json")
	if err := WriteJSON(reportPath, map[string]string{"status": "ok"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("task file mode = %o, want 600", got)
	}
	const appID = "wx0123456789abcdef"
	if err := SaveAppIDSecret(dirs, appID); err != nil {
		t.Fatal(err)
	}
	secretInfo, err := os.Stat(AppIDSecretPath(dirs))
	if err != nil {
		t.Fatal(err)
	}
	if got := secretInfo.Mode().Perm(); got != 0600 {
		t.Fatalf("AppID secret mode = %o, want 600", got)
	}
	if got, err := ReadAppIDSecret(dirs); err != nil || got != appID {
		t.Fatalf("ReadAppIDSecret = %q, %v", got, err)
	}
	if err := DeleteAppIDSecret(dirs); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(AppIDSecretPath(dirs)); !os.IsNotExist(err) {
		t.Fatalf("AppID secret was not deleted: %v", err)
	}
}

func TestResetTaskWorkspacePreservesOnlyInput(t *testing.T) {
	dirs, err := EnsureTaskDirs(t.TempDir(), "task-retry")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(InputFilePath(dirs), []byte("package"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveAppIDSecret(dirs, "wx0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(dirs.SourceDir, "partial.js"),
		filepath.Join(dirs.ReportsDir, "partial.json"),
		filepath.Join(dirs.RootDir, "fallback", "input", "partial.wxml"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("partial"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := ResetTaskWorkspace(dirs); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{InputFilePath(dirs), AppIDSecretPath(dirs)} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("input was removed during retry reset: %s: %v", path, err)
		}
	}
	for _, path := range []string{filepath.Join(dirs.SourceDir, "partial.js"), filepath.Join(dirs.ReportsDir, "partial.json"), filepath.Join(dirs.RootDir, "fallback")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("derived retry data survived reset: %s: %v", path, err)
		}
	}
}

func TestDeleteTaskInputPreservesRecoveredArtifacts(t *testing.T) {
	dirs, err := EnsureTaskDirs(t.TempDir(), "task-terminal")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(InputFilePath(dirs), []byte("private-package"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveAppIDSecret(dirs, "wx0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(dirs.SourceDir, "app.js")
	if err := os.WriteFile(artifact, []byte("App({})"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := DeleteTaskInput(dirs); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dirs.InputDir); !os.IsNotExist(err) {
		t.Fatalf("terminal input directory was retained: %v", err)
	}
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("recovered artifact was removed: %v", err)
	}
}

func TestWriteJSONFailurePreservesExistingFileAndRemovesTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "state.json")
	want := []byte("{\"status\":\"published\"}\n")
	if err := os.WriteFile(destination, want, 0644); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(destination, map[string]interface{}{"unsupported": make(chan struct{})}); err == nil {
		t.Fatal("expected JSON marshaling to fail")
	}
	got, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("failed write changed the published file: got=%q err=%v", got, err)
	}

	// Replacing a directory with a regular file deterministically fails after
	// the temporary file is written, including on Windows and when run as root.
	renameBlocker := filepath.Join(root, "rename-blocker")
	if err := os.Mkdir(renameBlocker, 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(renameBlocker, map[string]string{"status": "replacement"}); err == nil {
		t.Fatal("expected atomic rename onto a directory to fail")
	}
	matches, err := filepath.Glob(filepath.Join(root, ".json-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary JSON files were left behind: %v", matches)
	}
}

func TestZipDirStreamsFilesAndProducesReadableArchive(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(source, "pages"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	want := bytes.Repeat([]byte("wxapkg-data\n"), 100_000)
	if err := os.WriteFile(filepath.Join(source, "pages", "index.js"), want, 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	destination := filepath.Join(root, "result.zip")
	if _, err := ZipDirWithPrefixEntries(source, destination, "src"); err != nil {
		t.Fatalf("ZipDirWithPrefixEntries returned error: %v", err)
	}

	reader, err := zip.OpenReader(destination)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer reader.Close()
	if len(reader.File) != 1 || reader.File[0].Name != "src/pages/index.js" {
		t.Fatalf("unexpected archive entries: %+v", reader.File)
	}
	file, err := reader.File[0].Open()
	if err != nil {
		t.Fatalf("open entry: %v", err)
	}
	got, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(got, want) {
		t.Fatalf("archive content mismatch: readErr=%v closeErr=%v size=%d", readErr, closeErr, len(got))
	}
}

func TestZipDirWithPrefixKeepsEntriesUnderOneTopLevelDirectory(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(source, "pages"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "pages", "index.js"), []byte("Page({})"), 0644); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "result.zip")
	entries, err := ZipDirWithPrefixEntries(source, destination, "src")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0] != "src/pages/index.js" {
		t.Fatalf("reported entries do not match written archive: %#v", entries)
	}
	reader, err := zip.OpenReader(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if len(reader.File) != 1 || reader.File[0].Name != "src/pages/index.js" {
		t.Fatalf("unexpected prefixed entries: %+v", reader.File)
	}
}

func TestValidateZipEntryPathRejectsCrossPlatformTraversal(t *testing.T) {
	for _, entry := range []string{"", ".", "../escape.js", "src/../../escape.js", `src/..\..\escape.js`, "src/C:/escape.js", "/src/app.js", "src//app.js", "src/app.js/"} {
		if err := ValidateZipEntryPath(entry); err == nil {
			t.Fatalf("expected unsafe ZIP entry %q to be rejected", entry)
		}
	}
	if err := ValidateZipEntryPath("src/pages/home/index.js"); err != nil {
		t.Fatalf("safe ZIP entry rejected: %v", err)
	}
}

func TestZipDirFailurePreservesPublishedArchive(t *testing.T) {
	if filepath.Separator == '\\' {
		t.Skip("backslash cannot be created as a filename on Windows")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, `..\..\escape.js`), []byte("bad"), 0644); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "result.zip")
	want := []byte("previous-published-archive")
	if err := os.WriteFile(destination, want, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ZipDirWithPrefixEntries(source, destination, "src"); err == nil {
		t.Fatal("expected cross-platform unsafe source name to be rejected")
	}
	got, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("failed rebuild damaged the published archive: got=%q err=%v", got, err)
	}
}

func TestZipDirWithPrefixRejectsUnsafePrefix(t *testing.T) {
	source := t.TempDir()
	for _, prefix := range []string{"", ".", "../src", "/src", "src/../other", `src\other`, "src\x00other", "C:/src"} {
		if _, err := ZipDirWithPrefixEntries(source, filepath.Join(t.TempDir(), "result.zip"), prefix); err == nil {
			t.Fatalf("expected unsafe prefix %q to be rejected", prefix)
		}
	}
}

func TestZipDirRejectsSymlinkAndRemovesPartialArchive(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	outside := filepath.Join(root, "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0644); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	destination := filepath.Join(root, "result.zip")
	if _, err := ZipDirWithPrefixEntries(source, destination, "src"); err == nil {
		t.Fatal("expected symlink to be rejected")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("partial archive was retained: %v", err)
	}
}

func TestZipDirRejectsSymlinkSourceRoot(t *testing.T) {
	root := t.TempDir()
	realSource := filepath.Join(root, "real-source")
	if err := os.MkdirAll(realSource, 0755); err != nil {
		t.Fatal(err)
	}
	linkedSource := filepath.Join(root, "linked-source")
	if err := os.Symlink(realSource, linkedSource); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := ZipDirWithPrefixEntries(linkedSource, filepath.Join(root, "result.zip"), "src"); err == nil {
		t.Fatal("expected symbolic-link source root to be rejected")
	}
}

func TestZipDirRejectsDestinationInsideSource(t *testing.T) {
	source := t.TempDir()
	if _, err := ZipDirWithPrefixEntries(source, filepath.Join(source, "result.zip"), "src"); err == nil {
		t.Fatal("expected destination-inside-source error")
	}
}

func TestRetentionCleanupPreservesStateDirectoryAndExpiresOldRecords(t *testing.T) {
	tempDir := filepath.Join(t.TempDir(), "temp")
	outputDir := filepath.Join(t.TempDir(), "output")
	expiredTaskID := "11111111-1111-4111-8111-111111111111"
	activeTaskID := "22222222-2222-4222-8222-222222222222"
	old := time.Now().Add(-48 * time.Hour)
	for _, path := range []string{
		filepath.Join(tempDir, "queue", "pending"),
		filepath.Join(tempDir, "queue", "dlq"),
		filepath.Join(tempDir, "task-state"),
		filepath.Join(tempDir, expiredTaskID),
		filepath.Join(tempDir, activeTaskID),
		outputDir,
	} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	expiredArchive := filepath.Join(outputDir, expiredTaskID+".zip")
	activeArchive := filepath.Join(outputDir, activeTaskID+".zip")
	unrelatedTemp := filepath.Join(tempDir, "unrelated-old-directory")
	unrelatedOutput := filepath.Join(outputDir, "unrelated-old-file.zip")
	for _, path := range []string{expiredArchive, activeArchive, unrelatedOutput} {
		if err := os.WriteFile(path, []byte("zip"), 0644); err != nil {
			t.Fatalf("write output: %v", err)
		}
	}
	if err := os.MkdirAll(unrelatedTemp, 0755); err != nil {
		t.Fatalf("write output: %v", err)
	}
	oldState := filepath.Join(tempDir, "task-state", expiredTaskID+".json")
	oldAtomicRemainder := filepath.Join(tempDir, "task-state", ".task-crash.tmp")
	newState := filepath.Join(tempDir, "task-state", activeTaskID+".json")
	newAtomicRemainder := filepath.Join(tempDir, "task-state", ".task-active.tmp")
	ignoredStateFile := filepath.Join(tempDir, "task-state", "do-not-delete.txt")
	for _, path := range []string{oldState, oldAtomicRemainder, newState, newAtomicRemainder, ignoredStateFile} {
		if err := os.WriteFile(path, []byte(`{"status":"completed"}`), 0644); err != nil {
			t.Fatalf("write task state: %v", err)
		}
	}
	oldQueueFailure := filepath.Join(tempDir, "queue", "dlq", "expired.job")
	newQueueFailure := filepath.Join(tempDir, "queue", "dlq", "active.job")
	oldPending := filepath.Join(tempDir, "queue", "pending", "expired.job")
	oldWorking := filepath.Join(tempDir, "queue", "pending", "expired.job.worker.working")
	newPending := filepath.Join(tempDir, "queue", "pending", "active.job")
	for _, path := range []string{oldQueueFailure, newQueueFailure, oldPending, oldWorking, newPending} {
		if err := os.WriteFile(path, []byte(`{"taskId":"redacted"}`), 0600); err != nil {
			t.Fatalf("write queue failure: %v", err)
		}
	}
	for _, path := range []string{
		filepath.Join(tempDir, "queue"),
		filepath.Join(tempDir, "task-state"),
		filepath.Join(tempDir, expiredTaskID),
		expiredArchive,
		unrelatedTemp,
		unrelatedOutput,
		oldState,
		oldAtomicRemainder,
		ignoredStateFile,
		oldQueueFailure,
		oldPending,
		oldWorking,
	} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatalf("age %s: %v", path, err)
		}
	}

	cleanupRetentionRoots(tempDir, outputDir, 24*time.Hour)
	for _, name := range []string{"queue", "task-state"} {
		if _, err := os.Stat(filepath.Join(tempDir, name)); err != nil {
			t.Fatalf("persistent directory %s was removed: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(tempDir, expiredTaskID)); !os.IsNotExist(err) {
		t.Fatalf("expired task was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, activeTaskID)); err != nil {
		t.Fatalf("active task was removed: %v", err)
	}
	if _, err := os.Stat(expiredArchive); !os.IsNotExist(err) {
		t.Fatalf("expired archive was not removed: %v", err)
	}
	if _, err := os.Stat(activeArchive); err != nil {
		t.Fatalf("active archive was removed: %v", err)
	}
	for _, path := range []string{unrelatedTemp, unrelatedOutput} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unrelated old entry was removed: %s: %v", path, err)
		}
	}
	if _, err := os.Stat(oldState); !os.IsNotExist(err) {
		t.Fatalf("expired task-state record was not removed: %v", err)
	}
	if _, err := os.Stat(oldAtomicRemainder); !os.IsNotExist(err) {
		t.Fatalf("expired atomic task-state remainder was not removed: %v", err)
	}
	if _, err := os.Stat(newState); err != nil {
		t.Fatalf("active task-state record was removed: %v", err)
	}
	if _, err := os.Stat(newAtomicRemainder); err != nil {
		t.Fatalf("active atomic task-state remainder was removed: %v", err)
	}
	if _, err := os.Stat(ignoredStateFile); err != nil {
		t.Fatalf("unrelated task-state file was removed: %v", err)
	}
	if _, err := os.Stat(oldQueueFailure); !os.IsNotExist(err) {
		t.Fatalf("expired queue failure was not removed: %v", err)
	}
	if _, err := os.Stat(newQueueFailure); err != nil {
		t.Fatalf("active queue failure was removed: %v", err)
	}
	for _, expired := range []string{oldPending, oldWorking} {
		if _, err := os.Stat(expired); !os.IsNotExist(err) {
			t.Fatalf("expired queue record was not removed: %s: %v", expired, err)
		}
	}
	if _, err := os.Stat(newPending); err != nil {
		t.Fatalf("active pending job was removed: %v", err)
	}
}
