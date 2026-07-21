package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/keepbuild/seewxapkg/internal/app"
	"github.com/keepbuild/seewxapkg/internal/config"
	"github.com/keepbuild/seewxapkg/internal/domain/task"
	"github.com/keepbuild/seewxapkg/internal/infra/events"
	"github.com/keepbuild/seewxapkg/internal/infra/persistence"
)

func TestGetTaskRemovesStreamAfterAuthoritativeTerminalState(t *testing.T) {
	repo := persistence.NewMemoryTaskRepo()
	taskID := "11111111-1111-4111-8111-111111111111"
	current := &task.Task{ID: taskID, Status: task.TaskCompleted, Progress: 100, CurrentStage: "completed"}
	if err := repo.Create(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	broker := events.NewBroker()
	broker.Create(taskID)
	broker.Publish(taskID, task.TaskEvent{Type: "progress", Status: string(task.TaskPackaging), Percent: 90})

	router := newTaskHandlerTestRouter(app.NewTaskQueryService(&config.Config{}, repo), broker)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/tasks/"+taskID, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GetTask status = %d, want 200: %s", response.Code, response.Body.String())
	}
	assertBrokerStreamRemoved(t, broker, taskID)
}

func TestGetTaskKeepsActiveStream(t *testing.T) {
	repo := persistence.NewMemoryTaskRepo()
	taskID := "22222222-2222-4222-8222-222222222222"
	current := &task.Task{ID: taskID, Status: task.TaskQueued, CurrentStage: "queued"}
	if err := repo.Create(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	broker := events.NewBroker()
	broker.Create(taskID)

	router := newTaskHandlerTestRouter(app.NewTaskQueryService(&config.Config{}, repo), broker)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/tasks/"+taskID, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GetTask status = %d, want 200: %s", response.Code, response.Body.String())
	}
	eventStream, _, cancel, err := broker.Subscribe(taskID)
	if err != nil || eventStream == nil {
		t.Fatalf("active stream was removed: stream=%v err=%v", eventStream != nil, err)
	}
	cancel()
	broker.CloseAndRemove(taskID)
}

func TestStreamTaskEventsPollsFileWorkerTerminalStateAndRemovesStream(t *testing.T) {
	innerRepo := persistence.NewMemoryTaskRepo()
	taskID := "33333333-3333-4333-8333-333333333333"
	current := &task.Task{
		ID:             taskID,
		Status:         task.TaskQueued,
		Progress:       0,
		CurrentStage:   "queued",
		CurrentMessage: "任务已创建，等待处理",
	}
	if err := innerRepo.Create(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	repo := &firstGetSignalingRepository{
		Repository: innerRepo,
		firstGet:   make(chan struct{}),
	}
	broker := events.NewBroker()
	broker.Create(taskID)
	router := newTaskHandlerTestRouter(app.NewTaskQueryService(&config.Config{}, repo), broker)

	requestContext, cancelRequest := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelRequest()
	request := httptest.NewRequest(http.MethodGet, "/events?taskId="+taskID, nil).WithContext(requestContext)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		router.ServeHTTP(response, request)
	}()

	select {
	case <-repo.firstGet:
	case <-time.After(time.Second):
		cancelRequest()
		<-done
		t.Fatal("SSE handler did not query the initial task state")
	}
	terminal := current.Clone()
	terminal.Status = task.TaskCompleted
	terminal.Progress = 100
	terminal.CurrentStage = "completed"
	terminal.CurrentMessage = "反编译结果已生成"
	if err := innerRepo.Update(context.Background(), terminal); err != nil {
		cancelRequest()
		<-done
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		cancelRequest()
		<-done
		t.Fatal("SSE handler did not observe the terminal file-worker task state")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("SSE status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "private, no-cache, no-store" {
		t.Fatalf("SSE Cache-Control = %q", got)
	}
	body := response.Body.String()
	if !strings.Contains(body, `"type":"complete"`) || !strings.Contains(body, `"status":"completed"`) {
		t.Fatalf("SSE response did not include authoritative terminal state: %s", body)
	}
	assertBrokerStreamRemoved(t, broker, taskID)
}

type firstGetSignalingRepository struct {
	task.Repository
	firstGet chan struct{}
	once     sync.Once
}

func (r *firstGetSignalingRepository) Get(ctx context.Context, taskID string) (*task.Task, error) {
	current, err := r.Repository.Get(ctx, taskID)
	r.once.Do(func() { close(r.firstGet) })
	return current, err
}

func newTaskHandlerTestRouter(query *app.TaskQueryService, broker *events.Broker) *gin.Engine {
	gin.SetMode(gin.TestMode)
	handler := NewTaskHandler(query, broker)
	router := gin.New()
	router.GET("/tasks/:taskId", handler.GetTask)
	router.GET("/events", handler.StreamTaskEvents)
	return router
}

func assertBrokerStreamRemoved(t *testing.T, broker *events.Broker, taskID string) {
	t.Helper()
	eventStream, _, cancel, err := broker.Subscribe(taskID)
	if cancel != nil {
		cancel()
	}
	if eventStream != nil || !errors.Is(err, events.ErrStreamNotFound) {
		t.Fatalf("broker retained terminal stream %q: stream=%v err=%v", taskID, eventStream != nil, err)
	}
}
