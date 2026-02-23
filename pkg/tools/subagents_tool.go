package tools

import (
	"context"
	"fmt"
	"strings"
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
			"action":  map[string]interface{}{"type": "string", "description": "list|info|kill|steer"},
			"id":      map[string]interface{}{"type": "string", "description": "subagent id for info/kill/steer"},
			"message": map[string]interface{}{"type": "string", "description": "steering message for steer action"},
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

	switch action {
	case "list":
		tasks := t.manager.ListTasks()
		if len(tasks) == 0 {
			return "No subagents.", nil
		}
		var sb strings.Builder
		sb.WriteString("Subagents:\n")
		for _, task := range tasks {
			sb.WriteString(fmt.Sprintf("- %s [%s] label=%s\n", task.ID, task.Status, task.Label))
		}
		return strings.TrimSpace(sb.String()), nil
	case "info":
		if id == "" {
			return "id is required for info", nil
		}
		task, ok := t.manager.GetTask(id)
		if !ok {
			return "subagent not found", nil
		}
		return fmt.Sprintf("ID: %s\nStatus: %s\nLabel: %s\nTask: %s\nResult:\n%s", task.ID, task.Status, task.Label, task.Task, task.Result), nil
	case "kill":
		if id == "" {
			return "id is required for kill", nil
		}
		if !t.manager.KillTask(id) {
			return "subagent not found", nil
		}
		return "subagent kill requested", nil
	case "steer":
		if id == "" || message == "" {
			return "id and message are required for steer", nil
		}
		if !t.manager.SteerTask(id, message) {
			return "subagent not found", nil
		}
		return "steering message accepted", nil
	default:
		return "unsupported action", nil
	}
}
