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
	_ = ctx
	if t.cs == nil {
		return "Error: cron service not available", nil
	}
	action := strings.ToLower(MapStringArg(args, "action"))
	if action == "" {
		action = "list"
	}
	id := MapStringArg(args, "id")
	handlers := map[string]func() (string, error){
		"list": func() (string, error) {
			b, _ := json.Marshal(t.cs.ListJobs(true))
			return string(b), nil
		},
		"delete": func() (string, error) {
			if id == "" {
				return "", fmt.Errorf("%w: id for action=delete", ErrMissingField)
			}
			if !t.cs.RemoveJob(id) {
				return fmt.Sprintf("job not found: %s", id), nil
			}
			return fmt.Sprintf("deleted job: %s", id), nil
		},
		"enable": func() (string, error) {
			if id == "" {
				return "", fmt.Errorf("%w: id for action=enable", ErrMissingField)
			}
			if t.cs.EnableJob(id, true) == nil {
				return fmt.Sprintf("job not found: %s", id), nil
			}
			return fmt.Sprintf("enabled job: %s", id), nil
		},
		"disable": func() (string, error) {
			if id == "" {
				return "", fmt.Errorf("%w: id for action=disable", ErrMissingField)
			}
			if t.cs.EnableJob(id, false) == nil {
				return fmt.Sprintf("job not found: %s", id), nil
			}
			return fmt.Sprintf("disabled job: %s", id), nil
		},
	}
	if handler := handlers[action]; handler != nil {
		return handler()
	}
	return "", fmt.Errorf("%w: %s", ErrUnsupportedAction, action)
}
