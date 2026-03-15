package config

import (
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

func TestValidateAgentsAllowsKnownPeers(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Agents.Agents["main"] = AgentConfig{
		Enabled:    true,
		Type:       "agent",
		PromptFile: "agents/main/AGENT.md",
	}
	cfg.Agents.Agents["coder"] = AgentConfig{
		Enabled:    true,
		Type:       "agent",
		PromptFile: "agents/coder/AGENT.md",
		Runtime: AgentRuntimeConfig{
			Provider: "openai",
		},
	}

	if errs := Validate(cfg); len(errs) != 0 {
		t.Fatalf("expected config to be valid, got %v", errs)
	}
}

func TestValidateAgentsRejectsAbsolutePromptFile(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Agents.Agents["coder"] = AgentConfig{
		Enabled:    true,
		PromptFile: "/tmp/AGENT.md",
		Runtime: AgentRuntimeConfig{
			Provider: "openai",
		},
	}

	if errs := Validate(cfg); len(errs) == 0 {
		t.Fatalf("expected validation errors")
	}
}

func TestValidateAgentsRequiresPromptFileWhenEnabled(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Agents.Agents["coder"] = AgentConfig{
		Enabled: true,
		Runtime: AgentRuntimeConfig{
			Provider: "openai",
		},
	}

	if errs := Validate(cfg); len(errs) == 0 {
		t.Fatalf("expected validation errors")
	}
}

func TestValidateNodeBackedAgentAllowsMissingPromptFile(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Agents.Agents["main"] = AgentConfig{
		Enabled:    true,
		Type:       "agent",
		PromptFile: "agents/main/AGENT.md",
	}
	cfg.Agents.Agents["node.edge.main"] = AgentConfig{
		Enabled:   true,
		Type:      "agent",
		Transport: "node",
		NodeID:    "edge",
	}

	if errs := Validate(cfg); len(errs) != 0 {
		t.Fatalf("expected node-backed config to be valid, got %v", errs)
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
