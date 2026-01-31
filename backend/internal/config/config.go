package config

import (
	"os"
	"strconv"
)

type Config struct {
	ServerHost    string
	ServerPort    int
	MaxUploadSize int64
	TempDir       string
	OutputDir     string
}

func Load() *Config {
	cfg := &Config{
		ServerHost:    getEnv("SERVER_HOST", "0.0.0.0"),
		ServerPort:    getEnvInt("SERVER_PORT", 8080),
		MaxUploadSize: getEnvInt64("MAX_UPLOAD_SIZE", 50*1024*1024), // 50MB
		TempDir:       getEnv("TEMP_DIR", "/tmp/seewxapkg"),
		OutputDir:     getEnv("OUTPUT_DIR", "/output"),
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
