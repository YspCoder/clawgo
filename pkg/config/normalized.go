package config

import "strings"

type NormalizedConfig struct {
	Core    NormalizedCoreConfig    `json:"core"`
	Runtime NormalizedRuntimeConfig `json:"runtime"`
}

type NormalizedCoreConfig struct {
	DefaultProvider string                              `json:"default_provider,omitempty"`
	DefaultModel    string                              `json:"default_model,omitempty"`
	MainAgentID     string                              `json:"main_agent_id,omitempty"`
	Subagents       map[string]NormalizedSubagentConfig `json:"subagents,omitempty"`
	Tools           NormalizedCoreToolsConfig           `json:"tools,omitempty"`
	Gateway         NormalizedCoreGatewayConfig         `json:"gateway,omitempty"`
}

type NormalizedSubagentConfig struct {
	Enabled       bool     `json:"enabled"`
	Role          string   `json:"role,omitempty"`
	Prompt        string   `json:"prompt,omitempty"`
	Provider      string   `json:"provider,omitempty"`
	ToolAllowlist []string `json:"tool_allowlist,omitempty"`
	RuntimeClass  string   `json:"runtime_class,omitempty"`
}

type NormalizedCoreToolsConfig struct {
	ShellEnabled bool `json:"shell_enabled"`
	MCPEnabled   bool `json:"mcp_enabled"`
}

type NormalizedCoreGatewayConfig struct {
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`
}

type NormalizedRuntimeConfig struct {
	Router    NormalizedRuntimeRouterConfig              `json:"router,omitempty"`
	Providers map[string]NormalizedRuntimeProviderConfig `json:"providers,omitempty"`
}

type NormalizedRuntimeRouterConfig struct {
	Enabled              bool             `json:"enabled"`
	Strategy             string           `json:"strategy,omitempty"`
	AllowDirectAgentChat bool             `json:"allow_direct_agent_chat,omitempty"`
	MaxHops              int              `json:"max_hops,omitempty"`
	DefaultTimeoutSec    int              `json:"default_timeout_sec,omitempty"`
	StickyThreadOwner    bool             `json:"sticky_thread_owner,omitempty"`
	Rules                []AgentRouteRule `json:"rules,omitempty"`
}

type NormalizedRuntimeProviderConfig struct {
	Auth               string                  `json:"auth,omitempty"`
	APIBase            string                  `json:"api_base,omitempty"`
	TimeoutSec         int                     `json:"timeout_sec,omitempty"`
	OAuth              ProviderOAuthConfig     `json:"oauth,omitempty"`
	RuntimePersist     bool                    `json:"runtime_persist,omitempty"`
	RuntimeHistoryFile string                  `json:"runtime_history_file,omitempty"`
	RuntimeHistoryMax  int                     `json:"runtime_history_max,omitempty"`
	Responses          ProviderResponsesConfig `json:"responses,omitempty"`
}

func (c *Config) Normalize() {
	if c == nil {
		return
	}
	if strings.TrimSpace(c.Agents.Router.MainAgentID) == "" {
		c.Agents.Router.MainAgentID = "main"
	}
	if c.Agents.Subagents == nil {
		c.Agents.Subagents = map[string]SubagentConfig{}
	}
	if c.Agents.Router.Enabled {
		mainID := strings.TrimSpace(c.Agents.Router.MainAgentID)
		if mainID == "" {
			mainID = "main"
			c.Agents.Router.MainAgentID = mainID
		}
		main := c.Agents.Subagents[mainID]
		if !main.Enabled {
			main.Enabled = true
		}
		if strings.TrimSpace(main.Role) == "" {
			main.Role = "orchestrator"
		}
		if strings.TrimSpace(main.Type) == "" {
			main.Type = "router"
		}
		if strings.TrimSpace(main.SystemPromptFile) == "" {
			main.SystemPromptFile = "agents/main/AGENT.md"
		}
		c.Agents.Subagents[mainID] = main
	}
	if provider, model := ParseProviderModelRef(c.Agents.Defaults.Model.Primary); provider != "" && model != "" {
		c.Agents.Defaults.Model.Primary = provider + "/" + model
	}
}

func (c *Config) NormalizedView() NormalizedConfig {
	view := NormalizedConfig{
		Core: NormalizedCoreConfig{
			MainAgentID: strings.TrimSpace(c.Agents.Router.MainAgentID),
			Subagents:   map[string]NormalizedSubagentConfig{},
			Tools: NormalizedCoreToolsConfig{
				ShellEnabled: c.Tools.Shell.Enabled,
				MCPEnabled:   c.Tools.MCP.Enabled,
			},
			Gateway: NormalizedCoreGatewayConfig{
				Host: c.Gateway.Host,
				Port: c.Gateway.Port,
			},
		},
		Runtime: NormalizedRuntimeConfig{
			Router: NormalizedRuntimeRouterConfig{
				Enabled:              c.Agents.Router.Enabled,
				Strategy:             c.Agents.Router.Strategy,
				AllowDirectAgentChat: c.Agents.Router.AllowDirectAgentChat,
				MaxHops:              c.Agents.Router.MaxHops,
				DefaultTimeoutSec:    c.Agents.Router.DefaultTimeoutSec,
				StickyThreadOwner:    c.Agents.Router.StickyThreadOwner,
				Rules:                append([]AgentRouteRule(nil), c.Agents.Router.Rules...),
			},
			Providers: map[string]NormalizedRuntimeProviderConfig{},
		},
	}
	view.Core.DefaultProvider, view.Core.DefaultModel = ParseProviderModelRef(c.Agents.Defaults.Model.Primary)
	if view.Core.DefaultProvider == "" {
		view.Core.DefaultProvider = PrimaryProviderName(c)
		view.Core.DefaultModel = strings.TrimSpace(c.Agents.Defaults.Model.Primary)
	}
	for id, subcfg := range c.Agents.Subagents {
		view.Core.Subagents[id] = NormalizedSubagentConfig{
			Enabled:       subcfg.Enabled,
			Role:          subcfg.Role,
			Prompt:        subcfg.SystemPromptFile,
			Provider:      subcfg.Runtime.Provider,
			ToolAllowlist: append([]string(nil), subcfg.Tools.Allowlist...),
			RuntimeClass:  firstNonEmptyRuntimeClass(subcfg),
		}
	}
	for name, pc := range c.Models.Providers {
		view.Runtime.Providers[name] = NormalizedRuntimeProviderConfig{
			Auth:               pc.Auth,
			APIBase:            pc.APIBase,
			TimeoutSec:         pc.TimeoutSec,
			OAuth:              pc.OAuth,
			RuntimePersist:     pc.RuntimePersist,
			RuntimeHistoryFile: pc.RuntimeHistoryFile,
			RuntimeHistoryMax:  pc.RuntimeHistoryMax,
			Responses:          pc.Responses,
		}
	}
	return view
}

func firstNonEmptyRuntimeClass(subcfg SubagentConfig) string {
	switch {
	case strings.TrimSpace(subcfg.Runtime.Provider) != "":
		return "provider_bound"
	case strings.TrimSpace(subcfg.Transport) != "":
		return strings.TrimSpace(subcfg.Transport)
	default:
		return "default"
	}
}

func (c *Config) ApplyNormalizedView(view NormalizedConfig) {
	if c == nil {
		return
	}
	defaultProvider := strings.TrimSpace(view.Core.DefaultProvider)
	defaultModel := strings.TrimSpace(view.Core.DefaultModel)
	if defaultProvider != "" && defaultModel != "" {
		c.Agents.Defaults.Model.Primary = normalizeProviderNameAlias(defaultProvider) + "/" + defaultModel
	}
	if strings.TrimSpace(view.Core.MainAgentID) != "" {
		c.Agents.Router.MainAgentID = strings.TrimSpace(view.Core.MainAgentID)
	}
	c.Tools.Shell.Enabled = view.Core.Tools.ShellEnabled
	c.Tools.MCP.Enabled = view.Core.Tools.MCPEnabled
	if strings.TrimSpace(view.Core.Gateway.Host) != "" {
		c.Gateway.Host = strings.TrimSpace(view.Core.Gateway.Host)
	}
	if view.Core.Gateway.Port > 0 {
		c.Gateway.Port = view.Core.Gateway.Port
	}

	nextSubagents := map[string]SubagentConfig{}
	for id, current := range c.Agents.Subagents {
		nextSubagents[id] = current
	}
	for id, item := range view.Core.Subagents {
		current := c.Agents.Subagents[id]
		current.Enabled = item.Enabled
		current.Role = strings.TrimSpace(item.Role)
		current.SystemPromptFile = strings.TrimSpace(item.Prompt)
		current.Tools.Allowlist = append([]string(nil), item.ToolAllowlist...)
		current.Runtime.Provider = strings.TrimSpace(item.Provider)
		switch strings.TrimSpace(item.RuntimeClass) {
		case "", "default":
		case "provider_bound":
			if strings.TrimSpace(current.Transport) == "" {
				current.Transport = "local"
			}
		default:
			current.Transport = strings.TrimSpace(item.RuntimeClass)
		}
		nextSubagents[id] = current
	}
	c.Agents.Subagents = nextSubagents

	c.Agents.Router.Enabled = view.Runtime.Router.Enabled
	if strings.TrimSpace(view.Runtime.Router.Strategy) != "" {
		c.Agents.Router.Strategy = strings.TrimSpace(view.Runtime.Router.Strategy)
	}
	c.Agents.Router.AllowDirectAgentChat = view.Runtime.Router.AllowDirectAgentChat
	if view.Runtime.Router.MaxHops > 0 {
		c.Agents.Router.MaxHops = view.Runtime.Router.MaxHops
	}
	if view.Runtime.Router.DefaultTimeoutSec > 0 {
		c.Agents.Router.DefaultTimeoutSec = view.Runtime.Router.DefaultTimeoutSec
	}
	c.Agents.Router.StickyThreadOwner = view.Runtime.Router.StickyThreadOwner
	c.Agents.Router.Rules = append([]AgentRouteRule(nil), view.Runtime.Router.Rules...)

	nextProviders := map[string]ProviderConfig{}
	for name, current := range c.Models.Providers {
		nextProviders[name] = current
	}
	for name, item := range view.Runtime.Providers {
		current := c.Models.Providers[name]
		current.Auth = strings.TrimSpace(item.Auth)
		current.APIBase = strings.TrimSpace(item.APIBase)
		if item.TimeoutSec > 0 {
			current.TimeoutSec = item.TimeoutSec
		}
		current.OAuth = item.OAuth
		current.RuntimePersist = item.RuntimePersist
		current.RuntimeHistoryFile = item.RuntimeHistoryFile
		current.RuntimeHistoryMax = item.RuntimeHistoryMax
		current.Responses = item.Responses
		nextProviders[name] = current
	}
	c.Models.Providers = nextProviders
	c.Normalize()
}
