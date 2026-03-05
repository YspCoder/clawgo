package agent

import "testing"

func TestWithToolContextArgsPipelineAutoInject(t *testing.T) {
	args := map[string]interface{}{
		"objective": "build feature",
	}
	got := withToolContextArgs("pipeline_create", args, "telegram", "123")
	if got["channel"] != "telegram" {
		t.Fatalf("expected channel auto-injected for pipeline_create, got %v", got["channel"])
	}
	if got["chat_id"] != "123" {
		t.Fatalf("expected chat_id auto-injected for pipeline_create, got %v", got["chat_id"])
	}
}

func TestWithToolContextArgsPipelineRespectsExplicitTarget(t *testing.T) {
	args := map[string]interface{}{
		"pipeline_id": "p-1",
		"channel":     "webui",
		"chat_id":     "panel",
	}
	got := withToolContextArgs("pipeline_dispatch", args, "telegram", "123")
	if got["channel"] != "webui" {
		t.Fatalf("expected explicit channel preserved, got %v", got["channel"])
	}
	if got["chat_id"] != "panel" {
		t.Fatalf("expected explicit chat_id preserved, got %v", got["chat_id"])
	}
}

func TestWithToolContextArgsIgnoresUnrelatedTools(t *testing.T) {
	args := map[string]interface{}{
		"path": "README.md",
	}
	got := withToolContextArgs("read_file", args, "telegram", "123")
	if _, ok := got["channel"]; ok {
		t.Fatalf("did not expect channel for unrelated tool")
	}
	if _, ok := got["chat_id"]; ok {
		t.Fatalf("did not expect chat_id for unrelated tool")
	}
}
