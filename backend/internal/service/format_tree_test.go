package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatSourceTreeFormatsJSONAndReportsUnavailableEngine(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "app.json"), []byte(`{"pages":["pages/index"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte(`App({onLaunch(){}})`), 0644); err != nil {
		t.Fatal(err)
	}

	previous := beautifyService
	beautifyService = nil
	t.Cleanup(func() { beautifyService = previous })

	result, err := FormatSourceTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Formatted != 1 || result.Skipped != 1 || result.Partial {
		t.Fatalf("skipped files preserve original content and must not mark the stage partial: %+v", result)
	}
	formatted, err := os.ReadFile(filepath.Join(root, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(formatted), "\n") {
		t.Fatalf("expected formatted JSON, got %q", formatted)
	}
}

func TestFormatSourceTreePreservesInvalidJSON(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "broken.json")
	original := []byte(`{"broken":`)
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}

	result, err := FormatSourceTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed != 1 || !result.Partial {
		t.Fatalf("expected one preserved failure, got %+v", result)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("invalid JSON changed: %q", got)
	}
}
