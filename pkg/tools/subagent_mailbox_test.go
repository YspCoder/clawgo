package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentMailboxStoreReloadsInboxIndexes(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	store := NewAgentMailboxStore(workspace)

	thread, err := store.EnsureThread(AgentThread{
		Owner:     "planner",
		Topic:     "handoff",
		CreatedAt: 10,
		UpdatedAt: 10,
	})
	if err != nil {
		t.Fatalf("ensure thread: %v", err)
	}
	msg, err := store.AppendMessage(AgentMessage{
		ThreadID:      thread.ThreadID,
		FromAgent:     "planner",
		ToAgent:       "worker-a",
		Type:          "task",
		Content:       "check deploy logs",
		RequiresReply: true,
		Status:        "queued",
		CreatedAt:     20,
	})
	if err != nil {
		t.Fatalf("append message: %v", err)
	}

	inbox, err := store.Inbox("worker-a", 10)
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	if len(inbox) != 1 || inbox[0].MessageID != msg.MessageID {
		t.Fatalf("unexpected inbox: %#v", inbox)
	}

	reloaded := NewAgentMailboxStore(workspace)
	threadInbox, err := reloaded.ThreadInbox(thread.ThreadID, "worker-a", 10)
	if err != nil {
		t.Fatalf("thread inbox: %v", err)
	}
	if len(threadInbox) != 1 || threadInbox[0].Content != "check deploy logs" {
		t.Fatalf("unexpected thread inbox: %#v", threadInbox)
	}

	if _, err := reloaded.UpdateMessageStatus(msg.MessageID, "processed", 30); err != nil {
		t.Fatalf("update status: %v", err)
	}
	inbox, err = reloaded.Inbox("worker-a", 10)
	if err != nil {
		t.Fatalf("inbox after status update: %v", err)
	}
	if len(inbox) != 0 {
		t.Fatalf("expected empty inbox after status update, got %#v", inbox)
	}

	runtimeDir := filepath.Join(workspace, "agents", "runtime")
	if err := os.Remove(filepath.Join(runtimeDir, "threads.meta.json")); err != nil {
		t.Fatalf("remove thread meta: %v", err)
	}
	if err := os.Remove(filepath.Join(runtimeDir, "agent_messages.meta.json")); err != nil {
		t.Fatalf("remove messages meta: %v", err)
	}
	rebuilt := NewAgentMailboxStore(workspace)
	messages, err := rebuilt.MessagesByThread(thread.ThreadID, 10)
	if err != nil {
		t.Fatalf("messages by thread: %v", err)
	}
	if len(messages) != 1 || messages[0].Status != "processed" {
		t.Fatalf("unexpected rebuilt messages: %#v", messages)
	}
}
