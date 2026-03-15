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
			"namespace": map[string]interface{}{
				"type":        "string",
				"description": "Optional memory namespace. Use main for workspace memory, or agent id for isolated memory.",
				"default":     "main",
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
	content := MapRawStringArg(args, "content")
	if content == "" {
		return "error: content is required", nil
	}
	namespace := parseMemoryNamespaceArg(args)
	baseDir := memoryNamespaceBaseDir(t.workspace, namespace)

	kind := strings.ToLower(MapStringArg(args, "kind"))
	if kind == "" {
		kind = "daily"
	}

	importance := normalizeImportance(MapStringArg(args, "importance"))

	source := MapStringArg(args, "source")
	if source == "" {
		source = "user"
	}

	tags := parseTags(args["tags"])

	appendMode := true
	if v, ok := MapBoolArg(args, "append"); ok {
		appendMode = v
	}

	formatted := formatMemoryLine(content, importance, source, tags)

	if (kind == "longterm" || kind == "memory" || kind == "permanent") && !allowLongTermWrite(importance, tags) {
		kind = "daily"
		formatted = formatMemoryLine(content, importance, source, append(tags, "downgraded:longterm_gate"))
	}

	switch kind {
	case "longterm", "memory", "permanent":
		path := filepath.Join(baseDir, "MEMORY.md")
		if appendMode {
			return t.appendWithTimestamp(path, formatted)
		}
		if err := os.WriteFile(path, []byte(formatted+"\n"), 0644); err != nil {
			return "", err
		}
		return fmt.Sprintf("Wrote long-term memory: %s (namespace=%s)", path, namespace), nil
	case "daily", "log", "today":
		memDir := filepath.Join(baseDir, "memory")
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
		return fmt.Sprintf("Wrote daily memory: %s (namespace=%s)", path, namespace), nil
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
	items := MapStringListArg(map[string]interface{}{"tags": raw}, "tags")
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, strings.ToLower(strings.TrimSpace(item)))
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

func allowLongTermWrite(importance string, tags []string) bool {
	if strings.ToLower(strings.TrimSpace(importance)) == "high" {
		return true
	}
	for _, t := range tags {
		s := strings.ToLower(strings.TrimSpace(t))
		switch s {
		case "preference", "decision", "rule", "policy", "identity":
			return true
		}
	}
	return false
}
