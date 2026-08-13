package beautify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/keepbuild/seewxapkg/internal/infra/process"
)

// Service manages the beautification process
type Service struct {
	enabled     bool
	nodePath    string
	serverURL   string
	httpClient  *http.Client
	timeout     time.Duration
	maxFileSize int
	deobfuscate bool

	// Sidecar start parameters, kept for crash recovery.
	beautifyDir string
	serverPort  int

	// Process management
	cmd       *exec.Cmd
	processMu sync.Mutex
	stopOnce  sync.Once

	// Circuit breaker for stability
	circuitBreaker *CircuitBreaker

	// Health check
	healthy   bool
	healthMu  sync.RWMutex
	stopCheck chan struct{}
}

// Config holds configuration for the beautify service
type Config struct {
	Enabled          bool
	NodePath         string
	ServerPort       int
	Timeout          time.Duration
	MaxFileSize      int
	BeautifyDir      string
	FailureThreshold int
	Deobfuscate      bool
}

// ConfigFromParams creates a Config from individual parameters
func ConfigFromParams(enabled bool, timeoutSeconds, maxFileSize, failureLimit int, deobfuscate bool) Config {
	nodePath, _ := exec.LookPath("node")
	return Config{
		Enabled:          enabled,
		NodePath:         nodePath,
		ServerPort:       3001,
		Timeout:          time.Duration(timeoutSeconds) * time.Second,
		MaxFileSize:      maxFileSize,
		BeautifyDir:      "./internal/beautify",
		FailureThreshold: failureLimit,
		Deobfuscate:      deobfuscate,
	}
}

// Request represents a beautify request
type Request struct {
	Content  string `json:"content"`
	Type     string `json:"type"`
	Filename string `json:"filename"`
}

// Response represents a beautify response
type Response struct {
	Success   bool   `json:"success"`
	Status    string `json:"status,omitempty"`
	Content   string `json:"content"`
	Formatter string `json:"formatter,omitempty"`
	Error     string `json:"error,omitempty"`
	Warning   string `json:"warning,omitempty"`
}

type Result struct {
	Content   []byte
	Status    string
	Formatter string
	Warning   string
	Error     error
}

// NewService creates a new beautify service
func NewService(cfg Config) (*Service, error) {
	if !cfg.Enabled {
		return newDisabledService(), nil
	}

	if cfg.NodePath == "" {
		return nil, fmt.Errorf("node.js not found while beautification is enabled")
	}

	cb := NewCircuitBreakerWithConfig(CircuitBreakerConfig{
		FailureThreshold: cfg.FailureThreshold,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
	})

	s := &Service{
		enabled:        cfg.Enabled,
		nodePath:       cfg.NodePath,
		serverURL:      fmt.Sprintf("http://127.0.0.1:%d", cfg.ServerPort),
		timeout:        cfg.Timeout,
		maxFileSize:    cfg.MaxFileSize,
		deobfuscate:    cfg.Deobfuscate,
		beautifyDir:    cfg.BeautifyDir,
		serverPort:     cfg.ServerPort,
		circuitBreaker: cb,
		healthy:        false,
		stopCheck:      make(chan struct{}),
		httpClient: &http.Client{
			Timeout: cfg.Timeout + 2*time.Second,
		},
	}

	// Start the Node.js server
	if err := s.startServer(cfg.BeautifyDir, cfg.ServerPort); err != nil {
		return nil, fmt.Errorf("start beautify server: %w", err)
	}

	// Start health check goroutine
	go s.healthCheckLoop()

	return s, nil
}

func newDisabledService() *Service {
	return &Service{
		enabled:        false,
		circuitBreaker: NewCircuitBreaker(),
		stopCheck:      make(chan struct{}),
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

// startServer starts the Node.js beautification server
func (s *Service) startServer(beautifyDir string, port int) error {
	s.processMu.Lock()
	defer s.processMu.Unlock()

	absBeautifyDir, err := process.ResolveExistingPath(
		beautifyDir,
		filepath.Join("backend", "internal", "beautify"),
		filepath.Join("internal", "beautify"),
	)
	if err != nil {
		return fmt.Errorf("resolve beautify dir: %w", err)
	}

	serverPath := filepath.Join(absBeautifyDir, "server.js")

	// Check if server.js exists
	if _, err := os.Stat(serverPath); os.IsNotExist(err) {
		return fmt.Errorf("server.js not found at %s", serverPath)
	}

	// Check if node_modules exists, if not, install dependencies
	nodeModulesPath := filepath.Join(absBeautifyDir, "node_modules")
	if _, err := os.Stat(nodeModulesPath); os.IsNotExist(err) {
		log.Println("[Beautify] Installing dependencies with npm ci...")
		installArgs := []string{"ci", "--omit=dev"}
		if _, statErr := os.Stat(filepath.Join(absBeautifyDir, "package-lock.json")); os.IsNotExist(statErr) {
			installArgs = []string{"install", "--production"}
		}
		installCmd := exec.Command("npm", installArgs...)
		installCmd.Dir = absBeautifyDir
		installCmd.Stdout = os.Stdout
		installCmd.Stderr = os.Stderr
		if err := installCmd.Run(); err != nil {
			return fmt.Errorf("npm install failed: %w", err)
		}
	}

	// Start the server
	cmd := exec.Command(s.nodePath, serverPath)
	cmd.Dir = absBeautifyDir
	deobfuscateStr := "false"
	if s.deobfuscate {
		deobfuscateStr = "true"
	}
	// The pool size is operator-tunable (fewer workers lower peak memory);
	// the container env wins over the baked default of two.
	workers := os.Getenv("BEAUTIFY_WORKERS")
	if workers == "" {
		workers = "2"
	}
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("BEAUTIFY_PORT=%d", port),
		"BEAUTIFY_HOST=127.0.0.1",
		fmt.Sprintf("MAX_CONTENT_SIZE=%d", s.maxFileSize),
		fmt.Sprintf("BEAUTIFY_JOB_TIMEOUT_MS=%d", s.timeout.Milliseconds()),
		"BEAUTIFY_QUEUE_SIZE=32",
		fmt.Sprintf("BEAUTIFY_WORKERS=%s", workers),
		fmt.Sprintf("DEOBFUSCATE_ENABLED=%s", deobfuscateStr),
	)

	// Capture stdout/stderr for debugging
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start node server: %w", err)
	}

	s.cmd = cmd

	// Wait for server to be ready
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
				_, _ = cmd.Process.Wait()
			}
			return fmt.Errorf("timeout waiting for server to start")
		case <-time.After(100 * time.Millisecond):
			if s.checkHealth() {
				log.Printf("[Beautify] Server started on port %d", port)
				s.setHealthy(true)
				return nil
			}
		}
	}
}

// checkHealth checks if the beautification server is healthy
func (s *Service) checkHealth() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", s.serverURL+"/health", nil)
	if err != nil {
		return false
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == 200
}

// healthCheckLoop periodically checks server health and restarts the sidecar
// when it has died, so a single crash (e.g. an OOM kill) does not silently
// disable beautification until the whole worker is restarted.
func (s *Service) healthCheckLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.checkAndRecover()
		case <-s.stopCheck:
			return
		}
	}
}

// checkAndRecover probes the sidecar and starts a fresh one when the probe
// fails. The 30s tick of the calling loop acts as natural restart backoff.
func (s *Service) checkAndRecover() {
	if s.checkHealth() {
		s.setHealthy(true)
		return
	}
	s.setHealthy(false)
	if !s.enabled {
		log.Println("[Beautify] Server unhealthy")
		return
	}
	log.Println("[Beautify] Server unhealthy, restarting sidecar")
	if err := s.restartServer(); err != nil {
		log.Printf("[Beautify] Sidecar restart failed (%T)", err)
	}
}

// restartServer stops any stale sidecar process and starts a fresh one.
func (s *Service) restartServer() error {
	s.processMu.Lock()
	oldCmd := s.cmd
	s.cmd = nil
	s.processMu.Unlock()

	if oldCmd != nil && oldCmd.Process != nil {
		_ = oldCmd.Process.Kill()
		_, _ = oldCmd.Process.Wait()
		log.Println("[Beautify] Stopped stale sidecar process")
	}

	return s.startServer(s.beautifyDir, s.serverPort)
}

// setHealthy sets the health status
func (s *Service) setHealthy(healthy bool) {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	s.healthy = healthy
}

// IsHealthy returns whether the service is healthy
func (s *Service) IsHealthy() bool {
	s.healthMu.RLock()
	defer s.healthMu.RUnlock()
	return s.healthy
}

// Beautify beautifies the given content
func (s *Service) Beautify(content []byte, filename string) []byte {
	return s.BeautifyDetailed(content, filename).Content
}

// BeautifyDetailed distinguishes actual formatting from unchanged, skipped and
// failed outcomes. The original bytes are always returned on non-formatted
// outcomes so a formatter can never make extraction destructive.
func (s *Service) BeautifyDetailed(content []byte, filename string) Result {
	// Check if beautification is enabled
	if !s.enabled {
		return Result{Content: content, Status: "skipped", Warning: "formatter disabled"}
	}

	// Check file size
	if len(content) > s.maxFileSize {
		log.Printf("[Beautify] File too large (%d bytes), skipping", len(content))
		return Result{Content: content, Status: "skipped", Warning: "file exceeds formatter limit"}
	}

	// Check health
	if !s.IsHealthy() {
		log.Printf("[Beautify] Service unhealthy, skipping file")
		return Result{Content: content, Status: "skipped", Warning: "formatter unavailable"}
	}

	// Check the circuit only after the sidecar is healthy. Otherwise moving an
	// open circuit to half-open would reserve its single probe without ever
	// issuing a request, leaving the breaker stuck indefinitely.
	if s.circuitBreaker.IsOpen() {
		log.Printf("[Beautify] Circuit breaker open, skipping file")
		return Result{Content: content, Status: "skipped", Warning: "formatter circuit breaker open"}
	}

	// Determine file type
	fileType := s.getFileType(filename)

	// Make request with timeout
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	response, err := s.beautifyWithContext(ctx, content, fileType, filename)
	if err != nil {
		s.circuitBreaker.RecordFailure()
		log.Printf("[Beautify] Formatting failed (%T), returning original", err)
		return Result{Content: content, Status: "failed", Error: err}
	}

	status := response.Status
	if status == "" {
		status = "formatted"
		if response.Content == string(content) {
			status = "unchanged"
		}
	}
	result := Result{
		Content:   []byte(response.Content),
		Status:    status,
		Formatter: response.Formatter,
		Warning:   response.Warning,
	}
	if status == "failed" || !response.Success {
		errText := response.Error
		if errText == "" {
			errText = response.Warning
		}
		if errText == "" {
			errText = "formatter reported failure"
		}
		result.Content = content
		result.Error = fmt.Errorf("%s", errText)
		s.circuitBreaker.RecordFailure()
		return result
	}
	if status == "formatted" || status == "unchanged" {
		s.circuitBreaker.RecordSuccess()
	}
	return result
}

// beautifyWithContext performs beautification with context
func (s *Service) beautifyWithContext(ctx context.Context, content []byte, fileType, filename string) (Response, error) {
	reqBody := Request{
		Content:  string(content),
		Type:     fileType,
		Filename: filename,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.serverURL+"/beautify", bytes.NewReader(bodyBytes))
	if err != nil {
		return Response{}, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	maxResponseBytes := int64(s.maxFileSize)*2 + 64*1024
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}
	if int64(len(respBody)) > maxResponseBytes {
		return Response{}, fmt.Errorf("formatter response exceeds %d bytes", maxResponseBytes)
	}

	if resp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result Response
	if err := json.Unmarshal(respBody, &result); err != nil {
		return Response{}, fmt.Errorf("unmarshal response: %w", err)
	}

	if !result.Success {
		return result, fmt.Errorf("beautify failed: %s", result.Error)
	}

	if result.Warning != "" {
		log.Printf("[Beautify] Formatter warning file=%s type=%s status=%s: %s", filename, fileType, result.Status, result.Warning)
	}

	return result, nil
}

// getFileType determines the file type from filename
func (s *Service) getFileType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".js", ".wxs":
		return "javascript"
	case ".wxml", ".html":
		return "html"
	case ".wxss", ".css":
		return "css"
	case ".json":
		return "json"
	default:
		return "unknown"
	}
}

// Stop stops the beautification service
func (s *Service) Stop() error {
	s.stopOnce.Do(func() {
		if s.stopCheck != nil {
			close(s.stopCheck)
		}
	})

	s.processMu.Lock()
	defer s.processMu.Unlock()

	if s.cmd != nil && s.cmd.Process != nil {
		if err := s.cmd.Process.Signal(os.Interrupt); err != nil {
			return s.cmd.Process.Kill()
		}
		return s.cmd.Wait()
	}

	return nil
}

// GetStats returns service statistics
func (s *Service) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"enabled":        s.enabled,
		"healthy":        s.IsHealthy(),
		"circuitBreaker": s.circuitBreaker.GetStats(),
		"maxFileSize":    s.maxFileSize,
		"timeout":        s.timeout.String(),
	}
}
