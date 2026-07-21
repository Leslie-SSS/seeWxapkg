package main

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestLoggerMiddlewareDoesNotPersistRequestSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(loggerMiddleware())
	router.GET("/api/tasks/:taskId/report", func(c *gin.Context) { c.Status(http.StatusOK) })

	var output bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	originalPrefix := log.Prefix()
	log.SetOutput(&output)
	log.SetFlags(0)
	log.SetPrefix("")
	defer func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
		log.SetPrefix(originalPrefix)
	}()

	request := httptest.NewRequest(http.MethodGet, "/api/tasks/secret-task-id/report?name=private&appId=wx0123456789abcdef", nil)
	request.RemoteAddr = "203.0.113.10:4321"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	logged := output.String()
	for _, secret := range []string{"secret-task-id", "private", "wx0123456789abcdef", "203.0.113.10"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("request log retained sensitive value %q: %s", secret, logged)
		}
	}
	if !strings.Contains(logged, "[GET] /api/tasks/:taskId/report 200") {
		t.Fatalf("request log lost useful route metadata: %s", logged)
	}
}

func TestRecoveryMiddlewareDoesNotLogRequestOrPanicSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(privacyRecoveryMiddleware())
	router.GET("/api/events", func(_ *gin.Context) {
		panic("panic contains wx0123456789abcdef")
	})

	var output bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&output)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	}()

	request := httptest.NewRequest(http.MethodGet, "/api/events?taskId=secret-task-id", nil)
	request.RemoteAddr = "203.0.113.10:4321"
	request.Header.Set("X-Private-Metadata", "private-header")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	logged := output.String()
	for _, secret := range []string{"secret-task-id", "wx0123456789abcdef", "203.0.113.10", "private-header"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("recovery log retained sensitive value %q: %s", secret, logged)
		}
	}
	if !strings.Contains(logged, "[Recovery]") {
		t.Fatalf("recovery log lost the safe failure signal: %s", logged)
	}
}

func TestCORSMiddlewareDefaultsToSameOriginOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(corsMiddleware(nil))
	router.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	request.Header.Set("Origin", "https://example.test")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("default configuration must not enable cross-origin access, got %q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("wildcard CORS must not allow credentials, got %q", got)
	}
}

func TestShutdownServerDrainsHTTPBeforeCancelingWorkers(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseRequest
		w.WriteHeader(http.StatusNoContent)
	}))
	testServer.Start()
	defer testServer.Close()

	requestDone := make(chan error, 1)
	go func() {
		response, err := testServer.Client().Get(testServer.URL)
		if err == nil {
			err = response.Body.Close()
		}
		requestDone <- err
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}

	workerCtx, workerCancel := context.WithCancel(context.Background())
	workersWaited := make(chan struct{})
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- shutdownServer(testServer.Config, workerCancel, func() {
			<-workerCtx.Done()
			close(workersWaited)
		}, time.Second)
	}()
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned before active request drained: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	select {
	case <-workerCtx.Done():
		t.Fatal("workers were canceled before HTTP requests drained")
	default:
	}

	close(releaseRequest)
	if err := <-shutdownDone; err != nil {
		t.Fatalf("shutdownServer returned error: %v", err)
	}
	if err := <-requestDone; err != nil {
		t.Fatalf("active request failed during drain: %v", err)
	}
	select {
	case <-workerCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("workers were not canceled after HTTP shutdown")
	}
	select {
	case <-workersWaited:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not wait for workers")
	}
}

func TestCORSMiddlewareAllowsOnlyConfiguredOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(corsMiddleware([]string{"https://allowed.example"}))
	router.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })

	allowedRequest := httptest.NewRequest(http.MethodGet, "/health", nil)
	allowedRequest.Header.Set("Origin", "https://allowed.example")
	allowedResponse := httptest.NewRecorder()
	router.ServeHTTP(allowedResponse, allowedRequest)
	if got := allowedResponse.Header().Get("Access-Control-Allow-Origin"); got != "https://allowed.example" {
		t.Fatalf("configured origin was not allowed: %q", got)
	}
	if got := allowedResponse.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("expected Origin in Vary, got %q", got)
	}

	deniedRequest := httptest.NewRequest(http.MethodGet, "/health", nil)
	deniedRequest.Header.Set("Origin", "https://denied.example")
	deniedResponse := httptest.NewRecorder()
	router.ServeHTTP(deniedResponse, deniedRequest)
	if got := deniedResponse.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected cross-origin permission: %q", got)
	}
}

func TestCORSMiddlewareHandlesPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(corsMiddleware([]string{"https://allowed.example"}))
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/compile", nil)
	request.Header.Set("Origin", "https://allowed.example")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", response.Code)
	}
	if got := response.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST, OPTIONS" {
		t.Fatalf("unexpected allowed methods: %q", got)
	}
}
