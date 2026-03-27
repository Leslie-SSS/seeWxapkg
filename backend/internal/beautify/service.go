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

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	nodePath, _ := exec.LookPath("node")
	return Config{
		Enabled:          false,
		NodePath:         nodePath,
		ServerPort:       3001,
		Timeout:          5 * time.Second,
		MaxFileSize:      500 * 1024, // 500KB
		BeautifyDir:      "./internal/beautify",
		FailureThreshold: 5,
		Deobfuscate:      true,
	}
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
	Success bool   `json:"success"`
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
	Warning string `json:"warning,omitempty"`
}

// NewService creates a new beautify service
func NewService(cfg Config) (*Service, error) {
	if !cfg.Enabled {
		return newDisabledService(), nil
	}

	if cfg.NodePath == "" {
		log.Println("[Beautify] Node.js not found, beautification disabled")
		return newDisabledService(), nil
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
		circuitBreaker: cb,
		healthy:        false,
		stopCheck:      make(chan struct{}),
		httpClient: &http.Client{
			Timeout: cfg.Timeout + 2*time.Second,
		},
	}

	// Start the Node.js server
	if err := s.startServer(cfg.BeautifyDir, cfg.ServerPort); err != nil {
		log.Printf("[Beautify] Failed to start server: %v", err)
		return newDisabledService(), nil
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

	absBeautifyDir, err := filepath.Abs(beautifyDir)
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
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("BEAUTIFY_PORT=%d", port),
		fmt.Sprintf("BEAUTIFY_HOST=127.0.0.1"),
		fmt.Sprintf("MAX_CONTENT_SIZE=%d", s.maxFileSize),
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

// healthCheckLoop periodically checks server health
func (s *Service) healthCheckLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			healthy := s.checkHealth()
			s.setHealthy(healthy)

			if !healthy && s.enabled {
				log.Println("[Beautify] Server unhealthy, circuit breaker may engage")
			}
		case <-s.stopCheck:
			return
		}
	}
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
	// Check if beautification is enabled
	if !s.enabled {
		return content
	}

	// Check file size
	if len(content) > s.maxFileSize {
		log.Printf("[Beautify] File too large (%d bytes), skipping: %s", len(content), filename)
		return content
	}

	// Check circuit breaker
	if s.circuitBreaker.IsOpen() {
		log.Printf("[Beautify] Circuit breaker open, skipping: %s", filename)
		return content
	}

	// Check health
	if !s.IsHealthy() {
		log.Printf("[Beautify] Service unhealthy, skipping: %s", filename)
		return content
	}

	// Determine file type
	fileType := s.getFileType(filename)

	// Make request with timeout
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	result, err := s.beautifyWithContext(ctx, content, fileType, filename)
	if err != nil {
		s.circuitBreaker.RecordFailure()
		log.Printf("[Beautify] Failed for %s: %v, returning original", filename, err)
		return content
	}

	s.circuitBreaker.RecordSuccess()
	return result
}

// beautifyWithContext performs beautification with context
func (s *Service) beautifyWithContext(ctx context.Context, content []byte, fileType, filename string) ([]byte, error) {
	reqBody := Request{
		Content:  string(content),
		Type:     fileType,
		Filename: filename,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.serverURL+"/beautify", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result Response
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("beautify failed: %s", result.Error)
	}

	if result.Warning != "" {
		log.Printf("[Beautify] Warning for %s: %s", filename, result.Warning)
	}

	return []byte(result.Content), nil
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
