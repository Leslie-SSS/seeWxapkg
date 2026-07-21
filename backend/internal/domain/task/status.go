package task

type TaskStatus string

const (
	TaskQueued             TaskStatus = "queued"
	TaskClassifying        TaskStatus = "classifying"
	TaskDecrypting         TaskStatus = "decrypting"
	TaskUnpacking          TaskStatus = "unpacking"
	TaskNormalizing        TaskStatus = "normalizing"
	TaskRecoveringManifest TaskStatus = "recovering_manifest"
	TaskRecoveringJS       TaskStatus = "recovering_js"
	TaskRecoveringWXML     TaskStatus = "recovering_wxml"
	TaskRecoveringWXSS     TaskStatus = "recovering_wxss"
	TaskFallbackRecovering TaskStatus = "fallback_recovering"
	TaskFormatting         TaskStatus = "formatting"
	TaskVerifying          TaskStatus = "verifying"
	TaskPackaging          TaskStatus = "packaging"
	TaskCompleted          TaskStatus = "completed"
	TaskPartial            TaskStatus = "partial"
	TaskFailed             TaskStatus = "failed"
)
