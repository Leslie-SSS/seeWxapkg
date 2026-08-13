package recover

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadFallbackStatusCompletedRequiresNoDiagnostics(t *testing.T) {
	outputDir := t.TempDir()
	writeFallbackTestFile(t, outputDir, fallbackStatusFilename, `{"status":"completed","diagnostics":[]}`)

	status, diagnostics := readFallbackStatus(outputDir)
	if status != "completed" || len(diagnostics) != 0 {
		t.Fatalf("unexpected completed status: status=%q diagnostics=%#v", status, diagnostics)
	}
	files := collectRecoveredFiles(outputDir, "fallback")
	if len(files) != 0 {
		t.Fatalf("status file leaked into deliverable artifacts: %#v", files)
	}
	if hasFallbackSourceArtifacts(files) {
		t.Fatal("status-only output must not count as a recovered source artifact")
	}
}

func TestReadFallbackStatusPropagatesPartialDiagnostics(t *testing.T) {
	outputDir := t.TempDir()
	writeFallbackTestFile(t, outputDir, fallbackStatusFilename, `{
		"status":"partial",
		"diagnostics":[{
			"code":"fallback.wxml.opcode_skipped",
			"level":"warning",
			"message":"non-static opcode",
			"status":"partial",
			"file":"pages/home/index.wxml",
			"metadata":{
				"count":16,
				"bindings":["bindtap"],
				"expressionSamples":["{{item.id}}"]
			}
		}]
	}`)
	writeFallbackTestFile(t, outputDir, "pages/home/index.wxml", `<view />`)

	status, diagnostics := readFallbackStatus(outputDir)
	if status != "partial" || len(diagnostics) != 1 {
		t.Fatalf("unexpected partial status: status=%q diagnostics=%#v", status, diagnostics)
	}
	if diagnostics[0].Code != "fallback.wxml.opcode_skipped" || diagnostics[0].File != "pages/home/index.wxml" {
		t.Fatalf("fallback diagnostic was not preserved: %#v", diagnostics[0])
	}
	if diagnostics[0].Metadata["count"] != float64(16) || diagnostics[0].Metadata["fallbackLevel"] != "warning" || diagnostics[0].Metadata["fallbackStatus"] != "partial" {
		t.Fatalf("fallback diagnostic metadata was not preserved: %#v", diagnostics[0].Metadata)
	}
	bindings, ok := diagnostics[0].Metadata["bindings"].([]interface{})
	if !ok || len(bindings) != 1 || bindings[0] != "bindtap" {
		t.Fatalf("fallback binding metadata was not preserved: %#v", diagnostics[0].Metadata)
	}
	files := collectRecoveredFiles(outputDir, "fallback")
	if !hasFallbackSourceArtifacts(files) {
		t.Fatalf("expected recovered WXML to count as an artifact: %#v", files)
	}
	for _, file := range files {
		if file.Path == fallbackStatusFilename {
			t.Fatal("status file leaked into recovered file list")
		}
	}
}

func TestReadFallbackStatusMissingDoesNotForcePartial(t *testing.T) {
	// wuWxapkg.js does not write the status file; absence of bookkeeping must
	// not downgrade an otherwise complete recovery. The warn diagnostic stays
	// so operators can see the status gap.
	status, diagnostics := readFallbackStatus(t.TempDir())
	if status != "completed" || len(diagnostics) != 1 || diagnostics[0].Code != "recover.fallback.status_missing" {
		t.Fatalf("missing status must default to completed with a warn: status=%q diagnostics=%#v", status, diagnostics)
	}
}

func TestCollectRecoveredFilesExcludesUnmergeableArtifacts(t *testing.T) {
	outputDir := t.TempDir()
	writeFallbackTestFile(t, outputDir, "pages/home/index.js", `Page({})`)
	writeFallbackTestFile(t, outputDir, "assets/logo.png", "not-a-source-file")
	writeFallbackTestFile(t, outputDir, "runtime/page-frame.html", "not-merged")

	files := collectRecoveredFiles(outputDir, "fallback")
	if len(files) != 1 || files[0].Path != "pages/home/index.js" {
		t.Fatalf("unexpected recovered files: %#v", files)
	}
}

func writeFallbackTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
