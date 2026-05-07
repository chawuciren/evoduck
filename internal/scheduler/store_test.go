package scheduler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	cronpkg "github.com/chawuciren/evoduck/internal/cron"
)

type noopExecutor struct{}

func (noopExecutor) Execute(req *ExecutionRequest) error { return nil }

func TestStoreLoadAllSkipsInvalidSchedules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.jsonl")
	content := strings.Join([]string{
		`{"id":"","scope":"","name":"","schedule":"","enabled":false}`,
		`{"id":"sched-1","scope":"user","name":"daily","schedule":"0 9 * * *","enabled":true}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write tasks file: %v", err)
	}

	store := NewStore(path)
	schedules, err := store.LoadAll()
	if err != nil {
		t.Fatalf("load schedules: %v", err)
	}
	if len(schedules) != 1 {
		t.Fatalf("expected 1 valid schedule, got %d", len(schedules))
	}
	if schedules[0].ID != "sched-1" {
		t.Fatalf("expected valid schedule to remain, got %q", schedules[0].ID)
	}
}

func TestServiceSaveSkipsInvalidSchedules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.jsonl")
	service := NewService(cronpkg.New(), NewStore(path), nil, noopExecutor{})
	service.tasks[""] = ScheduleRecord{UpdatedAt: 1}
	service.tasks["sched-1"] = ScheduleRecord{ID: "sched-1", Scope: ScheduleScopeUser, Name: "daily", Schedule: "0 9 * * *", Prompt: "do work", Enabled: true}

	if err := service.Save(); err != nil {
		t.Fatalf("save schedules: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tasks file: %v", err)
	}
	text := string(data)
	if strings.Contains(text, `"id":""`) {
		t.Fatalf("expected invalid schedule to be omitted, file content: %s", text)
	}
	if !strings.Contains(text, `"id":"sched-1"`) {
		t.Fatalf("expected valid schedule to remain, file content: %s", text)
	}
}
