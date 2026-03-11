package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/bus"
)

func TestAppendTaskAuditEventPersistsContextCharStats(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	al := &AgentLoop{workspace: workspace}
	msg := bus.InboundMessage{
		Channel:    "chat",
		SessionKey: "s1",
		Content:    "Task Context:\nEKG: repeat_errsig=perm\nTask:\ndeploy",
		Metadata: map[string]string{
			"context_extra_chars":  "42",
			"context_ekg_chars":    "18",
			"context_memory_chars": "0",
		},
	}

	al.appendTaskAuditEvent("task-1", msg, "success", time.Now().Add(-time.Second), 1000, "completed", false)

	b, err := os.ReadFile(filepath.Join(workspace, "memory", "task-audit.jsonl"))
	if err != nil {
		t.Fatalf("read task audit: %v", err)
	}
	var row map[string]interface{}
	if err := json.Unmarshal(b[:len(b)-1], &row); err != nil {
		t.Fatalf("decode task audit row: %v", err)
	}
	if got := int(row["context_extra_chars"].(float64)); got != 42 {
		t.Fatalf("expected context_extra_chars=42, got %d", got)
	}
	if got := int(row["context_ekg_chars"].(float64)); got != 18 {
		t.Fatalf("expected context_ekg_chars=18, got %d", got)
	}
	if got := int(row["context_memory_chars"].(float64)); got != 0 {
		t.Fatalf("expected context_memory_chars=0, got %d", got)
	}
}
