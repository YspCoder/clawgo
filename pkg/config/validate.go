package config

import (
	"fmt"
	"path/filepath"
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
	exec := cfg.Agents.Defaults.Execution
	if exec.RunStateTTLSeconds < 60 {
		errs = append(errs, fmt.Errorf("agents.defaults.execution.run_state_ttl_seconds must be >= 60"))
	}
	if exec.RunStateMax <= 0 {
		errs = append(errs, fmt.Errorf("agents.defaults.execution.run_state_max must be > 0"))
	}
	errs = append(errs, validateNonEmptyStringList("agents.defaults.execution.tool_parallel_safe_names", exec.ToolParallelSafeNames)...)
	if exec.ToolMaxParallelCalls <= 0 {
		errs = append(errs, fmt.Errorf("agents.defaults.execution.tool_max_parallel_calls must be > 0"))
	}
	summary := cfg.Agents.Defaults.SummaryPolicy
	if strings.TrimSpace(summary.Marker) == "" {
		errs = append(errs, fmt.Errorf("agents.defaults.summary_policy.marker must be non-empty"))
	}
	if strings.TrimSpace(summary.CompletedPrefix) == "" {
		errs = append(errs, fmt.Errorf("agents.defaults.summary_policy.completed_prefix must be non-empty"))
	}
	if strings.TrimSpace(summary.ChangesPrefix) == "" {
		errs = append(errs, fmt.Errorf("agents.defaults.summary_policy.changes_prefix must be non-empty"))
	}
	if strings.TrimSpace(summary.OutcomePrefix) == "" {
		errs = append(errs, fmt.Errorf("agents.defaults.summary_policy.outcome_prefix must be non-empty"))
	}
	if strings.TrimSpace(summary.CompletedTitle) == "" {
		errs = append(errs, fmt.Errorf("agents.defaults.summary_policy.completed_title must be non-empty"))
	}
	if strings.TrimSpace(summary.ChangesTitle) == "" {
		errs = append(errs, fmt.Errorf("agents.defaults.summary_policy.changes_title must be non-empty"))
	}
	if strings.TrimSpace(summary.OutcomesTitle) == "" {
		errs = append(errs, fmt.Errorf("agents.defaults.summary_policy.outcomes_title must be non-empty"))
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
	errs = append(errs, validateAgentRouter(cfg)...)
	errs = append(errs, validateAgentCommunication(cfg)...)
	errs = append(errs, validateSubagents(cfg)...)

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

func validateAgentRouter(cfg *Config) []error {
	router := cfg.Agents.Router
	var errs []error
	if router.Policy.IntentMaxInputChars < 200 {
		errs = append(errs, fmt.Errorf("agents.router.policy.intent_max_input_chars must be >= 200"))
	}
	if router.Policy.MaxRoundsWithoutUser <= 0 {
		errs = append(errs, fmt.Errorf("agents.router.policy.max_rounds_without_user must be > 0"))
	}
	if strings.TrimSpace(router.Strategy) != "" {
		switch strings.TrimSpace(router.Strategy) {
		case "rules_first", "round_robin", "manual":
		default:
			errs = append(errs, fmt.Errorf("agents.router.strategy must be one of: rules_first, round_robin, manual"))
		}
	}
	if router.MaxHops < 0 {
		errs = append(errs, fmt.Errorf("agents.router.max_hops must be >= 0"))
	}
	if router.DefaultTimeoutSec < 0 {
		errs = append(errs, fmt.Errorf("agents.router.default_timeout_sec must be >= 0"))
	}
	if router.Enabled && strings.TrimSpace(router.MainAgentID) == "" {
		errs = append(errs, fmt.Errorf("agents.router.main_agent_id is required when agents.router.enabled=true"))
	}
	for i, rule := range router.Rules {
		agentID := strings.TrimSpace(rule.AgentID)
		if agentID == "" {
			errs = append(errs, fmt.Errorf("agents.router.rules[%d].agent_id is required", i))
			continue
		}
		if _, ok := cfg.Agents.Subagents[agentID]; !ok {
			errs = append(errs, fmt.Errorf("agents.router.rules[%d].agent_id %q not found in agents.subagents", i, agentID))
		}
		if len(rule.Keywords) == 0 {
			errs = append(errs, fmt.Errorf("agents.router.rules[%d].keywords must not be empty", i))
		}
		for _, kw := range rule.Keywords {
			if strings.TrimSpace(kw) == "" {
				errs = append(errs, fmt.Errorf("agents.router.rules[%d].keywords must not contain empty values", i))
			}
		}
	}
	return errs
}

func validateAgentCommunication(cfg *Config) []error {
	comm := cfg.Agents.Communication
	var errs []error
	if strings.TrimSpace(comm.Mode) != "" {
		switch strings.TrimSpace(comm.Mode) {
		case "mediated", "direct":
		default:
			errs = append(errs, fmt.Errorf("agents.communication.mode must be one of: mediated, direct"))
		}
	}
	if comm.MaxMessagesPerThread < 0 {
		errs = append(errs, fmt.Errorf("agents.communication.max_messages_per_thread must be >= 0"))
	}
	if comm.DefaultMessageTTLSec < 0 {
		errs = append(errs, fmt.Errorf("agents.communication.default_message_ttl_sec must be >= 0"))
	}
	return errs
}

func validateSubagents(cfg *Config) []error {
	var errs []error
	if len(cfg.Agents.Subagents) == 0 {
		return errs
	}
	mainID := strings.TrimSpace(cfg.Agents.Router.MainAgentID)
	if cfg.Agents.Router.Enabled && mainID != "" {
		if _, ok := cfg.Agents.Subagents[mainID]; !ok {
			errs = append(errs, fmt.Errorf("agents.router.main_agent_id %q not found in agents.subagents", mainID))
		}
	}
	for agentID, raw := range cfg.Agents.Subagents {
		id := strings.TrimSpace(agentID)
		if id == "" {
			errs = append(errs, fmt.Errorf("agents.subagents contains an empty agent id"))
			continue
		}
		if strings.TrimSpace(raw.Type) != "" {
			switch strings.TrimSpace(raw.Type) {
			case "router", "worker", "reviewer", "observer":
			default:
				errs = append(errs, fmt.Errorf("agents.subagents.%s.type must be one of: router, worker, reviewer, observer", id))
			}
		}
		if raw.Runtime.TimeoutSec < 0 {
			errs = append(errs, fmt.Errorf("agents.subagents.%s.runtime.timeout_sec must be >= 0", id))
		}
		if raw.Runtime.MaxRetries < 0 {
			errs = append(errs, fmt.Errorf("agents.subagents.%s.runtime.max_retries must be >= 0", id))
		}
		if raw.Runtime.RetryBackoffMs < 0 {
			errs = append(errs, fmt.Errorf("agents.subagents.%s.runtime.retry_backoff_ms must be >= 0", id))
		}
		if raw.Runtime.MaxTaskChars < 0 {
			errs = append(errs, fmt.Errorf("agents.subagents.%s.runtime.max_task_chars must be >= 0", id))
		}
		if raw.Runtime.MaxResultChars < 0 {
			errs = append(errs, fmt.Errorf("agents.subagents.%s.runtime.max_result_chars must be >= 0", id))
		}
		if raw.Runtime.MaxParallelRuns < 0 {
			errs = append(errs, fmt.Errorf("agents.subagents.%s.runtime.max_parallel_runs must be >= 0", id))
		}
		if raw.Tools.MaxParallelCalls < 0 {
			errs = append(errs, fmt.Errorf("agents.subagents.%s.tools.max_parallel_calls must be >= 0", id))
		}
		if promptFile := strings.TrimSpace(raw.SystemPromptFile); promptFile != "" {
			if filepath.IsAbs(promptFile) {
				errs = append(errs, fmt.Errorf("agents.subagents.%s.system_prompt_file must be relative", id))
			}
			if cleaned := filepath.Clean(promptFile); strings.HasPrefix(cleaned, "..") {
				errs = append(errs, fmt.Errorf("agents.subagents.%s.system_prompt_file must stay within workspace", id))
			}
		}
		if proxy := strings.TrimSpace(raw.Runtime.Proxy); proxy != "" && !providerExists(cfg, proxy) {
			errs = append(errs, fmt.Errorf("agents.subagents.%s.runtime.proxy %q not found in providers", id, proxy))
		}
		for _, sender := range raw.AcceptFrom {
			sender = strings.TrimSpace(sender)
			if sender == "" {
				errs = append(errs, fmt.Errorf("agents.subagents.%s.accept_from must not contain empty values", id))
				continue
			}
			if sender != "user" && sender != id {
				if _, ok := cfg.Agents.Subagents[sender]; !ok {
					errs = append(errs, fmt.Errorf("agents.subagents.%s.accept_from references unknown agent %q", id, sender))
				}
			}
		}
		for _, target := range raw.CanTalkTo {
			target = strings.TrimSpace(target)
			if target == "" {
				errs = append(errs, fmt.Errorf("agents.subagents.%s.can_talk_to must not contain empty values", id))
				continue
			}
			if target != "user" {
				if _, ok := cfg.Agents.Subagents[target]; !ok {
					errs = append(errs, fmt.Errorf("agents.subagents.%s.can_talk_to references unknown agent %q", id, target))
				}
			}
		}
		if raw.RequiresMainMediation && mainID != "" && id == mainID {
			errs = append(errs, fmt.Errorf("agents.subagents.%s.requires_main_mediation must be false for main agent", id))
		}
	}
	for agentID, raw := range cfg.Agents.Subagents {
		id := strings.TrimSpace(agentID)
		for _, target := range raw.CanTalkTo {
			target = strings.TrimSpace(target)
			if target == "" || target == "user" {
				continue
			}
			peer, ok := cfg.Agents.Subagents[target]
			if !ok {
				continue
			}
			if !containsString(raw.AcceptFrom, target) && !containsString(peer.AcceptFrom, id) {
				errs = append(errs, fmt.Errorf("agents.subagents.%s.can_talk_to %q is not reciprocated by accept_from", id, target))
			}
		}
	}
	return errs
}

func containsString(items []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, item := range items {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
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
