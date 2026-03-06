package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"clawgo/pkg/config"
	"clawgo/pkg/configops"
	"clawgo/pkg/runtimecfg"
)

func DraftConfigSubagent(description, agentIDHint string) map[string]interface{} {
	desc := strings.TrimSpace(description)
	lower := strings.ToLower(desc)
	role := inferDraftRole(lower)
	agentID := strings.TrimSpace(agentIDHint)
	if agentID == "" {
		agentID = inferDraftAgentID(role, lower)
	}
	displayName := inferDraftDisplayName(role, agentID)
	toolAllowlist := inferDraftToolAllowlist(role)
	keywords := inferDraftKeywords(role, lower)
	systemPrompt := inferDraftSystemPrompt(role, desc)
	return map[string]interface{}{
		"agent_id":         agentID,
		"role":             role,
		"display_name":     displayName,
		"description":      desc,
		"system_prompt":    systemPrompt,
		"memory_namespace": agentID,
		"tool_allowlist":   toolAllowlist,
		"routing_keywords": keywords,
		"type":             "worker",
	}
}

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
	if cfg.Agents.Subagents == nil {
		cfg.Agents.Subagents = map[string]config.SubagentConfig{}
	}
	subcfg := cfg.Agents.Subagents[agentID]
	if enabled, ok := boolArgFromMap(args, "enabled"); ok {
		subcfg.Enabled = enabled
	} else if !subcfg.Enabled {
		subcfg.Enabled = true
	}
	if v := stringArgFromMap(args, "role"); v != "" {
		subcfg.Role = v
	}
	if v := stringArgFromMap(args, "display_name"); v != "" {
		subcfg.DisplayName = v
	}
	if v := stringArgFromMap(args, "description"); v != "" {
		subcfg.Description = v
	}
	if v := stringArgFromMap(args, "system_prompt"); v != "" {
		subcfg.SystemPrompt = v
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

func inferDraftRole(lower string) string {
	switch {
	case containsDraftKeyword(lower, "test", "regression", "qa", "回归", "测试", "验证"):
		return "testing"
	case containsDraftKeyword(lower, "doc", "docs", "readme", "文档", "说明"):
		return "docs"
	case containsDraftKeyword(lower, "research", "investigate", "analyze", "调研", "分析", "研究"):
		return "research"
	default:
		return "coding"
	}
}

func inferDraftAgentID(role, lower string) string {
	switch role {
	case "testing":
		if containsDraftKeyword(lower, "review", "审查", "reviewer") {
			return "reviewer"
		}
		return "tester"
	case "docs":
		return "doc_writer"
	case "research":
		return "researcher"
	default:
		if containsDraftKeyword(lower, "frontend", "ui", "前端") {
			return "frontend-coder"
		}
		if containsDraftKeyword(lower, "backend", "api", "后端") {
			return "backend-coder"
		}
		return "coder"
	}
}

func inferDraftDisplayName(role, agentID string) string {
	switch role {
	case "testing":
		return "Test Agent"
	case "docs":
		return "Docs Agent"
	case "research":
		return "Research Agent"
	default:
		if strings.Contains(agentID, "frontend") {
			return "Frontend Code Agent"
		}
		if strings.Contains(agentID, "backend") {
			return "Backend Code Agent"
		}
		return "Code Agent"
	}
}

func inferDraftToolAllowlist(role string) []string {
	switch role {
	case "testing":
		return []string{"shell", "filesystem", "process_manager", "sessions"}
	case "docs":
		return []string{"filesystem", "read_file", "write_file", "edit_file", "repo_map", "sessions"}
	case "research":
		return []string{"web_search", "web_fetch", "repo_map", "sessions", "memory_search"}
	default:
		return []string{"filesystem", "shell", "repo_map", "sessions"}
	}
}

func inferDraftKeywords(role, lower string) []string {
	seed := []string{}
	switch role {
	case "testing":
		seed = []string{"test", "regression", "verify", "回归", "测试", "验证"}
	case "docs":
		seed = []string{"docs", "readme", "document", "文档", "说明"}
	case "research":
		seed = []string{"research", "analyze", "investigate", "调研", "分析", "研究"}
	default:
		seed = []string{"code", "implement", "fix", "refactor", "代码", "实现", "修复", "重构"}
	}
	if containsDraftKeyword(lower, "frontend", "前端", "ui") {
		seed = append(seed, "frontend", "ui", "前端")
	}
	if containsDraftKeyword(lower, "backend", "后端", "api") {
		seed = append(seed, "backend", "api", "后端")
	}
	return normalizeKeywords(seed)
}

func inferDraftSystemPrompt(role, description string) string {
	switch role {
	case "testing":
		return "你负责测试、验证、回归检查与风险反馈。任务描述：" + description
	case "docs":
		return "你负责文档编写、结构整理和说明补全。任务描述：" + description
	case "research":
		return "你负责调研、分析、比较方案，并输出结论与依据。任务描述：" + description
	default:
		return "你负责代码实现与重构，输出具体修改建议和变更结果。任务描述：" + description
	}
}

func containsDraftKeyword(text string, items ...string) bool {
	for _, item := range items {
		if strings.Contains(text, strings.ToLower(strings.TrimSpace(item))) {
			return true
		}
	}
	return false
}
