package config

import "testing"

func TestNormalizedViewProjectsCoreAndRuntime(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agents.Router.Enabled = true
	cfg.Models.Providers["openai"] = ProviderConfig{
		APIBase:     "https://api.openai.com/v1",
		Models:      []string{"gpt-5.4"},
		MaxTokens:   12288,
		Temperature: 0.35,
		TimeoutSec:  90,
	}
	cfg.Agents.Subagents["coder"] = SubagentConfig{
		Enabled:          true,
		Role:             "coding",
		SystemPromptFile: "agents/coder/AGENT.md",
		Tools:            SubagentToolsConfig{Allowlist: []string{"shell"}},
		Runtime:          SubagentRuntimeConfig{Provider: "openai"},
	}

	view := cfg.NormalizedView()
	if view.Core.DefaultProvider != "openai" || view.Core.DefaultModel != "gpt-5.4" {
		t.Fatalf("unexpected default model projection: %+v", view.Core)
	}
	subcfg, ok := view.Core.Subagents["coder"]
	if !ok {
		t.Fatalf("expected normalized subagent")
	}
	if subcfg.Prompt != "agents/coder/AGENT.md" || subcfg.Provider != "openai" {
		t.Fatalf("unexpected normalized subagent: %+v", subcfg)
	}
	if !view.Runtime.Router.Enabled || view.Runtime.Router.Strategy != "rules_first" {
		t.Fatalf("unexpected runtime router: %+v", view.Runtime.Router)
	}
	if got := view.Runtime.Providers["openai"].MaxTokens; got != 12288 {
		t.Fatalf("expected provider max_tokens in normalized runtime view, got %d", got)
	}
	if got := view.Runtime.Providers["openai"].Temperature; got != 0.35 {
		t.Fatalf("expected provider temperature in normalized runtime view, got %v", got)
	}
}
