package normalize

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectRawArtifactsDoesNotLoadBinaryAssets(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "app.json"), []byte(`{"pages":["pages/home/index"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	asset := []byte("binary-looking-prefix-without-a-nul")
	if err := os.WriteFile(filepath.Join(root, "large-image.png"), asset, 0644); err != nil {
		t.Fatal(err)
	}

	raw, err := collectRawArtifacts(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, loaded := raw.FileContents["large-image.png"]; loaded {
		t.Fatal("binary asset must not be loaded into FileContents")
	}
	if _, loaded := raw.FileContents["app.json"]; !loaded {
		t.Fatal("text artifact should be loaded")
	}
}
