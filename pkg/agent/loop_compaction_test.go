package agent

import (
	"fmt"
	"strings"
	"testing"

	"clawgo/pkg/providers"
)

func TestShouldCompactBySize(t *testing.T) {
	history := []providers.Message{
		{Role: "user", Content: strings.Repeat("a", 80)},
		{Role: "assistant", Content: strings.Repeat("b", 80)},
	}

	if !shouldCompactBySize("", history, 120) {
		t.Fatalf("expected size-based compaction trigger")
	}
	if shouldCompactBySize("", history, 10000) {
		t.Fatalf("did not expect trigger for large threshold")
	}
}

func TestFormatCompactionTranscript_HeadTailWhenOversized(t *testing.T) {
	msgs := make([]providers.Message, 0, 30)
	for i := 0; i < 30; i++ {
		msgs = append(msgs, providers.Message{
			Role:    "user",
			Content: fmt.Sprintf("msg-%02d %s", i, strings.Repeat("x", 80)),
		})
	}

	out := formatCompactionTranscript(msgs, 700)
	if out == "" {
		t.Fatalf("expected non-empty transcript")
	}
	if !strings.Contains(out, "msg-00") {
		t.Fatalf("expected head messages preserved, got: %q", out)
	}
	if !strings.Contains(out, "msg-29") {
		t.Fatalf("expected tail messages preserved, got: %q", out)
	}
	if !strings.Contains(out, "messages omitted for compaction") {
		t.Fatalf("expected omitted marker, got: %q", out)
	}
	if len(out) > 700 {
		t.Fatalf("expected output <= max chars, got %d", len(out))
	}
}

func TestFormatCompactionTranscript_TrimsToolPayloadMoreAggressively(t *testing.T) {
	msgs := []providers.Message{
		{Role: "tool", Content: strings.Repeat("z", 2000)},
	}
	out := formatCompactionTranscript(msgs, 2000)
	if len(out) >= 1200 {
		t.Fatalf("expected tool content to be trimmed aggressively, got length %d", len(out))
	}
}
