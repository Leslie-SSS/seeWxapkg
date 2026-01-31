package service

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"fmt"

	"golang.org/x/crypto/pbkdf2"
)

const (
	Salt          = "saltiest"
	IV            = "the iv: 16 bytes"
	FileHeader    = "V1MMWX"
	DefaultXORKey = 0x66
	Iterations    = 1000
	KeyLength     = 32
)

// IsDecrypted 检查文件是否已解密
func IsDecrypted(data []byte) bool {
	if len(data) < 14 {
		return false
	}

	firstMark := data[0]
	lastMark := data[13]

	return firstMark == 0xBE && lastMark == 0xED
}

// IsEncrypted 检查文件是否是加密的 wxapkg (以 V1MMWX 开头)
func IsEncrypted(data []byte) bool {
	if len(data) < len(FileHeader) {
		return false
	}
	header := string(data[:len(FileHeader)])
	return header == FileHeader
}

// ValidateHeader 验证加密文件头
func ValidateHeader(data []byte) error {
	if len(data) < len(FileHeader) {
		return fmt.Errorf("file too small")
	}

	header := string(data[:len(FileHeader)])
	if header != FileHeader {
		return fmt.Errorf("invalid wxapkg file format (expected V1MMWX)")
	}

	return nil
}

// DecryptWxapkg 解密 wxapkg 文件
func DecryptWxapkg(data []byte, appID string) ([]byte, error) {
	if IsDecrypted(data) {
		return data, nil
	}

	if err := ValidateHeader(data); err != nil {
		return nil, err
	}

	salt := []byte(Salt)
	iv := []byte(IV)
	key := pbkdf2.Key([]byte(appID), salt, Iterations, KeyLength, crypto.SHA1.New)

	headerLen := len(FileHeader)
	if len(data) < headerLen+1024 {
		return nil, fmt.Errorf("file too small to decrypt")
	}

	encrypted1024 := make([]byte, 1024)
	copy(encrypted1024, data[headerLen:headerLen+1024])

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("AES cipher creation failed: %w", err)
	}

	if len(encrypted1024)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("encrypted data size %d is not a multiple of block size %d",
			len(encrypted1024), aes.BlockSize)
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	decrypted1024 := make([]byte, 1024)
	mode.CryptBlocks(decrypted1024, encrypted1024)

	xorKey := DefaultXORKey
	if len(appID) >= 2 {
		xorKey = int(appID[len(appID)-2])
	}

	remainingLen := len(data) - 1024 - headerLen
	remaining := make([]byte, remainingLen)
	for i := 0; i < remainingLen; i++ {
		remaining[i] = data[1024+headerLen+i] ^ byte(xorKey)
	}

	result := make([]byte, 1023+len(remaining))
	copy(result, decrypted1024[:1023])
	copy(result[1023:], remaining)

	return result, nil
}
