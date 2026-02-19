package agent

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"clawgo/pkg/config"
	"clawgo/pkg/providers"
	"clawgo/pkg/session"
	"clawgo/pkg/tools"
)

func TestToolCallsSignatureStableForSameInput(t *testing.T) {
	t.Parallel()

	calls := []providers.ToolCall{
		{
			Name:      "shell",
			Arguments: map[string]interface{}{"cmd": "ls -la", "cwd": "/tmp"},
		},
		{
			Name:      "read_file",
			Arguments: map[string]interface{}{"path": "README.md"},
		},
	}

	s1 := toolCallsSignature(calls)
	s2 := toolCallsSignature(calls)
	if s1 == "" {
		t.Fatalf("expected non-empty signature")
	}
	if s1 != s2 {
		t.Fatalf("expected stable signature, got %q vs %q", s1, s2)
	}
}

func TestToolCallsSignatureDiffersByArguments(t *testing.T) {
	t.Parallel()

	callsA := []providers.ToolCall{
		{Name: "shell", Arguments: map[string]interface{}{"cmd": "ls -la"}},
	}
	callsB := []providers.ToolCall{
		{Name: "shell", Arguments: map[string]interface{}{"cmd": "pwd"}},
	}

	if toolCallsSignature(callsA) == toolCallsSignature(callsB) {
		t.Fatalf("expected different signatures for different arguments")
	}
}

func TestNormalizeReflectDecision(t *testing.T) {
	t.Parallel()

	if got := normalizeReflectDecision("DONE"); got != "done" {
		t.Fatalf("expected done, got %s", got)
	}
	if got := normalizeReflectDecision("blocked"); got != "blocked" {
		t.Fatalf("expected blocked, got %s", got)
	}
	if got := normalizeReflectDecision("unknown"); got != "continue" {
		t.Fatalf("expected continue, got %s", got)
	}
}

func TestShouldTriggerReflectionReplayScenarios(t *testing.T) {
	t.Parallel()

	al := &AgentLoop{maxIterations: 5}
	tests := []struct {
		name    string
		state   toolLoopState
		outcome toolActOutcome
		want    bool
	}{
		{
			name:    "tool failure",
			state:   toolLoopState{iteration: 2},
			outcome: toolActOutcome{executedCalls: 2, roundToolErrors: 1, lastToolResult: "Error: denied"},
			want:    true,
		},
		{
			name:    "repetition hint",
			state:   toolLoopState{iteration: 2, repeatedToolCallRounds: 1},
			outcome: toolActOutcome{executedCalls: 1, lastToolResult: "ok"},
			want:    true,
		},
		{
			name:    "near iteration limit",
			state:   toolLoopState{iteration: 4},
			outcome: toolActOutcome{executedCalls: 1, lastToolResult: "ok"},
			want:    true,
		},
		{
			name:    "empty tool result",
			state:   toolLoopState{iteration: 1},
			outcome: toolActOutcome{executedCalls: 1, lastToolResult: ""},
			want:    true,
		},
		{
			name:    "healthy progress",
			state:   toolLoopState{iteration: 1},
			outcome: toolActOutcome{executedCalls: 1, lastToolResult: "done step 1"},
			want:    true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := al.shouldTriggerReflection(tt.state, tt.outcome)
			if got != tt.want {
				t.Fatalf("shouldTriggerReflection=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestShouldTriggerReflectionCooldown(t *testing.T) {
	t.Parallel()

	al := &AgentLoop{maxIterations: 10}
	state := toolLoopState{
		iteration:            3,
		lastReflectIteration: 2,
	}
	// No hard trigger, within cooldown window -> false.
	if al.shouldTriggerReflection(state, toolActOutcome{executedCalls: 1, lastToolResult: "ok"}) {
		t.Fatalf("expected reflection suppressed by cooldown")
	}

	// Hard trigger bypasses cooldown.
	if !al.shouldTriggerReflection(state, toolActOutcome{executedCalls: 1, roundToolErrors: 1, lastToolResult: "Error: x"}) {
		t.Fatalf("expected hard trigger to bypass cooldown")
	}
}

type replayTool struct {
	name         string
	parallelSafe *bool
	resourceKeys func(args map[string]interface{}) []string
	run          func(context.Context, map[string]interface{}) (string, error)
}

func (t replayTool) Name() string        { return t.name }
func (t replayTool) Description() string { return "replay tool" }
func (t replayTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}
func (t replayTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if t.run != nil {
		return t.run(ctx, args)
	}
	return fmt.Sprintf("ok:%s", t.name), nil
}

func (t replayTool) ParallelSafe() bool {
	if t.parallelSafe == nil {
		return false
	}
	return *t.parallelSafe
}

func (t replayTool) ResourceKeys(args map[string]interface{}) []string {
	if t.resourceKeys == nil {
		return nil
	}
	return t.resourceKeys(args)
}

type deferralRetryProvider struct {
	planCalls int
}

func (p *deferralRetryProvider) Chat(ctx context.Context, messages []providers.Message, defs []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	if len(defs) == 0 {
		return &providers.LLMResponse{Content: "finalized"}, nil
	}
	p.planCalls++
	switch p.planCalls {
	case 1:
		return &providers.LLMResponse{Content: "需要先查看一下当前工作区才能确认，请稍等。"}, nil
	case 2:
		return &providers.LLMResponse{
			Content: "先检查状态",
			ToolCalls: []providers.ToolCall{
				{ID: "tc-status-1", Name: "read_file", Arguments: map[string]interface{}{"path": "README.md"}},
			},
		}, nil
	default:
		return &providers.LLMResponse{Content: "已完成状态检查，当前一切正常。"}, nil
	}
}

func (p *deferralRetryProvider) GetDefaultModel() string { return "test-model" }

func TestActToolCalls_BudgetTruncationReplay(t *testing.T) {
	t.Parallel()

	reg := tools.NewToolRegistry()
	calls := make([]providers.ToolCall, 0, toolLoopMaxCallsPerIteration+2)
	for i := 0; i < toolLoopMaxCallsPerIteration+2; i++ {
		name := fmt.Sprintf("tool_%d", i)
		reg.Register(replayTool{name: name})
		calls = append(calls, providers.ToolCall{
			ID:        fmt.Sprintf("tc-%d", i),
			Name:      name,
			Arguments: map[string]interface{}{},
		})
	}

	al := &AgentLoop{
		tools:    reg,
		sessions: session.NewSessionManager(""),
	}
	msgs := []providers.Message{}
	out := al.actToolCalls(context.Background(), "", calls, &msgs, "s1", 1, toolLoopBudget{}, false, nil)

	if !out.truncated {
		t.Fatalf("expected truncation due to budget")
	}
	if out.executedCalls != toolLoopMaxCallsPerIteration {
		t.Fatalf("executed=%d want=%d", out.executedCalls, toolLoopMaxCallsPerIteration)
	}
	if out.droppedCalls != 2 {
		t.Fatalf("dropped=%d want=2", out.droppedCalls)
	}
}

func TestComputeToolLoopBudget(t *testing.T) {
	t.Parallel()

	al := &AgentLoop{maxIterations: 6}

	early := al.computeToolLoopBudget(toolLoopState{iteration: 1})
	if early.maxCallsPerIteration <= toolLoopMaxCallsPerIteration {
		t.Fatalf("expected wider early budget, got %d", early.maxCallsPerIteration)
	}

	degraded := al.computeToolLoopBudget(toolLoopState{iteration: 2, consecutiveAllToolErrorRounds: 1})
	if degraded.maxCallsPerIteration >= toolLoopMaxCallsPerIteration {
		t.Fatalf("expected tighter degraded budget, got %d", degraded.maxCallsPerIteration)
	}

	nearLimit := al.computeToolLoopBudget(toolLoopState{iteration: 5})
	if nearLimit.maxCallsPerIteration != toolLoopMinCallsPerIteration {
		t.Fatalf("expected minimal near-limit calls, got %d", nearLimit.maxCallsPerIteration)
	}
	if nearLimit.singleCallTimeout != toolLoopMinSingleCallTimeout {
		t.Fatalf("expected minimal near-limit timeout, got %s", nearLimit.singleCallTimeout)
	}

	lowConfContinue := al.computeToolLoopBudget(toolLoopState{
		iteration:             2,
		lastReflectDecision:   "continue",
		lastReflectConfidence: 0.42,
		lastReflectIteration:  1,
	})
	if lowConfContinue.maxCallsPerIteration >= toolLoopMaxCallsPerIteration {
		t.Fatalf("expected low-confidence continue to tighten calls, got %d", lowConfContinue.maxCallsPerIteration)
	}

	highConfContinue := al.computeToolLoopBudget(toolLoopState{
		iteration:             2,
		lastReflectDecision:   "continue",
		lastReflectConfidence: 0.91,
		lastReflectIteration:  1,
	})
	if highConfContinue.maxCallsPerIteration <= toolLoopMaxCallsPerIteration {
		t.Fatalf("expected high-confidence continue to widen calls, got %d", highConfContinue.maxCallsPerIteration)
	}

	blocked := al.computeToolLoopBudget(toolLoopState{
		iteration:             2,
		lastReflectDecision:   "blocked",
		lastReflectConfidence: 0.8,
		lastReflectIteration:  1,
	})
	if blocked.maxCallsPerIteration != toolLoopMinCallsPerIteration {
		t.Fatalf("expected blocked reflection to force min calls, got %d", blocked.maxCallsPerIteration)
	}
}

func TestParallelSafeToolDeclarationOverridesWhitelist(t *testing.T) {
	t.Parallel()

	yes := true
	no := false
	reg := tools.NewToolRegistry()
	reg.Register(replayTool{name: "read_file", parallelSafe: &no})
	reg.Register(replayTool{name: "custom_safe", parallelSafe: &yes})

	al := &AgentLoop{
		tools: reg,
		parallelSafeTools: map[string]struct{}{
			"read_file": {},
		},
	}

	if al.isParallelSafeTool("read_file") {
		t.Fatalf("tool declaration should override whitelist to false")
	}
	if !al.isParallelSafeTool("custom_safe") {
		t.Fatalf("tool declaration true should be respected")
	}
}

func TestClassifyToolExecutionError(t *testing.T) {
	t.Parallel()

	typ, retryable, blocked := classifyToolExecutionError(fmt.Errorf("permission denied to write file"), false)
	if typ != "permission" || retryable || !blocked {
		t.Fatalf("unexpected permission classification: %s %v %v", typ, retryable, blocked)
	}

	typ, retryable, blocked = classifyToolExecutionError(fmt.Errorf("temporary unavailable 503"), false)
	if typ != "transient" || !retryable || blocked {
		t.Fatalf("unexpected transient classification: %s %v %v", typ, retryable, blocked)
	}
}

func TestSummarizeToolActOutcome(t *testing.T) {
	t.Parallel()

	out := summarizeToolActOutcome(toolActOutcome{
		executedCalls: 1,
		records: []toolExecutionRecord{
			{Tool: "shell", Status: "error", ErrorType: "permission", Retryable: false},
		},
		hardErrors:    1,
		blockedLikely: true,
	})
	if out == "" || !strings.Contains(out, "\"blocked_likely\":true") {
		t.Fatalf("unexpected summary: %s", out)
	}
	if !strings.Contains(out, "\"error_type\":\"permission\"") {
		t.Fatalf("missing record fields in summary: %s", out)
	}
	if !strings.Contains(out, "\"records_truncated\":0") {
		t.Fatalf("expected records_truncated field, got: %s", out)
	}
}

func TestShouldPersistToolResultRecord(t *testing.T) {
	t.Parallel()

	if !shouldPersistToolResultRecord(toolExecutionRecord{Status: "ok"}, 0, 3) {
		t.Fatalf("first tool result should persist")
	}
	if !shouldPersistToolResultRecord(toolExecutionRecord{Status: "ok"}, 2, 3) {
		t.Fatalf("last tool result should persist")
	}
	if shouldPersistToolResultRecord(toolExecutionRecord{Status: "ok"}, 1, 3) {
		t.Fatalf("middle successful tool result should be skipped")
	}
	if !shouldPersistToolResultRecord(toolExecutionRecord{Status: "error"}, 1, 3) {
		t.Fatalf("error tool result should persist")
	}
}

func TestCompactToolExecutionRecords(t *testing.T) {
	t.Parallel()

	records := []toolExecutionRecord{
		{Tool: "a", Status: "ok"},
		{Tool: "b", Status: "error", ErrorType: "permission"},
		{Tool: "c", Status: "ok"},
		{Tool: "d", Status: "error", ErrorType: "transient"},
		{Tool: "e", Status: "ok"},
		{Tool: "f", Status: "ok"},
	}
	out, truncated := compactToolExecutionRecords(records, 4)
	if len(out) != 4 {
		t.Fatalf("expected compact len 4, got %d", len(out))
	}
	if truncated != 2 {
		t.Fatalf("expected truncated 2, got %d", truncated)
	}
	foundErr := 0
	for _, r := range out {
		if r.Status == "error" {
			foundErr++
		}
	}
	if foundErr < 2 {
		t.Fatalf("expected to keep error records, got %d", foundErr)
	}
}

func TestShouldRunToolCallsInParallel(t *testing.T) {
	t.Parallel()

	al := &AgentLoop{
		parallelSafeTools: map[string]struct{}{
			"read_file":     {},
			"memory_search": {},
		},
	}
	ok := al.shouldRunToolCallsInParallel([]providers.ToolCall{
		{Name: "read_file"}, {Name: "memory_search"},
	})
	if !ok {
		t.Fatalf("expected parallel-safe tools to run in parallel")
	}

	notOK := al.shouldRunToolCallsInParallel([]providers.ToolCall{
		{Name: "read_file"}, {Name: "shell"},
	})
	if notOK {
		t.Fatalf("expected mixed tool set to stay serial")
	}
}

func TestActToolCalls_ParallelExecutionForSafeTools(t *testing.T) {
	t.Parallel()

	var active int32
	var maxActive int32
	probe := func() {
		cur := atomic.AddInt32(&active, 1)
		for {
			old := atomic.LoadInt32(&maxActive)
			if cur <= old || atomic.CompareAndSwapInt32(&maxActive, old, cur) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
		atomic.AddInt32(&active, -1)
	}

	reg := tools.NewToolRegistry()
	reg.Register(replayToolImpl{name: "read_file", run: func(ctx context.Context, args map[string]interface{}) (string, error) {
		probe()
		return "ok", nil
	}})
	reg.Register(replayToolImpl{name: "memory_search", run: func(ctx context.Context, args map[string]interface{}) (string, error) {
		probe()
		return "ok", nil
	}})

	al := &AgentLoop{
		tools:             reg,
		sessions:          session.NewSessionManager(""),
		parallelSafeTools: map[string]struct{}{"read_file": {}, "memory_search": {}},
		maxParallelCalls:  2,
	}
	msgs := []providers.Message{}
	calls := []providers.ToolCall{
		{ID: "1", Name: "read_file", Arguments: map[string]interface{}{}},
		{ID: "2", Name: "memory_search", Arguments: map[string]interface{}{}},
	}

	al.actToolCalls(context.Background(), "", calls, &msgs, "s1", 1, toolLoopBudget{
		maxCallsPerIteration: 2,
		singleCallTimeout:    2 * time.Second,
		maxActDuration:       2 * time.Second,
	}, false, nil)

	if atomic.LoadInt32(&maxActive) < 2 {
		t.Fatalf("expected concurrent execution, maxActive=%d", maxActive)
	}
}

func TestActToolCalls_ResourceConflictForcesSerial(t *testing.T) {
	t.Parallel()

	var active int32
	var maxActive int32
	probe := func() {
		cur := atomic.AddInt32(&active, 1)
		for {
			old := atomic.LoadInt32(&maxActive)
			if cur <= old || atomic.CompareAndSwapInt32(&maxActive, old, cur) {
				break
			}
		}
		time.Sleep(35 * time.Millisecond)
		atomic.AddInt32(&active, -1)
	}

	yes := true
	reg := tools.NewToolRegistry()
	reg.Register(replayTool{
		name:         "read_file",
		parallelSafe: &yes,
		resourceKeys: func(args map[string]interface{}) []string { return []string{"fs:/tmp/a"} },
		run: func(ctx context.Context, args map[string]interface{}) (string, error) {
			probe()
			return "ok", nil
		},
	})
	reg.Register(replayTool{
		name:         "memory_search",
		parallelSafe: &yes,
		resourceKeys: func(args map[string]interface{}) []string { return []string{"fs:/tmp/a"} },
		run: func(ctx context.Context, args map[string]interface{}) (string, error) {
			probe()
			return "ok", nil
		},
	})

	al := &AgentLoop{
		tools:             reg,
		sessions:          session.NewSessionManager(""),
		parallelSafeTools: map[string]struct{}{"read_file": {}, "memory_search": {}},
		maxParallelCalls:  2,
	}

	msgs := []providers.Message{}
	calls := []providers.ToolCall{
		{ID: "1", Name: "read_file", Arguments: map[string]interface{}{}},
		{ID: "2", Name: "memory_search", Arguments: map[string]interface{}{}},
	}
	al.actToolCalls(context.Background(), "", calls, &msgs, "s1", 1, toolLoopBudget{
		maxCallsPerIteration: 2,
		singleCallTimeout:    2 * time.Second,
		maxActDuration:       2 * time.Second,
	}, false, nil)

	if atomic.LoadInt32(&maxActive) > 1 {
		t.Fatalf("expected serial execution on same resource key, maxActive=%d", maxActive)
	}
}

func TestLoadToolParallelPolicyFromConfig(t *testing.T) {
	t.Parallel()

	allowed, maxCalls := loadToolParallelPolicyFromConfig(config.RuntimeControlConfig{
		ToolParallelSafeNames: []string{"Read_File", "memory_search"},
		ToolMaxParallelCalls:  3,
	})
	if maxCalls != 3 {
		t.Fatalf("unexpected max calls: %d", maxCalls)
	}
	if _, ok := allowed["read_file"]; !ok {
		t.Fatalf("expected normalized read_file in allowed set")
	}
}

func TestShouldRunFinalizePolish(t *testing.T) {
	t.Parallel()

	short := "done"
	if shouldRunFinalizePolish(short) {
		t.Fatalf("short draft should skip polish")
	}

	longButFlat := strings.Repeat("a", finalizeDraftMinCharsForPolish+10)
	if shouldRunFinalizePolish(longButFlat) {
		t.Fatalf("flat draft should skip polish")
	}

	longStructured := "1. Step one: check environment variables and baseline configs.\n2. Step two: apply fix and rerun validations.\nNext: verify rollout and provide follow-up actions."
	if !shouldRunFinalizePolish(longStructured) {
		t.Fatalf("structured draft should trigger polish")
	}
}

func TestLocalFinalizeDraftQualityScore(t *testing.T) {
	t.Parallel()

	high := localFinalizeDraftQualityScore("1. Step one: inspect environment.\n2. Step two: apply fix.\nNext steps: validate rollout and summarize conclusions.")
	low := localFinalizeDraftQualityScore("todo\ntodo\ntodo")
	if high <= low {
		t.Fatalf("expected high-quality score > low-quality score, got %.2f <= %.2f", high, low)
	}
	if high < 0.30 {
		t.Fatalf("unexpectedly low high-quality score: %.2f", high)
	}
}

func TestClamp01(t *testing.T) {
	t.Parallel()

	if got := clamp01(-0.1); got != 0 {
		t.Fatalf("expected 0, got %v", got)
	}
	if got := clamp01(1.2); got != 1 {
		t.Fatalf("expected 1, got %v", got)
	}
}

func TestInferLocalReflectionSignal(t *testing.T) {
	t.Parallel()

	blocked := inferLocalReflectionSignal([]providers.Message{
		{Role: "tool", Content: "Error: permission denied"},
		{Role: "tool", Content: "Error: permission denied"},
	})
	if blocked.decision != "blocked" || blocked.uncertain {
		t.Fatalf("expected blocked deterministic signal, got %+v", blocked)
	}

	done := inferLocalReflectionSignal([]providers.Message{
		{Role: "tool", Content: "success: completed ok"},
	})
	if done.decision != "done" || done.uncertain {
		t.Fatalf("expected done deterministic signal, got %+v", done)
	}

	unknown := inferLocalReflectionSignal([]providers.Message{
		{Role: "tool", Content: "partial result"},
	})
	if unknown.decision != "continue" || !unknown.uncertain {
		t.Fatalf("expected uncertain continue signal, got %+v", unknown)
	}
}

func TestShouldForceSelfRepairHeuristic(t *testing.T) {
	t.Parallel()

	needs, prompt := shouldForceSelfRepairHeuristic("Please provide steps to fix this", "It should work.")
	if !needs || strings.TrimSpace(prompt) == "" {
		t.Fatalf("expected self-repair for missing structured steps")
	}

	needs, _ = shouldForceSelfRepairHeuristic("summarize logs", "Here is summary.")
	if needs {
		t.Fatalf("did not expect repair for normal concise response")
	}
}

func TestShouldRetryAfterDeferralNoTools(t *testing.T) {
	t.Parallel()

	if !shouldRetryAfterDeferralNoTools("需要先查看一下当前工作区才能确认，请稍等。", 1, false, false, false) {
		t.Fatalf("expected deferral text to trigger retry")
	}
	if shouldRetryAfterDeferralNoTools("这里是直接答案。", 1, false, false, false) {
		t.Fatalf("did not expect normal direct answer to trigger retry")
	}
	if shouldRetryAfterDeferralNoTools("需要先查看一下当前工作区才能确认，请稍等。", 2, false, false, false) {
		t.Fatalf("did not expect retry after first iteration")
	}
}

func TestRunLLMToolLoop_RecoversFromDeferralWithoutTools(t *testing.T) {
	t.Parallel()

	var toolExecCount int32
	reg := tools.NewToolRegistry()
	reg.Register(replayToolImpl{
		name: "read_file",
		run: func(ctx context.Context, args map[string]interface{}) (string, error) {
			atomic.AddInt32(&toolExecCount, 1)
			return "README content", nil
		},
	})

	provider := &deferralRetryProvider{}
	al := &AgentLoop{
		provider:         provider,
		providersByProxy: map[string]providers.LLMProvider{"proxy": provider},
		modelsByProxy:    map[string][]string{"proxy": []string{"test-model"}},
		proxy:            "proxy",
		model:            "test-model",
		maxIterations:    5,
		llmCallTimeout:   3 * time.Second,
		tools:            reg,
		sessions:         session.NewSessionManager(""),
		workspace:        t.TempDir(),
	}

	msgs := []providers.Message{
		{Role: "system", Content: "test system"},
		{Role: "user", Content: "当前状态"},
	}

	out, iterations, err := al.runLLMToolLoop(context.Background(), msgs, "deferral:test", false, nil)
	if err != nil {
		t.Fatalf("runLLMToolLoop error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("expected non-empty output")
	}
	if provider.planCalls < 3 {
		t.Fatalf("expected additional planning round after deferral, got planCalls=%d", provider.planCalls)
	}
	if atomic.LoadInt32(&toolExecCount) == 0 {
		t.Fatalf("expected tool execution after deferral recovery")
	}
	if iterations < 3 {
		t.Fatalf("expected at least 3 iterations, got %d", iterations)
	}
}

func TestSelfRepairMemoryPromptDedup(t *testing.T) {
	t.Parallel()

	mem := selfRepairMemory{
		promptsUsed: map[string]struct{}{
			normalizeRepairPrompt("Provide structured step-by-step answer."): {},
		},
	}
	if !promptSeen(mem, "provide structured step-by-step answer.") {
		t.Fatalf("expected prompt to be detected as already used")
	}
	if promptSeen(mem, "different prompt") {
		t.Fatalf("did not expect unrelated prompt to be marked used")
	}
}
