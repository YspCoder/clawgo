package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type MemoryGetTool struct {
	workspace string
}

func NewMemoryGetTool(workspace string) *MemoryGetTool {
	return &MemoryGetTool{workspace: workspace}
}

func (t *MemoryGetTool) Name() string {
	return "memory_get"
}

func (t *MemoryGetTool) Description() string {
	return "Read safe snippets from MEMORY.md or memory/*.md using optional line range."
}

func (t *MemoryGetTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Relative path to MEMORY.md or memory/*.md",
			},
			"namespace": map[string]interface{}{
				"type":        "string",
				"description": "Optional memory namespace. Use main for workspace memory, or subagent id for isolated memory.",
				"default":     "main",
			},
			"from": map[string]interface{}{
				"type":        "integer",
				"description": "Start line (1-indexed)",
				"default":     1,
			},
			"lines": map[string]interface{}{
				"type":        "integer",
				"description": "Number of lines to read",
				"default":     80,
			},
		},
		"required": []string{"path"},
	}
}

func (t *MemoryGetTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	rawPath := MapStringArg(args, "path")
	if rawPath == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(rawPath) {
		return "", fmt.Errorf("absolute path is not allowed")
	}
	namespace := parseMemoryNamespaceArg(args)

	from := MapIntArg(args, "from", 1)
	lines := MapIntArg(args, "lines", 80)
	if lines > 500 {
		lines = 500
	}

	baseDir := memoryNamespaceBaseDir(t.workspace, namespace)
	fullPath := filepath.Clean(filepath.Join(baseDir, rawPath))
	if !t.isAllowedMemoryPath(fullPath, namespace) {
		return "", fmt.Errorf("path not allowed: %s", rawPath)
	}

	f, err := os.Open(fullPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNo := 0
	end := from + lines - 1
	var out strings.Builder
	for scanner.Scan() {
		lineNo++
		if lineNo < from {
			continue
		}
		if lineNo > end {
			break
		}
		out.WriteString(fmt.Sprintf("%d: %s\n", lineNo, scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	content := out.String()
	if content == "" {
		return fmt.Sprintf("No content in range for %s (from=%d, lines=%d)", rawPath, from, lines), nil
	}

	rel, err := filepath.Rel(t.workspace, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = fullPath
	}
	return fmt.Sprintf("Source: %s#L%d-L%d\n%s", rel, from, end, content), nil
}

func (t *MemoryGetTool) isAllowedMemoryPath(fullPath, namespace string) bool {
	baseDir := memoryNamespaceBaseDir(t.workspace, namespace)
	workspaceMemory := filepath.Join(baseDir, "MEMORY.md")
	if fullPath == workspaceMemory {
		return true
	}

	memoryDir := filepath.Join(baseDir, "memory")
	rel, err := filepath.Rel(memoryDir, fullPath)
	if err != nil {
		return false
	}
	if strings.HasPrefix(rel, "..") {
		return false
	}
	return strings.HasSuffix(strings.ToLower(fullPath), ".md")
}
