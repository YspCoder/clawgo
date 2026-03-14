package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPProviderExecuteJSONAttemptsReturnsEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	provider := NewHTTPProvider("test", "token", server.URL, "gpt-test", false, "bearer", 5*time.Second, nil)
	result, err := provider.executeJSONAttempts(context.Background(), server.URL, map[string]any{"hello": "world"}, nil, classifyOAuthFailure)
	if err != nil {
		t.Fatalf("executeJSONAttempts error: %v", err)
	}
	if result.StatusCode != http.StatusOK || result.ContentType != "application/json" || result.AttemptKind != "api_key" {
		t.Fatalf("unexpected envelope: %+v", result)
	}
	if string(result.Body) != `{"ok":true}` {
		t.Fatalf("unexpected body: %s", string(result.Body))
	}
}
