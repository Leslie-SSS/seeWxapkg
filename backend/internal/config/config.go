package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	ServerHost         string
	ServerPort         int
	MaxUploadSize      int64
	TempDir            string
	OutputDir          string
	CORSAllowedOrigins []string

	// Beautify service configuration
	BeautifyEnabled      bool
	BeautifyTimeout      int  // seconds
	BeautifyMaxFileSize  int  // bytes
	BeautifyFailureLimit int  // failures before circuit breaker opens
	DeobfuscateEnabled   bool // enable variable name deobfuscation

	TaskRepoDriver string
	QueueDriver    string

	NativeRecoverEnabled   bool
	FallbackRecoverEnabled bool
	VerificationEnabled    bool
	ReportEnabled          bool

	NodeBinary             string
	NodeExecTimeoutSeconds int
	NodeExecMemoryMB       int

	MaxConcurrentTasks   int
	RetainArtifactsHours int

	// DiagnosticSamplesDir, when non-empty, enables temporary retention of
	// failed/partial task inputs (package + AppID) for offline analysis.
	// Samples follow the same retention window as artifacts and are never
	// exposed through the API. Empty by default (collection disabled).
	DiagnosticSamplesDir string

	storageInitErr error
}

func Load() *Config {
	cfg := &Config{
		ServerHost:         getEnv("SERVER_HOST", "0.0.0.0"),
		ServerPort:         getEnvInt("SERVER_PORT", 9090),
		MaxUploadSize:      getEnvInt64("MAX_UPLOAD_SIZE", 50*1024*1024), // 50MB
		TempDir:            getEnv("TEMP_DIR", "/tmp/seewxapkg"),
		OutputDir:          getEnv("OUTPUT_DIR", "/output"),
		CORSAllowedOrigins: getEnvList("CORS_ALLOWED_ORIGINS"),

		// Safe formatting is enabled by default. Heuristic deobfuscation remains opt-in.
		BeautifyEnabled:      getEnvBool("BEAUTIFY_ENABLED", true),
		BeautifyTimeout:      getEnvInt("BEAUTIFY_TIMEOUT", 30),
		BeautifyMaxFileSize:  getEnvInt("BEAUTIFY_MAX_FILE_SIZE", 8*1024*1024),
		BeautifyFailureLimit: getEnvInt("BEAUTIFY_FAILURE_LIMIT", 5),
		DeobfuscateEnabled:   getEnvBool("DEOBFUSCATE_ENABLED", false),

		TaskRepoDriver: getEnv("TASK_REPO_DRIVER", "memory"),
		QueueDriver:    getEnv("QUEUE_DRIVER", "inmem"),

		NativeRecoverEnabled:   getEnvBool("NATIVE_RECOVER_ENABLED", true),
		FallbackRecoverEnabled: getEnvBool("FALLBACK_RECOVER_ENABLED", true),
		VerificationEnabled:    getEnvBool("VERIFICATION_ENABLED", true),
		ReportEnabled:          getEnvBool("REPORT_ENABLED", true),

		NodeBinary:             getEnv("NODE_BINARY", "node"),
		NodeExecTimeoutSeconds: getEnvInt("NODE_EXEC_TIMEOUT_SECONDS", 60),
		NodeExecMemoryMB:       getEnvInt("NODE_EXEC_MEMORY_MB", 512),

		MaxConcurrentTasks:   getEnvInt("MAX_CONCURRENT_TASKS", 4),
		RetainArtifactsHours: getEnvInt("RETAIN_ARTIFACTS_HOURS", 24),
		DiagnosticSamplesDir: getEnv("DIAGNOSTIC_SAMPLES_DIR", ""),
	}

	// These directories contain uploaded packages and recovered source. Tighten
	// permissions even when a directory was created by an older release.
	for _, directory := range []struct {
		label string
		path  string
	}{{"TEMP_DIR", cfg.TempDir}, {"OUTPUT_DIR", cfg.OutputDir}, {"DIAGNOSTIC_SAMPLES_DIR", cfg.DiagnosticSamplesDir}} {
		if directory.path == "" {
			continue
		}
		if err := validatePrivateStoragePath(directory.label, directory.path); err != nil {
			cfg.storageInitErr = err
			break
		}
		if err := os.MkdirAll(directory.path, 0700); err != nil {
			cfg.storageInitErr = fmt.Errorf("initialize %s: %w", directory.label, err)
			break
		}
		if err := os.Chmod(directory.path, 0700); err != nil {
			cfg.storageInitErr = fmt.Errorf("secure %s: %w", directory.label, err)
			break
		}
	}

	return cfg
}

func validatePrivateStoragePath(label, path string) error {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "." || !filepath.IsAbs(clean) {
		return fmt.Errorf("%s must be an absolute dedicated directory", label)
	}
	volumeRoot := string(filepath.Separator)
	if volume := filepath.VolumeName(clean); volume != "" {
		volumeRoot = volume + string(filepath.Separator)
	}
	for _, forbidden := range []string{volumeRoot, filepath.Clean(os.TempDir())} {
		if clean == forbidden {
			return fmt.Errorf("%s must not use a shared or filesystem root directory", label)
		}
	}
	if home, err := os.UserHomeDir(); err == nil && clean == filepath.Clean(home) {
		return fmt.Errorf("%s must not use the user home directory", label)
	}
	if resolved, err := filepath.EvalSymlinks(clean); err == nil && resolved != clean {
		return validatePrivateStoragePath(label, resolved)
	}
	return nil
}

func (c *Config) Validate() error {
	if c.storageInitErr != nil {
		return c.storageInitErr
	}
	tempInfo, tempErr := os.Stat(c.TempDir)
	outputInfo, outputErr := os.Stat(c.OutputDir)
	if tempErr != nil || outputErr != nil {
		return fmt.Errorf("private storage directories are unavailable")
	}
	if os.SameFile(tempInfo, outputInfo) {
		return fmt.Errorf("TEMP_DIR and OUTPUT_DIR must resolve to distinct directories")
	}
	if c.MaxUploadSize <= 0 {
		return fmt.Errorf("max upload size must be positive")
	}
	if c.NodeExecTimeoutSeconds <= 0 {
		return fmt.Errorf("node exec timeout must be positive")
	}
	if c.MaxConcurrentTasks <= 0 {
		return fmt.Errorf("max concurrent tasks must be positive")
	}
	if c.BeautifyTimeout <= 0 || c.BeautifyMaxFileSize <= 0 || c.BeautifyFailureLimit <= 0 {
		return fmt.Errorf("beautify timeout, max file size and failure limit must be positive")
	}
	if c.RetainArtifactsHours < 0 {
		return fmt.Errorf("retain artifacts hours cannot be negative")
	}
	if c.TempDir == "" || c.OutputDir == "" || filepath.Clean(c.TempDir) == filepath.Clean(c.OutputDir) {
		return fmt.Errorf("TEMP_DIR and OUTPUT_DIR must be non-empty distinct directories")
	}
	wildcardOrigins := 0
	for _, origin := range c.CORSAllowedOrigins {
		if origin == "*" {
			wildcardOrigins++
			continue
		}
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("invalid CORS_ALLOWED_ORIGINS entry %q; expected an http(s) origin without path", origin)
		}
	}
	if wildcardOrigins > 0 && len(c.CORSAllowedOrigins) != 1 {
		return fmt.Errorf("CORS_ALLOWED_ORIGINS wildcard must be used alone")
	}
	if c.TaskRepoDriver != "memory" && c.TaskRepoDriver != "file" {
		return fmt.Errorf("unsupported TASK_REPO_DRIVER %q", c.TaskRepoDriver)
	}
	if c.QueueDriver != "inmem" && c.QueueDriver != "file" {
		return fmt.Errorf("unsupported QUEUE_DRIVER %q", c.QueueDriver)
	}
	for _, key := range []string{
		"BEAUTIFY_ENABLED", "DEOBFUSCATE_ENABLED", "NATIVE_RECOVER_ENABLED",
		"FALLBACK_RECOVER_ENABLED", "VERIFICATION_ENABLED", "REPORT_ENABLED",
	} {
		if err := validateOptionalBoolEnv(key); err != nil {
			return err
		}
	}
	for _, key := range []string{
		"SERVER_PORT", "MAX_UPLOAD_SIZE", "BEAUTIFY_TIMEOUT", "BEAUTIFY_MAX_FILE_SIZE",
		"BEAUTIFY_FAILURE_LIMIT", "NODE_EXEC_TIMEOUT_SECONDS", "NODE_EXEC_MEMORY_MB",
		"MAX_CONCURRENT_TASKS", "RETAIN_ARTIFACTS_HOURS",
	} {
		if err := validateOptionalIntEnv(key); err != nil {
			return err
		}
	}
	return nil
}

func validateOptionalBoolEnv(key string) error {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "false", "1", "0":
		return nil
	default:
		return fmt.Errorf("%s must be true, false, 1 or 0", key)
	}
}

func validateOptionalIntEnv(key string) error {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return nil
	}
	if _, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err != nil {
		return fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvList(key string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return strings.ToLower(value) == "true" || value == "1"
	}
	return defaultValue
}
