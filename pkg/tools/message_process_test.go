package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

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

func TestProcessToolWatchPatternsMatchesLog(t *testing.T) {
	t.Parallel()

	pm := NewProcessManager(t.TempDir())
	id, err := pm.Start(context.Background(), "printf 'READY\\n'; sleep 0.05", "")
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	tool := NewProcessTool(pm)

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":      "watch_patterns",
		"session_id":  id,
		"patterns":    []interface{}{"ready"},
		"timeout_ms":  2000,
		"interval_ms": 50,
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid json output: %v (%s)", err, out)
	}
	if matched, _ := payload["matched"].(bool); !matched {
		t.Fatalf("expected matched response, got %v", payload)
	}
}

func TestProcessToolWatchPatternsTimesOut(t *testing.T) {
	t.Parallel()

	pm := NewProcessManager(t.TempDir())
	id, err := pm.Start(context.Background(), "sleep 0.3", "")
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	tool := NewProcessTool(pm)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	out, err := tool.Execute(ctx, map[string]interface{}{
		"action":      "watch_patterns",
		"session_id":  id,
		"patterns":    "nomatch",
		"timeout_ms":  "120",
		"interval_ms": "30",
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid json output: %v (%s)", err, out)
	}
	if timedOut, _ := payload["timed_out"].(bool); !timedOut {
		t.Fatalf("expected timed_out=true, got %v", payload)
	}
}
