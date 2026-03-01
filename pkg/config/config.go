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
	Heartbeat         HeartbeatConfig         `json:"heartbeat"`
	Autonomy          AutonomyConfig          `json:"autonomy"`
	Texts             AgentTextConfig         `json:"texts"`
	ContextCompaction ContextCompactionConfig `json:"context_compaction"`
	RuntimeControl    RuntimeControlConfig    `json:"runtime_control"`
}

type AutonomyConfig struct {
	Enabled                     bool   `json:"enabled" env:"CLAWGO_AGENTS_DEFAULTS_AUTONOMY_ENABLED"`
	TickIntervalSec             int    `json:"tick_interval_sec" env:"CLAWGO_AGENTS_DEFAULTS_AUTONOMY_TICK_INTERVAL_SEC"`
	MinRunIntervalSec           int    `json:"min_run_interval_sec" env:"CLAWGO_AGENTS_DEFAULTS_AUTONOMY_MIN_RUN_INTERVAL_SEC"`
	MaxPendingDurationSec       int    `json:"max_pending_duration_sec" env:"CLAWGO_AGENTS_DEFAULTS_AUTONOMY_MAX_PENDING_DURATION_SEC"`
	MaxConsecutiveStalls        int    `json:"max_consecutive_stalls" env:"CLAWGO_AGENTS_DEFAULTS_AUTONOMY_MAX_CONSECUTIVE_STALLS"`
	MaxDispatchPerTick          int    `json:"max_dispatch_per_tick" env:"CLAWGO_AGENTS_DEFAULTS_AUTONOMY_MAX_DISPATCH_PER_TICK"`
	NotifyCooldownSec           int    `json:"notify_cooldown_sec" env:"CLAWGO_AGENTS_DEFAULTS_AUTONOMY_NOTIFY_COOLDOWN_SEC"`
	NotifySameReasonCooldownSec int    `json:"notify_same_reason_cooldown_sec" env:"CLAWGO_AGENTS_DEFAULTS_AUTONOMY_NOTIFY_SAME_REASON_COOLDOWN_SEC"`
	QuietHours                  string `json:"quiet_hours" env:"CLAWGO_AGENTS_DEFAULTS_AUTONOMY_QUIET_HOURS"`
	UserIdleResumeSec           int      `json:"user_idle_resume_sec" env:"CLAWGO_AGENTS_DEFAULTS_AUTONOMY_USER_IDLE_RESUME_SEC"`
	MaxRoundsWithoutUser       int      `json:"max_rounds_without_user" env:"CLAWGO_AGENTS_DEFAULTS_AUTONOMY_MAX_ROUNDS_WITHOUT_USER"`
	TaskHistoryRetentionDays   int      `json:"task_history_retention_days" env:"CLAWGO_AGENTS_DEFAULTS_AUTONOMY_TASK_HISTORY_RETENTION_DAYS"`
	WaitingResumeDebounceSec   int      `json:"waiting_resume_debounce_sec" env:"CLAWGO_AGENTS_DEFAULTS_AUTONOMY_WAITING_RESUME_DEBOUNCE_SEC"`
	AllowedTaskKeywords        []string `json:"allowed_task_keywords" env:"CLAWGO_AGENTS_DEFAULTS_AUTONOMY_ALLOWED_TASK_KEYWORDS"`
	EKGConsecutiveErrorThreshold int  `json:"ekg_consecutive_error_threshold" env:"CLAWGO_AGENTS_DEFAULTS_AUTONOMY_EKG_CONSECUTIVE_ERROR_THRESHOLD"`
	// Deprecated: kept for backward compatibility with existing config files.
	NotifyChannel              string `json:"notify_channel,omitempty"`
	// Deprecated: kept for backward compatibility with existing config files.
	NotifyChatID               string `json:"notify_chat_id,omitempty"`
}

type AgentTextConfig struct {
	NoResponseFallback         string   `json:"no_response_fallback"`
	ThinkOnlyFallback          string   `json:"think_only_fallback"`
	MemoryRecallKeywords       []string `json:"memory_recall_keywords"`
	LangUsage                  string   `json:"lang_usage"`
	LangInvalid                string   `json:"lang_invalid"`
	LangUpdatedTemplate        string   `json:"lang_updated_template"`
	SubagentsNone              string   `json:"subagents_none"`
	SessionsNone               string   `json:"sessions_none"`
	UnsupportedAction          string   `json:"unsupported_action"`
	SystemRewriteTemplate      string   `json:"system_rewrite_template"`
	RuntimeCompactionNote      string   `json:"runtime_compaction_note"`
	StartupCompactionNote      string   `json:"startup_compaction_note"`
	AutonomyImportantKeywords  []string `json:"autonomy_important_keywords"`
	AutonomyCompletionTemplate string   `json:"autonomy_completion_template"`
	AutonomyBlockedTemplate    string   `json:"autonomy_blocked_template"`
}

type HeartbeatConfig struct {
	Enabled        bool   `json:"enabled" env:"CLAWGO_AGENTS_DEFAULTS_HEARTBEAT_ENABLED"`
	EverySec       int    `json:"every_sec" env:"CLAWGO_AGENTS_DEFAULTS_HEARTBEAT_EVERY_SEC"`
	AckMaxChars    int    `json:"ack_max_chars" env:"CLAWGO_AGENTS_DEFAULTS_HEARTBEAT_ACK_MAX_CHARS"`
	PromptTemplate string `json:"prompt_template" env:"CLAWGO_AGENTS_DEFAULTS_HEARTBEAT_PROMPT_TEMPLATE"`
}

type RuntimeControlConfig struct {
	IntentMaxInputChars           int                       `json:"intent_max_input_chars" env:"CLAWGO_INTENT_MAX_INPUT_CHARS"`
	AutonomyTickIntervalSec       int                       `json:"autonomy_tick_interval_sec" env:"CLAWGO_AUTONOMY_TICK_INTERVAL_SEC"`
	AutonomyMinRunIntervalSec     int                       `json:"autonomy_min_run_interval_sec" env:"CLAWGO_AUTONOMY_MIN_RUN_INTERVAL_SEC"`
	AutonomyIdleThresholdSec      int                       `json:"autonomy_idle_threshold_sec" env:"CLAWGO_AUTONOMY_IDLE_THRESHOLD_SEC"`
	AutonomyMaxRoundsWithoutUser  int                       `json:"autonomy_max_rounds_without_user" env:"CLAWGO_AUTONOMY_MAX_ROUNDS_WITHOUT_USER"`
	AutonomyMaxPendingDurationSec int                       `json:"autonomy_max_pending_duration_sec" env:"CLAWGO_AUTONOMY_MAX_PENDING_DURATION_SEC"`
	AutonomyMaxConsecutiveStalls  int                       `json:"autonomy_max_consecutive_stalls" env:"CLAWGO_AUTONOMY_MAX_STALLS"`
	AutoLearnMaxRoundsWithoutUser int                       `json:"autolearn_max_rounds_without_user" env:"CLAWGO_AUTOLEARN_MAX_ROUNDS_WITHOUT_USER"`
	RunStateTTLSeconds            int                       `json:"run_state_ttl_seconds" env:"CLAWGO_RUN_STATE_TTL_SECONDS"`
	RunStateMax                   int                       `json:"run_state_max" env:"CLAWGO_RUN_STATE_MAX"`
	ToolParallelSafeNames         []string                  `json:"tool_parallel_safe_names"`
	ToolMaxParallelCalls          int                       `json:"tool_max_parallel_calls"`
	SystemSummary                 SystemSummaryPolicyConfig `json:"system_summary"`
}

type SystemSummaryPolicyConfig struct {
	Marker          string `json:"marker"`
	CompletedPrefix string `json:"completed_prefix"`
	ChangesPrefix   string `json:"changes_prefix"`
	OutcomePrefix   string `json:"outcome_prefix"`
	CompletedTitle  string `json:"completed_title"`
	ChangesTitle    string `json:"changes_title"`
	OutcomesTitle   string `json:"outcomes_title"`
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
	InboundMessageIDDedupeTTLSeconds int            `json:"inbound_message_id_dedupe_ttl_seconds" env:"CLAWGO_CHANNELS_INBOUND_MESSAGE_ID_DEDUPE_TTL_SECONDS"`
	InboundContentDedupeWindowSeconds int           `json:"inbound_content_dedupe_window_seconds" env:"CLAWGO_CHANNELS_INBOUND_CONTENT_DEDUPE_WINDOW_SECONDS"`
	OutboundDedupeWindowSeconds       int           `json:"outbound_dedupe_window_seconds" env:"CLAWGO_CHANNELS_OUTBOUND_DEDUPE_WINDOW_SECONDS"`
	WhatsApp                          WhatsAppConfig `json:"whatsapp"`
	Telegram                          TelegramConfig `json:"telegram"`
	Feishu                            FeishuConfig   `json:"feishu"`
	Discord                           DiscordConfig  `json:"discord"`
	MaixCam                           MaixCamConfig  `json:"maixcam"`
	QQ                                QQConfig       `json:"qq"`
	DingTalk                          DingTalkConfig `json:"dingtalk"`
}

type WhatsAppConfig struct {
	Enabled   bool     `json:"enabled" env:"CLAWGO_CHANNELS_WHATSAPP_ENABLED"`
	BridgeURL string   `json:"bridge_url" env:"CLAWGO_CHANNELS_WHATSAPP_BRIDGE_URL"`
	AllowFrom []string `json:"allow_from" env:"CLAWGO_CHANNELS_WHATSAPP_ALLOW_FROM"`
}

type TelegramConfig struct {
	Enabled                bool     `json:"enabled" env:"CLAWGO_CHANNELS_TELEGRAM_ENABLED"`
	Token                  string   `json:"token" env:"CLAWGO_CHANNELS_TELEGRAM_TOKEN"`
	AllowFrom              []string `json:"allow_from" env:"CLAWGO_CHANNELS_TELEGRAM_ALLOW_FROM"`
	AllowChats             []string `json:"allow_chats" env:"CLAWGO_CHANNELS_TELEGRAM_ALLOW_CHATS"`
	EnableGroups           bool     `json:"enable_groups" env:"CLAWGO_CHANNELS_TELEGRAM_ENABLE_GROUPS"`
	RequireMentionInGroups bool     `json:"require_mention_in_groups" env:"CLAWGO_CHANNELS_TELEGRAM_REQUIRE_MENTION_IN_GROUPS"`
}

type FeishuConfig struct {
	Enabled              bool     `json:"enabled" env:"CLAWGO_CHANNELS_FEISHU_ENABLED"`
	AppID                string   `json:"app_id" env:"CLAWGO_CHANNELS_FEISHU_APP_ID"`
	AppSecret            string   `json:"app_secret" env:"CLAWGO_CHANNELS_FEISHU_APP_SECRET"`
	EncryptKey           string   `json:"encrypt_key" env:"CLAWGO_CHANNELS_FEISHU_ENCRYPT_KEY"`
	VerificationToken    string   `json:"verification_token" env:"CLAWGO_CHANNELS_FEISHU_VERIFICATION_TOKEN"`
	AllowFrom            []string `json:"allow_from" env:"CLAWGO_CHANNELS_FEISHU_ALLOW_FROM"`
	AllowChats           []string `json:"allow_chats" env:"CLAWGO_CHANNELS_FEISHU_ALLOW_CHATS"`
	EnableGroups         bool     `json:"enable_groups" env:"CLAWGO_CHANNELS_FEISHU_ENABLE_GROUPS"`
	RequireMentionInGroups bool   `json:"require_mention_in_groups" env:"CLAWGO_CHANNELS_FEISHU_REQUIRE_MENTION_IN_GROUPS"`
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

type providerProxyItem struct {
	Name string `json:"name"`
	ProviderConfig
}

func (p *ProvidersConfig) UnmarshalJSON(data []byte) error {
	var tmp struct {
		Proxy   ProviderConfig  `json:"proxy"`
		Proxies json.RawMessage `json:"proxies"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	p.Proxy = tmp.Proxy
	p.Proxies = map[string]ProviderConfig{}
	if len(bytes.TrimSpace(tmp.Proxies)) == 0 || string(bytes.TrimSpace(tmp.Proxies)) == "null" {
		return nil
	}
	// Preferred format: object map
	var asMap map[string]ProviderConfig
	if err := json.Unmarshal(tmp.Proxies, &asMap); err == nil {
		for k, v := range asMap {
			if k == "" {
				continue
			}
			p.Proxies[k] = v
		}
		return nil
	}
	// Compatibility format: array [{name, ...provider fields...}]
	var asArr []providerProxyItem
	if err := json.Unmarshal(tmp.Proxies, &asArr); err == nil {
		for _, it := range asArr {
			if it.Name == "" {
				continue
			}
			p.Proxies[it.Name] = it.ProviderConfig
		}
		return nil
	}
	return fmt.Errorf("providers.proxies must be object map or array of {name,...}")
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
	Host  string `json:"host" env:"CLAWGO_GATEWAY_HOST"`
	Port  int    `json:"port" env:"CLAWGO_GATEWAY_PORT"`
	Token string `json:"token" env:"CLAWGO_GATEWAY_TOKEN"`
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
	Enabled            bool          `json:"enabled" env:"CLAWGO_TOOLS_SHELL_ENABLED"`
	WorkingDir         string        `json:"working_dir" env:"CLAWGO_TOOLS_SHELL_WORKING_DIR"`
	Timeout            time.Duration `json:"timeout" env:"CLAWGO_TOOLS_SHELL_TIMEOUT"`
	AutoInstallMissing bool          `json:"auto_install_missing" env:"CLAWGO_TOOLS_SHELL_AUTO_INSTALL_MISSING"`
	Sandbox            SandboxConfig `json:"sandbox"`
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
				Heartbeat: HeartbeatConfig{
					Enabled:        true,
					EverySec:       30 * 60,
					AckMaxChars:    64,
					PromptTemplate: "Read HEARTBEAT.md if it exists (workspace context). Follow it strictly. Do not infer or repeat old tasks from prior chats. If nothing needs attention, reply HEARTBEAT_OK.",
				},
				Autonomy: AutonomyConfig{
					Enabled:                     false,
					TickIntervalSec:             30,
					MinRunIntervalSec:           20,
					MaxPendingDurationSec:       180,
					MaxConsecutiveStalls:        3,
					MaxDispatchPerTick:          2,
					NotifyCooldownSec:           300,
					NotifySameReasonCooldownSec: 900,
					QuietHours:                  "23:00-08:00",
					UserIdleResumeSec:           20,
					MaxRoundsWithoutUser:        12,
					TaskHistoryRetentionDays:    3,
					WaitingResumeDebounceSec:    5,
					AllowedTaskKeywords:         []string{},
					EKGConsecutiveErrorThreshold: 3,
				},
				Texts: AgentTextConfig{
					NoResponseFallback:         "I've completed processing but have no response to give.",
					ThinkOnlyFallback:          "Thinking process completed.",
					MemoryRecallKeywords:       []string{"remember", "previous", "preference", "todo", "decision", "date", "when did", "what did we"},
					LangUsage:                  "Usage: /lang <code>",
					LangInvalid:                "Invalid language code.",
					LangUpdatedTemplate:        "Language preference updated to %s",
					SubagentsNone:              "No subagents.",
					SessionsNone:               "No sessions.",
					UnsupportedAction:          "unsupported action",
					SystemRewriteTemplate:      "Rewrite the following internal system update in concise user-facing language:\n\n%s",
					RuntimeCompactionNote:      "[runtime-compaction] removed %d old messages, kept %d recent messages",
					StartupCompactionNote:      "[startup-compaction] removed %d old messages, kept %d recent messages",
					AutonomyImportantKeywords:  []string{"urgent", "payment", "release", "deadline", "p0", "asap"},
					AutonomyCompletionTemplate: "✅ Completed: %s\nReply \"continue %s\" to proceed to the next step.",
					AutonomyBlockedTemplate:    "⚠️ Task blocked: %s (%s)\nReply \"continue %s\" and I will retry.",
				},
				ContextCompaction: ContextCompactionConfig{
					Enabled:            true,
					Mode:               "summary",
					TriggerMessages:    60,
					KeepRecentMessages: 20,
					MaxSummaryChars:    6000,
					MaxTranscriptChars: 20000,
				},
				RuntimeControl: RuntimeControlConfig{
					IntentMaxInputChars:           1200,
					AutonomyTickIntervalSec:       20,
					AutonomyMinRunIntervalSec:     20,
					AutonomyIdleThresholdSec:      20,
					AutonomyMaxRoundsWithoutUser:  120,
					AutonomyMaxPendingDurationSec: 180,
					AutonomyMaxConsecutiveStalls:  3,
					AutoLearnMaxRoundsWithoutUser: 200,
					RunStateTTLSeconds:            1800,
					RunStateMax:                   500,
					ToolParallelSafeNames:         []string{"read_file", "list_files", "find_files", "grep_files", "memory_search", "web_search", "repo_map", "system_info"},
					ToolMaxParallelCalls:          2,
					SystemSummary: SystemSummaryPolicyConfig{
						Marker:          "## System Task Summary",
						CompletedPrefix: "- Completed:",
						ChangesPrefix:   "- Changes:",
						OutcomePrefix:   "- Outcome:",
						CompletedTitle:  "Completed Actions",
						ChangesTitle:    "Change Summaries",
						OutcomesTitle:   "Execution Outcomes",
					},
				},
			},
		},
		Channels: ChannelsConfig{
			InboundMessageIDDedupeTTLSeconds: 600,
			InboundContentDedupeWindowSeconds: 12,
			OutboundDedupeWindowSeconds:       12,
			WhatsApp: WhatsAppConfig{
				Enabled:   false,
				BridgeURL: "ws://localhost:3001",
				AllowFrom: []string{},
			},
			Telegram: TelegramConfig{
				Enabled:                false,
				Token:                  "",
				AllowFrom:              []string{},
				AllowChats:             []string{},
				EnableGroups:           true,
				RequireMentionInGroups: true,
			},
			Feishu: FeishuConfig{
				Enabled:                false,
				AppID:                  "",
				AppSecret:              "",
				EncryptKey:             "",
				VerificationToken:      "",
				AllowFrom:              []string{},
				AllowChats:             []string{},
				EnableGroups:           true,
				RequireMentionInGroups: true,
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
			Host:  "0.0.0.0",
			Port:  18790,
			Token: "",
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
				Enabled:            true,
				Timeout:            60 * time.Second,
				AutoInstallMissing: false,
				Sandbox: SandboxConfig{
					Enabled: false,
					Image:   "alpine:3.20",
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
