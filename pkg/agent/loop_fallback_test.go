package agent

import (
	"context"
	"fmt"
	"testing"

	"clawgo/pkg/providers"
)

type fallbackTestProvider struct {
	byModel map[string]fallbackResult
	called  []string
}

type fallbackResult struct {
	resp *providers.LLMResponse
	err  error
}

func (p *fallbackTestProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	p.called = append(p.called, model)
	if r, ok := p.byModel[model]; ok {
		return r.resp, r.err
	}
	return nil, fmt.Errorf("unexpected model: %s", model)
}

func (p *fallbackTestProvider) GetDefaultModel() string {
	return ""
}

func TestCallLLMWithModelFallback_RetriesOnUnknownProvider(t *testing.T) {
	p := &fallbackTestProvider{
		byModel: map[string]fallbackResult{
			"gemini-3-flash": {err: fmt.Errorf(`API error (status 502): {"error":{"message":"unknown provider for model gemini-3-flash"}}`)},
			"gpt-4o-mini":    {resp: &providers.LLMResponse{Content: "ok"}},
		},
	}

	al := &AgentLoop{
		provider:       p,
		model:          "gemini-3-flash",
		modelFallbacks: []string{"gpt-4o-mini"},
	}

	resp, err := al.callLLMWithModelFallback(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if len(p.called) != 2 {
		t.Fatalf("expected 2 model attempts, got %d (%v)", len(p.called), p.called)
	}
	if p.called[0] != "gemini-3-flash" || p.called[1] != "gpt-4o-mini" {
		t.Fatalf("unexpected model order: %v", p.called)
	}
	if al.model != "gpt-4o-mini" {
		t.Fatalf("expected model switch to fallback, got %q", al.model)
	}
}

func TestCallLLMWithModelFallback_NoRetryOnNonRetryableError(t *testing.T) {
	p := &fallbackTestProvider{
		byModel: map[string]fallbackResult{
			"gemini-3-flash": {err: fmt.Errorf("API error (status 500): internal server error")},
		},
	}

	al := &AgentLoop{
		provider:       p,
		model:          "gemini-3-flash",
		modelFallbacks: []string{"gpt-4o-mini"},
	}

	_, err := al.callLLMWithModelFallback(context.Background(), nil, nil, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(p.called) != 1 {
		t.Fatalf("expected single model attempt, got %d (%v)", len(p.called), p.called)
	}
}

func TestShouldRetryWithFallbackModel_UnknownProviderError(t *testing.T) {
	err := fmt.Errorf(`API error (status 502): {"error":{"message":"unknown provider for model gemini-3-flash","type":"servererror"}}`)
	if !shouldRetryWithFallbackModel(err) {
		t.Fatalf("expected unknown provider error to trigger fallback retry")
	}
}
