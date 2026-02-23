package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"clawgo/pkg/providers"
)

type SessionInfo struct {
	Key       string
	Kind      string
	Summary   string
	UpdatedAt time.Time
}

type SessionsTool struct {
	listFn    func(limit int) []SessionInfo
	historyFn func(key string, limit int) []providers.Message
}

func NewSessionsTool(listFn func(limit int) []SessionInfo, historyFn func(key string, limit int) []providers.Message) *SessionsTool {
	return &SessionsTool{listFn: listFn, historyFn: historyFn}
}

func (t *SessionsTool) Name() string { return "sessions" }

func (t *SessionsTool) Description() string {
	return "Inspect sessions in current runtime: list or history"
}

func (t *SessionsTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string", "description": "list|history"},
			"key":    map[string]interface{}{"type": "string", "description": "session key for history"},
			"limit":  map[string]interface{}{"type": "integer", "description": "max items", "default": 20},
		},
		"required": []string{"action"},
	}
}

func (t *SessionsTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	_ = ctx
	action, _ := args["action"].(string)
	action = strings.ToLower(strings.TrimSpace(action))
	limit := 20
	if v, ok := args["limit"].(float64); ok && int(v) > 0 {
		limit = int(v)
	}

	switch action {
	case "list":
		if t.listFn == nil {
			return "sessions list unavailable", nil
		}
		items := t.listFn(limit)
		if len(items) == 0 {
			return "No sessions.", nil
		}
		sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
		var sb strings.Builder
		sb.WriteString("Sessions:\n")
		for _, s := range items {
			sb.WriteString(fmt.Sprintf("- %s kind=%s updated=%s\n", s.Key, s.Kind, s.UpdatedAt.Format(time.RFC3339)))
		}
		return strings.TrimSpace(sb.String()), nil
	case "history":
		if t.historyFn == nil {
			return "sessions history unavailable", nil
		}
		key, _ := args["key"].(string)
		key = strings.TrimSpace(key)
		if key == "" {
			return "key is required for history", nil
		}
		h := t.historyFn(key, limit)
		if len(h) == 0 {
			return "No history.", nil
		}
		if len(h) > limit {
			h = h[len(h)-limit:]
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("History for %s:\n", key))
		for _, m := range h {
			content := strings.TrimSpace(m.Content)
			if len(content) > 180 {
				content = content[:180] + "..."
			}
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", m.Role, content))
		}
		return strings.TrimSpace(sb.String()), nil
	default:
		return "unsupported action", nil
	}
}
