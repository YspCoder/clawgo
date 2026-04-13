package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/providers"
)

type fallbackTestProvider struct {
	response *providers.LLMResponse
	err      error
	options  map[string]interface{}
}

func (p *fallbackTestProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	p.options = map[string]interface{}{}
	for k, v := range options {
		p.options[k] = v
	}
	if p.err != nil {
		return nil, p.err
	}
	return p.response, nil
}

func (p *fallbackTestProvider) GetDefaultModel() string { return "fallback-model" }

type sequenceProvider struct {
	responses []*providers.LLMResponse
	errs      []error
}

func (p *sequenceProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	if len(p.responses) == 0 && len(p.errs) == 0 {
		return &providers.LLMResponse{Content: "ok", FinishReason: "stop"}, nil
	}
	resp := (*providers.LLMResponse)(nil)
	if len(p.responses) > 0 {
		resp = p.responses[0]
		p.responses = p.responses[1:]
	}
	var err error
	if len(p.errs) > 0 {
		err = p.errs[0]
		p.errs = p.errs[1:]
	}
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return &providers.LLMResponse{Content: "ok", FinishReason: "stop"}, nil
	}
	return resp, nil
}

func (p *sequenceProvider) GetDefaultModel() string { return "sequence-model" }

func TestBuildResponsesOptionsAddsCodexExecutionSession(t *testing.T) {
	loop := &AgentLoop{
		sessionProvider: map[string]string{
			"chat-1": "codex",
		},
	}

	options := loop.buildResponsesOptions("chat-1", 8192, 0.7)
	if got := options["codex_execution_session"]; got != "chat-1" {
		t.Fatalf("expected codex_execution_session chat-1, got %#v", got)
	}
}

func TestBuildResponsesOptionsSkipsCodexExecutionSessionForOtherProviders(t *testing.T) {
	loop := &AgentLoop{
		sessionProvider: map[string]string{
			"chat-1": "claude",
		},
	}

	options := loop.buildResponsesOptions("chat-1", 8192, 0.7)
	if _, ok := options["codex_execution_session"]; ok {
		t.Fatalf("expected no codex_execution_session for non-codex provider, got %#v", options["codex_execution_session"])
	}
}

func TestSyncSessionDefaultProviderOverridesStaleSessionProvider(t *testing.T) {
	loop := &AgentLoop{
		providerNames: []string{"openai"},
		sessionProvider: map[string]string{
			"chat-1": "codex",
		},
	}

	loop.syncSessionDefaultProvider("chat-1")

	if got := loop.getSessionProvider("chat-1"); got != "openai" {
		t.Fatalf("expected stale session provider to be replaced with current default, got %q", got)
	}
}

func TestSyncSessionDefaultProviderKeepsKnownSessionProvider(t *testing.T) {
	loop := &AgentLoop{
		providerNames: []string{"openai", "claude"},
		sessionProvider: map[string]string{
			"chat-1": "claude",
		},
	}

	loop.syncSessionDefaultProvider("chat-1")

	if got := loop.getSessionProvider("chat-1"); got != "claude" {
		t.Fatalf("expected valid session provider to be preserved, got %q", got)
	}
}

func TestMaxTokensForSessionUsesProviderOverride(t *testing.T) {
	loop := &AgentLoop{
		maxTokens:     4096,
		providerNames: []string{"openai"},
		sessionProvider: map[string]string{
			"chat-1": "claude",
		},
		providerMaxTokens: map[string]int{
			"claude": 16384,
		},
	}

	if got := loop.maxTokensForSession("chat-1"); got != 16384 {
		t.Fatalf("expected provider max_tokens override, got %d", got)
	}
}

func TestMaxTokensForSessionFallsBackToAgentDefault(t *testing.T) {
	loop := &AgentLoop{
		maxTokens:         4096,
		providerNames:     []string{"openai"},
		sessionProvider:   map[string]string{},
		providerMaxTokens: map[string]int{},
	}

	if got := loop.maxTokensForSession("chat-1"); got != 4096 {
		t.Fatalf("expected fallback to agent default max_tokens, got %d", got)
	}
}

func TestTemperatureForSessionUsesProviderOverride(t *testing.T) {
	loop := &AgentLoop{
		temperature:   0.7,
		providerNames: []string{"openai"},
		sessionProvider: map[string]string{
			"chat-1": "claude",
		},
		providerTemperatures: map[string]float64{
			"claude": 0.15,
		},
	}

	if got := loop.temperatureForSession("chat-1"); got != 0.15 {
		t.Fatalf("expected provider temperature override, got %v", got)
	}
}

func TestTemperatureForSessionFallsBackToAgentDefault(t *testing.T) {
	loop := &AgentLoop{
		temperature:          0.7,
		providerNames:        []string{"openai"},
		sessionProvider:      map[string]string{},
		providerTemperatures: map[string]float64{},
	}

	if got := loop.temperatureForSession("chat-1"); got != 0.7 {
		t.Fatalf("expected fallback to agent default temperature, got %v", got)
	}
}

func TestTryFallbackProvidersUsesFallbackProviderOptionsAndPersistsSelection(t *testing.T) {
	fallback := &fallbackTestProvider{
		response: &providers.LLMResponse{Content: "fallback", FinishReason: "stop"},
	}
	loop := &AgentLoop{
		maxTokens:            4096,
		temperature:          0.7,
		providerNames:        []string{"openai", "claude"},
		sessionProvider:      map[string]string{"chat-1": "openai"},
		providerPool:         map[string]providers.LLMProvider{"claude": fallback},
		providerChain:        []providerCandidate{{name: "openai", model: "gpt-a"}, {name: "claude", model: "claude-b"}},
		providerMaxTokens:    map[string]int{"claude": 16384},
		providerTemperatures: map[string]float64{"claude": 0.15},
		providerResponses: map[string]config.ProviderResponsesConfig{
			"claude": {WebSearchEnabled: true},
		},
	}

	resp, providerName, attempts, err := loop.tryFallbackProviders(context.Background(), bus.InboundMessage{SessionKey: "chat-1"}, nil, nil, errors.New("primary failed"))
	if err != nil {
		t.Fatalf("expected fallback success, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected one fallback attempt, got %d", attempts)
	}
	if resp == nil || resp.Content != "fallback" {
		t.Fatalf("unexpected fallback response: %#v", resp)
	}
	if providerName != "claude" {
		t.Fatalf("expected provider claude, got %q", providerName)
	}
	if got := loop.getSessionProvider("chat-1"); got != "claude" {
		t.Fatalf("expected session provider to switch to fallback provider, got %q", got)
	}
	if got := fallback.options["max_tokens"]; got != int64(16384) {
		t.Fatalf("expected fallback max_tokens 16384, got %#v", got)
	}
	if got := fallback.options["temperature"]; got != 0.15 {
		t.Fatalf("expected fallback temperature 0.15, got %#v", got)
	}
	if _, ok := fallback.options["responses_tools"]; !ok {
		t.Fatalf("expected fallback responses_tools to be populated")
	}
}

func TestProcessMessageDoesNotPersistPartialAssistantToolHistoryOnFailure(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = t.TempDir()
	cfg.Agents.Defaults.MaxToolIterations = 2

	provider := &sequenceProvider{
		responses: []*providers.LLMResponse{
			{
				Content: "",
				ToolCalls: []providers.ToolCall{
					{ID: "tool-1", Name: "read_file", Arguments: map[string]interface{}{"path": "missing.txt"}},
				},
				FinishReason: "tool_calls",
			},
		},
		errs: []error{nil, errors.New("second pass failed")},
	}

	loop := NewAgentLoop(cfg, bus.NewMessageBus(), provider, nil)
	_, err := loop.processMessage(context.Background(), bus.InboundMessage{
		Channel:    "cli",
		ChatID:     "direct",
		SenderID:   "user",
		SessionKey: "cli:direct",
		Content:    "read file",
	})
	if err == nil {
		t.Fatalf("expected processMessage error")
	}

	history := loop.sessions.GetHistory("cli:direct")
	if len(history) != 1 {
		t.Fatalf("expected only user message persisted on failure, got %d entries: %#v", len(history), history)
	}
	if history[0].Role != "user" || history[0].Content != "read file" {
		t.Fatalf("unexpected persisted history: %#v", history)
	}
}
