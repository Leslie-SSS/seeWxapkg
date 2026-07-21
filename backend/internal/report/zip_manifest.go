package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/keepbuild/seewxapkg/internal/infra/storage"
)

type ZipManifest struct {
	TaskID string   `json:"taskId"`
	Files  []string `json:"files"`
}

func BuildZipManifest(taskID string, archiveEntries []string, archivePrefix string) (*ZipManifest, error) {
	if err := storage.ValidateZipEntryPath(archivePrefix); err != nil {
		return nil, fmt.Errorf("invalid ZIP layout prefix %q", archivePrefix)
	}
	manifest := &ZipManifest{TaskID: taskID, Files: make([]string, 0, len(archiveEntries))}
	seen := make(map[string]struct{}, len(archiveEntries))
	for _, entry := range archiveEntries {
		if err := storage.ValidateZipEntryPath(entry); err != nil {
			return nil, err
		}
		if !strings.HasPrefix(entry, archivePrefix+"/") {
			return nil, fmt.Errorf("ZIP entry %q is outside required prefix %q", entry, archivePrefix)
		}
		if _, duplicate := seen[entry]; duplicate {
			return nil, fmt.Errorf("duplicate ZIP entry %q", entry)
		}
		seen[entry] = struct{}{}
		manifest.Files = append(manifest.Files, entry)
	}
	sort.Strings(manifest.Files)
	return manifest, nil
}

func WriteZipManifest(path string, manifest *ZipManifest) error {
	return storage.WriteJSON(path, manifest)
}
