package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"clawgo/pkg/config"
	"clawgo/pkg/runtimecfg"
	"clawgo/pkg/tools"
)

func (al *AgentLoop) HandleSubagentRuntime(ctx context.Context, action string, args map[string]interface{}) (interface{}, error) {
	if al == nil || al.subagentManager == nil {
		return nil, fmt.Errorf("subagent runtime is not configured")
	}
	if al.subagentRouter == nil {
		return nil, fmt.Errorf("subagent router is not configured")
	}
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		action = "list"
	}

	sm := al.subagentManager
	router := al.subagentRouter
	switch action {
	case "list":
		tasks := sm.ListTasks()
		items := make([]*tools.SubagentTask, 0, len(tasks))
		for _, task := range tasks {
			items = append(items, cloneSubagentTask(task))
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Created > items[j].Created })
		return map[string]interface{}{"items": items}, nil
	case "get", "info":
		taskID, err := resolveSubagentTaskIDForRuntime(sm, runtimeStringArg(args, "id"))
		if err != nil {
			return nil, err
		}
		task, ok := sm.GetTask(taskID)
		if !ok {
			return map[string]interface{}{"found": false}, nil
		}
		return map[string]interface{}{"found": true, "task": cloneSubagentTask(task)}, nil
	case "spawn", "create":
		taskInput := runtimeStringArg(args, "task")
		if taskInput == "" {
			return nil, fmt.Errorf("task is required")
		}
		msg, err := sm.Spawn(ctx, tools.SubagentSpawnOptions{
			Task:           taskInput,
			Label:          runtimeStringArg(args, "label"),
			Role:           runtimeStringArg(args, "role"),
			AgentID:        runtimeStringArg(args, "agent_id"),
			MaxRetries:     runtimeIntArg(args, "max_retries", 0),
			RetryBackoff:   runtimeIntArg(args, "retry_backoff_ms", 0),
			TimeoutSec:     runtimeIntArg(args, "timeout_sec", 0),
			MaxTaskChars:   runtimeIntArg(args, "max_task_chars", 0),
			MaxResultChars: runtimeIntArg(args, "max_result_chars", 0),
			OriginChannel:  fallbackString(runtimeStringArg(args, "channel"), "webui"),
			OriginChatID:   fallbackString(runtimeStringArg(args, "chat_id"), "webui"),
		})
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"message": msg}, nil
	case "dispatch_and_wait":
		taskInput := runtimeStringArg(args, "task")
		if taskInput == "" {
			return nil, fmt.Errorf("task is required")
		}
		task, err := router.DispatchTask(ctx, tools.RouterDispatchRequest{
			Task:           taskInput,
			Label:          runtimeStringArg(args, "label"),
			Role:           runtimeStringArg(args, "role"),
			AgentID:        runtimeStringArg(args, "agent_id"),
			ThreadID:       runtimeStringArg(args, "thread_id"),
			CorrelationID:  runtimeStringArg(args, "correlation_id"),
			ParentRunID:    runtimeStringArg(args, "parent_run_id"),
			OriginChannel:  fallbackString(runtimeStringArg(args, "channel"), "webui"),
			OriginChatID:   fallbackString(runtimeStringArg(args, "chat_id"), "webui"),
			MaxRetries:     runtimeIntArg(args, "max_retries", 0),
			RetryBackoff:   runtimeIntArg(args, "retry_backoff_ms", 0),
			TimeoutSec:     runtimeIntArg(args, "timeout_sec", 0),
			MaxTaskChars:   runtimeIntArg(args, "max_task_chars", 0),
			MaxResultChars: runtimeIntArg(args, "max_result_chars", 0),
		})
		if err != nil {
			return nil, err
		}
		waitTimeoutSec := runtimeIntArg(args, "wait_timeout_sec", 120)
		waitCtx := ctx
		var cancel context.CancelFunc
		if waitTimeoutSec > 0 {
			waitCtx, cancel = context.WithTimeout(ctx, time.Duration(waitTimeoutSec)*time.Second)
			defer cancel()
		}
		reply, err := router.WaitReply(waitCtx, task.ID, 100*time.Millisecond)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"task":   cloneSubagentTask(task),
			"reply":  reply,
			"merged": router.MergeResults([]*tools.RouterReply{reply}),
		}, nil
	case "registry":
		cfg := runtimecfg.Get()
		items := make([]map[string]interface{}, 0)
		if cfg != nil {
			items = make([]map[string]interface{}, 0, len(cfg.Agents.Subagents))
			for agentID, subcfg := range cfg.Agents.Subagents {
				promptFileFound := false
				if strings.TrimSpace(subcfg.SystemPromptFile) != "" {
					if absPath, err := al.resolvePromptFilePath(subcfg.SystemPromptFile); err == nil {
						if info, statErr := os.Stat(absPath); statErr == nil && !info.IsDir() {
							promptFileFound = true
						}
					}
				}
				items = append(items, map[string]interface{}{
					"agent_id":           agentID,
					"enabled":            subcfg.Enabled,
					"type":               subcfg.Type,
					"transport":          fallbackString(strings.TrimSpace(subcfg.Transport), "local"),
					"node_id":            strings.TrimSpace(subcfg.NodeID),
					"parent_agent_id":    strings.TrimSpace(subcfg.ParentAgentID),
					"notify_main_policy": fallbackString(strings.TrimSpace(subcfg.NotifyMainPolicy), "final_only"),
					"display_name":       subcfg.DisplayName,
					"role":               subcfg.Role,
					"description":        subcfg.Description,
					"system_prompt":      subcfg.SystemPrompt,
					"system_prompt_file": subcfg.SystemPromptFile,
					"prompt_file_found":  promptFileFound,
					"memory_namespace":   subcfg.MemoryNamespace,
					"tool_allowlist":     append([]string(nil), subcfg.Tools.Allowlist...),
					"routing_keywords":   routeKeywordsForRegistry(cfg.Agents.Router.Rules, agentID),
					"managed_by":         "config.json",
				})
			}
		}
		if store := sm.ProfileStore(); store != nil {
			if profiles, err := store.List(); err == nil {
				for _, profile := range profiles {
					if strings.TrimSpace(profile.ManagedBy) != "node_registry" {
						continue
					}
					items = append(items, map[string]interface{}{
						"agent_id":           profile.AgentID,
						"enabled":            strings.EqualFold(strings.TrimSpace(profile.Status), "active"),
						"type":               "node_branch",
						"transport":          profile.Transport,
						"node_id":            profile.NodeID,
						"parent_agent_id":    profile.ParentAgentID,
						"notify_main_policy": fallbackString(strings.TrimSpace(profile.NotifyMainPolicy), "final_only"),
						"display_name":       profile.Name,
						"role":               profile.Role,
						"description":        "Node-registered remote main agent branch",
						"system_prompt":      profile.SystemPrompt,
						"system_prompt_file": profile.SystemPromptFile,
						"prompt_file_found":  false,
						"memory_namespace":   profile.MemoryNamespace,
						"tool_allowlist":     append([]string(nil), profile.ToolAllowlist...),
						"routing_keywords":   []string{},
						"managed_by":         profile.ManagedBy,
					})
				}
			}
		}
		sort.Slice(items, func(i, j int) bool {
			left, _ := items[i]["agent_id"].(string)
			right, _ := items[j]["agent_id"].(string)
			return left < right
		})
		return map[string]interface{}{"items": items}, nil
	case "set_config_subagent_enabled":
		agentID := runtimeStringArg(args, "agent_id")
		if agentID == "" {
			return nil, fmt.Errorf("agent_id is required")
		}
		if al.isProtectedMainAgent(agentID) {
			return nil, fmt.Errorf("main agent %q cannot be disabled", agentID)
		}
		enabled, ok := args["enabled"].(bool)
		if !ok {
			return nil, fmt.Errorf("enabled is required")
		}
		return tools.UpsertConfigSubagent(al.configPath, map[string]interface{}{
			"agent_id": agentID,
			"enabled":  enabled,
		})
	case "delete_config_subagent":
		agentID := runtimeStringArg(args, "agent_id")
		if agentID == "" {
			return nil, fmt.Errorf("agent_id is required")
		}
		if al.isProtectedMainAgent(agentID) {
			return nil, fmt.Errorf("main agent %q cannot be deleted", agentID)
		}
		return tools.DeleteConfigSubagent(al.configPath, agentID)
	case "upsert_config_subagent":
		return tools.UpsertConfigSubagent(al.configPath, args)
	case "prompt_file_get":
		relPath := runtimeStringArg(args, "path")
		if relPath == "" {
			return nil, fmt.Errorf("path is required")
		}
		absPath, err := al.resolvePromptFilePath(relPath)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				return map[string]interface{}{"found": false, "path": relPath, "content": ""}, nil
			}
			return nil, err
		}
		return map[string]interface{}{"found": true, "path": relPath, "content": string(data)}, nil
	case "prompt_file_set":
		relPath := runtimeStringArg(args, "path")
		if relPath == "" {
			return nil, fmt.Errorf("path is required")
		}
		content := runtimeRawStringArg(args, "content")
		absPath, err := al.resolvePromptFilePath(relPath)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
			return nil, err
		}
		return map[string]interface{}{"ok": true, "path": relPath, "bytes": len(content)}, nil
	case "prompt_file_bootstrap":
		agentID := runtimeStringArg(args, "agent_id")
		if agentID == "" {
			return nil, fmt.Errorf("agent_id is required")
		}
		relPath := runtimeStringArg(args, "path")
		if relPath == "" {
			relPath = filepath.ToSlash(filepath.Join("agents", agentID, "AGENT.md"))
		}
		absPath, err := al.resolvePromptFilePath(relPath)
		if err != nil {
			return nil, err
		}
		overwrite, _ := args["overwrite"].(bool)
		if _, err := os.Stat(absPath); err == nil && !overwrite {
			data, readErr := os.ReadFile(absPath)
			if readErr != nil {
				return nil, readErr
			}
			return map[string]interface{}{
				"ok":      true,
				"created": false,
				"path":    relPath,
				"content": string(data),
			}, nil
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return nil, err
		}
		content := buildPromptTemplate(agentID, runtimeStringArg(args, "role"), runtimeStringArg(args, "display_name"))
		if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"ok":      true,
			"created": true,
			"path":    relPath,
			"content": content,
		}, nil
	case "kill":
		taskID, err := resolveSubagentTaskIDForRuntime(sm, runtimeStringArg(args, "id"))
		if err != nil {
			return nil, err
		}
		ok := sm.KillTask(taskID)
		return map[string]interface{}{"ok": ok}, nil
	case "resume":
		taskID, err := resolveSubagentTaskIDForRuntime(sm, runtimeStringArg(args, "id"))
		if err != nil {
			return nil, err
		}
		label, ok := sm.ResumeTask(ctx, taskID)
		return map[string]interface{}{"ok": ok, "label": label}, nil
	case "steer":
		taskID, err := resolveSubagentTaskIDForRuntime(sm, runtimeStringArg(args, "id"))
		if err != nil {
			return nil, err
		}
		msg := runtimeStringArg(args, "message")
		if msg == "" {
			return nil, fmt.Errorf("message is required")
		}
		ok := sm.SteerTask(taskID, msg)
		return map[string]interface{}{"ok": ok}, nil
	case "send":
		taskID, err := resolveSubagentTaskIDForRuntime(sm, runtimeStringArg(args, "id"))
		if err != nil {
			return nil, err
		}
		msg := runtimeStringArg(args, "message")
		if msg == "" {
			return nil, fmt.Errorf("message is required")
		}
		ok := sm.SendTaskMessage(taskID, msg)
		return map[string]interface{}{"ok": ok}, nil
	case "reply":
		taskID, err := resolveSubagentTaskIDForRuntime(sm, runtimeStringArg(args, "id"))
		if err != nil {
			return nil, err
		}
		msg := runtimeStringArg(args, "message")
		if msg == "" {
			return nil, fmt.Errorf("message is required")
		}
		ok := sm.ReplyToTask(taskID, runtimeStringArg(args, "message_id"), msg)
		return map[string]interface{}{"ok": ok}, nil
	case "ack":
		taskID, err := resolveSubagentTaskIDForRuntime(sm, runtimeStringArg(args, "id"))
		if err != nil {
			return nil, err
		}
		messageID := runtimeStringArg(args, "message_id")
		if messageID == "" {
			return nil, fmt.Errorf("message_id is required")
		}
		ok := sm.AckTaskMessage(taskID, messageID)
		return map[string]interface{}{"ok": ok}, nil
	case "thread", "trace":
		threadID := runtimeStringArg(args, "thread_id")
		if threadID == "" {
			taskID, err := resolveSubagentTaskIDForRuntime(sm, runtimeStringArg(args, "id"))
			if err != nil {
				return nil, err
			}
			task, ok := sm.GetTask(taskID)
			if !ok {
				return map[string]interface{}{"found": false}, nil
			}
			threadID = strings.TrimSpace(task.ThreadID)
		}
		if threadID == "" {
			return nil, fmt.Errorf("thread_id is required")
		}
		thread, ok := sm.Thread(threadID)
		if !ok {
			return map[string]interface{}{"found": false}, nil
		}
		items, err := sm.ThreadMessages(threadID, runtimeIntArg(args, "limit", 50))
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"found": true, "thread": thread, "messages": items}, nil
	case "stream":
		taskID, err := resolveSubagentTaskIDForRuntime(sm, runtimeStringArg(args, "id"))
		if err != nil {
			return nil, err
		}
		task, ok := sm.GetTask(taskID)
		if !ok {
			return map[string]interface{}{"found": false}, nil
		}
		events, err := sm.Events(taskID, runtimeIntArg(args, "limit", 100))
		if err != nil {
			return nil, err
		}
		var thread *tools.AgentThread
		var messages []tools.AgentMessage
		if strings.TrimSpace(task.ThreadID) != "" {
			if th, ok := sm.Thread(task.ThreadID); ok {
				thread = th
			}
			messages, err = sm.ThreadMessages(task.ThreadID, runtimeIntArg(args, "limit", 100))
			if err != nil {
				return nil, err
			}
		}
		stream := mergeSubagentStream(events, messages)
		return map[string]interface{}{
			"found":  true,
			"task":   cloneSubagentTask(task),
			"thread": thread,
			"items":  stream,
		}, nil
	case "inbox":
		agentID := runtimeStringArg(args, "agent_id")
		if agentID == "" {
			taskID, err := resolveSubagentTaskIDForRuntime(sm, runtimeStringArg(args, "id"))
			if err != nil {
				return nil, err
			}
			task, ok := sm.GetTask(taskID)
			if !ok {
				return map[string]interface{}{"found": false}, nil
			}
			agentID = strings.TrimSpace(task.AgentID)
		}
		if agentID == "" {
			return nil, fmt.Errorf("agent_id is required")
		}
		items, err := sm.Inbox(agentID, runtimeIntArg(args, "limit", 50))
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"found": true, "agent_id": agentID, "messages": items}, nil
	default:
		return nil, fmt.Errorf("unsupported action: %s", action)
	}
}

func mergeSubagentStream(events []tools.SubagentRunEvent, messages []tools.AgentMessage) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(events)+len(messages))
	for _, evt := range events {
		items = append(items, map[string]interface{}{
			"kind":        "event",
			"at":          evt.At,
			"run_id":      evt.RunID,
			"agent_id":    evt.AgentID,
			"event_type":  evt.Type,
			"status":      evt.Status,
			"message":     evt.Message,
			"retry_count": evt.RetryCount,
		})
	}
	for _, msg := range messages {
		items = append(items, map[string]interface{}{
			"kind":           "message",
			"at":             msg.CreatedAt,
			"message_id":     msg.MessageID,
			"thread_id":      msg.ThreadID,
			"from_agent":     msg.FromAgent,
			"to_agent":       msg.ToAgent,
			"reply_to":       msg.ReplyTo,
			"correlation_id": msg.CorrelationID,
			"message_type":   msg.Type,
			"content":        msg.Content,
			"status":         msg.Status,
			"requires_reply": msg.RequiresReply,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		left, _ := items[i]["at"].(int64)
		right, _ := items[j]["at"].(int64)
		if left != right {
			return left < right
		}
		return fmt.Sprintf("%v", items[i]["kind"]) < fmt.Sprintf("%v", items[j]["kind"])
	})
	return items
}

func cloneSubagentTask(in *tools.SubagentTask) *tools.SubagentTask {
	if in == nil {
		return nil
	}
	out := *in
	if len(in.ToolAllowlist) > 0 {
		out.ToolAllowlist = append([]string(nil), in.ToolAllowlist...)
	}
	if len(in.Steering) > 0 {
		out.Steering = append([]string(nil), in.Steering...)
	}
	if in.SharedState != nil {
		out.SharedState = make(map[string]interface{}, len(in.SharedState))
		for k, v := range in.SharedState {
			out.SharedState[k] = v
		}
	}
	return &out
}

func resolveSubagentTaskIDForRuntime(sm *tools.SubagentManager, raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if id == "" {
		return "", fmt.Errorf("id is required")
	}
	if !strings.HasPrefix(id, "#") {
		return id, nil
	}
	idx, err := strconv.Atoi(strings.TrimPrefix(id, "#"))
	if err != nil || idx <= 0 {
		return "", fmt.Errorf("invalid subagent index")
	}
	tasks := sm.ListTasks()
	if len(tasks) == 0 {
		return "", fmt.Errorf("no subagents")
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Created > tasks[j].Created })
	if idx > len(tasks) {
		return "", fmt.Errorf("subagent index out of range")
	}
	return tasks[idx-1].ID, nil
}

func runtimeStringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func runtimeRawStringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	v, _ := args[key].(string)
	return v
}

func runtimeIntArg(args map[string]interface{}, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	switch v := args[key].(type) {
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	}
	return fallback
}

func fallbackString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func routeKeywordsForRegistry(rules []config.AgentRouteRule, agentID string) []string {
	agentID = strings.TrimSpace(agentID)
	for _, rule := range rules {
		if strings.TrimSpace(rule.AgentID) == agentID {
			return append([]string(nil), rule.Keywords...)
		}
	}
	return nil
}

func (al *AgentLoop) isProtectedMainAgent(agentID string) bool {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return false
	}
	cfg := runtimecfg.Get()
	if cfg == nil {
		return agentID == "main"
	}
	mainID := strings.TrimSpace(cfg.Agents.Router.MainAgentID)
	if mainID == "" {
		mainID = "main"
	}
	return agentID == mainID
}

func (al *AgentLoop) resolvePromptFilePath(relPath string) (string, error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("path must be relative")
	}
	cleaned := filepath.Clean(relPath)
	if cleaned == "." || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("path must stay within workspace")
	}
	workspace := "."
	if al != nil && strings.TrimSpace(al.workspace) != "" {
		workspace = al.workspace
	}
	return filepath.Join(workspace, cleaned), nil
}

func buildPromptTemplate(agentID, role, displayName string) string {
	agentID = strings.TrimSpace(agentID)
	role = strings.TrimSpace(role)
	displayName = strings.TrimSpace(displayName)
	title := displayName
	if title == "" {
		title = agentID
	}
	if title == "" {
		title = "subagent"
	}
	if role == "" {
		role = "worker"
	}
	return strings.TrimSpace(fmt.Sprintf(`# %s

## Role
You are the %s subagent. Work within your role boundary and report concrete outcomes.

## Priorities
- Follow workspace-level policy from workspace/AGENTS.md.
- Complete the assigned task directly. Do not redefine the objective.
- Prefer concrete edits, verification, and concise reporting over long analysis.

## Collaboration
- Treat the main agent as the coordinator unless the task explicitly says otherwise.
- Surface blockers, assumptions, and verification status in your reply.
- Keep outputs short and execution-focused.

## Output Format
- Summary: what you changed or checked.
- Risks: anything not verified or still uncertain.
- Next: the most useful immediate follow-up, if any.
`, title, role))
}
