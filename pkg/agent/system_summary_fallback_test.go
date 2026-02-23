package agent

import (
	"strings"
	"testing"
)

func TestBuildSystemTaskSummaryFallbackUsesPolicyPrefixes(t *testing.T) {
	policy := defaultSystemSummaryPolicy()
	policy.marker = "## Runtime Summary"
	policy.completedPrefix = "- Done:"
	policy.changesPrefix = "- Delta:"
	policy.outcomePrefix = "- Result:"

	out := buildSystemTaskSummaryFallback("task", "updated README.md\nbuild passed", policy)
	if !strings.HasPrefix(out, "## Runtime Summary") {
		t.Fatalf("expected custom marker, got: %s", out)
	}
	if !strings.Contains(out, "- Done:") || !strings.Contains(out, "- Delta:") || !strings.Contains(out, "- Result:") {
		t.Fatalf("expected custom prefixes, got: %s", out)
	}
}
