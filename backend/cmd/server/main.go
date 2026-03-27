package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/keepbuild/seewxapkg/internal/api"
	"github.com/keepbuild/seewxapkg/internal/config"
	"github.com/keepbuild/seewxapkg/internal/service"
)

func main() {
	// 加载配置
	cfg := config.Load()

	// 初始化美化服务
	if err := service.InitBeautifyService(
		cfg.BeautifyEnabled,
		cfg.BeautifyTimeout,
		cfg.BeautifyMaxFileSize,
		cfg.BeautifyFailureLimit,
		cfg.DeobfuscateEnabled,
	); err != nil {
		log.Printf("Warning: Failed to initialize beautify service: %v", err)
	}
	defer service.StopBeautifyService()

	// 设置 Gin 模式
	gin.SetMode(gin.ReleaseMode)

	// 创建路由
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(loggerMiddleware())
	r.Use(corsMiddleware())

	// 注册路由
	handler := api.NewHandler(cfg)
	handler.RegisterRoutes(r)

	// 启动服务器
	addr := fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.ServerPort)
	log.Printf("Starting SeeWxapkg server on %s", addr)
	log.Printf("Max upload size: %d bytes", cfg.MaxUploadSize)
	log.Printf("Temp dir: %s", cfg.TempDir)
	log.Printf("Output dir: %s", cfg.OutputDir)
	log.Printf("Beautify enabled: %v", cfg.BeautifyEnabled)

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down server...")
		service.StopBeautifyService()
		os.Exit(0)
	}()

	if err := r.Run(addr); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

// loggerMiddleware 日志中间件
func loggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		method := c.Request.Method
		clientIP := c.ClientIP()

		if query != "" {
			path = path + "?" + query
		}

		log.Printf("[%s] %s %s %d %v",
			method,
			clientIP,
			path,
			status,
			latency,
		)
	}
}

// corsMiddleware CORS 中间件
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
