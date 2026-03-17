package agent

import (
	"strings"
	"testing"
)

func TestSummarizePlannedTaskProgressBodyPreservesUsefulLines(t *testing.T) {
	t.Parallel()

	body := "subagent 已写入 config.json。\npath: /root/.clawgo/config.json\nagent_id: tester"
	out := summarizePlannedTaskProgressBody(body, 6, 320)

	if !strings.Contains(out, "subagent 已写入 config.json。") {
		t.Fatalf("expected title line, got:\n%s", out)
	}
	if !strings.Contains(out, "agent_id: tester") {
		t.Fatalf("expected agent id line, got:\n%s", out)
	}
	if strings.Contains(out, "subagent 已写入 config.json。 path:") {
		t.Fatalf("expected multi-line formatting, got:\n%s", out)
	}
}

func TestCompactMemoryHintDropsVerboseScaffolding(t *testing.T) {
	t.Parallel()

	raw := "Found 1 memories for 'deploy config' (namespace=main):\n\nSource: memory/2026-03-10.md#L12-L18\nDeploy must restart config service after updating the file.\nValidate permissions before rollout.\n\n"
	got := compactMemoryHint(raw, 120)
	if strings.Contains(strings.ToLower(got), "found 1 memories") {
		t.Fatalf("expected summary header removed, got: %s", got)
	}
	if !strings.Contains(got, "src=memory/2026-03-10.md#L12-L18") {
		t.Fatalf("expected source to remain compactly, got: %s", got)
	}
	if !strings.Contains(got, "Deploy must restart config service") {
		t.Fatalf("expected main snippet retained, got: %s", got)
	}
}

func TestDedupeTaskPromptHintsDropsRepeatedContext(t *testing.T) {
	t.Parallel()

	seen := map[string]struct{}{}
	first := taskPromptHints{memory: "src=memory/a.md#L1-L2 | restart service"}
	second := taskPromptHints{memory: "src=memory/a.md#L1-L2 | restart service"}

	dedupeTaskPromptHints(&first, seen)
	dedupeTaskPromptHints(&second, seen)

	if first.memory == "" {
		t.Fatalf("expected first hint set to remain: %+v", first)
	}
	if second.memory != "" {
		t.Fatalf("expected repeated hints removed: %+v", second)
	}
}

func TestBuildPlannedTaskPromptKeepsCompactShape(t *testing.T) {
	t.Parallel()

	got := buildPlannedTaskPrompt("deploy config service", taskPromptHints{
		memory: "src=memory/x.md#L1-L2 | restart after change",
	})
	if !strings.Contains(got.content, "Task Context:\nMemory: src=memory/x.md#L1-L2 | restart after change\nTask:\ndeploy config service") {
		t.Fatalf("unexpected prompt shape: %+v", got)
	}
}

func TestBuildPlannedTaskPromptTracksExtraChars(t *testing.T) {
	t.Parallel()

	prompt := buildPlannedTaskPrompt("deploy config service", taskPromptHints{
		memory: "src=memory/x.md#L1-L2 | restart after change",
	})
	if prompt.content == "" {
		t.Fatalf("expected prompt content")
	}
	if prompt.extraChars <= 0 || prompt.memoryChars <= 0 {
		t.Fatalf("expected prompt stats populated: %+v", prompt)
	}
}

func TestApplyPromptBudgetDropsMemoryWhenOverBudget(t *testing.T) {
	t.Parallel()

	hints := taskPromptHints{
		memory: "src=memory/x.md#L1-L2 | restart after change",
	}
	remaining := estimateHintChars(hints) - 1
	applyPromptBudget(&hints, &remaining)
	if hints.memory != "" {
		t.Fatalf("expected memory dropped under budget pressure: %+v", hints)
	}
}

func TestApplyPromptBudgetDropsAllWhenBudgetExhausted(t *testing.T) {
	t.Parallel()

	hints := taskPromptHints{
		memory: "src=memory/x.md#L1-L2 | restart after change",
	}
	remaining := 8
	applyPromptBudget(&hints, &remaining)
	if hints.memory != "" {
		t.Fatalf("expected all hints removed under tiny budget: %+v", hints)
	}
}
