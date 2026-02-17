package agent

import (
	"errors"
	"strings"
	"testing"

	"clawgo/pkg/bus"
	"clawgo/pkg/session"
)

func TestFormatProcessingErrorMessage_ChineseCurrentMessage(t *testing.T) {
	al := &AgentLoop{}
	msg := bus.InboundMessage{
		SessionKey: "s-zh-current",
		Content:    "请帮我看一下这个错误",
	}

	out := al.formatProcessingErrorMessage(msg, errors.New("boom"))
	if !strings.HasPrefix(out, "处理消息时发生错误：") {
		t.Fatalf("expected Chinese error prefix, got %q", out)
	}
}

func TestFormatProcessingErrorMessage_EnglishCurrentMessage(t *testing.T) {
	al := &AgentLoop{}
	msg := bus.InboundMessage{
		SessionKey: "s-en-current",
		Content:    "Please help debug this issue",
	}

	out := al.formatProcessingErrorMessage(msg, errors.New("boom"))
	if !strings.HasPrefix(out, "Error processing message:") {
		t.Fatalf("expected English error prefix, got %q", out)
	}
}

func TestFormatProcessingErrorMessage_UsesSessionHistoryLanguage(t *testing.T) {
	sm := session.NewSessionManager(t.TempDir())
	sm.AddMessage("s-history", "user", "请继续，按这个方向修复")

	al := &AgentLoop{sessions: sm}
	msg := bus.InboundMessage{
		SessionKey: "s-history",
		Content:    "ok",
	}

	out := al.formatProcessingErrorMessage(msg, errors.New("boom"))
	if !strings.HasPrefix(out, "处理消息时发生错误：") {
		t.Fatalf("expected Chinese error prefix from session history, got %q", out)
	}
}
