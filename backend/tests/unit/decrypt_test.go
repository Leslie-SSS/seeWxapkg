package unit

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"strings"
	"testing"

	"github.com/keepbuild/seewxapkg/internal/pipeline/decrypt"
	"github.com/keepbuild/seewxapkg/tests/testutil"
	"golang.org/x/crypto/pbkdf2"
)

func TestIsEncrypted(t *testing.T) {
	if !decrypt.IsEncrypted([]byte("V1MMWXhello")) {
		t.Fatalf("expected encrypted header to be detected")
	}
	if decrypt.IsEncrypted([]byte("random")) {
		t.Fatalf("unexpected encrypted detection")
	}
}

func TestIsDecrypted(t *testing.T) {
	data := testutil.MustBuildWxapkg(map[string]string{
		"app.json": `{"pages":["pages/home/index"]}`,
	})
	if !decrypt.IsDecrypted(data) {
		t.Fatalf("expected standard wxapkg to be detected as decrypted")
	}
}

func TestDecryptWxapkgWithBadAppID(t *testing.T) {
	_, err := decrypt.DecryptWxapkg([]byte("V1MMWXmorebytesmorebytesmorebytesmorebytes"), "bad")
	if err == nil {
		t.Fatalf("expected invalid appID error")
	}
}

// encryptForTest mirrors the official WeChat wxapkg encryption algorithm so
// the decryption path can be verified against a known-good sample without
// shipping real packages: PBKDF2-HMAC-SHA1(appID, "saltiest", 1000, 32),
// AES-256-CBC over the first 1024 bytes, then XOR of the remainder with the
// second-to-last appID byte.
func encryptForTest(t *testing.T, plain []byte, appID string) []byte {
	t.Helper()
	salt := []byte(decrypt.Salt)
	iv := []byte(decrypt.IV)
	key := pbkdf2.Key([]byte(appID), salt, decrypt.Iterations, decrypt.KeyLength, sha1.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	blockBytes := make([]byte, 1024)
	copy(blockBytes, plain)
	encrypted := make([]byte, 1024)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(encrypted, blockBytes)
	xorKey := decrypt.DefaultXORKey
	if len(appID) >= 2 {
		xorKey = int(appID[len(appID)-2])
	}
	out := append([]byte(decrypt.FileHeader), encrypted...)
	for i := 1023; i < len(plain); i++ {
		out = append(out, plain[i]^byte(xorKey))
	}
	return out
}

func TestDecryptWxapkgRoundTrip(t *testing.T) {
	// Build a standard package larger than the 1024-byte AES block so both the
	// CBC head and the XOR tail are exercised.
	payload := strings.Repeat("0123456789abcdef", 160) // 2560 bytes
	plain := testutil.MustBuildWxapkg(map[string]string{
		"app.json": `{"pages":["pages/home/index"]}`,
		"pages/home/index.js": "Page({data:{big:\"" + payload + "\"}});\n",
	})
	if len(plain) <= 1024 {
		t.Fatalf("fixture too small: %d bytes", len(plain))
	}
	appID := "wx0123456789abcdef"
	encrypted := encryptForTest(t, plain, appID)
	if !decrypt.IsEncrypted(encrypted) {
		t.Fatalf("encrypted fixture must carry the V1MMWX header")
	}
	decrypted, err := decrypt.DecryptWxapkg(encrypted, appID)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(decrypted, plain) {
		t.Fatalf("round-trip mismatch: got %d bytes, want %d", len(decrypted), len(plain))
	}
}

func TestDecryptWxapkgWrongAppIDProducesGarbage(t *testing.T) {
	plain := testutil.MustBuildWxapkg(map[string]string{
		"app.json": `{"pages":["pages/home/index"]}`,
	})
	encrypted := encryptForTest(t, plain, "wx0123456789abcdef")
	// A wrong AppID derives a different key: decryption succeeds (AES-CBC has
	// no authentication) but the output is garbage that must fail the wxapkg
	// magic check downstream.
	decrypted, err := decrypt.DecryptWxapkg(encrypted, "wxffffffffffffffff")
	if err != nil {
		t.Fatalf("wrong AppID must not error at the decrypt layer: %v", err)
	}
	if len(decrypted) < 14 || decrypted[0] == 0xBE {
		t.Fatalf("wrong AppID must not produce a magic-valid package")
	}
}
