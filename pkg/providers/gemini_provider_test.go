package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/config"
)

func TestGeminiBuildRequestBody(t *testing.T) {
	p := NewGeminiProvider("gemini", "", "", "gemini-2.5-pro", false, "oauth", 5*time.Second, nil)
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
			Parameters:  map[string]interface{}{"type": "object"},
		},
	}}, "gemini-2.5-pro", map[string]interface{}{
		"max_tokens":  128,
		"temperature": 0.3,
	}, false)

	request := body
	if system := asString(mapFromAny(request["systemInstruction"])["parts"].([]map[string]any)[0]["text"]); system != "You are helpful." {
		t.Fatalf("expected system instruction, got %q", system)
	}
	if got := len(request["contents"].([]map[string]any)); got != 3 {
		t.Fatalf("expected 3 content entries, got %d", got)
	}
	gen := mapFromAny(request["generationConfig"])
	if got := intValue(gen["maxOutputTokens"]); got != 128 {
		t.Fatalf("expected maxOutputTokens, got %#v", gen["maxOutputTokens"])
	}
}

func TestGeminiProviderCountTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-2.5-pro:countTokens" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"totalTokens":42}`))
	}))
	defer server.Close()

	p := NewGeminiProvider("gemini", "token", server.URL, "gemini-2.5-pro", false, "api_key", 5*time.Second, nil)
	usage, err := p.CountTokens(t.Context(), []Message{{Role: "user", Content: "hello"}}, nil, "gemini-2.5-pro", nil)
	if err != nil {
		t.Fatalf("CountTokens error: %v", err)
	}
	if usage == nil || usage.PromptTokens != 42 || usage.TotalTokens != 42 {
		t.Fatalf("usage = %#v, want 42", usage)
	}
}

func TestGeminiProviderChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-2.5-pro:generateContent" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("x-goog-api-key"); got != "token" {
			t.Fatalf("x-goog-api-key = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`))
	}))
	defer server.Close()

	p := NewGeminiProvider("gemini", "token", server.URL, "gemini-2.5-pro", false, "api_key", 5*time.Second, nil)
	resp, err := p.Chat(t.Context(), []Message{{Role: "user", Content: "hello"}}, nil, "gemini-2.5-pro", nil)
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q, want ok", resp.Content)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 2 {
		t.Fatalf("usage = %#v", resp.Usage)
	}
}

func TestGeminiBuildRequestBodyFixesImageAspectRatio(t *testing.T) {
	p := NewGeminiProvider("gemini", "", "", geminiImagePreviewModel, false, "api_key", 5*time.Second, nil)
	body := p.buildRequestBody([]Message{
		{Role: "user", Content: "draw a cat"},
	}, nil, geminiImagePreviewModel, map[string]interface{}{
		"gemini_generation_config": map[string]interface{}{
			"imageConfig": map[string]interface{}{"aspectRatio": "1:1"},
		},
	}, false)

	gen := mapFromAny(body["generationConfig"])
	if _, ok := gen["imageConfig"]; ok {
		t.Fatalf("expected imageConfig to be removed, got %#v", gen["imageConfig"])
	}
	if got := len(gen["responseModalities"].([]any)); got != 2 {
		t.Fatalf("responseModalities len = %d", got)
	}
	contents := body["contents"].([]map[string]any)
	parts := contents[0]["parts"].([]map[string]any)
	if len(parts) < 3 {
		t.Fatalf("parts = %#v", parts)
	}
	if _, ok := mapFromAny(parts[1]["inlineData"])["data"]; !ok {
		t.Fatalf("expected inlineData placeholder, got %#v", parts[1])
	}
}

func TestGeminiBuildRequestBodyAppliesBudgetThinkingSuffix(t *testing.T) {
	p := NewGeminiProvider("gemini", "", "", "gemini-2.5-pro", false, "api_key", 5*time.Second, nil)

	body := p.buildRequestBody([]Message{{Role: "user", Content: "hi"}}, nil, "gemini-2.5-pro(high)", nil, false)
	gen := mapFromAny(body["generationConfig"])
	thinking := mapFromAny(gen["thinkingConfig"])
	if got := intValue(thinking["thinkingBudget"]); got != 24576 {
		t.Fatalf("thinkingBudget = %v, want 24576", thinking["thinkingBudget"])
	}
	if got := fmt.Sprintf("%v", thinking["includeThoughts"]); got != "true" {
		t.Fatalf("includeThoughts = %v, want true", thinking["includeThoughts"])
	}

	body = p.buildRequestBody([]Message{{Role: "user", Content: "hi"}}, nil, "gemini-2.5-pro(none)", nil, false)
	gen = mapFromAny(body["generationConfig"])
	thinking = mapFromAny(gen["thinkingConfig"])
	if got := intValue(thinking["thinkingBudget"]); got != 128 {
		t.Fatalf("thinkingBudget = %v, want 128", thinking["thinkingBudget"])
	}
	if got := fmt.Sprintf("%v", thinking["includeThoughts"]); got != "false" {
		t.Fatalf("includeThoughts = %v, want false", thinking["includeThoughts"])
	}

	body = p.buildRequestBody([]Message{{Role: "user", Content: "hi"}}, nil, "gemini-2.5-pro(auto)", nil, false)
	gen = mapFromAny(body["generationConfig"])
	thinking = mapFromAny(gen["thinkingConfig"])
	if got := intValue(thinking["thinkingBudget"]); got != -1 {
		t.Fatalf("thinkingBudget = %v, want -1", thinking["thinkingBudget"])
	}

	body = p.buildRequestBody([]Message{{Role: "user", Content: "hi"}}, nil, "gemini-2.5-pro(8192)", nil, false)
	gen = mapFromAny(body["generationConfig"])
	thinking = mapFromAny(gen["thinkingConfig"])
	if got := intValue(thinking["thinkingBudget"]); got != 8192 {
		t.Fatalf("thinkingBudget = %v, want 8192", thinking["thinkingBudget"])
	}
}

func TestGeminiBuildRequestBodyAppliesLevelThinkingSuffix(t *testing.T) {
	p := NewGeminiProvider("gemini", "", "", "gemini-3-pro-preview", false, "api_key", 5*time.Second, nil)

	body := p.buildRequestBody([]Message{{Role: "user", Content: "hi"}}, nil, "gemini-3-pro-preview(high)", nil, false)
	gen := mapFromAny(body["generationConfig"])
	thinking := mapFromAny(gen["thinkingConfig"])
	if got := asString(thinking["thinkingLevel"]); got != "high" {
		t.Fatalf("thinkingLevel = %q, want high", got)
	}
	if got := fmt.Sprintf("%v", thinking["includeThoughts"]); got != "true" {
		t.Fatalf("includeThoughts = %v, want true", thinking["includeThoughts"])
	}

	body = p.buildRequestBody([]Message{{Role: "user", Content: "hi"}}, nil, "gemini-3-pro-preview(xhigh)", nil, false)
	gen = mapFromAny(body["generationConfig"])
	thinking = mapFromAny(gen["thinkingConfig"])
	if got := asString(thinking["thinkingLevel"]); got != "high" {
		t.Fatalf("thinkingLevel = %q, want high", got)
	}

	body = p.buildRequestBody([]Message{{Role: "user", Content: "hi"}}, nil, "gemini-3-pro-preview(none)", nil, false)
	gen = mapFromAny(body["generationConfig"])
	thinking = mapFromAny(gen["thinkingConfig"])
	if got := asString(thinking["thinkingLevel"]); got != "low" {
		t.Fatalf("thinkingLevel = %q, want low", got)
	}
	if got := fmt.Sprintf("%v", thinking["includeThoughts"]); got != "false" {
		t.Fatalf("includeThoughts = %v, want false", thinking["includeThoughts"])
	}

	body = p.buildRequestBody([]Message{{Role: "user", Content: "hi"}}, nil, "gemini-3-pro-preview(auto)", nil, false)
	gen = mapFromAny(body["generationConfig"])
	thinking = mapFromAny(gen["thinkingConfig"])
	if got := intValue(thinking["thinkingBudget"]); got != -1 {
		t.Fatalf("thinkingBudget = %v, want -1", thinking["thinkingBudget"])
	}
}

func TestFilterGeminiSSEUsageMetadataDropsNonTerminalUsage(t *testing.T) {
	raw := []byte(`{"candidates":[{"content":{"parts":[{"text":"hello"}]}}],"usageMetadata":{"promptTokenCount":1}}`)
	filtered := filterGeminiSSEUsageMetadata(raw)
	var payload map[string]any
	if err := json.Unmarshal(filtered, &payload); err != nil {
		t.Fatalf("unmarshal filtered: %v", err)
	}
	if _, ok := payload["usageMetadata"]; ok {
		t.Fatalf("expected usageMetadata to be removed: %#v", payload)
	}
}

func TestGeminiTextPartsSupportInlineDataAndFileURLs(t *testing.T) {
	parts := geminiTextParts(Message{
		Role: "user",
		ContentParts: []MessageContentPart{
			{Type: "input_image", ImageURL: "data:image/png;base64,AAAA", MIMEType: "image/png"},
			{Type: "input_file", FileURL: "https://example.com/doc.pdf", MIMEType: "application/pdf"},
		},
	})
	if len(parts) != 2 {
		t.Fatalf("parts = %#v", parts)
	}
	inline := mapFromAny(parts[0]["inlineData"])
	if got := asString(inline["data"]); got != "AAAA" {
		t.Fatalf("inline data = %q", got)
	}
	fileData := mapFromAny(parts[1]["fileData"])
	if got := asString(fileData["fileUri"]); got != "https://example.com/doc.pdf" {
		t.Fatalf("file uri = %q", got)
	}
}

func TestGeminiBaseURLForAttemptUsesSessionResourceURL(t *testing.T) {
	base := NewHTTPProvider("gemini", "token", geminiBaseURL, "gemini-2.5-pro", false, "oauth", 5*time.Second, nil)
	got := geminiBaseURLForAttempt(base, authAttempt{
		kind: "oauth",
		session: &oauthSession{
			ResourceURL: "https://generativelanguage.googleapis.com/v1beta/models",
		},
	})
	if got != geminiBaseURL {
		t.Fatalf("base url = %q, want %q", got, geminiBaseURL)
	}
}

func TestCreateProviderByNameRoutesGeminiCLIToGeminiProvider(t *testing.T) {
	cfg := &config.Config{
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				"gemini-cli": {
					APIBase:    "",
					APIKey:     "token",
					TimeoutSec: 30,
					Models:     []string{"gemini-2.5-pro"},
				},
			},
		},
	}
	provider, err := CreateProviderByName(cfg, "gemini-cli")
	if err != nil {
		t.Fatalf("CreateProviderByName error: %v", err)
	}
	if _, ok := provider.(*GeminiCLIProvider); !ok {
		t.Fatalf("provider = %T, want *GeminiCLIProvider", provider)
	}
}

func TestCreateProviderByNameRoutesAIStudioProviderViaGeminiTests(t *testing.T) {
	cfg := &config.Config{
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				"aistudio": {
					TimeoutSec: 30,
					Models:     []string{"gemini-2.5-pro"},
				},
			},
		},
	}
	provider, err := CreateProviderByName(cfg, "aistudio")
	if err != nil {
		t.Fatalf("CreateProviderByName error: %v", err)
	}
	if _, ok := provider.(*AistudioProvider); !ok {
		t.Fatalf("provider = %T, want *AistudioProvider", provider)
	}
}

func TestGeminiThinkingSuffixPreservesExplicitIncludeThoughts(t *testing.T) {
	p := NewGeminiProvider("gemini", "", "", "gemini-3-pro-preview", false, "api_key", 5*time.Second, nil)
	body := p.buildRequestBody([]Message{{Role: "user", Content: "hi"}}, nil, "gemini-3-pro-preview(high)", map[string]interface{}{
		"gemini_generation_config": map[string]interface{}{
			"thinkingConfig": map[string]interface{}{
				"includeThoughts": false,
			},
		},
	}, false)
	gen := mapFromAny(body["generationConfig"])
	thinking := mapFromAny(gen["thinkingConfig"])
	if got := asString(thinking["thinkingLevel"]); got != "high" {
		t.Fatalf("thinkingLevel = %q, want high", got)
	}
	if got := fmt.Sprintf("%v", thinking["includeThoughts"]); got != "false" {
		t.Fatalf("includeThoughts = %v, want false", thinking["includeThoughts"])
	}
}
