package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/bus"
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

func TestProcessSystemMessageSubagentBlockedQueuedIntoDigest(t *testing.T) {
	msgBus := bus.NewMessageBus()
	al := &AgentLoop{
		bus:                 msgBus,
		subagentDigestDelay: 10 * time.Millisecond,
		subagentDigests:     map[string]*subagentDigestState{},
	}
	out, err := al.processSystemMessage(context.Background(), bus.InboundMessage{
		Channel:  "system",
		SenderID: "subagent:subagent-3",
		ChatID:   "telegram:9527",
		Content:  "Subagent update\nagent: coder\nrun: subagent-3\nstatus: blocked\nreason: blocked\ntask: 修复登录\nsummary: rate limit",
		Metadata: map[string]string{
			"trigger":       "subagent",
			"agent_id":      "coder",
			"status":        "failed",
			"notify_reason": "blocked",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "" {
		t.Fatalf("expected queued digest with no immediate output, got %q", out)
	}
	al.flushDueSubagentDigests(time.Now().Add(20 * time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	outbound, ok := msgBus.SubscribeOutbound(ctx)
	if !ok {
		t.Fatalf("expected outbound digest")
	}
	if !strings.Contains(outbound.Content, "阶段总结") || !strings.Contains(outbound.Content, "coder") || !strings.Contains(outbound.Content, "受阻") {
		t.Fatalf("unexpected digest content: %q", outbound.Content)
	}
}

func TestProcessSystemMessageSubagentDigestMergesMultipleUpdates(t *testing.T) {
	msgBus := bus.NewMessageBus()
	al := &AgentLoop{
		bus:                 msgBus,
		subagentDigestDelay: 10 * time.Millisecond,
		subagentDigests:     map[string]*subagentDigestState{},
	}
	first := bus.InboundMessage{
		Channel:  "system",
		SenderID: "subagent:subagent-7",
		ChatID:   "telegram:9527",
		Content:  "Subagent update\nagent: tester\nrun: subagent-7\nstatus: completed\nreason: final\ntask: 回归测试\nsummary: 所有测试通过",
		Metadata: map[string]string{
			"trigger":       "subagent",
			"agent_id":      "tester",
			"status":        "completed",
			"notify_reason": "final",
		},
	}
	second := bus.InboundMessage{
		Channel:  "system",
		SenderID: "subagent:subagent-8",
		ChatID:   "telegram:9527",
		Content:  "Subagent update\nagent: coder\nrun: subagent-8\nstatus: completed\nreason: final\ntask: 修复登录\nsummary: 接口已联调",
		Metadata: map[string]string{
			"trigger":       "subagent",
			"agent_id":      "coder",
			"status":        "completed",
			"notify_reason": "final",
		},
	}
	if out, err := al.processSystemMessage(context.Background(), first); err != nil || out != "" {
		t.Fatalf("unexpected first result out=%q err=%v", out, err)
	}
	if out, err := al.processSystemMessage(context.Background(), second); err != nil || out != "" {
		t.Fatalf("unexpected second result out=%q err=%v", out, err)
	}
	al.flushDueSubagentDigests(time.Now().Add(20 * time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	outbound, ok := msgBus.SubscribeOutbound(ctx)
	if !ok {
		t.Fatalf("expected merged outbound digest")
	}
	if strings.Count(outbound.Content, "\n- ") != 2 {
		t.Fatalf("expected two digest lines, got: %q", outbound.Content)
	}
	if !strings.Contains(outbound.Content, "完成 2") {
		t.Fatalf("expected aggregate completion count, got: %q", outbound.Content)
	}
}
