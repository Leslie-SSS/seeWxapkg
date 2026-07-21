package process

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveExistingPathSearchesParentDirectories(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "internal", "beautify", "runtime", "verify_artifacts.js")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("console.log('ok')"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	nested := filepath.Join(root, "tests", "integration")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()
	if err := os.Chdir(nested); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	resolved, err := ResolveExistingPath(filepath.Join("internal", "beautify", "runtime", "verify_artifacts.js"))
	if err != nil {
		t.Fatalf("ResolveExistingPath returned error: %v", err)
	}
	targetInfo, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	resolvedInfo, err := os.Stat(resolved)
	if err != nil {
		t.Fatalf("stat resolved path: %v", err)
	}
	if !os.SameFile(targetInfo, resolvedInfo) {
		t.Fatalf("expected %s and %s to resolve to the same file", target, resolved)
	}
}

func TestBoundedTailBufferRetainsOnlyDiagnosticTail(t *testing.T) {
	buffer := newBoundedTailBuffer(8)
	if n, err := buffer.Write([]byte("012345")); err != nil || n != 6 {
		t.Fatalf("first write: n=%d err=%v", n, err)
	}
	if n, err := buffer.Write([]byte("6789abcdef")); err != nil || n != 10 {
		t.Fatalf("second write: n=%d err=%v", n, err)
	}
	if buffer.TotalBytes() != 16 {
		t.Fatalf("expected 16 total bytes, got %d", buffer.TotalBytes())
	}
	got := buffer.String()
	if !strings.Contains(got, "output truncated") || !strings.HasSuffix(got, "89abcdef") {
		t.Fatalf("unexpected bounded output: %q", got)
	}
}

func TestBoundedTailBufferHandlesSingleOversizedWrite(t *testing.T) {
	buffer := newBoundedTailBuffer(4)
	_, _ = buffer.Write([]byte("abcdefgh"))
	if got := buffer.String(); !strings.HasSuffix(got, "efgh") {
		t.Fatalf("expected final four bytes, got %q", got)
	}
}
