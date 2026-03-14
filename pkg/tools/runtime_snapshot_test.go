package tools

import (
	"context"
	"testing"
	"time"
)

func TestSubagentManagerRuntimeSnapshot(t *testing.T) {
	workspace := t.TempDir()
	manager := NewSubagentManager(nil, workspace, nil)
	manager.SetRunFunc(func(ctx context.Context, task *SubagentTask) (string, error) {
		return "snapshot-result", nil
	})
	task, err := manager.SpawnTask(context.Background(), SubagentSpawnOptions{
		Task:          "implement snapshot support",
		AgentID:       "coder",
		OriginChannel: "cli",
		OriginChatID:  "direct",
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
	if len(snapshot.Threads) == 0 || len(snapshot.Artifacts) == 0 {
		t.Fatalf("expected runtime snapshot to include thread and artifact records: %+v", snapshot)
	}
	msgArtifact := snapshot.Artifacts[0]
	if msgArtifact.SourceType != "agent_message" {
		t.Fatalf("expected agent message artifact source type, got %+v", msgArtifact)
	}
	if msgArtifact.FromAgent == "" || msgArtifact.ToAgent == "" || msgArtifact.Name == "" {
		t.Fatalf("expected runtime snapshot artifact to preserve message metadata, got %+v", msgArtifact)
	}
}
