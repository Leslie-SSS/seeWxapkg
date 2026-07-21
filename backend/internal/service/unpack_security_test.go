package service

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keepbuild/seewxapkg/internal/model"
	"github.com/keepbuild/seewxapkg/tests/testutil"
)

func TestUnpackRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "result")
	data := testutil.MustBuildWxapkg(map[string]string{"../escape.js": "bad"})

	if _, err := UnpackWxapkg(data, output, false); err == nil || !strings.Contains(err.Error(), "escapes output directory") {
		t.Fatalf("expected path traversal error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "escape.js")); !os.IsNotExist(err) {
		t.Fatalf("escaped file was created, stat error=%v", err)
	}
}

func TestUnpackRejectsWindowsStyleTraversalOnEveryHost(t *testing.T) {
	output := t.TempDir()
	data := testutil.MustBuildWxapkg(map[string]string{`..\..\escape.js`: "bad"})

	if _, err := UnpackWxapkg(data, output, false); err == nil || !strings.Contains(err.Error(), "invalid file path") {
		t.Fatalf("expected Windows-style traversal error, got %v", err)
	}
}

func TestUnpackAcceptsPackageRootPath(t *testing.T) {
	output := t.TempDir()
	data := testutil.MustBuildWxapkg(map[string]string{"/app.js": "App({})"})

	if _, err := UnpackWxapkg(data, output, false); err != nil {
		t.Fatalf("UnpackWxapkg returned error: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(output, "app.js")); err != nil || string(got) != "App({})" {
		t.Fatalf("unexpected extracted file: content=%q err=%v", got, err)
	}
}

func TestUnpackFileCountIncludesNestedFiles(t *testing.T) {
	output := t.TempDir()
	data := testutil.MustBuildWxapkg(map[string]string{
		"app.js":                  "App({})",
		"pages/home/index.js":     "Page({})",
		"components/card/card.js": "Component({})",
	})

	result, err := UnpackWxapkg(data, output, false)
	if err != nil {
		t.Fatalf("UnpackWxapkg returned error: %v", err)
	}
	if result.FileCount != 3 {
		t.Fatalf("FileCount = %d, want 3", result.FileCount)
	}
}

func TestUnpackRejectsExcessiveFileCountBeforeAllocation(t *testing.T) {
	buf := &bytes.Buffer{}
	buf.WriteByte(0xBE)
	_ = binary.Write(buf, binary.BigEndian, uint32(0))
	_ = binary.Write(buf, binary.BigEndian, uint32(4))
	_ = binary.Write(buf, binary.BigEndian, uint32(0))
	buf.WriteByte(0xED)
	_ = binary.Write(buf, binary.BigEndian, uint32(maxWxapkgFiles+1))

	if _, err := UnpackWxapkg(buf.Bytes(), t.TempDir(), false); err == nil || !strings.Contains(err.Error(), "too many files") {
		t.Fatalf("expected file-count limit error, got %v", err)
	}
}

func TestUnpackRejectsMalformedHeaderAndDeclaredSections(t *testing.T) {
	t.Run("short header", func(t *testing.T) {
		if _, err := UnpackWxapkg([]byte("bad"), t.TempDir(), false); err == nil || !strings.Contains(err.Error(), "file too small") {
			t.Fatalf("expected short header error, got %v", err)
		}
	})

	t.Run("section beyond package", func(t *testing.T) {
		buf := &bytes.Buffer{}
		buf.WriteByte(0xBE)
		_ = binary.Write(buf, binary.BigEndian, uint32(0))
		_ = binary.Write(buf, binary.BigEndian, uint32(64))
		_ = binary.Write(buf, binary.BigEndian, uint32(0))
		buf.WriteByte(0xED)

		if _, err := UnpackWxapkg(buf.Bytes(), t.TempDir(), false); err == nil || !strings.Contains(err.Error(), "sections out of bounds") {
			t.Fatalf("expected declared-section bounds error, got %v", err)
		}
	})
}

func TestUnpackRejectsInvalidFileNameMetadataBeforeAllocation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		nameLength  uint32
		wantMessage string
	}{
		{name: "oversized name", nameLength: maxWxapkgNameSize + 1, wantMessage: "invalid file name length"},
		{name: "entry crosses index", nameLength: 1, wantMessage: "exceeds declared index section"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			buf.WriteByte(0xBE)
			_ = binary.Write(buf, binary.BigEndian, uint32(0))
			_ = binary.Write(buf, binary.BigEndian, uint32(16))
			_ = binary.Write(buf, binary.BigEndian, uint32(0))
			buf.WriteByte(0xED)
			_ = binary.Write(buf, binary.BigEndian, uint32(1))
			_ = binary.Write(buf, binary.BigEndian, tc.nameLength)
			buf.Write(make([]byte, 8))

			if _, err := UnpackWxapkg(buf.Bytes(), t.TempDir(), false); err == nil || !strings.Contains(err.Error(), tc.wantMessage) {
				t.Fatalf("expected %q error, got %v", tc.wantMessage, err)
			}
		})
	}
}

func TestUnpackRejectsDeclaredExtractionAmplification(t *testing.T) {
	data := testutil.MustBuildWxapkg(map[string]string{"a.js": "a", "b.js": "b"})
	cursor := 18 // 14-byte header followed by the 4-byte file count.
	entrySize := uint32(len(data)/2 + 1)
	for range 2 {
		nameLength := int(binary.BigEndian.Uint32(data[cursor : cursor+4]))
		offsetPos := cursor + 4 + nameLength
		binary.BigEndian.PutUint32(data[offsetPos:offsetPos+4], 0)
		binary.BigEndian.PutUint32(data[offsetPos+4:offsetPos+8], entrySize)
		cursor = offsetPos + 8
	}

	if _, err := UnpackWxapkg(data, t.TempDir(), false); err == nil || !strings.Contains(err.Error(), "declared extracted data exceeds package size") {
		t.Fatalf("expected extraction amplification error, got %v", err)
	}
}

func TestUnpackRejectsDuplicateNormalizedPathBeforeWriting(t *testing.T) {
	output := t.TempDir()
	data := testutil.MustBuildWxapkg(map[string]string{
		"pages/a.js":      "a",
		"pages/x/../a.js": "b",
	})

	if _, err := UnpackWxapkg(data, output, false); err == nil || !strings.Contains(err.Error(), "duplicate output path") {
		t.Fatalf("expected duplicate normalized path error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(output, "pages", "a.js")); !os.IsNotExist(err) {
		t.Fatalf("duplicate validation must happen before extraction, stat error=%v", err)
	}
}

func TestExtractFileRejectsUint32Overflow(t *testing.T) {
	entry := model.FileEntry{Name: "app.js", Offset: math.MaxUint32, Size: 2}
	if err := extractFile([]byte("small"), entry, t.TempDir(), false); err == nil || !strings.Contains(err.Error(), "out of bounds") {
		t.Fatalf("expected bounds error, got %v", err)
	}
}

func TestInitBeautifyServiceReturnsStartupFailureWhenEnabled(t *testing.T) {
	StopBeautifyService()
	t.Setenv("PATH", "")
	if err := InitBeautifyService(true, 1, 1024, 1, false); err == nil {
		t.Fatal("expected enabled beautify service initialization to fail without Node.js")
	}
	if beautifyService != nil {
		t.Fatal("failed initialization must not leave a service instance")
	}
}
