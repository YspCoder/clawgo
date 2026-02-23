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
			"action":  map[string]interface{}{"type": "string", "description": "list|info|kill|steer|send|log"},
			"id":      map[string]interface{}{"type": "string", "description": "subagent id for info/kill/steer/send/log"},
			"message": map[string]interface{}{"type": "string", "description": "steering message for steer/send action"},
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
		return fmt.Sprintf("ID: %s\nStatus: %s\nLabel: %s\nCreated: %d\nUpdated: %d\nSteering Count: %d\nTask: %s\nResult:\n%s", task.ID, task.Status, task.Label, task.Created, task.Updated, len(task.Steering), task.Task, task.Result), nil
	case "kill":
		if id == "" {
			return "id is required for kill", nil
		}
		if !t.manager.KillTask(id) {
			return "subagent not found", nil
		}
		return "subagent kill requested", nil
	case "steer", "send":
		if id == "" || message == "" {
			return "id and message are required for steer/send", nil
		}
		if !t.manager.SteerTask(id, message) {
			return "subagent not found", nil
		}
		return "steering message accepted", nil
	case "log":
		if id == "" {
			return "id is required for log", nil
		}
		task, ok := t.manager.GetTask(id)
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
	default:
		return "unsupported action", nil
	}
}
