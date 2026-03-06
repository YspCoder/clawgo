package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawgo/pkg/bus"
	"clawgo/pkg/providers"
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

func TestSubagentBroadcastIncludesFailureStatus(t *testing.T) {
	workspace := t.TempDir()
	msgBus := bus.NewMessageBus()
	defer msgBus.Close()

	manager := NewSubagentManager(nil, workspace, msgBus, nil)
	manager.SetRunFunc(func(ctx context.Context, task *SubagentTask) (string, error) {
		return "", errors.New("boom")
	})

	_, err := manager.Spawn(context.Background(), SubagentSpawnOptions{
		Task:          "failing task",
		AgentID:       "coder",
		OriginChannel: "cli",
		OriginChatID:  "direct",
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}

	task := waitSubagentDone(t, manager, 4*time.Second)
	if task.Status != "failed" {
		t.Fatalf("expected failed task, got %s", task.Status)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msg, ok := msgBus.ConsumeInbound(ctx)
	if !ok {
		t.Fatalf("expected subagent completion message")
	}
	if got := strings.TrimSpace(msg.Metadata["status"]); got != "failed" {
		t.Fatalf("expected metadata status=failed, got %q", got)
	}
	if !strings.Contains(strings.ToLower(msg.Content), "failed") {
		t.Fatalf("expected failure wording in content, got %q", msg.Content)
	}
}

func TestSubagentManagerRestoresPersistedRuns(t *testing.T) {
	workspace := t.TempDir()
	manager := NewSubagentManager(nil, workspace, nil, nil)
	manager.SetRunFunc(func(ctx context.Context, task *SubagentTask) (string, error) {
		return "persisted", nil
	})

	_, err := manager.Spawn(context.Background(), SubagentSpawnOptions{
		Task:          "persist task",
		AgentID:       "coder",
		OriginChannel: "cli",
		OriginChatID:  "direct",
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}

	task := waitSubagentDone(t, manager, 4*time.Second)
	if task.Status != "completed" {
		t.Fatalf("expected completed task, got %s", task.Status)
	}

	reloaded := NewSubagentManager(nil, workspace, nil, nil)
	got, ok := reloaded.GetTask(task.ID)
	if !ok {
		t.Fatalf("expected persisted task to reload")
	}
	if got.Status != "completed" || got.Result != "persisted" {
		t.Fatalf("unexpected restored task: %+v", got)
	}

	_, err = reloaded.Spawn(context.Background(), SubagentSpawnOptions{
		Task:          "second task",
		AgentID:       "coder",
		OriginChannel: "cli",
		OriginChatID:  "direct",
	})
	if err != nil {
		t.Fatalf("spawn after reload failed: %v", err)
	}
	tasks := reloaded.ListTasks()
	found := false
	for _, item := range tasks {
		if item.ID == "subagent-2" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected nextID seed to continue from persisted runs, got %+v", tasks)
	}
	_ = waitSubagentDone(t, reloaded, 4*time.Second)
	time.Sleep(100 * time.Millisecond)
}

func TestSubagentManagerPersistsEvents(t *testing.T) {
	workspace := t.TempDir()
	manager := NewSubagentManager(nil, workspace, nil, nil)
	manager.SetRunFunc(func(ctx context.Context, task *SubagentTask) (string, error) {
		time.Sleep(100 * time.Millisecond)
		return "ok", nil
	})

	_, err := manager.Spawn(context.Background(), SubagentSpawnOptions{
		Task:          "event task",
		AgentID:       "coder",
		OriginChannel: "cli",
		OriginChatID:  "direct",
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	if !manager.SteerTask("subagent-1", "focus on tests") {
		t.Fatalf("expected steer to succeed")
	}
	task := waitSubagentDone(t, manager, 4*time.Second)
	events, err := manager.Events(task.ID, 0)
	if err != nil {
		t.Fatalf("events failed: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected persisted events")
	}
	hasSteer := false
	for _, evt := range events {
		if evt.Type == "steered" {
			hasSteer = true
			break
		}
	}
	if !hasSteer {
		t.Fatalf("expected steered event, got %+v", events)
	}
}

func TestSubagentMailboxStoresThreadAndReplies(t *testing.T) {
	workspace := t.TempDir()
	manager := NewSubagentManager(nil, workspace, nil, nil)
	manager.SetRunFunc(func(ctx context.Context, task *SubagentTask) (string, error) {
		return "done", nil
	})

	_, err := manager.Spawn(context.Background(), SubagentSpawnOptions{
		Task:          "implement feature",
		AgentID:       "coder",
		OriginChannel: "cli",
		OriginChatID:  "direct",
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}

	task := waitSubagentDone(t, manager, 4*time.Second)
	if task.ThreadID == "" {
		t.Fatalf("expected thread id")
	}
	thread, ok := manager.Thread(task.ThreadID)
	if !ok {
		t.Fatalf("expected thread to exist")
	}
	if thread.Owner != "main" {
		t.Fatalf("expected thread owner main, got %s", thread.Owner)
	}

	msgs, err := manager.ThreadMessages(task.ThreadID, 10)
	if err != nil {
		t.Fatalf("thread messages failed: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected task and reply messages, got %+v", msgs)
	}
	if msgs[0].FromAgent != "main" || msgs[0].ToAgent != "coder" {
		t.Fatalf("unexpected initial message: %+v", msgs[0])
	}
	last := msgs[len(msgs)-1]
	if last.FromAgent != "coder" || last.ToAgent != "main" || last.Type != "result" {
		t.Fatalf("unexpected reply message: %+v", last)
	}
}

func TestSubagentMailboxInboxIncludesControlMessages(t *testing.T) {
	workspace := t.TempDir()
	manager := NewSubagentManager(nil, workspace, nil, nil)
	manager.SetRunFunc(func(ctx context.Context, task *SubagentTask) (string, error) {
		time.Sleep(150 * time.Millisecond)
		return "ok", nil
	})

	_, err := manager.Spawn(context.Background(), SubagentSpawnOptions{
		Task:          "run checks",
		AgentID:       "tester",
		OriginChannel: "cli",
		OriginChatID:  "direct",
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}

	time.Sleep(30 * time.Millisecond)
	if !manager.SteerTask("subagent-1", "focus on regressions") {
		t.Fatalf("expected steer to succeed")
	}
	inbox, err := manager.Inbox("tester", 10)
	if err != nil {
		t.Fatalf("inbox failed: %v", err)
	}
	if len(inbox) < 1 {
		t.Fatalf("expected queued control message, got %+v", inbox)
	}
	foundControl := false
	for _, msg := range inbox {
		if msg.Type == "control" && strings.Contains(msg.Content, "regressions") {
			foundControl = true
			break
		}
	}
	if !foundControl {
		t.Fatalf("expected control message in inbox, got %+v", inbox)
	}
	_ = waitSubagentDone(t, manager, 4*time.Second)
}

func TestSubagentMailboxReplyAndAckFlow(t *testing.T) {
	workspace := t.TempDir()
	manager := NewSubagentManager(nil, workspace, nil, nil)
	manager.SetRunFunc(func(ctx context.Context, task *SubagentTask) (string, error) {
		time.Sleep(150 * time.Millisecond)
		return "ok", nil
	})

	_, err := manager.Spawn(context.Background(), SubagentSpawnOptions{
		Task:          "review patch",
		AgentID:       "tester",
		OriginChannel: "cli",
		OriginChatID:  "direct",
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if !manager.SendTaskMessage("subagent-1", "please confirm scope") {
		t.Fatalf("expected send to succeed")
	}

	inbox, err := manager.Inbox("tester", 10)
	if err != nil {
		t.Fatalf("inbox failed: %v", err)
	}
	if len(inbox) == 0 {
		t.Fatalf("expected inbox messages")
	}
	initial := inbox[0]
	if !manager.ReplyToTask("subagent-1", initial.MessageID, "working on it") {
		t.Fatalf("expected reply to succeed")
	}
	threadMsgs, err := manager.ThreadMessages(initial.ThreadID, 10)
	if err != nil {
		t.Fatalf("thread messages failed: %v", err)
	}
	foundReply := false
	for _, msg := range threadMsgs {
		if msg.Type == "reply" && msg.ReplyTo == initial.MessageID {
			foundReply = true
			break
		}
	}
	if !foundReply {
		t.Fatalf("expected reply message linked to %s, got %+v", initial.MessageID, threadMsgs)
	}
	if !manager.AckTaskMessage("subagent-1", initial.MessageID) {
		t.Fatalf("expected ack to succeed")
	}
	updated, ok := manager.Message(initial.MessageID)
	if !ok {
		t.Fatalf("expected message lookup to succeed")
	}
	if updated.Status != "acked" {
		t.Fatalf("expected acked status, got %+v", updated)
	}
	queuedInbox, err := manager.Inbox("tester", 10)
	if err != nil {
		t.Fatalf("queued inbox failed: %v", err)
	}
	for _, msg := range queuedInbox {
		if msg.MessageID == initial.MessageID {
			t.Fatalf("acked message should not remain in queued inbox: %+v", queuedInbox)
		}
	}
	_ = waitSubagentDone(t, manager, 4*time.Second)
}

func TestSubagentResumeConsumesQueuedThreadInbox(t *testing.T) {
	workspace := t.TempDir()
	manager := NewSubagentManager(nil, workspace, nil, nil)
	observedQueued := make(chan int, 4)
	manager.SetRunFunc(func(ctx context.Context, task *SubagentTask) (string, error) {
		inbox, err := manager.TaskInbox(task.ID, 10)
		if err != nil {
			return "", err
		}
		observedQueued <- len(inbox)
		return "ok", nil
	})

	_, err := manager.Spawn(context.Background(), SubagentSpawnOptions{
		Task:          "initial task",
		AgentID:       "coder",
		OriginChannel: "cli",
		OriginChatID:  "direct",
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}
	initial := waitSubagentDone(t, manager, 4*time.Second)
	if queued := <-observedQueued; queued != 0 {
		t.Fatalf("expected initial run to see empty queued inbox during execution, got %d", queued)
	}

	if !manager.SendTaskMessage(initial.ID, "please address follow-up") {
		t.Fatalf("expected send to succeed")
	}
	inbox, err := manager.Inbox("coder", 10)
	if err != nil {
		t.Fatalf("inbox failed: %v", err)
	}
	if len(inbox) == 0 {
		t.Fatalf("expected queued inbox after send")
	}
	messageID := inbox[0].MessageID

	if _, ok := manager.ResumeTask(context.Background(), initial.ID); !ok {
		t.Fatalf("expected resume to succeed")
	}
	_ = waitSubagentDone(t, manager, 4*time.Second)
	if queued := <-observedQueued; queued != 0 {
		t.Fatalf("expected resumed run to consume queued inbox before execution, got %d", queued)
	}
	remaining, err := manager.Inbox("coder", 10)
	if err != nil {
		t.Fatalf("remaining inbox failed: %v", err)
	}
	for _, msg := range remaining {
		if msg.MessageID == messageID {
			t.Fatalf("expected consumed message to leave queued inbox, got %+v", remaining)
		}
	}
	stored, ok := manager.Message(messageID)
	if !ok {
		t.Fatalf("expected stored message lookup")
	}
	if stored.Status != "acked" {
		t.Fatalf("expected consumed message to be acked, got %+v", stored)
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

type captureProvider struct {
	messages []providers.Message
}

func (p *captureProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	p.messages = append([]providers.Message(nil), messages...)
	return &providers.LLMResponse{Content: "ok", FinishReason: "stop"}, nil
}

func (p *captureProvider) GetDefaultModel() string { return "test-model" }

func TestSubagentUsesConfiguredSystemPromptFile(t *testing.T) {
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
	manager := NewSubagentManager(provider, workspace, nil, nil)
	if _, err := manager.ProfileStore().Upsert(SubagentProfile{
		AgentID:          "coder",
		Status:           "active",
		SystemPrompt:     "inline-fallback",
		SystemPromptFile: "agents/coder/AGENT.md",
	}); err != nil {
		t.Fatalf("profile upsert failed: %v", err)
	}

	_, err := manager.Spawn(context.Background(), SubagentSpawnOptions{
		Task:          "implement feature",
		AgentID:       "coder",
		OriginChannel: "cli",
		OriginChatID:  "direct",
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}
	_ = waitSubagentDone(t, manager, 4*time.Second)
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
