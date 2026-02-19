package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"clawgo/pkg/providers"
	"clawgo/pkg/session"
	"clawgo/pkg/tools"
)

type replayScenario string

const (
	replayDirectSuccess    replayScenario = "direct_success"
	replayOneToolSuccess   replayScenario = "one_tool_success"
	replayRepeatedToolCall replayScenario = "repeated_tool_call"
	replayTransientFailure replayScenario = "transient_failure"
	replayPermissionBlock  replayScenario = "permission_block"
)

type replayProvider struct {
	mu            sync.Mutex
	scenario      replayScenario
	planCalls     int
	reflectCalls  int
	finalizeCalls int
	polishCalls   int
	totalCalls    int
}

func (p *replayProvider) Chat(ctx context.Context, messages []providers.Message, defs []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.totalCalls++

	last := ""
	if len(messages) > 0 {
		last = strings.TrimSpace(messages[len(messages)-1].Content)
	}

	// Phase-2 polish call.
	if len(defs) == 0 && len(messages) > 0 && strings.Contains(strings.ToLower(strings.TrimSpace(messages[0].Content)), "rewrite the draft answer for end users") {
		p.polishCalls++
		return &providers.LLMResponse{Content: "polished final response"}, nil
	}

	// Reflection call.
	if len(defs) == 0 && strings.Contains(last, "Classify current execution progress using JSON only.") {
		p.reflectCalls++
		switch p.scenario {
		case replayTransientFailure:
			return &providers.LLMResponse{Content: `{"decision":"continue","reason":"transient failures may recover","confidence":0.74}`}, nil
		case replayRepeatedToolCall:
			return &providers.LLMResponse{Content: `{"decision":"continue","reason":"need another attempt","confidence":0.72}`}, nil
		default:
			return &providers.LLMResponse{Content: `{"decision":"continue","reason":"default continue","confidence":0.60}`}, nil
		}
	}

	// Finalization draft call.
	if len(defs) == 0 {
		p.finalizeCalls++
		return &providers.LLMResponse{Content: "draft final response"}, nil
	}

	// Planning/tool-loop calls.
	p.planCalls++
	switch p.scenario {
	case replayDirectSuccess:
		return &providers.LLMResponse{Content: "direct completed"}, nil
	case replayOneToolSuccess:
		if p.planCalls == 1 {
			return &providers.LLMResponse{
				Content: "call one tool",
				ToolCalls: []providers.ToolCall{
					{ID: "tc-1", Name: "ok_tool", Arguments: map[string]interface{}{"x": 1}},
				},
			}, nil
		}
		return &providers.LLMResponse{Content: "task completed after one tool"}, nil
	case replayRepeatedToolCall:
		return &providers.LLMResponse{
			Content: "repeating tool",
			ToolCalls: []providers.ToolCall{
				{ID: fmt.Sprintf("tc-r-%d", p.planCalls), Name: "ok_tool", Arguments: map[string]interface{}{"same": true}},
			},
		}, nil
	case replayTransientFailure:
		return &providers.LLMResponse{
			Content: "transient fail tool",
			ToolCalls: []providers.ToolCall{
				{ID: fmt.Sprintf("tc-t-%d", p.planCalls), Name: "fail_tool_transient", Arguments: map[string]interface{}{}},
			},
		}, nil
	case replayPermissionBlock:
		return &providers.LLMResponse{
			Content: "permission fail tool",
			ToolCalls: []providers.ToolCall{
				{ID: "tc-p-1", Name: "fail_tool_permission", Arguments: map[string]interface{}{}},
			},
		}, nil
	default:
		return &providers.LLMResponse{Content: "unexpected scenario"}, nil
	}
}

func (p *replayProvider) GetDefaultModel() string { return "test-model" }

type replayToolImpl struct {
	name string
	run  func(context.Context, map[string]interface{}) (string, error)
}

func (t replayToolImpl) Name() string        { return t.name }
func (t replayToolImpl) Description() string { return "replay tool" }
func (t replayToolImpl) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}
func (t replayToolImpl) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	return t.run(ctx, args)
}

type replayCaseResult struct {
	name         replayScenario
	ok           bool
	iterations   int
	llmCalls     int
	reflectCalls int
}

func TestAgentLoopReplayBaseline(t *testing.T) {
	t.Parallel()

	scenarios := []replayScenario{
		replayDirectSuccess,
		replayOneToolSuccess,
		replayRepeatedToolCall,
		replayTransientFailure,
		replayPermissionBlock,
	}

	results := make([]replayCaseResult, 0, len(scenarios))
	for _, sc := range scenarios {
		sc := sc
		t.Run(string(sc), func(t *testing.T) {
			reg := tools.NewToolRegistry()
			reg.Register(replayToolImpl{
				name: "ok_tool",
				run: func(ctx context.Context, args map[string]interface{}) (string, error) {
					return "ok", nil
				},
			})
			reg.Register(replayToolImpl{
				name: "fail_tool_transient",
				run: func(ctx context.Context, args map[string]interface{}) (string, error) {
					return "", fmt.Errorf("temporary unavailable 503")
				},
			})
			reg.Register(replayToolImpl{
				name: "fail_tool_permission",
				run: func(ctx context.Context, args map[string]interface{}) (string, error) {
					return "", fmt.Errorf("permission denied")
				},
			})

			provider := &replayProvider{scenario: sc}
			al := &AgentLoop{
				provider:         provider,
				providersByProxy: map[string]providers.LLMProvider{"proxy": provider},
				modelsByProxy:    map[string][]string{"proxy": {"test-model"}},
				proxy:            "proxy",
				model:            "test-model",
				maxIterations:    6,
				llmCallTimeout:   3 * time.Second,
				tools:            reg,
				sessions:         session.NewSessionManager(""),
				workspace:        t.TempDir(),
			}

			msgs := []providers.Message{
				{Role: "system", Content: "you are a test agent"},
				{Role: "user", Content: "complete task"},
			}

			out, iterations, err := al.runLLMToolLoop(context.Background(), msgs, "replay:"+string(sc), false, nil)
			if err != nil {
				t.Fatalf("runLLMToolLoop error: %v", err)
			}
			if strings.TrimSpace(out) == "" {
				t.Fatalf("empty output")
			}
			results = append(results, replayCaseResult{
				name:         sc,
				ok:           true,
				iterations:   iterations,
				llmCalls:     provider.totalCalls,
				reflectCalls: provider.reflectCalls,
			})
		})
	}

	total := len(results)
	if total != len(scenarios) {
		t.Fatalf("unexpected results count: %d", total)
	}
	success := 0
	iterSum := 0
	llmSum := 0
	reflectSum := 0
	for _, r := range results {
		if r.ok {
			success++
		}
		iterSum += r.iterations
		llmSum += r.llmCalls
		reflectSum += r.reflectCalls
	}
	successRate := float64(success) / float64(total)
	avgIter := float64(iterSum) / float64(total)
	avgLLM := float64(llmSum) / float64(total)
	avgReflect := float64(reflectSum) / float64(total)

	t.Logf("Replay baseline: success_rate=%.2f avg_iterations=%.2f avg_llm_calls=%.2f avg_reflect_calls=%.2f", successRate, avgIter, avgLLM, avgReflect)

	if successRate < 1.0 {
		t.Fatalf("expected all scenarios to succeed, got success_rate=%.2f", successRate)
	}
	if avgIter > 3.6 {
		t.Fatalf("avg_iterations too high: %.2f", avgIter)
	}
	if avgLLM > 6.0 {
		t.Fatalf("avg_llm_calls too high: %.2f", avgLLM)
	}
}
