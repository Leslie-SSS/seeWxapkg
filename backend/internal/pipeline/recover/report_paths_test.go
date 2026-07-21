package recover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
)

func TestRecoverySubreportsPersistPortableReportPaths(t *testing.T) {
	outDir := t.TempDir()
	reportsDir := t.TempDir()
	writeRecoveryReportFixture(t, outDir, "pages/home/index.wxml", "<view />")
	writeRecoveryReportFixture(t, outDir, "app.wxss", "page {}")
	normalized := &pkg.NormalizedPackage{
		Pages: []pkg.PageIR{{Path: "pages/home/index", TemplatePath: "pages/home/index.wxml"}},
		Scripts: []pkg.ScriptIR{{
			Path:    "app.js",
			Content: "App({})",
			Source:  "native",
		}},
	}

	jsResult, err := RecoverJS(normalized, outDir, reportsDir)
	if err != nil {
		t.Fatal(err)
	}
	wxmlResult, err := RecoverWXML(normalized, outDir, reportsDir)
	if err != nil {
		t.Fatal(err)
	}
	wxssResult, err := RecoverWXSS(normalized, outDir, reportsDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, reportPath := range []string{jsResult.ReportPath, wxmlResult.ReportPath, wxssResult.ReportPath} {
		data, err := os.ReadFile(reportPath)
		if err != nil {
			t.Fatal(err)
		}
		var persisted struct {
			ReportPath string `json:"reportPath"`
		}
		if err := json.Unmarshal(data, &persisted); err != nil {
			t.Fatal(err)
		}
		want := filepath.ToSlash(filepath.Join("reports", filepath.Base(reportPath)))
		if persisted.ReportPath != want || filepath.IsAbs(filepath.FromSlash(persisted.ReportPath)) {
			t.Fatalf("persisted reportPath = %q, want portable path %q", persisted.ReportPath, want)
		}
	}
}

func writeRecoveryReportFixture(t *testing.T, root, relative, content string) {
	t.Helper()
	filePath := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
