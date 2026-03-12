package providers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuildQwenChatRequestStripsSuffixAndAppliesThinking(t *testing.T) {
	base := NewHTTPProvider("qwen", "token", qwenCompatBaseURL, "qwen-max", false, "oauth", 5*time.Second, nil)
	body := buildQwenChatRequest(base, []Message{{Role: "user", Content: "hi"}}, nil, "qwen-max(high)", nil, false)

	if got := body["model"]; got != "qwen-max" {
		t.Fatalf("model = %#v, want qwen-max", got)
	}
	if got := body["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", got)
	}
}

func TestBuildQwenChatRequestAddsPoisonToolForStreamingWithoutTools(t *testing.T) {
	base := NewHTTPProvider("qwen", "token", qwenCompatBaseURL, "qwen-max", false, "oauth", 5*time.Second, nil)
	body := buildQwenChatRequest(base, []Message{{Role: "user", Content: "hi"}}, nil, "qwen-max", nil, true)

	tools, ok := body["tools"].([]map[string]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want single poison tool", body["tools"])
	}
	function, _ := tools[0]["function"].(map[string]interface{})
	if got := function["name"]; got != "do_not_call_me" {
		t.Fatalf("tool name = %#v, want do_not_call_me", got)
	}
}

func TestClassifyQwenFailureMapsQuotaTo429UntilNextMidnight(t *testing.T) {
	status, reason, retry, retryAfter := classifyQwenFailure(http.StatusForbidden, []byte(`{"error":{"code":"insufficient_quota","message":"free allocated quota exceeded"}}`))
	if status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", status, http.StatusTooManyRequests)
	}
	if reason != oauthFailureQuota || !retry {
		t.Fatalf("reason=%q retry=%v", reason, retry)
	}
	if retryAfter == nil || *retryAfter <= 0 || *retryAfter > 24*time.Hour {
		t.Fatalf("retryAfter = %#v, want within next day", retryAfter)
	}
}

func TestQwenProviderChatMapsQuota403To429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"insufficient_quota","message":"free allocated quota exceeded"}}`))
	}))
	defer server.Close()

	provider := NewQwenProvider("qwen-quota", "token", server.URL, "qwen-max", false, "api_key", 5*time.Second, nil)
	_, err := provider.Chat(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil, "qwen-max", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "status 429") {
		t.Fatalf("error = %v, want mapped 429", err)
	}
}

func TestQwenProviderCountTokens(t *testing.T) {
	provider := NewQwenProvider("qwen", "token", qwenCompatBaseURL, "qwen-max", false, "api_key", 5*time.Second, nil)
	usage, err := provider.CountTokens(t.Context(), []Message{{Role: "user", Content: "hello qwen"}}, nil, "qwen-max", nil)
	if err != nil {
		t.Fatalf("CountTokens error: %v", err)
	}
	if usage == nil || usage.PromptTokens <= 0 || usage.TotalTokens != usage.PromptTokens {
		t.Fatalf("usage = %#v, want positive prompt-only count", usage)
	}
}

func TestApplyAttemptProviderHeadersQwenUsesDynamicStainlessValues(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, qwenCompatBaseURL+"/chat/completions", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	provider := &HTTPProvider{
		oauth: &oauthManager{cfg: oauthConfig{Provider: defaultQwenOAuthProvider}},
	}

	applyAttemptProviderHeaders(req, authAttempt{kind: "oauth", token: "qwen-token"}, provider, true)

	if got := req.Header.Get("X-Stainless-Arch"); got != qwenStainlessArch() {
		t.Fatalf("X-Stainless-Arch = %q, want %q", got, qwenStainlessArch())
	}
	if got := req.Header.Get("X-Stainless-Os"); got != qwenStainlessOS() {
		t.Fatalf("X-Stainless-Os = %q, want %q", got, qwenStainlessOS())
	}
	if got := req.Header.Get("Accept"); got != "text/event-stream" {
		t.Fatalf("Accept = %q, want text/event-stream", got)
	}
}

func TestNormalizeQwenResourceURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "https://chat.qwen.ai/api", want: "https://chat.qwen.ai/v1"},
		{in: "chat.qwen.ai/api", want: "https://chat.qwen.ai/v1"},
		{in: "https://portal.qwen.ai/v1", want: "https://portal.qwen.ai/v1"},
	}
	for _, tt := range tests {
		if got := normalizeQwenResourceURL(tt.in); got != tt.want {
			t.Fatalf("normalizeQwenResourceURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestQwenHookUsesSessionResourceURL(t *testing.T) {
	hooks := qwenProviderHooks{}
	base := NewHTTPProvider("qwen", "token", qwenCompatBaseURL, "qwen-max", false, "oauth", 5*time.Second, nil)
	got := hooks.endpoint(base, authAttempt{
		kind: "oauth",
		session: &oauthSession{
			ResourceURL: "https://chat.qwen.ai/api",
		},
	}, "/chat/completions")
	if got != "https://chat.qwen.ai/v1/chat/completions" {
		t.Fatalf("endpoint = %q", got)
	}
}

func TestOpenAICompatMessagesPreserveMultimodalContentParts(t *testing.T) {
	msgs := openAICompatMessages([]Message{{
		Role: "user",
		ContentParts: []MessageContentPart{
			{Type: "text", Text: "look"},
			{Type: "input_image", ImageURL: "https://example.com/cat.png", Detail: "high"},
		},
	}})
	if len(msgs) != 1 {
		t.Fatalf("messages len = %d", len(msgs))
	}
	content, ok := msgs[0]["content"].([]map[string]interface{})
	if !ok || len(content) != 2 {
		t.Fatalf("content = %#v", msgs[0]["content"])
	}
	if got := content[0]["type"]; got != "text" {
		t.Fatalf("first part type = %#v", got)
	}
	imagePart, _ := content[1]["image_url"].(map[string]interface{})
	if got := content[1]["type"]; got != "image_url" {
		t.Fatalf("second part type = %#v", got)
	}
	if got := imagePart["url"]; got != "https://example.com/cat.png" {
		t.Fatalf("image url = %#v", got)
	}
	if got := imagePart["detail"]; got != "high" {
		t.Fatalf("image detail = %#v", got)
	}
}
