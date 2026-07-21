package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/keepbuild/seewxapkg/internal/app"
)

type DownloadHandler struct {
	query *app.TaskQueryService
}

func NewDownloadHandler(query *app.TaskQueryService) *DownloadHandler {
	return &DownloadHandler{query: query}
}

func (h *DownloadHandler) DownloadArtifacts(c *gin.Context) {
	taskID := c.Param("taskId")
	if !taskIDRegex.MatchString(taskID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务 ID"})
		return
	}

	zipPath, err := h.query.ResolveReadyZipPath(c.Request.Context(), taskID)
	if err != nil || zipPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在或尚未生成下载包"})
		return
	}
	info, err := os.Stat(zipPath)
	if err != nil || info.IsDir() {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在或尚未生成下载包"})
		return
	}

	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Disposition", "attachment; filename="+filepath.Base(zipPath))
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Length", strconv.FormatInt(info.Size(), 10))
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return
	}
	c.File(zipPath)
}
