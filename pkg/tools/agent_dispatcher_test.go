package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestAgentDispatcherDispatchAndWaitReply(t *testing.T) {
	workspace := t.TempDir()
	manager := NewAgentManager(nil, workspace, nil)
	manager.SetRunFunc(func(ctx context.Context, task *AgentTask) (string, error) {
		return "dispatch-result", nil
	})
	dispatcher := NewAgentDispatcher(manager)

	task, err := dispatcher.DispatchTask(context.Background(), AgentDispatchRequest{
		Task:          "implement feature",
		AgentID:       "coder",
		Origin:        &OriginRef{Channel: "cli", ChatID: "direct"},
	})
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if strings.TrimSpace(task.ID) == "" {
		t.Fatalf("expected task id on dispatched task")
	}

	reply, err := dispatcher.WaitReply(context.Background(), task.ID, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("wait reply failed: %v", err)
	}
	if reply.Status != "completed" || reply.Result != "dispatch-result" {
		t.Fatalf("unexpected reply: %+v", reply)
	}
}

func TestAgentDispatcherMergeResults(t *testing.T) {
	dispatcher := NewAgentDispatcher(nil)
	out := dispatcher.MergeResults([]*AgentDispatchReply{
		{TaskID: "agent-1", AgentID: "coder", Status: "completed", Result: "done"},
		{TaskID: "agent-2", AgentID: "tester", Status: "failed", Result: "boom"},
	})
	if !strings.Contains(out, "agent-1") || !strings.Contains(out, "agent=tester") {
		t.Fatalf("unexpected merged output: %s", out)
	}
}

func TestAgentDispatcherWaitReplyContextCancel(t *testing.T) {
	workspace := t.TempDir()
	manager := NewAgentManager(nil, workspace, nil)
	manager.SetRunFunc(func(ctx context.Context, task *AgentTask) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	dispatcher := NewAgentDispatcher(manager)

	task, err := dispatcher.DispatchTask(context.Background(), AgentDispatchRequest{
		Task:          "long task",
		AgentID:       "coder",
		Origin:        &OriginRef{Channel: "cli", ChatID: "direct"},
	})
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := dispatcher.WaitReply(waitCtx, task.ID, 20*time.Millisecond); err == nil {
		t.Fatalf("expected context cancellation error")
	}
}
