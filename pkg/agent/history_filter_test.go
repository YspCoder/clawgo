package agent

import (
	"testing"

	"clawgo/pkg/providers"
)

func TestPruneControlHistoryMessagesDoesNotDropRealUserContent(t *testing.T) {
	history := []providers.Message{
		{Role: "user", Content: "autonomy round 3 is failing in my app and I need debugging help"},
		{Role: "assistant", Content: "Let's inspect logs first."},
	}

	pruned := pruneControlHistoryMessages(history)
	if len(pruned) != 2 {
		t.Fatalf("expected real user content to be preserved, got %d messages", len(pruned))
	}
}

func TestPruneControlHistoryMessagesDropsSyntheticPromptOnly(t *testing.T) {
	history := []providers.Message{
		{Role: "user", Content: "[system:autonomy] internal control prompt"},
		{Role: "assistant", Content: "Background task completed."},
	}

	pruned := pruneControlHistoryMessages(history)
	if len(pruned) != 1 {
		t.Fatalf("expected only synthetic user prompt to be removed, got %d messages", len(pruned))
	}
	if pruned[0].Role != "assistant" {
		t.Fatalf("expected assistant message to remain, got role=%s", pruned[0].Role)
	}
}
