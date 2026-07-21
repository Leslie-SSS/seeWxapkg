package httpapi

import (
	"strings"
	"testing"

	"github.com/keepbuild/seewxapkg/internal/domain/task"
)

func TestTaskResponseSanitizesFailurePathsWithoutMutatingTask(t *testing.T) {
	rawCurrent := "处理 /data/tasks/task-1/input.wxapkg 失败"
	rawError := "open /data/output/task-1.zip; source /Users/person/project/app.js"
	current := &task.Task{
		ID:             "task-1",
		Status:         task.TaskFailed,
		CurrentMessage: rawCurrent,
		ErrorMessage:   &rawError,
		StageResults: []task.StageResult{{
			Stage:   "packaging",
			Metrics: map[string]interface{}{"fileCount": 2, "zipPath": "/data/output/task-1.zip"},
		}},
		ArtifactSummary: &task.ArtifactSummary{
			Files: []task.ArtifactFile{{
				Path: "/workspace/result/src/app.js", Kind: "js", Source: "legacy",
			}},
			SourceBreakdown: map[string]int{"native": 1, "/workspace/private/source": 2},
		},
	}

	dto := ToTaskResponseDTO(current)
	assertNoInternalPaths(t, dto.CurrentMessage)
	if dto.ErrorMessage == nil {
		t.Fatal("expected a public error message")
	}
	assertNoInternalPaths(t, *dto.ErrorMessage)
	if dto.Artifacts == nil || len(dto.Artifacts.Files) != 1 || strings.Contains(dto.Artifacts.Files[0].Path, "/workspace/") {
		t.Fatalf("artifact metadata exposed an internal path: %#v", dto.Artifacts)
	}
	if dto.Artifacts.Files[0].Source != "other" || dto.Artifacts.SourceBreakdown["other"] != 2 {
		t.Fatalf("artifact source labels exposed an undocumented value: %#v", dto.Artifacts)
	}
	if len(dto.Stages) != 1 || dto.Stages[0].Metrics["fileCount"] != 2 {
		t.Fatalf("public stage metrics lost a documented measurement: %#v", dto.Stages)
	}
	if _, ok := dto.Stages[0].Metrics["zipPath"]; ok {
		t.Fatalf("public stage metrics exposed legacy zipPath: %#v", dto.Stages[0].Metrics)
	}

	if current.CurrentMessage != rawCurrent || current.ErrorMessage == nil || *current.ErrorMessage != rawError {
		t.Fatalf("DTO conversion mutated internal task state: %#v", current)
	}
	*dto.ErrorMessage = "changed"
	if *current.ErrorMessage != rawError {
		t.Fatal("public error pointer aliases the persisted internal error")
	}
}

func TestTaskEventsSanitizeSnapshotAndBrokerHistoryPayloads(t *testing.T) {
	rawCurrent := "读取 /data/tasks/task-1/input.wxapkg 失败"
	rawError := "write /data/output/task-1.zip; inspect /Users/person/private/app.js"
	current := &task.Task{
		ID:             "task-1",
		Status:         task.TaskFailed,
		Progress:       100,
		CurrentMessage: rawCurrent,
		ErrorMessage:   &rawError,
	}

	snapshot := taskEventFromTask(current)
	assertNoInternalPaths(t, snapshot.Message)
	assertNoInternalPaths(t, snapshot.Error)

	history := sanitizeTaskEvent(task.TaskEvent{Message: rawCurrent, Error: rawError})
	assertNoInternalPaths(t, history.Message)
	assertNoInternalPaths(t, history.Error)
	if current.CurrentMessage != rawCurrent || *current.ErrorMessage != rawError {
		t.Fatal("event sanitization mutated the internal task")
	}
}

func assertNoInternalPaths(t *testing.T, value string) {
	t.Helper()
	for _, secret := range []string{"/data/tasks", "/data/output", "/Users"} {
		if strings.Contains(value, secret) {
			t.Fatalf("public value exposed %q: %s", secret, value)
		}
	}
}
