package config

import (
	"fmt"
	"net/url"
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

	for name, p := range cfg.Models.Providers {
		errs = append(errs, validateProviderConfig("models.providers."+name, p)...)
	}
	if len(cfg.Models.Providers) == 0 {
		errs = append(errs, fmt.Errorf("models.providers must contain at least one provider"))
	}
	if primaryRef := strings.TrimSpace(cfg.Agents.Defaults.Model.Primary); primaryRef != "" {
		providerName, modelName := ParseProviderModelRef(primaryRef)
		if providerName == "" {
			providerName = PrimaryProviderName(cfg)
		}
		if !ProviderExists(cfg, providerName) {
			errs = append(errs, fmt.Errorf("agents.defaults.model.primary %q references unknown provider %q", primaryRef, providerName))
		}
		if strings.TrimSpace(modelName) == "" {
			errs = append(errs, fmt.Errorf("agents.defaults.model.primary must include a model, expected provider/model"))
		}
	}
	if cfg.Agents.Defaults.ContextCompaction.Enabled && cfg.Agents.Defaults.ContextCompaction.Mode == "responses_compact" {
		active := PrimaryProviderName(cfg)
		if pc, ok := ProviderConfigByName(cfg, active); !ok || !pc.SupportsResponsesCompact {
			errs = append(errs, fmt.Errorf("context_compaction.mode=responses_compact requires active provider %q with supports_responses_compact=true", active))
		}
	}
	errs = append(errs, validateAgents(cfg)...)

	if cfg.Gateway.Port <= 0 || cfg.Gateway.Port > 65535 {
		errs = append(errs, fmt.Errorf("gateway.port must be in 1..65535"))
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Gateway.Nodes.P2P.Transport)) {
	case "", "websocket_tunnel", "webrtc":
	default:
		errs = append(errs, fmt.Errorf("gateway.nodes.p2p.transport must be one of: websocket_tunnel, webrtc"))
	}
	errs = append(errs, validateNonEmptyStringList("gateway.nodes.p2p.stun_servers", cfg.Gateway.Nodes.P2P.STUNServers)...)
	for i, server := range cfg.Gateway.Nodes.P2P.ICEServers {
		prefix := fmt.Sprintf("gateway.nodes.p2p.ice_servers[%d]", i)
		errs = append(errs, validateNonEmptyStringList(prefix+".urls", server.URLs)...)
		needsAuth := false
		for _, raw := range server.URLs {
			u := strings.ToLower(strings.TrimSpace(raw))
			if strings.HasPrefix(u, "turn:") || strings.HasPrefix(u, "turns:") {
				needsAuth = true
				break
			}
		}
		if needsAuth {
			if strings.TrimSpace(server.Username) == "" {
				errs = append(errs, fmt.Errorf("%s.username is required for turn/turns urls", prefix))
			}
			if strings.TrimSpace(server.Credential) == "" {
				errs = append(errs, fmt.Errorf("%s.credential is required for turn/turns urls", prefix))
			}
		}
	}
	errs = append(errs, validateDispatchTagMap("gateway.nodes.dispatch.action_tags", cfg.Gateway.Nodes.Dispatch.ActionTags)...)
	errs = append(errs, validateDispatchTagMap("gateway.nodes.dispatch.agent_tags", cfg.Gateway.Nodes.Dispatch.AgentTags)...)
	errs = append(errs, validateDispatchTagMap("gateway.nodes.dispatch.allow_actions", cfg.Gateway.Nodes.Dispatch.AllowActions)...)
	errs = append(errs, validateDispatchTagMap("gateway.nodes.dispatch.deny_actions", cfg.Gateway.Nodes.Dispatch.DenyActions)...)
	errs = append(errs, validateDispatchTagMap("gateway.nodes.dispatch.allow_agents", cfg.Gateway.Nodes.Dispatch.AllowAgents)...)
	errs = append(errs, validateDispatchTagMap("gateway.nodes.dispatch.deny_agents", cfg.Gateway.Nodes.Dispatch.DenyAgents)...)
	if cfg.Gateway.Nodes.Artifacts.Enabled && cfg.Gateway.Nodes.Artifacts.KeepLatest <= 0 {
		errs = append(errs, fmt.Errorf("gateway.nodes.artifacts.keep_latest must be > 0 when enabled=true"))
	}
	if cfg.Gateway.Nodes.Artifacts.KeepLatest < 0 {
		errs = append(errs, fmt.Errorf("gateway.nodes.artifacts.keep_latest must be >= 0"))
	}
	if cfg.Gateway.Nodes.Artifacts.RetainDays < 0 {
		errs = append(errs, fmt.Errorf("gateway.nodes.artifacts.retain_days must be >= 0"))
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
	if raw := strings.TrimSpace(cfg.Sentinel.WebhookURL); raw != "" {
		u, err := url.Parse(raw)
		if err != nil || u == nil || u.Host == "" {
			errs = append(errs, fmt.Errorf("sentinel.webhook_url must be a valid http/https URL"))
		} else {
			switch strings.ToLower(strings.TrimSpace(u.Scheme)) {
			case "http", "https":
			default:
				errs = append(errs, fmt.Errorf("sentinel.webhook_url must use http or https"))
			}
		}
	}
	if cfg.Memory.RecentDays <= 0 {
		errs = append(errs, fmt.Errorf("memory.recent_days must be > 0"))
	}
	errs = append(errs, validateMCPTools(cfg)...)

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

func validateDispatchTagMap(prefix string, mapping map[string][]string) []error {
	if len(mapping) == 0 {
		return nil
	}
	errs := make([]error, 0)
	for key, tags := range mapping {
		if strings.TrimSpace(key) == "" {
			errs = append(errs, fmt.Errorf("%s contains empty key", prefix))
			continue
		}
		errs = append(errs, validateNonEmptyStringList(fmt.Sprintf("%s.%s", prefix, key), tags)...)
	}
	return errs
}

func validateMCPTools(cfg *Config) []error {
	var errs []error
	mcp := cfg.Tools.MCP
	if !mcp.Enabled {
		return errs
	}
	if mcp.RequestTimeoutSec <= 0 {
		errs = append(errs, fmt.Errorf("tools.mcp.request_timeout_sec must be > 0 when tools.mcp.enabled=true"))
	}
	for name, server := range mcp.Servers {
		if strings.TrimSpace(name) == "" {
			errs = append(errs, fmt.Errorf("tools.mcp.servers contains an empty server name"))
			continue
		}
		if !server.Enabled {
			continue
		}
		transport := strings.ToLower(strings.TrimSpace(server.Transport))
		if transport == "" {
			transport = "stdio"
		}
		if transport != "stdio" && transport != "http" && transport != "streamable_http" && transport != "sse" {
			errs = append(errs, fmt.Errorf("tools.mcp.servers.%s.transport must be one of: stdio, http, streamable_http, sse", name))
		}
		if transport == "stdio" && strings.TrimSpace(server.Command) == "" {
			errs = append(errs, fmt.Errorf("tools.mcp.servers.%s.command is required when transport=stdio", name))
		}
		if (transport == "http" || transport == "streamable_http" || transport == "sse") && strings.TrimSpace(server.URL) == "" {
			errs = append(errs, fmt.Errorf("tools.mcp.servers.%s.url is required when transport=%s", name, transport))
		}
		permission := strings.ToLower(strings.TrimSpace(server.Permission))
		if permission == "" {
			permission = "workspace"
		}
		if permission != "workspace" && permission != "full" {
			errs = append(errs, fmt.Errorf("tools.mcp.servers.%s.permission must be one of: workspace, full", name))
		}
		if transport == "stdio" {
			if wd := strings.TrimSpace(server.WorkingDir); wd != "" {
				if permission == "full" {
					if !filepath.IsAbs(wd) {
						errs = append(errs, fmt.Errorf("tools.mcp.servers.%s.working_dir must be an absolute path when permission=full", name))
					}
				} else if filepath.IsAbs(wd) {
					workspace := cfg.WorkspacePath()
					rel, err := filepath.Rel(workspace, wd)
					if err != nil || strings.HasPrefix(rel, "..") {
						errs = append(errs, fmt.Errorf("tools.mcp.servers.%s.working_dir must stay within workspace unless permission=full", name))
					}
				}
			}
		}
	}
	return errs
}

func validateAgents(cfg *Config) []error {
	var errs []error
	if len(cfg.Agents.Agents) == 0 {
		return errs
	}
	for agentID, raw := range cfg.Agents.Agents {
		id := strings.TrimSpace(agentID)
		if id == "" {
			errs = append(errs, fmt.Errorf("agents.agents contains an empty agent id"))
			continue
		}
		if strings.TrimSpace(raw.Type) != "" {
			switch strings.TrimSpace(raw.Type) {
			case "agent", "npc", "tool":
			default:
				errs = append(errs, fmt.Errorf("agents.agents.%s.type must be one of: agent, npc, tool", id))
			}
		}
		transport := strings.TrimSpace(raw.Transport)
		if transport != "" {
			switch transport {
			case "local", "node":
			default:
				errs = append(errs, fmt.Errorf("agents.agents.%s.transport must be one of: local, node", id))
			}
		}
		if transport == "node" && strings.TrimSpace(raw.NodeID) == "" {
			errs = append(errs, fmt.Errorf("agents.agents.%s.node_id is required when transport=node", id))
		}
		if raw.Runtime.TimeoutSec < 0 {
			errs = append(errs, fmt.Errorf("agents.agents.%s.runtime.timeout_sec must be >= 0", id))
		}
		if raw.Runtime.MaxRetries < 0 {
			errs = append(errs, fmt.Errorf("agents.agents.%s.runtime.max_retries must be >= 0", id))
		}
		if raw.Runtime.RetryBackoffMs < 0 {
			errs = append(errs, fmt.Errorf("agents.agents.%s.runtime.retry_backoff_ms must be >= 0", id))
		}
		if raw.Runtime.MaxTaskChars < 0 {
			errs = append(errs, fmt.Errorf("agents.agents.%s.runtime.max_task_chars must be >= 0", id))
		}
		if raw.Runtime.MaxResultChars < 0 {
			errs = append(errs, fmt.Errorf("agents.agents.%s.runtime.max_result_chars must be >= 0", id))
		}
		if raw.Runtime.MaxParallelRuns < 0 {
			errs = append(errs, fmt.Errorf("agents.agents.%s.runtime.max_parallel_runs must be >= 0", id))
		}
		if raw.Tools.MaxParallelCalls < 0 {
			errs = append(errs, fmt.Errorf("agents.agents.%s.tools.max_parallel_calls must be >= 0", id))
		}
		if raw.Enabled && transport != "node" && strings.TrimSpace(raw.PromptFile) == "" {
			errs = append(errs, fmt.Errorf("agents.agents.%s.prompt_file is required when enabled=true", id))
		}
		if promptFile := strings.TrimSpace(raw.PromptFile); promptFile != "" {
			if filepath.IsAbs(promptFile) {
				errs = append(errs, fmt.Errorf("agents.agents.%s.prompt_file must be relative", id))
			}
			if cleaned := filepath.Clean(promptFile); strings.HasPrefix(cleaned, "..") {
				errs = append(errs, fmt.Errorf("agents.agents.%s.prompt_file must stay within workspace", id))
			}
		}
		if provider := strings.TrimSpace(raw.Runtime.Provider); provider != "" && !ProviderExists(cfg, provider) {
			errs = append(errs, fmt.Errorf("agents.agents.%s.runtime.provider %q not found in providers", id, provider))
		}
	}
	return errs
}

func validateProviderConfig(path string, p ProviderConfig) []error {
	var errs []error
	authMode := strings.ToLower(strings.TrimSpace(p.Auth))
	if p.APIBase == "" {
		errs = append(errs, fmt.Errorf("%s.api_base is required", path))
	}
	if p.TimeoutSec <= 0 {
		errs = append(errs, fmt.Errorf("%s.timeout_sec must be > 0", path))
	}
	switch authMode {
	case "", "bearer", "oauth", "none", "hybrid":
	default:
		errs = append(errs, fmt.Errorf("%s.auth must be one of: bearer, oauth, hybrid, none", path))
	}
	if len(p.Models) == 0 && authMode != "oauth" && authMode != "hybrid" {
		errs = append(errs, fmt.Errorf("%s.models must contain at least one model", path))
	}
	if authMode == "oauth" && strings.TrimSpace(p.OAuth.Provider) == "" {
		errs = append(errs, fmt.Errorf("%s.oauth.provider is required when auth=oauth", path))
	}
	if authMode == "hybrid" {
		if strings.TrimSpace(p.APIKey) == "" && strings.TrimSpace(p.OAuth.Provider) == "" {
			errs = append(errs, fmt.Errorf("%s.hybrid auth requires api_key or oauth.provider", path))
		}
		if strings.TrimSpace(p.OAuth.Provider) == "" {
			errs = append(errs, fmt.Errorf("%s.oauth.provider is required when auth=hybrid", path))
		}
	}
	if p.OAuth.CooldownSec < 0 {
		errs = append(errs, fmt.Errorf("%s.oauth.cooldown_sec must be >= 0", path))
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
