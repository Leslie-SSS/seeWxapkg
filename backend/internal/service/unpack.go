package service

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/keepbuild/seewxapkg/internal/model"
	"github.com/tidwall/pretty"
)

// UnpackResult 解包结果
type UnpackResult struct {
	Files     []model.FileEntry
	FileCount int
	Success   bool
	Error     error
}

// UnpackWxapkg 解包 wxapkg 文件
// 完全按照 Java 代码逻辑实现
func UnpackWxapkg(data []byte, outputDir string, beautify bool) (*UnpackResult, error) {
	result := &UnpackResult{
		Files: make([]model.FileEntry, 0),
	}

	log.Printf("[Unpack] Starting unpack, data size: %d bytes", len(data))

	// 创建输出目录
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	// Java: ByteBuffer buffer = ByteBuffer.wrap(data).order(ByteOrder.BIG_ENDIAN);
	// Java: byte firstMark = buffer.get();
	// Java: if (firstMark != (byte) 0xBE) { throw ... }
	if len(data) < 14 {
		return nil, fmt.Errorf("file too small: %d bytes", len(data))
	}

	firstMark := data[0]
	log.Printf("[Unpack] First byte: 0x%02X", firstMark)

	if firstMark != 0xBE {
		// 检查是否是加密文件
		if len(data) >= 6 {
			header := string(data[:6])
			log.Printf("[Unpack] First 6 bytes: %s", header)
		}
		if len(data) >= 6 && string(data[:6]) == "V1MMWX" {
			return nil, fmt.Errorf("文件是加密格式（V1MMWX），需要提供正确的 AppID 进行解密")
		}
		return nil, fmt.Errorf("无效的 wxapkg 文件：首标记错误（期望 0xBE，实际 0x%02X）", firstMark)
	}

	// 使用 bytes.Reader 按照大端序读取
	reader := bytes.NewReader(data)
	reader.Seek(1, io.SeekStart) // 跳过 firstMark

	// Java: int info1 = buffer.getInt();
	var info1 uint32
	if err := binary.Read(reader, binary.BigEndian, &info1); err != nil {
		return nil, fmt.Errorf("read info1: %w", err)
	}

	// Java: int indexInfoLength = buffer.getInt();
	var indexInfoLength uint32
	if err := binary.Read(reader, binary.BigEndian, &indexInfoLength); err != nil {
		return nil, fmt.Errorf("read indexInfoLength: %w", err)
	}

	// Java: int bodyInfoLength = buffer.getInt();
	var bodyInfoLength uint32
	if err := binary.Read(reader, binary.BigEndian, &bodyInfoLength); err != nil {
		return nil, fmt.Errorf("read bodyInfoLength: %w", err)
	}

	// Java: byte lastMark = buffer.get();
	var lastMark uint8
	if err := binary.Read(reader, binary.BigEndian, &lastMark); err != nil {
		return nil, fmt.Errorf("read lastMark: %w", err)
	}

	log.Printf("[Unpack] File header: info1=%d, indexLen=%d, bodyLen=%d, lastMark=0x%02X",
		info1, indexInfoLength, bodyInfoLength, lastMark)

	// Java: if (lastMark != (byte) 0xED) { throw ... }
	if lastMark != 0xED {
		return nil, fmt.Errorf("无效的 wxapkg 文件：尾标记错误（期望 0xED，实际 0x%02X）", lastMark)
	}

	// Java: int fileCount = buffer.getInt();
	var fileCount uint32
	if err := binary.Read(reader, binary.BigEndian, &fileCount); err != nil {
		return nil, fmt.Errorf("read fileCount: %w", err)
	}

	log.Printf("[Unpack] File count: %d", fileCount)

	// Java: for (int i = 0; i < fileCount; i++) {
	files := make([]model.FileEntry, fileCount)
	for i := uint32(0); i < fileCount; i++ {
		// Java: int nameLen = buffer.getInt();
		var nameLen uint32
		if err := binary.Read(reader, binary.BigEndian, &nameLen); err != nil {
			return nil, fmt.Errorf("read nameLen: %w", err)
		}

		// Java: byte[] nameBytes = new byte[nameLen];
		// Java: buffer.get(nameBytes);
		nameBytes := make([]byte, nameLen)
		if _, err := io.ReadFull(reader, nameBytes); err != nil {
			return nil, fmt.Errorf("read file name: %w", err)
		}
		files[i].Name = string(nameBytes)

		// Java: file.offset = buffer.getInt();
		if err := binary.Read(reader, binary.BigEndian, &files[i].Offset); err != nil {
			return nil, fmt.Errorf("read file offset: %w", err)
		}

		// Java: file.size = buffer.getInt();
		if err := binary.Read(reader, binary.BigEndian, &files[i].Size); err != nil {
			return nil, fmt.Errorf("read file size: %w", err)
		}
	}

	// 并发提取文件
	var wg sync.WaitGroup
	var mu sync.Mutex
	var extractErr error
	maxWorkers := 10
	semaphore := make(chan struct{}, maxWorkers)

	for _, file := range files {
		wg.Add(1)
		go func(f model.FileEntry) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if err := extractFile(data, f, outputDir, beautify); err != nil {
				mu.Lock()
				if extractErr == nil {
					extractErr = err
				}
				mu.Unlock()
			}
		}(file)
	}

	wg.Wait()

	if extractErr != nil {
		return nil, extractErr
	}

	result.Files = files
	result.FileCount = len(files)
	result.Success = true

	return result, nil
}

// extractFile 提取单个文件
func extractFile(data []byte, file model.FileEntry, outputDir string, beautify bool) error {
	// 检查边界
	if file.Offset+file.Size > uint32(len(data)) {
		return fmt.Errorf("file out of bounds: %s (offset=%d, size=%d, dataLen=%d)",
			file.Name, file.Offset, file.Size, len(data))
	}

	// 读取文件内容
	content := make([]byte, file.Size)
	copy(content, data[file.Offset:file.Offset+file.Size])

	// 美化代码
	if beautify {
		content = beautifyContent(content, file.Name)
	}

	// 创建完整路径
	fullPath := filepath.Join(outputDir, file.Name)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// 写入文件
	if err := os.WriteFile(fullPath, content, 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// beautifyContent 美化内容
func beautifyContent(content []byte, filename string) []byte {
	ext := strings.ToLower(filepath.Ext(filename))

	// 检查是否为文本文件
	if !isTextFile(content) {
		return content
	}

	switch ext {
	case ".json":
		return beautifyJSON(content)
	case ".js":
		// 简单的 JS 格式化
		return content
	case ".wxml", ".html":
		return beautifyHTML(content)
	default:
		return content
	}
}

// isTextFile 检查是否为文本文件
func isTextFile(data []byte) bool {
	if len(data) == 0 {
		return true
	}

	// 检查前 512 字节
	checkLen := len(data)
	if checkLen > 512 {
		checkLen = 512
	}

	for i := 0; i < checkLen; i++ {
		b := data[i]
		// 允许的文本字符
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' {
			return false
		}
	}

	return true
}

// beautifyJSON 美化 JSON
func beautifyJSON(data []byte) []byte {
	formatted := pretty.PrettyOptions(data, &pretty.Options{
		SortKeys: false,
		Indent:   "  ",
		Width:    80,
	})
	return formatted
}

// beautifyHTML 美化 HTML
func beautifyHTML(data []byte) []byte {
	// 简单的 HTML 格式化
	s := string(data)
	s = strings.ReplaceAll(s, ">", ">\n")
	s = strings.ReplaceAll(s, "<", "\n<")
	// 清理多余空行
	lines := strings.Split(s, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return []byte(strings.Join(result, "\n"))
}

// CreateZip 创建 ZIP 文件
func CreateZip(sourceDir string, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	defer writer.Close()

	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// 创建相对路径
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		// 创建 ZIP 条目
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = relPath
		header.Method = zip.Deflate

		writerFile, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}

		// 写入文件内容
		fileData, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		_, err = writerFile.Write(fileData)
		return err
	})
}
