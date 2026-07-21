package decrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"errors"
	"fmt"
	"regexp"
)

const (
	Salt          = "saltiest"
	IV            = "the iv: 16 bytes"
	FileHeader    = "V1MMWX"
	DefaultXORKey = 0x66
	Iterations    = 1000
	KeyLength     = 32
)

var (
	ErrNeedAppID     = errors.New("encrypted package requires appID")
	ErrBadAppID      = errors.New("invalid appID format")
	ErrInvalidHeader = errors.New("invalid wxapkg header")
	appIDPattern     = regexp.MustCompile(`^wx[a-f0-9]{16}$`)
)

type EncryptionMode string

const (
	EncryptionUnknown   EncryptionMode = "unknown"
	EncryptionEncrypted EncryptionMode = "encrypted"
	EncryptionPlain     EncryptionMode = "plain"
)

func DetectEncryptionMode(data []byte) EncryptionMode {
	switch {
	case IsEncrypted(data):
		return EncryptionEncrypted
	case IsDecrypted(data):
		return EncryptionPlain
	default:
		return EncryptionUnknown
	}
}

func IsDecrypted(data []byte) bool {
	if len(data) < 14 {
		return false
	}
	return data[0] == 0xBE && data[13] == 0xED
}

func IsEncrypted(data []byte) bool {
	if len(data) < len(FileHeader) {
		return false
	}
	return string(data[:len(FileHeader)]) == FileHeader
}

func ValidateHeader(data []byte) error {
	if len(data) < len(FileHeader) {
		return ErrInvalidHeader
	}
	if string(data[:len(FileHeader)]) != FileHeader {
		return ErrInvalidHeader
	}
	return nil
}

func ValidateAppID(appID string) error {
	if !appIDPattern.MatchString(appID) {
		return ErrBadAppID
	}
	return nil
}

func DecryptWxapkg(data []byte, appID string) ([]byte, error) {
	if IsDecrypted(data) {
		return data, nil
	}
	if IsEncrypted(data) && appID == "" {
		return nil, ErrNeedAppID
	}
	if appID != "" {
		if err := ValidateAppID(appID); err != nil {
			return nil, err
		}
	}
	if err := ValidateHeader(data); err != nil {
		return nil, err
	}

	salt := []byte(Salt)
	iv := []byte(IV)
	key, err := pbkdf2.Key(sha1.New, appID, salt, Iterations, KeyLength)
	if err != nil {
		return nil, fmt.Errorf("derive decryption key: %w", err)
	}

	headerLen := len(FileHeader)
	if len(data) < headerLen+1024 {
		return nil, fmt.Errorf("file too small to decrypt")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("AES cipher creation failed: %w", err)
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	decrypted1024 := make([]byte, 1024)
	mode.CryptBlocks(decrypted1024, data[headerLen:headerLen+1024])

	xorKey := DefaultXORKey
	if len(appID) >= 2 {
		xorKey = int(appID[len(appID)-2])
	}

	remainingLen := len(data) - 1024 - headerLen
	result := make([]byte, 1023+remainingLen)
	copy(result, decrypted1024[:1023])
	for i := 0; i < remainingLen; i++ {
		result[1023+i] = data[1024+headerLen+i] ^ byte(xorKey)
	}

	return result, nil
}
