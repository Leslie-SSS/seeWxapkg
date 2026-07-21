package recover

import pkg "github.com/keepbuild/seewxapkg/internal/domain/pkg"

type RecoveredFile struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Source    string `json:"source"`
	Generated bool   `json:"generated,omitempty"`
}

type JSRecoveryResult struct {
	Success     bool             `json:"success"`
	Partial     bool             `json:"partial"`
	Files       []RecoveredFile  `json:"files,omitempty"`
	Diagnostics []pkg.Diagnostic `json:"diagnostics,omitempty"`
	ReportPath  string           `json:"reportPath,omitempty"`
	Recovered   int              `json:"recovered"`
	Generated   int              `json:"generated"`
	Native      int              `json:"native"`
}

type WXMLRecoveryResult struct {
	Success     bool             `json:"success"`
	Partial     bool             `json:"partial"`
	Files       []RecoveredFile  `json:"files,omitempty"`
	Diagnostics []pkg.Diagnostic `json:"diagnostics,omitempty"`
	ReportPath  string           `json:"reportPath,omitempty"`
	Recovered   int              `json:"recovered"`
	Generated   int              `json:"generated"`
	Native      int              `json:"native"`
}

type WXSSRecoveryResult struct {
	Success     bool             `json:"success"`
	Partial     bool             `json:"partial"`
	Files       []RecoveredFile  `json:"files,omitempty"`
	Diagnostics []pkg.Diagnostic `json:"diagnostics,omitempty"`
	ReportPath  string           `json:"reportPath,omitempty"`
	Recovered   int              `json:"recovered"`
	Generated   int              `json:"generated"`
	Native      int              `json:"native"`
}

type FallbackResult struct {
	Success     bool             `json:"success"`
	Partial     bool             `json:"partial"`
	Status      string           `json:"status,omitempty"`
	OutputDir   string           `json:"outputDir,omitempty"`
	Files       []RecoveredFile  `json:"files,omitempty"`
	Diagnostics []pkg.Diagnostic `json:"diagnostics,omitempty"`
	Stdout      string           `json:"stdout,omitempty"`
	Stderr      string           `json:"stderr,omitempty"`
}

type FallbackMergeConflict struct {
	Path       string `json:"path"`
	Resolution string `json:"resolution"`
}

type FallbackMergeResult struct {
	Added     int                     `json:"added"`
	Identical int                     `json:"identical"`
	Preserved int                     `json:"preserved"`
	Conflicts []FallbackMergeConflict `json:"conflicts,omitempty"`
}
