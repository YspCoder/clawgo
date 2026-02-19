package agent

import (
	"context"
	"errors"
	"testing"

	"clawgo/pkg/bus"
	"clawgo/pkg/session"
)

func TestFormatProcessingErrorMessage_CurrentMessage(t *testing.T) {
	al := &AgentLoop{}
	msg := bus.InboundMessage{
		SessionKey: "s-current",
		Content:    "Please help check this error",
	}

	out := al.formatProcessingErrorMessage(context.Background(), msg, errors.New("boom"))
	if out != "Error processing message: boom" {
		t.Fatalf("expected formatted error message, got %q", out)
	}
}

func TestFormatProcessingErrorMessage_EnglishCurrentMessage(t *testing.T) {
	al := &AgentLoop{}
	msg := bus.InboundMessage{
		SessionKey: "s-en-current",
		Content:    "Please help debug this issue",
	}

	out := al.formatProcessingErrorMessage(context.Background(), msg, errors.New("boom"))
	if out != "Error processing message: boom" {
		t.Fatalf("expected formatted error message, got %q", out)
	}
}

func TestFormatProcessingErrorMessage_UsesSessionHistory(t *testing.T) {
	sm := session.NewSessionManager(t.TempDir())
	sm.AddMessage("s-history", "user", "Please continue fixing in this direction")

	al := &AgentLoop{sessions: sm}
	msg := bus.InboundMessage{
		SessionKey: "s-history",
		Content:    "ok",
	}

	out := al.formatProcessingErrorMessage(context.Background(), msg, errors.New("boom"))
	if out != "Error processing message: boom" {
		t.Fatalf("expected formatted error message from session history, got %q", out)
	}
}
