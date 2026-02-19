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
		provider: p,
		proxy:    "proxy",
		model:    "gemini-3-flash",
		providersByProxy: map[string]providers.LLMProvider{
			"proxy": p,
		},
		modelsByProxy: map[string][]string{
			"proxy": []string{"gemini-3-flash", "gpt-4o-mini"},
		},
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

func TestCallLLMWithModelFallback_RetriesOnGateway502(t *testing.T) {
	p := &fallbackTestProvider{
		byModel: map[string]fallbackResult{
			"gemini-3-flash": {err: fmt.Errorf("API error (status 502, content-type \"text/html\"): <html>bad gateway</html>")},
			"gpt-4o-mini":    {resp: &providers.LLMResponse{Content: "ok"}},
		},
	}

	al := &AgentLoop{
		provider: p,
		proxy:    "proxy",
		model:    "gemini-3-flash",
		providersByProxy: map[string]providers.LLMProvider{
			"proxy": p,
		},
		modelsByProxy: map[string][]string{
			"proxy": []string{"gemini-3-flash", "gpt-4o-mini"},
		},
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
}

func TestCallLLMWithModelFallback_RetriesOnGateway524(t *testing.T) {
	p := &fallbackTestProvider{
		byModel: map[string]fallbackResult{
			"gemini-3-flash": {err: fmt.Errorf("API error (status 524, content-type \"text/plain; charset=UTF-8\"): error code: 524")},
			"gpt-4o-mini":    {resp: &providers.LLMResponse{Content: "ok"}},
		},
	}

	al := &AgentLoop{
		provider: p,
		proxy:    "proxy",
		model:    "gemini-3-flash",
		providersByProxy: map[string]providers.LLMProvider{
			"proxy": p,
		},
		modelsByProxy: map[string][]string{
			"proxy": []string{"gemini-3-flash", "gpt-4o-mini"},
		},
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
}

func TestCallLLMWithModelFallback_RetriesOnAuthUnavailable500(t *testing.T) {
	p := &fallbackTestProvider{
		byModel: map[string]fallbackResult{
			"gemini-3-flash": {err: fmt.Errorf(`API error (status 500, content-type "application/json"): {"error":{"message":"auth_unavailable: no auth available","type":"server_error","code":"internal_server_error"}}`)},
			"gpt-4o-mini":    {resp: &providers.LLMResponse{Content: "ok"}},
		},
	}

	al := &AgentLoop{
		provider: p,
		proxy:    "proxy",
		model:    "gemini-3-flash",
		providersByProxy: map[string]providers.LLMProvider{
			"proxy": p,
		},
		modelsByProxy: map[string][]string{
			"proxy": []string{"gemini-3-flash", "gpt-4o-mini"},
		},
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
}

func TestCallLLMWithModelFallback_NoRetryOnNonRetryableError(t *testing.T) {
	p := &fallbackTestProvider{
		byModel: map[string]fallbackResult{
			"gemini-3-flash": {err: fmt.Errorf("API error (status 500): internal server error")},
		},
	}

	al := &AgentLoop{
		provider: p,
		proxy:    "proxy",
		model:    "gemini-3-flash",
		providersByProxy: map[string]providers.LLMProvider{
			"proxy": p,
		},
		modelsByProxy: map[string][]string{
			"proxy": []string{"gemini-3-flash", "gpt-4o-mini"},
		},
	}

	_, err := al.callLLMWithModelFallback(context.Background(), nil, nil, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(p.called) != 1 {
		t.Fatalf("expected single model attempt, got %d (%v)", len(p.called), p.called)
	}
}

func TestCallLLMWithModelFallback_SwitchesProxyAfterProxyModelsExhausted(t *testing.T) {
	primary := &fallbackTestProvider{
		byModel: map[string]fallbackResult{
			"gemini-3-flash": {err: fmt.Errorf(`API error (status 502): {"error":{"message":"unknown provider for model gemini-3-flash"}}`)},
			"gpt-4o-mini":    {err: fmt.Errorf(`API error (status 400): {"error":{"message":"model not found"}}`)},
		},
	}
	backup := &fallbackTestProvider{
		byModel: map[string]fallbackResult{
			"gemini-3-flash": {err: fmt.Errorf(`API error (status 400): {"error":{"message":"model not found"}}`)},
			"deepseek-chat":  {resp: &providers.LLMResponse{Content: "ok"}},
		},
	}

	al := &AgentLoop{
		proxy:          "primary",
		proxyFallbacks: []string{"backup"},
		model:          "gemini-3-flash",
		providersByProxy: map[string]providers.LLMProvider{
			"primary": primary,
			"backup":  backup,
		},
		modelsByProxy: map[string][]string{
			"primary": []string{"gemini-3-flash", "gpt-4o-mini"},
			"backup":  []string{"deepseek-chat"},
		},
	}

	resp, err := al.callLLMWithModelFallback(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if al.proxy != "backup" {
		t.Fatalf("expected proxy switch to backup, got %q", al.proxy)
	}
	if al.model != "deepseek-chat" {
		t.Fatalf("expected model switch to deepseek-chat, got %q", al.model)
	}
	if len(primary.called) != 2 {
		t.Fatalf("expected 2 model attempts in primary, got %d (%v)", len(primary.called), primary.called)
	}
	if len(backup.called) != 2 || backup.called[1] != "deepseek-chat" {
		t.Fatalf("unexpected backup attempts: %v", backup.called)
	}
}

func TestShouldRetryWithFallbackModel_UnknownProviderError(t *testing.T) {
	err := fmt.Errorf(`API error (status 502): {"error":{"message":"unknown provider for model gemini-3-flash","type":"servererror"}}`)
	if !shouldRetryWithFallbackModel(err) {
		t.Fatalf("expected unknown provider error to trigger fallback retry")
	}
}

func TestShouldRetryWithFallbackModel_HTMLUnmarshalError(t *testing.T) {
	err := fmt.Errorf("failed to unmarshal response: invalid character '<' looking for beginning of value")
	if !shouldRetryWithFallbackModel(err) {
		t.Fatalf("expected HTML parse error to trigger fallback retry")
	}
}

func TestShouldRetryWithFallbackModel_Gateway524Error(t *testing.T) {
	err := fmt.Errorf("API error (status 524, content-type \"text/plain; charset=UTF-8\"): error code: 524")
	if !shouldRetryWithFallbackModel(err) {
		t.Fatalf("expected 524 gateway timeout to trigger fallback retry")
	}
}

func TestShouldRetryWithFallbackModel_AuthUnavailableError(t *testing.T) {
	err := fmt.Errorf(`API error (status 500, content-type "application/json"): {"error":{"message":"auth_unavailable: no auth available","type":"server_error","code":"internal_server_error"}}`)
	if !shouldRetryWithFallbackModel(err) {
		t.Fatalf("expected auth_unavailable error to trigger fallback retry")
	}
}

func TestShouldRetryWithFallbackModel_ContextDeadlineExceeded(t *testing.T) {
	err := fmt.Errorf("failed to send request: Post \"https://v2.kkkk.dev/v1/chat/completions\": context deadline exceeded")
	if !shouldRetryWithFallbackModel(err) {
		t.Fatalf("expected context deadline exceeded to trigger fallback retry")
	}
}
