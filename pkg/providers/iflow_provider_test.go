package providers

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBuildIFlowChatRequestAppliesGLMThinking(t *testing.T) {
	base := NewHTTPProvider("iflow", "token", iflowCompatBaseURL, "glm-4.6", false, "api_key", 5*time.Second, nil)
	body := buildIFlowChatRequest(base, []Message{{Role: "user", Content: "hi"}}, nil, "glm-4.6(high)", nil, false)

	if got := body["model"]; got != "glm-4.6" {
		t.Fatalf("model = %#v, want glm-4.6", got)
	}
	kwargs, _ := body["chat_template_kwargs"].(map[string]interface{})
	if got := kwargs["enable_thinking"]; got != true {
		t.Fatalf("enable_thinking = %#v, want true", got)
	}
	if got := kwargs["clear_thinking"]; got != false {
		t.Fatalf("clear_thinking = %#v, want false", got)
	}
}

func TestBuildIFlowChatRequestAppliesMiniMaxThinking(t *testing.T) {
	base := NewHTTPProvider("iflow", "token", iflowCompatBaseURL, "minimax-m2", false, "api_key", 5*time.Second, nil)
	body := buildIFlowChatRequest(base, []Message{{Role: "user", Content: "hi"}}, nil, "minimax-m2(none)", nil, false)

	if got := body["reasoning_split"]; got != false {
		t.Fatalf("reasoning_split = %#v, want false", got)
	}
}

func TestIFlowEnsureToolsArrayAddsPlaceholder(t *testing.T) {
	body := map[string]interface{}{"tools": []interface{}{}}
	iflowEnsureToolsArray(body)

	tools, _ := body["tools"].([]map[string]interface{})
	if len(tools) != 1 {
		t.Fatalf("tools = %#v, want placeholder tool", body["tools"])
	}
	fn, _ := tools[0]["function"].(map[string]interface{})
	if got := fn["name"]; got != "noop" {
		t.Fatalf("tool name = %#v, want noop", got)
	}
}

func TestIFlowEnsureToolsArrayAddsPlaceholderWhenMissing(t *testing.T) {
	body := map[string]interface{}{}
	iflowEnsureToolsArray(body)

	tools, _ := body["tools"].([]map[string]interface{})
	if len(tools) != 1 {
		t.Fatalf("tools = %#v, want placeholder tool", body["tools"])
	}
	fn, _ := tools[0]["function"].(map[string]interface{})
	if got := fn["name"]; got != "noop" {
		t.Fatalf("tool name = %#v, want noop", got)
	}
}

func TestCreateIFlowSignature(t *testing.T) {
	got := createIFlowSignature(iflowCompatUserAgent, "session-1", 1234567890, "secret")
	want := "e42963e253333206027e32351580e1c1846b63936c65aed385cd41095aa516e9"
	if got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}

func TestApplyIFlowHeadersUsesSessionAPIKey(t *testing.T) {
	attempt := authAttempt{
		kind:  "oauth",
		token: "access-token",
		session: &oauthSession{
			Token: map[string]interface{}{"api_key": "session-api-key"},
		},
	}
	req, err := http.NewRequest(http.MethodPost, iflowCompatBaseURL+iflowCompatEndpoint, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	applyAttemptAuth(req, attempt)
	applyIFlowHeaders(req, iflowAttemptAPIKey(attempt), false)
	if got := req.Header.Get("Authorization"); got != "Bearer session-api-key" {
		t.Fatalf("Authorization = %q, want Bearer session-api-key", got)
	}
	if got := req.Header.Get("User-Agent"); got != iflowCompatUserAgent {
		t.Fatalf("User-Agent = %q", got)
	}
	if got := req.Header.Get("session-id"); !strings.HasPrefix(got, "session-") {
		t.Fatalf("session-id = %q", got)
	}
	if got := req.Header.Get("x-iflow-signature"); got == "" {
		t.Fatal("expected x-iflow-signature")
	}
}

func TestBuildIFlowChatRequestStreamAddsPlaceholderToolWhenMissing(t *testing.T) {
	base := NewHTTPProvider("iflow", "token", iflowCompatBaseURL, "glm-4.6", false, "api_key", 5*time.Second, nil)
	body := buildIFlowChatRequest(base, []Message{{Role: "user", Content: "hi"}}, nil, "glm-4.6", nil, true)

	if got := body["stream"]; got != true {
		t.Fatalf("stream = %#v, want true", got)
	}
	tools, _ := body["tools"].([]map[string]interface{})
	if len(tools) != 1 {
		t.Fatalf("tools = %#v, want placeholder tool", body["tools"])
	}
	fn, _ := tools[0]["function"].(map[string]interface{})
	if got := fn["name"]; got != "noop" {
		t.Fatalf("tool name = %#v, want noop", got)
	}
}

func TestNormalizeIFlowBaseURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "apis.iflow.cn", want: "https://apis.iflow.cn/v1"},
		{in: "https://apis.iflow.cn/v1", want: "https://apis.iflow.cn/v1"},
		{in: "https://apis.iflow.cn/v1/chat/completions", want: "https://apis.iflow.cn/v1"},
	}
	for _, tt := range tests {
		if got := normalizeIFlowBaseURL(tt.in); got != tt.want {
			t.Fatalf("normalizeIFlowBaseURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
