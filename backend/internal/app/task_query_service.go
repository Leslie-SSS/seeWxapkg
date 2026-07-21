package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/keepbuild/seewxapkg/internal/config"
	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
	"github.com/keepbuild/seewxapkg/internal/domain/task"
	"github.com/keepbuild/seewxapkg/internal/report"
)

var reportFiles = map[string]string{
	"recovery-report":          "recovery-report.json",
	"manifest-recovery-report": "manifest-recovery-report.json",
	"js-recovery-report":       "js-recovery-report.json",
	"wxml-recovery-report":     "wxml-recovery-report.json",
	"wxss-recovery-report":     "wxss-recovery-report.json",
	"format-report":            "format-report.json",
	"zip-manifest":             "zip-manifest.json",
	"package-profile":          "package-profile.json",
}

type TaskQueryService struct {
	cfg  *config.Config
	repo task.Repository
}

func NewTaskQueryService(cfg *config.Config, repo task.Repository) *TaskQueryService {
	return &TaskQueryService{cfg: cfg, repo: repo}
}

func (s *TaskQueryService) GetTask(ctx context.Context, taskID string) (*task.Task, error) {
	return s.repo.Get(ctx, taskID)
}

func (s *TaskQueryService) GetReport(ctx context.Context, taskID string) (*report.RecoveryReport, error) {
	t, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	reportPath := s.resolveReportPath(taskID, "recovery-report")
	if t.ArtifactSummary == nil || reportPath == "" {
		return report.BuildRecoveryReport(t), nil
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		return nil, err
	}
	data, err = report.SanitizeJSONBytes(data)
	if err != nil {
		return nil, err
	}
	var r report.RecoveryReport
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *TaskQueryService) GetDiagnostics(ctx context.Context, taskID string) ([]pkg.Diagnostic, error) {
	t, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return report.SanitizeDiagnostics(t.Diagnostics), nil
}

func (s *TaskQueryService) GetNamedReport(ctx context.Context, taskID string, name string) ([]byte, error) {
	t, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	reportPath := s.resolveReportPath(taskID, name)
	if t.ArtifactSummary == nil || reportPath == "" {
		return nil, os.ErrNotExist
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		return nil, err
	}
	return report.SanitizeJSONBytes(data)
}

func (s *TaskQueryService) ResolveZipPath(taskID string) string {
	if s.cfg == nil || s.cfg.OutputDir == "" || !safePathComponent(taskID) {
		return ""
	}
	return filepath.Join(s.cfg.OutputDir, taskID+".zip")
}

// ResolveReadyZipPath only exposes archives belonging to a persisted terminal
// task that explicitly declares its download ready. This prevents an orphaned
// ZIP from a failed finalization attempt from becoming publicly reachable.
func (s *TaskQueryService) ResolveReadyZipPath(ctx context.Context, taskID string) (string, error) {
	if !safePathComponent(taskID) {
		return "", os.ErrNotExist
	}
	current, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return "", err
	}
	if current.ArtifactSummary == nil || !current.ArtifactSummary.DownloadReady || (current.Status != task.TaskCompleted && current.Status != task.TaskPartial) {
		return "", os.ErrNotExist
	}
	zipPath := s.ResolveZipPath(taskID)
	if zipPath == "" {
		return "", os.ErrNotExist
	}
	return zipPath, nil
}

func (s *TaskQueryService) resolveReportPath(taskID string, name string) string {
	if s.cfg == nil || s.cfg.TempDir == "" || !safePathComponent(taskID) {
		return ""
	}
	if name == "" {
		name = "recovery-report"
	}
	filename, ok := reportFiles[name]
	if !ok {
		return ""
	}
	return filepath.Join(s.cfg.TempDir, taskID, "result", "reports", filename)
}

func safePathComponent(value string) bool {
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value && filepath.VolumeName(value) == ""
}
