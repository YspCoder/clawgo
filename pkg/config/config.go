package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Agents    AgentsConfig    `json:"agents"`
	Channels  ChannelsConfig  `json:"channels"`
	Providers ProvidersConfig `json:"providers"`
	Gateway   GatewayConfig   `json:"gateway"`
	Tools     ToolsConfig     `json:"tools"`
	Logging   LoggingConfig   `json:"logging"`
	Sentinel  SentinelConfig  `json:"sentinel"`
	Memory    MemoryConfig    `json:"memory"`
	mu        sync.RWMutex
}

type AgentsConfig struct {
	Defaults AgentDefaults `json:"defaults"`
}

type AgentDefaults struct {
	Workspace         string                  `json:"workspace" env:"CLAWGO_AGENTS_DEFAULTS_WORKSPACE"`
	Model             string                  `json:"model" env:"CLAWGO_AGENTS_DEFAULTS_MODEL"`
	ModelFallbacks    []string                `json:"model_fallbacks" env:"CLAWGO_AGENTS_DEFAULTS_MODEL_FALLBACKS"`
	MaxTokens         int                     `json:"max_tokens" env:"CLAWGO_AGENTS_DEFAULTS_MAX_TOKENS"`
	Temperature       float64                 `json:"temperature" env:"CLAWGO_AGENTS_DEFAULTS_TEMPERATURE"`
	MaxToolIterations int                     `json:"max_tool_iterations" env:"CLAWGO_AGENTS_DEFAULTS_MAX_TOOL_ITERATIONS"`
	ContextCompaction ContextCompactionConfig `json:"context_compaction"`
}

type ContextCompactionConfig struct {
	Enabled            bool `json:"enabled" env:"CLAWGO_AGENTS_DEFAULTS_CONTEXT_COMPACTION_ENABLED"`
	TriggerMessages    int  `json:"trigger_messages" env:"CLAWGO_AGENTS_DEFAULTS_CONTEXT_COMPACTION_TRIGGER_MESSAGES"`
	KeepRecentMessages int  `json:"keep_recent_messages" env:"CLAWGO_AGENTS_DEFAULTS_CONTEXT_COMPACTION_KEEP_RECENT_MESSAGES"`
	MaxSummaryChars    int  `json:"max_summary_chars" env:"CLAWGO_AGENTS_DEFAULTS_CONTEXT_COMPACTION_MAX_SUMMARY_CHARS"`
	MaxTranscriptChars int  `json:"max_transcript_chars" env:"CLAWGO_AGENTS_DEFAULTS_CONTEXT_COMPACTION_MAX_TRANSCRIPT_CHARS"`
}

type ChannelsConfig struct {
	WhatsApp WhatsAppConfig `json:"whatsapp"`
	Telegram TelegramConfig `json:"telegram"`
	Feishu   FeishuConfig   `json:"feishu"`
	Discord  DiscordConfig  `json:"discord"`
	MaixCam  MaixCamConfig  `json:"maixcam"`
	QQ       QQConfig       `json:"qq"`
	DingTalk DingTalkConfig `json:"dingtalk"`
}

type WhatsAppConfig struct {
	Enabled   bool     `json:"enabled" env:"CLAWGO_CHANNELS_WHATSAPP_ENABLED"`
	BridgeURL string   `json:"bridge_url" env:"CLAWGO_CHANNELS_WHATSAPP_BRIDGE_URL"`
	AllowFrom []string `json:"allow_from" env:"CLAWGO_CHANNELS_WHATSAPP_ALLOW_FROM"`
}

type TelegramConfig struct {
	Enabled   bool     `json:"enabled" env:"CLAWGO_CHANNELS_TELEGRAM_ENABLED"`
	Token     string   `json:"token" env:"CLAWGO_CHANNELS_TELEGRAM_TOKEN"`
	AllowFrom []string `json:"allow_from" env:"CLAWGO_CHANNELS_TELEGRAM_ALLOW_FROM"`
}

type FeishuConfig struct {
	Enabled           bool     `json:"enabled" env:"CLAWGO_CHANNELS_FEISHU_ENABLED"`
	AppID             string   `json:"app_id" env:"CLAWGO_CHANNELS_FEISHU_APP_ID"`
	AppSecret         string   `json:"app_secret" env:"CLAWGO_CHANNELS_FEISHU_APP_SECRET"`
	EncryptKey        string   `json:"encrypt_key" env:"CLAWGO_CHANNELS_FEISHU_ENCRYPT_KEY"`
	VerificationToken string   `json:"verification_token" env:"CLAWGO_CHANNELS_FEISHU_VERIFICATION_TOKEN"`
	AllowFrom         []string `json:"allow_from" env:"CLAWGO_CHANNELS_FEISHU_ALLOW_FROM"`
}

type DiscordConfig struct {
	Enabled   bool     `json:"enabled" env:"CLAWGO_CHANNELS_DISCORD_ENABLED"`
	Token     string   `json:"token" env:"CLAWGO_CHANNELS_DISCORD_TOKEN"`
	AllowFrom []string `json:"allow_from" env:"CLAWGO_CHANNELS_DISCORD_ALLOW_FROM"`
}

type MaixCamConfig struct {
	Enabled   bool     `json:"enabled" env:"CLAWGO_CHANNELS_MAIXCAM_ENABLED"`
	Host      string   `json:"host" env:"CLAWGO_CHANNELS_MAIXCAM_HOST"`
	Port      int      `json:"port" env:"CLAWGO_CHANNELS_MAIXCAM_PORT"`
	AllowFrom []string `json:"allow_from" env:"CLAWGO_CHANNELS_MAIXCAM_ALLOW_FROM"`
}

type QQConfig struct {
	Enabled   bool     `json:"enabled" env:"CLAWGO_CHANNELS_QQ_ENABLED"`
	AppID     string   `json:"app_id" env:"CLAWGO_CHANNELS_QQ_APP_ID"`
	AppSecret string   `json:"app_secret" env:"CLAWGO_CHANNELS_QQ_APP_SECRET"`
	AllowFrom []string `json:"allow_from" env:"CLAWGO_CHANNELS_QQ_ALLOW_FROM"`
}

type DingTalkConfig struct {
	Enabled      bool     `json:"enabled" env:"CLAWGO_CHANNELS_DINGTALK_ENABLED"`
	ClientID     string   `json:"client_id" env:"CLAWGO_CHANNELS_DINGTALK_CLIENT_ID"`
	ClientSecret string   `json:"client_secret" env:"CLAWGO_CHANNELS_DINGTALK_CLIENT_SECRET"`
	AllowFrom    []string `json:"allow_from" env:"CLAWGO_CHANNELS_DINGTALK_ALLOW_FROM"`
}

type ProvidersConfig struct {
	Proxy ProviderConfig `json:"proxy"`
}

type ProviderConfig struct {
	APIKey  string `json:"api_key" env:"CLAWGO_PROVIDERS_{{.Name}}_API_KEY"`
	APIBase string `json:"api_base" env:"CLAWGO_PROVIDERS_{{.Name}}_API_BASE"`
	Auth    string `json:"auth" env:"CLAWGO_PROVIDERS_{{.Name}}_AUTH"`
}

type GatewayConfig struct {
	Host string `json:"host" env:"CLAWGO_GATEWAY_HOST"`
	Port int    `json:"port" env:"CLAWGO_GATEWAY_PORT"`
}

type WebSearchConfig struct {
	APIKey     string `json:"api_key" env:"CLAWGO_TOOLS_WEB_SEARCH_API_KEY"`
	MaxResults int    `json:"max_results" env:"CLAWGO_TOOLS_WEB_SEARCH_MAX_RESULTS"`
}

type WebToolsConfig struct {
	Search WebSearchConfig `json:"search"`
}

type ShellConfig struct {
	Enabled      bool          `json:"enabled" env:"CLAWGO_TOOLS_SHELL_ENABLED"`
	WorkingDir   string        `json:"working_dir" env:"CLAWGO_TOOLS_SHELL_WORKING_DIR"`
	Timeout      time.Duration `json:"timeout" env:"CLAWGO_TOOLS_SHELL_TIMEOUT"`
	DeniedCmds   []string      `json:"denied_cmds" env:"CLAWGO_TOOLS_SHELL_DENIED_CMDS"`
	AllowedCmds  []string      `json:"allowed_cmds" env:"CLAWGO_TOOLS_SHELL_ALLOWED_CMDS"`
	Sandbox      SandboxConfig `json:"sandbox"`
	Risk         RiskConfig    `json:"risk"`
	RestrictPath bool          `json:"restrict_path" env:"CLAWGO_TOOLS_SHELL_RESTRICT_PATH"`
}

type RiskConfig struct {
	Enabled          bool `json:"enabled" env:"CLAWGO_TOOLS_SHELL_RISK_ENABLED"`
	AllowDestructive bool `json:"allow_destructive" env:"CLAWGO_TOOLS_SHELL_RISK_ALLOW_DESTRUCTIVE"`
	RequireDryRun    bool `json:"require_dry_run" env:"CLAWGO_TOOLS_SHELL_RISK_REQUIRE_DRY_RUN"`
	RequireForceFlag bool `json:"require_force_flag" env:"CLAWGO_TOOLS_SHELL_RISK_REQUIRE_FORCE_FLAG"`
}

type SandboxConfig struct {
	Enabled bool   `json:"enabled" env:"CLAWGO_TOOLS_SHELL_SANDBOX_ENABLED"`
	Image   string `json:"image" env:"CLAWGO_TOOLS_SHELL_SANDBOX_IMAGE"`
}

type FilesystemConfig struct {
	AllowedPaths []string `json:"allowed_paths" env:"CLAWGO_TOOLS_FILESYSTEM_ALLOWED_PATHS"`
	DeniedPaths  []string `json:"denied_paths" env:"CLAWGO_TOOLS_FILESYSTEM_DENIED_PATHS"`
}

type ToolsConfig struct {
	Web        WebToolsConfig   `json:"web"`
	Shell      ShellConfig      `json:"shell"`
	Filesystem FilesystemConfig `json:"filesystem"`
}

type LoggingConfig struct {
	Enabled       bool   `json:"enabled" env:"CLAWGO_LOGGING_ENABLED"`
	Dir           string `json:"dir" env:"CLAWGO_LOGGING_DIR"`
	Filename      string `json:"filename" env:"CLAWGO_LOGGING_FILENAME"`
	MaxSizeMB     int    `json:"max_size_mb" env:"CLAWGO_LOGGING_MAX_SIZE_MB"`
	RetentionDays int    `json:"retention_days" env:"CLAWGO_LOGGING_RETENTION_DAYS"`
}

type SentinelConfig struct {
	Enabled       bool   `json:"enabled" env:"CLAWGO_SENTINEL_ENABLED"`
	IntervalSec   int    `json:"interval_sec" env:"CLAWGO_SENTINEL_INTERVAL_SEC"`
	AutoHeal      bool   `json:"auto_heal" env:"CLAWGO_SENTINEL_AUTO_HEAL"`
	NotifyChannel string `json:"notify_channel" env:"CLAWGO_SENTINEL_NOTIFY_CHANNEL"`
	NotifyChatID  string `json:"notify_chat_id" env:"CLAWGO_SENTINEL_NOTIFY_CHAT_ID"`
}

type MemoryConfig struct {
	Layered    bool `json:"layered" env:"CLAWGO_MEMORY_LAYERED"`
	RecentDays int  `json:"recent_days" env:"CLAWGO_MEMORY_RECENT_DAYS"`
	Layers     struct {
		Profile    bool `json:"profile" env:"CLAWGO_MEMORY_LAYERS_PROFILE"`
		Project    bool `json:"project" env:"CLAWGO_MEMORY_LAYERS_PROJECT"`
		Procedures bool `json:"procedures" env:"CLAWGO_MEMORY_LAYERS_PROCEDURES"`
	} `json:"layers"`
}

var (
	isDebug bool
	muDebug sync.RWMutex
)

func SetDebugMode(debug bool) {
	muDebug.Lock()
	defer muDebug.Unlock()
	isDebug = debug
}

func IsDebugMode() bool {
	muDebug.RLock()
	defer muDebug.RUnlock()
	return isDebug
}

func GetConfigDir() string {
	if IsDebugMode() {
		return ".clawgo"
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".clawgo")
}

func DefaultConfig() *Config {
	configDir := GetConfigDir()
	return &Config{
		Agents: AgentsConfig{
			Defaults: AgentDefaults{
				Workspace:         filepath.Join(configDir, "workspace"),
				Model:             "glm-4.7",
				ModelFallbacks:    []string{},
				MaxTokens:         8192,
				Temperature:       0.7,
				MaxToolIterations: 20,
				ContextCompaction: ContextCompactionConfig{
					Enabled:            true,
					TriggerMessages:    60,
					KeepRecentMessages: 20,
					MaxSummaryChars:    6000,
					MaxTranscriptChars: 20000,
				},
			},
		},
		Channels: ChannelsConfig{
			WhatsApp: WhatsAppConfig{
				Enabled:   false,
				BridgeURL: "ws://localhost:3001",
				AllowFrom: []string{},
			},
			Telegram: TelegramConfig{
				Enabled:   false,
				Token:     "",
				AllowFrom: []string{},
			},
			Feishu: FeishuConfig{
				Enabled:           false,
				AppID:             "",
				AppSecret:         "",
				EncryptKey:        "",
				VerificationToken: "",
				AllowFrom:         []string{},
			},
			Discord: DiscordConfig{
				Enabled:   false,
				Token:     "",
				AllowFrom: []string{},
			},
			MaixCam: MaixCamConfig{
				Enabled:   false,
				Host:      "0.0.0.0",
				Port:      18790,
				AllowFrom: []string{},
			},
			QQ: QQConfig{
				Enabled:   false,
				AppID:     "",
				AppSecret: "",
				AllowFrom: []string{},
			},
			DingTalk: DingTalkConfig{
				Enabled:      false,
				ClientID:     "",
				ClientSecret: "",
				AllowFrom:    []string{},
			},
		},
		Providers: ProvidersConfig{
			Proxy: ProviderConfig{
				APIBase: "http://localhost:8080/v1",
			},
		},
		Gateway: GatewayConfig{
			Host: "0.0.0.0",
			Port: 18790,
		},
		Tools: ToolsConfig{
			Web: WebToolsConfig{
				Search: WebSearchConfig{
					APIKey:     "",
					MaxResults: 5,
				},
			},
			Shell: ShellConfig{
				Enabled: true,
				Timeout: 60 * time.Second,
				DeniedCmds: []string{
					"rm -rf /", "dd if=", "mkfs", "shutdown", "reboot",
				},
				Sandbox: SandboxConfig{
					Enabled: false,
					Image:   "golang:alpine",
				},
				Risk: RiskConfig{
					Enabled:          true,
					AllowDestructive: false,
					RequireDryRun:    true,
					RequireForceFlag: true,
				},
			},
			Filesystem: FilesystemConfig{
				AllowedPaths: []string{},
				DeniedPaths:  []string{"/etc/shadow", "/etc/passwd"},
			},
		},
		Logging: LoggingConfig{
			Enabled:       true,
			Dir:           filepath.Join(configDir, "logs"),
			Filename:      "clawgo.log",
			MaxSizeMB:     20,
			RetentionDays: 3,
		},
		Sentinel: SentinelConfig{
			Enabled:       true,
			IntervalSec:   60,
			AutoHeal:      true,
			NotifyChannel: "",
			NotifyChatID:  "",
		},
		Memory: MemoryConfig{
			Layered:    true,
			RecentDays: 3,
			Layers: struct {
				Profile    bool `json:"profile" env:"CLAWGO_MEMORY_LAYERS_PROFILE"`
				Project    bool `json:"project" env:"CLAWGO_MEMORY_LAYERS_PROJECT"`
				Procedures bool `json:"procedures" env:"CLAWGO_MEMORY_LAYERS_PROCEDURES"`
			}{
				Profile:    true,
				Project:    true,
				Procedures: true,
			},
		},
	}
}

func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if err := env.Parse(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func SaveConfig(path string, cfg *Config) error {
	cfg.mu.RLock()
	defer cfg.mu.RUnlock()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func (c *Config) WorkspacePath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return expandHome(c.Agents.Defaults.Workspace)
}

func (c *Config) GetAPIKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Providers.Proxy.APIKey
}

func (c *Config) GetAPIBase() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Providers.Proxy.APIBase
}

func (c *Config) LogFilePath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	dir := expandHome(c.Logging.Dir)
	filename := c.Logging.Filename
	if filename == "" {
		filename = "clawgo.log"
	}
	return filepath.Join(dir, filename)
}

func expandHome(path string) string {
	if path == "" {
		return path
	}
	if path[0] == '~' {
		home, _ := os.UserHomeDir()
		if len(path) > 1 && path[1] == '/' {
			return home + path[1:]
		}
		return home
	}
	return path
}
