package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ReadFileTool struct{}

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
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of bytes to read",
			},
			"offset": map[string]interface{}{
				"type":        "integer",
				"description": "Byte offset to start reading from",
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

	limit := int64(0)
	if val, ok := args["limit"].(float64); ok {
		limit = int64(val)
	}

	offset := int64(0)
	if val, ok := args["offset"].(float64); ok {
		offset = int64(val)
	}

	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}

	if offset >= info.Size() {
		return "", nil // Offset beyond file size
	}

	if _, err := f.Seek(offset, 0); err != nil {
		return "", fmt.Errorf("failed to seek: %w", err)
	}

	// Default read all if limit is not set or 0
	readLimit := info.Size() - offset
	if limit > 0 && limit < readLimit {
		readLimit = limit
	}

	// Safety cap: don't read insanely large files into memory unless requested
	// But tool says "read file", so we respect limit.
	// If limit is 0 (unspecified), maybe we should default to a reasonable max?
	// The original code used os.ReadFile which reads ALL. So I should probably keep that behavior if limit is 0.
	// However, if limit is explicitly passed as 0, it might mean "read 0 bytes". But usually in JSON APIs 0 means default or none.
	// Let's assume limit > 0 means limit. If limit <= 0, read until EOF.

	var content []byte
	if limit > 0 {
		content = make([]byte, readLimit)
		n, err := io.ReadFull(f, content)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return "", fmt.Errorf("failed to read file: %w", err)
		}
		content = content[:n]
	} else {
		// Read until EOF
		content, err = io.ReadAll(f)
		if err != nil {
			return "", fmt.Errorf("failed to read file: %w", err)
		}
	}

	return string(content), nil
}

type WriteFileTool struct{}

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

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return "File written successfully", nil
}

type ListDirTool struct{}

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
		path = "."
	}

	recursive, _ := args["recursive"].(bool)

	var result strings.Builder

	if recursive {
		err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			relPath, err := filepath.Rel(path, p)
			if err != nil {
				relPath = p
			}
			if relPath == "." {
				return nil
			}
			if info.IsDir() {
				result.WriteString(fmt.Sprintf("DIR:  %s\n", relPath))
			} else {
				result.WriteString(fmt.Sprintf("FILE: %s\n", relPath))
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("failed to walk directory: %w", err)
		}
	} else {
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", fmt.Errorf("failed to read directory: %w", err)
		}

		// Sort entries: directories first, then files
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].IsDir() && !entries[j].IsDir() {
				return true
			}
			if !entries[i].IsDir() && entries[j].IsDir() {
				return false
			}
			return entries[i].Name() < entries[j].Name()
		})

		for _, entry := range entries {
			if entry.IsDir() {
				result.WriteString(fmt.Sprintf("DIR:  %s\n", entry.Name()))
			} else {
				result.WriteString(fmt.Sprintf("FILE: %s\n", entry.Name()))
			}
		}
	}

	return result.String(), nil
}
