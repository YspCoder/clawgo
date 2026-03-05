package agent

import "testing"

func TestWithToolContextArgsInjectsDefaults(t *testing.T) {
	args := map[string]interface{}{"message": "hello"}
	got := withToolContextArgs("message", args, "telegram", "chat-1")
	if got["channel"] != "telegram" {
		t.Fatalf("expected channel injected, got %v", got["channel"])
	}
	if got["chat_id"] != "chat-1" {
		t.Fatalf("expected chat_id injected, got %v", got["chat_id"])
	}
}

func TestWithToolContextArgsPreservesExplicitTarget(t *testing.T) {
	args := map[string]interface{}{"message": "hello", "to": "target-2"}
	got := withToolContextArgs("message", args, "telegram", "chat-1")
	if _, ok := got["chat_id"]; ok {
		t.Fatalf("chat_id should not be injected when 'to' is provided")
	}
	if got["to"] != "target-2" {
		t.Fatalf("expected to preserved, got %v", got["to"])
	}
}

func TestWithToolContextArgsSkipsUnrelatedTools(t *testing.T) {
	args := map[string]interface{}{"query": "x"}
	got := withToolContextArgs("memory_search", args, "telegram", "chat-1")
	if len(got) != len(args) {
		t.Fatalf("expected unchanged args for unrelated tool")
	}
	if _, ok := got["channel"]; ok {
		t.Fatalf("unexpected channel key for unrelated tool")
	}
}
