package agent

import "testing"

func TestResolveMessageToolTargetUsesStringHelpers(t *testing.T) {
	channel, chat := resolveMessageToolTarget(map[string]interface{}{
		"channel": "telegram",
		"to":      "chat-2",
	}, "whatsapp", "chat-1")
	if channel != "telegram" || chat != "chat-2" {
		t.Fatalf("unexpected target: %s %s", channel, chat)
	}

	channel, chat = resolveMessageToolTarget(map[string]interface{}{}, "whatsapp", "chat-1")
	if channel != "whatsapp" || chat != "chat-1" {
		t.Fatalf("unexpected fallback target: %s %s", channel, chat)
	}
}

func TestNodeAgentTaskResultUsesCompatiblePayloadFields(t *testing.T) {
	if got := nodeAgentTaskResult(map[string]interface{}{"result": "done"}); got != "done" {
		t.Fatalf("unexpected result field extraction: %q", got)
	}
	if got := nodeAgentTaskResult(map[string]interface{}{"content": "fallback"}); got != "fallback" {
		t.Fatalf("unexpected content field extraction: %q", got)
	}
	if got := nodeAgentTaskResult(nil); got != "" {
		t.Fatalf("expected empty result for nil payload, got %q", got)
	}
}
