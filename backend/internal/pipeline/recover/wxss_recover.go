package recover

import (
	"path/filepath"

	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
	"github.com/keepbuild/seewxapkg/internal/infra/storage"
	"github.com/keepbuild/seewxapkg/internal/report"
)

func RecoverWXSS(np *pkg.NormalizedPackage, outDir, reportsDir string) (*WXSSRecoveryResult, error) {
	result := &WXSSRecoveryResult{Success: true}
	hasStyleSource := hasRuntimeStyle(np)

	if fileExists(filepath.Join(outDir, "app.wxss")) {
		result.Files = append(result.Files, RecoveredFile{Path: "app.wxss", Kind: "wxss", Source: "native"})
		result.Recovered++
		result.Native++
	} else if hasStyleSource {
		result.Partial = true
		result.Diagnostics = append(result.Diagnostics, pkg.Warn("recover.wxss.app.missing", "app.wxss 缺失，已识别 runtime 样式入口但未生成占位样式", "recovering_wxss", "app.wxss"))
	}

	for _, page := range np.Pages {
		if page.StylePath != "" && fileExists(filepath.Join(outDir, filepath.FromSlash(page.StylePath))) {
			result.Files = append(result.Files, RecoveredFile{Path: page.StylePath, Kind: "wxss", Source: "native"})
			result.Recovered++
			result.Native++
			continue
		}
		if !hasStyleSource {
			// WXSS is optional for both pages and components. With no runtime
			// evidence of a stylesheet, absence is a valid complete result.
			continue
		}
		// A global runtime stylesheet does not prove that this page originally
		// declared a page-level stylesheet, so absence remains valid here.
	}

	result.ReportPath = filepath.Join(reportsDir, "wxss-recovery-report.json")
	persisted := *result
	persisted.ReportPath = filepath.ToSlash(filepath.Join("reports", filepath.Base(result.ReportPath)))
	persisted.Diagnostics = report.SanitizeDiagnostics(result.Diagnostics)
	if err := storage.WriteJSON(result.ReportPath, &persisted); err != nil {
		return nil, err
	}

	return result, nil
}

func hasRuntimeStyle(np *pkg.NormalizedPackage) bool {
	for _, style := range np.Styles {
		if style.Path == "app.wxss" || style.Path == "app-wxss.js" {
			return true
		}
	}
	for _, script := range np.Scripts {
		if script.Path == "app-wxss.js" {
			return true
		}
	}
	return false
}
