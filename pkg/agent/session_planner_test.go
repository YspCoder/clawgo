package agent

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
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

type probeProvider struct {
	mu            sync.Mutex
	inFlight      int
	maxInFlight   int
	delayPerCall  time.Duration
	responseCount int
}

func (p *probeProvider) Chat(_ context.Context, _ []providers.Message, _ []providers.ToolDefinition, _ string, _ map[string]interface{}) (*providers.LLMResponse, error) {
	p.mu.Lock()
	p.inFlight++
	if p.inFlight > p.maxInFlight {
		p.maxInFlight = p.inFlight
	}
	p.responseCount++
	p.mu.Unlock()

	time.Sleep(p.delayPerCall)

	p.mu.Lock()
	n := p.responseCount
	p.inFlight--
	p.mu.Unlock()
	resp := providers.LLMResponse{Content: "done-" + strconv.Itoa(n), FinishReason: "stop"}
	return &resp, nil
}

func (p *probeProvider) GetDefaultModel() string { return "test-model" }

func TestRunPlannedTasks_NonConflictingKeysCanRunInParallel(t *testing.T) {
	p := &probeProvider{delayPerCall: 100 * time.Millisecond}
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = filepath.Join(t.TempDir(), "workspace")
	cfg.Agents.Defaults.MaxToolIterations = 2
	cfg.Agents.Defaults.ContextCompaction.Enabled = false
	loop := NewAgentLoop(cfg, bus.NewMessageBus(), p, nil)

	_, err := loop.processPlannedMessage(context.Background(), bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "u",
		ChatID:     "direct",
		SessionKey: "sess-plan-parallel",
		Content:    "[resource_keys: file:pkg/a.go] 修复 a；[resource_keys: file:pkg/b.go] 修复 b",
	})
	if err != nil {
		t.Fatalf("processPlannedMessage error: %v", err)
	}
	if p.maxInFlight < 2 {
		t.Fatalf("expected parallel execution for non-conflicting keys, got maxInFlight=%d", p.maxInFlight)
	}
}

func TestRunPlannedTasks_ConflictingKeysMutuallyExclusive(t *testing.T) {
	p := &probeProvider{delayPerCall: 100 * time.Millisecond}
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = filepath.Join(t.TempDir(), "workspace")
	cfg.Agents.Defaults.MaxToolIterations = 2
	cfg.Agents.Defaults.ContextCompaction.Enabled = false
	loop := NewAgentLoop(cfg, bus.NewMessageBus(), p, nil)

	_, err := loop.processPlannedMessage(context.Background(), bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "u",
		ChatID:     "direct",
		SessionKey: "sess-plan-locked",
		Content:    "[resource_keys: file:pkg/a.go] 修复 a；[resource_keys: file:pkg/a.go] 补测试",
	})
	if err != nil {
		t.Fatalf("processPlannedMessage error: %v", err)
	}
	if p.maxInFlight != 1 {
		t.Fatalf("expected mutual exclusion for conflicting keys, got maxInFlight=%d", p.maxInFlight)
	}
}

func TestRunPlannedTasks_PublishesStepProgress(t *testing.T) {
	rp := &recordingProvider{responses: []providers.LLMResponse{
		{Content: "done-a", FinishReason: "stop"},
		{Content: "done-b", FinishReason: "stop"},
	}}
	loop := setupLoop(t, rp)

	_, err := loop.processPlannedMessage(context.Background(), bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "u",
		ChatID:     "direct",
		SessionKey: "sess-plan-progress",
		Content:    "修复 pkg/a.go；补充 pkg/b.go 测试",
	})
	if err != nil {
		t.Fatalf("processPlannedMessage error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out1, ok := loop.bus.SubscribeOutbound(ctx)
	if !ok {
		t.Fatalf("expected first progress outbound")
	}
	out2, ok := loop.bus.SubscribeOutbound(ctx)
	if !ok {
		t.Fatalf("expected second progress outbound")
	}
	all := out1.Content + "\n" + out2.Content
	if !strings.Contains(all, "进度 1/2") || !strings.Contains(all, "进度 2/2") {
		t.Fatalf("unexpected progress outputs:\n%s", all)
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
