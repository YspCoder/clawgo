package agent

import (
	"strings"
	"testing"

	"clawgo/pkg/bus"
)

func TestPrepareOutboundSubagentNoReplyFallback(t *testing.T) {
	al := &AgentLoop{}
	msg := bus.InboundMessage{
		Channel:  "system",
		SenderID: "subagent:subagent-1",
		ChatID:   "telegram:9527",
		Content:  "Task 'coder' completed.\n\nResult:\nOK",
		Metadata: map[string]string{
			"trigger": "subagent",
		},
	}

	outbound, ok := al.prepareOutbound(msg, "NO_REPLY")
	if !ok {
		t.Fatalf("expected outbound notification for subagent NO_REPLY fallback")
	}
	if outbound.Channel != "telegram" || outbound.ChatID != "9527" {
		t.Fatalf("unexpected outbound target: %s:%s", outbound.Channel, outbound.ChatID)
	}
	if strings.TrimSpace(outbound.Content) != strings.TrimSpace(msg.Content) {
		t.Fatalf("expected fallback content from system message, got: %q", outbound.Content)
	}
}

func TestPrepareOutboundNoReplySuppressedForNonSubagent(t *testing.T) {
	al := &AgentLoop{}
	msg := bus.InboundMessage{
		Channel: "cli",
		ChatID:  "direct",
		Content: "hello",
	}

	if _, ok := al.prepareOutbound(msg, "NO_REPLY"); ok {
		t.Fatalf("expected NO_REPLY to be suppressed for non-subagent messages")
	}
}

func TestPrepareOutboundSubagentNoReplyFallbackWithMissingOrigin(t *testing.T) {
	al := &AgentLoop{}
	msg := bus.InboundMessage{
		Channel:  "system",
		SenderID: "subagent:subagent-9",
		ChatID:   ":",
		Metadata: map[string]string{
			"trigger": "subagent",
		},
	}

	outbound, ok := al.prepareOutbound(msg, "NO_REPLY")
	if !ok {
		t.Fatalf("expected outbound notification for malformed system origin")
	}
	if outbound.Channel != "cli" || outbound.ChatID != "direct" {
		t.Fatalf("expected fallback origin cli:direct, got %s:%s", outbound.Channel, outbound.ChatID)
	}
	if outbound.Content != "Subagent subagent-9 completed." {
		t.Fatalf("unexpected fallback content: %q", outbound.Content)
	}
}
