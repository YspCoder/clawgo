package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveToolPathRejectsAbsolutePath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	if _, err := resolveToolPath(base, "/tmp/outside.txt"); err == nil {
		t.Fatalf("expected absolute path to be rejected")
	}
}

func TestResolveToolPathRejectsTraversal(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	if _, err := resolveToolPath(base, "../outside.txt"); err == nil {
		t.Fatalf("expected traversal path to be rejected")
	}
}

func TestReadFileToolAllowsWorkspaceRelativePath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	path := filepath.Join(base, "notes.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	tool := NewReadFileTool(base)
	got, err := tool.Execute(context.Background(), map[string]interface{}{"path": "notes.txt"})
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if got != "hello" {
		t.Fatalf("unexpected content %q", got)
	}
}
