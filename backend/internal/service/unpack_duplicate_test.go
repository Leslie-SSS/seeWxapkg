package service

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/keepbuild/seewxapkg/internal/model"
)

// buildDuplicatedWxapkg builds a wxapkg buffer whose index may contain
// repeated names (map-based test helpers cannot express duplicates).
func buildDuplicatedWxapkg(t *testing.T, names []string, contents [][]byte) []byte {
	t.Helper()
	indexLen := 4
	for _, name := range names {
		indexLen += 4 + len(name) + 8
	}
	dataStart := uint32(14 + indexLen)
	var index bytes.Buffer
	_ = binary.Write(&index, binary.BigEndian, uint32(len(names)))
	var body bytes.Buffer
	offsets := make([]uint32, len(names))
	sizes := make([]uint32, len(names))
	var cursor uint32
	for i := range names {
		offsets[i] = dataStart + cursor
		sizes[i] = uint32(len(contents[i]))
		cursor += uint32(len(contents[i]))
	}
	for i, name := range names {
		_ = binary.Write(&index, binary.BigEndian, uint32(len(name)))
		index.WriteString(name)
		_ = binary.Write(&index, binary.BigEndian, offsets[i])
		_ = binary.Write(&index, binary.BigEndian, sizes[i])
	}
	for _, content := range contents {
		body.Write(content)
	}
	bodyLen := uint32(body.Len())
	var header bytes.Buffer
	header.WriteByte(0xBE)
	_ = binary.Write(&header, binary.BigEndian, uint32(0))
	_ = binary.Write(&header, binary.BigEndian, uint32(indexLen))
	_ = binary.Write(&header, binary.BigEndian, bodyLen)
	header.WriteByte(0xED)
	out := append(header.Bytes(), index.Bytes()...)
	out = append(out, body.Bytes()...)
	return out
}

func buildSharedRuntimeAliasWxapkg(t *testing.T) []byte {
	t.Helper()
	names := []string{"app.json", "appservice.app.js", "__extended__/wx1234567890abcdef/appservice.js"}
	appConfig := []byte(`{"pages":[]}`)
	appService := bytes.Repeat([]byte("x"), 64)
	indexLen := 4
	for _, name := range names {
		indexLen += 4 + len(name) + 8
	}
	dataStart := uint32(14 + indexLen)
	aliasOffset := dataStart + uint32(len(appConfig))

	var index bytes.Buffer
	_ = binary.Write(&index, binary.BigEndian, uint32(len(names)))
	writeEntry := func(name string, offset, size uint32) {
		_ = binary.Write(&index, binary.BigEndian, uint32(len(name)))
		index.WriteString(name)
		_ = binary.Write(&index, binary.BigEndian, offset)
		_ = binary.Write(&index, binary.BigEndian, size)
	}
	writeEntry(names[0], dataStart, uint32(len(appConfig)))
	writeEntry(names[1], aliasOffset, uint32(len(appService)))
	writeEntry(names[2], aliasOffset, uint32(len(appService)))

	body := append(append([]byte{}, appConfig...), appService...)
	var header bytes.Buffer
	header.WriteByte(0xBE)
	_ = binary.Write(&header, binary.BigEndian, uint32(0))
	_ = binary.Write(&header, binary.BigEndian, uint32(indexLen))
	_ = binary.Write(&header, binary.BigEndian, uint32(len(body)))
	header.WriteByte(0xED)
	return append(append(header.Bytes(), index.Bytes()...), body...)
}

func TestSameFileData(t *testing.T) {
	data := []byte("0123456701234567")
	a := model.FileEntry{Offset: 0, Size: 4, Name: "a"}
	b := model.FileEntry{Offset: 8, Size: 4, Name: "b"}
	if !sameFileData(data, a, b) {
		t.Fatal("same content at different offsets must be equal")
	}
	diff := model.FileEntry{Offset: 4, Size: 4, Name: "d"}
	if sameFileData(data, a, diff) {
		t.Fatal("differing content must not be equal")
	}
	c := model.FileEntry{Offset: 0, Size: 5, Name: "c"}
	if sameFileData(data, a, c) {
		t.Fatal("different sizes must not be equal")
	}
}

func TestUnpackToleratesIdenticalDuplicatePaths(t *testing.T) {
	payload := []byte(`{"allExtendedComponents":["miniprogram_npm/weui-miniprogram"]}`)
	data := buildDuplicatedWxapkg(t,
		[]string{"app.json", "__extended__/wx1234567890abcdef/plugin.json", "__extended__/wx1234567890abcdef/plugin.json"},
		[][]byte{[]byte(`{"pages":[]}`), payload, payload})
	outDir := t.TempDir()
	result, err := UnpackWxapkg(data, outDir, false)
	if err != nil {
		t.Fatalf("identical duplicate must be tolerated: %v", err)
	}
	if result.FileCount != 3 {
		t.Fatalf("expected 3 entries, got %d", result.FileCount)
	}
	out, err := os.ReadFile(filepath.Join(outDir, "__extended__", "wx1234567890abcdef", "plugin.json"))
	if err != nil {
		t.Fatalf("plugin.json must be written: %v", err)
	}
	if !bytes.Equal(out, payload) {
		t.Fatalf("plugin.json content mismatch")
	}
}

func TestUnpackToleratesSharedWeChatRuntimeAlias(t *testing.T) {
	data := buildSharedRuntimeAliasWxapkg(t)
	outDir := t.TempDir()
	result, err := UnpackWxapkg(data, outDir, false)
	if err != nil {
		t.Fatalf("shared runtime alias must be tolerated: %v", err)
	}
	if result.FileCount != 3 {
		t.Fatalf("expected 3 entries, got %d", result.FileCount)
	}
	alias, err := os.ReadFile(filepath.Join(outDir, "__extended__", "wx1234567890abcdef", "appservice.js"))
	if err != nil {
		t.Fatalf("runtime alias must be written: %v", err)
	}
	if !bytes.Equal(alias, bytes.Repeat([]byte("x"), 64)) {
		t.Fatalf("runtime alias content mismatch")
	}
}

func TestUnpackRejectsDivergentDuplicatePaths(t *testing.T) {
	data := buildDuplicatedWxapkg(t,
		[]string{"app.json", "dup.json", "dup.json"},
		[][]byte{[]byte(`{"pages":[]}`), []byte(`{"a":1}`), []byte(`{"a":2}`)})
	_, err := UnpackWxapkg(data, t.TempDir(), false)
	if err == nil {
		t.Fatal("divergent duplicate must be rejected")
	}
}
