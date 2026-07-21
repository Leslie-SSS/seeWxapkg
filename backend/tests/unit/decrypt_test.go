package unit

import (
	"testing"

	"github.com/keepbuild/seewxapkg/internal/pipeline/decrypt"
	"github.com/keepbuild/seewxapkg/tests/testutil"
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
