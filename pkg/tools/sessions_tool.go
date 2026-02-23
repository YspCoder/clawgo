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
	Key             string
	Kind            string
	Summary         string
	CompactionCount int
	UpdatedAt       time.Time
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
			"action":         map[string]interface{}{"type": "string", "description": "list|history"},
			"key":            map[string]interface{}{"type": "string", "description": "session key for history"},
			"limit":          map[string]interface{}{"type": "integer", "description": "max items", "default": 20},
			"active_minutes": map[string]interface{}{"type": "integer", "description": "only sessions updated in recent N minutes (list action)"},
			"kinds":          map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "optional session kinds filter for list"},
			"query":          map[string]interface{}{"type": "string", "description": "optional text query for list or history"},
			"include_tools":  map[string]interface{}{"type": "boolean", "description": "include tool role messages in history", "default": false},
			"from_me":        map[string]interface{}{"type": "boolean", "description": "history only: filter assistant messages when true, user messages when false"},
			"role":           map[string]interface{}{"type": "string", "description": "history only: filter by role, e.g. user|assistant|tool|system"},
			"around":         map[string]interface{}{"type": "integer", "description": "1-indexed message index center for history window"},
			"before":         map[string]interface{}{"type": "integer", "description": "1-indexed message index upper bound (exclusive)"},
			"after":          map[string]interface{}{"type": "integer", "description": "1-indexed message index lower bound (exclusive)"},
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
	includeTools := false
	if v, ok := args["include_tools"].(bool); ok {
		includeTools = v
	}
	around := 0
	if v, ok := args["around"].(float64); ok && int(v) > 0 {
		around = int(v)
	}
	before := 0
	if v, ok := args["before"].(float64); ok && int(v) > 0 {
		before = int(v)
	}
	after := 0
	if v, ok := args["after"].(float64); ok && int(v) > 0 {
		after = int(v)
	}
	activeMinutes := 0
	if v, ok := args["active_minutes"].(float64); ok && int(v) > 0 {
		activeMinutes = int(v)
	}
	query, _ := args["query"].(string)
	query = strings.ToLower(strings.TrimSpace(query))
	roleFilter, _ := args["role"].(string)
	roleFilter = strings.ToLower(strings.TrimSpace(roleFilter))
	fromMeSet := false
	fromMe := false
	if v, ok := args["from_me"].(bool); ok {
		fromMeSet = true
		fromMe = v
	}
	kindFilter := map[string]struct{}{}
	if rawKinds, ok := args["kinds"].([]interface{}); ok {
		for _, it := range rawKinds {
			if s, ok := it.(string); ok {
				s = strings.ToLower(strings.TrimSpace(s))
				if s != "" {
					kindFilter[s] = struct{}{}
				}
			}
		}
	}

	switch action {
	case "list":
		if t.listFn == nil {
			return "sessions list unavailable", nil
		}
		items := t.listFn(limit * 3)
		if len(items) == 0 {
			return "No sessions.", nil
		}
		if len(kindFilter) > 0 {
			filtered := make([]SessionInfo, 0, len(items))
			for _, s := range items {
				k := strings.ToLower(strings.TrimSpace(s.Kind))
				if _, ok := kindFilter[k]; ok {
					filtered = append(filtered, s)
				}
			}
			items = filtered
		}
		if activeMinutes > 0 {
			cutoff := time.Now().Add(-time.Duration(activeMinutes) * time.Minute)
			filtered := make([]SessionInfo, 0, len(items))
			for _, s := range items {
				if s.UpdatedAt.After(cutoff) {
					filtered = append(filtered, s)
				}
			}
			items = filtered
		}
		if query != "" {
			filtered := make([]SessionInfo, 0, len(items))
			for _, s := range items {
				blob := strings.ToLower(strings.TrimSpace(s.Key + "\n" + s.Kind + "\n" + s.Summary))
				if strings.Contains(blob, query) {
					filtered = append(filtered, s)
				}
			}
			items = filtered
		}
		if len(items) == 0 {
			return "No sessions (after filters).", nil
		}
		sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
		if len(items) > limit {
			items = items[:limit]
		}
		var sb strings.Builder
		sb.WriteString("Sessions:\n")
		for _, s := range items {
			sb.WriteString(fmt.Sprintf("- %s kind=%s compactions=%d updated=%s\n", s.Key, s.Kind, s.CompactionCount, s.UpdatedAt.Format(time.RFC3339)))
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
		raw := t.historyFn(key, 0)
		if len(raw) == 0 {
			return "No history.", nil
		}
		type indexedMsg struct {
			idx int
			msg providers.Message
		}
		window := make([]indexedMsg, 0, len(raw))
		for i, m := range raw {
			window = append(window, indexedMsg{idx: i + 1, msg: m})
		}

		// Window selectors are 1-indexed (human-friendly)
		if around > 0 {
			center := around - 1
			if center < 0 {
				center = 0
			}
			if center >= len(window) {
				center = len(window) - 1
			}
			half := limit / 2
			if half < 1 {
				half = 1
			}
			start := center - half
			if start < 0 {
				start = 0
			}
			end := center + half + 1
			if end > len(window) {
				end = len(window)
			}
			window = window[start:end]
		} else {
			start := 0
			end := len(window)
			if after > 0 {
				start = after
				if start > len(window) {
					start = len(window)
				}
			}
			if before > 0 {
				end = before - 1
				if end < 0 {
					end = 0
				}
				if end > len(window) {
					end = len(window)
				}
			}
			if start > end {
				start = end
			}
			window = window[start:end]
		}

		if !includeTools {
			filtered := make([]indexedMsg, 0, len(window))
			for _, m := range window {
				if strings.TrimSpace(strings.ToLower(m.msg.Role)) == "tool" {
					continue
				}
				filtered = append(filtered, m)
			}
			window = filtered
		}
		if roleFilter != "" {
			filtered := make([]indexedMsg, 0, len(window))
			for _, m := range window {
				if strings.ToLower(strings.TrimSpace(m.msg.Role)) == roleFilter {
					filtered = append(filtered, m)
				}
			}
			window = filtered
		}
		if fromMeSet {
			targetRole := "user"
			if fromMe {
				targetRole = "assistant"
			}
			filtered := make([]indexedMsg, 0, len(window))
			for _, m := range window {
				if strings.ToLower(strings.TrimSpace(m.msg.Role)) == targetRole {
					filtered = append(filtered, m)
				}
			}
			window = filtered
		}
		if query != "" {
			filtered := make([]indexedMsg, 0, len(window))
			for _, m := range window {
				blob := strings.ToLower(strings.TrimSpace(m.msg.Role + "\n" + m.msg.Content))
				if strings.Contains(blob, query) {
					filtered = append(filtered, m)
				}
			}
			window = filtered
		}
		if len(window) == 0 {
			return "No history (after filters).", nil
		}
		if len(window) > limit {
			window = window[len(window)-limit:]
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("History for %s:\n", key))
		for _, item := range window {
			content := strings.TrimSpace(item.msg.Content)
			if len(content) > 180 {
				content = content[:180] + "..."
			}
			sb.WriteString(fmt.Sprintf("- [#%d][%s] %s\n", item.idx, item.msg.Role, content))
		}
		return strings.TrimSpace(sb.String()), nil
	default:
		return "unsupported action", nil
	}
}
