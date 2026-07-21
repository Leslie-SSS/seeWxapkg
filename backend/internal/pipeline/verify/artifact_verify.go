package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
	"github.com/keepbuild/seewxapkg/internal/infra/process"
)

type ArtifactVerifyResult struct {
	Success                        bool             `json:"success"`
	CriticalFailure                bool             `json:"criticalFailure"`
	ParserPassed                   bool             `json:"parserPassed"`
	WXMLQualityPassed              bool             `json:"wxmlQualityPassed"`
	VerifierPassed                 bool             `json:"verifierPassed"`
	TotalPages                     int              `json:"totalPages"`
	PageTriplets                   int              `json:"pageTriplets"`
	JSFiles                        int              `json:"jsFiles"`
	WXMLFiles                      int              `json:"wxmlFiles"`
	WXSSFiles                      int              `json:"wxssFiles"`
	JSParseable                    int              `json:"jsParseable"`
	WXMLParseable                  int              `json:"wxmlParseable"`
	WXSSParseable                  int              `json:"wxssParseable"`
	WXMLMissingRefs                int              `json:"wxmlMissingRefs"`
	WXMLPlaceholderFiles           int              `json:"wxmlPlaceholderFiles"`
	WXMLPlaceholderCount           int              `json:"wxmlPlaceholderCount"`
	WXMLUnresolvedMarkerFiles      int              `json:"wxmlUnresolvedMarkerFiles"`
	WXMLUnresolvedMarkers          int              `json:"wxmlUnresolvedMarkers"`
	WXMLUnresolvedTextMarkers      int              `json:"wxmlUnresolvedTextMarkers"`
	WXMLUnresolvedAttributeMarkers int              `json:"wxmlUnresolvedAttributeMarkers"`
	WXMLSuspiciousEventFiles       int              `json:"wxmlSuspiciousEventFiles"`
	WXMLSuspiciousEventBindings    int              `json:"wxmlSuspiciousEventBindings"`
	WXMLDynamicEventFiles          int              `json:"wxmlDynamicEventFiles"`
	WXMLDynamicEventBindings       int              `json:"wxmlDynamicEventBindings"`
	WXMLQualityIssueFiles          int              `json:"wxmlQualityIssueFiles"`
	MissingPageTriplet             []string         `json:"missingPageTriplet,omitempty"`
	Diagnostics                    []pkg.Diagnostic `json:"diagnostics,omitempty"`
}

type artifactParserResult struct {
	JSFiles       int `json:"jsFiles"`
	JSParseable   int `json:"jsParseable"`
	WXMLFiles     int `json:"wxmlFiles"`
	WXMLParseable int `json:"wxmlParseable"`
	WXSSFiles     int `json:"wxssFiles"`
	WXSSParseable int `json:"wxssParseable"`
	JSErrors      []struct {
		File  string `json:"file"`
		Error string `json:"error"`
	} `json:"jsErrors"`
	WXMLErrors []struct {
		File  string `json:"file"`
		Error string `json:"error"`
	} `json:"wxmlErrors"`
	WXMLMissingRefs []struct {
		File   string `json:"file"`
		Tag    string `json:"tag"`
		Target string `json:"target"`
	} `json:"wxmlMissingRefs"`
	WXSSErrors []struct {
		File  string `json:"file"`
		Error string `json:"error"`
	} `json:"wxssErrors"`
}

func VerifyArtifacts(runner *process.NodeRunner, np *pkg.NormalizedPackage, sourceDir string) (*ArtifactVerifyResult, error) {
	result := &ArtifactVerifyResult{
		TotalPages:        len(np.Manifest.Pages),
		WXMLQualityPassed: true,
	}

	for _, page := range np.Manifest.Pages {
		base, safe := safePageBase(sourceDir, page)
		if !safe {
			result.MissingPageTriplet = append(result.MissingPageTriplet, page)
			continue
		}
		hasJS := fileExists(base + ".js")
		hasWXML := fileExists(base + ".wxml")
		if hasJS && hasWXML {
			result.PageTriplets++
			continue
		}
		result.MissingPageTriplet = append(result.MissingPageTriplet, page)
	}

	parserResult, err := runArtifactParser(runner, sourceDir)
	if err != nil {
		return nil, err
	}
	result.JSFiles = parserResult.JSFiles
	result.JSParseable = parserResult.JSParseable
	result.WXMLFiles = parserResult.WXMLFiles
	result.WXMLParseable = parserResult.WXMLParseable
	result.WXSSFiles = parserResult.WXSSFiles
	result.WXSSParseable = parserResult.WXSSParseable
	result.ParserPassed = len(parserResult.JSErrors) == 0 && len(parserResult.WXMLErrors) == 0 && len(parserResult.WXSSErrors) == 0
	result.WXMLMissingRefs = len(parserResult.WXMLMissingRefs)
	qualityIssueFiles := make(map[string]struct{})

	for _, item := range parserResult.JSErrors {
		result.Diagnostics = append(result.Diagnostics, pkg.Warn("verify.js.unparsable", "JS parser 校验失败: "+item.Error, "verifying", item.File))
	}
	for _, item := range parserResult.WXMLErrors {
		result.Diagnostics = append(result.Diagnostics, pkg.Warn("verify.wxml.unparsable", "WXML parser 校验失败: "+item.Error, "verifying", item.File))
	}
	for _, item := range parserResult.WXMLMissingRefs {
		qualityIssueFiles[item.File] = struct{}{}
		result.Diagnostics = append(result.Diagnostics, pkg.Warn("verify.wxml.missing_ref", "WXML 引用缺失: "+item.Tag+" -> "+item.Target, "verifying", item.File))
	}
	for _, item := range parserResult.WXSSErrors {
		result.Diagnostics = append(result.Diagnostics, pkg.Warn("verify.wxss.unparsable", "WXSS parser 校验失败: "+item.Error, "verifying", item.File))
	}

	if len(result.MissingPageTriplet) > 0 {
		result.Diagnostics = append(result.Diagnostics, pkg.Warn("verify.artifacts.partial", "部分页面缺少必需的 js/wxml 文件（wxss 为可选）", "verifying", "src"))
	}

	wxmlQuality, err := inspectWXMLQuality(sourceDir)
	if err != nil {
		return nil, err
	}
	result.WXMLPlaceholderFiles = wxmlQuality.PlaceholderFiles
	result.WXMLPlaceholderCount = wxmlQuality.PlaceholderCount
	result.WXMLUnresolvedMarkerFiles = wxmlQuality.UnresolvedMarkerFiles
	result.WXMLUnresolvedMarkers = wxmlQuality.UnresolvedMarkers
	result.WXMLUnresolvedTextMarkers = wxmlQuality.UnresolvedTextMarkers
	result.WXMLUnresolvedAttributeMarkers = wxmlQuality.UnresolvedAttributeMarkers
	result.WXMLSuspiciousEventFiles = wxmlQuality.SuspiciousEventFiles
	result.WXMLSuspiciousEventBindings = wxmlQuality.SuspiciousEventBindings
	result.WXMLDynamicEventFiles = wxmlQuality.DynamicEventFiles
	result.WXMLDynamicEventBindings = wxmlQuality.DynamicEventBindings
	result.Diagnostics = append(result.Diagnostics, wxmlQuality.Diagnostics...)
	for file := range wxmlQuality.IssueFiles {
		qualityIssueFiles[file] = struct{}{}
	}
	result.WXMLQualityIssueFiles = len(qualityIssueFiles)
	result.WXMLQualityPassed = result.WXMLQualityIssueFiles == 0

	if result.TotalPages > 0 {
		// Treat parser-invalid generated artifacts as critical only when a file class exists
		// but none of those files can be parsed. Purely missing classes are handled as partial.
		if result.JSFiles > 0 && result.JSParseable == 0 {
			result.CriticalFailure = true
		}
		if result.WXMLFiles > 0 && result.WXMLParseable == 0 {
			result.CriticalFailure = true
		}
		if result.WXSSFiles > 0 && result.WXSSParseable == 0 {
			result.CriticalFailure = true
		}
	}

	pageStructurePassed := result.TotalPages > 0 && len(result.MissingPageTriplet) == 0
	result.VerifierPassed = pageStructurePassed && result.ParserPassed && result.WXMLQualityPassed
	result.Success = result.VerifierPassed
	return result, nil
}

type wxmlQualityResult struct {
	PlaceholderFiles           int
	PlaceholderCount           int
	UnresolvedMarkerFiles      int
	UnresolvedMarkers          int
	UnresolvedTextMarkers      int
	UnresolvedAttributeMarkers int
	SuspiciousEventFiles       int
	SuspiciousEventBindings    int
	DynamicEventFiles          int
	DynamicEventBindings       int
	IssueFiles                 map[string]struct{}
	Diagnostics                []pkg.Diagnostic
}

var (
	wxmlComment               = regexp.MustCompile(`(?s)<!--.*?-->`)
	emptyWXMLTextSentinel     = regexp.MustCompile(`>\s*Empty\s*<`)
	emptyWXMLDoubleAttribute  = regexp.MustCompile(`(?i)\b[a-z][a-z0-9:_-]*\s*=\s*"Empty"`)
	emptyWXMLSingleAttribute  = regexp.MustCompile(`(?i)\b[a-z][a-z0-9:_-]*\s*=\s*'Empty'`)
	wxmlDoubleQuotedAttribute = regexp.MustCompile(`(?i)\b([a-z][a-z0-9:_-]*)\s*=\s*"([^"]*)"`)
	wxmlSingleQuotedAttribute = regexp.MustCompile(`(?i)\b([a-z][a-z0-9:_-]*)\s*=\s*'([^']*)'`)
	plainEventHandler         = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*(?:\.[A-Za-z_$][A-Za-z0-9_$]*)?$`)
)

var (
	unresolvedTextMarker = []byte("<!-- seewx-recovery: unresolved text omitted -->")
	unresolvedAttrMarker = []byte("<!-- seewx-recovery: unresolved attributes omitted -->")
)

func inspectWXMLQuality(sourceDir string) (*wxmlQualityResult, error) {
	result := &wxmlQualityResult{IssueFiles: make(map[string]struct{})}
	err := filepath.Walk(sourceDir, func(filePath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.EqualFold(filepath.Ext(filePath), ".wxml") {
			return nil
		}
		content, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(sourceDir, filePath)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)

		placeholderCount := countLegacyEmptySentinels(content)
		if placeholderCount > 0 {
			result.PlaceholderFiles++
			result.PlaceholderCount += placeholderCount
			result.IssueFiles[relative] = struct{}{}
			diagnostic := pkg.Warn("verify.wxml.placeholder_sentinel", "WXML 包含恢复引擎遗留的 Empty 占位标记；文件可解析不代表内容已完整还原", "verifying", relative)
			diagnostic.Metadata = map[string]interface{}{"count": placeholderCount}
			result.Diagnostics = append(result.Diagnostics, diagnostic)
		}

		unresolvedTextCount := bytes.Count(content, unresolvedTextMarker)
		unresolvedAttrCount := bytes.Count(content, unresolvedAttrMarker)
		unresolvedCount := unresolvedTextCount + unresolvedAttrCount
		if unresolvedCount > 0 {
			result.UnresolvedMarkerFiles++
			result.UnresolvedMarkers += unresolvedCount
			result.UnresolvedTextMarkers += unresolvedTextCount
			result.UnresolvedAttributeMarkers += unresolvedAttrCount
			result.IssueFiles[relative] = struct{}{}
			diagnostic := pkg.Warn(
				"verify.wxml.unresolved_recovery_marker",
				fmt.Sprintf("这个 WXML 文件仍有 %d 处内容未能自动还原（文本 %d 处、属性 %d 处）；文件虽然可以打开，但页面内容或交互可能不完整", unresolvedCount, unresolvedTextCount, unresolvedAttrCount),
				"verifying",
				relative,
			)
			diagnostic.Metadata = map[string]interface{}{
				"count":          unresolvedCount,
				"textCount":      unresolvedTextCount,
				"attributeCount": unresolvedAttrCount,
			}
			result.Diagnostics = append(result.Diagnostics, diagnostic)
		}

		eventQuality := inspectEventBindings(string(content))
		if eventQuality.Suspicious > 0 {
			result.SuspiciousEventFiles++
			result.SuspiciousEventBindings += eventQuality.Suspicious
			result.IssueFiles[relative] = struct{}{}
			diagnostic := pkg.Warn("verify.wxml.suspicious_event_binding", "WXML 中存在不像可调用处理函数的事件绑定值，相关交互可能未正确还原", "verifying", relative)
			diagnostic.Metadata = map[string]interface{}{"count": eventQuality.Suspicious}
			result.Diagnostics = append(result.Diagnostics, diagnostic)
		}
		if eventQuality.Dynamic > 0 {
			result.DynamicEventFiles++
			result.DynamicEventBindings += eventQuality.Dynamic
			diagnostic := pkg.Info("verify.wxml.dynamic_event_binding", "WXML 使用合法的 moustache 数据绑定事件处理函数；该统计不视为恢复质量缺口", "verifying", relative)
			diagnostic.Metadata = map[string]interface{}{"count": eventQuality.Dynamic}
			result.Diagnostics = append(result.Diagnostics, diagnostic)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// countLegacyEmptySentinels detects clusters produced by the historical
// recovery bug without treating a legitimate one-off "Empty" label or a
// developer comment as corruption.
func countLegacyEmptySentinels(content []byte) int {
	content = wxmlComment.ReplaceAll(content, nil)
	count := len(emptyWXMLTextSentinel.FindAll(content, -1))
	count += len(emptyWXMLDoubleAttribute.FindAll(content, -1))
	count += len(emptyWXMLSingleAttribute.FindAll(content, -1))
	if count < 3 {
		return 0
	}
	return count
}

type eventBindingQuality struct {
	Suspicious int
	Dynamic    int
}

func inspectEventBindings(content string) eventBindingQuality {
	result := eventBindingQuality{}
	for _, pattern := range []*regexp.Regexp{wxmlDoubleQuotedAttribute, wxmlSingleQuotedAttribute} {
		for _, match := range pattern.FindAllStringSubmatch(content, -1) {
			if len(match) != 3 || !isEventAttribute(match[1]) {
				continue
			}
			if isMoustacheEventHandler(match[2]) {
				result.Dynamic++
				continue
			}
			if isSuspiciousEventHandler(match[2]) {
				result.Suspicious++
			}
		}
	}
	return result
}

func isEventAttribute(name string) bool {
	name = strings.ToLower(name)
	for _, prefix := range []string{"capture-bind:", "capture-catch:", "mut-bind:", "bind:", "catch:"} {
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			return true
		}
	}

	for _, prefix := range []string{"bind", "catch"} {
		if strings.HasPrefix(name, prefix) && isKnownBareEvent(name[len(prefix):]) {
			return true
		}
	}
	return false
}

func isKnownBareEvent(name string) bool {
	_, ok := knownBareEvents[name]
	return ok
}

var knownBareEvents = map[string]struct{}{
	"animationend": {}, "animationiteration": {}, "animationstart": {},
	"blur": {}, "change": {}, "close": {}, "columnchange": {}, "complete": {}, "confirm": {},
	"contact": {}, "controltap": {}, "ended": {}, "error": {}, "focus": {}, "fullscreenchange": {},
	"getphonenumber": {}, "getuserinfo": {}, "input": {}, "keyboardheightchange": {}, "launchapp": {},
	"linechange": {}, "load": {}, "loadedmetadata": {}, "longpress": {}, "markertap": {}, "message": {},
	"open": {}, "opensetting": {}, "pause": {}, "pickend": {}, "pickstart": {}, "play": {}, "progress": {},
	"ready": {}, "regionchange": {}, "reset": {}, "scroll": {}, "scrolltolower": {}, "scrolltoupper": {},
	"seekcomplete": {}, "submit": {}, "tap": {}, "timeupdate": {}, "touchcancel": {}, "touchend": {},
	"touchforcechange": {}, "touchmove": {}, "touchstart": {}, "transitionend": {}, "updated": {}, "waiting": {},
}

func isSuspiciousEventHandler(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value == "Empty" {
		return true
	}
	if isMoustacheEventHandler(value) {
		return false
	}
	return !plainEventHandler.MatchString(value)
}

func isMoustacheEventHandler(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "{{") && strings.HasSuffix(value, "}}")
}

func runArtifactParser(runner *process.NodeRunner, sourceDir string) (*artifactParserResult, error) {
	script, err := process.ResolveExistingPath(
		filepath.Join("backend", "internal", "beautify", "runtime", "verify_artifacts.js"),
		filepath.Join("internal", "beautify", "runtime", "verify_artifacts.js"),
	)
	if err != nil {
		return nil, err
	}
	stdout, stderr, err := runner.Run(context.Background(), script, sourceDir)
	if err != nil {
		return nil, errWithStderr(err, stderr)
	}
	var parsed artifactParserResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

func errWithStderr(err error, stderr string) error {
	if strings.TrimSpace(stderr) == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
