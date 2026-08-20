package service

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/keepbuild/seewxapkg/internal/beautify"
	"github.com/keepbuild/seewxapkg/internal/infra/storage"
	"github.com/keepbuild/seewxapkg/internal/model"
	"github.com/tidwall/pretty"
)

const (
	maxWxapkgFiles    = 100_000
	maxWxapkgNameSize = 4 * 1024
	maxExtractWorkers = 10
)

// Global beautify service instance
var beautifyService *beautify.Service

// InitBeautifyService initializes the global beautify service
func InitBeautifyService(enabled bool, timeoutSeconds int, maxFileSize int, failureLimit int, deobfuscate bool) error {
	if !enabled {
		log.Println("[Unpack] Beautify service disabled")
		beautifyService = nil
		return nil
	}

	cfg := beautify.ConfigFromParams(enabled, timeoutSeconds, maxFileSize, failureLimit, deobfuscate)

	var err error
	beautifyService, err = beautify.NewService(cfg)
	if err != nil {
		log.Printf("[Unpack] Failed to initialize beautify service (%T)", err)
		beautifyService = nil
		return fmt.Errorf("initialize beautify service: %w", err)
	}

	log.Printf("[Unpack] Beautify service initialized successfully")
	return nil
}

// StopBeautifyService stops the beautify service
func StopBeautifyService() {
	if beautifyService != nil {
		if err := beautifyService.Stop(); err != nil {
			log.Printf("[Unpack] Error stopping beautify service (%T)", err)
		}
		beautifyService = nil
	}
}

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

	// 创建输出目录
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	// Java: ByteBuffer buffer = ByteBuffer.wrap(data).order(ByteOrder.BIG_ENDIAN);
	// Java: byte firstMark = buffer.get();
	// Java: if (firstMark != (byte) 0xBE) { throw ... }
	if len(data) < 14 {
		return nil, fmt.Errorf("file too small: %d bytes", len(data))
	}

	firstMark := data[0]
	if firstMark != 0xBE {
		// 检查是否是加密文件
		if len(data) >= 6 && string(data[:6]) == "V1MMWX" {
			return nil, fmt.Errorf("文件是加密格式（V1MMWX），需要提供正确的 AppID 进行解密")
		}
		return nil, fmt.Errorf("无效的 wxapkg 文件：首标记错误（期望 0xBE，实际 0x%02X）", firstMark)
	}

	// 使用 bytes.Reader 按照大端序读取
	reader := bytes.NewReader(data[1:])

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

	// Java: if (lastMark != (byte) 0xED) { throw ... }
	if lastMark != 0xED {
		return nil, fmt.Errorf("无效的 wxapkg 文件：尾标记错误（期望 0xED，实际 0x%02X）", lastMark)
	}
	if indexInfoLength < 4 {
		return nil, fmt.Errorf("invalid wxapkg index length: %d", indexInfoLength)
	}
	indexEnd := uint64(14) + uint64(indexInfoLength)
	packageEnd := indexEnd + uint64(bodyInfoLength)
	if indexEnd > uint64(len(data)) || packageEnd > uint64(len(data)) {
		return nil, fmt.Errorf("wxapkg sections out of bounds: index=%d, body=%d, dataLen=%d",
			indexInfoLength, bodyInfoLength, len(data))
	}

	// Never let malformed index metadata consume bytes from the package body.
	reader = bytes.NewReader(data[14:indexEnd])

	// Java: int fileCount = buffer.getInt();
	var fileCount uint32
	if err := binary.Read(reader, binary.BigEndian, &fileCount); err != nil {
		return nil, fmt.Errorf("read fileCount: %w", err)
	}

	if fileCount > maxWxapkgFiles {
		return nil, fmt.Errorf("wxapkg contains too many files: %d (max %d)", fileCount, maxWxapkgFiles)
	}
	// Even an empty-name entry needs nameLen, offset and size fields.
	if uint64(fileCount)*12 > uint64(reader.Len()) {
		return nil, fmt.Errorf("invalid wxapkg file count %d for index length %d", fileCount, indexInfoLength)
	}

	// Java: for (int i = 0; i < fileCount; i++) {
	files := make([]model.FileEntry, int(fileCount))
	var totalExtracted uint64
	maxReferencedEnd := uint64(indexEnd)
	runtimeAliasSeen := false
	seenTargets := make(map[string]int, int(fileCount))
	duplicateSkip := make(map[int]bool)
	for i := uint32(0); i < fileCount; i++ {
		// Java: int nameLen = buffer.getInt();
		var nameLen uint32
		if err := binary.Read(reader, binary.BigEndian, &nameLen); err != nil {
			return nil, fmt.Errorf("read nameLen: %w", err)
		}
		if nameLen == 0 || nameLen > maxWxapkgNameSize {
			return nil, fmt.Errorf("invalid file name length at index %d: %d", i, nameLen)
		}
		if uint64(nameLen)+8 > uint64(reader.Len()) {
			return nil, fmt.Errorf("file index entry %d exceeds declared index section", i)
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

		if err := validateFileBounds(files[i], len(data)); err != nil {
			return nil, err
		}
		fileEnd := uint64(files[i].Offset) + uint64(files[i].Size)
		target, err := safeOutputPath(outputDir, files[i].Name)
		if err != nil {
			return nil, err
		}
		if existing, exists := seenTargets[target]; exists {
			// WeChat plugin packages duplicate metadata entries
			// (`__extended__/<appid>/plugin.json` appears twice with identical
			// content). Tolerate identical duplicates — keep the first entry,
			// skip the copy — but keep rejecting divergent duplicates that
			// would silently overwrite distinct data.
			if !sameFileData(data, files[existing], files[i]) {
				return nil, fmt.Errorf("duplicate output path with differing content: %s", files[i].Name)
			}
			duplicateSkip[int(i)] = true
			continue
		}

		// Some WeChat runtimes add an app-service.js/appservice.js alias that
		// points back into an already indexed body range. It is a view of an
		// existing payload, not an extraction amplification. Keep the allowance
		// narrow: one known runtime alias, fully contained in a prior range.
		isRuntimeAlias := !runtimeAliasSeen &&
			uint64(files[i].Offset) < maxReferencedEnd &&
			fileEnd <= maxReferencedEnd &&
			uint64(files[i].Offset) >= indexEnd &&
			isSharedRuntimeAlias(files[i].Name)
		if !isRuntimeAlias {
			totalExtracted += uint64(files[i].Size)
			if totalExtracted > uint64(len(data)) {
				return nil, fmt.Errorf("declared extracted data exceeds package size")
			}
		} else {
			runtimeAliasSeen = true
		}
		if fileEnd > maxReferencedEnd {
			maxReferencedEnd = fileEnd
		}
		seenTargets[target] = int(i)
	}

	// Use a fixed worker pool so an attacker-controlled file count cannot create
	// an unbounded number of goroutines.
	var wg sync.WaitGroup
	var mu sync.Mutex
	var extractErr error
	jobs := make(chan model.FileEntry)
	workerCount := maxExtractWorkers
	if len(files) < workerCount {
		workerCount = len(files)
	}
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				if err := extractFile(data, f, outputDir, beautify); err != nil {
					mu.Lock()
					if extractErr == nil {
						extractErr = err
					}
					mu.Unlock()
				}
			}
		}()
	}
	for index, file := range files {
		if duplicateSkip[index] {
			continue
		}
		jobs <- file
	}
	close(jobs)
	wg.Wait()

	if extractErr != nil {
		return nil, extractErr
	}

	result.Files = files
	// Every validated index entry maps to one unique regular output file. Using
	// the index count keeps nested page/component files in the reported total.
	result.FileCount = len(files)
	result.Success = true

	return result, nil
}

// extractFile 提取单个文件
func extractFile(data []byte, file model.FileEntry, outputDir string, beautify bool) error {
	if err := validateFileBounds(file, len(data)); err != nil {
		return err
	}
	start := int(file.Offset)
	end := start + int(file.Size)

	// 读取文件内容
	content := make([]byte, int(file.Size))
	copy(content, data[start:end])

	// 美化代码
	if beautify {
		content = beautifyContent(content, file.Name)
	}

	// 创建完整路径
	fullPath, err := safeOutputPath(outputDir, file.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// 写入文件
	if err := os.WriteFile(fullPath, content, 0600); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

func validateFileBounds(file model.FileEntry, dataLen int) error {
	end := uint64(file.Offset) + uint64(file.Size)
	if uint64(file.Offset) > uint64(dataLen) || end > uint64(dataLen) {
		return fmt.Errorf("file out of bounds: %s (offset=%d, size=%d, dataLen=%d)",
			file.Name, file.Offset, file.Size, dataLen)
	}
	return nil
}

// sameFileData reports whether two index entries reference byte-identical
// content. Both entries are assumed to have passed validateFileBounds.
func sameFileData(data []byte, a, b model.FileEntry) bool {
	if a.Size != b.Size {
		return false
	}
	return bytes.Equal(data[a.Offset:a.Offset+a.Size], data[b.Offset:b.Offset+b.Size])
}

func isSharedRuntimeAlias(name string) bool {
	base := strings.ToLower(filepath.Base(filepath.ToSlash(name)))
	return base == "app-service.js" || base == "appservice.js"
}

func safeOutputPath(outputDir, name string) (string, error) {
	return storage.SafePackageOutputPath(outputDir, name)
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
	case ".js", ".wxs":
		// 使用新的美化服务
		if beautifyService != nil {
			return beautifyService.Beautify(content, filename)
		}
		return content
	case ".wxml", ".html":
		// 使用新的美化服务
		if beautifyService != nil {
			return beautifyService.Beautify(content, filename)
		}
		return content
	case ".wxss", ".css":
		// 使用新的美化服务
		if beautifyService != nil {
			return beautifyService.Beautify(content, filename)
		}
		return content
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
