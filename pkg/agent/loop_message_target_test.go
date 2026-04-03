package agent

import "testing"

func TestResolveMessageToolTargetUsesStringHelpers(t *testing.T) {
	channel, chat := resolveMessageToolTarget(map[string]interface{}{
		"channel": "telegram",
		"to":      "chat-2",
	}, "weixin", "chat-1")
	if channel != "telegram" || chat != "chat-2" {
		t.Fatalf("unexpected target: %s %s", channel, chat)
	}

	channel, chat = resolveMessageToolTarget(map[string]interface{}{}, "weixin", "chat-1")
	if channel != "weixin" || chat != "chat-1" {
		t.Fatalf("unexpected fallback target: %s %s", channel, chat)
	}
}
