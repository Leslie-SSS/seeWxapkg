package pkg

type DiagnosticSeverity string

const (
	SeverityInfo  DiagnosticSeverity = "info"
	SeverityWarn  DiagnosticSeverity = "warn"
	SeverityError DiagnosticSeverity = "error"
)

type Diagnostic struct {
	Code     string                 `json:"code"`
	Severity DiagnosticSeverity     `json:"severity"`
	Message  string                 `json:"message"`
	File     string                 `json:"file,omitempty"`
	Stage    string                 `json:"stage,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

func Info(code, msg, stage, file string) Diagnostic {
	return Diagnostic{Code: code, Severity: SeverityInfo, Message: msg, Stage: stage, File: file}
}

func Warn(code, msg, stage, file string) Diagnostic {
	return Diagnostic{Code: code, Severity: SeverityWarn, Message: msg, Stage: stage, File: file}
}

func Error(code, msg, stage, file string) Diagnostic {
	return Diagnostic{Code: code, Severity: SeverityError, Message: msg, Stage: stage, File: file}
}
