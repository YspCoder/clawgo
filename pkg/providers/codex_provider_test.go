package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNormalizeCodexRequestBody(t *testing.T) {
	body := normalizeCodexRequestBody(map[string]interface{}{
		"model":                "gpt-5.4",
		"max_output_tokens":    1024,
		"temperature":          0.2,
		"previous_response_id": "resp_123",
		"include":              []interface{}{"foo.bar", "reasoning.encrypted_content"},
		"input": []map[string]interface{}{
			{"type": "message", "role": "system", "content": "You are helpful."},
			{"type": "message", "role": "user", "content": "hello"},
		},
	}, false)

	if got := body["stream"]; got != true {
		t.Fatalf("expected stream=true, got %#v", got)
	}
	if got := body["store"]; got != false {
		t.Fatalf("expected store=false, got %#v", got)
	}
	if got := body["parallel_tool_calls"]; got != true {
		t.Fatalf("expected parallel_tool_calls=true, got %#v", got)
	}
	if got := body["instructions"]; got != "" {
		t.Fatalf("expected empty instructions default, got %#v", got)
	}
	if _, ok := body["max_output_tokens"]; ok {
		t.Fatalf("expected max_output_tokens removed, got %#v", body["max_output_tokens"])
	}
	if _, ok := body["temperature"]; ok {
		t.Fatalf("expected temperature removed, got %#v", body["temperature"])
	}
	if _, ok := body["previous_response_id"]; ok {
		t.Fatalf("expected previous_response_id removed, got %#v", body["previous_response_id"])
	}
	input := body["input"].([]map[string]interface{})
	if got := input[0]["role"]; got != "developer" {
		t.Fatalf("expected system role converted to developer, got %#v", got)
	}
	include := body["include"].([]string)
	if len(include) != 2 {
		t.Fatalf("expected deduped include values, got %#v", include)
	}
	if include[0] != "foo.bar" || include[1] != "reasoning.encrypted_content" {
		t.Fatalf("unexpected include ordering: %#v", include)
	}
}

func TestNormalizeCodexRequestBodyPreservesPreviousResponseIDForWebsocket(t *testing.T) {
	body := normalizeCodexRequestBody(map[string]interface{}{
		"model":                "gpt-5.4",
		"previous_response_id": "resp_123",
	}, true)
	if got := body["previous_response_id"]; got != "resp_123" {
		t.Fatalf("expected previous_response_id preserved for websocket path, got %#v", got)
	}
}

func TestApplyAttemptProviderHeaders_CodexOAuth(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	provider := &HTTPProvider{
		oauth: &oauthManager{cfg: oauthConfig{Provider: defaultCodexOAuthProvider}},
	}
	attempt := authAttempt{
		kind:  "oauth",
		token: "codex-token",
		session: &oauthSession{
			AccountID: "acct_123",
		},
	}

	applyAttemptProviderHeaders(req, attempt, provider, true)

	if got := req.Header.Get("Version"); got != codexClientVersion {
		t.Fatalf("expected codex version header, got %q", got)
	}
	if got := req.Header.Get("User-Agent"); got != codexCompatUserAgent {
		t.Fatalf("expected codex user agent, got %q", got)
	}
	if got := req.Header.Get("Accept"); got != "text/event-stream" {
		t.Fatalf("expected sse accept header, got %q", got)
	}
	if got := req.Header.Get("Originator"); got != "codex_cli_rs" {
		t.Fatalf("expected codex originator, got %q", got)
	}
	if got := req.Header.Get("Chatgpt-Account-Id"); got != "acct_123" {
		t.Fatalf("expected account id header, got %q", got)
	}
	if got := req.Header.Get("Session_id"); got == "" {
		t.Fatalf("expected generated session id header")
	}
}

func TestApplyCodexCacheHeaders(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	applyCodexCacheHeaders(req, map[string]interface{}{
		"prompt_cache_key": "cache_123",
	})
	if got := req.Header.Get("Conversation_id"); got != "cache_123" {
		t.Fatalf("expected conversation id header, got %q", got)
	}
	if got := req.Header.Get("Session_id"); got != "cache_123" {
		t.Fatalf("expected session id header to reuse prompt cache key, got %q", got)
	}
}

func TestCodexPayloadForAttempt_ApiKeyGetsStablePromptCacheKey(t *testing.T) {
	attempt := authAttempt{kind: "api_key", token: "test-api-key"}
	got := codexPayloadForAttempt(map[string]interface{}{
		"model": "gpt-5.4",
	}, attempt)
	want := uuid.NewSHA1(uuid.NameSpaceOID, []byte("cli-proxy-api:codex:prompt-cache:test-api-key")).String()
	if key := got["prompt_cache_key"]; key != want {
		t.Fatalf("expected stable prompt_cache_key %q, got %#v", want, key)
	}

	got2 := codexPayloadForAttempt(map[string]interface{}{
		"model": "gpt-5.4",
	}, attempt)
	if key := got2["prompt_cache_key"]; key != want {
		t.Fatalf("expected second prompt_cache_key %q, got %#v", want, key)
	}
}

func TestCodexPayloadForAttempt_MetadataUserIDGetsReusablePromptCacheKey(t *testing.T) {
	codexPromptCacheStore.mu.Lock()
	codexPromptCacheStore.items = map[string]codexPromptCacheEntry{}
	codexPromptCacheStore.mu.Unlock()

	first := codexPayloadForAttempt(map[string]interface{}{
		"model": "gpt-5.4",
		"metadata": map[string]interface{}{
			"user_id": "user-123",
		},
	}, authAttempt{kind: "oauth", token: "oauth-token"})
	second := codexPayloadForAttempt(map[string]interface{}{
		"model": "gpt-5.4",
		"metadata": map[string]interface{}{
			"user_id": "user-123",
		},
	}, authAttempt{kind: "oauth", token: "oauth-token"})

	firstKey, _ := first["prompt_cache_key"].(string)
	secondKey, _ := second["prompt_cache_key"].(string)
	if firstKey == "" || secondKey == "" {
		t.Fatalf("expected prompt_cache_key generated from metadata.user_id, got %#v / %#v", first, second)
	}
	if firstKey != secondKey {
		t.Fatalf("expected reusable prompt_cache_key for same model/user_id, got %q vs %q", firstKey, secondKey)
	}
}

func TestCodexProviderBuildSummaryViaResponsesCompact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/responses/compact":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"messages":[{"role":"user","content":"hello"}]}}`))
		case "/responses":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output_text\":\"Key Facts\\n- hello\"}}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := NewCodexProvider("codex", "test-api-key", server.URL, "gpt-5.4", true, "", 5*time.Second, nil)
	summary, err := provider.BuildSummaryViaResponsesCompact(t.Context(), "gpt-5.4", "", []Message{{Role: "user", Content: "hello"}}, 0)
	if err != nil {
		t.Fatalf("BuildSummaryViaResponsesCompact error: %v", err)
	}
	if summary != "Key Facts\n- hello" {
		t.Fatalf("unexpected summary: %q", summary)
	}
}

func TestCodexProviderChatFallsBackToHTTPStreamResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output_text\":\"hello\"}}\n\n")
	}))
	defer server.Close()

	provider := NewCodexProvider("codex", "test-api-key", server.URL, "gpt-5.4", false, "", 5*time.Second, nil)
	resp, err := provider.Chat(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil, "gpt-5.4", nil)
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("unexpected response content: %q", resp.Content)
	}
}

func TestCodexHandleAttemptFailureMarksAPIKeyCooldown(t *testing.T) {
	provider := NewCodexProvider("codex-websocket-failure", "test-api-key", "", "gpt-5.4", false, "", 5*time.Second, nil)
	provider.handleAttemptFailure(authAttempt{kind: "api_key", token: "test-api-key"}, http.StatusTooManyRequests, []byte(`{"error":{"message":"rate limit exceeded"}}`))

	providerRuntimeRegistry.mu.Lock()
	state := providerRuntimeRegistry.api["codex-websocket-failure"]
	providerRuntimeRegistry.mu.Unlock()

	if state.API.FailureCount <= 0 {
		t.Fatalf("expected api key failure count to increase, got %#v", state.API)
	}
	if state.API.CooldownUntil == "" {
		t.Fatalf("expected api key cooldown to be set, got %#v", state.API)
	}
	if state.API.LastFailure != string(oauthFailureRateLimit) {
		t.Fatalf("expected last failure %q, got %#v", oauthFailureRateLimit, state.API.LastFailure)
	}
}

func TestBuildCodexWebsocketRequestBodyPreservesPreviousResponseID(t *testing.T) {
	body := buildCodexWebsocketRequestBody(map[string]interface{}{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-1",
		"input": []map[string]interface{}{
			{"type": "message", "id": "msg-1"},
		},
	})
	if got := body["type"]; got != "response.create" {
		t.Fatalf("type = %#v, want response.create", got)
	}
	if got := body["previous_response_id"]; got != "resp-1" {
		t.Fatalf("previous_response_id = %#v, want resp-1", got)
	}
	input := body["input"].([]map[string]interface{})
	if got := input[0]["id"]; got != "msg-1" {
		t.Fatalf("input item id mismatch: %#v", got)
	}
}

func TestApplyCodexWebsocketHeadersDefaultsToCurrentResponsesBeta(t *testing.T) {
	headers := applyCodexWebsocketHeaders(http.Header{}, authAttempt{}, nil)
	if got := headers.Get("OpenAI-Beta"); got != codexResponsesWebsocketBetaHeaderValue {
		t.Fatalf("OpenAI-Beta = %s, want %s", got, codexResponsesWebsocketBetaHeaderValue)
	}
}

func TestApplyCodexWebsocketHeadersUsesTurnOptions(t *testing.T) {
	headers := applyCodexWebsocketHeaders(http.Header{}, authAttempt{}, map[string]interface{}{
		"codex_turn_state":    "state-1",
		"codex_turn_metadata": "meta-1",
	})
	if got := headers.Get("x-codex-turn-state"); got != "state-1" {
		t.Fatalf("x-codex-turn-state = %q, want state-1", got)
	}
	if got := headers.Get("x-codex-turn-metadata"); got != "meta-1" {
		t.Fatalf("x-codex-turn-metadata = %q, want meta-1", got)
	}
}

func TestApplyCodexWebsocketHeadersUsesResponsesStreamOptions(t *testing.T) {
	headers := applyCodexWebsocketHeaders(http.Header{}, authAttempt{}, map[string]interface{}{
		"responses_stream_options": map[string]interface{}{
			"turn_state":    "state-2",
			"turn_metadata": "meta-2",
		},
	})
	if got := headers.Get("x-codex-turn-state"); got != "state-2" {
		t.Fatalf("x-codex-turn-state = %q, want state-2", got)
	}
	if got := headers.Get("x-codex-turn-metadata"); got != "meta-2" {
		t.Fatalf("x-codex-turn-metadata = %q, want meta-2", got)
	}
}

func TestNormalizeCodexWebsocketCompletion(t *testing.T) {
	got := normalizeCodexWebsocketCompletion([]byte(`{"type":"response.done","response":{"status":"completed","output_text":"hello"}}`))
	var decoded map[string]interface{}
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("unmarshal normalized payload: %v", err)
	}
	if decoded["type"] != "response.completed" {
		t.Fatalf("expected response.completed, got %#v", decoded["type"])
	}
}

func TestParseCodexWebsocketError(t *testing.T) {
	err, status, headers, ok := parseCodexWebsocketError([]byte(`{"type":"error","status":429,"error":{"message":"rate limited"},"headers":{"retry-after":"60"}}`))
	if !ok {
		t.Fatal("expected websocket error to parse")
	}
	if status != 429 {
		t.Fatalf("expected status 429, got %d", status)
	}
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("unexpected error: %v", err)
	}
	if headers == nil || headers.Get("retry-after") != "60" {
		t.Fatalf("expected retry-after header, got %#v", headers)
	}
}

func TestCodexExecutionSessionID(t *testing.T) {
	if got := codexExecutionSessionID(map[string]interface{}{"codex_execution_session": " sess-1 "}); got != "sess-1" {
		t.Fatalf("expected sess-1, got %q", got)
	}
}

func TestCodexProviderGetExecutionSessionReusesByID(t *testing.T) {
	provider := NewCodexProvider("codex", "", "", "gpt-5.4", false, "", 5*time.Second, nil)
	first := provider.getExecutionSession("sess-1")
	second := provider.getExecutionSession("sess-1")
	if first == nil || second == nil {
		t.Fatal("expected sessions")
	}
	if first != second {
		t.Fatal("expected same execution session instance for same id")
	}
}

func TestCodexProviderCloseExecutionSessionRemovesSession(t *testing.T) {
	provider := NewCodexProvider("codex", "", "", "gpt-5.4", false, "", 5*time.Second, nil)
	_ = provider.getExecutionSession("sess-1")
	provider.CloseExecutionSession("sess-1")
	provider.sessionMu.Lock()
	_, ok := provider.sessions["sess-1"]
	provider.sessionMu.Unlock()
	if ok {
		t.Fatal("expected session to be removed after close")
	}
}
