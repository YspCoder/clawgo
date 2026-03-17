package config

import (
	"path/filepath"
	"strings"
	"testing"
)

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
			Provider: "openai",
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
	absolutePrompt := filepath.Join(t.TempDir(), "AGENT.md")
	cfg.Agents.Subagents["coder"] = SubagentConfig{
		Enabled:          true,
		SystemPromptFile: absolutePrompt,
		Runtime: SubagentRuntimeConfig{
			Provider: "openai",
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
			Provider: "openai",
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
			Provider: "openai",
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

func TestValidateGatewayNodeP2PIceServersAllowsStunOnly(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Gateway.Nodes.P2P.ICEServers = []GatewayICEConfig{
		{URLs: []string{"stun:stun.l.google.com:19302"}},
	}

	if errs := Validate(cfg); len(errs) != 0 {
		t.Fatalf("expected config to be valid, got %v", errs)
	}
}

func TestValidateGatewayNodeP2PIceServersRequireTurnCredentials(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Gateway.Nodes.P2P.ICEServers = []GatewayICEConfig{
		{URLs: []string{"turn:turn.example.com:3478?transport=udp"}},
	}

	if errs := Validate(cfg); len(errs) == 0 {
		t.Fatalf("expected validation errors")
	}
}

func TestValidateGatewayNodeDispatchRejectsEmptyTagKey(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Gateway.Nodes.Dispatch.ActionTags = map[string][]string{
		"": {"vision"},
	}

	if errs := Validate(cfg); len(errs) == 0 {
		t.Fatalf("expected validation errors")
	}
}

func TestValidateGatewayNodeDispatchRejectsEmptyAllowNodeKey(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Gateway.Nodes.Dispatch.AllowActions = map[string][]string{
		"": {"screen_snapshot"},
	}

	if errs := Validate(cfg); len(errs) == 0 {
		t.Fatalf("expected validation errors")
	}
}

func TestValidateSentinelWebhookURLRejectsInvalidScheme(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Sentinel.WebhookURL = "ftp://example.com/hook"

	if errs := Validate(cfg); len(errs) == 0 {
		t.Fatalf("expected validation errors")
	}
}

func TestValidateSentinelWebhookURLAllowsHTTPS(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Sentinel.WebhookURL = "https://example.com/hook"

	for _, err := range Validate(cfg) {
		if strings.Contains(err.Error(), "sentinel.webhook_url") {
			t.Fatalf("unexpected webhook validation error: %v", err)
		}
	}
}

func TestDefaultConfigSetsNodeArtifactRetentionDefaults(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	if cfg.Gateway.Nodes.Artifacts.Enabled {
		t.Fatalf("expected node artifact retention disabled by default")
	}
	if cfg.Gateway.Nodes.Artifacts.KeepLatest != 500 {
		t.Fatalf("unexpected default keep_latest: %d", cfg.Gateway.Nodes.Artifacts.KeepLatest)
	}
	if cfg.Gateway.Nodes.Artifacts.RetainDays != 7 {
		t.Fatalf("unexpected default retain_days: %d", cfg.Gateway.Nodes.Artifacts.RetainDays)
	}
	if !cfg.Gateway.Nodes.Artifacts.PruneOnRead {
		t.Fatalf("expected prune_on_read enabled by default")
	}
}

func TestValidateNodeArtifactRetentionRequiresPositiveKeepLatestWhenEnabled(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Gateway.Nodes.Artifacts.Enabled = true
	cfg.Gateway.Nodes.Artifacts.KeepLatest = 0

	if errs := Validate(cfg); len(errs) == 0 {
		t.Fatalf("expected validation errors")
	}
}

func TestValidateNodeArtifactRetentionRejectsNegativeRetainDays(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Gateway.Nodes.Artifacts.RetainDays = -1

	if errs := Validate(cfg); len(errs) == 0 {
		t.Fatalf("expected validation errors")
	}
}

func TestValidateProviderOAuthAllowsEmptyModelsBeforeLogin(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	pc := cfg.Models.Providers["openai"]
	pc.Auth = "oauth"
	pc.Models = nil
	pc.OAuth = ProviderOAuthConfig{Provider: "codex"}
	cfg.Models.Providers["openai"] = pc

	if errs := Validate(cfg); len(errs) != 0 {
		t.Fatalf("expected oauth provider config to be valid before model sync, got %v", errs)
	}
}

func TestValidateProviderOAuthRequiresProviderName(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	pc := cfg.Models.Providers["openai"]
	pc.Auth = "oauth"
	pc.Models = nil
	pc.OAuth = ProviderOAuthConfig{}
	cfg.Models.Providers["openai"] = pc

	errs := Validate(cfg)
	if len(errs) == 0 {
		t.Fatalf("expected validation errors")
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "models.providers.openai.oauth.provider") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected oauth.provider validation error, got %v", errs)
	}
}

func TestValidateProviderHybridAllowsEmptyModels(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	pc := cfg.Models.Providers["openai"]
	pc.Auth = "hybrid"
	pc.APIKey = "sk-test"
	pc.Models = nil
	pc.OAuth = ProviderOAuthConfig{Provider: "codex"}
	cfg.Models.Providers["openai"] = pc

	if errs := Validate(cfg); len(errs) != 0 {
		t.Fatalf("expected hybrid provider config to be valid before model sync, got %v", errs)
	}
}

func TestValidateProviderHybridRequiresOAuthProvider(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	pc := cfg.Models.Providers["openai"]
	pc.Auth = "hybrid"
	pc.APIKey = "sk-test"
	pc.Models = nil
	pc.OAuth = ProviderOAuthConfig{}
	cfg.Models.Providers["openai"] = pc

	errs := Validate(cfg)
	if len(errs) == 0 {
		t.Fatalf("expected validation errors")
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "models.providers.openai.oauth.provider") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected oauth.provider validation error, got %v", errs)
	}
}
