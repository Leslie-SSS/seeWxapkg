package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/keepbuild/seewxapkg/internal/app"
)

type githubStarsProviderStub struct {
	result app.GitHubStars
	err    error
	calls  int
}

func (s *githubStarsProviderStub) Get(context.Context) (app.GitHubStars, error) {
	s.calls++
	return s.result, s.err
}

func TestGitHubStarsHandlerReturnsMinimalResultAndIgnoresTargetParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &githubStarsProviderStub{result: app.GitHubStars{Count: 125, Stale: true}}
	engine := gin.New()
	api := engine.Group("/api", privateResponseHeaders)
	api.GET("/github/stars", NewGitHubStarsHandler(provider).Get)

	request := httptest.NewRequest(http.MethodGet, "/api/github/stars?owner=attacker&repo=private&url=http://127.0.0.1", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK || provider.calls != 1 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, provider.calls, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload) != 2 || payload["stars"] != float64(125) || payload["stale"] != true {
		t.Fatalf("unexpected public response: %#v", payload)
	}
	if got := response.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestGitHubStarsHandlerReturnsServiceUnavailableWithoutCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logs bytes.Buffer
	originalLogWriter := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(originalLogWriter)

	provider := &githubStarsProviderStub{err: errors.New("upstream detail must stay private")}
	engine := gin.New()
	engine.GET("/api/github/stars", NewGitHubStarsHandler(provider).Get)

	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/github/stars", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Retry-After") != "60" {
		t.Fatalf("Retry-After = %q", response.Header().Get("Retry-After"))
	}
	if response.Body.String() != "{\"error\":\"暂时无法获取 GitHub Star 数，请稍后再试\"}" {
		t.Fatalf("unexpected public error: %s", response.Body.String())
	}
	if logs.Len() != 0 {
		t.Fatalf("handler amplified an upstream outage into per-request logs: %q", logs.String())
	}
}
