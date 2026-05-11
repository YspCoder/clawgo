package providers

import (
	"fmt"
	"github.com/YspCoder/clawgo/pkg/config"
	"strings"
	"time"
)

func normalizeProviderRouteName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "geminicli", "gemini_cli":
		return "gemini-cli"
	case "aistudio", "ai-studio", "ai_studio", "google-ai-studio", "google_ai_studio", "googleaistudio":
		return "aistudio"
	case "google", "gemini-api-key", "gemini_api_key":
		return "gemini"
	case "anthropic", "claude-code", "claude_code", "claude-api-key", "claude_api_key":
		return "claude"
	case "openai-compatibility", "openai_compatibility", "openai-compat", "openai_compat":
		return "openai-compatibility"
	case "vertex-api-key", "vertex_api_key", "vertex-compat", "vertex_compat", "vertex-compatibility", "vertex_compatibility":
		return "vertex"
	case "codex-api-key", "codex_api_key":
		return "codex"
	case "i-flow", "i_flow":
		return "iflow"
	default:
		return strings.TrimSpace(name)
	}
}

func CreateProvider(cfg *config.Config) (LLMProvider, error) {
	name := config.PrimaryProviderName(cfg)
	provider, err := CreateProviderByName(cfg, name)
	if err != nil {
		return nil, err
	}
	_, model := config.ParseProviderModelRef(cfg.Agents.Defaults.Model.Primary)
	if hp, ok := provider.(*HTTPProvider); ok && strings.TrimSpace(model) != "" {
		hp.defaultModel = strings.TrimSpace(model)
	}
	return provider, nil
}

func CreateProviderByName(cfg *config.Config, name string) (LLMProvider, error) {
	routeName := normalizeProviderRouteName(name)
	pc, err := getProviderConfigByName(cfg, routeName)
	if err != nil {
		return nil, err
	}
	ConfigureProviderRuntime(routeName, pc)
	oauthProvider := normalizeOAuthProvider(pc.OAuth.Provider)
	if pc.APIBase == "" &&
		oauthProvider != defaultAntigravityOAuthProvider &&
		oauthProvider != defaultGeminiOAuthProvider &&
		oauthProvider != "aistudio" &&
		oauthProvider != defaultCodexOAuthProvider &&
		oauthProvider != defaultClaudeOAuthProvider &&
		oauthProvider != defaultQwenOAuthProvider &&
		oauthProvider != defaultKimiOAuthProvider &&
		oauthProvider != defaultIFlowOAuthProvider &&
		!strings.EqualFold(routeName, "gemini-cli") &&
		!strings.EqualFold(routeName, "aistudio") &&
		!strings.EqualFold(routeName, "vertex") &&
		!strings.EqualFold(routeName, defaultAntigravityOAuthProvider) &&
		!strings.EqualFold(routeName, defaultGeminiOAuthProvider) &&
		!strings.EqualFold(routeName, defaultCodexOAuthProvider) &&
		!strings.EqualFold(routeName, defaultClaudeOAuthProvider) &&
		!strings.EqualFold(routeName, defaultQwenOAuthProvider) &&
		!strings.EqualFold(routeName, defaultKimiOAuthProvider) &&
		!strings.EqualFold(routeName, defaultIFlowOAuthProvider) {
		return nil, fmt.Errorf("no API base configured for provider %q", name)
	}
	if pc.TimeoutSec <= 0 {
		return nil, fmt.Errorf("invalid timeout_sec for provider %q: %d", name, pc.TimeoutSec)
	}
	defaultModel := ""
	if len(pc.Models) > 0 {
		defaultModel = pc.Models[0]
	}
	var oauth *oauthManager
	if strings.EqualFold(strings.TrimSpace(pc.Auth), "oauth") || strings.EqualFold(strings.TrimSpace(pc.Auth), "hybrid") {
		oauth, err = newOAuthManager(pc, time.Duration(pc.TimeoutSec)*time.Second)
		if err != nil {
			return nil, err
		}
	}
	if oauthProvider == defaultAntigravityOAuthProvider || strings.EqualFold(routeName, defaultAntigravityOAuthProvider) {
		return NewAntigravityProvider(routeName, pc.APIKey, pc.APIBase, defaultModel, pc.SupportsResponsesCompact, pc.Auth, time.Duration(pc.TimeoutSec)*time.Second, oauth), nil
	}
	if oauthProvider == "aistudio" || strings.EqualFold(routeName, "aistudio") {
		return NewAistudioProvider(routeName, pc.APIKey, pc.APIBase, defaultModel, pc.SupportsResponsesCompact, pc.Auth, time.Duration(pc.TimeoutSec)*time.Second, oauth), nil
	}
	if strings.EqualFold(routeName, "gemini-cli") {
		return NewGeminiCLIProvider(routeName, pc.APIKey, pc.APIBase, defaultModel, pc.SupportsResponsesCompact, pc.Auth, time.Duration(pc.TimeoutSec)*time.Second, oauth), nil
	}
	if oauthProvider == defaultGeminiOAuthProvider || strings.EqualFold(routeName, defaultGeminiOAuthProvider) || strings.EqualFold(routeName, "aistudio") {
		return NewGeminiProvider(routeName, pc.APIKey, pc.APIBase, defaultModel, pc.SupportsResponsesCompact, pc.Auth, time.Duration(pc.TimeoutSec)*time.Second, oauth), nil
	}
	if strings.EqualFold(routeName, "vertex") {
		return NewVertexProvider(routeName, pc.APIKey, pc.APIBase, defaultModel, pc.SupportsResponsesCompact, pc.Auth, time.Duration(pc.TimeoutSec)*time.Second, oauth), nil
	}
	if oauthProvider == defaultCodexOAuthProvider || strings.EqualFold(routeName, defaultCodexOAuthProvider) {
		return NewCodexProvider(routeName, pc.APIKey, pc.APIBase, defaultModel, pc.SupportsResponsesCompact, pc.Auth, time.Duration(pc.TimeoutSec)*time.Second, oauth), nil
	}
	if oauthProvider == defaultClaudeOAuthProvider || strings.EqualFold(routeName, defaultClaudeOAuthProvider) {
		return NewClaudeProvider(routeName, pc.APIKey, pc.APIBase, defaultModel, pc.SupportsResponsesCompact, pc.Auth, time.Duration(pc.TimeoutSec)*time.Second, oauth), nil
	}
	if oauthProvider == defaultQwenOAuthProvider || strings.EqualFold(routeName, defaultQwenOAuthProvider) {
		return NewQwenProvider(routeName, pc.APIKey, pc.APIBase, defaultModel, pc.SupportsResponsesCompact, pc.Auth, time.Duration(pc.TimeoutSec)*time.Second, oauth), nil
	}
	if oauthProvider == defaultKimiOAuthProvider || strings.EqualFold(routeName, defaultKimiOAuthProvider) {
		return NewKimiProvider(routeName, pc.APIKey, pc.APIBase, defaultModel, pc.SupportsResponsesCompact, pc.Auth, time.Duration(pc.TimeoutSec)*time.Second, oauth), nil
	}
	if oauthProvider == defaultIFlowOAuthProvider || strings.EqualFold(routeName, defaultIFlowOAuthProvider) {
		return NewIFlowProvider(routeName, pc.APIKey, pc.APIBase, defaultModel, pc.SupportsResponsesCompact, pc.Auth, time.Duration(pc.TimeoutSec)*time.Second, oauth), nil
	}
	provider := NewHTTPProvider(routeName, pc.APIKey, pc.APIBase, defaultModel, pc.SupportsResponsesCompact, pc.Auth, time.Duration(pc.TimeoutSec)*time.Second, oauth)
	if api := strings.TrimSpace(pc.Responses.API); api != "" {
		provider.responsesAPI = api
	}
	return provider, nil
}

func ProviderSupportsResponsesCompact(cfg *config.Config, name string) bool {
	pc, err := getProviderConfigByName(cfg, name)
	if err != nil {
		return false
	}
	return pc.SupportsResponsesCompact
}

func getAllProviderConfigs(cfg *config.Config) map[string]config.ProviderConfig {
	return config.AllProviderConfigs(cfg)
}

func getProviderConfigByName(cfg *config.Config, name string) (config.ProviderConfig, error) {
	if pc, ok := config.ProviderConfigByName(cfg, name); ok {
		return pc, nil
	}
	return config.ProviderConfig{}, fmt.Errorf("provider %q not found", strings.TrimSpace(name))
}
