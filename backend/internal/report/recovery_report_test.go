package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
	"github.com/keepbuild/seewxapkg/internal/domain/task"
)

func TestRecoveryReportSanitizesInternalPaths(t *testing.T) {
	diagnostic := pkg.Warn("test.path", "failed at /data/tasks/task-1/fallback/input/page-frame.html", "verifying", "/data/tasks/task-1/result/src/pages/home/index.wxml")
	diagnostic.Metadata = map[string]interface{}{
		"path":   "/data/tasks/task-1/fallback/input/page-frame.html",
		"nested": map[string]interface{}{"output": "/data/output/task-1.zip"},
	}
	current := &task.Task{
		ID:          "task-1",
		Status:      task.TaskPartial,
		Diagnostics: []pkg.Diagnostic{diagnostic},
		StageResults: []task.StageResult{{
			Stage:           "packaging",
			Engine:          "/data/private/engine",
			SourceBreakdown: map[string]int{"native": 2, "/data/private/source": 1},
			Diagnostics:     []pkg.Diagnostic{diagnostic},
			Metrics: map[string]interface{}{
				"zipPath":       "/data/output/task-1.zip",
				"archiveSize":   1234,
				"archiveRoot":   "src/",
				"downloadReady": true,
				"fileCount":     -1,
				"generated":     0,
				"isEncrypted":   map[string]string{"/data/private": "true"},
				"mode":          "/data/private",
				"sources":       map[string]string{"/Users/person": "native"},
				"unknown":       "implementation-detail",
			},
		}},
		ArtifactSummary: &task.ArtifactSummary{
			DownloadReady: true,
			ArchiveSize:   1234,
			ZipPath:       "/data/output/task-1.zip",
		},
	}

	report := BuildRecoveryReport(current)
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(data)
	for _, secret := range []string{"/data/tasks/", "/data/output/"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("report exposed internal path %q: %s", secret, serialized)
		}
	}
	if report.Diagnostics[0].File != "src/pages/home/index.wxml" {
		t.Fatalf("diagnostic path was not made useful and relative: %#v", report.Diagnostics[0])
	}
	if report.Packaging.Status != "ready" || !report.Packaging.DownloadReady || report.Packaging.ArchiveSize != 1234 {
		t.Fatalf("live packaging metadata is inconsistent: %#v", report.Packaging)
	}
	if report.Packaging.ZipManifest != "report?name=zip-manifest" {
		t.Fatalf("live report exposes a misleading zip manifest reference: %#v", report.Packaging)
	}
	metrics := report.StageResults[0].Metrics
	if _, ok := metrics["zipPath"]; ok {
		t.Fatalf("stage metrics exposed legacy zipPath: %#v", metrics)
	}
	if _, ok := metrics["unknown"]; ok {
		t.Fatalf("stage metrics exposed an undocumented key: %#v", metrics)
	}
	if _, ok := metrics["downloadReady"]; ok {
		t.Fatalf("stage metrics exposed readiness before the terminal task state: %#v", metrics)
	}
	if _, ok := metrics["generated"]; ok {
		t.Fatalf("stage metrics exposed an unmeasured generated count: %#v", metrics)
	}
	if metrics["archiveRoot"] != "src/" {
		t.Fatalf("stage metrics lost the fixed public archive root: %#v", metrics)
	}
	if _, ok := metrics["isEncrypted"]; ok {
		t.Fatalf("stage metrics accepted an invalid boolean value: %#v", metrics)
	}
	if _, ok := metrics["mode"]; ok {
		t.Fatalf("stage metrics accepted an invalid mode: %#v", metrics)
	}
	if _, ok := metrics["fileCount"]; ok {
		t.Fatalf("stage metrics accepted a negative count: %#v", metrics)
	}
	if _, ok := metrics["sources"]; ok {
		t.Fatalf("stage metrics exposed a free-form source map: %#v", metrics)
	}
	if metrics["archiveSize"] != 1234 {
		t.Fatalf("stage metrics lost a public measurement: %#v", metrics)
	}
	if report.StageResults[0].Engine != "" {
		t.Fatalf("stage exposed an unknown engine: %#v", report.StageResults[0])
	}
	if report.StageResults[0].SourceBreakdown["native"] != 2 || report.StageResults[0].SourceBreakdown["other"] != 1 {
		t.Fatalf("stage source labels were not reduced to the public vocabulary: %#v", report.StageResults[0].SourceBreakdown)
	}
}

func TestWriteDiagnosticsPersistsOnlySanitizedPaths(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "diagnostics.json")
	items := []pkg.Diagnostic{pkg.Warn("test", "warning", "verifying", "/Users/person/private/result/src/app.js")}
	if err := WriteDiagnostics(filePath, items); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "/Users/person/") || !strings.Contains(string(data), `"file": "src/app.js"`) {
		t.Fatalf("diagnostics path was not sanitized: %s", data)
	}
}

func TestSanitizeTextRemovesInternalPathsWithoutMutatingInput(t *testing.T) {
	original := `open /data/tasks/task-1/input.wxapkg; write /data/output/task-1.zip; inspect /Users/person/project/app.js; load /app/runtime/server.js; mount /srv/service/config.yaml; volume /mnt/private/cache.db; workspace /workspace/build/main.go; windows C:\Users\person\private\app.js; forward D:/workspace/private/config.json; network \\build-host\private-share\release\app.exe`
	safe := SanitizeText(original)
	for _, secret := range []string{"/data/tasks", "/data/output", "/Users", "/app/", "/srv/", "/mnt/", "/workspace/", `C:\Users`, `D:/workspace`, `\\build-host\private-share`} {
		if strings.Contains(safe, secret) {
			t.Fatalf("public text exposed %q: %s", secret, safe)
		}
	}
	for _, usefulName := range []string{"input.wxapkg", "task-1.zip", "server.js", "config.yaml", "cache.db", "main.go", "app.js", "config.json", "app.exe"} {
		if !strings.Contains(safe, usefulName) {
			t.Fatalf("public text lost useful filename %q: %s", usefulName, safe)
		}
	}
	if original != `open /data/tasks/task-1/input.wxapkg; write /data/output/task-1.zip; inspect /Users/person/project/app.js; load /app/runtime/server.js; mount /srv/service/config.yaml; volume /mnt/private/cache.db; workspace /workspace/build/main.go; windows C:\Users\person\private\app.js; forward D:/workspace/private/config.json; network \\build-host\private-share\release\app.exe` {
		t.Fatalf("sanitizing mutated the original text: %s", original)
	}
}

func TestSanitizeTextPreservesPublicURLsAndSimilarRouteNames(t *testing.T) {
	original := "参考 https://docs.example.com/app/guide?next=/workspace/demo；状态 wss://events.example.com/srv/live；路由 /application/status 和 /workspace-name/info"
	if safe := SanitizeText(original); safe != original {
		t.Fatalf("sanitizer damaged public URLs or non-internal route names:\nwant: %s\n got: %s", original, safe)
	}
}

func TestSanitizeJSONBytesPreservesSafeRelativeFileLists(t *testing.T) {
	original := []byte(`{"taskId":"task-1","files":["src/pages/home/index.js","src/space name.js","../private.txt","C:/Users/alice/drive-secret.js","\\\\build-host\\private-share\\unc-secret.js","file:///Users/alice/file-secret.js"],"internal":"/home/person/private.txt","stages":[{"metrics":{"fileCount":2,"zipPath":"/data/output/task-1.zip","unknown":"private"}}]}`)
	safe, err := SanitizeJSONBytes(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Files    []string `json:"files"`
		Internal string   `json:"internal"`
		Stages   []struct {
			Metrics map[string]interface{} `json:"metrics"`
		} `json:"stages"`
	}
	if err := json.Unmarshal(safe, &decoded); err != nil {
		t.Fatal(err)
	}
	wantFiles := []string{"src/pages/home/index.js", "src/space name.js", "private.txt", "drive-secret.js", "unc-secret.js", "file-secret.js"}
	if len(decoded.Files) != len(wantFiles) {
		t.Fatalf("files = %#v, want %#v", decoded.Files, wantFiles)
	}
	for index := range wantFiles {
		if decoded.Files[index] != wantFiles[index] {
			t.Fatalf("files = %#v, want %#v", decoded.Files, wantFiles)
		}
	}
	if strings.Contains(decoded.Internal, "/home/") {
		t.Fatalf("internal path was not sanitized: %q", decoded.Internal)
	}
	if len(decoded.Stages) != 1 || decoded.Stages[0].Metrics["fileCount"] != float64(2) {
		t.Fatalf("public stage metric was not preserved: %#v", decoded.Stages)
	}
	if _, ok := decoded.Stages[0].Metrics["zipPath"]; ok {
		t.Fatalf("legacy zipPath metric was exposed: %#v", decoded.Stages[0].Metrics)
	}
	if _, ok := decoded.Stages[0].Metrics["unknown"]; ok {
		t.Fatalf("undocumented metric was exposed: %#v", decoded.Stages[0].Metrics)
	}
}

func TestStructuredPathSanitizationHandlesWindowsUNCAndFileURLs(t *testing.T) {
	diagnostics := SanitizeDiagnostics([]pkg.Diagnostic{
		pkg.Warn("drive", "warning", "stage", `C:\Users\alice\private\drive.js`),
		pkg.Warn("unc", "warning", "stage", `\\build-host\private-share\unc.js`),
		pkg.Warn("file-uri", "warning", "stage", `file:///Users/alice/private/file-uri.js`),
	})
	want := []string{"drive.js", "unc.js", "file-uri.js"}
	for index, item := range diagnostics {
		if item.File != want[index] {
			t.Fatalf("diagnostic %d file = %q, want %q", index, item.File, want[index])
		}
	}

	summary := SanitizeArtifactSummary(&task.ArtifactSummary{Files: []task.ArtifactFile{
		{Path: `C:\Users\alice\private\artifact.js`},
		{Path: `\\build-host\private-share\artifact-unc.js`},
		{Path: `file:///Users/alice/private/artifact-uri.js`},
	}})
	for index, expected := range []string{"artifact.js", "artifact-unc.js", "artifact-uri.js"} {
		if summary.Files[index].Path != expected {
			t.Fatalf("artifact %d path = %q, want %q", index, summary.Files[index].Path, expected)
		}
	}

	text := SanitizeText(`open file:///Users/alice/private/input.wxapkg; inspect C:\Users\alice\private\app.js`)
	for _, secret := range []string{"file:///Users", `C:\Users`, "alice/private"} {
		if strings.Contains(text, secret) {
			t.Fatalf("free text exposed %q: %s", secret, text)
		}
	}
}
