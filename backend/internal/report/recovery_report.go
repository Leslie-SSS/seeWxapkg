package report

import (
	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
	"github.com/keepbuild/seewxapkg/internal/domain/task"
	"github.com/keepbuild/seewxapkg/internal/infra/storage"
)

type RecoveryReport struct {
	TaskID          string                `json:"taskId"`
	Status          string                `json:"status"`
	SnapshotScope   string                `json:"snapshotScope"`
	Profile         *pkg.PackageProfile   `json:"profile,omitempty"`
	StageResults    []task.StageResult    `json:"stages,omitempty"`
	Score           *task.RecoveryScore   `json:"score,omitempty"`
	ScoreDisclosure *ScoreDisclosure      `json:"scoreDisclosure,omitempty"`
	Diagnostics     []pkg.Diagnostic      `json:"diagnostics,omitempty"`
	Artifacts       *task.ArtifactSummary `json:"artifacts,omitempty"`
	Packaging       PackagingSnapshot     `json:"packaging"`
}

const SnapshotScopeLiveTask = "live-task"

type ScoreDisclosure struct {
	Basis       string   `json:"basis"`
	Summary     string   `json:"summary"`
	NotMeasured []string `json:"notMeasured"`
}

type PackagingSnapshot struct {
	Status        string `json:"status"`
	DownloadReady bool   `json:"downloadReady"`
	ArchiveSize   int64  `json:"archiveSize,omitempty"`
	ZipManifest   string `json:"zipManifest,omitempty"`
}

func BuildRecoveryReport(t *task.Task) *RecoveryReport {
	if t == nil {
		return &RecoveryReport{SnapshotScope: SnapshotScopeLiveTask, Packaging: PackagingSnapshot{Status: "unavailable"}}
	}
	var profile *pkg.PackageProfile
	if t.PackageProfile != nil {
		profileCopy := *t.PackageProfile
		profile = &profileCopy
	}
	var score *task.RecoveryScore
	if t.RecoveryScore != nil {
		scoreCopy := *t.RecoveryScore
		score = &scoreCopy
	}
	report := &RecoveryReport{
		TaskID:        t.ID,
		Status:        string(t.Status),
		SnapshotScope: SnapshotScopeLiveTask,
		Profile:       profile,
		StageResults:  sanitizeStageResults(t.StageResults),
		Score:         score,
		ScoreDisclosure: &ScoreDisclosure{
			Basis:   "static-artifact-quality",
			Summary: "分数只反映页面文件覆盖、静态语法、引用和已知恢复缺口，不代表与原始源码一致。",
			NotMeasured: []string{
				"与原始源码逐字一致性",
				"运行时业务行为正确性",
				"交互和数据语义完整性",
			},
		},
		Diagnostics: SanitizeDiagnostics(t.Diagnostics),
		Artifacts:   sanitizeArtifactSummary(t.ArtifactSummary),
	}

	report.Packaging = PackagingSnapshot{Status: "pending", ZipManifest: "report?name=zip-manifest"}
	if t.ArtifactSummary != nil && t.ArtifactSummary.DownloadReady {
		report.Packaging.Status = "ready"
		report.Packaging.DownloadReady = true
		report.Packaging.ArchiveSize = t.ArtifactSummary.ArchiveSize
	} else if t.Status == task.TaskFailed {
		report.Packaging.Status = "unavailable"
	}
	return report
}

func WriteRecoveryReport(path string, r *RecoveryReport) error {
	return storage.WriteJSON(path, r)
}
