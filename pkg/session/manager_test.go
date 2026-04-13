package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/jsonlog"
	"github.com/YspCoder/clawgo/pkg/providers"
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

func TestSessionManagerWritesSidecarsAndSearches(t *testing.T) {
	t.Parallel()

	storage := t.TempDir()
	sm := NewSessionManager(storage)
	key := "cli:default"

	sm.AddMessage(key, "user", "deploy project alpha")
	sm.AddMessage(key, "assistant", "deployment failed with timeout after contacting api gateway")

	for _, name := range []string{
		key + ".active.jsonl",
		key + ".meta.json",
		key + ".index.json",
	} {
		if _, err := os.Stat(filepath.Join(storage, name)); err != nil {
			t.Fatalf("expected artifact %s: %v", name, err)
		}
	}

	results := sm.Search("deploy timeout", nil, "", 5)
	if len(results) != 1 {
		t.Fatalf("expected one search result, got %#v", results)
	}
	if results[0].Key != key || len(results[0].Snippets) == 0 {
		t.Fatalf("unexpected search result: %#v", results[0])
	}
}

func TestSessionManagerRebuildsMissingSidecarsFromJSONL(t *testing.T) {
	t.Parallel()

	storage := t.TempDir()
	sm := NewSessionManager(storage)
	key := "cli:summary"

	sm.AddMessage(key, "user", "remember previous deploy steps")
	sm.SetSummary(key, "Key Facts\n- Previous deploy steps were discussed.")

	if err := os.Remove(filepath.Join(storage, key+".meta.json")); err != nil {
		t.Fatalf("remove meta: %v", err)
	}
	if err := os.Remove(filepath.Join(storage, key+".index.json")); err != nil {
		t.Fatalf("remove index: %v", err)
	}

	reloaded := NewSessionManager(storage)
	if got := reloaded.GetSummary(key); !strings.Contains(got, "Previous deploy steps") {
		t.Fatalf("expected summary recovered from fallback index, got %q", got)
	}
	results := reloaded.Search("deploy", nil, "", 5)
	if len(results) != 1 || results[0].Key != key {
		t.Fatalf("expected rebuilt search result, got %#v", results)
	}
}

func TestSessionManagerRollsOverActiveSegment(t *testing.T) {
	t.Setenv("CLAWGO_SESSION_SEGMENT_MAX_MESSAGES", "2")
	storage := t.TempDir()
	sm := NewSessionManager(storage)
	key := "cli:rollover"

	sm.AddMessage(key, "user", "one")
	sm.AddMessage(key, "assistant", "two")
	sm.AddMessage(key, "user", "three")

	if _, err := os.Stat(filepath.Join(storage, key+".0001.jsonl")); err != nil {
		t.Fatalf("expected archived segment: %v", err)
	}
	if _, err := os.Stat(filepath.Join(storage, key+".active.jsonl")); err != nil {
		t.Fatalf("expected new active segment: %v", err)
	}
	history := sm.GetHistory(key)
	if len(history) != 3 {
		t.Fatalf("expected full history across segments, got %d", len(history))
	}
}

func TestSessionManagerHistoryWindowAndIncrementalIndex(t *testing.T) {
	t.Setenv("CLAWGO_SESSION_SEGMENT_MAX_MESSAGES", "2")
	storage := t.TempDir()
	sm := NewSessionManager(storage)
	key := "cli:window"

	sm.AddMessage(key, "user", "one")
	sm.AddMessage(key, "assistant", "two")
	sm.AddMessage(key, "user", "three")
	sm.AddMessage(key, "assistant", "four")

	window := sm.GetHistoryWindow(key, 0, 0, 2, 2)
	if len(window) != 2 || window[0].Content != "three" || window[1].Content != "four" {
		t.Fatalf("unexpected history window: %#v", window)
	}

	var index sessionIndexFile
	if err := jsonlog.ReadJSON(filepath.Join(storage, key+".index.json"), &index); err != nil {
		t.Fatalf("read index: %v", err)
	}
	size, err := jsonlog.FileSize(filepath.Join(storage, key+".active.jsonl"))
	if err != nil {
		t.Fatalf("file size: %v", err)
	}
	if index.LastSeq != 4 {
		t.Fatalf("expected last seq 4, got %d", index.LastSeq)
	}
	if index.LastOffset != size {
		t.Fatalf("expected last offset %d, got %d", size, index.LastOffset)
	}
	if index.Segment != key+".active.jsonl" {
		t.Fatalf("unexpected index segment %q", index.Segment)
	}
}

func TestSessionManagerSearchSupportsChineseBigrams(t *testing.T) {
	t.Parallel()

	storage := t.TempDir()
	sm := NewSessionManager(storage)
	key := "cli:zh"

	sm.AddMessage(key, "user", "之前讨论过发布回滚方案")
	sm.AddMessage(key, "assistant", "回滚需要先确认数据库版本")

	results := sm.Search("回滚方案", nil, "", 5)
	if len(results) != 1 || results[0].Key != key {
		t.Fatalf("expected chinese query to hit sidecar index, got %#v", results)
	}

	if err := os.Remove(filepath.Join(storage, key+".index.json")); err != nil {
		t.Fatalf("remove index: %v", err)
	}
	reloaded := NewSessionManager(storage)
	results = reloaded.Search("回滚方案", nil, "", 5)
	if len(results) != 1 || results[0].Key != key {
		t.Fatalf("expected chinese query to hit scan fallback, got %#v", results)
	}
}

func TestApplyCompactionIfUnchangedRejectsChangedSession(t *testing.T) {
	t.Parallel()

	sm := NewSessionManager(t.TempDir())
	key := "cli:guard"
	sm.AddMessage(key, "user", "one")
	sm.AddMessage(key, "assistant", "two")
	snapshot := sm.CompactionSnapshot(key)
	sm.AddMessage(key, "user", "three")

	applied := sm.ApplyCompactionIfUnchanged(key, snapshot.NextSeq, snapshot.Summary, []providers.Message{{Role: "assistant", Content: "two"}}, "Key Facts\n- compacted")
	if applied {
		t.Fatal("expected stale compaction application to be rejected")
	}
	history := sm.GetPromptHistory(key)
	if len(history) != 3 || history[2].Content != "three" {
		t.Fatalf("expected newer message to remain, got %#v", history)
	}
}

func TestSessionManagerAppendDoesNotRewriteSessionsIndex(t *testing.T) {
	t.Parallel()

	storage := t.TempDir()
	sm := NewSessionManager(storage)
	key := "cli:index"

	sm.AddMessage(key, "user", "first")
	indexPath := filepath.Join(storage, "sessions.json")
	before, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read sessions index: %v", err)
	}
	statBefore, err := os.Stat(indexPath)
	if err != nil {
		t.Fatalf("stat sessions index: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	sm.AddMessage(key, "assistant", "second")

	after, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read sessions index after append: %v", err)
	}
	statAfter, err := os.Stat(indexPath)
	if err != nil {
		t.Fatalf("stat sessions index after append: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("expected sessions.json to stay unchanged for hot-path append")
	}
	if !statAfter.ModTime().Equal(statBefore.ModTime()) {
		t.Fatalf("expected sessions.json mtime to stay unchanged, before=%s after=%s", statBefore.ModTime(), statAfter.ModTime())
	}
}
