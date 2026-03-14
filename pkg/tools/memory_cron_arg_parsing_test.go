package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/cron"
)

func TestCronToolParsesStringArgs(t *testing.T) {
	t.Parallel()

	cs := cron.NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	at := time.Now().Add(time.Minute).UnixMilli()
	if _, err := cs.AddJob("demo", cron.CronSchedule{Kind: "at", AtMS: &at}, "hello", true, "telegram", "chat-1"); err != nil {
		t.Fatalf("add job failed: %v", err)
	}
	tool := NewCronTool(cs)
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "list",
	})
	if err != nil {
		t.Fatalf("cron execute failed: %v", err)
	}
	if !strings.Contains(out, "demo") {
		t.Fatalf("unexpected cron output: %s", out)
	}
}

func TestMemoryGetAndWriteParseStringArgs(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	write := NewMemoryWriteTool(workspace)
	get := NewMemoryGetTool(workspace)

	if _, err := write.Execute(context.Background(), map[string]interface{}{
		"content":    "remember this",
		"kind":       "longterm",
		"importance": "high",
		"source":     "user",
		"tags":       "preference,decision",
		"append":     "true",
	}); err != nil {
		t.Fatalf("memory write failed: %v", err)
	}

	out, err := get.Execute(context.Background(), map[string]interface{}{
		"path":  "MEMORY.md",
		"from":  "1",
		"lines": "5",
	})
	if err != nil {
		t.Fatalf("memory get failed: %v", err)
	}
	if !strings.Contains(out, "remember this") {
		t.Fatalf("unexpected memory get output: %s", out)
	}
}
