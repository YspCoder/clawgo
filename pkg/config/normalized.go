package config

import "strings"

type NormalizedConfig struct {
	Core    NormalizedCoreConfig    `json:"core"`
	Runtime NormalizedRuntimeConfig `json:"runtime"`
}

type NormalizedCoreConfig struct {
	DefaultProvider string                           `json:"default_provider,omitempty"`
	DefaultModel    string                           `json:"default_model,omitempty"`
	MainAgentID     string                           `json:"main_agent_id,omitempty"`
	Agents          map[string]NormalizedAgentConfig `json:"agents,omitempty"`
	Tools           NormalizedCoreToolsConfig        `json:"tools,omitempty"`
	Gateway         NormalizedCoreGatewayConfig      `json:"gateway,omitempty"`
}

type NormalizedAgentConfig struct {
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
	Providers map[string]NormalizedRuntimeProviderConfig `json:"providers,omitempty"`
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
	if c.Agents.Agents == nil {
		c.Agents.Agents = map[string]AgentConfig{}
	}
	main := c.Agents.Agents["main"]
	if !main.Enabled {
		main.Enabled = true
	}
	if strings.TrimSpace(main.Role) == "" {
		main.Role = "orchestrator"
	}
	if strings.TrimSpace(main.Type) == "" {
		main.Type = "agent"
	}
	if strings.TrimSpace(main.PromptFile) == "" {
		main.PromptFile = "agents/main/AGENT.md"
	}
	c.Agents.Agents["main"] = main
	if provider, model := ParseProviderModelRef(c.Agents.Defaults.Model.Primary); provider != "" && model != "" {
		c.Agents.Defaults.Model.Primary = provider + "/" + model
	}
}

func (c *Config) NormalizedView() NormalizedConfig {
	view := NormalizedConfig{
		Core: NormalizedCoreConfig{
			MainAgentID: "main",
			Agents:      map[string]NormalizedAgentConfig{},
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
			Providers: map[string]NormalizedRuntimeProviderConfig{},
		},
	}
	view.Core.DefaultProvider, view.Core.DefaultModel = ParseProviderModelRef(c.Agents.Defaults.Model.Primary)
	if view.Core.DefaultProvider == "" {
		view.Core.DefaultProvider = PrimaryProviderName(c)
		view.Core.DefaultModel = strings.TrimSpace(c.Agents.Defaults.Model.Primary)
	}
	for id, subcfg := range c.Agents.Agents {
		view.Core.Agents[id] = NormalizedAgentConfig{
			Enabled:       subcfg.Enabled,
			Role:          subcfg.Role,
			Prompt:        subcfg.PromptFile,
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

func firstNonEmptyRuntimeClass(subcfg AgentConfig) string {
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
	c.Tools.Shell.Enabled = view.Core.Tools.ShellEnabled
	c.Tools.MCP.Enabled = view.Core.Tools.MCPEnabled
	if strings.TrimSpace(view.Core.Gateway.Host) != "" {
		c.Gateway.Host = strings.TrimSpace(view.Core.Gateway.Host)
	}
	if view.Core.Gateway.Port > 0 {
		c.Gateway.Port = view.Core.Gateway.Port
	}

	nextAgents := map[string]AgentConfig{}
	for id, current := range c.Agents.Agents {
		nextAgents[id] = current
	}
	for id, item := range view.Core.Agents {
		current := c.Agents.Agents[id]
		current.Enabled = item.Enabled
		current.Role = strings.TrimSpace(item.Role)
		current.PromptFile = strings.TrimSpace(item.Prompt)
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
		nextAgents[id] = current
	}
	c.Agents.Agents = nextAgents

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
