package config

import "testing"

func TestValidateSubagentsAllowsKnownPeers(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Agents.Router.Enabled = true
	cfg.Agents.Router.MainAgentID = "main"
	cfg.Agents.Subagents["main"] = SubagentConfig{
		Enabled:          true,
		Type:             "router",
		SystemPromptFile: "agents/main/AGENT.md",
		AcceptFrom:       []string{"user", "coder"},
		CanTalkTo:        []string{"coder"},
	}
	cfg.Agents.Subagents["coder"] = SubagentConfig{
		Enabled:          true,
		Type:             "worker",
		SystemPromptFile: "agents/coder/AGENT.md",
		AcceptFrom:       []string{"main"},
		CanTalkTo:        []string{"main"},
		Runtime: SubagentRuntimeConfig{
			Proxy: "proxy",
		},
	}

	if errs := Validate(cfg); len(errs) != 0 {
		t.Fatalf("expected config to be valid, got %v", errs)
	}
}

func TestValidateSubagentsRejectsUnknownPeer(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Agents.Subagents["coder"] = SubagentConfig{
		Enabled:          true,
		SystemPromptFile: "agents/coder/AGENT.md",
		AcceptFrom:       []string{"main"},
	}

	if errs := Validate(cfg); len(errs) == 0 {
		t.Fatalf("expected validation errors")
	}
}

func TestValidateSubagentsRejectsAbsolutePromptFile(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Agents.Subagents["coder"] = SubagentConfig{
		Enabled:          true,
		SystemPromptFile: "/tmp/AGENT.md",
		Runtime: SubagentRuntimeConfig{
			Proxy: "proxy",
		},
	}

	if errs := Validate(cfg); len(errs) == 0 {
		t.Fatalf("expected validation errors")
	}
}

func TestValidateSubagentsRequiresPromptFileWhenEnabled(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Agents.Subagents["coder"] = SubagentConfig{
		Enabled: true,
		Runtime: SubagentRuntimeConfig{
			Proxy: "proxy",
		},
	}

	if errs := Validate(cfg); len(errs) == 0 {
		t.Fatalf("expected validation errors")
	}
}
