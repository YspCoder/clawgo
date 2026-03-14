package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGeminiCLIProviderBuildRequestBodyWrapsEnvelope(t *testing.T) {
	p := NewGeminiCLIProvider("gemini-cli", "", "", "gemini-2.5-pro", false, "oauth", 5*time.Second, nil)
	body := p.buildRequestBody([]Message{{Role: "user", Content: "hello"}}, nil, "gemini-2.5-pro", nil, false, &oauthSession{ProjectID: "demo-project"})

	if got := asString(body["project"]); got != "demo-project" {
		t.Fatalf("project = %q, want demo-project", got)
	}
	if got := asString(body["model"]); got != "gemini-2.5-pro" {
		t.Fatalf("model = %q, want gemini-2.5-pro", got)
	}
	request := mapFromAny(body["request"])
	if len(request) == 0 {
		t.Fatalf("request envelope missing: %#v", body)
	}
	if _, ok := request["safetySettings"]; !ok {
		t.Fatalf("expected safetySettings in request: %#v", request)
	}
}

func TestGeminiCLIProviderChatUsesCloudCodeEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1internal:generateContent" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("X-Goog-Api-Client"); got != geminiCLIApiClient {
			t.Fatalf("x-goog-api-client = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != geminiCLIUserAgent("gemini-2.5-pro") {
			t.Fatalf("user-agent = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if got := asString(payload["project"]); got != "demo-project" {
			t.Fatalf("project = %q, want demo-project", got)
		}
		if got := asString(payload["model"]); got != "gemini-2.5-pro" {
			t.Fatalf("model = %q, want gemini-2.5-pro", got)
		}
		if len(mapFromAny(payload["request"])) == 0 {
			t.Fatalf("request envelope missing: %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"totalTokenCount":2}}`))
	}))
	defer server.Close()

	p := NewGeminiCLIProvider("gemini-cli", "token", server.URL, "gemini-2.5-pro", false, "api_key", 5*time.Second, nil)
	resp, err := p.Chat(t.Context(), []Message{{Role: "user", Content: "hello"}}, nil, "gemini-2.5-pro", map[string]any{"project_id": "demo-project"})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q, want ok", resp.Content)
	}
}

func TestGeminiCLIProviderCountTokensRemovesProjectAndModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1internal:countTokens" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, ok := payload["project"]; ok {
			t.Fatalf("project should be removed for countTokens: %#v", payload)
		}
		if _, ok := payload["model"]; ok {
			t.Fatalf("model should be removed for countTokens: %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"totalTokens":42}`))
	}))
	defer server.Close()

	p := NewGeminiCLIProvider("gemini-cli", "token", server.URL, "gemini-2.5-pro", false, "api_key", 5*time.Second, nil)
	usage, err := p.CountTokens(t.Context(), []Message{{Role: "user", Content: "hello"}}, nil, "gemini-2.5-pro", map[string]any{"project_id": "demo-project"})
	if err != nil {
		t.Fatalf("CountTokens error: %v", err)
	}
	if usage == nil || usage.TotalTokens != 42 {
		t.Fatalf("usage = %#v, want 42", usage)
	}
}

func TestGeminiRetryAfterParsesGoogleRetryInfo(t *testing.T) {
	retryAfter := geminiRetryAfter([]byte(`{
		"error": {
			"message": "rate limited",
			"details": [
				{
					"@type": "type.googleapis.com/google.rpc.RetryInfo",
					"retryDelay": "1.5s"
				}
			]
		}
	}`))
	if retryAfter == nil || *retryAfter != 1500*time.Millisecond {
		t.Fatalf("retryAfter = %#v, want 1.5s", retryAfter)
	}
}
