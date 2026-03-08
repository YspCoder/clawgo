package config

import "testing"

func TestDefaultConfigGeneratesGatewayToken(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	if cfg.Gateway.Token == "" {
		t.Fatalf("expected default gateway token")
	}
}

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

func TestValidateNodeBackedSubagentAllowsMissingPromptFile(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Agents.Router.Enabled = true
	cfg.Agents.Router.MainAgentID = "main"
	cfg.Agents.Subagents["main"] = SubagentConfig{
		Enabled:          true,
		Type:             "router",
		SystemPromptFile: "agents/main/AGENT.md",
	}
	cfg.Agents.Subagents["node.edge.main"] = SubagentConfig{
		Enabled:   true,
		Type:      "worker",
		Transport: "node",
		NodeID:    "edge",
	}

	if errs := Validate(cfg); len(errs) != 0 {
		t.Fatalf("expected node-backed config to be valid, got %v", errs)
	}
}

func TestValidateSubagentsRejectsInvalidNotifyMainPolicy(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Agents.Subagents["coder"] = SubagentConfig{
		Enabled:          true,
		SystemPromptFile: "agents/coder/AGENT.md",
		NotifyMainPolicy: "loud",
		Runtime: SubagentRuntimeConfig{
			Proxy: "proxy",
		},
	}

	if errs := Validate(cfg); len(errs) == 0 {
		t.Fatalf("expected validation errors")
	}
}

func TestDefaultConfigDisablesGatewayNodeP2P(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	if cfg.Gateway.Nodes.P2P.Enabled {
		t.Fatalf("expected gateway node p2p to be disabled by default")
	}
	if cfg.Gateway.Nodes.P2P.Transport != "websocket_tunnel" {
		t.Fatalf("unexpected default gateway node p2p transport: %s", cfg.Gateway.Nodes.P2P.Transport)
	}
}

func TestValidateRejectsUnknownGatewayNodeP2PTransport(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Gateway.Nodes.P2P.Transport = "udp"

	if errs := Validate(cfg); len(errs) == 0 {
		t.Fatalf("expected validation errors")
	}
}
