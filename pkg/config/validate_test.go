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

func TestValidateSubagentsRejectsNodeTransport(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Agents.Subagents["coder"] = SubagentConfig{
		Enabled:          true,
		Transport:        "node",
		SystemPromptFile: "agents/coder/AGENT.md",
		Runtime: SubagentRuntimeConfig{
			Provider: "openai",
		},
	}

	errs := Validate(cfg)
	if len(errs) == 0 {
		t.Fatalf("expected validation errors")
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "agents.subagents.coder.transport") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected transport validation error, got %v", errs)
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
