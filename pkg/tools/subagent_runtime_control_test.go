package tools

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSubagentSpawnEnforcesTaskQuota(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	manager := NewSubagentManager(nil, workspace, nil, nil)
	manager.SetRunFunc(func(ctx context.Context, task *SubagentTask) (string, error) {
		return "ok", nil
	})
	store := manager.ProfileStore()
	if store == nil {
		t.Fatalf("expected profile store")
	}
	if _, err := store.Upsert(SubagentProfile{
		AgentID:      "coder",
		MaxTaskChars: 8,
	}); err != nil {
		t.Fatalf("failed to create profile: %v", err)
	}

	_, err := manager.Spawn(context.Background(), SubagentSpawnOptions{
		Task:          "this task is too long",
		AgentID:       "coder",
		OriginChannel: "cli",
		OriginChatID:  "direct",
	})
	if err == nil {
		t.Fatalf("expected max_task_chars quota to reject spawn")
	}
}

func TestSubagentRunWithRetryEventuallySucceeds(t *testing.T) {
	workspace := t.TempDir()
	manager := NewSubagentManager(nil, workspace, nil, nil)
	attempts := 0
	manager.SetRunFunc(func(ctx context.Context, task *SubagentTask) (string, error) {
		attempts++
		if attempts == 1 {
			return "", errors.New("temporary failure")
		}
		return "retry success", nil
	})

	_, err := manager.Spawn(context.Background(), SubagentSpawnOptions{
		Task:          "retry task",
		AgentID:       "coder",
		OriginChannel: "cli",
		OriginChatID:  "direct",
		MaxRetries:    1,
		RetryBackoff:  1,
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}

	task := waitSubagentDone(t, manager, 4*time.Second)
	if task.Status != "completed" {
		t.Fatalf("expected completed task, got %s (%s)", task.Status, task.Result)
	}
	if task.RetryCount != 1 {
		t.Fatalf("expected retry_count=1, got %d", task.RetryCount)
	}
	if attempts < 2 {
		t.Fatalf("expected at least 2 attempts, got %d", attempts)
	}
}

func TestSubagentRunWithTimeoutFails(t *testing.T) {
	workspace := t.TempDir()
	manager := NewSubagentManager(nil, workspace, nil, nil)
	manager.SetRunFunc(func(ctx context.Context, task *SubagentTask) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
			return "unexpected", nil
		}
	})

	_, err := manager.Spawn(context.Background(), SubagentSpawnOptions{
		Task:          "timeout task",
		AgentID:       "coder",
		OriginChannel: "cli",
		OriginChatID:  "direct",
		TimeoutSec:    1,
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}

	task := waitSubagentDone(t, manager, 4*time.Second)
	if task.Status != "failed" {
		t.Fatalf("expected failed task on timeout, got %s", task.Status)
	}
	if task.RetryCount != 0 {
		t.Fatalf("expected retry_count=0, got %d", task.RetryCount)
	}
}

func waitSubagentDone(t *testing.T, manager *SubagentManager, timeout time.Duration) *SubagentTask {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		tasks := manager.ListTasks()
		if len(tasks) > 0 {
			task := tasks[0]
			if task.Status != "running" {
				return task
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for subagent completion")
	return nil
}
