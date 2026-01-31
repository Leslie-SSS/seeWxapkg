package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/keepbuild/seewxapkg/internal/config"
	"github.com/keepbuild/seewxapkg/internal/model"
	"github.com/keepbuild/seewxapkg/internal/service"
)

// 验证正则
var (
	taskIdRegex    = regexp.MustCompile(`^[a-f0-9-]+$`)
	appIdRegex     = regexp.MustCompile(`^wx[a-f0-9]{16}$`)
	maxFileSize    = int64(100 * 1024 * 1024) // 100MB
)

type Handler struct {
	cfg    *config.Config
	tasks  map[string]*model.Task
	events map[string]chan model.ProgressEvent
}

func NewHandler(cfg *config.Config) *Handler {
	return &Handler{
		cfg:    cfg,
		tasks:  make(map[string]*model.Task),
		events: make(map[string]chan model.ProgressEvent),
	}
}

// CompileResponse 编译响应
type CompileResponse struct {
	Success     bool   `json:"success"`
	TaskID      string `json:"taskId"`
	Message     string `json:"message"`
	DownloadURL string `json:"downloadUrl,omitempty"`
}

// HealthCheck 健康检查
func (h *Handler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"version": "1.0.0",
	})
}

// Compile 上传并编译
func (h *Handler) Compile(c *gin.Context) {
	// 限制请求大小
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxFileSize+1024)

	// 解析表单
	appID := c.PostForm("appId")
	beautify := c.PostForm("beautify") == "true"

	// 验证 AppID 格式（如果提供）
	if appID != "" && !appIdRegex.MatchString(appID) {
		c.JSON(http.StatusBadRequest, CompileResponse{
			Success: false,
			Message: "AppID 格式错误，应为 wx 开头加 16 位十六进制字符",
		})
		return
	}

	// 获取上传的文件
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, CompileResponse{
			Success: false,
			Message: "文件是必需的",
		})
		return
	}

	// 验证文件大小
	if file.Size > maxFileSize {
		c.JSON(http.StatusBadRequest, CompileResponse{
			Success: false,
			Message: fmt.Sprintf("文件过大，最大支持 %d MB", maxFileSize/(1024*1024)),
		})
		return
	}

	// 验证文件扩展名
	if !strings.HasSuffix(strings.ToLower(file.Filename), ".wxapkg") {
		c.JSON(http.StatusBadRequest, CompileResponse{
			Success: false,
			Message: "文件必须是 .wxapkg 格式",
		})
		return
	}

	// 创建任务
	taskID := uuid.New().String()
	task := &model.Task{
		ID:        taskID,
		AppID:     appID,
		Beautify:  beautify,
		Status:    "pending",
		Progress:  0,
		CreatedAt: time.Now(),
	}
	h.tasks[taskID] = task

	// 创建事件通道
	eventChan := make(chan model.ProgressEvent, 100)
	h.events[taskID] = eventChan

	// 返回任务ID，客户端可以通过 SSE 获取进度
	c.JSON(http.StatusOK, CompileResponse{
		Success: true,
		TaskID:  taskID,
		Message: "Task created",
	})

	// 异步处理
	go h.processFile(task, file, eventChan)
}

// ProcessEvents SSE 事件流
func (h *Handler) ProcessEvents(c *gin.Context) {
	taskID := c.Query("taskId")
	if taskID == "" {
		c.Status(http.StatusBadRequest)
		return
	}

	// 验证 taskId 格式
	if !taskIdRegex.MatchString(taskID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务 ID"})
		return
	}

	eventChan, exists := h.events[taskID]
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}

	// 设置 SSE 头
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")

	// 发送事件
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.Status(http.StatusInternalServerError)
		return
	}

	// 发送现有事件
	for {
		select {
		case event := <-eventChan:
			data, _ := json.Marshal(event)
			fmt.Fprintf(c.Writer, "data: %s\n\n", data)
			flusher.Flush()

			if event.Type == "complete" || event.Type == "error" {
				close(eventChan)
				delete(h.events, taskID)
				return
			}
		case <-c.Request.Context().Done():
			return
		}
	}
}

// Download 下载结果
func (h *Handler) Download(c *gin.Context) {
	taskID := c.Param("taskId")

	// 验证 taskId 格式（防止路径遍历攻击）
	if !taskIdRegex.MatchString(taskID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务 ID"})
		return
	}

	task, exists := h.tasks[taskID]
	if !exists || task.Status != "completed" {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在或未完成"})
		return
	}

	zipPath := filepath.Join(h.cfg.OutputDir, taskID+".zip")
	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "文件不存在"})
		return
	}

	// 设置下载头
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.zip", taskID))
	c.Header("Content-Type", "application/zip")

	c.File(zipPath)
}

// processFile 处理文件
func (h *Handler) processFile(task *model.Task, fileHeader *multipart.FileHeader, eventChan chan<- model.ProgressEvent) {
	// 发送开始事件
	eventChan <- model.ProgressEvent{
		Type:    "progress",
		Stage:   "uploading",
		Percent: 0,
		Message: "正在处理文件...",
	}

	// 打开上传的文件
	src, err := fileHeader.Open()
	if err != nil {
		h.sendError(eventChan, "打开文件失败: "+err.Error())
		return
	}
	defer src.Close()

	// 读取文件内容
	data, err := io.ReadAll(src)
	if err != nil {
		h.sendError(eventChan, "读取文件失败: "+err.Error())
		return
	}

	eventChan <- model.ProgressEvent{
		Type:    "progress",
		Stage:   "uploaded",
		Percent: 10,
		Message: "文件已上传",
	}

	// 检查是否是加密文件
	isEncrypted := service.IsEncrypted(data)

	// 记录文件类型信息（用于调试）
	if len(data) >= 6 {
		header := string(data[:6])
		firstByte := data[0]
		fileType := "unknown"
		if header == "V1MMWX" {
			fileType = "encrypted"
		} else if firstByte == 0xBE {
			fileType = "standard"
		}
		log.Printf("[Task %s] File type: %s, size: %d bytes, AppID provided: %v",
			task.ID, fileType, len(data), task.AppID != "")
	}

	// 解密
	eventChan <- model.ProgressEvent{
		Type:    "progress",
		Stage:   "decrypting",
		Percent: 20,
		Message: "正在解密...",
	}

	var decryptedData []byte

	// 如果是加密文件但没有 AppID，提示用户
	if isEncrypted && task.AppID == "" {
		h.sendError(eventChan, "这是一个加密的 wxapkg 文件，需要提供小程序 AppID 才能解密。请在小程序页面路径中找到 AppID（格式：wxXXXXXXXXXXXXXXXX）")
		return
	}

	if task.AppID != "" {
		log.Printf("[Task %s] Attempting decryption with AppID", task.ID)
		decryptedData, err = service.DecryptWxapkg(data, task.AppID)
		if err != nil {
			log.Printf("[Task %s] Decryption error: %v", task.ID, err)
			// 解密失败
			if isEncrypted {
				h.sendError(eventChan, "解密失败：可能是 AppID 不正确。请检查 AppID 是否与该小程序匹配（格式：wxXXXXXXXXXXXXXXXX）")
				return
			}
			// 如果不是加密文件，尝试直接解包
			eventChan <- model.ProgressEvent{
				Type:    "progress",
				Stage:   "decrypting",
				Percent: 25,
				Message: "无需解密，直接解包...",
			}
			if !service.IsDecrypted(data) {
				h.sendError(eventChan, "文件格式错误，无法解析。请确保上传的是有效的 .wxapkg 文件")
				return
			}
			decryptedData = data
		} else {
			log.Printf("[Task %s] Decryption completed, validating result...", task.ID)
			// 解密成功，验证结果
			if !service.IsDecrypted(decryptedData) {
				log.Printf("[Task %s] Decrypted data validation failed", task.ID)
				// 解密后的数据格式不正确，可能是 AppID 错误
				if isEncrypted {
					h.sendError(eventChan, "解密后的数据格式不正确。这通常意味着 AppID 不正确，请仔细检查 AppID 是否与该小程序完全匹配")
					return
				}
				// 原始文件不是加密的，使用原始数据
				if service.IsDecrypted(data) {
					log.Printf("[Task %s] Using original data (not encrypted)", task.ID)
					decryptedData = data
				} else {
					h.sendError(eventChan, "文件格式错误，无法解析")
					return
				}
			} else {
				log.Printf("[Task %s] Decrypted data validated successfully", task.ID)
			}
		}
	} else {
		// 无 AppID，直接使用原始数据
		if service.IsDecrypted(data) {
			decryptedData = data
			eventChan <- model.ProgressEvent{
				Type:    "progress",
				Stage:   "decrypting",
				Percent: 30,
				Message: "检测到未加密文件，直接解包...",
			}
		} else {
			eventChan <- model.ProgressEvent{
				Type:    "progress",
				Stage:   "decrypting",
				Percent: 30,
				Message: "未提供 AppID，尝试直接解包...",
			}
			decryptedData = data
		}
	}

	eventChan <- model.ProgressEvent{
		Type:    "progress",
		Stage:   "decrypted",
		Percent: 40,
		Message: "解密完成",
	}

	// 创建临时目录
	tempDir := filepath.Join(h.cfg.TempDir, task.ID)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		h.sendError(eventChan, "创建临时目录失败: "+err.Error())
		return
	}
	defer os.RemoveAll(tempDir)

	// 解包
	eventChan <- model.ProgressEvent{
		Type:    "progress",
		Stage:   "unpacking",
		Percent: 50,
		Message: "正在解包...",
	}

	// 记录解密后的数据头部信息
	log.Printf("[Task %s] Decrypted data: first 20 bytes = %v", task.ID, decryptedData[:20])

	result, err := service.UnpackWxapkg(decryptedData, tempDir, task.Beautify)
	if err != nil {
		log.Printf("[Task %s] Unpack failed: %v", task.ID, err)
		h.sendError(eventChan, "解包失败: "+err.Error())
		return
	}

	task.FileCount = result.FileCount

	eventChan <- model.ProgressEvent{
		Type:    "progress",
		Stage:   "unpacked",
		Percent: 70,
		Message: fmt.Sprintf("解包完成，共 %d 个文件", result.FileCount),
	}

	// 打包成 ZIP
	eventChan <- model.ProgressEvent{
		Type:    "progress",
		Stage:   "packing",
		Percent: 80,
		Message: "正在打包...",
	}

	zipPath := filepath.Join(h.cfg.OutputDir, task.ID+".zip")
	if err := service.CreateZip(tempDir, zipPath); err != nil {
		h.sendError(eventChan, "打包失败: "+err.Error())
		return
	}

	// 更新任务状态
	task.Status = "completed"
	task.Progress = 100
	now := time.Now()
	task.CompletedAt = &now

	// 发送完成事件
	eventChan <- model.ProgressEvent{
		Type:        "complete",
		Stage:       "completed",
		Percent:     100,
		Message:     "处理完成！",
		FileCount:   result.FileCount,
		TaskID:      task.ID,
		DownloadURL: "/api/download/" + task.ID,
	}
}

// sendError 发送错误事件
func (h *Handler) sendError(eventChan chan<- model.ProgressEvent, message string) {
	eventChan <- model.ProgressEvent{
		Type:    "error",
		Message: message,
		Error:   message,
	}
}

// RegisterRoutes 注册路由
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	// API 路由
	api := r.Group("/api")
	{
		api.GET("/health", h.HealthCheck)
		api.POST("/compile", h.Compile)
		api.GET("/events", h.ProcessEvents)
		api.GET("/download/:taskId", h.Download)
	}

	// 静态文件服务（用于前端开发）
	r.Static("/assets", "./frontend/dist/assets")
	r.GET("/", func(c *gin.Context) {
		c.File("./frontend/dist/index.html")
	})
}
