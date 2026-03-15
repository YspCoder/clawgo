package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/providers"
)

func TestAgentSpawnEnforcesTaskQuota(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	manager := NewAgentManager(nil, workspace, nil)
	manager.SetRunFunc(func(ctx context.Context, task *AgentTask) (string, error) {
		return "ok", nil
	})
	store := manager.ProfileStore()
	if store == nil {
		t.Fatalf("expected profile store")
	}
	if _, err := store.Upsert(AgentProfile{
		AgentID:      "coder",
		MaxTaskChars: 8,
	}); err != nil {
		t.Fatalf("failed to create profile: %v", err)
	}

	_, err := manager.Spawn(context.Background(), AgentSpawnOptions{
		Task:    "this task is too long",
		AgentID: "coder",
		Origin:  &OriginRef{Channel: "cli", ChatID: "direct"},
		ExecutionPolicy: &ExecutionPolicy{
			MaxTaskChars: 8,
		},
	})
	if err == nil {
		t.Fatalf("expected max_task_chars quota to reject spawn")
	}
}

func TestAgentRunWithRetryEventuallySucceeds(t *testing.T) {
	workspace := t.TempDir()
	manager := NewAgentManager(nil, workspace, nil)
	attempts := 0
	manager.SetRunFunc(func(ctx context.Context, task *AgentTask) (string, error) {
		attempts++
		if attempts == 1 {
			return "", errors.New("temporary failure")
		}
		return "retry success", nil
	})

	_, err := manager.Spawn(context.Background(), AgentSpawnOptions{
		Task:    "retry task",
		AgentID: "coder",
		Origin:  &OriginRef{Channel: "cli", ChatID: "direct"},
		ExecutionPolicy: &ExecutionPolicy{
			MaxRetries:   1,
			RetryBackoff: 1,
		},
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}

	task := waitAgentDone(t, manager, 4*time.Second)
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

func TestAgentRunAutoExtendsWhileStillRunning(t *testing.T) {
	workspace := t.TempDir()
	manager := NewAgentManager(nil, workspace, nil)
	manager.SetRunFunc(func(ctx context.Context, task *AgentTask) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
			return "completed after extension", nil
		}
	})

	_, err := manager.Spawn(context.Background(), AgentSpawnOptions{
		Task:    "timeout task",
		AgentID: "coder",
		Origin:  &OriginRef{Channel: "cli", ChatID: "direct"},
		ExecutionPolicy: &ExecutionPolicy{
			TimeoutSec: 1,
		},
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}

	task := waitAgentDone(t, manager, 4*time.Second)
	if task.Status != "completed" {
		t.Fatalf("expected completed task after watchdog extension, got %s", task.Status)
	}
	if task.RetryCount != 0 {
		t.Fatalf("expected retry_count=0, got %d", task.RetryCount)
	}
	if !strings.Contains(task.Result, "completed after extension") {
		t.Fatalf("expected extended result, got %q", task.Result)
	}
}

func TestAgentBroadcastIncludesFailureStatus(t *testing.T) {
	workspace := t.TempDir()
	msgBus := bus.NewMessageBus()
	defer msgBus.Close()

	manager := NewAgentManager(nil, workspace, msgBus)
	manager.SetRunFunc(func(ctx context.Context, task *AgentTask) (string, error) {
		return "", errors.New("boom")
	})

	_, err := manager.Spawn(context.Background(), AgentSpawnOptions{
		Task:          "failing task",
		AgentID:       "coder",
		Origin:        &OriginRef{Channel: "cli", ChatID: "direct"},
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}

	task := waitAgentDone(t, manager, 4*time.Second)
	if task.Status != "failed" {
		t.Fatalf("expected failed task, got %s", task.Status)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msg, ok := msgBus.ConsumeInbound(ctx)
	if !ok {
		t.Fatalf("expected agent completion message")
	}
	if got := strings.TrimSpace(msg.Metadata["status"]); got != "failed" {
		t.Fatalf("expected metadata status=failed, got %q", got)
	}
	if !strings.Contains(strings.ToLower(msg.Content), "status: failed") {
		t.Fatalf("expected structured failure status in content, got %q", msg.Content)
	}
	if got := strings.TrimSpace(msg.Metadata["notify_reason"]); got != "final" {
		t.Fatalf("expected notify_reason=final, got %q", got)
	}
}

func TestAgentManagerRestoresPersistedRuns(t *testing.T) {
	workspace := t.TempDir()
	manager := NewAgentManager(nil, workspace, nil)
	manager.SetRunFunc(func(ctx context.Context, task *AgentTask) (string, error) {
		return "persisted", nil
	})

	_, err := manager.Spawn(context.Background(), AgentSpawnOptions{
		Task:          "persist task",
		AgentID:       "coder",
		Origin:        &OriginRef{Channel: "cli", ChatID: "direct"},
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}

	task := waitAgentDone(t, manager, 4*time.Second)
	if task.Status != "completed" {
		t.Fatalf("expected completed task, got %s", task.Status)
	}

	reloaded := NewAgentManager(nil, workspace, nil)
	got, ok := reloaded.GetTask(task.ID)
	if !ok {
		t.Fatalf("expected persisted task to reload")
	}
	if got.Status != "completed" || got.Result != "persisted" {
		t.Fatalf("unexpected restored task: %+v", got)
	}

	_, err = reloaded.Spawn(context.Background(), AgentSpawnOptions{
		Task:          "second task",
		AgentID:       "coder",
		Origin:        &OriginRef{Channel: "cli", ChatID: "direct"},
	})
	if err != nil {
		t.Fatalf("spawn after reload failed: %v", err)
	}
	tasks := reloaded.ListTasks()
	found := false
	for _, item := range tasks {
		if item.ID == "agent-2" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected nextID seed to continue from persisted runs, got %+v", tasks)
	}
	_ = waitAgentDone(t, reloaded, 4*time.Second)
	time.Sleep(100 * time.Millisecond)
}

func TestAgentManagerWorldNPCSuppressesMainNotification(t *testing.T) {
	workspace := t.TempDir()
	msgBus := bus.NewMessageBus()
	manager := NewAgentManager(nil, workspace, msgBus)
	manager.SetRunFunc(func(ctx context.Context, task *AgentTask) (string, error) {
		return "silent-result", nil
	})

	_, err := manager.Spawn(context.Background(), AgentSpawnOptions{
		Task:    "npc world decision",
		RunKind: "world_npc",
		AgentID: "npc.guard",
		Origin:  &OriginRef{Channel: "cli", ChatID: "direct"},
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}
	task := waitAgentDone(t, manager, 4*time.Second)
	if task.Status != "completed" {
		t.Fatalf("expected completed task, got %s", task.Status)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if msg, ok := msgBus.ConsumeInbound(ctx); ok {
		t.Fatalf("did not expect main notification, got %+v", msg)
	}
}

func TestAgentManagerRecordsFailuresToEKG(t *testing.T) {
	workspace := t.TempDir()
	manager := NewAgentManager(nil, workspace, nil)
	manager.SetRunFunc(func(ctx context.Context, task *AgentTask) (string, error) {
		return "", errors.New("rate limit exceeded")
	})

	_, err := manager.Spawn(context.Background(), AgentSpawnOptions{
		Task:          "ekg failure",
		AgentID:       "coder",
		Origin:        &OriginRef{Channel: "cli", ChatID: "direct"},
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}
	_ = waitAgentDone(t, manager, 4*time.Second)

	data, err := os.ReadFile(filepath.Join(workspace, "memory", "ekg-events.jsonl"))
	if err != nil {
		t.Fatalf("expected ekg events to be written: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "\"source\":\"agent\"") {
		t.Fatalf("expected agent source in ekg log, got %s", text)
	}
	if !strings.Contains(text, "\"status\":\"error\"") {
		t.Fatalf("expected error status in ekg log, got %s", text)
	}
	if !strings.Contains(strings.ToLower(text), "rate limit exceeded") {
		t.Fatalf("expected failure text in ekg log, got %s", text)
	}
}

func TestAgentManagerAutoRecoversRunningTaskAfterRestart(t *testing.T) {
	workspace := t.TempDir()
	block := make(chan struct{})
	manager := NewAgentManager(nil, workspace, nil)
	manager.SetRunFunc(func(ctx context.Context, task *AgentTask) (string, error) {
		<-block
		return "should-not-complete-here", nil
	})

	_, err := manager.Spawn(context.Background(), AgentSpawnOptions{
		Task:          "recover me",
		AgentID:       "coder",
		Origin:        &OriginRef{Channel: "cli", ChatID: "direct"},
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}
	time.Sleep(80 * time.Millisecond)

	recovered := make(chan string, 1)
	reloaded := NewAgentManager(nil, workspace, nil)
	reloaded.SetRunFunc(func(ctx context.Context, task *AgentTask) (string, error) {
		recovered <- task.ID
		return "recovered-ok", nil
	})

	select {
	case taskID := <-recovered:
		if taskID != "agent-1" {
			t.Fatalf("expected recovered task id agent-1, got %s", taskID)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected running task to auto-recover after restart")
	}

	_ = waitAgentDone(t, reloaded, 4*time.Second)
	got, ok := reloaded.GetTask("agent-1")
	if !ok {
		t.Fatalf("expected recovered task to exist")
	}
	if got.Status != "completed" || got.Result != "recovered-ok" {
		t.Fatalf("unexpected recovered task: %+v", got)
	}

	close(block)
	_ = waitAgentDone(t, manager, 4*time.Second)
	time.Sleep(100 * time.Millisecond)
}

func TestAgentManagerPersistsEvents(t *testing.T) {
	workspace := t.TempDir()
	manager := NewAgentManager(nil, workspace, nil)
	manager.SetRunFunc(func(ctx context.Context, task *AgentTask) (string, error) {
		return "ok", nil
	})

	_, err := manager.Spawn(context.Background(), AgentSpawnOptions{
		Task:          "event task",
		AgentID:       "coder",
		Origin:        &OriginRef{Channel: "cli", ChatID: "direct"},
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}

	task := waitAgentDone(t, manager, 4*time.Second)
	events, err := manager.Events(task.ID, 0)
	if err != nil {
		t.Fatalf("events failed: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected persisted events")
	}
	hasCompleted := false
	for _, evt := range events {
		if evt.Type == "completed" {
			hasCompleted = true
			break
		}
	}
	if !hasCompleted {
		t.Fatalf("expected completed event, got %+v", events)
	}
}

func waitAgentDone(t *testing.T, manager *AgentManager, timeout time.Duration) *AgentTask {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		tasks := manager.ListTasks()
		if len(tasks) > 0 {
			task := tasks[0]
			for _, candidate := range tasks[1:] {
				if candidate.Created > task.Created || (candidate.Created == task.Created && candidate.ID > task.ID) {
					task = candidate
				}
			}
			manager.mu.RLock()
			_, stillRunning := manager.cancelFuncs[task.ID]
			manager.mu.RUnlock()
			if task.Status != "running" && !stillRunning {
				return task
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for agent completion")
	return nil
}

type captureProvider struct {
	messages []providers.Message
}

func (p *captureProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	p.messages = append([]providers.Message(nil), messages...)
	return &providers.LLMResponse{Content: "ok", FinishReason: "stop"}, nil
}

func (p *captureProvider) GetDefaultModel() string { return "test-model" }

func TestAgentUsesConfiguredPromptFile(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "agents", "coder"), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("workspace-policy"), 0644); err != nil {
		t.Fatalf("write workspace AGENTS failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "agents", "coder", "AGENT.md"), []byte("coder-policy-from-file"), 0644); err != nil {
		t.Fatalf("write coder AGENT failed: %v", err)
	}
	provider := &captureProvider{}
	manager := NewAgentManager(provider, workspace, nil)
	if _, err := manager.ProfileStore().Upsert(AgentProfile{
		AgentID:    "coder",
		Status:     "active",
		PromptFile: "agents/coder/AGENT.md",
	}); err != nil {
		t.Fatalf("profile upsert failed: %v", err)
	}

	_, err := manager.Spawn(context.Background(), AgentSpawnOptions{
		Task:          "implement feature",
		AgentID:       "coder",
		Origin:        &OriginRef{Channel: "cli", ChatID: "direct"},
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}
	_ = waitAgentDone(t, manager, 4*time.Second)
	if len(provider.messages) == 0 {
		t.Fatalf("expected provider to receive messages")
	}
	systemPrompt := provider.messages[0].Content
	if !strings.Contains(systemPrompt, "coder-policy-from-file") {
		t.Fatalf("expected system prompt to include configured file content, got: %s", systemPrompt)
	}
	if strings.Contains(systemPrompt, "inline-fallback") {
		t.Fatalf("expected configured file content to take precedence over inline prompt, got: %s", systemPrompt)
	}
}
