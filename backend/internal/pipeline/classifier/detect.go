package classifier

import (
	"os"
	"path/filepath"
	"strings"

	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
	"github.com/keepbuild/seewxapkg/internal/pipeline/decrypt"
)

func DetectPackageProfile(data []byte, extractedDir string) (*pkg.PackageProfile, error) {
	profile := &pkg.PackageProfile{
		IsEncrypted:      decrypt.IsEncrypted(data),
		IsStandardWxapkg: decrypt.IsDecrypted(data),
		IndexFileCount:   countIndexedFiles(data),
	}

	if extractedDir != "" {
		if err := detectExtractedVariant(extractedDir, profile); err != nil {
			return nil, err
		}
	}

	profile.IsWeChat4xLike = profile.HasAppConfigJSON || profile.HasPageFrameHTML || profile.HasPageFrameJS || profile.HasAppWxssJS

	switch {
	case profile.IsGamePackage:
		profile.SuspectedVariant = "game"
	case profile.IsSubPackage:
		profile.SuspectedVariant = "subpackage"
	case profile.IsWeChat4xLike:
		profile.SuspectedVariant = "wechat4x"
	case profile.IsStandardWxapkg:
		profile.SuspectedVariant = "standard"
	case profile.IsEncrypted:
		profile.SuspectedVariant = "encrypted"
	default:
		profile.SuspectedVariant = "unknown"
	}

	return profile, nil
}

func detectExtractedVariant(extractedDir string, profile *pkg.PackageProfile) error {
	return filepath.Walk(extractedDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		rel, err := filepath.Rel(extractedDir, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)

		switch name {
		case "app-config.json":
			profile.HasAppConfigJSON = true
		case "app-service.js":
			profile.HasAppServiceJS = true
		case "workers.js":
			profile.HasWorkersJS = true
		case "page-frame.html":
			profile.HasPageFrameHTML = true
		case "page-frame.js":
			profile.HasPageFrameJS = true
		case "app-wxss.js":
			profile.HasAppWxssJS = true
		case "game.json":
			profile.IsGamePackage = true
		}

		if strings.Contains(name, "/__APP__") || strings.HasPrefix(name, "__APP__/") {
			profile.IsSubPackage = true
		}
		return nil
	})
}

func countIndexedFiles(data []byte) int {
	if len(data) < 18 || data[0] != 0xBE {
		return 0
	}
	count := int(data[14])<<24 | int(data[15])<<16 | int(data[16])<<8 | int(data[17])
	if count < 0 {
		return 0
	}
	return count
}
