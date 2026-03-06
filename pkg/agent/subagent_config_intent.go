package agent

import (
	"context"
	"fmt"
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
	if isSubagentConfigConfirm(content) {
		return al.confirmPendingSubagentDraft(msg.SessionKey)
	}
	if isSubagentConfigCancel(content) {
		return al.cancelPendingSubagentDraft(msg.SessionKey)
	}
	if !looksLikeSubagentCreateRequest(content) {
		return "", false, nil
	}
	description := extractSubagentDescription(content)
	if description == "" {
		return "", false, nil
	}
	draft := tools.DraftConfigSubagent(description, "")
	al.storePendingSubagentDraft(msg.SessionKey, draft)
	return formatSubagentDraftForUser(draft), true, nil
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

func isSubagentConfigConfirm(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	phrases := []string{
		"确认创建", "确认保存", "确认生成", "保存这个子代理", "创建这个子代理",
		"confirm create", "confirm save", "save it", "create it",
	}
	for _, phrase := range phrases {
		if lower == phrase || strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func isSubagentConfigCancel(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	phrases := []string{
		"取消创建", "取消保存", "取消这个子代理", "放弃创建",
		"cancel create", "cancel save", "discard draft", "never mind",
	}
	for _, phrase := range phrases {
		if lower == phrase || strings.Contains(lower, phrase) {
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

func formatSubagentDraftForUser(draft map[string]interface{}) string {
	return fmt.Sprintf(
		"已生成 subagent 草案。\nagent_id: %v\nrole: %v\ndisplay_name: %v\ntool_allowlist: %v\nrouting_keywords: %v\nsystem_prompt: %v\n\n回复“确认创建”会写入 config.json，回复“取消创建”会丢弃这个草案。",
		draft["agent_id"],
		draft["role"],
		draft["display_name"],
		draft["tool_allowlist"],
		draft["routing_keywords"],
		draft["system_prompt"],
	)
}

func (al *AgentLoop) storePendingSubagentDraft(sessionKey string, draft map[string]interface{}) {
	if al == nil || draft == nil {
		return
	}
	if strings.TrimSpace(sessionKey) == "" {
		sessionKey = "main"
	}
	al.streamMu.Lock()
	defer al.streamMu.Unlock()
	if al.pendingSubagentDraft == nil {
		al.pendingSubagentDraft = map[string]map[string]interface{}{}
	}
	copied := make(map[string]interface{}, len(draft))
	for k, v := range draft {
		copied[k] = v
	}
	al.pendingSubagentDraft[sessionKey] = copied
	if al.pendingDraftStore != nil {
		_ = al.pendingDraftStore.Put(sessionKey, copied)
	}
}

func (al *AgentLoop) loadPendingSubagentDraft(sessionKey string) map[string]interface{} {
	if al == nil {
		return nil
	}
	if strings.TrimSpace(sessionKey) == "" {
		sessionKey = "main"
	}
	al.streamMu.Lock()
	defer al.streamMu.Unlock()
	draft := al.pendingSubagentDraft[sessionKey]
	if draft == nil {
		return nil
	}
	copied := make(map[string]interface{}, len(draft))
	for k, v := range draft {
		copied[k] = v
	}
	return copied
}

func (al *AgentLoop) deletePendingSubagentDraft(sessionKey string) {
	if al == nil {
		return
	}
	if strings.TrimSpace(sessionKey) == "" {
		sessionKey = "main"
	}
	al.streamMu.Lock()
	defer al.streamMu.Unlock()
	delete(al.pendingSubagentDraft, sessionKey)
	if al.pendingDraftStore != nil {
		_ = al.pendingDraftStore.Delete(sessionKey)
	}
}

func (al *AgentLoop) confirmPendingSubagentDraft(sessionKey string) (string, bool, error) {
	draft := al.loadPendingSubagentDraft(sessionKey)
	if draft == nil {
		return "", false, nil
	}
	result, err := tools.UpsertConfigSubagent(al.configPath, draft)
	if err != nil {
		return "", true, err
	}
	al.deletePendingSubagentDraft(sessionKey)
	return fmt.Sprintf("subagent 已写入 config.json。\nagent_id: %v\nrules: %v", result["agent_id"], result["rules"]), true, nil
}

func (al *AgentLoop) cancelPendingSubagentDraft(sessionKey string) (string, bool, error) {
	draft := al.loadPendingSubagentDraft(sessionKey)
	if draft == nil {
		return "", false, nil
	}
	al.deletePendingSubagentDraft(sessionKey)
	return "已取消这次 subagent 草案，不会写入 config.json。", true, nil
}
