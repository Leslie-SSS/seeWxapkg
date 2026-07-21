package recover

import (
	"path/filepath"
	"strings"

	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
	"github.com/keepbuild/seewxapkg/internal/infra/storage"
	"github.com/keepbuild/seewxapkg/internal/report"
)

func RecoverWXML(np *pkg.NormalizedPackage, outDir, reportsDir string) (*WXMLRecoveryResult, error) {
	result := &WXMLRecoveryResult{Success: true}
	hasFrameSource := hasTemplateSource(np)

	for _, page := range np.Pages {
		if page.TemplatePath != "" && fileExists(filepath.Join(outDir, filepath.FromSlash(page.TemplatePath))) {
			result.Files = append(result.Files, RecoveredFile{Path: page.TemplatePath, Kind: "wxml", Source: "native"})
			result.Recovered++
			result.Native++
			continue
		}

		if !hasFrameSource {
			result.Success = false
			result.Partial = true
			result.Diagnostics = append(result.Diagnostics, pkg.Warn("recover.wxml.page.missing", "页面 WXML 缺失且无 page-frame 线索", "recovering_wxml", page.Path+".wxml"))
			continue
		}
		result.Partial = true
		diagnostic := pkg.Warn("recover.wxml.page.missing_runtime", "页面 WXML 缺失，已识别 page-frame/webview 线索但未生成占位模板", "recovering_wxml", page.Path+".wxml")
		if page.RuntimeChunk != "" || page.RuntimeHTML != "" {
			diagnostic.Metadata = map[string]interface{}{}
			if page.RuntimeChunk != "" {
				diagnostic.Metadata["runtimeChunk"] = page.RuntimeChunk
			}
			if page.RuntimeHTML != "" {
				diagnostic.Metadata["runtimeHTML"] = page.RuntimeHTML
			}
		}
		result.Diagnostics = append(result.Diagnostics, diagnostic)
	}

	for _, component := range np.Components {
		targetPath := component.Path + ".wxml"
		if fileExists(filepath.Join(outDir, filepath.FromSlash(targetPath))) {
			result.Files = append(result.Files, RecoveredFile{Path: targetPath, Kind: "wxml", Source: "native"})
			result.Recovered++
			result.Native++
			continue
		}
		if !hasFrameSource {
			continue
		}
		result.Partial = true
		result.Diagnostics = append(result.Diagnostics, pkg.Warn("recover.wxml.component.missing_runtime", "组件 WXML 缺失，已识别 page-frame/webview 线索但未生成占位模板", "recovering_wxml", targetPath))
	}

	result.ReportPath = filepath.Join(reportsDir, "wxml-recovery-report.json")
	persisted := *result
	persisted.ReportPath = filepath.ToSlash(filepath.Join("reports", filepath.Base(result.ReportPath)))
	persisted.Diagnostics = report.SanitizeDiagnostics(result.Diagnostics)
	if err := storage.WriteJSON(result.ReportPath, &persisted); err != nil {
		return nil, err
	}

	if result.Recovered == 0 {
		result.Success = false
	}
	return result, nil
}

func hasTemplateSource(np *pkg.NormalizedPackage) bool {
	for _, template := range np.Templates {
		if strings.EqualFold(filepath.Ext(template.Path), ".wxml") {
			return true
		}
		if template.Path == "page-frame.html" || template.Path == "page-frame.js" || template.Name == "page-frame.html" {
			return true
		}
	}
	for _, script := range np.Scripts {
		if script.Path == "page-frame.js" {
			return true
		}
	}
	return false
}
