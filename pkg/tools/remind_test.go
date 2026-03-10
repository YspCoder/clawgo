package tools

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/YspCoder/clawgo/pkg/cron"
)

func TestRemindTool_UsesToolContextForDeliveryTarget(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	cs := cron.NewCronService(storePath, nil)
	tool := NewRemindTool(cs)
	tool.SetContext("telegram", "chat-123")

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"message":   "鍠濇按",
		"time_expr": "10m",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	jobs := cs.ListJobs(true)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	if !jobs[0].Payload.Deliver {
		t.Fatalf("expected deliver=true")
	}
	if jobs[0].Payload.Channel != "telegram" {
		t.Fatalf("expected channel telegram, got %q", jobs[0].Payload.Channel)
	}
	if jobs[0].Payload.To != "chat-123" {
		t.Fatalf("expected to chat-123, got %q", jobs[0].Payload.To)
	}
}
