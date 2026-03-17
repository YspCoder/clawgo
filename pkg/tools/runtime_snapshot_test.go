package tools

import (
	"context"
	"testing"
	"time"
)

func TestSubagentManagerRuntimeSnapshot(t *testing.T) {
	workspace := t.TempDir()
	manager := NewSubagentManager(nil, workspace, nil)
	manager.SetRunFunc(func(ctx context.Context, run *SubagentRun) (string, error) {
		return "snapshot-result", nil
	})
	run, err := manager.SpawnRun(context.Background(), SubagentSpawnOptions{
		Task:          "implement snapshot support",
		AgentID:       "coder",
		OriginChannel: "cli",
		OriginChatID:  "direct",
	})
	if err != nil {
		t.Fatalf("spawn run failed: %v", err)
	}
	if _, _, err := manager.waitRun(context.Background(), run.ID); err != nil {
		t.Fatalf("wait run failed: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	snapshot := manager.RuntimeSnapshot(20)
	if len(snapshot.Requests) == 0 || len(snapshot.Runs) == 0 {
		t.Fatalf("expected runtime snapshot to include request and run records: %+v", snapshot)
	}
	if snapshot.Runs[0].RequestID == "" {
		t.Fatalf("expected runtime snapshot run record to expose request_id: %+v", snapshot.Runs[0])
	}
	if len(snapshot.Threads) == 0 || len(snapshot.Artifacts) == 0 {
		t.Fatalf("expected runtime snapshot to include thread and artifact records: %+v", snapshot)
	}
	msgArtifact := snapshot.Artifacts[0]
	if msgArtifact.RequestID == "" {
		t.Fatalf("expected runtime snapshot artifact to expose request_id, got %+v", msgArtifact)
	}
	if msgArtifact.SourceType != "agent_message" {
		t.Fatalf("expected agent message artifact source type, got %+v", msgArtifact)
	}
	if msgArtifact.FromAgent == "" || msgArtifact.ToAgent == "" || msgArtifact.Name == "" {
		t.Fatalf("expected runtime snapshot artifact to preserve message metadata, got %+v", msgArtifact)
	}
}
