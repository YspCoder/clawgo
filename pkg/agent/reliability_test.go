package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/lifecycle"
	"github.com/YspCoder/clawgo/pkg/providers"
	"github.com/YspCoder/clawgo/pkg/session"
	toolspkg "github.com/YspCoder/clawgo/pkg/tools"
)

type pressureProvider struct {
	tokens int
}

func (p *pressureProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	return &providers.LLMResponse{Content: "Key Facts\n- compacted", FinishReason: "stop"}, nil
}

func (p *pressureProvider) GetDefaultModel() string { return "pressure-model" }

func (p *pressureProvider) CountTokens(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.UsageInfo, error) {
	return &providers.UsageInfo{PromptTokens: p.tokens, TotalTokens: p.tokens}, nil
}

type fallbackStreamingProvider struct {
	stream func(ctx context.Context, onDelta func(string)) (*providers.LLMResponse, error)
	chat   func(ctx context.Context) (*providers.LLMResponse, error)
}

func (p *fallbackStreamingProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	return p.chat(ctx)
}

func (p *fallbackStreamingProvider) ChatStream(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}, onDelta func(string)) (*providers.LLMResponse, error) {
	return p.stream(ctx, onDelta)
}

func (p *fallbackStreamingProvider) GetDefaultModel() string { return "stream-model" }

type asyncCompactionProvider struct {
	mu       sync.Mutex
	started  chan int
	release  chan struct{}
	finished chan int
	calls    int
}

func (p *asyncCompactionProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	if p.started != nil {
		p.started <- call
	}
	if p.release != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-p.release:
		}
	}
	if p.finished != nil {
		p.finished <- call
	}
	return &providers.LLMResponse{Content: "Key Facts\n- compacted", FinishReason: "stop"}, nil
}

func (p *asyncCompactionProvider) GetDefaultModel() string { return "async-model" }

func TestCompactSessionTriggeredByTokenPressure(t *testing.T) {
	t.Parallel()

	sm := session.NewSessionManager(t.TempDir())
	key := "cli:pressure"
	for _, content := range []string{"one", "two", "three", "four", "five", "six"} {
		sm.AddMessage(key, "user", content)
	}
	provider := &pressureProvider{tokens: 900}
	loop := &AgentLoop{
		provider:                     provider,
		model:                        provider.GetDefaultModel(),
		maxTokens:                    1000,
		providerNames:                []string{"pressure"},
		sessions:                     sm,
		compactionEnabled:            true,
		compactionTrigger:            100,
		compactionProtectLastN:       2,
		compactionKeepRecent:         2,
		compactionTargetRatio:        0.35,
		compactionPressureThreshold:  0.8,
		compactionMaxSummaryChars:    6000,
		compactionMaxTranscriptChars: 20000,
	}

	applied, _, _ := loop.compactSessionIfNeeded(context.Background(), key)
	if !applied {
		t.Fatal("expected compaction to apply")
	}

	history := sm.GetPromptHistory(key)
	if len(history) != 3 {
		t.Fatalf("expected ratio-based keep count 3, got %d", len(history))
	}
	if history[0].Content != "four" || history[2].Content != "six" {
		t.Fatalf("expected tail messages preserved, got %#v", history)
	}
	if summary := sm.GetSummary(key); summary == "" {
		t.Fatal("expected compaction summary to be written")
	}
}

func TestFinalizeUserMessageDoesNotWaitForCompaction(t *testing.T) {
	t.Parallel()

	sm := session.NewSessionManager(t.TempDir())
	key := "cli:async"
	for _, content := range []string{"one", "two", "three", "four", "five", "six"} {
		sm.AddMessage(key, "user", content)
	}
	provider := &asyncCompactionProvider{
		started: make(chan int, 2),
		release: make(chan struct{}),
	}
	loop := &AgentLoop{
		provider:                     provider,
		model:                        provider.GetDefaultModel(),
		sessions:                     sm,
		compactionEnabled:            true,
		compactionTrigger:            4,
		compactionProtectLastN:       2,
		compactionKeepRecent:         2,
		compactionTargetRatio:        0.35,
		compactionPressureThreshold:  0.1,
		compactionMaxSummaryChars:    6000,
		compactionMaxTranscriptChars: 20000,
		compactionRunner:             lifecycle.NewLoopRunner(),
		compactionSignal:             make(chan struct{}, 1),
		compactionQueued:             map[string]struct{}{},
		compactionInflight:           map[string]struct{}{},
		compactionDirty:              map[string]struct{}{},
	}
	t.Cleanup(loop.Stop)

	start := time.Now()
	loop.finalizeUserMessage(key, "en", nil, "final")
	if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
		t.Fatalf("expected finalizeUserMessage to return quickly, took %s", elapsed)
	}
	select {
	case <-provider.started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected async compaction to start in background")
	}
	close(provider.release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if summary := sm.GetSummary(key); summary != "" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("expected async compaction summary to be written")
}

func TestCompactionWorkerRetriesDirtySessionWithoutLosingNewMessages(t *testing.T) {
	t.Parallel()

	sm := session.NewSessionManager(t.TempDir())
	key := "cli:dirty"
	for _, content := range []string{"one", "two", "three", "four", "five", "six"} {
		sm.AddMessage(key, "user", content)
	}
	provider := &asyncCompactionProvider{
		started:  make(chan int, 4),
		release:  make(chan struct{}, 4),
		finished: make(chan int, 4),
	}
	loop := &AgentLoop{
		provider:                     provider,
		model:                        provider.GetDefaultModel(),
		sessions:                     sm,
		compactionEnabled:            true,
		compactionTrigger:            4,
		compactionProtectLastN:       2,
		compactionKeepRecent:         2,
		compactionTargetRatio:        0.35,
		compactionPressureThreshold:  0.1,
		compactionMaxSummaryChars:    6000,
		compactionMaxTranscriptChars: 20000,
		compactionRunner:             lifecycle.NewLoopRunner(),
		compactionSignal:             make(chan struct{}, 1),
		compactionQueued:             map[string]struct{}{},
		compactionInflight:           map[string]struct{}{},
		compactionDirty:              map[string]struct{}{},
	}
	t.Cleanup(loop.Stop)

	loop.enqueueSessionCompaction(key)
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("expected first compaction run to start")
	}

	sm.AddMessage(key, "assistant", "seven")
	loop.enqueueSessionCompaction(key)
	provider.release <- struct{}{}

	select {
	case <-provider.finished:
	case <-time.After(time.Second):
		t.Fatal("expected first compaction run to finish")
	}
	select {
	case call := <-provider.started:
		if call != 2 {
			t.Fatalf("expected second compaction attempt after dirty retry, got call %d", call)
		}
	case <-time.After(time.Second):
		t.Fatal("expected dirty session to trigger a second compaction run")
	}
	provider.release <- struct{}{}
	select {
	case <-provider.finished:
	case <-time.After(time.Second):
		t.Fatal("expected second compaction run to finish")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		history := sm.GetPromptHistory(key)
		if len(history) > 0 && history[len(history)-1].Content == "seven" && sm.GetSummary(key) != "" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("expected retried compaction to preserve new message and summary")
}

func TestRequestStreamingLLMResponseFallsBackBeforeFirstDelta(t *testing.T) {
	t.Parallel()

	provider := &fallbackStreamingProvider{
		stream: func(ctx context.Context, onDelta func(string)) (*providers.LLMResponse, error) {
			<-ctx.Done()
			return nil, providers.NewProviderExecutionError("stream_stale", "stream stale", "stream", true, "test")
		},
		chat: func(ctx context.Context) (*providers.LLMResponse, error) {
			return &providers.LLMResponse{Content: "fallback", FinishReason: "stop"}, nil
		},
	}
	loop := &AgentLoop{
		bus:             bus.NewMessageBus(),
		sessionStreamed: map[string]bool{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	resp, attempts, err := loop.requestStreamingLLMResponse(llmTurnLoopConfig{
		ctx:             ctx,
		sessionKey:      "cli:test",
		toolChannel:     "telegram",
		toolChatID:      "chat",
		enableStreaming: true,
	}, provider, provider, provider.GetDefaultModel(), []providers.Message{{Role: "user", Content: "hello"}}, nil, nil)
	if err != nil {
		t.Fatalf("expected fallback success, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected streaming + fallback attempts, got %d", attempts)
	}
	if resp == nil || resp.Content != "fallback" {
		t.Fatalf("unexpected fallback response: %#v", resp)
	}
}

func TestRequestStreamingLLMResponseDoesNotFallbackAfterDelta(t *testing.T) {
	t.Parallel()

	provider := &fallbackStreamingProvider{
		stream: func(ctx context.Context, onDelta func(string)) (*providers.LLMResponse, error) {
			onDelta("partial")
			return nil, providers.NewProviderExecutionError("stream_failed", "stream failed", "stream", true, "test")
		},
		chat: func(ctx context.Context) (*providers.LLMResponse, error) {
			return &providers.LLMResponse{Content: "fallback", FinishReason: "stop"}, nil
		},
	}
	loop := &AgentLoop{
		bus:             bus.NewMessageBus(),
		sessionStreamed: map[string]bool{},
	}

	resp, attempts, err := loop.requestStreamingLLMResponse(llmTurnLoopConfig{
		ctx:             context.Background(),
		sessionKey:      "cli:test",
		toolChannel:     "telegram",
		toolChatID:      "chat",
		enableStreaming: true,
	}, provider, provider, provider.GetDefaultModel(), []providers.Message{{Role: "user", Content: "hello"}}, nil, nil)
	if err == nil {
		t.Fatal("expected stream failure without fallback")
	}
	if attempts != 1 {
		t.Fatalf("expected single streaming attempt, got %d", attempts)
	}
	if resp != nil {
		t.Fatalf("expected nil response on post-delta stream failure, got %#v", resp)
	}
}

func TestRunLLMTurnLoopReturnsRetryLimitError(t *testing.T) {
	t.Parallel()

	provider := &sequenceProvider{
		responses: []*providers.LLMResponse{{
			Content: "",
			ToolCalls: []providers.ToolCall{
				{ID: "tool-1", Name: "system_info", Arguments: map[string]interface{}{}},
			},
			FinishReason: "tool_calls",
		}},
	}
	loop := &AgentLoop{
		provider:        provider,
		model:           provider.GetDefaultModel(),
		maxIterations:   1,
		tools:           toolspkg.NewToolRegistry(),
		providerNames:   []string{"sequence"},
		sessionProvider: map[string]string{},
	}
	loop.tools.Register(toolspkg.NewSystemInfoTool())
	_, err := loop.runLLMTurnLoop(llmTurnLoopConfig{
		ctx:        context.Background(),
		sessionKey: "cli:test",
		messages:   []providers.Message{{Role: "user", Content: "hello"}},
	})
	if err == nil || !strings.Contains(err.Error(), "max tool iterations exceeded") {
		t.Fatalf("expected retry limit error, got %v", err)
	}
}
