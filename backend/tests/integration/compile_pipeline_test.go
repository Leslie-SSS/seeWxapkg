package integration

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	httpapi "github.com/keepbuild/seewxapkg/internal/api/http"
	"github.com/keepbuild/seewxapkg/internal/app"
	"github.com/keepbuild/seewxapkg/internal/config"
	"github.com/keepbuild/seewxapkg/internal/infra/events"
	"github.com/keepbuild/seewxapkg/internal/infra/persistence"
	"github.com/keepbuild/seewxapkg/internal/infra/queue"
	"github.com/keepbuild/seewxapkg/internal/service"
	"github.com/keepbuild/seewxapkg/tests/testutil"
)

type compileResponse struct {
	TaskID string `json:"taskId"`
}

type taskResponse struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Artifacts *struct {
		FileCount       int               `json:"fileCount"`
		Files           []taskArtifactDTO `json:"files"`
		SourceBreakdown map[string]int    `json:"sourceBreakdown"`
	} `json:"artifacts"`
}

type taskArtifactDTO struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

func TestCompilePipelineStandardPackage(t *testing.T) {
	env := newTestEnv(t, true)
	data := testutil.MustBuildWxapkg(map[string]string{
		"app.json":              `{"pages":["pages/home/index"]}`,
		"app.js":                `App({})`,
		"app.wxss":              `page {}`,
		"pages/home/index.js":   `Page({})`,
		"pages/home/index.wxml": `<view>home</view>`,
		"pages/home/index.wxss": `.home {}`,
		"pages/home/index.json": `{"navigationBarTitleText":"Home"}`,
	})

	task := env.compileAndWait(t, data, map[string]string{"decompile": "true"})
	if task.Status != "completed" {
		t.Fatalf("expected completed, got %s", task.Status)
	}
	assertArtifactCountsConsistent(t, task)
}

func assertArtifactCountsConsistent(t *testing.T, current taskResponse) {
	t.Helper()
	if current.Artifacts == nil {
		t.Fatal("missing artifact summary")
	}
	if current.Artifacts.FileCount != len(current.Artifacts.Files) {
		t.Fatalf("fileCount=%d files=%d", current.Artifacts.FileCount, len(current.Artifacts.Files))
	}
	total := 0
	for _, count := range current.Artifacts.SourceBreakdown {
		total += count
	}
	if total != current.Artifacts.FileCount {
		t.Fatalf("sourceBreakdown total=%d fileCount=%d", total, current.Artifacts.FileCount)
	}
}

func TestCompilePipelineWeChat4xPackage(t *testing.T) {
	env := newTestEnv(t, true)
	data := testutil.MustBuildWxapkg(map[string]string{
		"app-config.json": `{"pages":["pages/home/index"],"global":{"window":{"navigationBarTitleText":"Demo"}}}`,
		"app-service.js":  `var __wxAppCode__ = {};`,
		"page-frame.html": `<template name="p"><view>frame</view></template>`,
		"app-wxss.js":     `setCssToHead([],undefined,{path:"app.wxss"});`,
	})

	task := env.compileAndWait(t, data, map[string]string{"decompile": "true"})
	if task.Status != "partial" {
		t.Fatalf("expected partial, got %s", task.Status)
	}
	env.assertReportStatus(t, task.ID, "partial")
}

func TestCompilePipelinePartialRecovery(t *testing.T) {
	env := newTestEnv(t, false)
	data := testutil.MustBuildWxapkg(map[string]string{
		"app-config.json": `{"pages":["pages/home/index"]}`,
		"page-frame.html": `<template name="p"><view>frame</view></template>`,
	})

	task := env.compileAndWait(t, data, map[string]string{"decompile": "true"})
	if task.Status != "partial" {
		t.Fatalf("expected partial, got %s", task.Status)
	}
	env.assertReportStatus(t, task.ID, "partial")
}

func TestCompilePipelineBrokenPackage(t *testing.T) {
	env := newTestEnv(t, true)
	task := env.compileAndWait(t, []byte("broken"), map[string]string{"decompile": "false"})
	if task.Status != "failed" {
		t.Fatalf("expected failed, got %s", task.Status)
	}
}

func TestDownloadHeadReturnsArchiveMetadata(t *testing.T) {
	env := newTestEnv(t, true)
	data := testutil.MustBuildWxapkg(map[string]string{
		"app.json":              `{"pages":["pages/home/index"]}`,
		"app.js":                `App({})`,
		"app.wxss":              `page {}`,
		"pages/home/index.js":   `Page({})`,
		"pages/home/index.wxml": `<view>home</view>`,
		"pages/home/index.wxss": `.home {}`,
		"pages/home/index.json": `{"navigationBarTitleText":"Home"}`,
	})

	task := env.compileAndWait(t, data, map[string]string{"decompile": "true"})
	if task.Status != "completed" {
		t.Fatalf("expected completed, got %s", task.Status)
	}

	req := httptest.NewRequest(http.MethodHead, "/api/download/"+task.ID, nil)
	resp := httptest.NewRecorder()
	env.router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if resp.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("expected zip content-type, got %q", resp.Header().Get("Content-Type"))
	}
	if resp.Header().Get("Content-Length") == "" {
		t.Fatalf("expected content-length header")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/download/"+task.ID, nil)
	getResp := httptest.NewRecorder()
	env.router.ServeHTTP(getResp, getReq)
	if getResp.Code != http.StatusOK {
		t.Fatalf("expected archive GET 200, got %d: %s", getResp.Code, getResp.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(getResp.Body.Bytes()), int64(getResp.Body.Len()))
	if err != nil {
		t.Fatalf("open downloaded archive: %v", err)
	}
	entries := make(map[string]*zip.File, len(archive.File))
	for _, entry := range archive.File {
		entries[entry.Name] = entry
	}
	if len(entries) == 0 || entries["src/app.json"] == nil {
		t.Fatalf("archive does not contain the recovered src tree: %#v", entries)
	}
	for name := range entries {
		if !strings.HasPrefix(name, "src/") {
			t.Fatalf("archive contains a non-src entry %q", name)
		}
	}
	if task.Artifacts == nil || task.Artifacts.FileCount != len(entries) {
		t.Fatalf("artifact summary count does not match archive: artifacts=%#v archiveFiles=%d", task.Artifacts, len(entries))
	}

	reportReq := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/report", nil)
	reportResp := httptest.NewRecorder()
	env.router.ServeHTTP(reportResp, reportReq)
	if reportResp.Code != http.StatusOK {
		t.Fatalf("expected online report 200, got %d: %s", reportResp.Code, reportResp.Body.String())
	}
	var onlineReport struct {
		SnapshotScope string `json:"snapshotScope"`
		Packaging     struct {
			Status        string `json:"status"`
			DownloadReady bool   `json:"downloadReady"`
			ArchiveSize   int64  `json:"archiveSize"`
			ZipManifest   string `json:"zipManifest"`
		} `json:"packaging"`
	}
	if err := json.Unmarshal(reportResp.Body.Bytes(), &onlineReport); err != nil {
		t.Fatal(err)
	}
	if onlineReport.SnapshotScope != "live-task" || onlineReport.Packaging.Status != "ready" || !onlineReport.Packaging.DownloadReady || onlineReport.Packaging.ArchiveSize <= 0 {
		t.Fatalf("online report is missing final archive metadata: %#v", onlineReport)
	}
	wantManifestURL := "/api/tasks/" + task.ID + "/report?name=zip-manifest"
	if onlineReport.Packaging.ZipManifest != "report?name=zip-manifest" {
		t.Fatalf("zip manifest reference = %q, want portable named-report reference", onlineReport.Packaging.ZipManifest)
	}

	manifestReq := httptest.NewRequest(http.MethodGet, wantManifestURL, nil)
	manifestResp := httptest.NewRecorder()
	env.router.ServeHTTP(manifestResp, manifestReq)
	if manifestResp.Code != http.StatusOK {
		t.Fatalf("expected zip manifest 200, got %d: %s", manifestResp.Code, manifestResp.Body.String())
	}
	var onlineManifest struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(manifestResp.Body.Bytes(), &onlineManifest); err != nil {
		t.Fatal(err)
	}
	if len(onlineManifest.Files) != len(entries) {
		t.Fatalf("zip manifest lists %d files, archive has %d", len(onlineManifest.Files), len(entries))
	}
	manifestEntries := make(map[string]struct{}, len(onlineManifest.Files))
	for _, name := range onlineManifest.Files {
		if _, duplicate := manifestEntries[name]; duplicate {
			t.Fatalf("zip manifest contains duplicate entry %q", name)
		}
		manifestEntries[name] = struct{}{}
		if entries[name] == nil {
			t.Fatalf("zip manifest lists missing entry %q", name)
		}
	}
	for name := range entries {
		if _, ok := manifestEntries[name]; !ok {
			t.Fatalf("zip manifest omits archive entry %q", name)
		}
	}

	for _, reportPath := range []string{
		"/api/tasks/" + task.ID + "/diagnostics",
		"/api/tasks/" + task.ID + "/artifacts",
		"/api/tasks/" + task.ID + "/report?name=package-profile",
	} {
		req := httptest.NewRequest(http.MethodGet, reportPath, nil)
		resp := httptest.NewRecorder()
		env.router.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK || !json.Valid(resp.Body.Bytes()) {
			t.Fatalf("retained online report endpoint %s failed: status=%d body=%s", reportPath, resp.Code, resp.Body.String())
		}
	}
}

type testEnv struct {
	router *gin.Engine
}

func newTestEnv(t *testing.T, fallbackEnabled bool) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	outputDir := t.TempDir()
	cfg := &config.Config{
		ServerHost:             "127.0.0.1",
		ServerPort:             0,
		MaxUploadSize:          10 * 1024 * 1024,
		TempDir:                tempDir,
		OutputDir:              outputDir,
		TaskRepoDriver:         "file",
		QueueDriver:            "inmem",
		NativeRecoverEnabled:   true,
		FallbackRecoverEnabled: fallbackEnabled,
		VerificationEnabled:    true,
		ReportEnabled:          true,
		NodeBinary:             "node",
		NodeExecTimeoutSeconds: 10,
		NodeExecMemoryMB:       256,
		MaxConcurrentTasks:     1,
		RetainArtifactsHours:   1,
	}

	if err := service.InitBeautifyService(false, 0, 0, 0, false); err != nil {
		t.Fatalf("InitBeautifyService: %v", err)
	}
	t.Cleanup(service.StopBeautifyService)

	repo, err := persistence.NewTaskRepository(cfg)
	if err != nil {
		t.Fatalf("NewTaskRepository: %v", err)
	}
	broker := events.NewBroker()
	jobQueue, err := queue.NewJobQueue(cfg)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	compileService := app.NewCompileService(cfg, repo, broker, jobQueue)
	queryService := app.NewTaskQueryService(cfg, repo)
	workerCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	jobQueue.StartWorkers(workerCtx, 1, func(ctx context.Context, taskID string) error {
		return compileService.RunTask(ctx, taskID)
	})

	router := gin.New()
	httpapi.NewRouter(
		httpapi.NewCompileHandler(compileService, cfg.MaxUploadSize),
		httpapi.NewTaskHandler(queryService, broker),
		httpapi.NewDownloadHandler(queryService),
		httpapi.NewGitHubStarsHandler(app.NewGitHubStarsService()),
	).RegisterRoutes(router)

	return &testEnv{router: router}
}

func (e *testEnv) compileAndWait(t *testing.T, pkgData []byte, fields map[string]string) taskResponse {
	t.Helper()

	recorder, body, contentType, err := testutil.BuildMultipartCompileRequest("fixture.wxapkg", pkgData, fields)
	if err != nil {
		t.Fatalf("BuildMultipartCompileRequest: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/compile", body)
	request.Header.Set("Content-Type", contentType)
	e.router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("compile failed: %d %s", recorder.Code, recorder.Body.String())
	}

	var compileResp compileResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &compileResp); err != nil {
		t.Fatalf("decode compile response: %v", err)
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		taskReq := httptest.NewRequest(http.MethodGet, "/api/tasks/"+compileResp.TaskID, nil)
		taskResp := httptest.NewRecorder()
		e.router.ServeHTTP(taskResp, taskReq)
		if taskResp.Code != http.StatusOK {
			t.Fatalf("task query failed: %d %s", taskResp.Code, taskResp.Body.String())
		}
		body, _ := io.ReadAll(taskResp.Body)
		var task taskResponse
		if err := json.Unmarshal(body, &task); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		if task.Status == "completed" || task.Status == "partial" || task.Status == "failed" {
			return task
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for task completion")
	return taskResponse{}
}

func (e *testEnv) assertReportStatus(t *testing.T, taskID string, expected string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+taskID+"/report", nil)
	resp := httptest.NewRecorder()
	e.router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("report query failed: %d %s", resp.Code, resp.Body.String())
	}
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode report response: %v", err)
	}
	if payload.Status != expected {
		t.Fatalf("expected report status %s, got %s", expected, payload.Status)
	}
}
