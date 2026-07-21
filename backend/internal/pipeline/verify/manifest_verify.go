package verify

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
)

type ManifestVerifyResult struct {
	Success            bool             `json:"success"`
	PageCount          int              `json:"pageCount"`
	MissingPages       []string         `json:"missingPages,omitempty"`
	InvalidPagePaths   []string         `json:"invalidPagePaths,omitempty"`
	InvalidTabBarPages []string         `json:"invalidTabBarPages,omitempty"`
	ManifestIssueCount int              `json:"manifestIssueCount"`
	Diagnostics        []pkg.Diagnostic `json:"diagnostics,omitempty"`
}

func VerifyManifest(np *pkg.NormalizedPackage, sourceDir string) (*ManifestVerifyResult, error) {
	result := &ManifestVerifyResult{
		Success:   true,
		PageCount: len(np.Manifest.Pages),
	}
	if len(np.Manifest.Pages) == 0 {
		result.Success = false
		result.ManifestIssueCount = 1
		result.Diagnostics = append(result.Diagnostics, pkg.Error("verify.manifest.pages_empty", "manifest 不包含任何页面", "verifying", "app.json"))
		return result, nil
	}

	pageSet := make(map[string]struct{}, len(np.Manifest.Pages))
	seenPages := make(map[string]struct{}, len(np.Manifest.Pages))
	for _, pageRoute := range np.Manifest.Pages {
		pageSet[pageRoute] = struct{}{}
		if _, duplicate := seenPages[pageRoute]; duplicate {
			result.Success = false
			result.ManifestIssueCount++
			diagnostic := pkg.Warn("verify.manifest.page_duplicate", "manifest 中存在重复页面路由", "verifying", "app.json")
			diagnostic.Metadata = map[string]interface{}{"pagePath": pageRoute}
			result.Diagnostics = append(result.Diagnostics, diagnostic)
			continue
		}
		seenPages[pageRoute] = struct{}{}

		if !validManifestRoute(pageRoute) {
			result.Success = false
			result.ManifestIssueCount++
			result.InvalidPagePaths = append(result.InvalidPagePaths, pageRoute)
			result.MissingPages = append(result.MissingPages, pageRoute)
			diagnostic := pkg.Warn("verify.manifest.page_path_invalid", "页面路由格式无效；路由应为不带文件扩展名的相对路径", "verifying", "app.json")
			diagnostic.Metadata = map[string]interface{}{"pagePath": pageRoute}
			result.Diagnostics = append(result.Diagnostics, diagnostic)
			continue
		}

		exists := false
		base, safe := safePageBase(sourceDir, pageRoute)
		if !safe {
			result.Success = false
			result.ManifestIssueCount++
			result.InvalidPagePaths = append(result.InvalidPagePaths, pageRoute)
			result.MissingPages = append(result.MissingPages, pageRoute)
			continue
		}
		for _, ext := range []string{".js", ".wxml", ".json"} {
			if _, err := os.Stat(base + ext); err == nil {
				exists = true
				break
			}
		}
		if !exists {
			result.Success = false
			result.ManifestIssueCount++
			result.MissingPages = append(result.MissingPages, pageRoute)
		}
	}

	if len(result.MissingPages) > 0 {
		result.Diagnostics = append(result.Diagnostics, pkg.Warn("verify.manifest.missing_pages", "部分 manifest 页面未找到对应源码文件", "verifying", "app.json"))
	}

	verifyTabBarRoutes(&np.Manifest, pageSet, result)

	return result, nil
}

func verifyTabBarRoutes(manifest *pkg.ManifestIR, pageSet map[string]struct{}, result *ManifestVerifyResult) {
	if manifest == nil || len(manifest.TabBar) == 0 {
		return
	}

	rawList, exists := manifest.TabBar["list"]
	if !exists {
		return
	}
	items, ok := rawList.([]interface{})
	if !ok {
		result.Success = false
		result.ManifestIssueCount++
		result.Diagnostics = append(result.Diagnostics, pkg.Warn("verify.manifest.tabbar_list_invalid", "tabBar.list 不是有效的列表，无法验证导航目标", "verifying", "app.json"))
		return
	}

	seen := make(map[string]struct{}, len(items))
	for index, rawItem := range items {
		item, ok := rawItem.(map[string]interface{})
		if !ok {
			addInvalidTabBarDiagnostic(result, "verify.manifest.tabbar_item_invalid", "tabBar 中存在格式无效的导航项", "", index, "")
			continue
		}
		pageRoute, ok := item["pagePath"].(string)
		if !ok || !validManifestRoute(pageRoute) {
			expected := strings.TrimSuffix(pageRoute, path.Ext(pageRoute))
			addInvalidTabBarDiagnostic(result, "verify.manifest.tabbar_page_path_invalid", "tabBar.pagePath 应为不带文件扩展名的页面路由", pageRoute, index, expected)
			continue
		}
		if _, duplicate := seen[pageRoute]; duplicate {
			addInvalidTabBarDiagnostic(result, "verify.manifest.tabbar_page_duplicate", "tabBar 中存在重复的页面目标", pageRoute, index, "")
			continue
		}
		seen[pageRoute] = struct{}{}
		if _, registered := pageSet[pageRoute]; !registered {
			addInvalidTabBarDiagnostic(result, "verify.manifest.tabbar_page_unregistered", "tabBar.pagePath 未出现在 manifest.pages 中", pageRoute, index, "")
		}
	}
}

func addInvalidTabBarDiagnostic(result *ManifestVerifyResult, code, message, pageRoute string, index int, expected string) {
	result.Success = false
	result.ManifestIssueCount++
	result.InvalidTabBarPages = append(result.InvalidTabBarPages, pageRoute)
	diagnostic := pkg.Warn(code, message, "verifying", "app.json")
	diagnostic.Metadata = map[string]interface{}{"index": index}
	if pageRoute != "" {
		diagnostic.Metadata["pagePath"] = pageRoute
	}
	if expected != "" {
		diagnostic.Metadata["expectedPagePath"] = expected
	}
	result.Diagnostics = append(result.Diagnostics, diagnostic)
}

func validManifestRoute(route string) bool {
	if route == "" || route != strings.TrimSpace(route) || strings.Contains(route, "\\") || strings.ContainsAny(route, "?#") {
		return false
	}
	if strings.HasPrefix(route, "/") || strings.HasSuffix(route, "/") || path.Clean(route) != route || route == "." || strings.HasPrefix(route, "../") {
		return false
	}
	switch strings.ToLower(path.Ext(route)) {
	case ".html", ".js", ".json", ".wxml", ".wxss", ".wxs":
		return false
	default:
		return true
	}
}

func safePageBase(sourceDir, page string) (string, bool) {
	if page == "" || filepath.IsAbs(filepath.FromSlash(page)) {
		return "", false
	}
	base := filepath.Join(sourceDir, filepath.FromSlash(page))
	relative, err := filepath.Rel(sourceDir, base)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return base, true
}
