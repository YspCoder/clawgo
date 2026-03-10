package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/YspCoder/clawgo/pkg/cron"
)

type CronTool struct {
	cs *cron.CronService
}

func NewCronTool(cs *cron.CronService) *CronTool {
	return &CronTool{cs: cs}
}

func (t *CronTool) Name() string { return "cron" }

func (t *CronTool) Description() string {
	return "Manage scheduled jobs. Actions: list|delete|enable|disable. Use this to inspect and remove your own timers."
}

func (t *CronTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string", "description": "list|delete|enable|disable"},
			"id":     map[string]interface{}{"type": "string", "description": "Job ID for delete/enable/disable"},
		},
	}
}

func (t *CronTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if t.cs == nil {
		return "Error: cron service not available", nil
	}
	action, _ := args["action"].(string)
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		action = "list"
	}
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)

	switch action {
	case "list":
		jobs := t.cs.ListJobs(true)
		b, _ := json.Marshal(jobs)
		return string(b), nil
	case "delete":
		if id == "" {
			return "", fmt.Errorf("%w: id for action=delete", ErrMissingField)
		}
		ok := t.cs.RemoveJob(id)
		if !ok {
			return fmt.Sprintf("job not found: %s", id), nil
		}
		return fmt.Sprintf("deleted job: %s", id), nil
	case "enable":
		if id == "" {
			return "", fmt.Errorf("%w: id for action=enable", ErrMissingField)
		}
		job := t.cs.EnableJob(id, true)
		if job == nil {
			return fmt.Sprintf("job not found: %s", id), nil
		}
		return fmt.Sprintf("enabled job: %s", id), nil
	case "disable":
		if id == "" {
			return "", fmt.Errorf("%w: id for action=disable", ErrMissingField)
		}
		job := t.cs.EnableJob(id, false)
		if job == nil {
			return fmt.Sprintf("job not found: %s", id), nil
		}
		return fmt.Sprintf("disabled job: %s", id), nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedAction, action)
	}
}
