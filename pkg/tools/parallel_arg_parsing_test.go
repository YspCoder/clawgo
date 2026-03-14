package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stubFetchTool struct{}

func (s *stubFetchTool) Name() string        { return "web_fetch" }
func (s *stubFetchTool) Description() string { return "stub" }
func (s *stubFetchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{}
}
func (s *stubFetchTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	return "fetched:" + MapStringArg(args, "url"), nil
}
func (s *stubFetchTool) ParallelSafe() bool { return true }

func TestMemorySearchToolParsesStringMaxResults(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	write := NewMemoryWriteTool(workspace)
	if _, err := write.Execute(context.Background(), map[string]interface{}{
		"content":    "alpha beta gamma",
		"kind":       "longterm",
		"importance": "high",
	}); err != nil {
		t.Fatalf("memory write failed: %v", err)
	}

	search := NewMemorySearchTool(workspace)
	out, err := search.Execute(context.Background(), map[string]interface{}{
		"query":      "alpha",
		"maxResults": "1",
	})
	if err != nil {
		t.Fatalf("memory search failed: %v", err)
	}
	if !strings.Contains(out, "alpha beta gamma") {
		t.Fatalf("unexpected search output: %s", out)
	}
}

func TestParallelToolParsesStringSlices(t *testing.T) {
	t.Parallel()

	reg := NewToolRegistry()
	reg.Register(&stubFetchTool{})
	tool := NewParallelTool(reg, 2, map[string]struct{}{"web_fetch": {}})

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"calls": []map[string]interface{}{
			{"tool": "web_fetch", "arguments": map[string]interface{}{"url": "https://example.com"}, "id": "first"},
		},
	})
	if err != nil {
		t.Fatalf("parallel execute failed: %v", err)
	}
	if !strings.Contains(out, "first") || !strings.Contains(out, "https://example.com") {
		t.Fatalf("unexpected parallel output: %s", out)
	}
}

func TestParallelFetchToolParsesStringURLsSlice(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("parallel fetch ok"))
	}))
	defer srv.Close()

	tool := NewParallelFetchTool(NewWebFetchTool(100), 2, map[string]struct{}{"web_fetch": {}})
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"urls": []string{srv.URL},
	})
	if err != nil {
		t.Fatalf("parallel_fetch execute failed: %v", err)
	}
	if !strings.Contains(out, "parallel fetch ok") {
		t.Fatalf("unexpected parallel_fetch output: %s", out)
	}
}
