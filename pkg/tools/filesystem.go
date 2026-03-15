package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func resolveToolPath(baseDir, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("absolute path is not allowed")
	}
	joined := path
	if baseDir != "" {
		joined = filepath.Join(baseDir, path)
	}
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}
	if baseDir != "" {
		absBase, err := filepath.Abs(baseDir)
		if err != nil {
			return "", fmt.Errorf("failed to resolve base path: %w", err)
		}
		rel, err := filepath.Rel(absBase, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path escapes allowed directory")
		}
	}
	return abs, nil
}

// ReadFileTool reads the contents of a file.
type ReadFileTool struct {
	allowedDir string
}

func NewReadFileTool(allowedDir string) *ReadFileTool {
	return &ReadFileTool{allowedDir: allowedDir}
}

func (t *ReadFileTool) Name() string {
	return "read_file"
}

func (t *ReadFileTool) Description() string {
	return "Read the contents of a file"
}

func (t *ReadFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to read",
			},
			"offset": map[string]interface{}{
				"type":        "integer",
				"description": "Byte offset to start reading from",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of bytes to read",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	path := MapStringArg(args, "path")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	resolvedPath, err := resolveToolPath(t.allowedDir, path)
	if err != nil {
		return "", err
	}

	f, err := os.Open(resolvedPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return "", err
	}

	offset := int64(0)
	if o := MapIntArg(args, "offset", 0); o > 0 {
		offset = int64(o)
	}

	limit := int64(stat.Size())
	if l := MapIntArg(args, "limit", 0); l > 0 {
		limit = int64(l)
	}

	if offset >= stat.Size() {
		return "", fmt.Errorf("offset %d is beyond file size %d", offset, stat.Size())
	}

	data := make([]byte, limit)
	n, err := f.ReadAt(data, offset)
	if err != nil && err.Error() != "EOF" {
		return "", err
	}

	return string(data[:n]), nil
}

// WriteFileTool writes content to a file.
type WriteFileTool struct {
	allowedDir string
}

func NewWriteFileTool(allowedDir string) *WriteFileTool {
	return &WriteFileTool{allowedDir: allowedDir}
}

func (t *WriteFileTool) Name() string {
	return "write_file"
}

func (t *WriteFileTool) Description() string {
	return "Write content to a file. Supports overwrite (default) and append mode."
}

func (t *WriteFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to write",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Content to write to the file",
			},
			"append": map[string]interface{}{
				"type":        "boolean",
				"description": "If true, append content to the file instead of overwriting it",
				"default":     false,
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	path := MapStringArg(args, "path")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	if args == nil {
		return "", fmt.Errorf("content is required")
	}
	rawContent, ok := args["content"]
	if !ok {
		return "", fmt.Errorf("content is required")
	}
	content, ok := rawContent.(string)
	if !ok {
		return "", fmt.Errorf("content is required")
	}
	appendMode, _ := MapBoolArg(args, "append")

	resolvedPath, err := resolveToolPath(t.allowedDir, path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(resolvedPath), 0755); err != nil {
		return "", err
	}

	if appendMode {
		f, err := os.OpenFile(resolvedPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return "", err
		}
		defer f.Close()
		if _, err := f.WriteString(content); err != nil {
			return "", err
		}
		return fmt.Sprintf("File appended successfully: %s", path), nil
	}

	if err := os.WriteFile(resolvedPath, []byte(content), 0644); err != nil {
		return "", err
	}
	return fmt.Sprintf("File written successfully: %s", path), nil
}

// ListDirTool lists files and directories in a path.
type ListDirTool struct {
	allowedDir string
}

func NewListDirTool(allowedDir string) *ListDirTool {
	return &ListDirTool{allowedDir: allowedDir}
}

func (t *ListDirTool) Name() string {
	return "list_dir"
}

func (t *ListDirTool) Description() string {
	return "List files and directories in a path"
}

func (t *ListDirTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to list",
			},
			"recursive": map[string]interface{}{
				"type":        "boolean",
				"description": "List recursively",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ListDirTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	path := MapStringArg(args, "path")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	recursive, _ := MapBoolArg(args, "recursive")

	resolvedPath, err := resolveToolPath(t.allowedDir, path)
	if err != nil {
		return "", err
	}

	var results []string
	if recursive {
		err := filepath.Walk(resolvedPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(resolvedPath, path)
			if rel == "." {
				return nil
			}
			prefix := "FILE: "
			if info.IsDir() {
				prefix = "DIR:  "
			}
			results = append(results, prefix+rel)
			return nil
		})
		if err != nil {
			return "", err
		}
	} else {
		entries, err := os.ReadDir(resolvedPath)
		if err != nil {
			return "", err
		}
		for _, entry := range entries {
			prefix := "FILE: "
			if entry.IsDir() {
				prefix = "DIR:  "
			}
			results = append(results, prefix+entry.Name())
		}
	}

	if len(results) == 0 {
		return "(empty)", nil
	}

	return strings.Join(results, "\n"), nil
}

// EditFileTool edits a file by replacing old_text with new_text.
// The old_text must exist exactly in the file.
type EditFileTool struct {
	allowedDir string
}

func NewEditFileTool(allowedDir string) *EditFileTool {
	return &EditFileTool{allowedDir: allowedDir}
}

func (t *EditFileTool) Name() string {
	return "edit_file"
}

func (t *EditFileTool) Description() string {
	return "Edit a file by replacing old_text with new_text. The old_text must exist exactly in the file."
}

func (t *EditFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The file path to edit",
			},
			"old_text": map[string]interface{}{
				"type":        "string",
				"description": "The exact text to find and replace",
			},
			"new_text": map[string]interface{}{
				"type":        "string",
				"description": "The text to replace with",
			},
		},
		"required": []string{"path", "old_text", "new_text"},
	}
}

func (t *EditFileTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	path := MapStringArg(args, "path")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	rawOldText, ok := args["old_text"]
	if !ok {
		return "", fmt.Errorf("old_text is required")
	}
	oldText, ok := rawOldText.(string)
	if !ok || oldText == "" {
		return "", fmt.Errorf("old_text is required")
	}

	rawNewText, ok := args["new_text"]
	if !ok {
		return "", fmt.Errorf("new_text is required")
	}
	newText, ok := rawNewText.(string)
	if !ok {
		return "", fmt.Errorf("new_text is required")
	}

	resolvedPath, err := resolveToolPath(t.allowedDir, path)
	if err != nil {
		return "", err
	}

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", err
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, oldText) {
		return "", fmt.Errorf("old_text not found in file")
	}

	count := strings.Count(contentStr, oldText)
	if count > 1 {
		return "", fmt.Errorf("old_text appears %d times, please make it unique", count)
	}

	newContent := strings.Replace(contentStr, oldText, newText, 1)
	if err := os.WriteFile(resolvedPath, []byte(newContent), 0644); err != nil {
		return "", err
	}

	return fmt.Sprintf("Successfully edited %s", path), nil
}
