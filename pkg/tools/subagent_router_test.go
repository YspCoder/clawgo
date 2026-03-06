package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSubagentRouterDispatchAndWaitReply(t *testing.T) {
	workspace := t.TempDir()
	manager := NewSubagentManager(nil, workspace, nil, nil)
	manager.SetRunFunc(func(ctx context.Context, task *SubagentTask) (string, error) {
		return "router-result", nil
	})
	router := NewSubagentRouter(manager)

	task, err := router.DispatchTask(context.Background(), RouterDispatchRequest{
		Task:          "implement feature",
		AgentID:       "coder",
		OriginChannel: "cli",
		OriginChatID:  "direct",
	})
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if task.ThreadID == "" {
		t.Fatalf("expected thread id on dispatched task")
	}

	reply, err := router.WaitReply(context.Background(), task.ID, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("wait reply failed: %v", err)
	}
	if reply.Status != "completed" || reply.Result != "router-result" {
		t.Fatalf("unexpected reply: %+v", reply)
	}
}

func TestSubagentRouterMergeResults(t *testing.T) {
	router := NewSubagentRouter(nil)
	out := router.MergeResults([]*RouterReply{
		{TaskID: "subagent-1", AgentID: "coder", Status: "completed", Result: "done"},
		{TaskID: "subagent-2", AgentID: "tester", Status: "failed", Result: "boom"},
	})
	if !strings.Contains(out, "subagent-1") || !strings.Contains(out, "agent=tester") {
		t.Fatalf("unexpected merged output: %s", out)
	}
}
