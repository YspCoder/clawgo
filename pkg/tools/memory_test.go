package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemorySearchToolClampsMaxResults(t *testing.T) {
	workspace := t.TempDir()
	memDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	content := "# Long-term Memory\n\nalpha one\n\nalpha two\n"
	if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte(content), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	tool := NewMemorySearchTool(workspace)
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"query":      "alpha",
		"maxResults": -5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Found 1 memories") {
		t.Fatalf("expected clamped result count, got: %s", out)
	}
}

func TestMemorySearchToolScannerHandlesLargeLine(t *testing.T) {
	workspace := t.TempDir()
	memDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	large := strings.Repeat("x", 80*1024) + " needle"
	if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte(large), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	tool := NewMemorySearchTool(workspace)
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": "needle",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "needle") {
		t.Fatalf("expected search hit in large line, got: %s", out)
	}
}

func TestMemorySearchToolPrefersCanonicalMemoryPath(t *testing.T) {
	workspace := t.TempDir()
	memDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte("canonical"), 0644); err != nil {
		t.Fatalf("write canonical failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "MEMORY.md"), []byte("legacy"), 0644); err != nil {
		t.Fatalf("write legacy failed: %v", err)
	}

	tool := NewMemorySearchTool(workspace)
	files := tool.getMemoryFiles()
	for _, file := range files {
		if file == filepath.Join(workspace, "MEMORY.md") {
			t.Fatalf("legacy path should be ignored when canonical exists: %v", files)
		}
	}
}

func TestMemorySearchToolReportsFileScanWarnings(t *testing.T) {
	workspace := t.TempDir()
	memDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	tooLargeLine := strings.Repeat("x", 2*1024*1024) + "\n"
	if err := os.WriteFile(filepath.Join(memDir, "bad.md"), []byte(tooLargeLine), 0644); err != nil {
		t.Fatalf("write bad file failed: %v", err)
	}

	tool := NewMemorySearchTool(workspace)
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": "needle",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Warning: memory_search skipped") {
		t.Fatalf("expected warning suffix when scan errors happen, got: %s", out)
	}
}
