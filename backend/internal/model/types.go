package model

import "time"

// CompileRequest 编译请求
type CompileRequest struct {
	AppID    string `json:"appId"`
	Beautify bool   `json:"beautify"`
}

// ProgressEvent 进度事件
type ProgressEvent struct {
	Type    string  `json:"type"`    // progress, complete, error
	Stage   string  `json:"stage"`   // uploading, decrypting, unpacking, beautifying, packing
	Percent int     `json:"percent"`
	Message string  `json:"message"`
	FileCount int   `json:"fileCount,omitempty"`
	TaskID   string `json:"taskId,omitempty"`
	DownloadURL string `json:"downloadUrl,omitempty"`
	Error    string  `json:"error,omitempty"`
}

// Task 任务
type Task struct {
	ID          string
	AppID       string
	Beautify    bool
	Status      string // pending, processing, completed, failed
	Progress    int
	Message     string
	FileCount   int
	CreatedAt   time.Time
	CompletedAt *time.Time
	ErrorMsg    string
}

// FileEntry 文件条目
type FileEntry struct {
	Name   string
	Offset uint32
	Size   uint32
}
