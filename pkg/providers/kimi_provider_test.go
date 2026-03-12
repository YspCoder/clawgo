package providers

import (
	"net/http"
	"testing"
	"time"
)

func TestBuildKimiChatRequestStripsPrefixAndAppliesThinking(t *testing.T) {
	base := NewHTTPProvider("kimi", "token", kimiCompatBaseURL, "kimi-k2.5", false, "oauth", 5*time.Second, nil)
	body := buildKimiChatRequest(base, []Message{{Role: "user", Content: "hi"}}, nil, "kimi-k2.5(high)", nil, false)

	if got := body["model"]; got != "k2.5" {
		t.Fatalf("model = %#v, want k2.5", got)
	}
	if got := body["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", got)
	}
}

func TestBuildKimiChatRequestDisablesThinking(t *testing.T) {
	base := NewHTTPProvider("kimi", "token", kimiCompatBaseURL, "kimi-k2.5", false, "oauth", 5*time.Second, nil)
	body := buildKimiChatRequest(base, []Message{{Role: "user", Content: "hi"}}, nil, "kimi-k2.5(none)", nil, false)

	thinking, _ := body["thinking"].(map[string]interface{})
	if got := thinking["type"]; got != "disabled" {
		t.Fatalf("thinking.type = %#v, want disabled", got)
	}
}

func TestNormalizeKimiToolMessagesBackfillsToolCallID(t *testing.T) {
	body := map[string]interface{}{
		"messages": []map[string]interface{}{
			{
				"role": "assistant",
				"tool_calls": []map[string]interface{}{
					{"id": "call_1"},
				},
				"content": "thinking content",
			},
			{
				"role":    "tool",
				"content": "tool result",
			},
		},
	}

	normalizeKimiToolMessages(body)

	msgs := body["messages"].([]map[string]interface{})
	if got := msgs[1]["tool_call_id"]; got != "call_1" {
		t.Fatalf("tool_call_id = %#v, want call_1", got)
	}
	if got := msgs[0]["reasoning_content"]; got != "thinking content" {
		t.Fatalf("reasoning_content = %#v, want fallback content", got)
	}
}

func TestNormalizeKimiToolMessagesPromotesCallID(t *testing.T) {
	body := map[string]interface{}{
		"messages": []map[string]interface{}{
			{
				"role":    "tool",
				"call_id": "call_2",
			},
		},
	}

	normalizeKimiToolMessages(body)

	msgs := body["messages"].([]map[string]interface{})
	if got := msgs[0]["tool_call_id"]; got != "call_2" {
		t.Fatalf("tool_call_id = %#v, want call_2", got)
	}
}

func TestApplyAttemptProviderHeadersKimiUsesResolvedDeviceFields(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, kimiCompatBaseURL+"/chat/completions", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	provider := &HTTPProvider{
		oauth: &oauthManager{cfg: oauthConfig{Provider: defaultKimiOAuthProvider}},
	}
	attempt := authAttempt{
		kind:  "oauth",
		token: "kimi-token",
		session: &oauthSession{
			DeviceID: "device-123",
		},
	}

	applyAttemptProviderHeaders(req, attempt, provider, true)

	if got := req.Header.Get("X-Msh-Device-Id"); got != "device-123" {
		t.Fatalf("X-Msh-Device-Id = %q, want device-123", got)
	}
	if got := req.Header.Get("X-Msh-Device-Model"); got != kimiDeviceModel() {
		t.Fatalf("X-Msh-Device-Model = %q, want %q", got, kimiDeviceModel())
	}
	if got := req.Header.Get("X-Msh-Device-Name"); got == "" {
		t.Fatal("expected X-Msh-Device-Name to be set")
	}
}

func TestKimiHookUsesSessionResourceURL(t *testing.T) {
	hooks := kimiProviderHooks{}
	base := NewHTTPProvider("kimi", "token", kimiCompatBaseURL, "kimi-k2.5", false, "oauth", 5*time.Second, nil)
	got := hooks.endpoint(base, authAttempt{
		kind: "oauth",
		session: &oauthSession{
			ResourceURL: "https://api.kimi.com/coding/v1",
		},
	}, "/chat/completions")
	if got != "https://api.kimi.com/coding/v1/chat/completions" {
		t.Fatalf("endpoint = %q", got)
	}
}

func TestNormalizeKimiResourceURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "https://api.kimi.com/coding", want: "https://api.kimi.com/coding/v1"},
		{in: "api.kimi.com/coding", want: "https://api.kimi.com/coding/v1"},
		{in: "https://api.kimi.com/coding/v1", want: "https://api.kimi.com/coding/v1"},
	}
	for _, tt := range tests {
		if got := normalizeKimiResourceURL(tt.in); got != tt.want {
			t.Fatalf("normalizeKimiResourceURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestKimiProviderCountTokens(t *testing.T) {
	provider := NewKimiProvider("kimi", "token", kimiCompatBaseURL, "kimi-k2.5", false, "api_key", 5*time.Second, nil)
	usage, err := provider.CountTokens(t.Context(), []Message{{Role: "user", Content: "hello kimi"}}, nil, "kimi-k2.5", nil)
	if err != nil {
		t.Fatalf("CountTokens error: %v", err)
	}
	if usage == nil || usage.PromptTokens <= 0 || usage.TotalTokens != usage.PromptTokens {
		t.Fatalf("usage = %#v, want positive prompt-only count", usage)
	}
}
