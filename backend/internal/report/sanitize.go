package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"

	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
	"github.com/keepbuild/seewxapkg/internal/domain/task"
)

const (
	unixInternalPathPattern = `(?:/private/tmp|/Users|/app|/data|/etc|/home|/mnt|/opt|/output|/root|/run|/srv|/tmp|/usr|/var|/workspace)(?:/[^\s"'<>;,)\]}]*)+`
	windowsPathPattern      = `(?:\b[A-Z]:[\\/]|\\\\[A-Z0-9._$-]+[\\/][A-Z0-9._$-]+)[^\s"'<>;,)\]}]*`
	publicNetworkURLPattern = `\b(?:https?|wss?|ftp)://[^\s"'<>]+`
	anyURLPattern           = `\b[A-Z][A-Z0-9+.-]*://[^\s"'<>]+`
)

var (
	publicURLOrInternalPath = regexp.MustCompile(`(?i)(?:` + anyURLPattern + `|` + windowsPathPattern + `|` + unixInternalPathPattern + `)`)
	publicNetworkURL        = regexp.MustCompile(`(?i)^` + publicNetworkURLPattern + `$`)
	// Stage metrics are persisted as an open-ended map so pipeline internals can
	// evolve without a storage migration. Public reports take the opposite
	// approach: only intentionally documented, user-facing measurements leave
	// the service. This prevents legacy keys such as zipPath from resurfacing
	// after their values have merely been shortened to a basename.
	publicStageMetricKeys = map[string]struct{}{
		"archiveRoot":                 {},
		"archiveSize":                 {},
		"artifactPassed":              {},
		"diagnostics":                 {},
		"failed":                      {},
		"fileCount":                   {},
		"formatted":                   {},
		"indexFileCount":              {},
		"invalidTabBarPages":          {},
		"isEncrypted":                 {},
		"manifestPassed":              {},
		"missingPages":                {},
		"mode":                        {},
		"native":                      {},
		"pageCount":                   {},
		"pages":                       {},
		"pageTriplets":                {},
		"parserPassed":                {},
		"recovered":                   {},
		"scripts":                     {},
		"skipped":                     {},
		"styles":                      {},
		"supportsRecovery":            {},
		"templates":                   {},
		"totalPages":                  {},
		"unchanged":                   {},
		"used":                        {},
		"variant":                     {},
		"wxmlDynamicEventBindings":    {},
		"wxmlPlaceholderCount":        {},
		"wxmlQualityIssueFiles":       {},
		"wxmlQualityPassed":           {},
		"wxmlSuspiciousEventBindings": {},
		"wxmlUnresolvedAttrMarkers":   {},
		"wxmlUnresolvedMarkerFiles":   {},
		"wxmlUnresolvedMarkers":       {},
		"wxmlUnresolvedTextMarkers":   {},
	}
	publicStageEngines = map[string]struct{}{
		"disabled":         {},
		"fallback":         {},
		"native":           {},
		"parser":           {},
		"safe-format":      {},
		"static-verifier":  {},
		"subpackage-guard": {},
	}
	publicArtifactSources = map[string]struct{}{
		"fallback":  {},
		"generated": {},
		"inferred":  {},
		"manifest":  {},
		"native":    {},
		"runtime":   {},
	}
)

// SanitizeDiagnostics returns a detached public copy. Internal paths remain
// useful while a task is running, but must not reveal host or container layout
// through reports and HTTP responses.
func SanitizeDiagnostics(items []pkg.Diagnostic) []pkg.Diagnostic {
	if len(items) == 0 {
		return nil
	}
	result := make([]pkg.Diagnostic, 0, len(items))
	for _, item := range items {
		copyItem := item
		copyItem.File = sanitizePath(item.File)
		copyItem.Message = sanitizeText(item.Message)
		copyItem.Metadata = sanitizeStringMap(item.Metadata)
		result = append(result, copyItem)
	}
	return result
}

// SanitizeStageResult returns a detached public copy of one pipeline stage.
func SanitizeStageResult(stage task.StageResult) task.StageResult {
	result := stage
	result.Engine = sanitizeStageEngine(stage.Engine)
	result.Message = sanitizeText(stage.Message)
	result.Diagnostics = SanitizeDiagnostics(stage.Diagnostics)
	result.Metrics = sanitizeStageMetrics(stage.Metrics)
	result.SourceBreakdown = sanitizeSourceBreakdown(stage.SourceBreakdown)
	return result
}

func sanitizeStageResults(stages []task.StageResult) []task.StageResult {
	if len(stages) == 0 {
		return nil
	}
	result := make([]task.StageResult, 0, len(stages))
	for _, stage := range stages {
		result = append(result, SanitizeStageResult(stage))
	}
	return result
}

func sanitizeArtifactSummary(summary *task.ArtifactSummary) *task.ArtifactSummary {
	if summary == nil {
		return nil
	}
	result := *summary
	result.ZipPath = ""
	result.ReportPath = ""
	result.DiagnosticsPath = ""
	result.ArtifactsPath = ""
	if len(summary.Files) > 0 {
		result.Files = append([]task.ArtifactFile(nil), summary.Files...)
		for index := range result.Files {
			result.Files[index].Path = sanitizePath(result.Files[index].Path)
			result.Files[index].Source = sanitizeArtifactSource(result.Files[index].Source)
		}
	}
	result.SourceBreakdown = sanitizeSourceBreakdown(summary.SourceBreakdown)
	return &result
}

// SanitizeArtifactSummary returns a detached public copy of artifact metadata.
func SanitizeArtifactSummary(summary *task.ArtifactSummary) *task.ArtifactSummary {
	return sanitizeArtifactSummary(summary)
}

// SanitizeJSONBytes removes internal paths from an existing JSON report. This
// also protects reports written by older releases that predate write-time
// sanitization.
func SanitizeJSONBytes(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("unexpected trailing JSON value")
	}
	return json.Marshal(sanitizeValue(value))
}

func sanitizeStringMap(input map[string]interface{}) map[string]interface{} {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]interface{}, len(input))
	for key, value := range input {
		if key == "metrics" {
			if metrics, ok := value.(map[string]interface{}); ok {
				if safeMetrics := sanitizeStageMetrics(metrics); len(safeMetrics) > 0 {
					result[key] = safeMetrics
				}
				continue
			}
		}
		if key == "files" {
			if files, ok := sanitizeRelativeFileList(value); ok {
				result[key] = files
				continue
			}
		}
		result[key] = sanitizeValue(value)
	}
	return result
}

func sanitizeStageMetrics(input map[string]interface{}) map[string]interface{} {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]interface{}, len(input))
	for key, value := range input {
		if _, allowed := publicStageMetricKeys[key]; !allowed {
			continue
		}
		if safeValue, valid := sanitizeStageMetricValue(key, value); valid {
			result[key] = safeValue
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func sanitizeStageMetricValue(key string, value interface{}) (interface{}, bool) {
	switch key {
	case "archiveRoot":
		text, ok := value.(string)
		return text, ok && text == "src/"
	case "mode":
		text, ok := value.(string)
		if !ok {
			return nil, false
		}
		switch text {
		case "encrypted", "plain", "unknown":
			return text, true
		default:
			return nil, false
		}
	case "variant":
		text, ok := value.(string)
		if !ok {
			return nil, false
		}
		switch text {
		case "encrypted", "game", "standard", "subpackage", "unknown", "wechat4x":
			return text, true
		default:
			return nil, false
		}
	case "artifactPassed", "isEncrypted", "manifestPassed", "parserPassed", "supportsRecovery", "used", "wxmlQualityPassed":
		flag, ok := value.(bool)
		return flag, ok
	default:
		return sanitizeNonNegativeInteger(value)
	}
}

func sanitizeNonNegativeInteger(value interface{}) (interface{}, bool) {
	switch number := value.(type) {
	case json.Number:
		parsed, err := number.Int64()
		return parsed, err == nil && parsed >= 0
	case int:
		return number, number >= 0
	case int8:
		return number, number >= 0
	case int16:
		return number, number >= 0
	case int32:
		return number, number >= 0
	case int64:
		return number, number >= 0
	case uint:
		return number, uint64(number) <= uint64(1<<63-1)
	case uint8:
		return number, true
	case uint16:
		return number, true
	case uint32:
		return number, true
	case uint64:
		return number, number <= uint64(1<<63-1)
	case float32:
		converted := float64(number)
		return number, converted >= 0 && converted <= float64(1<<24) && converted == float64(int64(converted))
	case float64:
		return number, number >= 0 && number <= float64(1<<53) && number == float64(int64(number))
	default:
		return nil, false
	}
}

func sanitizeStageEngine(value string) string {
	if _, allowed := publicStageEngines[value]; allowed {
		return value
	}
	return ""
}

func sanitizeArtifactSource(value string) string {
	if _, allowed := publicArtifactSources[value]; allowed {
		return value
	}
	return "other"
}

func sanitizeSourceBreakdown(input map[string]int) map[string]int {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]int, len(input))
	for key, value := range input {
		if _, allowed := publicArtifactSources[key]; allowed {
			result[key] += value
			continue
		}
		result["other"] += value
	}
	return result
}

// sanitizeRelativeFileList preserves valid archive-relative paths verbatim.
// Running these values through the free-text sanitizer can mistake a package
// directory such as "home" for a host path marker and corrupt the manifest.
func sanitizeRelativeFileList(value interface{}) ([]interface{}, bool) {
	items, ok := value.([]interface{})
	if !ok {
		return nil, false
	}
	result := make([]interface{}, len(items))
	for index, item := range items {
		text, ok := item.(string)
		if !ok {
			result[index] = sanitizeValue(item)
			continue
		}
		if !isSafeRelativeFilePath(text) {
			result[index] = sanitizePath(text)
			continue
		}
		result[index] = text
	}
	return result, true
}

func isSafeRelativeFilePath(value string) bool {
	if value == "" || strings.ContainsAny(value, "\\\x00:") || strings.HasPrefix(value, "/") || filepath.VolumeName(value) != "" {
		return false
	}
	normalized := pathpkg.Clean(value)
	return normalized == value && normalized != "." && normalized != ".." && !strings.HasPrefix(normalized, "../")
}

func sanitizeValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case string:
		return sanitizeText(typed)
	case map[string]interface{}:
		return sanitizeStringMap(typed)
	case map[string]string:
		result := make(map[string]string, len(typed))
		for key, item := range typed {
			result[key] = sanitizeText(item)
		}
		return result
	case map[string]int:
		result := make(map[string]int, len(typed))
		for key, item := range typed {
			result[key] = item
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(typed))
		for index, item := range typed {
			result[index] = sanitizeValue(item)
		}
		return result
	case []string:
		result := make([]string, len(typed))
		for index, item := range typed {
			result[index] = sanitizeText(item)
		}
		return result
	default:
		return value
	}
}

// SanitizeText returns a public-safe copy of text that may contain internal
// filesystem paths. It intentionally leaves the caller's original value
// untouched so internal persistence and logs can retain the full error cause.
func SanitizeText(value string) string {
	return sanitizeText(value)
}

func sanitizeText(value string) string {
	if value == "" {
		return ""
	}
	if filepath.IsAbs(filepath.FromSlash(value)) {
		return sanitizePath(value)
	}
	return publicURLOrInternalPath.ReplaceAllStringFunc(value, func(reference string) string {
		if isPublicNetworkURL(reference) {
			return reference
		}
		if strings.Contains(reference, "://") {
			return sanitizeUnsupportedURL(reference)
		}
		if len(reference) >= 3 && ((reference[1] == ':' && (reference[2] == '\\' || reference[2] == '/')) || strings.HasPrefix(reference, `\\`)) {
			return sanitizeWindowsPath(reference)
		}
		return sanitizePath(reference)
	})
}

func isPublicNetworkURL(value string) bool {
	return publicNetworkURL.MatchString(value)
}

func sanitizeUnsupportedURL(value string) string {
	separator := strings.Index(value, "://")
	if separator < 0 {
		return sanitizePath(value)
	}
	remainder := strings.ReplaceAll(value[separator+3:], `\`, "/")
	if remainder == "" {
		return "[non-public-url]"
	}
	if strings.HasPrefix(remainder, "/") || (len(remainder) >= 2 && remainder[1] == ':') {
		return sanitizePath(remainder)
	}
	if base := pathpkg.Base(strings.TrimRight(remainder, "/")); base != "." && base != "/" && base != "" {
		return base
	}
	return "[non-public-url]"
}

func sanitizeWindowsPath(value string) string {
	normalized := strings.ReplaceAll(value, `\`, "/")
	if len(normalized) >= 2 && normalized[1] == ':' {
		normalized = normalized[2:]
	}
	normalized = "/" + strings.TrimLeft(normalized, "/")
	return sanitizePath(normalized)
}

func sanitizePath(value string) string {
	if value == "" {
		return ""
	}
	if isPublicNetworkURL(value) {
		return value
	}
	if strings.Contains(value, "://") {
		return sanitizeUnsupportedURL(value)
	}
	if (len(value) >= 2 && value[1] == ':') || strings.HasPrefix(value, `\\`) {
		return sanitizeWindowsPath(value)
	}
	normalized := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	if !filepath.IsAbs(filepath.FromSlash(value)) {
		if normalized == ".." || strings.HasPrefix(normalized, "../") {
			return filepath.Base(normalized)
		}
		return normalized
	}

	for _, marker := range []struct {
		needle string
		prefix string
	}{
		{needle: "/result/", prefix: ""},
		{needle: "/fallback/input/", prefix: "fallback/"},
		{needle: "/fallback/", prefix: "fallback/"},
		{needle: "/reports/", prefix: "reports/"},
	} {
		if index := strings.LastIndex(normalized, marker.needle); index >= 0 {
			return marker.prefix + strings.TrimPrefix(normalized[index+len(marker.needle):], "/")
		}
	}
	for _, suffix := range []struct {
		value string
		label string
	}{
		{value: "/result/src", label: "src"},
		{value: "/result", label: "result"},
		{value: "/fallback/input", label: "fallback"},
		{value: "/fallback", label: "fallback"},
		{value: "/reports", label: "reports"},
	} {
		if strings.HasSuffix(normalized, suffix.value) {
			return suffix.label
		}
	}
	if base := filepath.Base(normalized); base != "." && base != string(filepath.Separator) && base != "" {
		return base
	}
	return "[internal-path]"
}
