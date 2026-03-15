package config

import "testing"

func TestNormalizedViewProjectsCoreAndRuntime(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agents.Agents["coder"] = AgentConfig{
		Enabled:    true,
		Role:       "coding",
		PromptFile: "agents/coder/AGENT.md",
		Tools:      AgentToolsConfig{Allowlist: []string{"shell"}},
		Runtime:    AgentRuntimeConfig{Provider: "openai"},
	}

	view := cfg.NormalizedView()
	if view.Core.DefaultProvider != "openai" || view.Core.DefaultModel != "gpt-5.4" {
		t.Fatalf("unexpected default model projection: %+v", view.Core)
	}
	subcfg, ok := view.Core.Agents["coder"]
	if !ok {
		t.Fatalf("expected normalized agent")
	}
	if subcfg.Prompt != "agents/coder/AGENT.md" || subcfg.Provider != "openai" {
		t.Fatalf("unexpected normalized agent: %+v", subcfg)
	}
	if len(view.Runtime.Providers) == 0 {
		t.Fatalf("expected normalized providers: %+v", view.Runtime)
	}
}
