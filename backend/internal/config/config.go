package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ServerHost    string
	ServerPort    int
	MaxUploadSize int64
	TempDir       string
	OutputDir     string

	// Beautify service configuration
	BeautifyEnabled      bool
	BeautifyTimeout      int  // seconds
	BeautifyMaxFileSize  int  // bytes
	BeautifyFailureLimit int  // failures before circuit breaker opens
	DeobfuscateEnabled   bool // enable variable name deobfuscation
}

type BeautifyConfig struct {
	Enabled      bool
	Timeout      int
	MaxFileSize  int
	FailureLimit int
}

func Load() *Config {
	cfg := &Config{
		ServerHost:    getEnv("SERVER_HOST", "0.0.0.0"),
		ServerPort:    getEnvInt("SERVER_PORT", 9090),
		MaxUploadSize: getEnvInt64("MAX_UPLOAD_SIZE", 50*1024*1024), // 50MB
		TempDir:       getEnv("TEMP_DIR", "/tmp/seewxapkg"),
		OutputDir:     getEnv("OUTPUT_DIR", "/output"),

		// Beautify configuration - defaults to disabled for stability
		BeautifyEnabled:      getEnvBool("BEAUTIFY_ENABLED", false),
		BeautifyTimeout:      getEnvInt("BEAUTIFY_TIMEOUT", 5),
		BeautifyMaxFileSize:  getEnvInt("BEAUTIFY_MAX_FILE_SIZE", 500*1024), // 500KB
		BeautifyFailureLimit: getEnvInt("BEAUTIFY_FAILURE_LIMIT", 5),
		DeobfuscateEnabled:   getEnvBool("DEOBFUSCATE_ENABLED", true), // conservative mode by default
	}

	// 确保目录存在
	os.MkdirAll(cfg.TempDir, 0755)
	os.MkdirAll(cfg.OutputDir, 0755)

	return cfg
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
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
