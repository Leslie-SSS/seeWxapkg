package beautify

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestServiceGetFileType(t *testing.T) {
	svc := &Service{}

	testCases := []struct {
		filename string
		want     string
	}{
		{filename: "index.js", want: "javascript"},
		{filename: "logic.wxs", want: "javascript"},
		{filename: "index.wxml", want: "html"},
		{filename: "index.wxss", want: "css"},
		{filename: "index.json", want: "json"},
		{filename: "asset.bin", want: "unknown"},
	}

	for _, tc := range testCases {
		t.Run(tc.filename, func(t *testing.T) {
			if got := svc.getFileType(tc.filename); got != tc.want {
				t.Fatalf("getFileType(%q) = %q, want %q", tc.filename, got, tc.want)
			}
		})
	}
}

func TestBeautifyReturnsOriginalWhenTooLarge(t *testing.T) {
	input := []byte("const x = 1;")
	svc := &Service{
		enabled:        true,
		maxFileSize:    len(input) - 1,
		circuitBreaker: NewCircuitBreaker(),
		stopCheck:      make(chan struct{}),
	}
	svc.setHealthy(true)

	if got := svc.Beautify(input, "index.js"); string(got) != string(input) {
		t.Fatalf("Beautify should return original content for oversized input")
	}
}

func TestBeautifyReturnsOriginalWhenUnhealthy(t *testing.T) {
	input := []byte("const x = 1;")
	svc := &Service{
		enabled:        true,
		maxFileSize:    1024,
		circuitBreaker: NewCircuitBreaker(),
		stopCheck:      make(chan struct{}),
	}

	if got := svc.Beautify(input, "index.js"); string(got) != string(input) {
		t.Fatalf("Beautify should return original content when unhealthy")
	}
}

func TestBeautifyReturnsOriginalOnSidecarFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Response{
			Success: false,
			Error:   "boom",
		})
	}))
	defer server.Close()

	input := []byte("const x = 1;")
	svc := &Service{
		enabled:        true,
		serverURL:      server.URL,
		httpClient:     server.Client(),
		timeout:        time.Second,
		maxFileSize:    1024,
		circuitBreaker: NewCircuitBreaker(),
		stopCheck:      make(chan struct{}),
	}
	svc.setHealthy(true)

	if got := svc.Beautify(input, "index.js"); string(got) != string(input) {
		t.Fatalf("Beautify should return original content when sidecar reports failure")
	}

	stats := svc.circuitBreaker.GetStats()
	if stats["failures"].(int) != 1 {
		t.Fatalf("expected circuit breaker failures to increment, got %+v", stats)
	}
}

func TestDisabledServiceStopIsSafe(t *testing.T) {
	svc := newDisabledService()

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop should be idempotent: %v", err)
	}
}

func TestServiceSmokeStartsNodeSidecar(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	cfg := ConfigFromParams(true, 3, 8*1024*1024, 5, false)
	cfg.BeautifyDir = wd
	cfg.ServerPort = freePort(t)

	svc, err := NewService(cfg)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	defer func() {
		if err := svc.Stop(); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	}()

	if !svc.enabled {
		t.Fatalf("expected enabled service, got disabled")
	}
	if !svc.IsHealthy() {
		t.Fatalf("expected healthy service")
	}

	input := []byte(`Page({data:{list:[]},onLoad:function(e){var t=this;e.a.forEach(function(e){console.log(e)})}})`)
	output := string(svc.Beautify(input, "index.js"))

	wantSnippets := []string{
		"onLoad: function (e)",
		"var t = this;",
		"function (e)",
	}

	for _, snippet := range wantSnippets {
		if !strings.Contains(output, snippet) {
			t.Fatalf("expected output to contain %q, got:\n%s", snippet, output)
		}
	}
}

func TestUnhealthyServiceDoesNotConsumeHalfOpenProbe(t *testing.T) {
	breaker := NewCircuitBreakerWithConfig(CircuitBreakerConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		Timeout:          time.Millisecond,
	})
	breaker.RecordFailure()
	breaker.lastFailure = time.Now().Add(-time.Second)
	service := &Service{
		enabled:        true,
		maxFileSize:    1024,
		circuitBreaker: breaker,
		healthy:        false,
	}

	result := service.BeautifyDetailed([]byte("const x=1"), "index.js")
	if result.Status != "skipped" || result.Warning != "formatter unavailable" {
		t.Fatalf("unexpected result: %#v", result)
	}
	breaker.mu.RLock()
	state := breaker.state
	breaker.mu.RUnlock()
	if state != StateOpen {
		t.Fatalf("circuit state = %s, want open", state)
	}
}

func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port
}
