package recover

import (
	"os"
	"path/filepath"

	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
	"github.com/keepbuild/seewxapkg/internal/infra/storage"
	"github.com/keepbuild/seewxapkg/internal/report"
)

type ManifestRecoveryResult struct {
	Success     bool              `json:"success"`
	OutputPath  string            `json:"outputPath"`
	ReportPath  string            `json:"reportPath"`
	Sources     map[string]string `json:"sources,omitempty"`
	PageCount   int               `json:"pageCount"`
	Diagnostics []pkg.Diagnostic  `json:"diagnostics,omitempty"`
}

type manifestRecoveryReport struct {
	Success     bool              `json:"success"`
	PageCount   int               `json:"pageCount"`
	Sources     map[string]string `json:"sources"`
	Diagnostics []pkg.Diagnostic  `json:"diagnostics,omitempty"`
	Manifest    pkg.ManifestIR    `json:"manifest"`
}

func RecoverManifest(np *pkg.NormalizedPackage, sourceDir, reportsDir string) (*ManifestRecoveryResult, error) {
	outputPath := filepath.Join(sourceDir, "app.json")
	if err := writeAppJSON(np.Manifest, outputPath); err != nil {
		return nil, err
	}
	if err := writeRecoveredPageConfigs(np.Pages, sourceDir); err != nil {
		return nil, err
	}

	result := &ManifestRecoveryResult{
		Success:     true,
		OutputPath:  outputPath,
		ReportPath:  filepath.Join(reportsDir, "manifest-recovery-report.json"),
		Sources:     np.Manifest.Sources,
		PageCount:   len(np.Manifest.Pages),
		Diagnostics: np.Diagnostics,
	}

	if err := writeManifestRecoveryReport(result, np, reportsDir); err != nil {
		return nil, err
	}

	return result, nil
}

func writeAppJSON(manifest pkg.ManifestIR, outputPath string) error {
	appJSON := cloneJSONObject(manifest.Original)

	if len(manifest.Pages) > 0 || manifest.Sources["pages"] != "" {
		appJSON["pages"] = manifest.Pages
	}
	if len(manifest.SubPackages) > 0 {
		// Keep the authoritative representation when present. Besides preserving
		// the accepted subpackages spelling, this retains version-specific fields
		// on each subpackage that the current IR does not yet understand.
		if _, canonical := appJSON["subPackages"]; !canonical {
			if _, legacy := appJSON["subpackages"]; !legacy {
				appJSON["subPackages"] = manifest.SubPackages
			}
		}
	}
	if len(manifest.Window) > 0 || manifest.Sources["window"] != "" {
		appJSON["window"] = manifest.Window
	}
	if len(manifest.TabBar) > 0 || manifest.Sources["tabBar"] != "" {
		appJSON["tabBar"] = manifest.TabBar
	}
	if len(manifest.NetworkTimeout) > 0 || manifest.Sources["networkTimeout"] != "" {
		appJSON["networkTimeout"] = manifest.NetworkTimeout
	}
	if len(manifest.UsingComponents) > 0 || manifest.Sources["usingComponents"] != "" {
		appJSON["usingComponents"] = manifest.UsingComponents
	}
	if len(manifest.Plugins) > 0 {
		appJSON["plugins"] = manifest.Plugins
	}
	if len(manifest.Permission) > 0 {
		appJSON["permission"] = manifest.Permission
	}
	if manifest.Renderer != nil {
		appJSON["renderer"] = *manifest.Renderer
	}
	if manifest.Style != nil {
		appJSON["style"] = *manifest.Style
	}
	if manifest.SitemapLocation != nil {
		appJSON["sitemapLocation"] = *manifest.SitemapLocation
	}
	if len(manifest.RequiredBackgroundModes) > 0 || manifest.Sources["requiredBackgroundModes"] != "" {
		appJSON["requiredBackgroundModes"] = manifest.RequiredBackgroundModes
	}
	if len(manifest.PreloadRule) > 0 || manifest.Sources["preloadRule"] != "" {
		appJSON["preloadRule"] = manifest.PreloadRule
	}
	if manifest.Workers != nil {
		appJSON["workers"] = *manifest.Workers
	}
	if manifest.Debug != nil {
		appJSON["debug"] = *manifest.Debug
	}
	if len(manifest.NavigateToMiniProgramAppIDList) > 0 || manifest.Sources["navigateToMiniProgramAppIdList"] != "" {
		appJSON["navigateToMiniProgramAppIdList"] = manifest.NavigateToMiniProgramAppIDList
	}

	return storage.WriteJSON(outputPath, appJSON)
}

func writeRecoveredPageConfigs(pages []pkg.PageIR, sourceDir string) error {
	for _, page := range pages {
		if page.Config == nil {
			continue
		}
		rel := page.JSONPath
		if rel == "" {
			rel = page.Path + ".json"
		}
		target, ok := safeOutputPath(sourceDir, rel)
		if !ok {
			continue
		}
		if _, err := os.Stat(target); err == nil {
			// An extracted page config is stronger evidence than runtime-derived IR.
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		if err := storage.WriteJSON(target, page.Config); err != nil {
			return err
		}
	}
	return nil
}

func safeOutputPath(root, rel string) (string, bool) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", false
	}
	target := filepath.Join(root, filepath.FromSlash(rel))
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || len(relative) >= 3 && relative[:3] == ".."+string(filepath.Separator) {
		return "", false
	}
	return target, true
}

func cloneJSONObject(input map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(input))
	for key, value := range input {
		result[key] = cloneJSONValue(value)
	}
	return result
}

func cloneJSONValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return cloneJSONObject(typed)
	case []interface{}:
		result := make([]interface{}, len(typed))
		for i, item := range typed {
			result[i] = cloneJSONValue(item)
		}
		return result
	default:
		return value
	}
}

func writeManifestRecoveryReport(result *ManifestRecoveryResult, np *pkg.NormalizedPackage, reportsDir string) error {
	snapshot := manifestRecoveryReport{
		Success:     result.Success,
		PageCount:   result.PageCount,
		Sources:     result.Sources,
		Diagnostics: report.SanitizeDiagnostics(np.Diagnostics),
		Manifest:    np.Manifest,
	}
	return storage.WriteJSON(result.ReportPath, snapshot)
}
