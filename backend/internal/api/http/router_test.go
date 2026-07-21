package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAPIRoutesDisableSensitiveResponseCaching(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	router := NewRouter(&CompileHandler{}, &TaskHandler{}, &DownloadHandler{}, &GitHubStarsHandler{})
	router.RegisterRoutes(engine)

	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/download/invalid!", nil))
	if got := response.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q, want private, no-store", got)
	}
	if got := response.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma = %q, want no-cache", got)
	}
}
