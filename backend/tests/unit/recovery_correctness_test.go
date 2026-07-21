package unit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
	"github.com/keepbuild/seewxapkg/internal/infra/process"
	"github.com/keepbuild/seewxapkg/internal/pipeline/normalize"
	"github.com/keepbuild/seewxapkg/internal/pipeline/recover"
	"github.com/keepbuild/seewxapkg/internal/pipeline/verify"
)

func TestStandardAppJSONIsAuthoritativeAndRoundTripsUnknownFields(t *testing.T) {
	extracted := t.TempDir()
	writeFixtureFile(t, extracted, "app.json", `{
		"pages":["pages/detail/index","pages/home/index","pages/detail/index"],
		"window":{"navigationBarTitleText":"Authoritative"},
		"usingComponents":{"global-card":"/components/global-card"},
		"subPackages":[{"root":"feature","pages":["index"],"independent":true}],
		"darkmode":true,
		"futureOption":{"enabled":true}
	}`)
	writeFixtureFile(t, extracted, "app-config.json", `{"pages":["pages/wrong/index"],"window":{"navigationBarTitleText":"Compiled"}}`)
	writeFixtureFile(t, extracted, "pages/detail/index.json", `{"usingComponents":{"detail-card":"/components/detail-card"},"navigationBarTitleText":"Detail"}`)
	writeFixtureFile(t, extracted, "pages/detail/index.js", `Page({})`)
	writeFixtureFile(t, extracted, "pages/detail/index.wxml", `<view />`)
	writeFixtureFile(t, extracted, "pages/home/index.js", `Page({})`)
	writeFixtureFile(t, extracted, "pages/home/index.wxml", `<view />`)

	normalized, err := normalize.NormalizePackage(extracted, &pkg.PackageProfile{})
	if err != nil {
		t.Fatalf("NormalizePackage returned error: %v", err)
	}
	wantPages := []string{"pages/detail/index", "pages/home/index"}
	if !reflect.DeepEqual(normalized.Manifest.Pages, wantPages) {
		t.Fatalf("pages order/dedup mismatch: got %#v want %#v", normalized.Manifest.Pages, wantPages)
	}
	if got := normalized.Manifest.Sources["pages"]; got != "app.json" {
		t.Fatalf("standard app.json must be authoritative, got source %q", got)
	}
	if _, promoted := normalized.Manifest.UsingComponents["detail-card"]; promoted {
		t.Fatal("page-level usingComponents was promoted to global manifest")
	}
	if len(normalized.Pages) != 2 || normalized.Pages[0].UsingComponents["detail-card"] != "/components/detail-card" {
		t.Fatalf("page config was not retained in PageIR: %#v", normalized.Pages)
	}

	sourceDir := filepath.Join(t.TempDir(), "src")
	reportsDir := filepath.Join(t.TempDir(), "reports")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(reportsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := recover.RecoverManifest(normalized, sourceDir, reportsDir); err != nil {
		t.Fatalf("RecoverManifest returned error: %v", err)
	}

	var recovered map[string]interface{}
	readJSONFixture(t, filepath.Join(sourceDir, "app.json"), &recovered)
	if recovered["darkmode"] != true {
		t.Fatalf("unknown boolean field was lost: %#v", recovered)
	}
	if future, ok := recovered["futureOption"].(map[string]interface{}); !ok || future["enabled"] != true {
		t.Fatalf("unknown nested field was lost: %#v", recovered["futureOption"])
	}
	subpackages := recovered["subPackages"].([]interface{})
	if subpackages[0].(map[string]interface{})["independent"] != true {
		t.Fatalf("unknown subpackage field was lost: %#v", subpackages[0])
	}
	usingComponents := recovered["usingComponents"].(map[string]interface{})
	if _, promoted := usingComponents["detail-card"]; promoted {
		t.Fatal("recovered app.json contains page-level usingComponents")
	}

	var recoveredPage map[string]interface{}
	readJSONFixture(t, filepath.Join(sourceDir, "pages/detail/index.json"), &recoveredPage)
	pageComponents := recoveredPage["usingComponents"].(map[string]interface{})
	if pageComponents["detail-card"] != "/components/detail-card" {
		t.Fatalf("page-level config was not recovered: %#v", recoveredPage)
	}
}

func TestMalformedAuthoritativeAppJSONFailsNormalization(t *testing.T) {
	extracted := t.TempDir()
	writeFixtureFile(t, extracted, "app.json", `{"pages":[}`)
	writeFixtureFile(t, extracted, "app-config.json", `{"pages":["pages/fallback/index"]}`)
	if _, err := normalize.NormalizePackage(extracted, &pkg.PackageProfile{}); err == nil {
		t.Fatal("malformed app.json must not silently fall back to app-config.json")
	}
}

func TestFallbackMergePreservesNativeConflicts(t *testing.T) {
	target := t.TempDir()
	fallback := t.TempDir()
	writeFixtureFile(t, target, "pages/home/index.js", "native")
	writeFixtureFile(t, target, "pages/home/index.wxml", "")
	writeFixtureFile(t, fallback, "pages/home/index.js", "fallback")
	writeFixtureFile(t, fallback, "pages/home/index.wxml", "<view />")
	writeFixtureFile(t, fallback, "pages/home/index.wxss", ".page{}")
	writeFixtureFile(t, fallback, "app.json", `{"pages":["wrong"]}`)

	result, err := recover.MergeFallbackArtifactsWithPolicy(target, fallback)
	if err != nil {
		t.Fatalf("MergeFallbackArtifactsWithPolicy returned error: %v", err)
	}
	if result.Preserved != 2 || result.Added != 1 || len(result.Conflicts) != 2 {
		t.Fatalf("unexpected merge result: %#v", result)
	}
	if got := string(readFixtureFile(t, filepath.Join(target, "pages/home/index.js"))); got != "native" {
		t.Fatalf("fallback overwrote native file: %q", got)
	}
	if got := string(readFixtureFile(t, filepath.Join(target, "pages/home/index.wxml"))); got != "" {
		t.Fatalf("fallback overwrote a valid empty native file: %q", got)
	}
	if _, err := os.Stat(filepath.Join(target, "app.json")); !os.IsNotExist(err) {
		t.Fatalf("fallback app.json must not be merged, stat error: %v", err)
	}
}

func TestManifestVerifierDoesNotTreatGlobalRuntimeAsPageSource(t *testing.T) {
	normalized := &pkg.NormalizedPackage{
		Profile:  pkg.PackageProfile{HasAppServiceJS: true, HasPageFrameHTML: true},
		Manifest: pkg.ManifestIR{Pages: []string{"pages/missing/index"}},
		Pages: []pkg.PageIR{{
			Path:         "pages/missing/index",
			RuntimeChunk: "common.app.js",
			RuntimeHTML:  "page-frame.html",
		}},
	}
	result, err := verify.VerifyManifest(normalized, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if result.Success || !reflect.DeepEqual(result.MissingPages, []string{"pages/missing/index"}) {
		t.Fatalf("runtime metadata incorrectly satisfied missing page: %#v", result)
	}

	empty, err := verify.VerifyManifest(&pkg.NormalizedPackage{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if empty.Success || len(empty.Diagnostics) == 0 {
		t.Fatalf("empty manifest should fail verification: %#v", empty)
	}

	root := t.TempDir()
	sourceDir := filepath.Join(root, "src")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, root, "outside.json", `{}`)
	escaping := &pkg.NormalizedPackage{Manifest: pkg.ManifestIR{Pages: []string{"../outside"}}}
	escapingResult, err := verify.VerifyManifest(escaping, sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	if escapingResult.Success || len(escapingResult.MissingPages) != 1 {
		t.Fatalf("page path outside source root must not satisfy verification: %#v", escapingResult)
	}
}

func TestArtifactVerifierTreatsWXSSAsOptionalAndResolvesRootRefs(t *testing.T) {
	sourceDir := t.TempDir()
	writeFixtureFile(t, sourceDir, "pages/home/index.js", "Page({})")
	writeFixtureFile(t, sourceDir, "pages/home/index.wxml", `<wxs module="compare">var less = 1 < 2; var markup = "<view>";</wxs><view data-label="a > b" wx:if="{{count > 1}}"><include src="/shared/card.wxml"/><include src="{{dynamicTemplate}}"/></view>`)
	writeFixtureFile(t, sourceDir, "shared/card.wxml", `<view>card</view>`)
	normalized := &pkg.NormalizedPackage{Manifest: pkg.ManifestIR{Pages: []string{"pages/home/index"}}}
	runner := &process.NodeRunner{Binary: "node", Timeout: 10 * time.Second, MemoryMB: 256}

	result, err := verify.VerifyArtifacts(runner, normalized, sourceDir)
	if err != nil {
		t.Fatalf("VerifyArtifacts returned error: %v", err)
	}
	if !result.Success || !result.VerifierPassed || result.PageTriplets != 1 {
		t.Fatalf("valid page without wxss should pass: %#v", result)
	}
	if result.WXSSFiles != 0 || result.WXSSParseable != 0 {
		t.Fatalf("unexpected wxss counts: %#v", result)
	}
}

func TestWXSSRecoveryTreatsMissingStylesAsValid(t *testing.T) {
	normalized := &pkg.NormalizedPackage{
		Manifest: pkg.ManifestIR{Pages: []string{"pages/home/index"}},
		Pages:    []pkg.PageIR{{Path: "pages/home/index"}},
	}
	reportsDir := t.TempDir()
	result, err := recover.RecoverWXSS(normalized, t.TempDir(), reportsDir)
	if err != nil {
		t.Fatalf("RecoverWXSS returned error: %v", err)
	}
	if !result.Success || result.Partial || result.Recovered != 0 {
		t.Fatalf("missing optional styles should be a valid result: %#v", result)
	}
}

func TestArtifactVerifierRejectsBrokenWXMLNesting(t *testing.T) {
	sourceDir := t.TempDir()
	writeFixtureFile(t, sourceDir, "pages/home/index.js", "Page({})")
	writeFixtureFile(t, sourceDir, "pages/home/index.wxml", `<view><text></view>`)
	normalized := &pkg.NormalizedPackage{Manifest: pkg.ManifestIR{Pages: []string{"pages/home/index"}}}
	runner := &process.NodeRunner{Binary: "node", Timeout: 10 * time.Second, MemoryMB: 256}

	result, err := verify.VerifyArtifacts(runner, normalized, sourceDir)
	if err != nil {
		t.Fatalf("VerifyArtifacts returned error: %v", err)
	}
	if result.Success || result.VerifierPassed || result.WXMLParseable != 0 || len(result.Diagnostics) == 0 {
		t.Fatalf("broken WXML nesting should fail: %#v", result)
	}
}

func writeFixtureFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func readFixtureFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readJSONFixture(t *testing.T, path string, target interface{}) {
	t.Helper()
	if err := json.Unmarshal(readFixtureFile(t, path), target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}
