package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSubagentRunStoreReloadsFromMetaAndJSONL(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	store := NewSubagentRunStore(workspace)
	run := &SubagentRun{
		ID:      "subagent-0007",
		Task:    "review deploy plan",
		AgentID: "worker-a",
		Status:  RuntimeStatusCompleted,
		Result:  "done",
		Created: 10,
		Updated: 20,
	}
	if err := store.AppendRun(run); err != nil {
		t.Fatalf("append run: %v", err)
	}
	if err := store.AppendEvent(SubagentRunEvent{
		RunID:   run.ID,
		AgentID: run.AgentID,
		Type:    "completed",
		Status:  RuntimeStatusCompleted,
		Message: "done",
		At:      20,
	}); err != nil {
		t.Fatalf("append event: %v", err)
	}

	reloaded := NewSubagentRunStore(workspace)
	if got, ok := reloaded.Get(run.ID); !ok || got.Result != "done" {
		t.Fatalf("expected run restored, got %#v ok=%v", got, ok)
	}
	if seed := reloaded.NextIDSeed(); seed != 8 {
		t.Fatalf("expected next seed 8, got %d", seed)
	}
	events, err := reloaded.Events(run.ID, 10)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(events) != 1 || events[0].Type != "completed" {
		t.Fatalf("unexpected events: %#v", events)
	}

	runtimeDir := filepath.Join(workspace, "agents", "runtime")
	if err := os.Remove(filepath.Join(runtimeDir, "subagent_runs.meta.json")); err != nil {
		t.Fatalf("remove run meta: %v", err)
	}
	if err := os.Remove(filepath.Join(runtimeDir, "subagent_events.meta.json")); err != nil {
		t.Fatalf("remove event meta: %v", err)
	}
	rebuilt := NewSubagentRunStore(workspace)
	if seed := rebuilt.NextIDSeed(); seed != 8 {
		t.Fatalf("expected rebuilt next seed 8, got %d", seed)
	}
	events, err = rebuilt.Events(run.ID, 10)
	if err != nil {
		t.Fatalf("events after rebuild: %v", err)
	}
	if len(events) != 1 || events[0].Message != "done" {
		t.Fatalf("unexpected rebuilt events: %#v", events)
	}
}
