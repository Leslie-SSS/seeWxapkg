package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/keepbuild/seewxapkg/internal/app"
	"github.com/keepbuild/seewxapkg/internal/config"
	"github.com/keepbuild/seewxapkg/internal/infra/events"
	"github.com/keepbuild/seewxapkg/internal/infra/persistence"
	"github.com/keepbuild/seewxapkg/internal/infra/queue"
	"github.com/keepbuild/seewxapkg/internal/infra/storage"
	"github.com/keepbuild/seewxapkg/internal/service"
)

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatal("invalid config: ", err)
	}
	if err := service.InitBeautifyService(
		cfg.BeautifyEnabled,
		cfg.BeautifyTimeout,
		cfg.BeautifyMaxFileSize,
		cfg.BeautifyFailureLimit,
		cfg.DeobfuscateEnabled,
	); err != nil {
		log.Fatal("failed to initialize beautify service: ", err)
	}
	defer service.StopBeautifyService()

	repo, err := persistence.NewTaskRepository(cfg)
	if err != nil {
		log.Fatal("failed to initialize task repository: ", err)
	}
	jobQueue, err := queue.NewJobQueue(cfg)
	if err != nil {
		log.Fatal("failed to initialize task queue: ", err)
	}
	compileService := app.NewCompileService(cfg, repo, events.NewBroker(), jobQueue)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	persistence.StartRetentionJanitor(ctx, repo, cfg.RetainArtifactsHours)
	storage.StartRetentionJanitorWithSamples(ctx, cfg.TempDir, cfg.OutputDir, cfg.DiagnosticSamplesDir, cfg.RetainArtifactsHours)
	jobQueue.StartWorkers(ctx, cfg.MaxConcurrentTasks, func(workerCtx context.Context, taskID string) error {
		return compileService.RunTask(workerCtx, taskID)
	})

	log.Printf("SeeWxapkg worker started with queue=%s, repo=%s", cfg.QueueDriver, cfg.TaskRepoDriver)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("Shutting down worker...")
	cancel()
	workersDone := make(chan struct{})
	go func() {
		jobQueue.Wait()
		close(workersDone)
	}()
	select {
	case <-workersDone:
		log.Println("Worker shutdown complete")
	case <-time.After(30 * time.Second):
		log.Println("Worker shutdown timed out after 30s")
	}
}
