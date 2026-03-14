package config

import "testing"

func TestNormalizedViewProjectsCoreAndRuntime(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agents.Router.Enabled = true
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
}
