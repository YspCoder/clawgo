package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"clawgo/pkg/providers"
)

type compactionTestProvider struct {
	errByModel     map[string]error
	summaryByModel map[string]string
	calledModels   []string
}

func (p *compactionTestProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	return &providers.LLMResponse{Content: "summary-fallback"}, nil
}

func (p *compactionTestProvider) GetDefaultModel() string {
	return ""
}

func (p *compactionTestProvider) SupportsResponsesCompact() bool {
	return true
}

func (p *compactionTestProvider) BuildSummaryViaResponsesCompact(ctx context.Context, model string, existingSummary string, messages []providers.Message, maxSummaryChars int) (string, error) {
	p.calledModels = append(p.calledModels, model)
	if err := p.errByModel[model]; err != nil {
		return "", err
	}
	if out := strings.TrimSpace(p.summaryByModel[model]); out != "" {
		return out, nil
	}
	return "", fmt.Errorf(`responses compact request failed (status 400): {"error":{"message":"model not found"}}`)
}

func TestShouldCompactBySize(t *testing.T) {
	history := []providers.Message{
		{Role: "user", Content: strings.Repeat("a", 80)},
		{Role: "assistant", Content: strings.Repeat("b", 80)},
	}

	if !shouldCompactBySize("", history, 120) {
		t.Fatalf("expected size-based compaction trigger")
	}
	if shouldCompactBySize("", history, 10000) {
		t.Fatalf("did not expect trigger for large threshold")
	}
}

func TestFormatCompactionTranscript_HeadTailWhenOversized(t *testing.T) {
	msgs := make([]providers.Message, 0, 30)
	for i := 0; i < 30; i++ {
		msgs = append(msgs, providers.Message{
			Role:    "user",
			Content: fmt.Sprintf("msg-%02d %s", i, strings.Repeat("x", 80)),
		})
	}

	out := formatCompactionTranscript(msgs, 700)
	if out == "" {
		t.Fatalf("expected non-empty transcript")
	}
	if !strings.Contains(out, "msg-00") {
		t.Fatalf("expected head messages preserved, got: %q", out)
	}
	if !strings.Contains(out, "msg-29") {
		t.Fatalf("expected tail messages preserved, got: %q", out)
	}
	if !strings.Contains(out, "messages omitted for compaction") {
		t.Fatalf("expected omitted marker, got: %q", out)
	}
	if len(out) > 700 {
		t.Fatalf("expected output <= max chars, got %d", len(out))
	}
}

func TestFormatCompactionTranscript_TrimsToolPayloadMoreAggressively(t *testing.T) {
	msgs := []providers.Message{
		{Role: "tool", Content: strings.Repeat("z", 2000)},
	}
	out := formatCompactionTranscript(msgs, 2000)
	if len(out) >= 1200 {
		t.Fatalf("expected tool content to be trimmed aggressively, got length %d", len(out))
	}
}

func TestBuildCompactedSummary_ResponsesCompactFallsBackToBackupProxyOnTimeout(t *testing.T) {
	primary := &compactionTestProvider{
		errByModel: map[string]error{
			"gpt-4o-mini": fmt.Errorf("failed to send request: Post \"https://primary/v1/chat/completions\": context deadline exceeded"),
		},
	}
	backup := &compactionTestProvider{
		summaryByModel: map[string]string{
			"deepseek-chat": "compacted summary",
		},
	}

	al := &AgentLoop{
		provider:       primary,
		proxy:          "primary",
		proxyFallbacks: []string{"backup"},
		model:          "gpt-4o-mini",
		providersByProxy: map[string]providers.LLMProvider{
			"primary": primary,
			"backup":  backup,
		},
		modelsByProxy: map[string][]string{
			"primary": []string{"gpt-4o-mini"},
			"backup":  []string{"deepseek-chat"},
		},
	}

	out, err := al.buildCompactedSummary(context.Background(), "", []providers.Message{{Role: "user", Content: "a"}}, 2000, 1200, "responses_compact")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != "compacted summary" {
		t.Fatalf("unexpected summary: %q", out)
	}
	if al.proxy != "backup" {
		t.Fatalf("expected proxy switched to backup, got %q", al.proxy)
	}
	if al.model != "deepseek-chat" {
		t.Fatalf("expected model switched to deepseek-chat, got %q", al.model)
	}
}
