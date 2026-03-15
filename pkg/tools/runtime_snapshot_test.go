package tools

import (
	"context"
	"testing"
	"time"
)

func TestAgentManagerRuntimeSnapshot(t *testing.T) {
	workspace := t.TempDir()
	manager := NewAgentManager(nil, workspace, nil)
	manager.SetRunFunc(func(ctx context.Context, task *AgentTask) (string, error) {
		return "snapshot-result", nil
	})
	task, err := manager.SpawnTask(context.Background(), AgentSpawnOptions{
		Task:          "implement snapshot support",
		AgentID:       "coder",
		Origin:        &OriginRef{Channel: "cli", ChatID: "direct"},
	})
	if err != nil {
		t.Fatalf("spawn task failed: %v", err)
	}
	if _, _, err := manager.WaitTask(context.Background(), task.ID); err != nil {
		t.Fatalf("wait task failed: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	snapshot := manager.RuntimeSnapshot(20)
	if len(snapshot.Tasks) == 0 || len(snapshot.Runs) == 0 {
		t.Fatalf("expected runtime snapshot to include task and run records: %+v", snapshot)
	}
	if len(snapshot.Events) == 0 {
		t.Fatalf("expected runtime snapshot to include events: %+v", snapshot)
	}
}
