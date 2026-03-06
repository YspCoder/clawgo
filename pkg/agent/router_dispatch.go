package agent

import (
	"context"
	"strings"
	"time"

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
	"clawgo/pkg/runtimecfg"
	"clawgo/pkg/tools"
)

func (al *AgentLoop) maybeAutoRoute(ctx context.Context, msg bus.InboundMessage) (string, bool, error) {
	if al == nil || al.subagentRouter == nil {
		return "", false, nil
	}
	if msg.Channel == "system" || msg.Channel == "internal" {
		return "", false, nil
	}
	if msg.Metadata != nil {
		if trigger := strings.ToLower(strings.TrimSpace(msg.Metadata["trigger"])); trigger != "" && trigger != "user" {
			return "", false, nil
		}
	}
	cfg := runtimecfg.Get()
	if cfg == nil || !cfg.Agents.Router.Enabled {
		return "", false, nil
	}
	agentID, taskText := resolveAutoRouteTarget(cfg, msg.Content)
	if agentID == "" || strings.TrimSpace(taskText) == "" {
		return "", false, nil
	}
	waitTimeout := cfg.Agents.Router.DefaultTimeoutSec
	if waitTimeout <= 0 {
		waitTimeout = 120
	}
	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(waitTimeout)*time.Second)
	defer cancel()
	task, err := al.subagentRouter.DispatchTask(waitCtx, tools.RouterDispatchRequest{
		Task:          taskText,
		AgentID:       agentID,
		OriginChannel: msg.Channel,
		OriginChatID:  msg.ChatID,
	})
	if err != nil {
		return "", true, err
	}
	reply, err := al.subagentRouter.WaitReply(waitCtx, task.ID, 100*time.Millisecond)
	if err != nil {
		return "", true, err
	}
	return al.subagentRouter.MergeResults([]*tools.RouterReply{reply}), true, nil
}

func resolveAutoRouteTarget(cfg *config.Config, raw string) (string, string) {
	if cfg == nil {
		return "", ""
	}
	content := strings.TrimSpace(raw)
	if content == "" || len(cfg.Agents.Subagents) == 0 {
		return "", ""
	}
	maxChars := cfg.Agents.Router.Policy.IntentMaxInputChars
	if maxChars > 0 && len([]rune(content)) > maxChars {
		return "", ""
	}
	lower := strings.ToLower(content)
	for agentID, subcfg := range cfg.Agents.Subagents {
		if !subcfg.Enabled {
			continue
		}
		marker := "@" + strings.ToLower(strings.TrimSpace(agentID))
		if strings.HasPrefix(lower, marker+" ") || lower == marker {
			return agentID, strings.TrimSpace(content[len(marker):])
		}
		prefix := "agent:" + strings.ToLower(strings.TrimSpace(agentID))
		if strings.HasPrefix(lower, prefix+" ") || lower == prefix {
			return agentID, strings.TrimSpace(content[len(prefix):])
		}
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Agents.Router.Strategy), "rules_first") {
		if agentID := selectAgentByRules(cfg, content); agentID != "" {
			return agentID, content
		}
	}
	return "", ""
}

func selectAgentByRules(cfg *config.Config, content string) string {
	if cfg == nil {
		return ""
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	lower := strings.ToLower(content)
	bestID := ""
	bestScore := 0
	tied := false
	if agentID := selectAgentByConfiguredRules(cfg, lower); agentID != "" {
		return agentID
	}
	for agentID, subcfg := range cfg.Agents.Subagents {
		if !subcfg.Enabled {
			continue
		}
		score := scoreRouteCandidate(agentID, subcfg, lower)
		if score <= 0 {
			continue
		}
		if score > bestScore {
			bestID = agentID
			bestScore = score
			tied = false
			continue
		}
		if score == bestScore {
			tied = true
		}
	}
	if tied || bestScore < 2 {
		return ""
	}
	return bestID
}

func selectAgentByConfiguredRules(cfg *config.Config, content string) string {
	if cfg == nil {
		return ""
	}
	bestID := ""
	bestScore := 0
	tied := false
	for _, rule := range cfg.Agents.Router.Rules {
		agentID := strings.TrimSpace(rule.AgentID)
		if agentID == "" {
			continue
		}
		subcfg, ok := cfg.Agents.Subagents[agentID]
		if !ok || !subcfg.Enabled {
			continue
		}
		score := 0
		for _, kw := range rule.Keywords {
			kw = strings.ToLower(strings.TrimSpace(kw))
			if kw != "" && strings.Contains(content, kw) {
				score++
			}
		}
		if score <= 0 {
			continue
		}
		if score > bestScore {
			bestID = agentID
			bestScore = score
			tied = false
			continue
		}
		if score == bestScore {
			tied = true
		}
	}
	if tied || bestScore < 1 {
		return ""
	}
	return bestID
}

func scoreRouteCandidate(agentID string, subcfg config.SubagentConfig, content string) int {
	score := 0
	for _, token := range routeKeywords(agentID, subcfg) {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == "" {
			continue
		}
		if strings.Contains(content, token) {
			score++
		}
	}
	return score
}

func routeKeywords(agentID string, subcfg config.SubagentConfig) []string {
	set := map[string]struct{}{}
	add := func(items ...string) {
		for _, item := range items {
			item = strings.ToLower(strings.TrimSpace(item))
			if item == "" {
				continue
			}
			set[item] = struct{}{}
		}
	}
	add(agentID, subcfg.Role, subcfg.DisplayName, subcfg.Type)
	role := strings.ToLower(strings.TrimSpace(subcfg.Role))
	switch role {
	case "code", "coding", "coder", "dev", "developer":
		add("code", "coding", "implement", "refactor", "fix bug", "bugfix", "debug", "写代码", "实现", "重构", "修复", "改代码")
	case "test", "tester", "testing", "qa":
		add("test", "testing", "regression", "verify", "validate", "qa", "回归", "测试", "验证", "检查")
	case "docs", "doc", "writer", "documentation":
		add("docs", "documentation", "write docs", "document", "readme", "文档", "说明", "README")
	case "research", "researcher":
		add("research", "investigate", "analyze", "compare", "调研", "分析", "研究", "比较")
	}
	agentLower := strings.ToLower(strings.TrimSpace(agentID))
	switch agentLower {
	case "coder":
		add("code", "implement", "fix", "debug", "写代码", "实现", "修复")
	case "tester":
		add("test", "regression", "verify", "回归", "测试", "验证")
	case "researcher":
		add("research", "analyze", "investigate", "调研", "分析")
	case "doc_writer", "writer", "docs":
		add("docs", "readme", "document", "文档", "说明")
	}
	out := make([]string, 0, len(set))
	for item := range set {
		out = append(out, item)
	}
	return out
}
