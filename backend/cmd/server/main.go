package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	httpapi "github.com/keepbuild/seewxapkg/internal/api/http"
	"github.com/keepbuild/seewxapkg/internal/app"
	"github.com/keepbuild/seewxapkg/internal/config"
	"github.com/keepbuild/seewxapkg/internal/infra/events"
	"github.com/keepbuild/seewxapkg/internal/infra/persistence"
	"github.com/keepbuild/seewxapkg/internal/infra/queue"
	"github.com/keepbuild/seewxapkg/internal/infra/storage"
	"github.com/keepbuild/seewxapkg/internal/service"
)

func main() {
	if err := run(); err != nil {
		log.Printf("SeeWxapkg server stopped with error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	// 加载配置
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// 初始化美化服务
	if err := service.InitBeautifyService(
		cfg.BeautifyEnabled,
		cfg.BeautifyTimeout,
		cfg.BeautifyMaxFileSize,
		cfg.BeautifyFailureLimit,
		cfg.DeobfuscateEnabled,
	); err != nil {
		return err
	}
	defer service.StopBeautifyService()

	// 设置 Gin 模式
	gin.SetMode(gin.ReleaseMode)

	// 创建路由
	r := gin.New()
	r.Use(privacyRecoveryMiddleware())
	r.Use(loggerMiddleware())
	r.Use(corsMiddleware(cfg.CORSAllowedOrigins))

	repo, err := persistence.NewTaskRepository(cfg)
	if err != nil {
		return fmt.Errorf("initialize task repository: %w", err)
	}
	broker := events.NewBroker()
	jobQueue, err := queue.NewJobQueue(cfg)
	if err != nil {
		return fmt.Errorf("initialize task queue: %w", err)
	}
	compileService := app.NewCompileService(cfg, repo, broker, jobQueue)
	queryService := app.NewTaskQueryService(cfg, repo)
	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	persistence.StartRetentionJanitor(workerCtx, repo, cfg.RetainArtifactsHours)
	storage.StartRetentionJanitor(workerCtx, cfg.TempDir, cfg.OutputDir, cfg.RetainArtifactsHours)
	if cfg.QueueDriver == "inmem" {
		jobQueue.StartWorkers(workerCtx, cfg.MaxConcurrentTasks, func(ctx context.Context, taskID string) error {
			return compileService.RunTask(ctx, taskID)
		})
	}

	router := httpapi.NewRouter(
		httpapi.NewCompileHandler(compileService, cfg.MaxUploadSize),
		httpapi.NewTaskHandler(queryService, broker),
		httpapi.NewDownloadHandler(queryService),
		httpapi.NewGitHubStarsHandler(app.NewGitHubStarsService()),
	)
	router.RegisterRoutes(r)

	// 启动服务器
	addr := fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.ServerPort)
	log.Print("Starting SeeWxapkg server")
	log.Printf("Beautify enabled: %v", cfg.BeautifyEnabled)
	log.Printf("Fallback recover enabled: %v", cfg.FallbackRecoverEnabled)
	log.Printf("Task repo driver: %s", cfg.TaskRepoDriver)
	log.Printf("Queue driver: %s", cfg.QueueDriver)

	server := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
		// net/http panic logs include RemoteAddr by default. Gin owns request
		// recovery, so suppress the unsafe framework-level fallback logger.
		ErrorLog: log.New(io.Discard, "", 0),
	}
	serverErr := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serverErr <- err
	}()

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	select {
	case err := <-serverErr:
		workerCancel()
		return err
	case <-signalCtx.Done():
		log.Println("Shutting down server...")
	}

	return shutdownServer(server, workerCancel, jobQueue.Wait, 15*time.Second)
}

func shutdownServer(server *http.Server, workerCancel context.CancelFunc, waitWorkers func(), timeout time.Duration) error {
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), timeout)
	defer cancelShutdown()
	httpErr := server.Shutdown(shutdownCtx)
	if httpErr != nil {
		_ = server.Close()
	}
	workerCancel()
	workersDone := make(chan struct{})
	go func() {
		waitWorkers()
		close(workersDone)
	}()
	select {
	case <-workersDone:
	case <-shutdownCtx.Done():
		if httpErr != nil {
			return fmt.Errorf("graceful HTTP shutdown: %w", httpErr)
		}
		return fmt.Errorf("graceful worker shutdown: %w", shutdownCtx.Err())
	}
	if httpErr != nil {
		return fmt.Errorf("graceful HTTP shutdown: %w", httpErr)
	}
	return nil
}

func privacyRecoveryMiddleware() gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, _ any) {
		log.Print("[Recovery] request aborted after an internal panic")
		c.AbortWithStatus(http.StatusInternalServerError)
	})
}

// loggerMiddleware 日志中间件
func loggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := privacySafeLogPath(c.Request.URL.Path)

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		method := c.Request.Method
		log.Printf("[%s] %s %d %v",
			method,
			path,
			status,
			latency,
		)
	}
}

// privacySafeLogPath keeps request logs useful without persisting task IDs.
// Query strings are intentionally never passed to this function or logged.
func privacySafeLogPath(requestPath string) string {
	parts := strings.Split(requestPath, "/")
	for index := 1; index < len(parts); index++ {
		if parts[index] == "" {
			continue
		}
		if parts[index-1] == "tasks" || parts[index-1] == "download" {
			parts[index] = ":taskId"
			break
		}
	}
	return strings.Join(parts, "/")
}

// corsMiddleware CORS 中间件
func corsMiddleware(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	wildcard := false
	for _, origin := range allowedOrigins {
		if origin == "*" {
			wildcard = true
			continue
		}
		allowed[origin] = struct{}{}
	}
	return func(c *gin.Context) {
		requestOrigin := c.GetHeader("Origin")
		originAllowed := false
		if wildcard {
			c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
			originAllowed = true
		} else if _, ok := allowed[requestOrigin]; ok && requestOrigin != "" {
			c.Writer.Header().Set("Access-Control-Allow-Origin", requestOrigin)
			c.Writer.Header().Add("Vary", "Origin")
			originAllowed = true
		}
		if originAllowed {
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, Accept, Origin, Cache-Control, X-Requested-With")
			c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			c.Writer.Header().Set("Access-Control-Max-Age", "600")
		}

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
