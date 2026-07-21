package normalize

import "testing"

func TestNormalizeManifestFromAppService(t *testing.T) {
	raw := &RawArtifactSet{
		RootDir: "/tmp/pkg",
		AppServiceJS: `
			var __wxAppCode__ = __wxAppCode__ || {};
			__wxAppCode__['app.json'] = {"pages":["pages/home/index"]};
			__wxAppCode__['pages/home/index.json'] = {"usingComponents":{"demo-card":"/components/demo-card"}};
			var __LAZY_CODE_LOADING_CHUNK_MAP__ = __LAZY_CODE_LOADING_CHUNK_MAP__ || {};
			[['chunk_1',['pages/home/index',]]].forEach(function(a){(a[1]||[]).forEach(function(b){__LAZY_CODE_LOADING_CHUNK_MAP__[b]=__LAZY_CODE_LOADING_CHUNK_MAP__[b]||a[0]||''});});
		`,
		PageJSON: map[string]map[string]interface{}{},
	}
	raw.AppServiceApp, raw.PageJSON, raw.LazyChunkMap = extractAppServiceMetadata(raw.AppServiceJS)

	manifest, diagnostics, err := NormalizeManifest(raw)
	if err != nil {
		t.Fatalf("NormalizeManifest returned error: %v", err)
	}
	if len(manifest.Pages) != 1 || manifest.Pages[0] != "pages/home/index" {
		t.Fatalf("expected app-service pages to be recovered, got %+v", manifest.Pages)
	}
	if manifest.Sources["pages"] != "app-service.js" {
		t.Fatalf("expected pages source app-service.js, got %q", manifest.Sources["pages"])
	}
	if _, promoted := manifest.UsingComponents["demo-card"]; promoted {
		t.Fatalf("page-level usingComponents must not be promoted to app.json")
	}
	if manifest.Confidence["pages"] != "runtime" {
		t.Fatalf("expected runtime confidence, got %q", manifest.Confidence["pages"])
	}
	if len(diagnostics) == 0 {
		t.Fatalf("expected diagnostics from app-service parsing")
	}
}
