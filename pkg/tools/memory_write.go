package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type MemoryWriteTool struct {
	workspace string
}

func NewMemoryWriteTool(workspace string) *MemoryWriteTool {
	return &MemoryWriteTool{workspace: workspace}
}

func (t *MemoryWriteTool) Name() string {
	return "memory_write"
}

func (t *MemoryWriteTool) Description() string {
	return "Write memory entries to long-term MEMORY.md or daily memory/YYYY-MM-DD.md. Use longterm for durable preferences/decisions, daily for raw logs."
}

func (t *MemoryWriteTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Memory text to write",
			},
			"kind": map[string]interface{}{
				"type":        "string",
				"description": "Target memory kind: longterm or daily",
				"default":     "daily",
			},
			"append": map[string]interface{}{
				"type":        "boolean",
				"description": "Append mode (default true)",
				"default":     true,
			},
		},
		"required": []string{"content"},
	}
}

func (t *MemoryWriteTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	content, _ := args["content"].(string)
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("content is required")
	}

	kind, _ := args["kind"].(string)
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = "daily"
	}

	appendMode := true
	if v, ok := args["append"].(bool); ok {
		appendMode = v
	}

	switch kind {
	case "longterm", "memory", "permanent":
		path := filepath.Join(t.workspace, "MEMORY.md")
		if appendMode {
			return t.appendWithTimestamp(path, content)
		}
		if err := os.WriteFile(path, []byte(content+"\n"), 0644); err != nil {
			return "", err
		}
		return fmt.Sprintf("Wrote long-term memory: %s", path), nil
	case "daily", "log", "today":
		memDir := filepath.Join(t.workspace, "memory")
		if err := os.MkdirAll(memDir, 0755); err != nil {
			return "", err
		}
		path := filepath.Join(memDir, time.Now().Format("2006-01-02")+".md")
		if appendMode {
			return t.appendWithTimestamp(path, content)
		}
		if err := os.WriteFile(path, []byte(content+"\n"), 0644); err != nil {
			return "", err
		}
		return fmt.Sprintf("Wrote daily memory: %s", path), nil
	default:
		return "", fmt.Errorf("invalid kind '%s', expected longterm or daily", kind)
	}
}

func (t *MemoryWriteTool) appendWithTimestamp(path, content string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	line := fmt.Sprintf("- [%s] %s\n", time.Now().Format("15:04"), content)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		return "", err
	}
	return fmt.Sprintf("Appended memory to %s", path), nil
}
