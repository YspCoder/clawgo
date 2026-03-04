package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"clawgo/pkg/bus"
	"clawgo/pkg/ekg"
	"clawgo/pkg/providers"
)

func TestSplitPlannedSegments_Bullets(t *testing.T) {
	parts := splitPlannedSegments("- 修复 a.go\n- 补充 b.go 测试")
	if len(parts) != 2 {
		t.Fatalf("unexpected parts: %#v", parts)
	}
}

func TestPlanSessionTasks_Semicolon(t *testing.T) {
	loop := &AgentLoop{}
	tasks := loop.planSessionTasks(bus.InboundMessage{Channel: "cli", Content: "修复 pkg/a.go；修复 pkg/b.go"})
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %#v", tasks)
	}
	if tasks[0].Content == tasks[1].Content {
		t.Fatalf("expected distinct tasks: %#v", tasks)
	}
}

func TestProcessPlannedMessage_AggregatesResults(t *testing.T) {
	rp := &recordingProvider{responses: []providers.LLMResponse{
		{Content: "done-a", FinishReason: "stop"},
		{Content: "done-b", FinishReason: "stop"},
	}}
	loop := setupLoop(t, rp)

	resp, err := loop.processPlannedMessage(context.Background(), bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "u",
		ChatID:     "direct",
		SessionKey: "sess-plan",
		Content:    "修复 pkg/a.go；补充 pkg/b.go 测试",
	})
	if err != nil {
		t.Fatalf("processPlannedMessage error: %v", err)
	}
	if len(rp.calls) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(rp.calls))
	}
	if resp == "" {
		t.Fatalf("expected aggregate response")
	}
}

func TestFindRecentRelatedErrorEvent(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "workspace")
	_ = os.MkdirAll(filepath.Join(ws, "memory"), 0o755)
	line := `{"task_id":"t1","status":"error","log":"open /tmp/a.go failed","input_preview":"修复 pkg/a.go 的读取错误","source":"direct","channel":"cli"}`
	if err := os.WriteFile(filepath.Join(ws, "memory", "task-audit.jsonl"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write audit: %v", err)
	}
	loop := &AgentLoop{workspace: ws, ekg: ekg.New(ws)}
	loop.ekg.Record(ekg.Event{TaskID: "t1", Status: "error", Log: "open /tmp/a.go failed"})
	loop.ekg.Record(ekg.Event{TaskID: "t1", Status: "error", Log: "open /tmp/a.go failed"})
	loop.ekg.Record(ekg.Event{TaskID: "t1", Status: "error", Log: "open /tmp/a.go failed"})

	ev, ok := loop.findRecentRelatedErrorEvent("请修复 pkg/a.go 的读取问题")
	if !ok {
		t.Fatalf("expected matched recent error event")
	}
	if ev.TaskID != "t1" {
		t.Fatalf("unexpected task id: %s", ev.TaskID)
	}
	if hint := loop.ekgHintForTask(plannedTask{Content: "修复 pkg/a.go"}); hint == "" {
		t.Fatalf("expected non-empty ekg hint")
	}
}
