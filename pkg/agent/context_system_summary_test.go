package agent

import (
	"fmt"
	"strings"
	"testing"

	"clawgo/pkg/config"
	"clawgo/pkg/providers"
)

func TestExtractSystemTaskSummariesFromHistory(t *testing.T) {
	history := []providers.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "## System Task Summary\n- Completed: A\n- Changes: B\n- Outcome: C"},
		{Role: "assistant", Content: "normal assistant reply"},
	}

	filtered, summaries := extractSystemTaskSummariesFromHistory(history)
	if len(summaries) != 1 {
		t.Fatalf("expected one summary, got %d", len(summaries))
	}
	if len(filtered) != 2 {
		t.Fatalf("expected summary message removed from history, got %d entries", len(filtered))
	}
}

func TestExtractSystemTaskSummariesKeepsRecentN(t *testing.T) {
	history := make([]providers.Message, 0, maxSystemTaskSummaries+2)
	for i := 0; i < maxSystemTaskSummaries+2; i++ {
		history = append(history, providers.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("## System Task Summary\n- Completed: task-%d\n- Changes: x\n- Outcome: ok", i),
		})
	}

	_, summaries := extractSystemTaskSummariesFromHistory(history)
	if len(summaries) != maxSystemTaskSummaries {
		t.Fatalf("expected %d summaries, got %d", maxSystemTaskSummaries, len(summaries))
	}
	if !strings.Contains(summaries[0], "task-2") {
		t.Fatalf("expected oldest retained summary to be task-2, got: %s", summaries[0])
	}
}

func TestFormatSystemTaskSummariesStructuredSections(t *testing.T) {
	summaries := []string{
		"## System Task Summary\n- Completed: update deps\n- Changes: modified go.mod\n- Outcome: build passed",
		"## System Task Summary\n- Completed: cleanup\n- Outcome: no action needed",
	}

	out := formatSystemTaskSummaries(summaries)
	if !strings.Contains(out, "### Completed Actions") {
		t.Fatalf("expected completed section, got: %s", out)
	}
	if !strings.Contains(out, "### Change Summaries") {
		t.Fatalf("expected change section, got: %s", out)
	}
	if !strings.Contains(out, "### Execution Outcomes") {
		t.Fatalf("expected outcome section, got: %s", out)
	}
	if !strings.Contains(out, "No explicit file-level changes noted.") {
		t.Fatalf("expected fallback changes text, got: %s", out)
	}
}

func TestSystemSummaryPolicyFromConfig(t *testing.T) {
	cfg := config.SystemSummaryPolicyConfig{
		CompletedTitle:  "完成事项",
		ChangesTitle:    "变更事项",
		OutcomesTitle:   "执行结果",
		CompletedPrefix: "- Done:",
		ChangesPrefix:   "- Delta:",
		OutcomePrefix:   "- Result:",
		Marker:          "## My Task Summary",
	}
	p := systemSummaryPolicyFromConfig(cfg)
	if p.completedSectionTitle != "完成事项" || p.changesSectionTitle != "变更事项" || p.outcomesSectionTitle != "执行结果" {
		t.Fatalf("section titles override failed: %#v", p)
	}
	if p.completedPrefix != "- Done:" || p.changesPrefix != "- Delta:" || p.outcomePrefix != "- Result:" || p.marker != "## My Task Summary" {
		t.Fatalf("field prefixes override failed: %#v", p)
	}
}

func TestParseSystemTaskSummaryWithCustomPolicy(t *testing.T) {
	p := defaultSystemSummaryPolicy()
	p.completedPrefix = "- Done:"
	p.changesPrefix = "- Delta:"
	p.outcomePrefix = "- Result:"

	raw := "## System Task Summary\n- Done: sync docs\n- Delta: modified README.md\n- Result: success"
	entry := parseSystemTaskSummaryWithPolicy(raw, p)
	if entry.completed != "sync docs" || entry.changes != "modified README.md" || entry.outcome != "success" {
		t.Fatalf("unexpected parsed entry: %#v", entry)
	}
}
