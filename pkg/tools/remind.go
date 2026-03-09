package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/YspCoder/clawgo/pkg/cron"
)

type RemindTool struct {
	mu             sync.RWMutex
	cs             *cron.CronService
	defaultChannel string
	defaultChatID  string
}

func NewRemindTool(cs *cron.CronService) *RemindTool {
	return &RemindTool{cs: cs}
}

func (t *RemindTool) SetContext(channel, chatID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.defaultChannel = channel
	t.defaultChatID = chatID
}

func (t *RemindTool) Name() string {
	return "remind"
}

func (t *RemindTool) Description() string {
	return "Set a reminder for a future time"
}

func (t *RemindTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"message": map[string]interface{}{
				"type":        "string",
				"description": "The reminder message",
			},
			"time_expr": map[string]interface{}{
				"type":        "string",
				"description": "When to remind (e.g., '10m', '1h', '2026-02-12 10:00')",
			},
			"channel": map[string]interface{}{
				"type":        "string",
				"description": "Optional destination channel override",
			},
			"chat_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional destination chat ID override",
			},
		},
		"required": []string{"message", "time_expr"},
	}
}

func (t *RemindTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if t.cs == nil {
		return "", fmt.Errorf("cron service not available")
	}

	message, ok := args["message"].(string)
	if !ok {
		return "", fmt.Errorf("message is required")
	}

	timeExpr, ok := args["time_expr"].(string)
	if !ok {
		return "", fmt.Errorf("time_expr is required")
	}

	channel, _ := args["channel"].(string)
	chatID, _ := args["chat_id"].(string)
	if channel == "" || chatID == "" {
		t.mu.RLock()
		defaultChannel := t.defaultChannel
		defaultChatID := t.defaultChatID
		t.mu.RUnlock()
		if channel == "" {
			channel = defaultChannel
		}
		if chatID == "" {
			chatID = defaultChatID
		}
	}

	// Try duration first (e.g., "10m", "1h30m")
	if d, err := time.ParseDuration(timeExpr); err == nil {
		at := time.Now().Add(d).UnixMilli()
		schedule := cron.CronSchedule{
			Kind: "at",
			AtMS: &at,
		}
		job, err := t.cs.AddJob("Reminder", schedule, message, true, channel, chatID)
		if err != nil {
			return "", fmt.Errorf("failed to schedule reminder: %w", err)
		}
		return fmt.Sprintf("Reminder set for %s (in %s). Job ID: %s", time.UnixMilli(at).Format(time.RFC1123), d, job.ID), nil
	}

	// Try absolute date/time formats
	formats := []string{
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
		"15:04",
		"15:04:05",
	}

	var parsedTime time.Time
	var parseErr error
	parsed := false

	for _, layout := range formats {
		if t, err := time.ParseInLocation(layout, timeExpr, time.Local); err == nil {
			parsedTime = t
			parsed = true
			// If format was time-only, use today or tomorrow
			if layout == "15:04" || layout == "15:04:05" {
				now := time.Now()
				// Combine today's date with parsed time
				combined := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local)
				if combined.Before(now) {
					// If time passed today, assume tomorrow
					combined = combined.Add(24 * time.Hour)
				}
				parsedTime = combined
			}
			break
		} else {
			parseErr = err
		}
	}

	if !parsed {
		return "", fmt.Errorf("could not parse time expression '%s': %v", timeExpr, parseErr)
	}

	at := parsedTime.UnixMilli()
	schedule := cron.CronSchedule{
		Kind: "at",
		AtMS: &at,
	}

	job, err := t.cs.AddJob("Reminder", schedule, message, true, channel, chatID)
	if err != nil {
		return "", fmt.Errorf("failed to schedule reminder: %w", err)
	}

	return fmt.Sprintf("Reminder set for %s. Job ID: %s", parsedTime.Format(time.RFC1123), job.ID), nil
}
