package unit

import (
	"testing"

	normalize "github.com/keepbuild/seewxapkg/internal/pipeline/normalize"
)

func TestNormalizeManifestFromAppConfig(t *testing.T) {
	raw := &normalize.RawArtifactSet{
		RootDir: "/tmp/pkg",
		AppConfig: map[string]interface{}{
			"pages": []interface{}{"pages/home/index", "pages/detail/index"},
			"global": map[string]interface{}{
				"window": map[string]interface{}{
					"navigationBarTitleText": "Demo",
				},
			},
			"tabBar": map[string]interface{}{
				"list": []interface{}{},
			},
		},
		PageJSON: map[string]map[string]interface{}{},
	}

	manifest, diagnostics, err := normalize.NormalizeManifest(raw)
	if err != nil {
		t.Fatalf("NormalizeManifest returned error: %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diagnostics)
	}
	if len(manifest.Pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(manifest.Pages))
	}
	if manifest.Sources["pages"] != "app-config.json" {
		t.Fatalf("expected pages source to be app-config.json, got %q", manifest.Sources["pages"])
	}
	if manifest.Window["navigationBarTitleText"] != "Demo" {
		t.Fatalf("expected window.title to be recovered")
	}
}

func TestNormalizeManifestCanonicalizesCompiledPageAndTabBarRoutes(t *testing.T) {
	raw := &normalize.RawArtifactSet{
		RootDir: "/tmp/pkg",
		AppConfig: map[string]interface{}{
			"pages": []interface{}{`/pages/index/index.html`, `pages/user/profile.wxml`},
			"tabBar": map[string]interface{}{
				"list": []interface{}{
					map[string]interface{}{"pagePath": `/pages/index/index.html`, "text": "首页"},
					map[string]interface{}{"pagePath": `pages/user/profile.js`, "text": "我的"},
				},
			},
		},
	}

	manifest, diagnostics, err := normalize.NormalizeManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Pages) != 2 || manifest.Pages[0] != "pages/index/index" || manifest.Pages[1] != "pages/user/profile" {
		t.Fatalf("compiled page routes were not normalized: %#v", manifest.Pages)
	}
	list, ok := manifest.TabBar["list"].([]interface{})
	if !ok || len(list) != 2 {
		t.Fatalf("tabBar list was not preserved: %#v", manifest.TabBar)
	}
	first := list[0].(map[string]interface{})
	second := list[1].(map[string]interface{})
	if first["pagePath"] != "pages/index/index" || second["pagePath"] != "pages/user/profile" {
		t.Fatalf("tabBar routes were not normalized: %#v", list)
	}
	if first["text"] != "首页" || second["text"] != "我的" {
		t.Fatalf("tabBar business fields were changed: %#v", list)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "manifest.tabbar.routes_normalized" {
		t.Fatalf("normalization should be reported once without lowering quality: %#v", diagnostics)
	}
}

func TestNormalizeManifestFallsBackToDirectoryScan(t *testing.T) {
	raw := &normalize.RawArtifactSet{
		RootDir:  "/tmp/pkg",
		PageJSON: map[string]map[string]interface{}{},
		Files: []string{
			"pages/home/index.wxml",
			"pages/home/index.js",
			"pages/detail/index.wxss",
			"app-service.js",
		},
	}

	manifest, diagnostics, err := normalize.NormalizeManifest(raw)
	if err != nil {
		t.Fatalf("NormalizeManifest returned error: %v", err)
	}
	if len(manifest.Pages) != 2 {
		t.Fatalf("expected inferred pages, got %+v", manifest.Pages)
	}
	if manifest.Sources["pages"] != "directory-scan" {
		t.Fatalf("expected pages source to be directory-scan, got %q", manifest.Sources["pages"])
	}
	if len(diagnostics) == 0 {
		t.Fatalf("expected diagnostics for directory-scan fallback")
	}
}

func TestNormalizeManifestDoesNotOverrideEmptyAuthoritativePages(t *testing.T) {
	raw := &normalize.RawArtifactSet{
		RootDir: "/tmp/pkg",
		AppJSON: map[string]interface{}{
			"pages": []interface{}{},
		},
		PageJSON: map[string]map[string]interface{}{
			"pages/inferred/index.json": {},
		},
		Files: []string{"pages/inferred/index.wxml"},
	}

	manifest, diagnostics, err := normalize.NormalizeManifest(raw)
	if err != nil {
		t.Fatalf("NormalizeManifest returned error: %v", err)
	}
	if len(manifest.Pages) != 0 || manifest.Sources["pages"] != "app.json" {
		t.Fatalf("empty authoritative pages was replaced: %#v", manifest)
	}
	if len(diagnostics) == 0 {
		t.Fatal("expected an empty-pages diagnostic")
	}
}
