package normalize

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
)

type RawArtifactSet struct {
	RootDir       string
	AppJSONPath   string
	AppJSON       map[string]interface{}
	AppConfigPath string
	AppConfig     map[string]interface{}
	AppServiceJS  string
	AppServiceApp map[string]interface{}
	PageJSON      map[string]map[string]interface{}
	LazyChunkMap  map[string]string
	Files         []string
	FileContents  map[string]string
}

func NormalizeManifest(raw *RawArtifactSet) (*pkg.ManifestIR, []pkg.Diagnostic, error) {
	manifest := pkg.NewManifestIR()
	var diagnostics []pkg.Diagnostic

	diagnostics = append(diagnostics, mergeFromAppConfig(raw, &manifest)...)
	diagnostics = append(diagnostics, mergeFromAppService(raw, &manifest)...)
	diagnostics = append(diagnostics, mergeFromPageJSON(raw, &manifest)...)
	diagnostics = append(diagnostics, mergeFromDirectoryScan(raw, &manifest)...)
	diagnostics = append(diagnostics, finalizeManifest(&manifest)...)

	return &manifest, diagnostics, nil
}

func mergeFromAppConfig(raw *RawArtifactSet, m *pkg.ManifestIR) []pkg.Diagnostic {
	config := raw.AppConfig
	source := "app-config.json"
	if raw.AppJSON != nil {
		config = raw.AppJSON
		source = "app.json"
		m.Original = cloneMapDeep(raw.AppJSON)
	}
	if config == nil {
		return nil
	}

	if pages, ok := toStringSlice(config["pages"]); ok {
		m.Pages = append(m.Pages, pages...)
		m.Sources["pages"] = source
		m.Confidence["pages"] = "authoritative"
	}

	if subPackages, ok := config["subPackages"]; ok {
		m.SubPackages = toSubPackages(subPackages)
		m.Sources["subPackages"] = source
		m.Confidence["subPackages"] = "authoritative"
	} else if subPackages, ok := config["subpackages"]; ok {
		m.SubPackages = toSubPackages(subPackages)
		m.Sources["subPackages"] = source
		m.Confidence["subPackages"] = "authoritative"
	}

	copyMapField(config, "window", &m.Window, m.Sources, source)
	if _, exists := m.Sources["window"]; !exists {
		if global, ok := config["global"].(map[string]interface{}); ok {
			copyMapField(global, "window", &m.Window, m.Sources, source)
		}
	}
	copyMapField(config, "tabBar", &m.TabBar, m.Sources, source)
	copyMapField(config, "networkTimeout", &m.NetworkTimeout, m.Sources, source)
	copyStringMapField(config, "usingComponents", &m.UsingComponents, m.Sources, source)
	copyInterfaceMapField(config, "plugins", &m.Plugins, m.Sources, source)
	copyInterfaceMapField(config, "permission", &m.Permission, m.Sources, source)
	copyStringField(config, "workers", &m.Workers, m.Sources, source)
	copyStringField(config, "renderer", &m.Renderer, m.Sources, source)
	copyStringField(config, "style", &m.Style, m.Sources, source)
	copyStringField(config, "sitemapLocation", &m.SitemapLocation, m.Sources, source)
	copyBoolField(config, "debug", &m.Debug, m.Sources, source)
	if list, ok := toStringSlice(config["requiredBackgroundModes"]); ok {
		m.RequiredBackgroundModes = list
		m.Sources["requiredBackgroundModes"] = source
	}
	if list, ok := toStringSlice(config["navigateToMiniProgramAppIdList"]); ok {
		m.NavigateToMiniProgramAppIDList = list
		m.Sources["navigateToMiniProgramAppIdList"] = source
	}
	copyInterfaceMapField(config, "preloadRule", &m.PreloadRule, m.Sources, source)

	return nil
}

func mergeFromAppService(raw *RawArtifactSet, m *pkg.ManifestIR) []pkg.Diagnostic {
	if raw.AppServiceJS == "" {
		return nil
	}

	var diagnostics []pkg.Diagnostic
	if raw.AppServiceApp != nil {
		if _, exists := m.Sources["pages"]; !exists {
			if pages, ok := toStringSlice(raw.AppServiceApp["pages"]); ok {
				m.Pages = append(m.Pages, pages...)
				m.Sources["pages"] = "app-service.js"
				m.Confidence["pages"] = "runtime"
			}
		}
		copyMapField(raw.AppServiceApp, "window", &m.Window, m.Sources, "app-service.js")
		copyMapField(raw.AppServiceApp, "tabBar", &m.TabBar, m.Sources, "app-service.js")
		copyStringMapField(raw.AppServiceApp, "usingComponents", &m.UsingComponents, m.Sources, "app-service.js")
		copyInterfaceMapField(raw.AppServiceApp, "permission", &m.Permission, m.Sources, "app-service.js")
		copyInterfaceMapField(raw.AppServiceApp, "plugins", &m.Plugins, m.Sources, "app-service.js")
		copyStringField(raw.AppServiceApp, "workers", &m.Workers, m.Sources, "app-service.js")
	}
	if len(raw.PageJSON) > 0 {
		diagnostics = append(diagnostics, pkg.Info("manifest.app_service.parsed", "已从 app-service.js 提取页面级 json 与 lazy chunk 信息", "normalizing", "app-service.js"))
	} else {
		diagnostics = append(diagnostics, pkg.Info("manifest.app_service.unparsed", "app-service.js 已收集，但未提取到可用 manifest 字段", "normalizing", "app-service.js"))
	}
	return diagnostics
}

func mergeFromPageJSON(raw *RawArtifactSet, m *pkg.ManifestIR) []pkg.Diagnostic {
	if len(raw.PageJSON) == 0 {
		return nil
	}

	var diagnostics []pkg.Diagnostic
	pageJSONPaths := make([]string, 0, len(raw.PageJSON))
	for rel := range raw.PageJSON {
		pageJSONPaths = append(pageJSONPaths, rel)
	}
	slices.Sort(pageJSONPaths)
	for _, rel := range pageJSONPaths {
		content := raw.PageJSON[rel]
		pagePath := strings.TrimSuffix(filepath.ToSlash(rel), ".json")
		if shouldIgnoreManifestJSON(pagePath, content) {
			continue
		}

		if !slices.Contains(m.Pages, pagePath) && shouldAppendManifestPage(m, pagePath, content) {
			m.Pages = append(m.Pages, pagePath)
			if _, exists := m.Sources["pages"]; !exists {
				m.Sources["pages"] = "page-json-inferred"
				m.Confidence["pages"] = "inferred"
			}
		}

		if len(content) == 0 {
			diagnostics = append(diagnostics, pkg.Warn("manifest.page_json.empty", "页面 JSON 为空，已跳过字段提取", "normalizing", rel))
		}
	}
	return diagnostics
}

func mergeFromDirectoryScan(raw *RawArtifactSet, m *pkg.ManifestIR) []pkg.Diagnostic {
	if len(m.Pages) > 0 || m.Sources["pages"] != "" {
		return nil
	}

	seen := make(map[string]struct{})
	for _, file := range raw.Files {
		ext := strings.ToLower(filepath.Ext(file))
		if ext != ".wxml" && ext != ".js" && ext != ".json" {
			continue
		}

		pagePath := strings.TrimSuffix(filepath.ToSlash(file), ext)
		if pagePath == "app" || strings.Contains(pagePath, "common/") || looksLikeComponentPath(pagePath) || isIgnoredManifestPath(pagePath) {
			continue
		}
		if _, ok := seen[pagePath]; ok {
			continue
		}
		seen[pagePath] = struct{}{}
		m.Pages = append(m.Pages, pagePath)
	}

	if len(m.Pages) == 0 {
		return []pkg.Diagnostic{
			pkg.Warn("manifest.pages.missing", "未能从目录中推断页面列表", "normalizing", raw.RootDir),
		}
	}

	m.Sources["pages"] = "directory-scan"
	m.Confidence["pages"] = "fallback"
	return []pkg.Diagnostic{
		pkg.Warn("manifest.pages.inferred", "页面列表来自目录扫描，准确性可能受包形态影响", "normalizing", raw.RootDir),
	}
}

func finalizeManifest(m *pkg.ManifestIR) []pkg.Diagnostic {
	for index := range m.Pages {
		m.Pages[index] = normalizeManifestRoute(m.Pages[index])
	}
	m.Pages = stableUniqueStrings(m.Pages)
	if len(m.Pages) == 0 {
		return []pkg.Diagnostic{pkg.Error("manifest.pages.empty", "manifest 未恢复出 pages 字段", "normalizing", "app.json")}
	}

	var diagnostics []pkg.Diagnostic
	if count := normalizeTabBarRoutes(m.TabBar); count > 0 {
		diagnostic := pkg.Info("manifest.tabbar.routes_normalized", "已将导航栏页面地址转换为小程序要求的不带文件扩展名格式", "normalizing", "app.json")
		diagnostic.Metadata = map[string]interface{}{"count": count}
		diagnostics = append(diagnostics, diagnostic)
	}

	if _, ok := m.Confidence["pages"]; !ok {
		switch m.Sources["pages"] {
		case "app.json", "app-config.json":
			m.Confidence["pages"] = "authoritative"
		case "app-service.js":
			m.Confidence["pages"] = "runtime"
		case "page-json-inferred":
			m.Confidence["pages"] = "inferred"
		default:
			m.Confidence["pages"] = "fallback"
		}
	}
	return diagnostics
}

func normalizeTabBarRoutes(tabBar map[string]interface{}) int {
	list, ok := tabBar["list"].([]interface{})
	if !ok {
		return 0
	}
	changed := 0
	for _, rawItem := range list {
		item, ok := rawItem.(map[string]interface{})
		if !ok {
			continue
		}
		pagePath, ok := item["pagePath"].(string)
		if !ok {
			continue
		}
		normalized := normalizeManifestRoute(pagePath)
		if normalized == pagePath {
			continue
		}
		item["pagePath"] = normalized
		changed++
	}
	return changed
}

func normalizeManifestRoute(route string) string {
	route = strings.TrimSpace(strings.ReplaceAll(route, `\`, "/"))
	route = strings.TrimLeft(route, "/")
	for _, extension := range []string{".html", ".wxml", ".js"} {
		if strings.HasSuffix(strings.ToLower(route), extension) {
			return route[:len(route)-len(extension)]
		}
	}
	return route
}

func copyMapField(src map[string]interface{}, key string, dst *map[string]interface{}, sources map[string]string, source string) {
	if _, exists := sources[key]; exists {
		return
	}
	value, ok := src[key].(map[string]interface{})
	if !ok {
		return
	}
	*dst = cloneMapDeep(value)
	sources[key] = source
}

func copyInterfaceMapField(src map[string]interface{}, key string, dst *map[string]interface{}, sources map[string]string, source string) {
	if _, exists := sources[key]; exists {
		return
	}
	value, ok := src[key].(map[string]interface{})
	if !ok {
		return
	}
	*dst = cloneMapDeep(value)
	sources[key] = source
}

func copyStringMapField(src map[string]interface{}, key string, dst *map[string]string, sources map[string]string, source string) {
	if _, exists := sources[key]; exists {
		return
	}
	raw, ok := src[key].(map[string]interface{})
	if !ok {
		return
	}
	value := make(map[string]string, len(raw))
	for name, item := range raw {
		if text, ok := item.(string); ok {
			value[name] = text
		}
	}
	*dst = value
	sources[key] = source
}

func copyStringField(src map[string]interface{}, key string, dst **string, sources map[string]string, source string) {
	if _, exists := sources[key]; exists {
		return
	}
	value, ok := src[key].(string)
	if !ok {
		return
	}
	*dst = &value
	sources[key] = source
}

func copyBoolField(src map[string]interface{}, key string, dst **bool, sources map[string]string, source string) {
	if _, exists := sources[key]; exists {
		return
	}
	value, ok := src[key].(bool)
	if !ok {
		return
	}
	*dst = &value
	sources[key] = source
}

func cloneMapDeep(input map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(input))
	for key, value := range input {
		cloned[key] = cloneJSONValue(value)
	}
	return cloned
}

func cloneJSONValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return cloneMapDeep(typed)
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, item := range typed {
			out[i] = cloneJSONValue(item)
		}
		return out
	default:
		return value
	}
}

func stableUniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func toStringSlice(input interface{}) ([]string, bool) {
	raw, ok := input.([]interface{})
	if !ok {
		values, ok := input.([]string)
		return values, ok
	}

	out := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			continue
		}
		out = append(out, text)
	}
	return out, true
}

func toSubPackages(input interface{}) []pkg.SubPackageIR {
	rawList, ok := input.([]interface{})
	if !ok {
		return nil
	}

	result := make([]pkg.SubPackageIR, 0, len(rawList))
	for _, raw := range rawList {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		sub := pkg.SubPackageIR{}
		if root, ok := item["root"].(string); ok {
			sub.Root = root
		}
		if pages, ok := toStringSlice(item["pages"]); ok {
			sub.Pages = pages
		}
		result = append(result, sub)
	}
	return result
}

func loadJSONFile(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := make(map[string]interface{})
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func extractAppServiceMetadata(content string) (map[string]interface{}, map[string]map[string]interface{}, map[string]string) {
	appJSON := map[string]interface{}{}
	pageJSON := map[string]map[string]interface{}{}
	chunkMap := map[string]string{}

	for _, entry := range extractWxAppCodeAssignments(content) {
		if strings.HasSuffix(entry.Name, ".json") {
			parsed := map[string]interface{}{}
			if err := json.Unmarshal([]byte(entry.Body), &parsed); err != nil {
				continue
			}
			if entry.Name == "app.json" {
				appJSON = parsed
				continue
			}
			pageJSON[entry.Name] = parsed
		}
	}

	for page, chunk := range extractLazyChunkMap(content) {
		chunkMap[page] = chunk
	}
	return appJSON, pageJSON, chunkMap
}

type appCodeAssignment struct {
	Name string
	Body string
}

func extractWxAppCodeAssignments(content string) []appCodeAssignment {
	const marker = "__wxAppCode__['"
	assignments := []appCodeAssignment{}
	for offset := 0; offset < len(content); {
		start := strings.Index(content[offset:], marker)
		if start < 0 {
			break
		}
		start += offset
		nameStart := start + len(marker)
		nameEnd := strings.Index(content[nameStart:], "']")
		if nameEnd < 0 {
			break
		}
		nameEnd += nameStart
		name := content[nameStart:nameEnd]
		assignStart := strings.Index(content[nameEnd:], "=")
		if assignStart < 0 {
			offset = nameEnd + 2
			continue
		}
		assignStart += nameEnd + 1
		bodyStart := strings.Index(content[assignStart:], "{")
		if bodyStart < 0 {
			offset = nameEnd + 2
			continue
		}
		bodyStart += assignStart
		body, next := extractBalancedObject(content, bodyStart)
		if body != "" {
			assignments = append(assignments, appCodeAssignment{Name: name, Body: body})
		}
		offset = next
	}
	return assignments
}

func extractBalancedObject(content string, start int) (string, int) {
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(content); i++ {
		ch := content[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return content[start : i+1], i + 1
			}
		}
	}
	return "", len(content)
}

func extractLazyChunkMap(content string) map[string]string {
	result := map[string]string{}
	segmentStart := strings.Index(content, "__LAZY_CODE_LOADING_CHUNK_MAP__")
	if segmentStart < 0 {
		return result
	}
	segment := content[segmentStart:]
	segmentEnd := strings.Index(segment, ".forEach(")
	if segmentEnd > 0 {
		segment = segment[:segmentEnd]
	}
	re := regexp.MustCompile(`\['([^']+)'\s*,\[((?:'[^']*',?)*)\]\]`)
	matches := re.FindAllStringSubmatch(segment, -1)
	for _, match := range matches {
		chunkName := match[1]
		items := strings.Split(strings.TrimSuffix(strings.TrimPrefix(match[2], "'"), "'"), "','")
		for _, item := range items {
			item = strings.Trim(item, "', ")
			if item == "" {
				continue
			}
			switch chunkName {
			case "__COMMON__":
				result[item] = "common.app.js"
			default:
				result[item] = chunkName + ".appservice.js"
			}
		}
	}
	return result
}

func shouldAppendManifestPage(m *pkg.ManifestIR, pagePath string, content map[string]interface{}) bool {
	if content != nil {
		if component, ok := content["component"].(bool); ok && component {
			return false
		}
	}
	if source := m.Sources["pages"]; source == "app.json" || source == "app-config.json" || source == "app-service.js" {
		return false
	}
	return true
}

func shouldIgnoreManifestJSON(pagePath string, content map[string]interface{}) bool {
	if isIgnoredManifestPath(pagePath) {
		return true
	}
	if content != nil {
		if component, ok := content["component"].(bool); ok && component {
			return true
		}
	}
	return looksLikeComponentPath(pagePath)
}

func isIgnoredManifestPath(pagePath string) bool {
	switch pagePath {
	case "app", "project.config", "project.private.config", "sitemap", "ext", "ext-app":
		return true
	default:
		return false
	}
}

func looksLikeComponentPath(pagePath string) bool {
	return strings.HasPrefix(pagePath, "components/") ||
		strings.Contains(pagePath, "/components/") ||
		strings.HasPrefix(pagePath, "uni_modules/") ||
		strings.HasPrefix(pagePath, "wxcomponents/")
}
