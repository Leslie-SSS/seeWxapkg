package storage

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type TaskDirs struct {
	RootDir    string
	InputDir   string
	SourceDir  string
	ReportsDir string
}

func EnsureTaskDirs(base, taskID string) (TaskDirs, error) {
	root := filepath.Join(base, taskID)
	dirs := TaskDirs{
		RootDir:    root,
		InputDir:   filepath.Join(root, "input"),
		SourceDir:  filepath.Join(root, "result", "src"),
		ReportsDir: filepath.Join(root, "result", "reports"),
	}

	for _, dir := range []string{dirs.RootDir, dirs.InputDir, dirs.SourceDir, dirs.ReportsDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return TaskDirs{}, err
		}
		if err := os.Chmod(dir, 0700); err != nil {
			return TaskDirs{}, err
		}
	}

	return dirs, nil
}

// ResetTaskWorkspace removes every derived file from an interrupted attempt
// while preserving the uploaded package and its one-shot AppID secret.
func ResetTaskWorkspace(dirs TaskDirs) error {
	for _, path := range []string{filepath.Join(dirs.RootDir, "result"), filepath.Join(dirs.RootDir, "fallback")} {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	for _, path := range []string{dirs.SourceDir, dirs.ReportsDir} {
		if err := os.MkdirAll(path, 0700); err != nil {
			return err
		}
		if err := os.Chmod(path, 0700); err != nil {
			return err
		}
	}
	return syncDirectory(dirs.RootDir)
}

func InputFilePath(dirs TaskDirs) string {
	return filepath.Join(dirs.InputDir, "input.wxapkg")
}

func AppIDSecretPath(dirs TaskDirs) string {
	return filepath.Join(dirs.InputDir, ".appid")
}

func SaveAppIDSecret(dirs TaskDirs, appID string) error {
	if appID == "" {
		return nil
	}
	return writePrivateFileAtomic(AppIDSecretPath(dirs), func(file *os.File) error {
		_, err := file.Write([]byte(appID))
		return err
	})
}

func ReadAppIDSecret(dirs TaskDirs) (string, error) {
	data, err := os.ReadFile(AppIDSecretPath(dirs))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func DeleteAppIDSecret(dirs TaskDirs) error {
	err := os.Remove(AppIDSecretPath(dirs))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return syncDirectory(dirs.InputDir)
}

// DeleteTaskInput removes the uploaded package and any one-shot credential as
// soon as a task reaches a terminal state. Reports, recovered source and the
// downloadable archive are retained independently according to policy.
func DeleteTaskInput(dirs TaskDirs) error {
	if err := os.RemoveAll(dirs.InputDir); err != nil {
		return err
	}
	return syncDirectory(dirs.RootDir)
}

// SaveDiagnosticSample retains a failed/partial task's package (preferring the
// decrypted bytes, which carry the most analysis value) together with the
// one-shot AppID in a private per-task directory. Samples are never exposed
// through the API and are removed by the retention janitor like other
// artifacts. bestEffort=true means failures are logged, never fatal.
func SaveDiagnosticSample(samplesDir string, taskID string, data []byte, appID string) error {
	if samplesDir == "" || taskID == "" || len(data) == 0 {
		return nil
	}
	dir := filepath.Join(samplesDir, taskID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := writePrivateFileAtomic(filepath.Join(dir, "input.wxapkg"), func(file *os.File) error {
		_, err := file.Write(data)
		return err
	}); err != nil {
		return err
	}
	if appID != "" {
		if err := writePrivateFileAtomic(filepath.Join(dir, "appid.txt"), func(file *os.File) error {
			_, err := file.Write([]byte(appID))
			return err
		}); err != nil {
			return err
		}
	}
	return syncDirectory(dir)
}

// CleanupDiagnosticSamples removes per-task sample directories older than the
// cutoff. The janitor calls this alongside artifact cleanup; the samples dir is
// a separate root so a failure there never disturbs artifact retention.
func CleanupDiagnosticSamples(samplesDir string, cutoff time.Duration) {
	if samplesDir == "" {
		return
	}
	entries, err := os.ReadDir(samplesDir)
	if err != nil {
		return
	}
	deadline := time.Now().Add(-cutoff)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(deadline) {
			_ = os.RemoveAll(filepath.Join(samplesDir, entry.Name()))
		}
	}
}

func SaveUploadedFile(dirs TaskDirs, file *multipart.FileHeader) (string, error) {
	src, err := file.Open()
	if err != nil {
		return "", err
	}
	defer src.Close()

	dstPath := InputFilePath(dirs)
	if err := writePrivateFileAtomic(dstPath, func(dst *os.File) error {
		_, err := io.Copy(dst, src)
		return err
	}); err != nil {
		return "", err
	}
	return dstPath, nil
}

func WriteJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFileAtomic(path, func(file *os.File) error {
		_, err := file.Write(data)
		return err
	})
}

func writePrivateFileAtomic(path string, write func(*os.File) error) (retErr error) {
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".private-*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := file.Chmod(0600); err != nil {
		return err
	}
	if err := write(file); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	if err := syncDirectory(directory); err != nil {
		return err
	}
	committed = true
	return nil
}

// ZipDirWithPrefixEntries returns the exact entry names written to the
// successfully published archive. Callers can use this single source of truth
// for a manifest instead of racing a second filesystem walk.
func ZipDirWithPrefixEntries(src, dst, prefix string) ([]string, error) {
	archivePrefix, err := normalizeArchivePrefix(prefix)
	if err != nil {
		return nil, err
	}
	return zipDir(src, dst, archivePrefix)
}

func normalizeArchivePrefix(prefix string) (string, error) {
	if err := ValidateZipEntryPath(prefix); err != nil {
		return "", fmt.Errorf("invalid ZIP entry prefix %q", prefix)
	}
	return pathpkg.Clean(prefix), nil
}

// ValidateZipEntryPath applies platform-independent ZIP path rules. In
// particular, backslashes and drive separators are rejected even on Linux so
// a later Windows extraction cannot reinterpret them as traversal syntax.
func ValidateZipEntryPath(value string) error {
	if value == "" || strings.ContainsAny(value, "\\\x00:") || strings.HasPrefix(value, "/") || filepath.VolumeName(value) != "" {
		return fmt.Errorf("unsafe ZIP entry path %q", value)
	}
	cleaned := pathpkg.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != value {
		return fmt.Errorf("unsafe ZIP entry path %q", value)
	}
	return nil
}

// SafePackageOutputPath resolves a wxapkg entry below root using POSIX package
// path semantics on every host OS. A leading slash means package-root relative,
// as used by legitimate wxapkg files; Windows separators and drive syntax are
// never accepted.
func SafePackageOutputPath(root, name string) (string, error) {
	if name == "" || strings.ContainsAny(name, "\\\x00:") {
		return "", fmt.Errorf("invalid file path: %q", name)
	}
	trimmed := strings.TrimLeft(name, "/")
	if trimmed == "" {
		return "", fmt.Errorf("invalid file path: %q", name)
	}
	cleaned := pathpkg.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("file path escapes output directory: %q", name)
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve output directory: %w", err)
	}
	target := filepath.Join(base, filepath.FromSlash(cleaned))
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file path escapes output directory: %q", name)
	}
	return target, nil
}

func zipDir(src, dst, archivePrefix string) (entries []string, retErr error) {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return nil, err
	}
	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		return nil, err
	}
	if inside, err := pathWithin(srcAbs, dstAbs); err != nil {
		return nil, err
	} else if inside {
		return nil, fmt.Errorf("zip destination must not be inside source directory")
	}
	if info, err := os.Lstat(srcAbs); err != nil {
		return nil, err
	} else if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("zip source must not be a symbolic link: %s", src)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("zip source is not a directory: %s", src)
	}
	if err := os.MkdirAll(filepath.Dir(dstAbs), 0700); err != nil {
		return nil, err
	}

	file, err := os.CreateTemp(filepath.Dir(dstAbs), ".archive-*.tmp")
	if err != nil {
		return nil, err
	}
	tmpPath := file.Name()
	defer func() {
		if retErr != nil {
			_ = file.Close()
		}
		_ = os.Remove(tmpPath)
	}()

	writer := zip.NewWriter(file)
	walkErr := filepath.Walk(srcAbs, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}

		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to archive symbolic link: %s", path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to archive non-regular file: %s", path)
		}

		relPath, err := filepath.Rel(srcAbs, path)
		if err != nil {
			return err
		}
		entryPath := filepath.ToSlash(relPath)
		if err := ValidateZipEntryPath(entryPath); err != nil {
			return err
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = entryPath
		if archivePrefix != "" {
			header.Name = pathpkg.Join(archivePrefix, header.Name)
		}
		if err := ValidateZipEntryPath(header.Name); err != nil {
			return err
		}
		header.Method = zip.Deflate

		writerFile, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}

		source, err := os.Open(path)
		if err != nil {
			return err
		}
		openedInfo, statErr := source.Stat()
		if statErr != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
			_ = source.Close()
			if statErr != nil {
				return statErr
			}
			return fmt.Errorf("ZIP source changed while archiving: %s", path)
		}
		_, copyErr := io.Copy(writerFile, source)
		closeErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		entries = append(entries, header.Name)
		return nil
	})
	if walkErr != nil {
		_ = writer.Close()
		return nil, walkErr
	}
	if len(entries) == 0 {
		_ = writer.Close()
		return nil, fmt.Errorf("refusing to publish empty ZIP archive")
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if err := file.Sync(); err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpPath, dstAbs); err != nil {
		return nil, err
	}
	if err := syncDirectory(filepath.Dir(dstAbs)); err != nil {
		return nil, err
	}
	return entries, nil
}

func StartRetentionJanitor(ctx context.Context, tempDir, outputDir string, retainHours int) {
	StartRetentionJanitorWithSamples(ctx, tempDir, outputDir, "", retainHours)
}

// StartRetentionJanitorWithSamples also sweeps the diagnostic samples dir when
// sample collection is enabled.
func StartRetentionJanitorWithSamples(ctx context.Context, tempDir, outputDir, samplesDir string, retainHours int) {
	if retainHours <= 0 {
		return
	}
	ticker := time.NewTicker(time.Hour)
	go func() {
		defer ticker.Stop()
		cutoff := time.Duration(retainHours) * time.Hour
		cleanupRetentionRoots(tempDir, outputDir, cutoff)
		CleanupDiagnosticSamples(samplesDir, cutoff)
		for {
			select {
			case <-ticker.C:
				cleanupRetentionRoots(tempDir, outputDir, cutoff)
				CleanupDiagnosticSamples(samplesDir, cutoff)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func cleanupRetentionRoots(tempDir, outputDir string, cutoff time.Duration) {
	tempClean := filepath.Clean(tempDir)
	outputClean := filepath.Clean(outputDir)
	tempPreserved := map[string]struct{}{
		"queue":      {},
		"task-state": {},
	}
	if child := directChildContaining(tempClean, outputClean); child != "" {
		tempPreserved[child] = struct{}{}
	}
	if tempClean == outputClean {
		cleanupArtifactsExcept(tempClean, cutoff, tempPreserved, func(entry os.DirEntry) bool {
			return isTaskArtifactEntry(entry) || isOutputArtifactEntry(entry)
		})
		cleanupOldStateFiles(filepath.Join(tempClean, "task-state"), cutoff)
		cleanupOldQueueRecords(filepath.Join(tempClean, "queue"), cutoff)
		return
	}
	outputPreserved := make(map[string]struct{})
	if child := directChildContaining(outputClean, tempClean); child != "" {
		outputPreserved[child] = struct{}{}
	}
	cleanupArtifactsExcept(tempClean, cutoff, tempPreserved, isTaskArtifactEntry)
	cleanupArtifactsExcept(outputClean, cutoff, outputPreserved, isOutputArtifactEntry)
	cleanupOldStateFiles(filepath.Join(tempClean, "task-state"), cutoff)
	cleanupOldQueueRecords(filepath.Join(tempClean, "queue"), cutoff)
}

func cleanupOldStateFiles(root string, cutoff time.Duration) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[Retention] task-state scan failed (%T)", err)
		}
		return
	}
	expireBefore := time.Now().Add(-cutoff)
	failed := 0
	for _, entry := range entries {
		name := entry.Name()
		isTaskState := strings.HasSuffix(name, ".json") && isCanonicalTaskID(strings.TrimSuffix(name, ".json"))
		isAtomicRemainder := strings.HasPrefix(name, ".task-") && strings.HasSuffix(name, ".tmp")
		if entry.IsDir() || !isTaskState && !isAtomicRemainder {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			failed++
			continue
		}
		if !info.Mode().IsRegular() || info.ModTime().After(expireBefore) {
			continue
		}
		if err := os.Remove(filepath.Join(root, entry.Name())); err != nil && !os.IsNotExist(err) {
			failed++
		}
	}
	if failed > 0 {
		log.Printf("[Retention] failed to remove %d expired task-state records", failed)
	}
}

func cleanupOldQueueRecords(root string, cutoff time.Duration) {
	for _, name := range []string{"pending", "dlq"} {
		cleanupOldRegularFiles(filepath.Join(root, name), cutoff)
	}
}

func cleanupOldRegularFiles(root string, cutoff time.Duration) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[Retention] queue scan failed (%T)", err)
		}
		return
	}
	expireBefore := time.Now().Add(-cutoff)
	failed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			failed++
			continue
		}
		if !info.Mode().IsRegular() || info.ModTime().After(expireBefore) {
			continue
		}
		if err := os.Remove(filepath.Join(root, entry.Name())); err != nil && !os.IsNotExist(err) {
			failed++
		}
	}
	if failed > 0 {
		log.Printf("[Retention] failed to remove %d expired queue records", failed)
	}
}

func cleanupArtifactsExcept(root string, cutoff time.Duration, preserved map[string]struct{}, allowed func(os.DirEntry) bool) {
	if root == "" {
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[Retention] artifact scan failed (%T)", err)
		}
		return
	}
	expireBefore := time.Now().Add(-cutoff)
	failed := 0
	for _, entry := range entries {
		if _, keep := preserved[entry.Name()]; keep {
			continue
		}
		if allowed == nil || !allowed(entry) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err != nil {
			failed++
			continue
		}
		if info.ModTime().After(expireBefore) {
			continue
		}
		if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
			failed++
		}
	}
	if failed > 0 {
		log.Printf("[Retention] failed to remove %d expired artifact entries", failed)
	}
}

func isTaskArtifactEntry(entry os.DirEntry) bool {
	return entry.IsDir() && isCanonicalTaskID(entry.Name())
}

func isOutputArtifactEntry(entry os.DirEntry) bool {
	if entry.IsDir() {
		return false
	}
	name := entry.Name()
	if strings.HasPrefix(name, ".archive-") && strings.HasSuffix(name, ".tmp") {
		return true
	}
	return strings.HasSuffix(name, ".zip") && isCanonicalTaskID(strings.TrimSuffix(name, ".zip"))
}

func isCanonicalTaskID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func directChildContaining(root, candidate string) string {
	if root == "" || candidate == "" || root == candidate {
		return ""
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || filepath.IsAbs(rel) || rel == "." || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	return parts[0]
}

func pathWithin(root, candidate string) (bool, error) {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false, err
	}
	return rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
