package task

import (
	"time"

	pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"
)

type RequestedOptions struct {
	Beautify  bool `json:"beautify"`
	Decompile bool `json:"decompile"`
}

type ArtifactFile struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Source string `json:"source"`
}

type ArtifactSummary struct {
	FileCount       int            `json:"fileCount"`
	DownloadURL     string         `json:"downloadUrl,omitempty"`
	ReportURL       string         `json:"reportUrl,omitempty"`
	DiagnosticsURL  string         `json:"diagnosticsUrl,omitempty"`
	ArtifactsURL    string         `json:"artifactsUrl,omitempty"`
	PackageProfile  string         `json:"packageProfileUrl,omitempty"`
	Files           []ArtifactFile `json:"files,omitempty"`
	DownloadReady   bool           `json:"downloadReady"`
	ArchiveSize     int64          `json:"archiveSize,omitempty"`
	SourceBreakdown map[string]int `json:"sourceBreakdown,omitempty"`
	ZipPath         string         `json:"-"`
	ReportPath      string         `json:"-"`
	DiagnosticsPath string         `json:"-"`
	ArtifactsPath   string         `json:"-"`
}

type RecoveryScore struct {
	Overall         int  `json:"overall"`
	Manifest        int  `json:"manifest"`
	JS              int  `json:"js"`
	WXML            int  `json:"wxml"`
	WXSS            int  `json:"wxss"`
	DecompileHit    bool `json:"decompileHit"`
	FallbackUsed    bool `json:"fallbackUsed"`
	GeneratedRatio  int  `json:"generatedRatio"`
	FallbackPenalty int  `json:"fallbackPenalty"`
	VerifierPassed  bool `json:"verifierPassed"`
}

type StageResult struct {
	Stage           string                 `json:"stage"`
	Success         bool                   `json:"success"`
	Partial         bool                   `json:"partial"`
	Status          string                 `json:"status"`
	StartedAt       time.Time              `json:"startedAt"`
	FinishedAt      time.Time              `json:"finishedAt"`
	DurationMs      int64                  `json:"durationMs"`
	Attempt         int                    `json:"attempt,omitempty"`
	Engine          string                 `json:"engine,omitempty"`
	SourceBreakdown map[string]int         `json:"sourceBreakdown,omitempty"`
	Message         string                 `json:"message,omitempty"`
	Metrics         map[string]interface{} `json:"metrics,omitempty"`
	Diagnostics     []pkg.Diagnostic       `json:"diagnostics,omitempty"`
}

type Task struct {
	ID               string               `json:"id"`
	Status           TaskStatus           `json:"status"`
	RequestedOptions RequestedOptions     `json:"requestedOptions"`
	PackageProfile   *pkg.PackageProfile  `json:"profile,omitempty"`
	StageResults     []StageResult        `json:"stages,omitempty"`
	ArtifactSummary  *ArtifactSummary     `json:"artifacts,omitempty"`
	RecoveryScore    *RecoveryScore       `json:"score,omitempty"`
	Diagnostics      []pkg.Diagnostic     `json:"diagnostics,omitempty"`
	ErrorCode        *string              `json:"errorCode,omitempty"`
	ErrorMessage     *string              `json:"errorMessage,omitempty"`
	Progress         int                  `json:"progress"`
	CurrentStage     string               `json:"currentStage,omitempty"`
	CurrentMessage   string               `json:"currentMessage,omitempty"`
	CreatedAt        time.Time            `json:"createdAt"`
	UpdatedAt        time.Time            `json:"updatedAt"`
	CompletedAt      *time.Time           `json:"completedAt,omitempty"`
	StageStartedAt   map[string]time.Time `json:"-"`
	StageAttempts    map[string]int       `json:"-"`
}

func (t *Task) Clone() *Task {
	if t == nil {
		return nil
	}

	clone := *t
	if t.PackageProfile != nil {
		profileCopy := *t.PackageProfile
		clone.PackageProfile = &profileCopy
	}
	if t.ArtifactSummary != nil {
		artifactCopy := *t.ArtifactSummary
		if len(t.ArtifactSummary.Files) > 0 {
			artifactCopy.Files = append([]ArtifactFile(nil), t.ArtifactSummary.Files...)
		}
		if len(t.ArtifactSummary.SourceBreakdown) > 0 {
			artifactCopy.SourceBreakdown = cloneIntMap(t.ArtifactSummary.SourceBreakdown)
		}
		clone.ArtifactSummary = &artifactCopy
	}
	if t.RecoveryScore != nil {
		scoreCopy := *t.RecoveryScore
		clone.RecoveryScore = &scoreCopy
	}
	if len(t.StageResults) > 0 {
		clone.StageResults = append([]StageResult(nil), t.StageResults...)
		for i := range clone.StageResults {
			if len(t.StageResults[i].SourceBreakdown) > 0 {
				clone.StageResults[i].SourceBreakdown = cloneIntMap(t.StageResults[i].SourceBreakdown)
			}
		}
	}
	if len(t.Diagnostics) > 0 {
		clone.Diagnostics = append([]pkg.Diagnostic(nil), t.Diagnostics...)
	}
	if len(t.StageStartedAt) > 0 {
		clone.StageStartedAt = make(map[string]time.Time, len(t.StageStartedAt))
		for key, value := range t.StageStartedAt {
			clone.StageStartedAt[key] = value
		}
	}
	if len(t.StageAttempts) > 0 {
		clone.StageAttempts = cloneIntMap(t.StageAttempts)
	}
	return &clone
}

func cloneIntMap(input map[string]int) map[string]int {
	output := make(map[string]int, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
