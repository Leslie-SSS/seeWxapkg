package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FormatFileResult struct {
	Path       string `json:"path"`
	Status     string `json:"status"`
	Formatter  string `json:"formatter,omitempty"`
	Warning    string `json:"warning,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"durationMs"`
	InputHash  string `json:"inputHash"`
	OutputHash string `json:"outputHash"`
}

type FormatTreeResult struct {
	Success   bool               `json:"success"`
	Partial   bool               `json:"partial"`
	Formatted int                `json:"formatted"`
	Unchanged int                `json:"unchanged"`
	Skipped   int                `json:"skipped"`
	Failed    int                `json:"failed"`
	Files     []FormatFileResult `json:"files"`
}

func FormatSourceTree(root string) (*FormatTreeResult, error) {
	result := &FormatTreeResult{Success: true}
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".js", ".wxs", ".wxml", ".html", ".wxss", ".css", ".json":
		default:
			return nil
		}

		input, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		started := time.Now()
		fileResult := FormatFileResult{InputHash: contentHash(input)}
		fileResult.Path, _ = filepath.Rel(root, path)
		fileResult.Path = filepath.ToSlash(fileResult.Path)
		output := input

		if ext == ".json" {
			if !json.Valid(input) {
				fileResult.Status = "failed"
				fileResult.Error = "invalid JSON preserved unchanged"
			} else {
				output = beautifyJSON(input)
				fileResult.Formatter = "go-json-safe"
				if bytes.Equal(input, output) {
					fileResult.Status = "unchanged"
				} else {
					fileResult.Status = "formatted"
				}
			}
		} else if beautifyService == nil {
			fileResult.Status = "skipped"
			fileResult.Warning = "formatter unavailable"
		} else {
			formatted := beautifyService.BeautifyDetailed(input, fileResult.Path)
			output = formatted.Content
			fileResult.Status = formatted.Status
			fileResult.Formatter = formatted.Formatter
			fileResult.Warning = formatted.Warning
			if formatted.Error != nil {
				fileResult.Error = formatted.Error.Error()
			}
		}

		if fileResult.Status == "formatted" {
			if err := writeFileAtomically(path, output, info.Mode().Perm()); err != nil {
				return err
			}
			result.Formatted++
		} else {
			switch fileResult.Status {
			case "unchanged":
				result.Unchanged++
			case "skipped":
				result.Skipped++
			case "failed":
				result.Failed++
			default:
				unknownStatus := fileResult.Status
				result.Failed++
				fileResult.Status = "failed"
				fileResult.Error = fmt.Sprintf("unknown formatter status %q", unknownStatus)
			}
		}
		fileResult.OutputHash = contentHash(output)
		fileResult.DurationMs = time.Since(started).Milliseconds()
		result.Files = append(result.Files, fileResult)
		return nil
	})
	if err != nil {
		return nil, err
	}
	result.Partial = result.Failed > 0 || result.Skipped > 0
	result.Success = result.Failed == 0
	return result, nil
}

func writeFileAtomically(path string, content []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".format-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}

func contentHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}
