package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/YspCoder/clawgo/pkg/session"
)

type SessionSearchTool struct {
	manager *session.SessionManager
}

func NewSessionSearchTool(manager *session.SessionManager) *SessionSearchTool {
	return &SessionSearchTool{manager: manager}
}

func (t *SessionSearchTool) Name() string { return "session_search" }

func (t *SessionSearchTool) Description() string {
	return "Search past session history across JSONL session logs. Use when the user refers to previous conversations, past decisions, or earlier project work."
}

func (t *SessionSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Keywords or short phrase to search across past sessions.",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of matching sessions to return.",
				"default":     5,
			},
			"kinds": map[string]interface{}{
				"type":        "array",
				"description": "Optional session kinds filter, e.g. main, cron, subagent, hook.",
				"items":       map[string]interface{}{"type": "string"},
			},
			"exclude_current": map[string]interface{}{
				"type":        "boolean",
				"description": "Exclude the current session from results when session_key is provided.",
				"default":     true,
			},
			"session_key": map[string]interface{}{
				"type":        "string",
				"description": "Optional current session key for exclude_current behavior.",
			},
		},
		"required": []string{"query"},
	}
}

func (t *SessionSearchTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	_ = ctx
	if t == nil || t.manager == nil {
		return "", fmt.Errorf("session manager not configured")
	}
	query := MapStringArg(args, "query")
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	limit := MapIntArg(args, "limit", 5)
	kinds := MapStringListArg(args, "kinds")
	excludeCurrent, excludeSet := MapBoolArg(args, "exclude_current")
	excludeKey := ""
	if excludeSet && excludeCurrent {
		excludeKey = MapStringArg(args, "session_key")
	}

	results := t.manager.Search(query, kinds, excludeKey, limit)
	if len(results) == 0 {
		return fmt.Sprintf("No past sessions matched %q.", query), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Session search results for %q:\n\n", query))
	for _, item := range results {
		sb.WriteString(fmt.Sprintf("- %s kind=%s updated=%s score=%d\n", item.Key, item.Kind, item.UpdatedAt.Format("2006-01-02 15:04:05"), item.Score))
		if summary := strings.TrimSpace(item.Summary); summary != "" {
			sb.WriteString("  summary: " + singleLine(summary, 220) + "\n")
		}
		for _, snippet := range item.Snippets {
			line := singleLine(snippet.Content, 220)
			sb.WriteString(fmt.Sprintf("  [#%d][%s] %s\n", snippet.Seq, snippet.Role, line))
		}
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String()), nil
}

func (t *SessionSearchTool) ParallelSafe() bool { return true }

func singleLine(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if max > 0 && len(s) > max {
		return s[:max] + "..."
	}
	return s
}
