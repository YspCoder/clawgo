package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestAntigravityBuildRequestBody(t *testing.T) {
	p := NewAntigravityProvider("openai", "", "", "gemini-2.5-pro", false, "oauth", 0, nil)
	body := p.buildRequestBody([]Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "hello"},
		{
			Role:    "assistant",
			Content: "calling tool",
			ToolCalls: []ToolCall{{
				ID:   "call_1",
				Name: "lookup",
				Function: &FunctionCall{
					Name:      "lookup",
					Arguments: `{"q":"weather"}`,
				},
			}},
		},
		{Role: "tool", ToolCallID: "call_1", Content: `{"ok":true}`},
	}, []ToolDefinition{{
		Type: "function",
		Function: ToolFunctionDefinition{
			Name:        "lookup",
			Description: "Lookup data",
			Parameters: map[string]interface{}{
				"type": "object",
			},
		},
	}}, "gemini-2.5-pro", map[string]interface{}{
		"max_tokens":  256,
		"temperature": 0.2,
	}, &oauthSession{ProjectID: "demo-project"}, false)

	if got := body["project"]; got != "demo-project" {
		t.Fatalf("expected project id to be preserved, got %#v", got)
	}
	request := mapFromAny(body["request"])
	if system := asString(mapFromAny(request["systemInstruction"])["parts"].([]map[string]any)[0]["text"]); system != "You are helpful." {
		t.Fatalf("expected system instruction, got %q", system)
	}
	if got := len(request["contents"].([]map[string]any)); got != 3 {
		t.Fatalf("expected 3 content entries, got %d", got)
	}
	gen := mapFromAny(request["generationConfig"])
	if got := intValue(gen["maxOutputTokens"]); got != 256 {
		t.Fatalf("expected maxOutputTokens, got %#v", gen["maxOutputTokens"])
	}
	if got := gen["temperature"]; got != 0.2 {
		t.Fatalf("expected temperature, got %#v", got)
	}
}

func TestParseAntigravityResponse(t *testing.T) {
	raw := []byte(`{
		"response": {
			"candidates": [{
				"finishReason": "STOP",
				"content": {
					"parts": [
						{"text": "hello"},
						{"functionCall": {"id": "call_1", "name": "lookup", "args": {"q":"weather"}}}
					]
				}
			}],
			"usageMetadata": {
				"promptTokenCount": 11,
				"candidatesTokenCount": 7,
				"totalTokenCount": 18
			}
		}
	}`)
	resp, err := parseAntigravityResponse(raw)
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("expected content, got %q", resp.Content)
	}
	if resp.FinishReason != "STOP" {
		t.Fatalf("expected finish reason passthrough, got %q", resp.FinishReason)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "lookup" {
		t.Fatalf("expected tool call, got %#v", resp.ToolCalls)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 18 {
		t.Fatalf("expected usage, got %#v", resp.Usage)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(resp.ToolCalls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	if got := asString(args["q"]); got != "weather" {
		t.Fatalf("expected tool args, got %#v", args)
	}
}

func TestAntigravityProviderCountTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1internal:countTokens" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"totalTokens":42}`))
	}))
	defer server.Close()

	p := NewAntigravityProvider("antigravity", "token", server.URL, "gemini-2.5-pro", false, "api_key", 5*time.Second, nil)
	usage, err := p.CountTokens(t.Context(), []Message{{Role: "user", Content: "hello"}}, nil, "gemini-2.5-pro", nil)
	if err != nil {
		t.Fatalf("CountTokens error: %v", err)
	}
	if usage == nil || usage.PromptTokens != 42 || usage.TotalTokens != 42 {
		t.Fatalf("usage = %#v, want 42", usage)
	}
}

func TestAntigravityProviderRetriesNoCapacity(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1internal:generateContent" {
			http.NotFound(w, r)
			return
		}
		if atomic.AddInt32(&hits, 1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"no capacity available"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}}`))
	}))
	defer server.Close()

	p := NewAntigravityProvider("antigravity", "token", server.URL, "gemini-2.5-pro", false, "api_key", 5*time.Second, nil)
	resp, err := p.Chat(t.Context(), []Message{{Role: "user", Content: "hello"}}, nil, "gemini-2.5-pro", nil)
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q, want ok", resp.Content)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("hits = %d, want 2", got)
	}
}

func TestAntigravityBaseURLsIncludeProdFallback(t *testing.T) {
	p := NewAntigravityProvider("antigravity", "", "", "gemini-2.5-pro", false, "oauth", 0, nil)
	got := p.baseURLs()
	if len(got) < 3 {
		t.Fatalf("baseURLs = %#v", got)
	}
	if got[0] != antigravityDailyBaseURL || got[1] != antigravitySandboxBaseURL || got[2] != antigravityProdBaseURL {
		t.Fatalf("unexpected fallback order: %#v", got)
	}
}

func TestAntigravityBuildRequestBodyAppliesThinkingSuffix(t *testing.T) {
	p := NewAntigravityProvider("antigravity", "", "", "gemini-3-pro", false, "oauth", 0, nil)
	body := p.buildRequestBody([]Message{{Role: "user", Content: "hello"}}, nil, "gemini-3-pro(high)", nil, &oauthSession{ProjectID: "demo-project"}, false)
	if got := body["model"]; got != "gemini-3-pro" {
		t.Fatalf("model = %#v, want gemini-3-pro", got)
	}
	request := mapFromAny(body["request"])
	gen := mapFromAny(request["generationConfig"])
	thinking := mapFromAny(gen["thinkingConfig"])
	if got := asString(thinking["thinkingLevel"]); got != "high" {
		t.Fatalf("thinkingLevel = %q, want high", got)
	}
	if got := fmt.Sprintf("%v", thinking["includeThoughts"]); got != "true" {
		t.Fatalf("includeThoughts = %v, want true", thinking["includeThoughts"])
	}
}

func TestAntigravityBuildRequestBodyDisablesThinkingOutput(t *testing.T) {
	p := NewAntigravityProvider("antigravity", "", "", "gemini-2.5-pro", false, "oauth", 0, nil)
	body := p.buildRequestBody([]Message{{Role: "user", Content: "hello"}}, nil, "gemini-2.5-pro(0)", nil, &oauthSession{ProjectID: "demo-project"}, false)
	request := mapFromAny(body["request"])
	gen := mapFromAny(request["generationConfig"])
	thinking := mapFromAny(gen["thinkingConfig"])
	if got := intValue(thinking["thinkingBudget"]); got != 128 {
		t.Fatalf("thinkingBudget = %v, want 128", thinking["thinkingBudget"])
	}
	if got := fmt.Sprintf("%v", thinking["includeThoughts"]); got != "false" {
		t.Fatalf("includeThoughts = %v, want false", thinking["includeThoughts"])
	}
}

func TestAntigravityThinkingSuffixPreservesExplicitIncludeThoughts(t *testing.T) {
	p := NewAntigravityProvider("antigravity", "", "", "gemini-3-pro", false, "oauth", 0, nil)
	body := p.buildRequestBody([]Message{{Role: "user", Content: "hello"}}, nil, "gemini-3-pro(high)", map[string]interface{}{
		"gemini_generation_config": map[string]interface{}{
			"thinkingConfig": map[string]interface{}{
				"includeThoughts": false,
			},
		},
	}, &oauthSession{ProjectID: "demo-project"}, false)
	request := mapFromAny(body["request"])
	gen := mapFromAny(request["generationConfig"])
	thinking := mapFromAny(gen["thinkingConfig"])
	if got := asString(thinking["thinkingLevel"]); got != "high" {
		t.Fatalf("thinkingLevel = %q, want high", got)
	}
	if got := fmt.Sprintf("%v", thinking["includeThoughts"]); got != "false" {
		t.Fatalf("includeThoughts = %v, want false", thinking["includeThoughts"])
	}
}
