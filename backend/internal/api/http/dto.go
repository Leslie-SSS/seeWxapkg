package httpapi

import (
	"time"

	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
	"github.com/keepbuild/seewxapkg/internal/domain/task"
	"github.com/keepbuild/seewxapkg/internal/report"
)

type CompileRequestDTO struct {
	AppID           string `form:"appId"`
	Beautify        bool   `form:"beautify"`
	Decompile       bool   `form:"decompile"`
	RemoveGuideHTML bool   `form:"removeGuideHtml"`
}

type CompileResponseDTO struct {
	Success bool   `json:"success"`
	TaskID  string `json:"taskId"`
	Message string `json:"message"`
}

type StageResponseDTO struct {
	Stage           string                 `json:"stage"`
	Success         bool                   `json:"success"`
	Partial         bool                   `json:"partial"`
	Status          string                 `json:"status"`
	StartedAt       string                 `json:"startedAt,omitempty"`
	FinishedAt      string                 `json:"finishedAt,omitempty"`
	DurationMs      int64                  `json:"durationMs,omitempty"`
	Attempt         int                    `json:"attempt,omitempty"`
	Engine          string                 `json:"engine,omitempty"`
	SourceBreakdown map[string]int         `json:"sourceBreakdown,omitempty"`
	Message         string                 `json:"message,omitempty"`
	Metrics         map[string]interface{} `json:"metrics,omitempty"`
	Diagnostics     []pkg.Diagnostic       `json:"diagnostics,omitempty"`
}

type TaskResponseDTO struct {
	ID               string                `json:"id"`
	Status           string                `json:"status"`
	Progress         int                   `json:"progress"`
	CurrentStage     string                `json:"currentStage,omitempty"`
	CurrentMessage   string                `json:"currentMessage,omitempty"`
	Profile          *pkg.PackageProfile   `json:"profile,omitempty"`
	Stages           []StageResponseDTO    `json:"stages,omitempty"`
	Score            *task.RecoveryScore   `json:"score,omitempty"`
	Artifacts        *task.ArtifactSummary `json:"artifacts,omitempty"`
	Reports          map[string]string     `json:"reports,omitempty"`
	DiagnosticsCount int                   `json:"diagnosticsCount"`
	ErrorCode        *string               `json:"errorCode,omitempty"`
	ErrorMessage     *string               `json:"errorMessage,omitempty"`
	ErrorDetail      *string               `json:"errorDetail,omitempty"`
}

func ToTaskResponseDTO(t *task.Task) TaskResponseDTO {
	stages := make([]StageResponseDTO, 0, len(t.StageResults))
	for _, stage := range t.StageResults {
		stages = append(stages, ToStageResponseDTO(stage))
	}

	reports := map[string]string{}
	if t.ArtifactSummary != nil {
		if t.ArtifactSummary.ReportURL != "" {
			reports["recovery"] = t.ArtifactSummary.ReportURL
			reports["manifest"] = t.ArtifactSummary.ReportURL + "?name=manifest-recovery-report"
			if t.RequestedOptions.Decompile {
				reports["js"] = t.ArtifactSummary.ReportURL + "?name=js-recovery-report"
				reports["wxml"] = t.ArtifactSummary.ReportURL + "?name=wxml-recovery-report"
				reports["wxss"] = t.ArtifactSummary.ReportURL + "?name=wxss-recovery-report"
			}
			if t.RequestedOptions.Beautify {
				reports["format"] = t.ArtifactSummary.ReportURL + "?name=format-report"
			}
			reports["zipManifest"] = t.ArtifactSummary.ReportURL + "?name=zip-manifest"
		}
		if t.ArtifactSummary.DiagnosticsURL != "" {
			reports["diagnostics"] = t.ArtifactSummary.DiagnosticsURL
		}
		if t.ArtifactSummary.PackageProfile != "" {
			reports["packageProfile"] = t.ArtifactSummary.PackageProfile
		}
	}

	var errorMessage *string
	if t.ErrorMessage != nil {
		safeMessage := report.SanitizeText(*t.ErrorMessage)
		errorMessage = &safeMessage
	}

	// The persisted failure cause is already sanitized and truncated at write
	// time; re-sanitize defensively so a cause can never leak host paths to the
	// public DTO even if the persistence invariant regresses.
	var errorDetail *string
	if t.FailureCause != nil {
		safeDetail := report.SanitizeText(*t.FailureCause)
		errorDetail = &safeDetail
	}

	return TaskResponseDTO{
		ID:               t.ID,
		Status:           string(t.Status),
		Progress:         t.Progress,
		CurrentStage:     t.CurrentStage,
		CurrentMessage:   report.SanitizeText(t.CurrentMessage),
		Profile:          t.PackageProfile,
		Stages:           stages,
		Score:            t.RecoveryScore,
		Artifacts:        report.SanitizeArtifactSummary(t.ArtifactSummary),
		Reports:          reports,
		DiagnosticsCount: len(t.Diagnostics),
		ErrorCode:        t.ErrorCode,
		ErrorMessage:     errorMessage,
		ErrorDetail:      errorDetail,
	}
}

func ToStageResponseDTO(stage task.StageResult) StageResponseDTO {
	stage = report.SanitizeStageResult(stage)
	return StageResponseDTO{
		Stage:           stage.Stage,
		Success:         stage.Success,
		Partial:         stage.Partial,
		Status:          stage.Status,
		StartedAt:       stage.StartedAt.Format(time.RFC3339Nano),
		FinishedAt:      stage.FinishedAt.Format(time.RFC3339Nano),
		DurationMs:      stage.DurationMs,
		Attempt:         stage.Attempt,
		Engine:          stage.Engine,
		SourceBreakdown: stage.SourceBreakdown,
		Message:         stage.Message,
		Metrics:         stage.Metrics,
		Diagnostics:     stage.Diagnostics,
	}
}
