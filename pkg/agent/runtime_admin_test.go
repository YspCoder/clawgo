package agent

import (
	"context"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/tools"
)

func TestHandleSubagentRuntimeDispatchAndWait(t *testing.T) {
	workspace := t.TempDir()
	manager := tools.NewSubagentManager(nil, workspace, nil)
	manager.SetRunFunc(func(ctx context.Context, task *tools.SubagentTask) (string, error) {
		return "runtime-admin-result", nil
	})
	loop := &AgentLoop{
		subagentManager: manager,
		subagentRouter:  tools.NewSubagentRouter(manager),
	}

	out, err := loop.HandleSubagentRuntime(context.Background(), "dispatch_and_wait", map[string]interface{}{
		"task":             "implement runtime action",
		"agent_id":         "coder",
		"channel":          "webui",
		"chat_id":          "webui",
		"wait_timeout_sec": float64(5),
	})
	if err != nil {
		t.Fatalf("dispatch_and_wait failed: %v", err)
	}
	payload, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected payload type: %T", out)
	}
	reply, ok := payload["reply"].(*tools.RouterReply)
	if !ok {
		t.Fatalf("expected router reply, got %T", payload["reply"])
	}
	if reply.Status != "completed" || reply.Result != "runtime-admin-result" {
		t.Fatalf("unexpected reply: %+v", reply)
	}
	merged, _ := payload["merged"].(string)
	if merged == "" {
		t.Fatalf("expected merged output")
	}
	time.Sleep(20 * time.Millisecond)
}

func TestHandleSubagentRuntimeStream(t *testing.T) {
	workspace := t.TempDir()
	manager := tools.NewSubagentManager(nil, workspace, nil)
	manager.SetRunFunc(func(ctx context.Context, task *tools.SubagentTask) (string, error) {
		return "stream-result", nil
	})
	loop := &AgentLoop{
		workspace:       workspace,
		subagentManager: manager,
		subagentRouter:  tools.NewSubagentRouter(manager),
	}

	out, err := loop.HandleSubagentRuntime(context.Background(), "spawn", map[string]interface{}{
		"task":     "prepare streamable task",
		"agent_id": "coder",
		"channel":  "webui",
		"chat_id":  "webui",
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}
	if _, ok := out.(map[string]interface{}); !ok {
		t.Fatalf("unexpected spawn payload: %T", out)
	}
	var task *tools.SubagentTask
	for i := 0; i < 50; i++ {
		tasks := manager.ListTasks()
		if len(tasks) > 0 && tasks[0].Status == "completed" {
			task = tasks[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if task == nil {
		t.Fatalf("expected completed task")
	}

	out, err = loop.HandleSubagentRuntime(context.Background(), "stream", map[string]interface{}{
		"id": task.ID,
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}
	streamPayload, ok := out.(map[string]interface{})
	if !ok || streamPayload["found"] != true {
		t.Fatalf("unexpected stream payload: %#v", out)
	}
	items, ok := streamPayload["items"].([]map[string]interface{})
	if !ok || len(items) == 0 {
		t.Fatalf("expected merged stream items, got %#v", streamPayload["items"])
	}
	foundEvent := false
	foundMessage := false
	for _, item := range items {
		switch item["kind"] {
		case "event":
			foundEvent = true
		case "message":
			foundMessage = true
		}
	}
	if !foundEvent || !foundMessage {
		t.Fatalf("expected merged event and message items, got %#v", items)
	}
}
