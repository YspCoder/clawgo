package agent

import (
	"testing"

	"github.com/YspCoder/clawgo/pkg/providers"
)

func TestMergeUsageTotals(t *testing.T) {
	t.Parallel()

	var result llmTurnLoopResult
	mergeUsageTotals(&result, &providers.UsageInfo{PromptTokens: 10, CompletionTokens: 4, TotalTokens: 0})
	mergeUsageTotals(&result, &providers.UsageInfo{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 9})

	if result.promptTokens != 13 {
		t.Fatalf("prompt tokens = %d, want 13", result.promptTokens)
	}
	if result.completionTokens != 6 {
		t.Fatalf("completion tokens = %d, want 6", result.completionTokens)
	}
	// First merge falls back to prompt+completion (14), second uses explicit total (9).
	if result.totalTokens != 23 {
		t.Fatalf("total tokens = %d, want 23", result.totalTokens)
	}
}
