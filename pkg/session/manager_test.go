package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSessionsReturnsScannerErrorForOversizedLine(t *testing.T) {
	t.Parallel()

	storage := t.TempDir()
	line := `{"role":"user","content":"` + strings.Repeat("x", 2*1024*1024) + `"}`
	if err := os.WriteFile(filepath.Join(storage, "huge.jsonl"), []byte(line+"\n"), 0644); err != nil {
		t.Fatalf("write session file failed: %v", err)
	}

	sm := &SessionManager{
		sessions: map[string]*Session{},
		storage:  storage,
	}
	if err := sm.loadSessions(); err != nil {
		t.Fatalf("expected oversized line to load with expanded scanner buffer, got %v", err)
	}
}

func TestFromJSONLLineParsesOpenClawToolResult(t *testing.T) {
	t.Parallel()

	line := []byte(`{"type":"message","message":{"role":"toolResult","content":[{"type":"text","text":"done"}],"toolCallId":"call-1"}}`)
	msg, ok := fromJSONLLine(line)
	if !ok {
		t.Fatal("expected line to parse")
	}
	if msg.Role != "tool" || msg.ToolCallID != "call-1" || msg.Content != "done" {
		t.Fatalf("unexpected parsed message: %+v", msg)
	}
}

