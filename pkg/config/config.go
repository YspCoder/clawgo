package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Agents   AgentsConfig   `json:"agents"`
	Channels ChannelsConfig `json:"channels"`
	Models   ModelsConfig   `json:"models,omitempty"`
	Gateway  GatewayConfig  `json:"gateway"`
	Cron     CronConfig     `json:"cron"`
	Tools    ToolsConfig    `json:"tools"`
	Logging  LoggingConfig  `json:"logging"`
	Sentinel SentinelConfig `json:"sentinel"`
	Memory   MemoryConfig   `json:"memory"`
	mu       sync.RWMutex
}

type AgentsConfig struct {
	Defaults      AgentDefaults             `json:"defaults"`
	Router        AgentRouterConfig         `json:"router,omitempty"`
	Communication AgentCommunicationConfig  `json:"communication,omitempty"`
	Subagents     map[string]SubagentConfig `json:"subagents,omitempty"`
}

type AgentRouterConfig struct {
	Enabled              bool                    `json:"enabled"`
	MainAgentID          string                  `json:"main_agent_id,omitempty"`
	Strategy             string                  `json:"strategy,omitempty"`
	Policy               AgentRouterPolicyConfig `json:"policy,omitempty"`
	Rules                []AgentRouteRule        `json:"rules,omitempty"`
	AllowDirectAgentChat bool                    `json:"allow_direct_agent_chat,omitempty"`
	MaxHops              int                     `json:"max_hops,omitempty"`
	DefaultTimeoutSec    int                     `json:"default_timeout_sec,omitempty"`
	StickyThreadOwner    bool                    `json:"sticky_thread_owner,omitempty"`
}

type AgentRouterPolicyConfig struct {
	IntentMaxInputChars  int `json:"intent_max_input_chars" env:"CLAWGO_INTENT_MAX_INPUT_CHARS"`
	MaxRoundsWithoutUser int `json:"max_rounds_without_user" env:"CLAWGO_AUTOLEARN_MAX_ROUNDS_WITHOUT_USER"`
}

type AgentRouteRule struct {
	AgentID  string   `json:"agent_id"`
	Keywords []string `json:"keywords,omitempty"`
}

type AgentCommunicationConfig struct {
	Mode                 string `json:"mode,omitempty"`
	PersistThreads       bool   `json:"persist_threads,omitempty"`
	PersistMessages      bool   `json:"persist_messages,omitempty"`
	MaxMessagesPerThread int    `json:"max_messages_per_thread,omitempty"`
	DeadLetterQueue      bool   `json:"dead_letter_queue,omitempty"`
	DefaultMessageTTLSec int    `json:"default_message_ttl_sec,omitempty"`
}

type SubagentConfig struct {
	Enabled               bool                  `json:"enabled"`
	Type                  string                `json:"type,omitempty"`
	Transport             string                `json:"transport,omitempty"`
	ParentAgentID         string                `json:"parent_agent_id,omitempty"`
	NotifyMainPolicy      string                `json:"notify_main_policy,omitempty"`
	DisplayName           string                `json:"display_name,omitempty"`
	Role                  string                `json:"role,omitempty"`
	Description           string                `json:"description,omitempty"`
	SystemPromptFile      string                `json:"system_prompt_file,omitempty"`
	MemoryNamespace       string                `json:"memory_namespace,omitempty"`
	AcceptFrom            []string              `json:"accept_from,omitempty"`
	CanTalkTo             []string              `json:"can_talk_to,omitempty"`
	RequiresMainMediation bool                  `json:"requires_main_mediation,omitempty"`
	DefaultReplyTo        string                `json:"default_reply_to,omitempty"`
	Tools                 SubagentToolsConfig   `json:"tools,omitempty"`
	Runtime               SubagentRuntimeConfig `json:"runtime,omitempty"`
}

func (s *SubagentConfig) UnmarshalJSON(data []byte) error {
	type alias SubagentConfig
	var raw struct {
		alias
		LegacySystemPrompt string `json:"system_prompt"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*s = SubagentConfig(raw.alias)
	return nil
}

type SubagentToolsConfig struct {
	Allowlist        []string `json:"allowlist,omitempty"`
	Denylist         []string `json:"denylist,omitempty"`
	MaxParallelCalls int      `json:"max_parallel_calls,omitempty"`
}

type SubagentRuntimeConfig struct {
	Provider        string  `json:"provider,omitempty"`
	Model           string  `json:"model,omitempty"`
	Temperature     float64 `json:"temperature,omitempty"`
	TimeoutSec      int     `json:"timeout_sec,omitempty"`
	MaxRetries      int     `json:"max_retries,omitempty"`
	RetryBackoffMs  int     `json:"retry_backoff_ms,omitempty"`
	MaxTaskChars    int     `json:"max_task_chars,omitempty"`
	MaxResultChars  int     `json:"max_result_chars,omitempty"`
	MaxParallelRuns int     `json:"max_parallel_runs,omitempty"`
}

type AgentDefaults struct {
	Workspace         string                    `json:"workspace" env:"CLAWGO_AGENTS_DEFAULTS_WORKSPACE"`
	Model             AgentModelDefaults        `json:"model,omitempty"`
	MaxTokens         int                       `json:"max_tokens" env:"CLAWGO_AGENTS_DEFAULTS_MAX_TOKENS"`
	Temperature       float64                   `json:"temperature" env:"CLAWGO_AGENTS_DEFAULTS_TEMPERATURE"`
	MaxToolIterations int                       `json:"max_tool_iterations" env:"CLAWGO_AGENTS_DEFAULTS_MAX_TOOL_ITERATIONS"`
	Heartbeat         HeartbeatConfig           `json:"heartbeat"`
	ContextCompaction ContextCompactionConfig   `json:"context_compaction"`
	Execution         AgentExecutionConfig      `json:"execution"`
	SummaryPolicy     SystemSummaryPolicyConfig `json:"summary_policy"`
}

type AgentModelDefaults struct {
	Primary string `json:"primary,omitempty" env:"CLAWGO_AGENTS_DEFAULTS_MODEL_PRIMARY"`
}

type HeartbeatConfig struct {
	Enabled        bool   `json:"enabled" env:"CLAWGO_AGENTS_DEFAULTS_HEARTBEAT_ENABLED"`
	EverySec       int    `json:"every_sec" env:"CLAWGO_AGENTS_DEFAULTS_HEARTBEAT_EVERY_SEC"`
	AckMaxChars    int    `json:"ack_max_chars" env:"CLAWGO_AGENTS_DEFAULTS_HEARTBEAT_ACK_MAX_CHARS"`
	PromptTemplate string `json:"prompt_template" env:"CLAWGO_AGENTS_DEFAULTS_HEARTBEAT_PROMPT_TEMPLATE"`
}

type AgentExecutionConfig struct {
	RunStateTTLSeconds    int      `json:"run_state_ttl_seconds" env:"CLAWGO_RUN_STATE_TTL_SECONDS"`
	RunStateMax           int      `json:"run_state_max" env:"CLAWGO_RUN_STATE_MAX"`
	ToolParallelSafeNames []string `json:"tool_parallel_safe_names"`
	ToolMaxParallelCalls  int      `json:"tool_max_parallel_calls"`
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
	InboundMessageIDDedupeTTLSeconds  int            `json:"inbound_message_id_dedupe_ttl_seconds" env:"CLAWGO_CHANNELS_INBOUND_MESSAGE_ID_DEDUPE_TTL_SECONDS"`
	InboundContentDedupeWindowSeconds int            `json:"inbound_content_dedupe_window_seconds" env:"CLAWGO_CHANNELS_INBOUND_CONTENT_DEDUPE_WINDOW_SECONDS"`
	OutboundDedupeWindowSeconds       int            `json:"outbound_dedupe_window_seconds" env:"CLAWGO_CHANNELS_OUTBOUND_DEDUPE_WINDOW_SECONDS"`
	WhatsApp                          WhatsAppConfig `json:"whatsapp"`
	Telegram                          TelegramConfig `json:"telegram"`
	Feishu                            FeishuConfig   `json:"feishu"`
	Discord                           DiscordConfig  `json:"discord"`
	MaixCam                           MaixCamConfig  `json:"maixcam"`
	QQ                                QQConfig       `json:"qq"`
	DingTalk                          DingTalkConfig `json:"dingtalk"`
}

type WhatsAppConfig struct {
	Enabled                bool     `json:"enabled" env:"CLAWGO_CHANNELS_WHATSAPP_ENABLED"`
	BridgeURL              string   `json:"bridge_url" env:"CLAWGO_CHANNELS_WHATSAPP_BRIDGE_URL"`
	AllowFrom              []string `json:"allow_from" env:"CLAWGO_CHANNELS_WHATSAPP_ALLOW_FROM"`
	EnableGroups           bool     `json:"enable_groups" env:"CLAWGO_CHANNELS_WHATSAPP_ENABLE_GROUPS"`
	RequireMentionInGroups bool     `json:"require_mention_in_groups" env:"CLAWGO_CHANNELS_WHATSAPP_REQUIRE_MENTION_IN_GROUPS"`
}

type TelegramConfig struct {
	Enabled                bool     `json:"enabled" env:"CLAWGO_CHANNELS_TELEGRAM_ENABLED"`
	Token                  string   `json:"token" env:"CLAWGO_CHANNELS_TELEGRAM_TOKEN"`
	Streaming              bool     `json:"streaming"`
	AllowFrom              []string `json:"allow_from" env:"CLAWGO_CHANNELS_TELEGRAM_ALLOW_FROM"`
	AllowChats             []string `json:"allow_chats" env:"CLAWGO_CHANNELS_TELEGRAM_ALLOW_CHATS"`
	EnableGroups           bool     `json:"enable_groups" env:"CLAWGO_CHANNELS_TELEGRAM_ENABLE_GROUPS"`
	RequireMentionInGroups bool     `json:"require_mention_in_groups" env:"CLAWGO_CHANNELS_TELEGRAM_REQUIRE_MENTION_IN_GROUPS"`
}

type FeishuConfig struct {
	Enabled                bool     `json:"enabled" env:"CLAWGO_CHANNELS_FEISHU_ENABLED"`
	AppID                  string   `json:"app_id" env:"CLAWGO_CHANNELS_FEISHU_APP_ID"`
	AppSecret              string   `json:"app_secret" env:"CLAWGO_CHANNELS_FEISHU_APP_SECRET"`
	EncryptKey             string   `json:"encrypt_key" env:"CLAWGO_CHANNELS_FEISHU_ENCRYPT_KEY"`
	VerificationToken      string   `json:"verification_token" env:"CLAWGO_CHANNELS_FEISHU_VERIFICATION_TOKEN"`
	AllowFrom              []string `json:"allow_from" env:"CLAWGO_CHANNELS_FEISHU_ALLOW_FROM"`
	AllowChats             []string `json:"allow_chats" env:"CLAWGO_CHANNELS_FEISHU_ALLOW_CHATS"`
	EnableGroups           bool     `json:"enable_groups" env:"CLAWGO_CHANNELS_FEISHU_ENABLE_GROUPS"`
	RequireMentionInGroups bool     `json:"require_mention_in_groups" env:"CLAWGO_CHANNELS_FEISHU_REQUIRE_MENTION_IN_GROUPS"`
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

type ModelsConfig struct {
	Providers map[string]ProviderConfig `json:"providers,omitempty"`
}

type ProviderConfig struct {
	APIKey                   string                  `json:"api_key" env:"CLAWGO_PROVIDERS_{{.Name}}_API_KEY"`
	APIBase                  string                  `json:"api_base" env:"CLAWGO_PROVIDERS_{{.Name}}_API_BASE"`
	Models                   []string                `json:"models" env:"CLAWGO_PROVIDERS_{{.Name}}_MODELS"`
	SupportsResponsesCompact bool                    `json:"supports_responses_compact" env:"CLAWGO_PROVIDERS_{{.Name}}_SUPPORTS_RESPONSES_COMPACT"`
	Auth                     string                  `json:"auth" env:"CLAWGO_PROVIDERS_{{.Name}}_AUTH"`
	TimeoutSec               int                     `json:"timeout_sec" env:"CLAWGO_PROVIDERS_PROXY_TIMEOUT_SEC"`
	RuntimePersist           bool                    `json:"runtime_persist,omitempty"`
	RuntimeHistoryFile       string                  `json:"runtime_history_file,omitempty"`
	RuntimeHistoryMax        int                     `json:"runtime_history_max,omitempty"`
	OAuth                    ProviderOAuthConfig     `json:"oauth,omitempty"`
	Responses                ProviderResponsesConfig `json:"responses"`
}

type ProviderOAuthConfig struct {
	Provider        string   `json:"provider,omitempty"`
	NetworkProxy    string   `json:"network_proxy,omitempty"`
	CredentialFile  string   `json:"credential_file,omitempty"`
	CredentialFiles []string `json:"credential_files,omitempty"`
	CallbackPort    int      `json:"callback_port,omitempty"`
	ClientID        string   `json:"client_id,omitempty"`
	ClientSecret    string   `json:"client_secret,omitempty"`
	AuthURL         string   `json:"auth_url,omitempty"`
	TokenURL        string   `json:"token_url,omitempty"`
	RedirectURL     string   `json:"redirect_url,omitempty"`
	Scopes          []string `json:"scopes,omitempty"`
	CooldownSec     int      `json:"cooldown_sec,omitempty"`
	RefreshScanSec  int      `json:"refresh_scan_sec,omitempty"`
	RefreshLeadSec  int      `json:"refresh_lead_sec,omitempty"`
}

type ProviderResponsesConfig struct {
	WebSearchEnabled         bool     `json:"web_search_enabled"`
	WebSearchContextSize     string   `json:"web_search_context_size"`
	FileSearchVectorStoreIDs []string `json:"file_search_vector_store_ids"`
	FileSearchMaxNumResults  int      `json:"file_search_max_num_results"`
	Include                  []string `json:"include"`
	StreamIncludeUsage       bool     `json:"stream_include_usage"`
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

type MCPServerConfig struct {
	Enabled     bool              `json:"enabled"`
	Transport   string            `json:"transport"`
	URL         string            `json:"url,omitempty"`
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Permission  string            `json:"permission,omitempty"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	Description string            `json:"description,omitempty"`
	Package     string            `json:"package,omitempty"`
}

type MCPToolsConfig struct {
	Enabled           bool                       `json:"enabled"`
	RequestTimeoutSec int                        `json:"request_timeout_sec"`
	Servers           map[string]MCPServerConfig `json:"servers,omitempty"`
}

type ToolsConfig struct {
	Web        WebToolsConfig   `json:"web"`
	Shell      ShellConfig      `json:"shell"`
	Filesystem FilesystemConfig `json:"filesystem"`
	MCP        MCPToolsConfig   `json:"mcp"`
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
	WebhookURL    string `json:"webhook_url" env:"CLAWGO_SENTINEL_WEBHOOK_URL"`
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
	cfg := &Config{
		Agents: AgentsConfig{
			Defaults: AgentDefaults{
				Workspace:         filepath.Join(configDir, "workspace"),
				Model:             AgentModelDefaults{Primary: "openai/gpt-5.4"},
				MaxTokens:         8192,
				Temperature:       0.7,
				MaxToolIterations: 20,
				Heartbeat: HeartbeatConfig{
					Enabled:        true,
					EverySec:       30 * 60,
					AckMaxChars:    64,
					PromptTemplate: "",
				},
				ContextCompaction: ContextCompactionConfig{
					Enabled:            true,
					Mode:               "summary",
					TriggerMessages:    60,
					KeepRecentMessages: 20,
					MaxSummaryChars:    6000,
					MaxTranscriptChars: 20000,
				},
				Execution: AgentExecutionConfig{
					RunStateTTLSeconds:    1800,
					RunStateMax:           500,
					ToolParallelSafeNames: []string{"read_file", "list_files", "find_files", "grep_files", "memory_search", "web_search", "system_info"},
					ToolMaxParallelCalls:  2,
				},
				SummaryPolicy: SystemSummaryPolicyConfig{
					Marker:          "## System Task Summary",
					CompletedPrefix: "- Completed:",
					ChangesPrefix:   "- Changes:",
					OutcomePrefix:   "- Outcome:",
					CompletedTitle:  "Completed Actions",
					ChangesTitle:    "Change Summaries",
					OutcomesTitle:   "Execution Outcomes",
				},
			},
			Router: AgentRouterConfig{
				Enabled:     false,
				MainAgentID: "main",
				Strategy:    "rules_first",
				Policy: AgentRouterPolicyConfig{
					IntentMaxInputChars:  1200,
					MaxRoundsWithoutUser: 200,
				},
				Rules:                []AgentRouteRule{},
				AllowDirectAgentChat: false,
				MaxHops:              6,
				DefaultTimeoutSec:    600,
				StickyThreadOwner:    true,
			},
			Communication: AgentCommunicationConfig{
				Mode:                 "mediated",
				PersistThreads:       true,
				PersistMessages:      true,
				MaxMessagesPerThread: 100,
				DeadLetterQueue:      true,
				DefaultMessageTTLSec: 86400,
			},
			Subagents: map[string]SubagentConfig{},
		},
		Channels: ChannelsConfig{
			InboundMessageIDDedupeTTLSeconds:  600,
			InboundContentDedupeWindowSeconds: 12,
			OutboundDedupeWindowSeconds:       12,
			WhatsApp: WhatsAppConfig{
				Enabled:                false,
				BridgeURL:              "",
				AllowFrom:              []string{},
				EnableGroups:           true,
				RequireMentionInGroups: true,
			},
			Telegram: TelegramConfig{
				Enabled:                false,
				Token:                  "",
				Streaming:              false,
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
		Models: ModelsConfig{
			Providers: map[string]ProviderConfig{
				"openai": {
					APIBase:    "https://api.openai.com/v1",
					Models:     []string{"gpt-5.4"},
					TimeoutSec: 90,
				},
			},
		},
		Gateway: GatewayConfig{
			Host:  "0.0.0.0",
			Port:  18790,
			Token: generateGatewayToken(),
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
			MCP: MCPToolsConfig{
				Enabled:           false,
				RequestTimeoutSec: 20,
				Servers:           map[string]MCPServerConfig{},
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
			WebhookURL:    "",
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
	cfg.Normalize()
	return cfg
}

func normalizeProviderNameAlias(name string) string {
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

func ParseProviderModelRef(raw string) (provider string, model string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ""
	}
	if idx := strings.Index(trimmed, "/"); idx > 0 {
		return normalizeProviderNameAlias(trimmed[:idx]), strings.TrimSpace(trimmed[idx+1:])
	}
	return "", trimmed
}

func AllProviderConfigs(cfg *Config) map[string]ProviderConfig {
	out := map[string]ProviderConfig{}
	if cfg == nil {
		return out
	}
	for name, pc := range cfg.Models.Providers {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		out[trimmed] = pc
	}
	return out
}

func ProviderConfigByName(cfg *Config, name string) (ProviderConfig, bool) {
	if cfg == nil {
		return ProviderConfig{}, false
	}
	configs := AllProviderConfigs(cfg)
	trimmed := strings.TrimSpace(name)
	if pc, ok := configs[trimmed]; ok {
		return pc, true
	}
	pc, ok := configs[normalizeProviderNameAlias(trimmed)]
	return pc, ok
}

func ProviderExists(cfg *Config, name string) bool {
	_, ok := ProviderConfigByName(cfg, name)
	return ok
}

func PrimaryProviderName(cfg *Config) string {
	if cfg == nil {
		return "openai"
	}
	if provider, _ := ParseProviderModelRef(cfg.Agents.Defaults.Model.Primary); provider != "" {
		return normalizeProviderNameAlias(provider)
	}
	for name := range cfg.Models.Providers {
		if trimmed := normalizeProviderNameAlias(name); trimmed != "" {
			return trimmed
		}
	}
	return "openai"
}

func generateGatewayToken() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return ""
	}
	return "cg_" + hex.EncodeToString(buf[:])
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
	cfg.Normalize()

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
	if pc, ok := c.Models.Providers[PrimaryProviderName(c)]; ok {
		return pc.APIKey
	}
	return ""
}

func (c *Config) GetAPIBase() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if pc, ok := c.Models.Providers[PrimaryProviderName(c)]; ok {
		return pc.APIBase
	}
	return ""
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
