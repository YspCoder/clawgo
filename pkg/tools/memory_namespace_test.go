package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMemoryWriteToolNamespaceIsolation(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tool := NewMemoryWriteTool(workspace)

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"content":   "main note",
		"kind":      "daily",
		"namespace": "main",
	})
	if err != nil {
		t.Fatalf("main write failed: %v", err)
	}

	_, err = tool.Execute(context.Background(), map[string]interface{}{
		"content":   "coder note",
		"kind":      "daily",
		"namespace": "coder",
	})
	if err != nil {
		t.Fatalf("namespace write failed: %v", err)
	}

	today := time.Now().Format("2006-01-02") + ".md"
	mainPath := filepath.Join(workspace, "memory", today)
	coderPath := filepath.Join(workspace, "agents", "coder", "memory", today)

	mainData, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read main daily file failed: %v", err)
	}
	if !strings.Contains(string(mainData), "main note") {
		t.Fatalf("main daily memory missing expected content: %s", string(mainData))
	}

	coderData, err := os.ReadFile(coderPath)
	if err != nil {
		t.Fatalf("read namespaced daily file failed: %v", err)
	}
	if !strings.Contains(string(coderData), "coder note") {
		t.Fatalf("namespaced daily memory missing expected content: %s", string(coderData))
	}
}

func TestMemorySearchToolNamespaceIsolation(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	write := NewMemoryWriteTool(workspace)
	search := NewMemorySearchTool(workspace)

	_, _ = write.Execute(context.Background(), map[string]interface{}{
		"content":    "main_unique_keyword_123",
		"kind":       "longterm",
		"importance": "high",
		"namespace":  "main",
	})
	_, _ = write.Execute(context.Background(), map[string]interface{}{
		"content":    "coder_unique_keyword_456",
		"kind":       "longterm",
		"importance": "high",
		"namespace":  "coder",
	})

	mainRes, err := search.Execute(context.Background(), map[string]interface{}{
		"query":      "main_unique_keyword_123",
		"namespace":  "main",
		"maxResults": float64(3),
	})
	if err != nil {
		t.Fatalf("main namespace search failed: %v", err)
	}
	if !strings.Contains(mainRes, "main_unique_keyword_123") {
		t.Fatalf("expected main namespace result to include keyword, got: %s", mainRes)
	}

	coderRes, err := search.Execute(context.Background(), map[string]interface{}{
		"query":      "coder_unique_keyword_456",
		"namespace":  "coder",
		"maxResults": float64(3),
	})
	if err != nil {
		t.Fatalf("coder namespace search failed: %v", err)
	}
	if !strings.Contains(coderRes, "coder_unique_keyword_456") {
		t.Fatalf("expected coder namespace result to include keyword, got: %s", coderRes)
	}
	if strings.Contains(coderRes, "main_unique_keyword_123") {
		t.Fatalf("namespace isolation violated, coder search leaked main data: %s", coderRes)
	}
}
