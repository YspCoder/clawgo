package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/configops"
	"github.com/YspCoder/clawgo/pkg/runtimecfg"
)

func UpsertConfigSubagent(configPath string, args map[string]interface{}) (map[string]interface{}, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return nil, fmt.Errorf("config path not configured")
	}
	agentID := stringArgFromMap(args, "agent_id")
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	mainID := strings.TrimSpace(cfg.Agents.Router.MainAgentID)
	if mainID == "" {
		mainID = "main"
	}
	if cfg.Agents.Subagents == nil {
		cfg.Agents.Subagents = map[string]config.SubagentConfig{}
	}
	subcfg := cfg.Agents.Subagents[agentID]
	if enabled, ok := boolArgFromMap(args, "enabled"); ok {
		if agentID == mainID && !enabled {
			return nil, fmt.Errorf("main agent %q cannot be disabled", agentID)
		}
		subcfg.Enabled = enabled
	} else if !subcfg.Enabled {
		subcfg.Enabled = true
	}
	if v := stringArgFromMap(args, "role"); v != "" {
		subcfg.Role = v
	}
	if v := stringArgFromMap(args, "transport"); v != "" {
		subcfg.Transport = v
	}
	if v := stringArgFromMap(args, "node_id"); v != "" {
		subcfg.NodeID = v
	}
	if v := stringArgFromMap(args, "parent_agent_id"); v != "" {
		subcfg.ParentAgentID = v
	}
	if v := stringArgFromMap(args, "display_name"); v != "" {
		subcfg.DisplayName = v
	}
	if v := stringArgFromMap(args, "notify_main_policy"); v != "" {
		subcfg.NotifyMainPolicy = v
	}
	if v := stringArgFromMap(args, "description"); v != "" {
		subcfg.Description = v
	}
	if v := stringArgFromMap(args, "system_prompt_file"); v != "" {
		subcfg.SystemPromptFile = v
	}
	if v := stringArgFromMap(args, "memory_namespace"); v != "" {
		subcfg.MemoryNamespace = v
	}
	if items := stringListArgFromMap(args, "tool_allowlist"); len(items) > 0 {
		subcfg.Tools.Allowlist = items
	}
	if v := stringArgFromMap(args, "type"); v != "" {
		subcfg.Type = v
	} else if strings.TrimSpace(subcfg.Type) == "" {
		subcfg.Type = "worker"
	}
	if strings.TrimSpace(subcfg.Transport) == "" {
		subcfg.Transport = "local"
	}
	if subcfg.Enabled && strings.TrimSpace(subcfg.Transport) != "node" && strings.TrimSpace(subcfg.SystemPromptFile) == "" {
		return nil, fmt.Errorf("system_prompt_file is required for enabled agent %q", agentID)
	}
	cfg.Agents.Subagents[agentID] = subcfg
	if kws := stringListArgFromMap(args, "routing_keywords"); len(kws) > 0 {
		cfg.Agents.Router.Rules = upsertRouteRuleConfig(cfg.Agents.Router.Rules, config.AgentRouteRule{
			AgentID:  agentID,
			Keywords: kws,
		})
	}
	if errs := config.Validate(cfg); len(errs) > 0 {
		return nil, fmt.Errorf("config validation failed: %v", errs[0])
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	if _, err := configops.WriteConfigAtomicWithBackup(configPath, data); err != nil {
		return nil, err
	}
	runtimecfg.Set(cfg)
	return map[string]interface{}{
		"ok":       true,
		"agent_id": agentID,
		"subagent": cfg.Agents.Subagents[agentID],
		"rules":    cfg.Agents.Router.Rules,
	}, nil
}

func DeleteConfigSubagent(configPath, agentID string) (map[string]interface{}, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return nil, fmt.Errorf("config path not configured")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	mainID := strings.TrimSpace(cfg.Agents.Router.MainAgentID)
	if mainID == "" {
		mainID = "main"
	}
	if agentID == mainID {
		return nil, fmt.Errorf("main agent %q cannot be deleted", agentID)
	}
	if cfg.Agents.Subagents == nil {
		return map[string]interface{}{"ok": false, "found": false, "agent_id": agentID}, nil
	}
	if _, ok := cfg.Agents.Subagents[agentID]; !ok {
		return map[string]interface{}{"ok": false, "found": false, "agent_id": agentID}, nil
	}
	delete(cfg.Agents.Subagents, agentID)
	cfg.Agents.Router.Rules = removeRouteRuleConfig(cfg.Agents.Router.Rules, agentID)
	if errs := config.Validate(cfg); len(errs) > 0 {
		return nil, fmt.Errorf("config validation failed: %v", errs[0])
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	if _, err := configops.WriteConfigAtomicWithBackup(configPath, data); err != nil {
		return nil, err
	}
	runtimecfg.Set(cfg)
	return map[string]interface{}{
		"ok":       true,
		"found":    true,
		"agent_id": agentID,
		"rules":    cfg.Agents.Router.Rules,
	}, nil
}

func stringArgFromMap(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func boolArgFromMap(args map[string]interface{}, key string) (bool, bool) {
	if args == nil {
		return false, false
	}
	raw, ok := args[key]
	if !ok {
		return false, false
	}
	switch v := raw.(type) {
	case bool:
		return v, true
	default:
		return false, false
	}
}

func stringListArgFromMap(args map[string]interface{}, key string) []string {
	if args == nil {
		return nil
	}
	raw, ok := args[key]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return normalizeKeywords(v)
	case []interface{}:
		items := make([]string, 0, len(v))
		for _, item := range v {
			s, _ := item.(string)
			s = strings.TrimSpace(s)
			if s != "" {
				items = append(items, s)
			}
		}
		return normalizeKeywords(items)
	default:
		return nil
	}
}

func upsertRouteRuleConfig(rules []config.AgentRouteRule, rule config.AgentRouteRule) []config.AgentRouteRule {
	agentID := strings.TrimSpace(rule.AgentID)
	if agentID == "" {
		return rules
	}
	rule.Keywords = normalizeKeywords(rule.Keywords)
	if len(rule.Keywords) == 0 {
		return rules
	}
	out := make([]config.AgentRouteRule, 0, len(rules)+1)
	replaced := false
	for _, existing := range rules {
		if strings.TrimSpace(existing.AgentID) == agentID {
			out = append(out, rule)
			replaced = true
			continue
		}
		out = append(out, existing)
	}
	if !replaced {
		out = append(out, rule)
	}
	return out
}

func removeRouteRuleConfig(rules []config.AgentRouteRule, agentID string) []config.AgentRouteRule {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return rules
	}
	out := make([]config.AgentRouteRule, 0, len(rules))
	for _, existing := range rules {
		if strings.TrimSpace(existing.AgentID) == agentID {
			continue
		}
		out = append(out, existing)
	}
	return out
}

func normalizeKeywords(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
