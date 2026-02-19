package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	Cron      CronConfig      `json:"cron"`
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
	Proxy             string                  `json:"proxy" env:"CLAWGO_AGENTS_DEFAULTS_PROXY"`
	ProxyFallbacks    []string                `json:"proxy_fallbacks" env:"CLAWGO_AGENTS_DEFAULTS_PROXY_FALLBACKS"`
	MaxTokens         int                     `json:"max_tokens" env:"CLAWGO_AGENTS_DEFAULTS_MAX_TOKENS"`
	Temperature       float64                 `json:"temperature" env:"CLAWGO_AGENTS_DEFAULTS_TEMPERATURE"`
	MaxToolIterations int                     `json:"max_tool_iterations" env:"CLAWGO_AGENTS_DEFAULTS_MAX_TOOL_ITERATIONS"`
	ContextCompaction ContextCompactionConfig `json:"context_compaction"`
	RuntimeControl    RuntimeControlConfig    `json:"runtime_control"`
}

type RuntimeControlConfig struct {
	IntentHighConfidence          float64  `json:"intent_high_confidence" env:"CLAWGO_INTENT_HIGH_CONFIDENCE"`
	IntentConfirmMinConfidence    float64  `json:"intent_confirm_min_confidence" env:"CLAWGO_INTENT_CONFIRM_MIN_CONFIDENCE"`
	IntentMaxInputChars           int      `json:"intent_max_input_chars" env:"CLAWGO_INTENT_MAX_INPUT_CHARS"`
	ConfirmTTLSeconds             int      `json:"confirm_ttl_seconds" env:"CLAWGO_CONFIRM_TTL_SECONDS"`
	ConfirmMaxClarificationTurns  int      `json:"confirm_max_clarification_turns" env:"CLAWGO_CONFIRM_MAX_CLARIFY_TURNS"`
	AutonomyTickIntervalSec       int      `json:"autonomy_tick_interval_sec" env:"CLAWGO_AUTONOMY_TICK_INTERVAL_SEC"`
	AutonomyMinRunIntervalSec     int      `json:"autonomy_min_run_interval_sec" env:"CLAWGO_AUTONOMY_MIN_RUN_INTERVAL_SEC"`
	AutonomyIdleThresholdSec      int      `json:"autonomy_idle_threshold_sec" env:"CLAWGO_AUTONOMY_IDLE_THRESHOLD_SEC"`
	AutonomyMaxRoundsWithoutUser  int      `json:"autonomy_max_rounds_without_user" env:"CLAWGO_AUTONOMY_MAX_ROUNDS_WITHOUT_USER"`
	AutonomyMaxPendingDurationSec int      `json:"autonomy_max_pending_duration_sec" env:"CLAWGO_AUTONOMY_MAX_PENDING_DURATION_SEC"`
	AutonomyMaxConsecutiveStalls  int      `json:"autonomy_max_consecutive_stalls" env:"CLAWGO_AUTONOMY_MAX_STALLS"`
	AutoLearnMaxRoundsWithoutUser int      `json:"autolearn_max_rounds_without_user" env:"CLAWGO_AUTOLEARN_MAX_ROUNDS_WITHOUT_USER"`
	RunStateTTLSeconds            int      `json:"run_state_ttl_seconds" env:"CLAWGO_RUN_STATE_TTL_SECONDS"`
	RunStateMax                   int      `json:"run_state_max" env:"CLAWGO_RUN_STATE_MAX"`
	RunControlLatestKeywords      []string `json:"run_control_latest_keywords"`
	RunControlWaitKeywords        []string `json:"run_control_wait_keywords"`
	RunControlStatusKeywords      []string `json:"run_control_status_keywords"`
	RunControlRunMentionKeywords  []string `json:"run_control_run_mention_keywords"`
	RunControlMinuteUnits         []string `json:"run_control_minute_units"`
	ToolParallelSafeNames         []string `json:"tool_parallel_safe_names"`
	ToolMaxParallelCalls          int      `json:"tool_max_parallel_calls"`
}

type ContextCompactionConfig struct {
	Enabled            bool   `json:"enabled" env:"CLAWGO_AGENTS_DEFAULTS_CONTEXT_COMPACTION_ENABLED"`
	Mode               string `json:"mode" env:"CLAWGO_AGENTS_DEFAULTS_CONTEXT_COMPACTION_MODE"`
	TriggerMessages    int    `json:"trigger_messages" env:"CLAWGO_AGENTS_DEFAULTS_CONTEXT_COMPACTION_TRIGGER_MESSAGES"`
	KeepRecentMessages int    `json:"keep_recent_messages" env:"CLAWGO_AGENTS_DEFAULTS_CONTEXT_COMPACTION_KEEP_RECENT_MESSAGES"`
	MaxSummaryChars    int    `json:"max_summary_chars" env:"CLAWGO_AGENTS_DEFAULTS_CONTEXT_COMPACTION_MAX_SUMMARY_CHARS"`
	MaxTranscriptChars int    `json:"max_transcript_chars" env:"CLAWGO_AGENTS_DEFAULTS_CONTEXT_COMPACTION_MAX_TRANSCRIPT_CHARS"`
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
	Proxy   ProviderConfig            `json:"proxy"`
	Proxies map[string]ProviderConfig `json:"proxies"`
}

type ProviderConfig struct {
	APIKey                   string   `json:"api_key" env:"CLAWGO_PROVIDERS_{{.Name}}_API_KEY"`
	APIBase                  string   `json:"api_base" env:"CLAWGO_PROVIDERS_{{.Name}}_API_BASE"`
	Protocol                 string   `json:"protocol" env:"CLAWGO_PROVIDERS_{{.Name}}_PROTOCOL"`
	Models                   []string `json:"models" env:"CLAWGO_PROVIDERS_{{.Name}}_MODELS"`
	SupportsResponsesCompact bool     `json:"supports_responses_compact" env:"CLAWGO_PROVIDERS_{{.Name}}_SUPPORTS_RESPONSES_COMPACT"`
	Auth                     string   `json:"auth" env:"CLAWGO_PROVIDERS_{{.Name}}_AUTH"`
	TimeoutSec               int      `json:"timeout_sec" env:"CLAWGO_PROVIDERS_PROXY_TIMEOUT_SEC"`
}

type GatewayConfig struct {
	Host string `json:"host" env:"CLAWGO_GATEWAY_HOST"`
	Port int    `json:"port" env:"CLAWGO_GATEWAY_PORT"`
}

type CronConfig struct {
	MinSleepSec                  int `json:"min_sleep_sec" env:"CLAWGO_CRON_MIN_SLEEP_SEC"`
	MaxSleepSec                  int `json:"max_sleep_sec" env:"CLAWGO_CRON_MAX_SLEEP_SEC"`
	RetryBackoffBaseSec          int `json:"retry_backoff_base_sec" env:"CLAWGO_CRON_RETRY_BACKOFF_BASE_SEC"`
	RetryBackoffMaxSec           int `json:"retry_backoff_max_sec" env:"CLAWGO_CRON_RETRY_BACKOFF_MAX_SEC"`
	MaxConsecutiveFailureRetries int `json:"max_consecutive_failure_retries" env:"CLAWGO_CRON_MAX_CONSECUTIVE_FAILURE_RETRIES"`
	MaxWorkers                   int `json:"max_workers" env:"CLAWGO_CRON_MAX_WORKERS"`
}

type WebSearchConfig struct {
	APIKey     string `json:"api_key" env:"CLAWGO_TOOLS_WEB_SEARCH_API_KEY"`
	MaxResults int    `json:"max_results" env:"CLAWGO_TOOLS_WEB_SEARCH_MAX_RESULTS"`
}

type WebToolsConfig struct {
	Search WebSearchConfig `json:"search"`
}

type ShellConfig struct {
	Enabled    bool          `json:"enabled" env:"CLAWGO_TOOLS_SHELL_ENABLED"`
	WorkingDir string        `json:"working_dir" env:"CLAWGO_TOOLS_SHELL_WORKING_DIR"`
	Timeout    time.Duration `json:"timeout" env:"CLAWGO_TOOLS_SHELL_TIMEOUT"`
	Sandbox    SandboxConfig `json:"sandbox"`
}

type SandboxConfig struct {
	Enabled bool   `json:"enabled" env:"CLAWGO_TOOLS_SHELL_SANDBOX_ENABLED"`
	Image   string `json:"image" env:"CLAWGO_TOOLS_SHELL_SANDBOX_IMAGE"`
}

type FilesystemConfig struct{}

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
				Proxy:             "proxy",
				ProxyFallbacks:    []string{},
				MaxTokens:         8192,
				Temperature:       0.7,
				MaxToolIterations: 20,
				ContextCompaction: ContextCompactionConfig{
					Enabled:            true,
					Mode:               "summary",
					TriggerMessages:    60,
					KeepRecentMessages: 20,
					MaxSummaryChars:    6000,
					MaxTranscriptChars: 20000,
				},
				RuntimeControl: RuntimeControlConfig{
					IntentHighConfidence:          0.75,
					IntentConfirmMinConfidence:    0.45,
					IntentMaxInputChars:           1200,
					ConfirmTTLSeconds:             300,
					ConfirmMaxClarificationTurns:  2,
					AutonomyTickIntervalSec:       20,
					AutonomyMinRunIntervalSec:     20,
					AutonomyIdleThresholdSec:      20,
					AutonomyMaxRoundsWithoutUser:  120,
					AutonomyMaxPendingDurationSec: 180,
					AutonomyMaxConsecutiveStalls:  3,
					AutoLearnMaxRoundsWithoutUser: 200,
					RunStateTTLSeconds:            1800,
					RunStateMax:                   500,
					RunControlLatestKeywords:      []string{"latest", "last run", "recent run", "最新", "最近", "上一次", "上个"},
					RunControlWaitKeywords:        []string{"wait", "等待", "等到", "阻塞"},
					RunControlStatusKeywords:      []string{"status", "状态", "进度", "running", "运行"},
					RunControlRunMentionKeywords:  []string{"run", "任务"},
					RunControlMinuteUnits:         []string{"分钟", "min", "mins", "minute", "minutes", "m"},
					ToolParallelSafeNames:         []string{"read_file", "list_files", "find_files", "grep_files", "memory_search", "web_search", "repo_map", "system_info"},
					ToolMaxParallelCalls:          2,
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
				APIBase:    "http://localhost:8080/v1",
				Protocol:   "chat_completions",
				Models:     []string{"glm-4.7"},
				TimeoutSec: 90,
			},
			Proxies: map[string]ProviderConfig{},
		},
		Gateway: GatewayConfig{
			Host: "0.0.0.0",
			Port: 18790,
		},
		Cron: CronConfig{
			MinSleepSec:                  1,
			MaxSleepSec:                  30,
			RetryBackoffBaseSec:          30,
			RetryBackoffMaxSec:           1800,
			MaxConsecutiveFailureRetries: 5,
			MaxWorkers:                   4,
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
				Sandbox: SandboxConfig{
					Enabled: false,
					Image:   "golang:alpine",
				},
			},
			Filesystem: FilesystemConfig{},
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

	if err := unmarshalConfigStrict(data, cfg); err != nil {
		return nil, err
	}

	if err := env.Parse(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func unmarshalConfigStrict(data []byte, cfg *Config) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("invalid config: trailing JSON content")
		}
		return err
	}
	return nil
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
