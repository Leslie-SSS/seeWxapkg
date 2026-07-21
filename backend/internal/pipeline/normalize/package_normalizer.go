package normalize

import (
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"

	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
)

func NormalizePackage(extractedDir string, profile *pkg.PackageProfile) (*pkg.NormalizedPackage, error) {
	raw, err := collectRawArtifacts(extractedDir)
	if err != nil {
		return nil, err
	}

	manifest, diagnostics, err := NormalizeManifest(raw)
	if err != nil {
		return nil, err
	}

	normalized := &pkg.NormalizedPackage{
		Profile:     *profile,
		Manifest:    *manifest,
		Diagnostics: diagnostics,
	}

	for _, page := range manifest.Pages {
		normalized.Pages = append(normalized.Pages, buildPageIR(raw, page))
	}

	for _, file := range raw.Files {
		switch strings.ToLower(filepath.Ext(file)) {
		case ".js", ".wxs":
			normalized.Scripts = append(normalized.Scripts, pkg.ScriptIR{
				Path:      file,
				Content:   raw.FileContents[file],
				Source:    sourceForFile(file),
				EntryKind: inferScriptKind(file),
			})
		case ".wxss", ".css":
			normalized.Styles = append(normalized.Styles, pkg.StyleIR{
				Path:    file,
				Content: raw.FileContents[file],
				Source:  sourceForFile(file),
			})
		case ".wxml", ".html":
			kind := "file"
			if strings.HasSuffix(strings.ToLower(file), ".wxml") {
				kind = "page"
			}
			normalized.Templates = append(normalized.Templates, pkg.TemplateIR{
				Name:    file,
				Kind:    kind,
				Path:    file,
				Content: raw.FileContents[file],
				Source:  sourceForFile(file),
			})
		default:
			normalized.Assets = append(normalized.Assets, pkg.AssetIR{Path: file, Source: sourceForFile(file)})
		}
	}

	normalized.Components = inferComponents(raw, *manifest)

	return normalized, nil
}

func collectRawArtifacts(extractedDir string) (*RawArtifactSet, error) {
	raw := &RawArtifactSet{
		RootDir:      extractedDir,
		PageJSON:     map[string]map[string]interface{}{},
		LazyChunkMap: map[string]string{},
		FileContents: map[string]string{},
	}

	if err := filepath.Walk(extractedDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		rel, err := filepath.Rel(extractedDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		raw.Files = append(raw.Files, rel)
		if isNormalizedTextArtifact(rel) {
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return fmt.Errorf("read text artifact %s: %w", rel, readErr)
			}
			if isLikelyText(content) {
				raw.FileContents[rel] = string(content)
			}
		}

		switch rel {
		case "app.json":
			raw.AppJSONPath = path
			appJSON, err := loadJSONFile(path)
			if err != nil {
				return fmt.Errorf("parse authoritative app.json: %w", err)
			}
			raw.AppJSON = appJSON
		case "app-config.json":
			raw.AppConfigPath = path
			appConfig, err := loadJSONFile(path)
			if err == nil {
				raw.AppConfig = appConfig
			}
		case "app-service.js":
			if content, ok := raw.FileContents[rel]; ok {
				raw.AppServiceJS = content
				appServiceApp, rawPageJSON, rawChunkMap := extractAppServiceMetadata(raw.AppServiceJS)
				raw.AppServiceApp = appServiceApp
				for name, content := range rawPageJSON {
					if _, exists := raw.PageJSON[name]; !exists {
						raw.PageJSON[name] = content
					}
				}
				for name, chunk := range rawChunkMap {
					raw.LazyChunkMap[name] = chunk
				}
			}
		default:
			if strings.HasSuffix(rel, ".json") {
				pageJSON, err := loadJSONFile(path)
				if err == nil {
					raw.PageJSON[rel] = pageJSON
				}
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return raw, nil
}

func isNormalizedTextArtifact(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".wxs", ".json", ".wxml", ".html", ".wxss", ".css":
		return true
	default:
		return false
	}
}

func buildPageIR(raw *RawArtifactSet, page string) pkg.PageIR {
	pageIR := pkg.PageIR{Path: page}
	if config, ok := raw.PageJSON[page+".json"]; ok {
		pageIR.Config = cloneMapDeep(config)
		if components, ok := config["usingComponents"].(map[string]interface{}); ok {
			pageIR.UsingComponents = make(map[string]string, len(components))
			for name, value := range components {
				if componentPath, ok := value.(string); ok {
					pageIR.UsingComponents[name] = componentPath
				}
			}
		}
	}
	for _, ext := range []string{".json", ".js", ".wxss", ".wxml"} {
		path := page + ext
		if !slices.Contains(raw.Files, path) {
			continue
		}
		switch ext {
		case ".json":
			pageIR.JSONPath = path
		case ".js":
			pageIR.ScriptPath = path
		case ".wxss":
			pageIR.StylePath = path
		case ".wxml":
			pageIR.TemplatePath = path
		}
	}
	if runtimeChunk, ok := raw.LazyChunkMap[page]; ok && runtimeChunk != "" {
		pageIR.RuntimeChunk = runtimeChunk
	}
	if htmlPath := page + ".html"; slices.Contains(raw.Files, htmlPath) {
		pageIR.RuntimeHTML = htmlPath
	}
	return pageIR
}

func inferComponents(raw *RawArtifactSet, manifest pkg.ManifestIR) []pkg.ComponentIR {
	componentPaths := make([]string, 0, len(manifest.UsingComponents))
	for _, componentPath := range manifest.UsingComponents {
		if resolved := resolveComponentPath("", componentPath); resolved != "" {
			componentPaths = append(componentPaths, resolved)
		}
	}
	for rel, config := range raw.PageJSON {
		pagePath := strings.TrimSuffix(filepath.ToSlash(rel), ".json")
		if shouldIgnoreManifestJSON(pagePath, config) {
			continue
		}
		if usingComponents, ok := config["usingComponents"].(map[string]interface{}); ok {
			for _, value := range usingComponents {
				if componentPath, ok := value.(string); ok {
					if resolved := resolveComponentPath(pagePath, componentPath); resolved != "" {
						componentPaths = append(componentPaths, resolved)
					}
				}
			}
		}
	}
	slices.Sort(componentPaths)
	components := make([]pkg.ComponentIR, 0, len(componentPaths))
	seen := make(map[string]struct{})
	for _, clean := range componentPaths {
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		if slices.Contains(raw.Files, clean+".wxml") || slices.Contains(raw.Files, clean+".js") {
			components = append(components, pkg.ComponentIR{Path: clean})
		}
	}
	return components
}

func resolveComponentPath(pagePath, reference string) string {
	if reference == "" || strings.Contains(reference, "://") {
		return ""
	}
	var clean string
	if strings.HasPrefix(reference, "/") || pagePath == "" {
		clean = pathpkg.Clean(strings.TrimPrefix(reference, "/"))
	} else {
		clean = pathpkg.Clean(pathpkg.Join(pathpkg.Dir(pagePath), reference))
	}
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return ""
	}
	return clean
}

func inferScriptKind(path string) string {
	switch {
	case path == "app.js":
		return "app"
	case path == "workers.js":
		return "worker"
	case strings.HasSuffix(path, ".wxs"):
		return "wxs"
	default:
		return "page"
	}
}

func sourceForFile(path string) string {
	switch path {
	case "app-service.js", "app-config.json", "page-frame.html", "page-frame.js", "app-wxss.js":
		return "runtime"
	default:
		return "native"
	}
}

func isLikelyText(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	max := len(data)
	if max > 512 {
		max = 512
	}
	for i := 0; i < max; i++ {
		if data[i] == 0 {
			return false
		}
	}
	return true
}
