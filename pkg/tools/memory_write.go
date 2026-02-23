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
			"importance": map[string]interface{}{
				"type":        "string",
				"description": "low|medium|high. high is recommended for longterm",
				"default":     "medium",
			},
			"source": map[string]interface{}{
				"type":        "string",
				"description": "Source/context for this memory, e.g. user, system, tool",
				"default":     "user",
			},
			"tags": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Optional tags for filtering/search, e.g. preference,todo,decision",
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

	importance, _ := args["importance"].(string)
	importance = normalizeImportance(importance)

	source, _ := args["source"].(string)
	source = strings.TrimSpace(source)
	if source == "" {
		source = "user"
	}

	tags := parseTags(args["tags"])

	appendMode := true
	if v, ok := args["append"].(bool); ok {
		appendMode = v
	}

	formatted := formatMemoryLine(content, importance, source, tags)

	switch kind {
	case "longterm", "memory", "permanent":
		path := filepath.Join(t.workspace, "MEMORY.md")
		if appendMode {
			return t.appendWithTimestamp(path, formatted)
		}
		if err := os.WriteFile(path, []byte(formatted+"\n"), 0644); err != nil {
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
			return t.appendWithTimestamp(path, formatted)
		}
		if err := os.WriteFile(path, []byte(formatted+"\n"), 0644); err != nil {
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

func normalizeImportance(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	switch s {
	case "high", "medium", "low":
		return s
	default:
		return "medium"
	}
}

func parseTags(raw interface{}) []string {
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, it := range items {
		s, _ := it.(string)
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if _, exists := seen[s]; exists {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func formatMemoryLine(content, importance, source string, tags []string) string {
	var meta []string
	meta = append(meta, "importance="+importance)
	meta = append(meta, "source="+source)
	if len(tags) > 0 {
		meta = append(meta, "tags="+strings.Join(tags, ","))
	}
	return fmt.Sprintf("[%s] %s", strings.Join(meta, " | "), content)
}
