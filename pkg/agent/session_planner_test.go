package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YspCoder/clawgo/pkg/ekg"
)

func TestSummarizePlannedTaskProgressBodyPreservesUsefulLines(t *testing.T) {
	t.Parallel()

	body := "agent 已写入 config.json。\npath: /root/.clawgo/config.json\nagent_id: tester"
	out := summarizePlannedTaskProgressBody(body, 6, 320)

	if !strings.Contains(out, "agent 已写入 config.json。") {
		t.Fatalf("expected title line, got:\n%s", out)
	}
	if !strings.Contains(out, "agent_id: tester") {
		t.Fatalf("expected agent id line, got:\n%s", out)
	}
	if strings.Contains(out, "agent 已写入 config.json。 path:") {
		t.Fatalf("expected multi-line formatting, got:\n%s", out)
	}
}

func TestEKGHintForTaskRequiresStrongMatchAndStaysCompact(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	memoryDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	logText := "open /srv/app/config.yaml: permission denied after deploy 42"
	taskAudit := []string{
		fmt.Sprintf(`{"task_id":"task-1","status":"error","source":"planner","channel":"chat","input_preview":"check nginx logs quickly","log":"%s"}`, logText),
		fmt.Sprintf(`{"task_id":"task-2","status":"error","source":"planner","channel":"chat","input_preview":"deploy config service restart on cluster","log":"%s"}`, logText),
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "task-audit.jsonl"), []byte(strings.Join(taskAudit, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write task audit: %v", err)
	}
	errSig := ekg.NormalizeErrorSignature(logText)
	ekgEvents := []string{
		fmt.Sprintf(`{"task_id":"task-2","status":"error","errsig":"%s","log":"%s"}`, errSig, logText),
		fmt.Sprintf(`{"task_id":"task-2","status":"error","errsig":"%s","log":"%s"}`, errSig, logText),
		fmt.Sprintf(`{"task_id":"task-2","status":"error","errsig":"%s","log":"%s"}`, errSig, logText),
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "ekg-events.jsonl"), []byte(strings.Join(ekgEvents, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write ekg events: %v", err)
	}

	al := &AgentLoop{workspace: workspace, ekg: ekg.New(workspace)}
	hint := al.ekgHintForTask(plannedTask{Content: "deploy config service restart after rollout"})
	if hint == "" {
		t.Fatalf("expected compact ekg hint")
	}
	if !strings.Contains(hint, "repeat_errsig=") || !strings.Contains(hint, "backoff=300s") {
		t.Fatalf("expected compact fields, got: %s", hint)
	}
	if strings.Contains(strings.ToLower(hint), "last error") || strings.Contains(hint, logText) {
		t.Fatalf("expected raw error log to be omitted, got: %s", hint)
	}
}

func TestEKGHintForTaskSkipsWeakTaskMatch(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	memoryDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	logText := "dial tcp 10.0.0.8:443: i/o timeout"
	taskAudit := `{"task_id":"task-3","status":"error","source":"planner","channel":"chat","input_preview":"investigate cache timeout","log":"dial tcp 10.0.0.8:443: i/o timeout"}`
	if err := os.WriteFile(filepath.Join(memoryDir, "task-audit.jsonl"), []byte(taskAudit+"\n"), 0o644); err != nil {
		t.Fatalf("write task audit: %v", err)
	}
	errSig := ekg.NormalizeErrorSignature(logText)
	ekgEvents := []string{
		fmt.Sprintf(`{"task_id":"task-3","status":"error","errsig":"%s","log":"%s"}`, errSig, logText),
		fmt.Sprintf(`{"task_id":"task-3","status":"error","errsig":"%s","log":"%s"}`, errSig, logText),
		fmt.Sprintf(`{"task_id":"task-3","status":"error","errsig":"%s","log":"%s"}`, errSig, logText),
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "ekg-events.jsonl"), []byte(strings.Join(ekgEvents, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write ekg events: %v", err)
	}

	al := &AgentLoop{workspace: workspace, ekg: ekg.New(workspace)}
	hint := al.ekgHintForTask(plannedTask{Content: "cache rebuild"})
	if hint != "" {
		t.Fatalf("expected weak match to skip ekg hint, got: %s", hint)
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
	first := taskPromptHints{ekg: "repeat_errsig=x; backoff=300s", memory: "src=memory/a.md#L1-L2 | restart service"}
	second := taskPromptHints{ekg: "repeat_errsig=x; backoff=300s", memory: "src=memory/a.md#L1-L2 | restart service"}

	dedupeTaskPromptHints(&first, seen)
	dedupeTaskPromptHints(&second, seen)

	if first.ekg == "" || first.memory == "" {
		t.Fatalf("expected first hint set to remain: %+v", first)
	}
	if second.ekg != "" || second.memory != "" {
		t.Fatalf("expected repeated hints removed: %+v", second)
	}
}

func TestRenderTaskPromptWithHintsKeepsCompactShape(t *testing.T) {
	t.Parallel()

	got := renderTaskPromptWithHints("deploy config service", taskPromptHints{
		ekg:    "repeat_errsig=perm; backoff=300s",
		memory: "src=memory/x.md#L1-L2 | restart after change",
	})
	if !strings.Contains(got, "Task Context:\nEKG: repeat_errsig=perm; backoff=300s\nMemory: src=memory/x.md#L1-L2 | restart after change\nTask:\ndeploy config service") {
		t.Fatalf("unexpected prompt shape: %s", got)
	}
}

func TestBuildPlannedTaskPromptTracksExtraChars(t *testing.T) {
	t.Parallel()

	prompt := buildPlannedTaskPrompt("deploy config service", taskPromptHints{
		ekg:    "repeat_errsig=perm; backoff=300s",
		memory: "src=memory/x.md#L1-L2 | restart after change",
	})
	if prompt.content == "" {
		t.Fatalf("expected prompt content")
	}
	if prompt.extraChars <= 0 || prompt.ekgChars <= 0 || prompt.memoryChars <= 0 {
		t.Fatalf("expected prompt stats populated: %+v", prompt)
	}
}

func TestApplyPromptBudgetPrefersEKGOverMemory(t *testing.T) {
	t.Parallel()

	hints := taskPromptHints{
		ekg:    "repeat_errsig=perm; backoff=300s",
		memory: "src=memory/x.md#L1-L2 | restart after change",
	}
	remaining := estimateHintChars(hints) - len(hints.memory)
	applyPromptBudget(&hints, &remaining)
	if hints.ekg == "" {
		t.Fatalf("expected ekg retained")
	}
	if hints.memory != "" {
		t.Fatalf("expected memory dropped under budget pressure: %+v", hints)
	}
}

func TestApplyPromptBudgetDropsAllWhenBudgetExhausted(t *testing.T) {
	t.Parallel()

	hints := taskPromptHints{
		ekg:    "repeat_errsig=perm; backoff=300s",
		memory: "src=memory/x.md#L1-L2 | restart after change",
	}
	remaining := 8
	applyPromptBudget(&hints, &remaining)
	if hints.ekg != "" || hints.memory != "" {
		t.Fatalf("expected all hints removed under tiny budget: %+v", hints)
	}
}
