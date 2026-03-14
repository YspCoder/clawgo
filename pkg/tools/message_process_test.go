package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/YspCoder/clawgo/pkg/bus"
)

func TestMessageToolParsesStringAliases(t *testing.T) {
	t.Parallel()

	tool := NewMessageTool()
	tool.SetContext("telegram", "chat-1")

	called := false
	tool.SetSendCallback(func(channel, chatID, action, content, media, messageID, emoji string, buttons [][]bus.Button) error {
		called = true
		if channel != "telegram" || chatID != "chat-2" {
			t.Fatalf("unexpected target: %s %s", channel, chatID)
		}
		if action != "send" || content != "hello" || media != "/tmp/a.png" {
			t.Fatalf("unexpected payload: %s %s %s", action, content, media)
		}
		if len(buttons) != 1 || len(buttons[0]) != 1 || buttons[0][0].Text != "Open" {
			t.Fatalf("unexpected buttons: %#v", buttons)
		}
		return nil
	})

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"message": "hello",
		"to":      "chat-2",
		"path":    "/tmp/a.png",
		"buttons": []interface{}{
			[]interface{}{
				map[string]interface{}{"text": "Open", "data": "open"},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !called || !strings.Contains(out, "Message action=send") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestProcessToolParsesStringIntegers(t *testing.T) {
	t.Parallel()

	pm := NewProcessManager(t.TempDir())
	tool := NewProcessTool(pm)

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":     "list",
		"offset":     "10",
		"limit":      "20",
		"timeout_ms": "5",
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Fatalf("expected json list output, got %s", out)
	}
}
