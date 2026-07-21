package task

type TaskEvent struct {
	Type             string `json:"type"`
	Stage            string `json:"stage,omitempty"`
	Status           string `json:"status,omitempty"`
	Percent          int    `json:"percent"`
	Message          string `json:"message,omitempty"`
	FileCount        int    `json:"fileCount,omitempty"`
	TaskID           string `json:"taskId,omitempty"`
	DownloadURL      string `json:"downloadUrl,omitempty"`
	ReportURL        string `json:"reportUrl,omitempty"`
	DiagnosticsURL   string `json:"diagnosticsUrl,omitempty"`
	DiagnosticsCount int    `json:"diagnosticsCount,omitempty"`
	ErrorCode        string `json:"errorCode,omitempty"`
	Error            string `json:"error,omitempty"`
}
