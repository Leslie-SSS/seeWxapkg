package recover

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
	"github.com/keepbuild/seewxapkg/internal/infra/process"
)

const fallbackStatusFilename = ".seewx-recovery-status"

type fallbackStatusReport struct {
	Status      string `json:"status"`
	Diagnostics []struct {
		Code     string                 `json:"code"`
		Level    string                 `json:"level"`
		Message  string                 `json:"message"`
		File     string                 `json:"file"`
		Status   string                 `json:"status"`
		Metadata map[string]interface{} `json:"metadata"`
	} `json:"diagnostics"`
}

func RunFallbackRecovery(ctx context.Context, runner *process.NodeRunner, scriptPath, inputWxapkgPath, workDir string) (*FallbackResult, error) {
	result := &FallbackResult{}

	if err := os.MkdirAll(workDir, 0700); err != nil {
		return nil, err
	}
	stdout, stderr, err := runner.RunInDir(ctx, workDir, scriptPath, inputWxapkgPath)
	result.Stdout = stdout
	result.Stderr = stderr
	outputDir := strings.TrimSuffix(inputWxapkgPath, ".wxapkg")
	result.OutputDir = outputDir
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, pkg.Warn("recover.fallback.failed", "wxappUnpacker 执行失败，系统将保留主轨结果", "recovering_js", outputDir))
		if stderr != "" {
			result.Diagnostics[len(result.Diagnostics)-1].Metadata = map[string]interface{}{"stderr": stderr}
		}
		return result, nil
	}

	if !fileExists(outputDir) && !dirExists(outputDir) {
		result.Diagnostics = append(result.Diagnostics, pkg.Warn("recover.fallback.empty", "fallback 执行成功但未产生可合并目录", "recovering_js", outputDir))
		return result, nil
	}

	result.Files = collectRecoveredFiles(outputDir, "fallback")
	status, statusDiagnostics := readFallbackStatus(outputDir)
	result.Status = status
	result.Diagnostics = append(result.Diagnostics, statusDiagnostics...)
	result.Partial = status != "completed"
	if !hasFallbackSourceArtifacts(result.Files) {
		result.Partial = true
		result.Status = "partial"
		result.Diagnostics = append(result.Diagnostics, pkg.Warn("recover.fallback.no_source_artifacts", "fallback 未产生可合并的源码产物", "fallback_recovering", outputDir))
		return result, nil
	}

	result.Success = true
	if result.Partial {
		result.Diagnostics = append(result.Diagnostics, pkg.Warn("recover.fallback.partial", "fallback 仅完成部分静态恢复，产物将按 partial 交付", "fallback_recovering", outputDir))
	} else {
		result.Diagnostics = append(result.Diagnostics, pkg.Info("recover.fallback.used", "fallback 静态恢复完整且已产生源码产物", "fallback_recovering", outputDir))
	}
	return result, nil
}

func readFallbackStatus(outputDir string) (string, []pkg.Diagnostic) {
	statusPath := filepath.Join(outputDir, fallbackStatusFilename)
	data, err := os.ReadFile(statusPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "partial", []pkg.Diagnostic{
				pkg.Warn("recover.fallback.status_missing", "fallback 未提供静态恢复状态，已按 partial 处理", "fallback_recovering", statusPath),
			}
		}
		return "partial", []pkg.Diagnostic{
			pkg.Warn("recover.fallback.status_unreadable", "无法读取 fallback 静态恢复状态，已按 partial 处理", "fallback_recovering", statusPath),
		}
	}

	var report fallbackStatusReport
	if err := json.Unmarshal(data, &report); err != nil {
		diagnostic := pkg.Warn("recover.fallback.status_invalid", "fallback 静态恢复状态格式无效，已按 partial 处理", "fallback_recovering", statusPath)
		diagnostic.Metadata = map[string]interface{}{"error": err.Error()}
		return "partial", []pkg.Diagnostic{diagnostic}
	}

	diagnostics := make([]pkg.Diagnostic, 0, len(report.Diagnostics)+1)
	for _, item := range report.Diagnostics {
		code := item.Code
		if code == "" {
			code = "recover.fallback.partial_detail"
		}
		message := item.Message
		if message == "" {
			message = "fallback 报告了未完成的静态恢复步骤"
		}
		diagnostic := pkg.Warn(code, message, "fallback_recovering", item.File)
		if len(item.Metadata) > 0 || item.Level != "" || item.Status != "" {
			diagnostic.Metadata = make(map[string]interface{}, len(item.Metadata)+2)
			for key, value := range item.Metadata {
				diagnostic.Metadata[key] = value
			}
			if item.Level != "" {
				diagnostic.Metadata["fallbackLevel"] = item.Level
			}
			if item.Status != "" {
				diagnostic.Metadata["fallbackStatus"] = item.Status
			}
		}
		diagnostics = append(diagnostics, diagnostic)
	}

	if report.Status == "completed" && len(report.Diagnostics) == 0 {
		return "completed", diagnostics
	}
	if report.Status != "partial" && report.Status != "completed" {
		diagnostics = append(diagnostics, pkg.Warn("recover.fallback.status_unknown", "fallback 返回未知恢复状态，已按 partial 处理", "fallback_recovering", statusPath))
	}
	return "partial", diagnostics
}

func hasFallbackSourceArtifacts(files []RecoveredFile) bool {
	for _, file := range files {
		if file.Path == "app.json" {
			continue
		}
		switch strings.ToLower(file.Kind) {
		case "js", "json", "wxml", "wxss", "wxs":
			return true
		}
	}
	return false
}

func MergeFallbackArtifacts(targetDir, fallbackDir string) error {
	_, err := MergeFallbackArtifactsWithPolicy(targetDir, fallbackDir)
	return err
}

// MergeFallbackArtifactsWithPolicy treats files already produced by the native
// track as stronger evidence. Fallback can add missing files, but never
// overwrites an existing file: an empty native WXSS is valid evidence too.
func MergeFallbackArtifactsWithPolicy(targetDir, fallbackDir string) (*FallbackMergeResult, error) {
	result := &FallbackMergeResult{}
	err := filepath.Walk(fallbackDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(fallbackDir, path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(rel) == fallbackStatusFilename || rel == "app.json" {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(rel))
		switch ext {
		case ".js", ".json", ".wxml", ".wxss", ".wxs":
		default:
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(targetDir, rel)
		existing, existingErr := os.ReadFile(target)
		switch {
		case existingErr == nil && bytes.Equal(existing, data):
			result.Identical++
			return nil
		case existingErr == nil:
			result.Preserved++
			result.Conflicts = append(result.Conflicts, FallbackMergeConflict{
				Path:       filepath.ToSlash(rel),
				Resolution: "preserved_native",
			})
			return nil
		case existingErr != nil && !os.IsNotExist(existingErr):
			return existingErr
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0600); err != nil {
			return err
		}
		result.Added++
		return nil
	})
	return result, err
}

func collectRecoveredFiles(rootDir, source string) []RecoveredFile {
	var files []RecoveredFile
	_ = filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil
		}
		if filepath.ToSlash(rel) == fallbackStatusFilename {
			return nil
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(rel)), ".")
		switch ext {
		case "js", "json", "wxml", "wxss", "wxs":
		default:
			return nil
		}
		files = append(files, RecoveredFile{
			Path:   filepath.ToSlash(rel),
			Kind:   ext,
			Source: source,
		})
		return nil
	})
	return files
}
