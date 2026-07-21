package recover

import (
	"os"
	"path/filepath"
	"strings"

	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
	"github.com/keepbuild/seewxapkg/internal/infra/storage"
	"github.com/keepbuild/seewxapkg/internal/report"
)

func RecoverJS(np *pkg.NormalizedPackage, outDir, reportsDir string) (*JSRecoveryResult, error) {
	result := &JSRecoveryResult{Success: true}
	hasRuntime := hasRuntimeScript(np)

	for _, script := range np.Scripts {
		if !strings.EqualFold(filepath.Ext(script.Path), ".js") {
			continue
		}
		if !fileExists(filepath.Join(outDir, filepath.FromSlash(script.Path))) {
			if script.Content == "" {
				continue
			}
			if err := writeRecoveredFile(filepath.Join(outDir, filepath.FromSlash(script.Path)), script.Content); err != nil {
				return nil, err
			}
		}
		result.Files = append(result.Files, RecoveredFile{Path: script.Path, Kind: "js", Source: pickSource(script.Source, "native")})
		result.Recovered++
		if script.Source == "native" {
			result.Native++
		}
	}

	appExists := fileExists(filepath.Join(outDir, "app.js"))
	if !appExists && hasRuntime {
		result.Partial = true
		result.Diagnostics = append(result.Diagnostics, pkg.Warn("recover.js.app.missing", "app.js 缺失，已识别到 app-service/appservice bundle，但原生恢复未直接产出页面入口", "recovering_js", "app.js"))
	}

	for _, page := range np.Pages {
		if page.ScriptPath != "" {
			continue
		}
		if !hasRuntime {
			result.Success = false
			result.Partial = true
			result.Diagnostics = append(result.Diagnostics, pkg.Warn("recover.js.page.missing", "页面 JS 缺失且无运行时脚本可推断", "recovering_js", page.Path))
			continue
		}
		result.Partial = true
		diagnostic := pkg.Warn("recover.js.page.missing_runtime", "页面 JS 缺失，已识别运行时 bundle 线索但未生成占位文件", "recovering_js", page.Path+".js")
		if page.RuntimeChunk != "" {
			diagnostic.Metadata = map[string]interface{}{
				"runtimeChunk": page.RuntimeChunk,
			}
		}
		result.Diagnostics = append(result.Diagnostics, diagnostic)
	}

	result.ReportPath = filepath.Join(reportsDir, "js-recovery-report.json")
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

func hasRuntimeScript(np *pkg.NormalizedPackage) bool {
	for _, script := range np.Scripts {
		if script.Path == "app-service.js" || script.Path == "workers.js" {
			return true
		}
	}
	return false
}

func pickSource(source, fallback string) string {
	if source == "" {
		return fallback
	}
	return source
}

func writeRecoveredFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0600)
}
