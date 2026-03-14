package tools

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type SubagentsTool struct {
	manager *SubagentManager
}

func NewSubagentsTool(m *SubagentManager) *SubagentsTool {
	return &SubagentsTool{manager: m}
}

func (t *SubagentsTool) Name() string { return "subagents" }

func (t *SubagentsTool) Description() string {
	return "Manage subagent runs in current process: list, info, kill, steer"
}

func (t *SubagentsTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action":         map[string]interface{}{"type": "string", "description": "list|info|kill|steer|send|log|resume|thread|inbox|reply|trace|ack"},
			"id":             map[string]interface{}{"type": "string", "description": "subagent id/#index/all for info/kill/steer/send/log"},
			"message":        map[string]interface{}{"type": "string", "description": "steering message for steer/send action"},
			"message_id":     map[string]interface{}{"type": "string", "description": "message id for reply/ack"},
			"thread_id":      map[string]interface{}{"type": "string", "description": "thread id for thread/trace action; defaults to task thread"},
			"agent_id":       map[string]interface{}{"type": "string", "description": "agent id for inbox action; defaults to task agent"},
			"limit":          map[string]interface{}{"type": "integer", "description": "max messages/events to show", "default": 20},
			"recent_minutes": map[string]interface{}{"type": "integer", "description": "optional list/info all filter by recent updated minutes"},
		},
		"required": []string{"action"},
	}
}

func (t *SubagentsTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	_ = ctx
	if t.manager == nil {
		return "subagent manager not available", nil
	}
	action := strings.ToLower(MapStringArg(args, "action"))
	id := MapStringArg(args, "id")
	message := MapStringArg(args, "message")
	messageID := MapStringArg(args, "message_id")
	threadID := MapStringArg(args, "thread_id")
	agentID := MapStringArg(args, "agent_id")
	limit := MapIntArg(args, "limit", 20)
	recentMinutes := MapIntArg(args, "recent_minutes", 0)
	type subagentActionHandler func() (string, error)
	var threadHandler subagentActionHandler
	handlers := map[string]subagentActionHandler{
		"list": func() (string, error) {
			tasks := t.filterRecent(t.manager.ListTasks(), recentMinutes)
			if len(tasks) == 0 {
				return "No subagents.", nil
			}
			var sb strings.Builder
			sb.WriteString("Subagents:\n")
			sort.Slice(tasks, func(i, j int) bool { return tasks[i].Created > tasks[j].Created })
			for i, task := range tasks {
				sb.WriteString(fmt.Sprintf("- #%d %s [%s] label=%s agent=%s role=%s session=%s allowlist=%d retry=%d timeout=%ds\n",
					i+1, task.ID, task.Status, task.Label, task.AgentID, task.Role, task.SessionKey, len(task.ToolAllowlist), task.MaxRetries, task.TimeoutSec))
			}
			return strings.TrimSpace(sb.String()), nil
		},
		"info": func() (string, error) {
			if strings.EqualFold(strings.TrimSpace(id), "all") {
				tasks := t.filterRecent(t.manager.ListTasks(), recentMinutes)
				if len(tasks) == 0 {
					return "No subagents.", nil
				}
				sort.Slice(tasks, func(i, j int) bool { return tasks[i].Created > tasks[j].Created })
				var sb strings.Builder
				sb.WriteString("Subagents Summary:\n")
				for i, task := range tasks {
					sb.WriteString(fmt.Sprintf("- #%d %s [%s] label=%s agent=%s role=%s steering=%d allowlist=%d retry=%d timeout=%ds\n",
						i+1, task.ID, task.Status, task.Label, task.AgentID, task.Role, len(task.Steering), len(task.ToolAllowlist), task.MaxRetries, task.TimeoutSec))
				}
				return strings.TrimSpace(sb.String()), nil
			}
			resolvedID, err := t.resolveTaskID(id)
			if err != nil {
				return err.Error(), nil
			}
			task, ok := t.manager.GetTask(resolvedID)
			if !ok {
				return "subagent not found", nil
			}
			info := fmt.Sprintf("ID: %s\nStatus: %s\nLabel: %s\nAgent ID: %s\nRole: %s\nSession Key: %s\nThread ID: %s\nCorrelation ID: %s\nWaiting Reply: %t\nMemory Namespace: %s\nTool Allowlist: %v\nMax Retries: %d\nRetry Count: %d\nRetry Backoff(ms): %d\nTimeout(s): %d\nMax Task Chars: %d\nMax Result Chars: %d\nCreated: %d\nUpdated: %d\nSteering Count: %d\nTask: %s\nResult:\n%s",
				task.ID, task.Status, task.Label, task.AgentID, task.Role, task.SessionKey, task.ThreadID, task.CorrelationID, task.WaitingReply, task.MemoryNS,
				task.ToolAllowlist, task.MaxRetries, task.RetryCount, task.RetryBackoff, task.TimeoutSec, task.MaxTaskChars, task.MaxResultChars,
				task.Created, task.Updated, len(task.Steering), task.Task, task.Result)
			if events, err := t.manager.Events(task.ID, 6); err == nil && len(events) > 0 {
				var sb strings.Builder
				sb.WriteString(info)
				sb.WriteString("\nEvents:\n")
				for _, evt := range events {
					sb.WriteString(formatSubagentEventLog(evt) + "\n")
				}
				return strings.TrimSpace(sb.String()), nil
			}
			return info, nil
		},
		"kill": func() (string, error) {
			if strings.EqualFold(strings.TrimSpace(id), "all") {
				tasks := t.filterRecent(t.manager.ListTasks(), recentMinutes)
				if len(tasks) == 0 {
					return "No subagents.", nil
				}
				killed := 0
				for _, task := range tasks {
					if t.manager.KillTask(task.ID) {
						killed++
					}
				}
				return fmt.Sprintf("subagent kill requested for %d tasks", killed), nil
			}
			resolvedID, err := t.resolveTaskID(id)
			if err != nil {
				return err.Error(), nil
			}
			if !t.manager.KillTask(resolvedID) {
				return "subagent not found", nil
			}
			return "subagent kill requested", nil
		},
		"steer": func() (string, error) {
			if message == "" {
				return "message is required for steer", nil
			}
			resolvedID, err := t.resolveTaskID(id)
			if err != nil {
				return err.Error(), nil
			}
			if !t.manager.SteerTask(resolvedID, message) {
				return "subagent not found", nil
			}
			return "steering message accepted", nil
		},
		"send": func() (string, error) {
			if message == "" {
				return "message is required for send", nil
			}
			resolvedID, err := t.resolveTaskID(id)
			if err != nil {
				return err.Error(), nil
			}
			if !t.manager.SendTaskMessage(resolvedID, message) {
				return "subagent not found", nil
			}
			return "message sent", nil
		},
		"reply": func() (string, error) {
			if message == "" {
				return "message is required for reply", nil
			}
			resolvedID, err := t.resolveTaskID(id)
			if err != nil {
				return err.Error(), nil
			}
			if !t.manager.ReplyToTask(resolvedID, messageID, message) {
				return "subagent not found", nil
			}
			return "reply sent", nil
		},
		"ack": func() (string, error) {
			if messageID == "" {
				return "message_id is required for ack", nil
			}
			resolvedID, err := t.resolveTaskID(id)
			if err != nil {
				return err.Error(), nil
			}
			if !t.manager.AckTaskMessage(resolvedID, messageID) {
				return "subagent or message not found", nil
			}
			return "message acked", nil
		},
		"thread": func() (string, error) {
			if threadID == "" {
				resolvedID, err := t.resolveTaskID(id)
				if err != nil {
					return err.Error(), nil
				}
				task, ok := t.manager.GetTask(resolvedID)
				if !ok {
					return "subagent not found", nil
				}
				threadID = task.ThreadID
			}
			if threadID == "" {
				return "thread_id is required", nil
			}
			thread, ok := t.manager.Thread(threadID)
			if !ok {
				return "thread not found", nil
			}
			msgs, err := t.manager.ThreadMessages(threadID, limit)
			if err != nil {
				return "", err
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Thread: %s\nOwner: %s\nStatus: %s\nParticipants: %s\nTopic: %s\n",
				thread.ThreadID, thread.Owner, thread.Status, strings.Join(thread.Participants, ","), thread.Topic))
			if len(msgs) > 0 {
				sb.WriteString("Messages:\n")
				for _, msg := range msgs {
					sb.WriteString(fmt.Sprintf("- %s %s -> %s type=%s reply_to=%s status=%s\n  %s\n",
						msg.MessageID, msg.FromAgent, msg.ToAgent, msg.Type, msg.ReplyTo, msg.Status, msg.Content))
				}
			}
			return strings.TrimSpace(sb.String()), nil
		},
		"inbox": func() (string, error) {
			if agentID == "" {
				resolvedID, err := t.resolveTaskID(id)
				if err != nil {
					return err.Error(), nil
				}
				task, ok := t.manager.GetTask(resolvedID)
				if !ok {
					return "subagent not found", nil
				}
				agentID = task.AgentID
			}
			if agentID == "" {
				return "agent_id is required", nil
			}
			msgs, err := t.manager.Inbox(agentID, limit)
			if err != nil {
				return "", err
			}
			if len(msgs) == 0 {
				return "No inbox messages.", nil
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Inbox for %s:\n", agentID))
			for _, msg := range msgs {
				sb.WriteString(fmt.Sprintf("- %s thread=%s from=%s type=%s status=%s\n  %s\n",
					msg.MessageID, msg.ThreadID, msg.FromAgent, msg.Type, msg.Status, msg.Content))
			}
			return strings.TrimSpace(sb.String()), nil
		},
		"log": func() (string, error) {
			resolvedID, err := t.resolveTaskID(id)
			if err != nil {
				return err.Error(), nil
			}
			task, ok := t.manager.GetTask(resolvedID)
			if !ok {
				return "subagent not found", nil
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Subagent %s Log\n", task.ID))
			sb.WriteString(fmt.Sprintf("Status: %s\n", task.Status))
			sb.WriteString(fmt.Sprintf("Agent ID: %s\nRole: %s\nSession Key: %s\nThread ID: %s\nCorrelation ID: %s\nWaiting Reply: %t\nTool Allowlist: %v\nMax Retries: %d\nRetry Count: %d\nRetry Backoff(ms): %d\nTimeout(s): %d\n",
				task.AgentID, task.Role, task.SessionKey, task.ThreadID, task.CorrelationID, task.WaitingReply, task.ToolAllowlist, task.MaxRetries, task.RetryCount, task.RetryBackoff, task.TimeoutSec))
			if len(task.Steering) > 0 {
				sb.WriteString("Steering Messages:\n")
				for _, m := range task.Steering {
					sb.WriteString("- " + m + "\n")
				}
			}
			if events, err := t.manager.Events(task.ID, 20); err == nil && len(events) > 0 {
				sb.WriteString("Events:\n")
				for _, evt := range events {
					sb.WriteString(formatSubagentEventLog(evt) + "\n")
				}
			}
			if strings.TrimSpace(task.Result) != "" {
				result := strings.TrimSpace(task.Result)
				if len(result) > 500 {
					result = result[:500] + "..."
				}
				sb.WriteString("Result Preview:\n" + result)
			}
			return strings.TrimSpace(sb.String()), nil
		},
		"resume": func() (string, error) {
			resolvedID, err := t.resolveTaskID(id)
			if err != nil {
				return err.Error(), nil
			}
			label, ok := t.manager.ResumeTask(ctx, resolvedID)
			if !ok {
				return "subagent resume failed", nil
			}
			return fmt.Sprintf("subagent resumed as %s", label), nil
		},
	}
	threadHandler = handlers["thread"]
	handlers["trace"] = func() (string, error) { return threadHandler() }
	if handler := handlers[action]; handler != nil {
		return handler()
	}
	return "unsupported action", nil
}

func (t *SubagentsTool) resolveTaskID(idOrIndex string) (string, error) {
	idOrIndex = strings.TrimSpace(idOrIndex)
	if idOrIndex == "" {
		return "", fmt.Errorf("id is required")
	}
	if strings.HasPrefix(idOrIndex, "#") {
		n, err := strconv.Atoi(strings.TrimPrefix(idOrIndex, "#"))
		if err != nil || n <= 0 {
			return "", fmt.Errorf("invalid subagent index")
		}
		tasks := t.manager.ListTasks()
		if len(tasks) == 0 {
			return "", fmt.Errorf("no subagents")
		}
		sort.Slice(tasks, func(i, j int) bool { return tasks[i].Created > tasks[j].Created })
		if n > len(tasks) {
			return "", fmt.Errorf("subagent index out of range")
		}
		return tasks[n-1].ID, nil
	}
	return idOrIndex, nil
}

func (t *SubagentsTool) filterRecent(tasks []*SubagentTask, recentMinutes int) []*SubagentTask {
	if recentMinutes <= 0 {
		return tasks
	}
	cutoff := time.Now().Add(-time.Duration(recentMinutes) * time.Minute).UnixMilli()
	out := make([]*SubagentTask, 0, len(tasks))
	for _, task := range tasks {
		if task.Updated >= cutoff {
			out = append(out, task)
		}
	}
	return out
}
