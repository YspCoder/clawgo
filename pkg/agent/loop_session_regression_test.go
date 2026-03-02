package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
	"clawgo/pkg/providers"
)

type recordingProvider struct {
	mu        sync.Mutex
	calls     [][]providers.Message
	responses []providers.LLMResponse
}

func (p *recordingProvider) Chat(_ context.Context, messages []providers.Message, _ []providers.ToolDefinition, _ string, _ map[string]interface{}) (*providers.LLMResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := make([]providers.Message, len(messages))
	copy(cp, messages)
	p.calls = append(p.calls, cp)
	if len(p.responses) == 0 {
		resp := providers.LLMResponse{Content: "ok", FinishReason: "stop"}
		return &resp, nil
	}
	resp := p.responses[0]
	p.responses = p.responses[1:]
	return &resp, nil
}

func (p *recordingProvider) GetDefaultModel() string { return "test-model" }

func setupLoop(t *testing.T, rp *recordingProvider) *AgentLoop {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = filepath.Join(t.TempDir(), "workspace")
	cfg.Agents.Defaults.MaxToolIterations = 2
	cfg.Agents.Defaults.ContextCompaction.Enabled = false
	return NewAgentLoop(cfg, bus.NewMessageBus(), rp, nil)
}

func lastUserContent(msgs []providers.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].Content
		}
	}
	return ""
}

func containsUserContent(msgs []providers.Message, needle string) bool {
	for _, m := range msgs {
		if m.Role == "user" && m.Content == needle {
			return true
		}
	}
	return false
}

func TestProcessDirect_UsesCallerSessionKey(t *testing.T) {
	rp := &recordingProvider{}
	loop := setupLoop(t, rp)

	if _, err := loop.ProcessDirect(context.Background(), "from-session-a", "session-a"); err != nil {
		t.Fatalf("ProcessDirect session-a failed: %v", err)
	}
	if _, err := loop.ProcessDirect(context.Background(), "from-session-b", "session-b"); err != nil {
		t.Fatalf("ProcessDirect session-b failed: %v", err)
	}

	if len(rp.calls) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(rp.calls))
	}
	second := rp.calls[1]
	if got := lastUserContent(second); got != "from-session-b" {
		t.Fatalf("unexpected last user content in second call: %q", got)
	}
	if containsUserContent(second, "from-session-a") {
		t.Fatalf("session-a message leaked into session-b history")
	}
}

func TestProcessSystemMessage_UsesOriginSessionKey(t *testing.T) {
	rp := &recordingProvider{}
	loop := setupLoop(t, rp)

	sys := bus.InboundMessage{Channel: "system", SenderID: "cron", ChatID: "telegram:chat-1", Content: "system task"}
	if _, err := loop.processMessage(context.Background(), sys); err != nil {
		t.Fatalf("processMessage(system) failed: %v", err)
	}
	if _, err := loop.ProcessDirect(context.Background(), "follow-up", "telegram:chat-1"); err != nil {
		t.Fatalf("ProcessDirect follow-up failed: %v", err)
	}

	if len(rp.calls) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(rp.calls))
	}
	second := rp.calls[1]
	want := "[System: cron] " + rewriteSystemMessageContent("system task", loop.systemRewriteTemplate)
	if !containsUserContent(second, want) {
		t.Fatalf("expected system marker in follow-up history, want=%q got=%v", want, summarizeUsers(second))
	}
}

func TestProcessInbound_SystemMessagePublishesToOriginChannel(t *testing.T) {
	rp := &recordingProvider{}
	loop := setupLoop(t, rp)

	in := bus.InboundMessage{Channel: "system", SenderID: "cron", ChatID: "telegram:chat-1", Content: "system task"}
	loop.processInbound(context.Background(), in)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, ok := loop.bus.SubscribeOutbound(ctx)
	if !ok {
		t.Fatalf("expected outbound message")
	}
	if out.Channel != "telegram" {
		t.Fatalf("expected outbound channel telegram, got %q", out.Channel)
	}
	if out.ChatID != "chat-1" {
		t.Fatalf("expected outbound chat_id chat-1, got %q", out.ChatID)
	}
}

func summarizeUsers(msgs []providers.Message) []string {
	out := []string{}
	for _, m := range msgs {
		if m.Role == "user" {
			out = append(out, fmt.Sprintf("%q", m.Content))
		}
	}
	return out
}
