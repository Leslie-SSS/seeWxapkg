package report

import (
	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
	"github.com/keepbuild/seewxapkg/internal/infra/storage"
)

func WriteDiagnostics(path string, list []pkg.Diagnostic) error {
	return storage.WriteJSON(path, SanitizeDiagnostics(list))
}
