package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSubagentRouterDispatchAndWaitRun(t *testing.T) {
	workspace := t.TempDir()
	manager := NewSubagentManager(nil, workspace, nil)
	manager.SetRunFunc(func(ctx context.Context, run *SubagentRun) (string, error) {
		return "router-result", nil
	})
	router := NewSubagentRouter(manager)

	run, err := router.DispatchRun(context.Background(), RouterDispatchRequest{
		Task:          "implement feature",
		AgentID:       "coder",
		OriginChannel: "cli",
		OriginChatID:  "direct",
	})
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if run.ThreadID == "" {
		t.Fatalf("expected thread id on dispatched task")
	}

	reply, err := router.WaitRun(context.Background(), run.ID, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("wait run failed: %v", err)
	}
	if reply.Status != "completed" || reply.Result != "router-result" {
		t.Fatalf("unexpected reply: %+v", reply)
	}
}

func TestSubagentRouterMergeResults(t *testing.T) {
	router := NewSubagentRouter(nil)
	out := router.MergeResults([]*RouterReply{
		{RunID: "subagent-1", AgentID: "coder", Status: "completed", Result: "done"},
		{RunID: "subagent-2", AgentID: "tester", Status: "failed", Result: "boom"},
	})
	if !strings.Contains(out, "subagent-1") || !strings.Contains(out, "agent=tester") {
		t.Fatalf("unexpected merged output: %s", out)
	}
}

func TestSubagentRouterWaitRunContextCancel(t *testing.T) {
	workspace := t.TempDir()
	manager := NewSubagentManager(nil, workspace, nil)
	manager.SetRunFunc(func(ctx context.Context, run *SubagentRun) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	router := NewSubagentRouter(manager)

	run, err := router.DispatchRun(context.Background(), RouterDispatchRequest{
		Task:          "long task",
		AgentID:       "coder",
		OriginChannel: "cli",
		OriginChatID:  "direct",
	})
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := router.WaitRun(waitCtx, run.ID, 20*time.Millisecond); err == nil {
		t.Fatalf("expected context cancellation error")
	}
}
