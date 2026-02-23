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
			"action":         map[string]interface{}{"type": "string", "description": "list|info|kill|steer|send|log|resume"},
			"id":             map[string]interface{}{"type": "string", "description": "subagent id/#index/all for info/kill/steer/send/log"},
			"message":        map[string]interface{}{"type": "string", "description": "steering message for steer/send action"},
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
	action, _ := args["action"].(string)
	action = strings.ToLower(strings.TrimSpace(action))
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	message, _ := args["message"].(string)
	message = strings.TrimSpace(message)
	recentMinutes := 0
	if v, ok := args["recent_minutes"].(float64); ok && int(v) > 0 {
		recentMinutes = int(v)
	}

	switch action {
	case "list":
		tasks := t.filterRecent(t.manager.ListTasks(), recentMinutes)
		if len(tasks) == 0 {
			return "No subagents.", nil
		}
		var sb strings.Builder
		sb.WriteString("Subagents:\n")
		sort.Slice(tasks, func(i, j int) bool { return tasks[i].Created > tasks[j].Created })
		for i, task := range tasks {
			sb.WriteString(fmt.Sprintf("- #%d %s [%s] label=%s\n", i+1, task.ID, task.Status, task.Label))
		}
		return strings.TrimSpace(sb.String()), nil
	case "info":
		if strings.EqualFold(strings.TrimSpace(id), "all") {
			tasks := t.filterRecent(t.manager.ListTasks(), recentMinutes)
			if len(tasks) == 0 {
				return "No subagents.", nil
			}
			sort.Slice(tasks, func(i, j int) bool { return tasks[i].Created > tasks[j].Created })
			var sb strings.Builder
			sb.WriteString("Subagents Summary:\n")
			for i, task := range tasks {
				sb.WriteString(fmt.Sprintf("- #%d %s [%s] label=%s steering=%d\n", i+1, task.ID, task.Status, task.Label, len(task.Steering)))
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
		return fmt.Sprintf("ID: %s\nStatus: %s\nLabel: %s\nCreated: %d\nUpdated: %d\nSteering Count: %d\nTask: %s\nResult:\n%s", task.ID, task.Status, task.Label, task.Created, task.Updated, len(task.Steering), task.Task, task.Result), nil
	case "kill":
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
	case "steer", "send":
		if message == "" {
			return "message is required for steer/send", nil
		}
		resolvedID, err := t.resolveTaskID(id)
		if err != nil {
			return err.Error(), nil
		}
		if !t.manager.SteerTask(resolvedID, message) {
			return "subagent not found", nil
		}
		return "steering message accepted", nil
	case "log":
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
		if len(task.Steering) > 0 {
			sb.WriteString("Steering Messages:\n")
			for _, m := range task.Steering {
				sb.WriteString("- " + m + "\n")
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
	case "resume":
		resolvedID, err := t.resolveTaskID(id)
		if err != nil {
			return err.Error(), nil
		}
		label, ok := t.manager.ResumeTask(ctx, resolvedID)
		if !ok {
			return "subagent resume failed", nil
		}
		return fmt.Sprintf("subagent resumed as %s", label), nil
	default:
		return "unsupported action", nil
	}
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
