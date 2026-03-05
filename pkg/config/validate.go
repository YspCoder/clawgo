package config

import (
	"fmt"
	"strings"
)

// Validate returns configuration problems found in cfg.
// It does not mutate cfg.
func Validate(cfg *Config) []error {
	if cfg == nil {
		return []error{fmt.Errorf("config is nil")}
	}

	var errs []error

	if cfg.Agents.Defaults.MaxToolIterations <= 0 {
		errs = append(errs, fmt.Errorf("agents.defaults.max_tool_iterations must be > 0"))
	}
	rc := cfg.Agents.Defaults.RuntimeControl
	if rc.IntentMaxInputChars < 200 {
		errs = append(errs, fmt.Errorf("agents.defaults.runtime_control.intent_max_input_chars must be >= 200"))
	}
	if rc.AutoLearnMaxRoundsWithoutUser <= 0 {
		errs = append(errs, fmt.Errorf("agents.defaults.runtime_control.autolearn_max_rounds_without_user must be > 0"))
	}
	if rc.RunStateTTLSeconds < 60 {
		errs = append(errs, fmt.Errorf("agents.defaults.runtime_control.run_state_ttl_seconds must be >= 60"))
	}
	if rc.RunStateMax <= 0 {
		errs = append(errs, fmt.Errorf("agents.defaults.runtime_control.run_state_max must be > 0"))
	}
	errs = append(errs, validateNonEmptyStringList("agents.defaults.runtime_control.tool_parallel_safe_names", rc.ToolParallelSafeNames)...)
	if rc.ToolMaxParallelCalls <= 0 {
		errs = append(errs, fmt.Errorf("agents.defaults.runtime_control.tool_max_parallel_calls must be > 0"))
	}
	if strings.TrimSpace(rc.SystemSummary.Marker) == "" {
		errs = append(errs, fmt.Errorf("agents.defaults.runtime_control.system_summary.marker must be non-empty"))
	}
	if strings.TrimSpace(rc.SystemSummary.CompletedPrefix) == "" {
		errs = append(errs, fmt.Errorf("agents.defaults.runtime_control.system_summary.completed_prefix must be non-empty"))
	}
	if strings.TrimSpace(rc.SystemSummary.ChangesPrefix) == "" {
		errs = append(errs, fmt.Errorf("agents.defaults.runtime_control.system_summary.changes_prefix must be non-empty"))
	}
	if strings.TrimSpace(rc.SystemSummary.OutcomePrefix) == "" {
		errs = append(errs, fmt.Errorf("agents.defaults.runtime_control.system_summary.outcome_prefix must be non-empty"))
	}
	if strings.TrimSpace(rc.SystemSummary.CompletedTitle) == "" {
		errs = append(errs, fmt.Errorf("agents.defaults.runtime_control.system_summary.completed_title must be non-empty"))
	}
	if strings.TrimSpace(rc.SystemSummary.ChangesTitle) == "" {
		errs = append(errs, fmt.Errorf("agents.defaults.runtime_control.system_summary.changes_title must be non-empty"))
	}
	if strings.TrimSpace(rc.SystemSummary.OutcomesTitle) == "" {
		errs = append(errs, fmt.Errorf("agents.defaults.runtime_control.system_summary.outcomes_title must be non-empty"))
	}
	hb := cfg.Agents.Defaults.Heartbeat
	if hb.Enabled {
		if hb.EverySec <= 0 {
			errs = append(errs, fmt.Errorf("agents.defaults.heartbeat.every_sec must be > 0 when enabled=true"))
		}
		if hb.AckMaxChars <= 0 {
			errs = append(errs, fmt.Errorf("agents.defaults.heartbeat.ack_max_chars must be > 0 when enabled=true"))
		}
	}
	if cfg.Agents.Defaults.ContextCompaction.Enabled {
		cc := cfg.Agents.Defaults.ContextCompaction
		if cc.Mode != "" {
			switch cc.Mode {
			case "summary", "responses_compact", "hybrid":
			default:
				errs = append(errs, fmt.Errorf("agents.defaults.context_compaction.mode must be one of: summary, responses_compact, hybrid"))
			}
		}
		if cc.TriggerMessages <= 0 {
			errs = append(errs, fmt.Errorf("agents.defaults.context_compaction.trigger_messages must be > 0 when enabled=true"))
		}
		if cc.KeepRecentMessages <= 0 {
			errs = append(errs, fmt.Errorf("agents.defaults.context_compaction.keep_recent_messages must be > 0 when enabled=true"))
		}
		if cc.TriggerMessages > 0 && cc.KeepRecentMessages >= cc.TriggerMessages {
			errs = append(errs, fmt.Errorf("agents.defaults.context_compaction.keep_recent_messages must be < trigger_messages"))
		}
		if cc.MaxSummaryChars <= 0 {
			errs = append(errs, fmt.Errorf("agents.defaults.context_compaction.max_summary_chars must be > 0 when enabled=true"))
		}
		if cc.MaxTranscriptChars <= 0 {
			errs = append(errs, fmt.Errorf("agents.defaults.context_compaction.max_transcript_chars must be > 0 when enabled=true"))
		}
	}

	if len(cfg.Providers.Proxies) == 0 {
		errs = append(errs, validateProviderConfig("providers.proxy", cfg.Providers.Proxy)...)
	} else {
		for name, p := range cfg.Providers.Proxies {
			errs = append(errs, validateProviderConfig("providers.proxies."+name, p)...)
		}
	}
	if cfg.Agents.Defaults.Proxy != "" {
		if !providerExists(cfg, cfg.Agents.Defaults.Proxy) {
			errs = append(errs, fmt.Errorf("agents.defaults.proxy %q not found in providers", cfg.Agents.Defaults.Proxy))
		}
	}
	for _, name := range cfg.Agents.Defaults.ProxyFallbacks {
		if !providerExists(cfg, name) {
			errs = append(errs, fmt.Errorf("agents.defaults.proxy_fallbacks contains unknown proxy %q", name))
		}
	}
	if cfg.Agents.Defaults.ContextCompaction.Enabled && cfg.Agents.Defaults.ContextCompaction.Mode == "responses_compact" {
		active := cfg.Agents.Defaults.Proxy
		if active == "" {
			active = "proxy"
		}
		if pc, ok := providerConfigByName(cfg, active); !ok || !pc.SupportsResponsesCompact {
			errs = append(errs, fmt.Errorf("context_compaction.mode=responses_compact requires active proxy %q with supports_responses_compact=true", active))
		}
	}

	if cfg.Gateway.Port <= 0 || cfg.Gateway.Port > 65535 {
		errs = append(errs, fmt.Errorf("gateway.port must be in 1..65535"))
	}
	if cfg.Cron.MinSleepSec <= 0 {
		errs = append(errs, fmt.Errorf("cron.min_sleep_sec must be > 0"))
	}
	if cfg.Cron.MaxSleepSec <= 0 {
		errs = append(errs, fmt.Errorf("cron.max_sleep_sec must be > 0"))
	}
	if cfg.Cron.MinSleepSec > cfg.Cron.MaxSleepSec {
		errs = append(errs, fmt.Errorf("cron.min_sleep_sec must be <= cron.max_sleep_sec"))
	}
	if cfg.Cron.RetryBackoffBaseSec <= 0 {
		errs = append(errs, fmt.Errorf("cron.retry_backoff_base_sec must be > 0"))
	}
	if cfg.Cron.RetryBackoffMaxSec <= 0 {
		errs = append(errs, fmt.Errorf("cron.retry_backoff_max_sec must be > 0"))
	}
	if cfg.Cron.RetryBackoffBaseSec > cfg.Cron.RetryBackoffMaxSec {
		errs = append(errs, fmt.Errorf("cron.retry_backoff_base_sec must be <= cron.retry_backoff_max_sec"))
	}
	if cfg.Cron.MaxConsecutiveFailureRetries < 0 {
		errs = append(errs, fmt.Errorf("cron.max_consecutive_failure_retries must be >= 0"))
	}
	if cfg.Cron.MaxWorkers <= 0 {
		errs = append(errs, fmt.Errorf("cron.max_workers must be > 0"))
	}

	if cfg.Logging.Enabled {
		if cfg.Logging.Dir == "" {
			errs = append(errs, fmt.Errorf("logging.dir is required when logging.enabled=true"))
		}
		if cfg.Logging.Filename == "" {
			errs = append(errs, fmt.Errorf("logging.filename is required when logging.enabled=true"))
		}
		if cfg.Logging.MaxSizeMB <= 0 {
			errs = append(errs, fmt.Errorf("logging.max_size_mb must be > 0"))
		}
		if cfg.Logging.RetentionDays <= 0 {
			errs = append(errs, fmt.Errorf("logging.retention_days must be > 0"))
		}
	}

	if cfg.Sentinel.Enabled && cfg.Sentinel.IntervalSec <= 0 {
		errs = append(errs, fmt.Errorf("sentinel.interval_sec must be > 0 when sentinel.enabled=true"))
	}
	if cfg.Memory.RecentDays <= 0 {
		errs = append(errs, fmt.Errorf("memory.recent_days must be > 0"))
	}

	if cfg.Channels.InboundMessageIDDedupeTTLSeconds <= 0 {
		errs = append(errs, fmt.Errorf("channels.inbound_message_id_dedupe_ttl_seconds must be > 0"))
	}
	if cfg.Channels.InboundContentDedupeWindowSeconds <= 0 {
		errs = append(errs, fmt.Errorf("channels.inbound_content_dedupe_window_seconds must be > 0"))
	}
	if cfg.Channels.OutboundDedupeWindowSeconds <= 0 {
		errs = append(errs, fmt.Errorf("channels.outbound_dedupe_window_seconds must be > 0"))
	}
	if cfg.Channels.Telegram.Enabled && cfg.Channels.Telegram.Token == "" {
		errs = append(errs, fmt.Errorf("channels.telegram.token is required when channels.telegram.enabled=true"))
	}
	if cfg.Channels.Discord.Enabled && cfg.Channels.Discord.Token == "" {
		errs = append(errs, fmt.Errorf("channels.discord.token is required when channels.discord.enabled=true"))
	}
	if cfg.Channels.WhatsApp.Enabled && cfg.Channels.WhatsApp.BridgeURL == "" {
		errs = append(errs, fmt.Errorf("channels.whatsapp.bridge_url is required when channels.whatsapp.enabled=true"))
	}
	if cfg.Channels.DingTalk.Enabled {
		if cfg.Channels.DingTalk.ClientID == "" {
			errs = append(errs, fmt.Errorf("channels.dingtalk.client_id is required when channels.dingtalk.enabled=true"))
		}
		if cfg.Channels.DingTalk.ClientSecret == "" {
			errs = append(errs, fmt.Errorf("channels.dingtalk.client_secret is required when channels.dingtalk.enabled=true"))
		}
	}
	if cfg.Channels.Feishu.Enabled {
		if cfg.Channels.Feishu.AppID == "" {
			errs = append(errs, fmt.Errorf("channels.feishu.app_id is required when channels.feishu.enabled=true"))
		}
		if cfg.Channels.Feishu.AppSecret == "" {
			errs = append(errs, fmt.Errorf("channels.feishu.app_secret is required when channels.feishu.enabled=true"))
		}
	}
	if cfg.Channels.QQ.Enabled {
		if cfg.Channels.QQ.AppID == "" {
			errs = append(errs, fmt.Errorf("channels.qq.app_id is required when channels.qq.enabled=true"))
		}
		if cfg.Channels.QQ.AppSecret == "" {
			errs = append(errs, fmt.Errorf("channels.qq.app_secret is required when channels.qq.enabled=true"))
		}
	}

	return errs
}

func validateProviderConfig(path string, p ProviderConfig) []error {
	var errs []error
	if p.APIBase == "" {
		errs = append(errs, fmt.Errorf("%s.api_base is required", path))
	}
	if p.TimeoutSec <= 0 {
		errs = append(errs, fmt.Errorf("%s.timeout_sec must be > 0", path))
	}
	if len(p.Models) == 0 {
		errs = append(errs, fmt.Errorf("%s.models must contain at least one model", path))
	}
	if p.Responses.WebSearchContextSize != "" {
		switch p.Responses.WebSearchContextSize {
		case "low", "medium", "high":
		default:
			errs = append(errs, fmt.Errorf("%s.responses.web_search_context_size must be one of: low, medium, high", path))
		}
	}
	if p.Responses.FileSearchMaxNumResults < 0 {
		errs = append(errs, fmt.Errorf("%s.responses.file_search_max_num_results must be >= 0", path))
	}
	errs = append(errs, validateNonEmptyStringList(path+".responses.file_search_vector_store_ids", p.Responses.FileSearchVectorStoreIDs)...)
	errs = append(errs, validateNonEmptyStringList(path+".responses.include", p.Responses.Include)...)
	return errs
}

func providerExists(cfg *Config, name string) bool {
	if name == "proxy" && cfg.Providers.Proxy.APIBase != "" {
		return true
	}
	if cfg.Providers.Proxies == nil {
		return false
	}
	_, ok := cfg.Providers.Proxies[name]
	return ok
}

func providerConfigByName(cfg *Config, name string) (ProviderConfig, bool) {
	if strings.TrimSpace(name) == "proxy" {
		return cfg.Providers.Proxy, true
	}
	pc, ok := cfg.Providers.Proxies[name]
	return pc, ok
}

func validateNonEmptyStringList(path string, values []string) []error {
	if len(values) == 0 {
		return nil
	}
	var errs []error
	for i, value := range values {
		if strings.TrimSpace(value) == "" {
			errs = append(errs, fmt.Errorf("%s[%d] must not be empty", path, i))
		}
	}
	return errs
}
