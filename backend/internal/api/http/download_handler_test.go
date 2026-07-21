package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/keepbuild/seewxapkg/internal/app"
	"github.com/keepbuild/seewxapkg/internal/config"
	"github.com/keepbuild/seewxapkg/internal/domain/task"
	"github.com/keepbuild/seewxapkg/internal/infra/persistence"
)

func TestDownloadRejectsOrphanArchiveForFailedTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	outputDir := t.TempDir()
	repo := persistence.NewMemoryTaskRepo()
	taskID := "11111111-1111-4111-8111-111111111111"
	current := &task.Task{
		ID:     taskID,
		Status: task.TaskFailed,
		ArtifactSummary: &task.ArtifactSummary{
			DownloadReady: true,
		},
	}
	if err := repo.Create(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, taskID+".zip"), []byte("orphan"), 0644); err != nil {
		t.Fatal(err)
	}

	handler := NewDownloadHandler(app.NewTaskQueryService(&config.Config{OutputDir: outputDir}, repo))
	router := gin.New()
	router.GET("/api/download/:taskId", handler.DownloadArtifacts)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/download/"+taskID, nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("orphan archive was publicly downloadable: status=%d body=%s", response.Code, response.Body.String())
	}
}
