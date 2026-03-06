package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"clawgo/pkg/bus"
	"clawgo/pkg/tools"
)

func (al *AgentLoop) maybeHandleSubagentConfigIntent(ctx context.Context, msg bus.InboundMessage) (string, bool, error) {
	_ = ctx
	if al == nil {
		return "", false, nil
	}
	if msg.Channel == "system" || msg.Channel == "internal" {
		return "", false, nil
	}
	content := strings.TrimSpace(msg.Content)
	if content == "" {
		return "", false, nil
	}
	if !looksLikeSubagentCreateRequest(content) {
		return "", false, nil
	}
	description := extractSubagentDescription(content)
	if description == "" {
		return "", false, nil
	}
	draft := tools.DraftConfigSubagent(description, "")
	result, err := tools.UpsertConfigSubagent(al.configPath, draft)
	if err != nil {
		return "", true, fmt.Errorf("persist subagent config to %s failed: %w", al.displayConfigPath(), err)
	}
	return formatCreatedSubagentForUser(result, al.displayConfigPath()), true, nil
}

func looksLikeSubagentCreateRequest(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" {
		return false
	}
	createMarkers := []string{
		"创建", "新建", "增加", "添加", "配置一个", "生成一个",
		"create", "add", "new",
	}
	subagentMarkers := []string{
		"subagent", "sub-agent", "agent", "子代理", "子 agent", "工作代理",
	}
	hasCreate := false
	for _, item := range createMarkers {
		if strings.Contains(lower, item) {
			hasCreate = true
			break
		}
	}
	if !hasCreate {
		return false
	}
	for _, item := range subagentMarkers {
		if strings.Contains(lower, item) {
			return true
		}
	}
	return false
}

func extractSubagentDescription(content string) string {
	content = strings.TrimSpace(content)
	replacers := []string{
		"请", "帮我", "给我", "创建", "新建", "增加", "添加", "配置", "生成",
		"a ", "an ", "new ", "create ", "add ",
	}
	out := content
	for _, item := range replacers {
		out = strings.ReplaceAll(out, item, "")
	}
	out = strings.ReplaceAll(out, "子代理", "")
	out = strings.ReplaceAll(out, "subagent", "")
	out = strings.ReplaceAll(out, "sub-agent", "")
	out = strings.TrimSpace(out)
	if out == "" {
		return strings.TrimSpace(content)
	}
	return out
}

func formatCreatedSubagentForUser(result map[string]interface{}, configPath string) string {
	return fmt.Sprintf(
		"subagent 已写入 config.json。\npath: %s\nagent_id: %v\nrole: %v\ndisplay_name: %v\ntool_allowlist: %v\nrouting_keywords: %v\nsystem_prompt_file: %v",
		configPath,
		result["agent_id"],
		result["role"],
		result["display_name"],
		result["tool_allowlist"],
		result["routing_keywords"],
		result["system_prompt_file"],
	)
}

func (al *AgentLoop) displayConfigPath() string {
	if al == nil || strings.TrimSpace(al.configPath) == "" {
		return "config path not configured"
	}
	if abs, err := filepath.Abs(al.configPath); err == nil {
		return abs
	}
	return strings.TrimSpace(al.configPath)
}
