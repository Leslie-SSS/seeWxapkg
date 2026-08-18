package httpapi

import (
	"log"
	"mime/multipart"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/keepbuild/seewxapkg/internal/app"
	buildversion "github.com/keepbuild/seewxapkg/internal/version"
)

var (
	appIDRegex  = regexp.MustCompile(`^wx[a-f0-9]{16}$`)
	taskIDRegex = regexp.MustCompile(`^[a-f0-9-]+$`)
)

type CompileHandler struct {
	service        *app.CompileService
	maxUploadBytes int64
}

func NewCompileHandler(service *app.CompileService, maxUploadBytes int64) *CompileHandler {
	return &CompileHandler{service: service, maxUploadBytes: maxUploadBytes}
}

func (h *CompileHandler) HealthCheck(c *gin.Context) {
	capabilities, err := h.service.Readiness()
	statusCode := http.StatusOK
	status := "ok"
	response := gin.H{
		"status":       "ok",
		"version":      buildversion.Version,
		"commit":       buildversion.Commit,
		"builtAt":      buildversion.BuiltAt,
		"capabilities": capabilities,
	}
	if err != nil {
		log.Printf("[Health] readiness check failed (%T)", err)
		statusCode = http.StatusServiceUnavailable
		status = "degraded"
		response["error"] = "服务依赖尚未就绪"
	}
	response["status"] = status
	c.JSON(statusCode, response)
}

func (h *CompileHandler) Compile(c *gin.Context) {
	if _, err := h.service.Readiness(); err != nil {
		log.Printf("[Compile] readiness check failed (%T)", err)
		c.JSON(http.StatusServiceUnavailable, CompileResponseDTO{Success: false, Message: "服务依赖尚未就绪，请稍后重试"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.maxUploadBytes+1024)
	defer func() {
		if c.Request.MultipartForm != nil {
			if err := c.Request.MultipartForm.RemoveAll(); err != nil {
				log.Printf("[Compile] multipart temporary-file cleanup failed (%T)", err)
			}
		}
	}()

	dto, file, err := parseCompileRequest(c)
	if err != nil {
		log.Printf("[Compile] parse upload request failed (%T)", err)
		c.JSON(http.StatusBadRequest, CompileResponseDTO{Success: false, Message: "上传请求格式错误，请重新选择文件后重试"})
		return
	}

	if err := validateCompileRequest(dto, file, h.maxUploadBytes); err != nil {
		c.JSON(http.StatusBadRequest, CompileResponseDTO{Success: false, Message: err.Error()})
		return
	}

	task, err := h.service.StartTask(c.Request.Context(), app.StartCompileCommand{
		AppID:           dto.AppID,
		Beautify:        dto.Beautify,
		Decompile:       dto.Decompile,
		RemoveGuideHTML: dto.RemoveGuideHTML,
		File:            file,
	})
	if err != nil {
		log.Printf("[Compile] task creation failed (%T)", err)
		c.JSON(http.StatusInternalServerError, CompileResponseDTO{Success: false, Message: "任务创建失败，请稍后重试"})
		return
	}

	c.JSON(http.StatusOK, CompileResponseDTO{
		Success: true,
		TaskID:  task.ID,
		Message: "task created",
	})
}

func parseCompileRequest(c *gin.Context) (CompileRequestDTO, *multipart.FileHeader, error) {
	dto := CompileRequestDTO{
		AppID:           c.PostForm("appId"),
		Beautify:        c.PostForm("beautify") == "true",
		Decompile:       c.PostForm("decompile") == "true",
		RemoveGuideHTML: removeGuideHTML(c.PostForm("removeGuideHtml")),
	}
	file, err := c.FormFile("file")
	return dto, file, err
}

// removeGuideHTML defaults to true so the 4.x runtime-guide `.html` scaffolds
// are dropped unless the client explicitly sends "false".
func removeGuideHTML(raw string) bool {
	return raw != "false"
}

func validateCompileRequest(dto CompileRequestDTO, file *multipart.FileHeader, maxUploadBytes int64) error {
	if dto.AppID != "" && !appIDRegex.MatchString(dto.AppID) {
		return httpError("AppID 格式错误，应为 wx 开头加 16 位十六进制字符")
	}
	if file == nil {
		return httpError("文件是必需的")
	}
	if file.Size > maxUploadBytes {
		return httpError("文件过大，超过服务限制")
	}
	if !strings.HasSuffix(strings.ToLower(file.Filename), ".wxapkg") {
		return httpError("文件必须是 .wxapkg 格式")
	}
	return nil
}
