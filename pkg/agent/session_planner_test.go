package agent

import (
	"context"
	"testing"

	"clawgo/pkg/bus"
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
