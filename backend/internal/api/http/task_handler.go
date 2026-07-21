package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/keepbuild/seewxapkg/internal/app"
	"github.com/keepbuild/seewxapkg/internal/domain/task"
	"github.com/keepbuild/seewxapkg/internal/infra/events"
	"github.com/keepbuild/seewxapkg/internal/report"
)

type TaskHandler struct {
	query  *app.TaskQueryService
	broker *events.Broker
}

func NewTaskHandler(query *app.TaskQueryService, broker *events.Broker) *TaskHandler {
	return &TaskHandler{query: query, broker: broker}
}

func (h *TaskHandler) GetTask(c *gin.Context) {
	taskID := c.Param("taskId")
	if !taskIDRegex.MatchString(taskID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务 ID"})
		return
	}

	t, err := h.query.GetTask(c.Request.Context(), taskID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	if isTerminalTaskStatus(t.Status) {
		h.broker.CloseAndRemove(taskID)
	}

	c.JSON(http.StatusOK, ToTaskResponseDTO(t))
}

func (h *TaskHandler) StreamTaskEvents(c *gin.Context) {
	taskID := c.Query("taskId")
	if !taskIDRegex.MatchString(taskID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务 ID"})
		return
	}

	current, err := h.query.GetTask(c.Request.Context(), taskID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	terminalObserved := isTerminalTaskStatus(current.Status)
	defer func() {
		if terminalObserved {
			h.broker.CloseAndRemove(taskID)
		}
	}()

	stream, history, cancel, subscribeErr := h.broker.Subscribe(taskID)
	if subscribeErr != nil && subscribeErr != events.ErrStreamNotFound {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "任务事件流不可用"})
		return
	}
	if cancel != nil {
		defer cancel()
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "private, no-cache, no-store")
	c.Writer.Header().Set("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.Status(http.StatusInternalServerError)
		return
	}

	writeEvent := func(event task.TaskEvent) bool {
		event = sanitizeTaskEvent(event)
		payload, err := json.Marshal(event)
		if err != nil {
			return false
		}
		if _, err := c.Writer.Write([]byte("data: " + string(payload) + "\n\n")); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	lastSnapshot := ""
	historyTerminal := false
	for _, event := range history {
		if isTerminalTaskEvent(event) {
			terminalObserved = true
			historyTerminal = true
		}
		if !writeEvent(event) {
			return
		}
		lastSnapshot = eventSignature(event.Status, event.Percent, event.Message)
	}
	if historyTerminal {
		return
	}

	initial := taskEventFromTask(current)
	if signature := eventSignature(initial.Status, initial.Percent, initial.Message); signature != lastSnapshot {
		if !writeEvent(initial) {
			return
		}
		lastSnapshot = signature
	}
	if isTerminalTaskStatus(current.Status) {
		terminalObserved = true
		return
	}

	pollTicker := time.NewTicker(time.Second)
	keepAliveTicker := time.NewTicker(15 * time.Second)
	defer pollTicker.Stop()
	defer keepAliveTicker.Stop()

	for {
		select {
		case event, ok := <-stream:
			if ok {
				terminalEvent := isTerminalTaskEvent(event)
				if terminalEvent {
					terminalObserved = true
				}
				if !writeEvent(event) {
					return
				}
				lastSnapshot = eventSignature(event.Status, event.Percent, event.Message)
				if terminalEvent {
					return
				}
			} else {
				stream = nil
			}
		case <-pollTicker.C:
			latest, err := h.query.GetTask(c.Request.Context(), taskID)
			if err != nil {
				continue
			}
			event := taskEventFromTask(latest)
			terminal := isTerminalTaskStatus(latest.Status)
			if terminal {
				terminalObserved = true
			}
			signature := eventSignature(event.Status, event.Percent, event.Message)
			if signature != lastSnapshot {
				if !writeEvent(event) {
					return
				}
				lastSnapshot = signature
			}
			if terminal {
				return
			}
		case <-keepAliveTicker.C:
			_, _ = c.Writer.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		case <-c.Request.Context().Done():
			return
		}
	}
}

func taskEventFromTask(t *task.Task) task.TaskEvent {
	eventType := "progress"
	if t.Status == task.TaskCompleted {
		eventType = "complete"
	} else if t.Status == task.TaskPartial {
		eventType = "partial"
	} else if t.Status == task.TaskFailed {
		eventType = "error"
	}
	event := task.TaskEvent{
		Type:             eventType,
		TaskID:           t.ID,
		Stage:            t.CurrentStage,
		Status:           string(t.Status),
		Percent:          t.Progress,
		Message:          report.SanitizeText(t.CurrentMessage),
		DiagnosticsCount: len(t.Diagnostics),
	}
	if t.ArtifactSummary != nil {
		event.FileCount = t.ArtifactSummary.FileCount
		event.DownloadURL = t.ArtifactSummary.DownloadURL
		event.ReportURL = t.ArtifactSummary.ReportURL
		event.DiagnosticsURL = t.ArtifactSummary.DiagnosticsURL
	}
	if t.ErrorCode != nil {
		event.ErrorCode = *t.ErrorCode
	}
	if t.ErrorMessage != nil {
		event.Error = report.SanitizeText(*t.ErrorMessage)
	}
	return event
}

func sanitizeTaskEvent(event task.TaskEvent) task.TaskEvent {
	event.Message = report.SanitizeText(event.Message)
	event.Error = report.SanitizeText(event.Error)
	return event
}

func eventSignature(status string, percent int, message string) string {
	return fmt.Sprintf("%s/%d/%s", status, percent, report.SanitizeText(message))
}

func isTerminalTaskStatus(status task.TaskStatus) bool {
	return status == task.TaskCompleted || status == task.TaskPartial || status == task.TaskFailed
}

func isTerminalTaskEvent(event task.TaskEvent) bool {
	return event.Type == "complete" || event.Type == "partial" || event.Type == "error" ||
		isTerminalTaskStatus(task.TaskStatus(event.Status))
}

func (h *TaskHandler) GetTaskReport(c *gin.Context) {
	taskID := c.Param("taskId")
	if !taskIDRegex.MatchString(taskID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务 ID"})
		return
	}

	reportName := c.Query("name")
	if reportName != "" {
		payload, err := h.query.GetNamedReport(c.Request.Context(), taskID, reportName)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "报告不存在"})
			return
		}
		c.Data(http.StatusOK, "application/json", payload)
		return
	}

	report, err := h.query.GetReport(c.Request.Context(), taskID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "报告不存在"})
		return
	}
	c.JSON(http.StatusOK, report)
}

func (h *TaskHandler) GetTaskDiagnostics(c *gin.Context) {
	taskID := c.Param("taskId")
	if !taskIDRegex.MatchString(taskID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务 ID"})
		return
	}

	diagnostics, err := h.query.GetDiagnostics(c.Request.Context(), taskID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "诊断信息不存在"})
		return
	}
	c.JSON(http.StatusOK, diagnostics)
}

func (h *TaskHandler) GetTaskArtifacts(c *gin.Context) {
	taskID := c.Param("taskId")
	if !taskIDRegex.MatchString(taskID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务 ID"})
		return
	}

	t, err := h.query.GetTask(c.Request.Context(), taskID)
	if err != nil || t.ArtifactSummary == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务产物不存在"})
		return
	}
	c.JSON(http.StatusOK, report.SanitizeArtifactSummary(t.ArtifactSummary))
}
