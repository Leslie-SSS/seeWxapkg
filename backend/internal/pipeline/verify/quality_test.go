package verify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
	"github.com/keepbuild/seewxapkg/internal/infra/process"
)

func TestVerifyManifestRejectsInvalidTabBarRoutes(t *testing.T) {
	sourceDir := t.TempDir()
	writeVerifyFixture(t, sourceDir, "pages/home/index.js", "Page({})")
	writeVerifyFixture(t, sourceDir, "pages/profile/index.js", "Page({})")
	normalized := &pkg.NormalizedPackage{Manifest: pkg.ManifestIR{
		Pages: []string{"pages/home/index", "pages/profile/index"},
		TabBar: map[string]interface{}{
			"list": []interface{}{
				map[string]interface{}{"pagePath": "pages/home/index.html"},
				map[string]interface{}{"pagePath": "pages/missing/index"},
			},
		},
	}}

	result, err := VerifyManifest(normalized, sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Fatalf("invalid tabBar routes must fail manifest verification: %#v", result)
	}
	if len(result.InvalidTabBarPages) != 2 || result.ManifestIssueCount != 2 {
		t.Fatalf("tabBar issues were not counted conservatively: %#v", result)
	}
	if len(result.MissingPages) != 0 {
		t.Fatalf("valid page source coverage must remain distinct from tabBar issues: %#v", result)
	}
}

func TestVerifyArtifactsSeparatesParseabilityFromWXMLQuality(t *testing.T) {
	sourceDir := t.TempDir()
	writeVerifyFixture(t, sourceDir, "pages/home/index.js", "Page({})")
	writeVerifyFixture(t, sourceDir, "pages/home/index.wxml", `<view><button bindtap="Empty">Empty</button><button catchtap="card active">Empty</button></view>`)
	normalized := &pkg.NormalizedPackage{Manifest: pkg.ManifestIR{Pages: []string{"pages/home/index"}}}
	runner := &process.NodeRunner{Binary: "node", Timeout: 10 * time.Second, MemoryMB: 256}

	result, err := VerifyArtifacts(runner, normalized, sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ParserPassed || result.WXMLParseable != 1 {
		t.Fatalf("fixture should remain syntactically parseable: %#v", result)
	}
	if result.Success || result.VerifierPassed || result.WXMLQualityPassed {
		t.Fatalf("placeholder WXML must not pass the complete verification gate: %#v", result)
	}
	if result.WXMLPlaceholderFiles != 1 || result.WXMLPlaceholderCount != 3 {
		t.Fatalf("placeholder counts are wrong: %#v", result)
	}
	if result.WXMLSuspiciousEventFiles != 1 || result.WXMLSuspiciousEventBindings != 2 || result.WXMLQualityIssueFiles != 1 {
		t.Fatalf("suspicious binding counts are wrong: %#v", result)
	}
	if len(result.Diagnostics) < 2 {
		t.Fatalf("expected actionable WXML quality diagnostics: %#v", result.Diagnostics)
	}
}

func TestRecoveryOmissionMarkersAreCountedByTypeAndFile(t *testing.T) {
	root := t.TempDir()
	writeVerifyFixture(t, root, "pages/home/index.wxml", `<view>
<!-- seewx-recovery: unresolved text omitted -->
<!-- seewx-recovery: unresolved text omitted -->
<!-- seewx-recovery: unresolved attributes omitted -->
</view>`)
	writeVerifyFixture(t, root, "pages/profile/index.wxml", `<view><!-- seewx-recovery: unresolved attributes omitted --></view>`)
	writeVerifyFixture(t, root, "pages/about/index.wxml", `<view>
<!-- Ordinary business note: unresolved text may be omitted by the product. -->
<!-- unresolved attributes omitted -->
<!-- seewx-recovery: unresolved text discussed during design review -->
</view>`)

	result, err := inspectWXMLQuality(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.UnresolvedMarkerFiles != 2 || result.UnresolvedMarkers != 4 {
		t.Fatalf("recovery marker file/total counts are wrong: %#v", result)
	}
	if result.UnresolvedTextMarkers != 2 || result.UnresolvedAttributeMarkers != 2 {
		t.Fatalf("recovery marker type counts are wrong: %#v", result)
	}
	if len(result.IssueFiles) != 2 {
		t.Fatalf("only files with exact recovery markers should be quality issues: %#v", result.IssueFiles)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("expected one plain-language diagnostic per affected file: %#v", result.Diagnostics)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code != "verify.wxml.unresolved_recovery_marker" || diagnostic.Severity != pkg.SeverityWarn {
			t.Fatalf("unexpected recovery marker diagnostic: %#v", diagnostic)
		}
		if diagnostic.Metadata["count"].(int) < 1 {
			t.Fatalf("diagnostic must include an actionable per-file count: %#v", diagnostic)
		}
	}
}

func TestRecoveryOmissionMarkerBlocksVerifierAndScore(t *testing.T) {
	sourceDir := t.TempDir()
	writeVerifyFixture(t, sourceDir, "pages/home/index.js", "Page({})")
	writeVerifyFixture(t, sourceDir, "pages/home/index.wxml", `<view><!-- seewx-recovery: unresolved text omitted --></view>`)
	normalized := &pkg.NormalizedPackage{Manifest: pkg.ManifestIR{Pages: []string{"pages/home/index"}}}
	runner := &process.NodeRunner{Binary: "node", Timeout: 10 * time.Second, MemoryMB: 256}

	result, err := VerifyArtifacts(runner, normalized, sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ParserPassed || result.WXMLParseable != 1 {
		t.Fatalf("the marker comment should remain syntactically parseable: %#v", result)
	}
	if result.Success || result.VerifierPassed || result.WXMLQualityPassed {
		t.Fatalf("unresolved recovery output must not pass the quality gate: %#v", result)
	}
	if result.WXMLUnresolvedMarkerFiles != 1 || result.WXMLUnresolvedMarkers != 1 || result.WXMLQualityIssueFiles != 1 {
		t.Fatalf("unresolved recovery output was not counted as a quality issue: %#v", result)
	}

	score := ComputeRecoveryScore(&ManifestVerifyResult{Success: true, PageCount: 1}, result, true, false)
	if score.VerifierPassed || score.WXML != 0 || score.Overall > 79 {
		t.Fatalf("unresolved recovery output must lower the WXML score and block a high overall score: %#v", score)
	}
}

func TestValidEventBindingsDoNotCreateQualityIssues(t *testing.T) {
	result, err := inspectWXMLQuality(writeWXMLQualityTree(t, `<view bindtap="onTap" bind:change="handleChange" binding="card active" />`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.IssueFiles) != 0 || result.SuspiciousEventBindings != 0 || result.PlaceholderCount != 0 {
		t.Fatalf("valid handlers were classified as suspicious: %#v", result)
	}
}

func TestBusinessEmptyTextAndCommentsDoNotCreateQualityIssues(t *testing.T) {
	result, err := inspectWXMLQuality(writeWXMLQualityTree(t, `<view><!-- Empty is a valid product term --><text>Empty</text><view aria-label="Empty" /></view>`))
	if err != nil {
		t.Fatal(err)
	}
	if result.PlaceholderCount != 0 || len(result.IssueFiles) != 0 {
		t.Fatalf("one-off business text and comments must not be treated as recovery placeholders: %#v", result)
	}
}

func TestMoustacheEventBindingsAreInformationalOnly(t *testing.T) {
	result, err := inspectWXMLQuality(writeWXMLQualityTree(t, `<view bindtap="{{item.id}}" bindchange="{{groupType==='standard'}}" />`))
	if err != nil {
		t.Fatal(err)
	}
	if result.DynamicEventFiles != 1 || result.DynamicEventBindings != 2 || result.SuspiciousEventBindings != 0 || len(result.IssueFiles) != 0 {
		t.Fatalf("dynamic event bindings were not counted: %#v", result)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "verify.wxml.dynamic_event_binding" || result.Diagnostics[0].Severity != pkg.SeverityInfo {
		t.Fatalf("dynamic event diagnostic is missing: %#v", result.Diagnostics)
	}
}

func TestVerifyArtifactsAcceptsMoustacheEventBindings(t *testing.T) {
	sourceDir := t.TempDir()
	writeVerifyFixture(t, sourceDir, "pages/home/index.js", "Page({})")
	writeVerifyFixture(t, sourceDir, "pages/home/index.wxml", `<view bindtap="{{ handlerName }}" bindchange="{{wxsModule.handleChange}}" />`)
	normalized := &pkg.NormalizedPackage{Manifest: pkg.ManifestIR{Pages: []string{"pages/home/index"}}}
	runner := &process.NodeRunner{Binary: "node", Timeout: 10 * time.Second, MemoryMB: 256}

	result, err := VerifyArtifacts(runner, normalized, sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || !result.VerifierPassed || !result.WXMLQualityPassed || result.WXMLQualityIssueFiles != 0 {
		t.Fatalf("legal moustache bindings must not lower WXML quality: %#v", result)
	}
	if result.WXMLDynamicEventBindings != 2 || result.WXMLSuspiciousEventBindings != 0 {
		t.Fatalf("legal dynamic bindings should only be informational: %#v", result)
	}
}

func TestVerifyArtifactsRejectsToleratedRawWXSSSelector(t *testing.T) {
	sourceDir := t.TempDir()
	writeVerifyFixture(t, sourceDir, "pages/home/index.js", "Page({})")
	writeVerifyFixture(t, sourceDir, "pages/home/index.wxml", `<view />`)
	writeVerifyFixture(t, sourceDir, "pages/home/index.wxss", `./styles/order-common.wxss.page { color: red; }`)
	normalized := &pkg.NormalizedPackage{Manifest: pkg.ManifestIR{Pages: []string{"pages/home/index"}}}
	runner := &process.NodeRunner{Binary: "node", Timeout: 10 * time.Second, MemoryMB: 256}

	result, err := VerifyArtifacts(runner, normalized, sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	if result.ParserPassed || result.WXSSParseable != 0 || !result.CriticalFailure || result.Success {
		t.Fatalf("malformed raw selector must not receive a passing WXSS score: %#v", result)
	}
	if len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != "verify.wxss.unparsable" {
		t.Fatalf("expected an actionable WXSS parser diagnostic: %#v", result.Diagnostics)
	}
}

func TestAcceptanceSampleWhenConfigured(t *testing.T) {
	sourceDir := os.Getenv("SEEWX_ACCEPTANCE_SOURCE")
	if sourceDir == "" {
		t.Skip("SEEWX_ACCEPTANCE_SOURCE is not configured")
	}
	manifestData, err := os.ReadFile(filepath.Join(sourceDir, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest pkg.ManifestIR
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	normalized := &pkg.NormalizedPackage{Manifest: manifest}
	runner := &process.NodeRunner{Binary: "node", Timeout: 30 * time.Second, MemoryMB: 512}

	manifestResult, err := VerifyManifest(normalized, sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	artifactResult, err := VerifyArtifacts(runner, normalized, sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	score := ComputeRecoveryScore(manifestResult, artifactResult, true, true)
	t.Logf("manifestIssues=%d invalidTabBar=%d placeholders=%d placeholderFiles=%d unresolvedMarkers=%d unresolvedMarkerFiles=%d suspiciousBindings=%d dynamicBindings=%d dynamicFiles=%d qualityIssueFiles=%d score=%+v",
		manifestResult.ManifestIssueCount,
		len(manifestResult.InvalidTabBarPages),
		artifactResult.WXMLPlaceholderCount,
		artifactResult.WXMLPlaceholderFiles,
		artifactResult.WXMLUnresolvedMarkers,
		artifactResult.WXMLUnresolvedMarkerFiles,
		artifactResult.WXMLSuspiciousEventBindings,
		artifactResult.WXMLDynamicEventBindings,
		artifactResult.WXMLDynamicEventFiles,
		artifactResult.WXMLQualityIssueFiles,
		score,
	)

	if !manifestResult.Success || len(manifestResult.InvalidTabBarPages) != 0 || manifestResult.ManifestIssueCount != 0 {
		t.Fatalf("acceptance output still has manifest or tabBar defects: %#v", manifestResult)
	}
	if !artifactResult.ParserPassed || !artifactResult.VerifierPassed || !artifactResult.WXMLQualityPassed {
		t.Fatalf("acceptance output did not pass parser and WXML quality gates: %#v", artifactResult)
	}
	if artifactResult.WXMLPlaceholderCount != 0 || artifactResult.WXMLUnresolvedMarkers != 0 || artifactResult.WXMLSuspiciousEventBindings != 0 || artifactResult.WXMLQualityIssueFiles != 0 {
		t.Fatalf("acceptance output still contains recovery placeholders or suspicious event bindings: %#v", artifactResult)
	}
	if score.Manifest != 100 || score.WXML != 100 || !score.VerifierPassed || score.Overall < 90 {
		t.Fatalf("acceptance output static quality score is unexpectedly low: %#v", score)
	}
}

func writeWXMLQualityTree(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	writeVerifyFixture(t, root, "component/index.wxml", content)
	return root
}

func writeVerifyFixture(t *testing.T, root, relative, content string) {
	t.Helper()
	filePath := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
