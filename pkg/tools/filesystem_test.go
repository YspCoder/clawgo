package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadFileToolResolvesRelativePathFromAllowedDir(t *testing.T) {
	workspace := t.TempDir()
	targetPath := filepath.Join(workspace, "cmd", "clawgo", "main.go")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte("package main"), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	tool := NewReadFileTool(workspace)
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": "cmd/clawgo/main.go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "package main" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestReadFileToolAllowsParentTraversalWhenPermitted(t *testing.T) {
	workspace := t.TempDir()
	parentFile := filepath.Join(filepath.Dir(workspace), "outside.txt")
	if err := os.WriteFile(parentFile, []byte("outside"), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	tool := NewReadFileTool(workspace)
	relPath, err := filepath.Rel(workspace, parentFile)
	if err != nil {
		t.Fatalf("rel failed: %v", err)
	}

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": relPath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "outside" {
		t.Fatalf("unexpected output: %q", out)
	}
}
