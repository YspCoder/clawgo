package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"clawgo/pkg/providers"
)

func TestSessionIndexReadWriteOpenClawFormat(t *testing.T) {
	dir := t.TempDir()
	sm := NewSessionManager(dir)

	meta := SessionMeta{
		SessionID:   "sid-1",
		SessionFile: filepath.Join(dir, "sid-1.jsonl"),
		UpdatedAt:   1770962127556,
	}
	if err := sm.saveSessionMeta("channel:chat", meta); err != nil {
		t.Fatalf("saveSessionMeta failed: %v", err)
	}

	indexPath := filepath.Join(dir, sessionsIndexFile)
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("failed to read sessions index: %v", err)
	}

	raw := map[string]map[string]interface{}{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to parse sessions index: %v", err)
	}
	entry, ok := raw["channel:chat"]
	if !ok {
		t.Fatalf("sessions index missing key")
	}

	if _, ok := entry["sessionId"].(string); !ok {
		t.Fatalf("sessionId should be string, got %T", entry["sessionId"])
	}
	if _, ok := entry["sessionFile"].(string); !ok {
		t.Fatalf("sessionFile should be string, got %T", entry["sessionFile"])
	}
	if _, ok := entry["updatedAt"].(float64); !ok {
		t.Fatalf("updatedAt should be number(ms), got %T", entry["updatedAt"])
	}
}

func TestAppendAndLoadSessionHistoryOpenClawJSONL(t *testing.T) {
	dir := t.TempDir()
	sm := NewSessionManager(dir)

	sessionKey := "telegram:chat-1"
	sm.AddMessage(sessionKey, "user", "hello")
	sm.AddMessage(sessionKey, "assistant", "world")

	meta, ok := sm.getSession(sessionKey)
	if !ok {
		t.Fatalf("expected session meta for key")
	}

	f, err := os.Open(meta.SessionFile)
	if err != nil {
		t.Fatalf("failed to open session file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lines := make([]string, 0)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines(session + 2 messages), got %d", len(lines))
	}

	var first map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("invalid first event json: %v", err)
	}
	if first["type"] != "session" {
		t.Fatalf("first event should be session, got %v", first["type"])
	}

	all, err := sm.loadSessionHistory(meta.SessionID, 0)
	if err != nil {
		t.Fatalf("loadSessionHistory failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("unexpected history len: got %d want 2", len(all))
	}
	if all[0].Content != "hello" || all[1].Content != "world" {
		t.Fatalf("unexpected loaded content: %+v", all)
	}

	limited, err := sm.loadSessionHistory(meta.SessionID, 1)
	if err != nil {
		t.Fatalf("loadSessionHistory(limit) failed: %v", err)
	}
	if len(limited) != 1 || limited[0].Content != "world" {
		t.Fatalf("unexpected limited history: %+v", limited)
	}
}

func TestMultipleEventsUnderSameSessionKeyOpenClaw(t *testing.T) {
	dir := t.TempDir()
	sm := NewSessionManager(dir)

	sessionKey := "discord:room-1"
	events := []providers.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
		{Role: "user", Content: "third"},
	}
	for _, e := range events {
		sm.AddMessageFull(sessionKey, e)
	}

	meta, ok := sm.getSession(sessionKey)
	if !ok {
		t.Fatalf("expected session meta")
	}

	history, err := sm.loadSessionHistory(meta.SessionID, 0)
	if err != nil {
		t.Fatalf("loadSessionHistory failed: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("unexpected history len: got %d want 3", len(history))
	}
	if history[2].Content != "third" {
		t.Fatalf("expected latest content third, got %q", history[2].Content)
	}

	sm2 := NewSessionManager(dir)
	reloaded := sm2.GetHistory(sessionKey)
	if len(reloaded) != 3 {
		t.Fatalf("unexpected reloaded history len: got %d want 3", len(reloaded))
	}
}
