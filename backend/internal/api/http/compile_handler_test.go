package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/keepbuild/seewxapkg/internal/app"
	"github.com/keepbuild/seewxapkg/internal/config"
	"github.com/keepbuild/seewxapkg/internal/domain/task"
	"github.com/keepbuild/seewxapkg/internal/infra/events"
)

type createFailingRepository struct {
	err error
}

func (r createFailingRepository) Create(context.Context, *task.Task) error {
	return r.err
}

func (r createFailingRepository) Update(context.Context, *task.Task) error {
	return nil
}

func (r createFailingRepository) Get(context.Context, string) (*task.Task, error) {
	return nil, errors.New("task not found")
}

func TestHealthCheckDoesNotExposeReadinessPath(t *testing.T) {
	missingDir := filepath.Join("/app", "private-health-"+filepath.Base(t.TempDir()))
	service := app.NewCompileService(&config.Config{
		TempDir:   missingDir,
		OutputDir: t.TempDir(),
	}, createFailingRepository{}, events.NewBroker(), nil)
	handler := NewCompileHandler(service, 1024)
	router := newCompileTestRouter(handler)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	router.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("health status = %d, want %d: %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
	assertGenericServerFailure(t, response.Body.String(), "error", "服务依赖尚未就绪", missingDir)
}

func TestCompileDoesNotExposeTaskCreationPath(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := t.TempDir()
	rawCause := errors.New(`create /data/tasks/task-1; write /data/output/task-1.zip; load /Users/person/app.js; start /workspace/bin; windows C:\private\task.db`)
	service := app.NewCompileService(&config.Config{
		TempDir:   tempDir,
		OutputDir: outputDir,
	}, createFailingRepository{err: rawCause}, events.NewBroker(), nil)
	handler := NewCompileHandler(service, 1024)
	router := newCompileTestRouter(handler)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	filePart, err := writer.CreateFormFile("file", "sample.wxapkg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := filePart.Write([]byte("test package")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/compile", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("compile status = %d, want %d: %s", response.Code, http.StatusInternalServerError, response.Body.String())
	}
	assertGenericServerFailure(t, response.Body.String(), "message", "任务创建失败，请稍后重试", rawCause.Error())
}

func TestCompileDoesNotEchoMultipartParserErrors(t *testing.T) {
	service := app.NewCompileService(&config.Config{
		TempDir:   t.TempDir(),
		OutputDir: t.TempDir(),
	}, createFailingRepository{}, events.NewBroker(), nil)
	handler := NewCompileHandler(service, 1024)
	router := newCompileTestRouter(handler)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/compile", strings.NewReader("not multipart"))
	request.Header.Set("Content-Type", "text/plain")
	router.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("compile status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	assertGenericServerFailure(t, response.Body.String(), "message", "上传请求格式错误，请重新选择文件后重试", "request Content-Type")
}

func TestCompileRemovesMultipartTemporaryFiles(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	service := app.NewCompileService(&config.Config{
		TempDir:   t.TempDir(),
		OutputDir: t.TempDir(),
	}, createFailingRepository{err: errors.New("expected create failure")}, events.NewBroker(), nil)
	handler := NewCompileHandler(service, 4096)
	router := newCompileTestRouter(handler)
	router.MaxMultipartMemory = 1

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	filePart, err := writer.CreateFormFile("file", "sample.wxapkg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := filePart.Write(bytes.Repeat([]byte("x"), 2048)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/compile", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(httptest.NewRecorder(), request)

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("multipart temporary files were retained: %v", entries)
	}
}

func newCompileTestRouter(handler *CompileHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/health", handler.HealthCheck)
	router.POST("/compile", handler.Compile)
	return router
}

func assertGenericServerFailure(t *testing.T, body, messageField, expectedMessage string, privateValues ...string) {
	t.Helper()
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode response: %v: %s", err, body)
	}
	serialized := body
	if message, _ := payload[messageField].(string); message != expectedMessage {
		t.Fatalf("%s = %q, want %q: %s", messageField, message, expectedMessage, serialized)
	}
	for _, privateValue := range privateValues {
		if privateValue != "" && strings.Contains(serialized, privateValue) {
			t.Fatalf("response exposed private value %q: %s", privateValue, serialized)
		}
	}
	for _, prefix := range []string{"/app/", "/data/", "/Users/", "/mnt/", "/srv/", "/workspace/", `C:\`} {
		if strings.Contains(serialized, prefix) {
			t.Fatalf("response exposed internal path prefix %q: %s", prefix, serialized)
		}
	}
}

func TestRemoveGuideHTMLDefaultsOn(t *testing.T) {
	if !removeGuideHTML("") {
		t.Fatal("omitted field must default to removing guide html")
	}
	if !removeGuideHTML("true") {
		t.Fatal("explicit true must remove guide html")
	}
	if removeGuideHTML("false") {
		t.Fatal("explicit false must keep guide html")
	}
}
