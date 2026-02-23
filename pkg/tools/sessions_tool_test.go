package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"clawgo/pkg/providers"
)

func TestSessionsToolListWithKindsAndQuery(t *testing.T) {
	tool := NewSessionsTool(func(limit int) []SessionInfo {
		return []SessionInfo{
			{Key: "telegram:1", Kind: "main", Summary: "project alpha", UpdatedAt: time.Now()},
			{Key: "cron:1", Kind: "cron", Summary: "nightly sync", UpdatedAt: time.Now()},
		}
	}, nil, "", "")

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "list",
		"kinds":  []interface{}{"main"},
		"query":  "alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "telegram:1") || strings.Contains(out, "cron:1") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestSessionsToolHistoryWithoutTools(t *testing.T) {
	tool := NewSessionsTool(nil, func(key string, limit int) []providers.Message {
		return []providers.Message{
			{Role: "user", Content: "hello"},
			{Role: "tool", Content: "tool output"},
			{Role: "assistant", Content: "ok"},
		}
	}, "", "")

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "history",
		"key":    "telegram:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(out), "tool output") {
		t.Fatalf("tool message should be filtered: %s", out)
	}
}

func TestSessionsToolHistoryFromMe(t *testing.T) {
	tool := NewSessionsTool(nil, func(key string, limit int) []providers.Message {
		return []providers.Message{
			{Role: "user", Content: "u1"},
			{Role: "assistant", Content: "a1"},
			{Role: "assistant", Content: "a2"},
		}
	}, "", "")

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":  "history",
		"key":     "telegram:1",
		"from_me": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "u1") || !strings.Contains(out, "a1") {
		t.Fatalf("unexpected filtered output: %s", out)
	}
}
