package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func resolveToolPath(baseDir, path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	if baseDir != "" {
		return filepath.Clean(filepath.Join(baseDir, path)), nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
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
	path, ok := args["path"].(string)
	if !ok {
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
	if o, ok := args["offset"].(float64); ok {
		offset = int64(o)
	}

	limit := int64(stat.Size())
	if l, ok := args["limit"].(float64); ok {
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
	return "Write content to a file"
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
		},
		"required": []string{"path", "content"},
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok {
		return "", fmt.Errorf("path is required")
	}

	content, ok := args["content"].(string)
	if !ok {
		return "", fmt.Errorf("content is required")
	}

	resolvedPath, err := resolveToolPath(t.allowedDir, path)
	if err != nil {
		return "", err
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
	path, ok := args["path"].(string)
	if !ok {
		return "", fmt.Errorf("path is required")
	}

	recursive, _ := args["recursive"].(bool)

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
	path, ok := args["path"].(string)
	if !ok {
		return "", fmt.Errorf("path is required")
	}

	oldText, ok := args["old_text"].(string)
	if !ok {
		return "", fmt.Errorf("old_text is required")
	}

	newText, ok := args["new_text"].(string)
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
