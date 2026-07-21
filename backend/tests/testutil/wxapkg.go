package testutil

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"path/filepath"
	"sort"
)

func BuildWxapkg(files map[string]string) ([]byte, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, filepath.ToSlash(name))
	}
	sort.Strings(names)

	type entry struct {
		name    string
		content []byte
		offset  uint32
	}

	entries := make([]entry, 0, len(names))
	indexBuf := &bytes.Buffer{}
	if err := binary.Write(indexBuf, binary.BigEndian, uint32(len(names))); err != nil {
		return nil, err
	}

	for _, name := range names {
		content := []byte(files[name])
		entries = append(entries, entry{name: name, content: content})
		if err := binary.Write(indexBuf, binary.BigEndian, uint32(len(name))); err != nil {
			return nil, err
		}
		if _, err := indexBuf.WriteString(name); err != nil {
			return nil, err
		}
		if err := binary.Write(indexBuf, binary.BigEndian, uint32(0)); err != nil {
			return nil, err
		}
		if err := binary.Write(indexBuf, binary.BigEndian, uint32(len(content))); err != nil {
			return nil, err
		}
	}

	indexData := indexBuf.Bytes()
	headerLen := 14
	bodyOffset := uint32(headerLen + len(indexData))
	body := &bytes.Buffer{}
	cursor := bodyOffset

	fixedIndex := &bytes.Buffer{}
	if err := binary.Write(fixedIndex, binary.BigEndian, uint32(len(names))); err != nil {
		return nil, err
	}
	for _, item := range entries {
		item.offset = cursor
		cursor += uint32(len(item.content))
		if err := binary.Write(fixedIndex, binary.BigEndian, uint32(len(item.name))); err != nil {
			return nil, err
		}
		if _, err := fixedIndex.WriteString(item.name); err != nil {
			return nil, err
		}
		if err := binary.Write(fixedIndex, binary.BigEndian, item.offset); err != nil {
			return nil, err
		}
		if err := binary.Write(fixedIndex, binary.BigEndian, uint32(len(item.content))); err != nil {
			return nil, err
		}
		if _, err := body.Write(item.content); err != nil {
			return nil, err
		}
	}

	out := &bytes.Buffer{}
	out.WriteByte(0xBE)
	if err := binary.Write(out, binary.BigEndian, uint32(0)); err != nil {
		return nil, err
	}
	if err := binary.Write(out, binary.BigEndian, uint32(fixedIndex.Len())); err != nil {
		return nil, err
	}
	if err := binary.Write(out, binary.BigEndian, uint32(body.Len())); err != nil {
		return nil, err
	}
	out.WriteByte(0xED)
	if _, err := out.Write(fixedIndex.Bytes()); err != nil {
		return nil, err
	}
	if _, err := out.Write(body.Bytes()); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func BuildMultipartCompileRequest(filename string, data []byte, fields map[string]string) (*httptest.ResponseRecorder, *bytes.Buffer, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, nil, "", err
	}
	if _, err := part.Write(data); err != nil {
		return nil, nil, "", err
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, nil, "", err
	}

	return httptest.NewRecorder(), body, writer.FormDataContentType(), nil
}

func MustBuildWxapkg(files map[string]string) []byte {
	data, err := BuildWxapkg(files)
	if err != nil {
		panic(fmt.Sprintf("build wxapkg: %v", err))
	}
	return data
}
