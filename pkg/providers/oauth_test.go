package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/config"
)

func TestHTTPProviderOAuthRefreshesExpiredSession(t *testing.T) {
	t.Parallel()

	var refreshCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			atomic.AddInt32(&refreshCalls, 1)
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse token form failed: %v", err)
			}
			if got := r.Form.Get("grant_type"); got != "refresh_token" {
				t.Fatalf("unexpected grant_type: %s", got)
			}
			if got := r.Form.Get("refresh_token"); got != "refresh-token" {
				t.Fatalf("unexpected refresh_token: %s", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"fresh-token","refresh_token":"refresh-token","expires_in":3600}`))
		case "/v1/responses":
			if got := r.Header.Get("Authorization"); got != "Bearer fresh-token" {
				t.Fatalf("unexpected authorization header: %s", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"completed","output_text":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	credFile := filepath.Join(t.TempDir(), "codex.json")
	initial := oauthSession{
		Provider:     "codex",
		AccessToken:  "expired-token",
		RefreshToken: "refresh-token",
		Expire:       time.Now().Add(-time.Hour).Format(time.RFC3339),
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatalf("marshal session failed: %v", err)
	}
	if err := os.WriteFile(credFile, raw, 0o600); err != nil {
		t.Fatalf("write credential file failed: %v", err)
	}

	pc := config.ProviderConfig{
		APIBase:    server.URL + "/v1",
		Auth:       "oauth",
		TimeoutSec: 5,
		OAuth: config.ProviderOAuthConfig{
			Provider:       "codex",
			CredentialFile: credFile,
			ClientID:       "test-client",
			TokenURL:       server.URL + "/oauth/token",
			AuthURL:        server.URL + "/oauth/authorize",
		},
	}
	oauth, err := newOAuthManager(pc, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}
	provider := NewHTTPProvider("test-oauth-refresh", "", pc.APIBase, "gpt-test", false, "oauth", 5*time.Second, oauth)

	resp, err := provider.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil, "gpt-test", nil)
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("unexpected chat content: %q", resp.Content)
	}
	if got := atomic.LoadInt32(&refreshCalls); got != 1 {
		t.Fatalf("expected exactly one refresh call, got %d", got)
	}

	savedRaw, err := os.ReadFile(credFile)
	if err != nil {
		t.Fatalf("read refreshed credential file failed: %v", err)
	}
	if !strings.Contains(string(savedRaw), "fresh-token") {
		t.Fatalf("expected refreshed token to be persisted, got %s", string(savedRaw))
	}
}

func TestOAuthLoginManualCallbackURLParse(t *testing.T) {
	t.Parallel()

	result, err := waitForOAuthCodeManual(
		"https://example.com/auth?state=test-state",
		bytes.NewBufferString("http://localhost:1455/auth/callback?code=auth-code&state=test-state\n"),
	)
	if err != nil {
		t.Fatalf("manual callback parse failed: %v", err)
	}
	if result.Code != "auth-code" {
		t.Fatalf("unexpected auth code: %s", result.Code)
	}
	if result.State != "test-state" {
		t.Fatalf("unexpected state: %s", result.State)
	}
}

func TestHTTPProviderOAuthSwitchesAccountOnQuota(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	firstFile := filepath.Join(dir, "first.json")
	secondFile := filepath.Join(dir, "second.json")
	writeSession := func(path, token, email string) {
		t.Helper()
		raw, err := json.Marshal(oauthSession{
			Provider:    "codex",
			AccessToken: token,
			Email:       email,
			Expire:      time.Now().Add(time.Hour).Format(time.RFC3339),
		})
		if err != nil {
			t.Fatalf("marshal session failed: %v", err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatalf("write session failed: %v", err)
		}
	}
	writeSession(firstFile, "token-a", "a@example.com")
	writeSession(secondFile, "token-b", "b@example.com")

	var tokenAUsed int32
	var tokenBUsed int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		switch r.Header.Get("Authorization") {
		case "Bearer token-a":
			atomic.AddInt32(&tokenAUsed, 1)
			w.WriteHeader(http.StatusTooManyRequests)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"error":{"code":"insufficient_quota","message":"quota exceeded"}}`))
		case "Bearer token-b":
			atomic.AddInt32(&tokenBUsed, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"completed","output_text":"ok-from-second"}`))
		default:
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
	}))
	defer server.Close()

	pc := config.ProviderConfig{
		APIBase:    server.URL + "/v1",
		Auth:       "oauth",
		TimeoutSec: 5,
		OAuth: config.ProviderOAuthConfig{
			Provider:        "codex",
			CredentialFile:  firstFile,
			CredentialFiles: []string{firstFile, secondFile},
			ClientID:        "test-client",
			TokenURL:        server.URL + "/oauth/token",
			AuthURL:         server.URL + "/oauth/authorize",
		},
	}
	oauth, err := newOAuthManager(pc, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}
	provider := NewHTTPProvider("test-oauth-quota", "", pc.APIBase, "gpt-test", false, "oauth", 5*time.Second, oauth)
	resp, err := provider.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil, "gpt-test", nil)
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if resp.Content != "ok-from-second" {
		t.Fatalf("unexpected response content: %q", resp.Content)
	}
	if atomic.LoadInt32(&tokenAUsed) != 1 || atomic.LoadInt32(&tokenBUsed) != 1 {
		t.Fatalf("expected one attempt per token, got token-a=%d token-b=%d", tokenAUsed, tokenBUsed)
	}
}

func TestOAuthManagerPreRefreshesExpiringSession(t *testing.T) {
	t.Parallel()

	var refreshCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(&refreshCalls, 1)
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse token form failed: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Fatalf("unexpected grant_type: %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"prefreshed-token","refresh_token":"refresh-token","expires_in":3600}`))
	}))
	defer server.Close()

	credFile := filepath.Join(t.TempDir(), "prefresh.json")
	raw, err := json.Marshal(oauthSession{
		Provider:     "codex",
		AccessToken:  "old-token",
		RefreshToken: "refresh-token",
		Expire:       time.Now().Add(2 * time.Minute).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal session failed: %v", err)
	}
	if err := os.WriteFile(credFile, raw, 0o600); err != nil {
		t.Fatalf("write session failed: %v", err)
	}

	originalAPI := defaultAntigravityAPIEndpoint
	originalUserInfo := defaultAntigravityUserInfoURL
	defaultAntigravityAPIEndpoint = server.URL
	defaultAntigravityUserInfoURL = server.URL + "/userinfo"
	t.Cleanup(func() {
		defaultAntigravityAPIEndpoint = originalAPI
		defaultAntigravityUserInfoURL = originalUserInfo
	})

	manager, err := newOAuthManager(config.ProviderConfig{
		Auth:       "oauth",
		TimeoutSec: 5,
		OAuth: config.ProviderOAuthConfig{
			Provider:       "codex",
			CredentialFile: credFile,
			ClientID:       "test-client",
			TokenURL:       server.URL + "/oauth/token",
			AuthURL:        server.URL + "/oauth/authorize",
		},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}
	defer manager.bgCancel()

	result, err := manager.refreshExpiringSessions(context.Background(), 10*time.Minute)
	if err != nil {
		t.Fatalf("pre-refresh failed: %v", err)
	}
	if result == nil || result.Refreshed != 1 {
		t.Fatalf("expected one refreshed account, got %#v", result)
	}
	if atomic.LoadInt32(&refreshCalls) != 1 {
		t.Fatalf("expected one refresh call, got %d", refreshCalls)
	}
	saved, err := os.ReadFile(credFile)
	if err != nil {
		t.Fatalf("read saved session failed: %v", err)
	}
	if !strings.Contains(string(saved), "prefreshed-token") {
		t.Fatalf("expected prefreshed token in file, got %s", string(saved))
	}
}

func TestResolveOAuthConfigSupportsAdditionalProviders(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		provider string
		want     string
		flow     string
	}{
		{name: "anthropic-alias", provider: "anthropic", want: "claude", flow: oauthFlowCallback},
		{name: "antigravity", provider: "antigravity", want: "antigravity", flow: oauthFlowCallback},
		{name: "gemini", provider: "gemini", want: "gemini", flow: oauthFlowCallback},
		{name: "kimi", provider: "kimi", want: "kimi", flow: oauthFlowDevice},
		{name: "qwen", provider: "qwen", want: "qwen", flow: oauthFlowDevice},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := resolveOAuthConfig(config.ProviderConfig{
				Auth: "oauth",
				OAuth: config.ProviderOAuthConfig{
					Provider: tc.provider,
				},
			})
			if err != nil {
				t.Fatalf("resolve oauth config failed: %v", err)
			}
			if cfg.Provider != tc.want {
				t.Fatalf("unexpected provider: %s", cfg.Provider)
			}
			if cfg.FlowKind != tc.flow {
				t.Fatalf("unexpected flow kind: %s", cfg.FlowKind)
			}
		})
	}
}

func TestNewOAuthManagerUsesAnthropicTransportForClaude(t *testing.T) {
	t.Parallel()

	manager, err := newOAuthManager(config.ProviderConfig{
		Auth: "oauth",
		OAuth: config.ProviderOAuthConfig{
			Provider: "anthropic",
		},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}
	defer manager.bgCancel()

	if _, ok := manager.httpClient.Transport.(*anthropicOAuthRoundTripper); !ok {
		t.Fatalf("expected anthropic oauth transport, got %T", manager.httpClient.Transport)
	}
}

func TestNewOAuthManagerUsesDefaultTransportForNonClaude(t *testing.T) {
	t.Parallel()

	manager, err := newOAuthManager(config.ProviderConfig{
		Auth: "oauth",
		OAuth: config.ProviderOAuthConfig{
			Provider: "codex",
		},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}
	defer manager.bgCancel()

	if manager.httpClient.Transport != nil {
		t.Fatalf("expected default transport for non-claude provider, got %T", manager.httpClient.Transport)
	}
}

func TestResolveOAuthConfigAppliesProviderRefreshLeadDefaults(t *testing.T) {
	t.Parallel()

	cases := []struct {
		provider string
		want     time.Duration
	}{
		{provider: "codex", want: 5 * 24 * time.Hour},
		{provider: "anthropic", want: 4 * time.Hour},
		{provider: "antigravity", want: 5 * time.Minute},
		{provider: "gemini", want: 30 * time.Minute},
		{provider: "kimi", want: 5 * time.Minute},
		{provider: "qwen", want: 3 * time.Hour},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			cfg, err := resolveOAuthConfig(config.ProviderConfig{
				Auth: "oauth",
				OAuth: config.ProviderOAuthConfig{
					Provider: tc.provider,
				},
			})
			if err != nil {
				t.Fatalf("resolve oauth config failed: %v", err)
			}
			if cfg.RefreshLead != tc.want {
				t.Fatalf("unexpected refresh lead for %s: got %v want %v", tc.provider, cfg.RefreshLead, tc.want)
			}
		})
	}
}

func TestHTTPProviderOAuthSessionProxyRoutesRefreshAndResponses(t *testing.T) {
	t.Parallel()

	var refreshCalls int32
	var responseCalls int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			atomic.AddInt32(&refreshCalls, 1)
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse token form failed: %v", err)
			}
			if got := r.Form.Get("grant_type"); got != "refresh_token" {
				t.Fatalf("unexpected grant_type: %s", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"proxied-fresh-token","refresh_token":"refresh-token","expires_in":3600}`))
		case "/v1/responses":
			atomic.AddInt32(&responseCalls, 1)
			if got := r.Header.Get("Authorization"); got != "Bearer proxied-fresh-token" {
				t.Fatalf("unexpected authorization header: %s", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"completed","output_text":"ok-via-proxy"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer target.Close()

	var proxyCalls int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxyCalls, 1)
		targetURL := r.URL.String()
		if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
			targetURL = target.URL + r.URL.Path
			if rawQuery := strings.TrimSpace(r.URL.RawQuery); rawQuery != "" {
				targetURL += "?" + rawQuery
			}
		}
		req, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
		if err != nil {
			t.Fatalf("create proxied request failed: %v", err)
		}
		req.Header = r.Header.Clone()
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatalf("proxy round trip failed: %v", err)
		}
		defer resp.Body.Close()
		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	defer proxyServer.Close()

	credFile := filepath.Join(t.TempDir(), "proxied.json")
	raw, err := json.Marshal(oauthSession{
		Provider:     "codex",
		AccessToken:  "expired-token",
		RefreshToken: "refresh-token",
		Expire:       time.Now().Add(-time.Hour).Format(time.RFC3339),
		NetworkProxy: proxyServer.URL,
	})
	if err != nil {
		t.Fatalf("marshal session failed: %v", err)
	}
	if err := os.WriteFile(credFile, raw, 0o600); err != nil {
		t.Fatalf("write credential file failed: %v", err)
	}

	pc := config.ProviderConfig{
		APIBase:    target.URL + "/v1",
		Auth:       "oauth",
		TimeoutSec: 5,
		OAuth: config.ProviderOAuthConfig{
			Provider:       "codex",
			CredentialFile: credFile,
			ClientID:       "test-client",
			TokenURL:       target.URL + "/oauth/token",
			AuthURL:        target.URL + "/oauth/authorize",
		},
	}
	oauth, err := newOAuthManager(pc, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}
	defer oauth.bgCancel()

	provider := NewHTTPProvider("test-oauth-proxy", "", pc.APIBase, "gpt-test", false, "oauth", 5*time.Second, oauth)
	resp, err := provider.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil, "gpt-test", nil)
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if resp.Content != "ok-via-proxy" {
		t.Fatalf("unexpected response content: %q", resp.Content)
	}
	if atomic.LoadInt32(&refreshCalls) != 1 {
		t.Fatalf("expected one refresh call, got %d", refreshCalls)
	}
	if atomic.LoadInt32(&responseCalls) != 1 {
		t.Fatalf("expected one response call, got %d", responseCalls)
	}
	if got := atomic.LoadInt32(&proxyCalls); got < 2 {
		t.Fatalf("expected proxy to receive refresh and response requests, got %d", got)
	}
}

func TestOAuthImportGeminiNestedTokenRefreshesWithTokenMetadata(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form failed: %v", err)
			}
			if got := r.Form.Get("client_secret"); got != "secret-1" {
				t.Fatalf("unexpected client_secret: %s", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"gemini-fresh","refresh_token":"gemini-refresh","expires_in":3600}`))
		case "/userinfo":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"email":"gemini@example.com"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	manager, err := newOAuthManager(config.ProviderConfig{
		Auth: "oauth",
		OAuth: config.ProviderOAuthConfig{
			Provider:     "gemini",
			TokenURL:     server.URL + "/oauth/token",
			ClientID:     "client-1",
			ClientSecret: "secret-1",
		},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}
	manager.cfg.UserInfoURL = server.URL + "/userinfo"

	raw := []byte(`{
	  "type": "gemini",
	  "email": "gemini@example.com",
	  "project_id": "demo-project",
	  "token": {
	    "refresh_token": "gemini-refresh",
	    "client_id": "client-1",
	    "client_secret": "secret-1",
	    "token_uri": "` + server.URL + `/oauth/token"
	  }
	}`)
	session, err := parseImportedOAuthSession("gemini", "gemini.json", raw)
	if err != nil {
		t.Fatalf("parse imported oauth session failed: %v", err)
	}
	refreshed, err := manager.refreshImportedSession(context.Background(), session)
	if err != nil {
		t.Fatalf("refresh imported session failed: %v", err)
	}
	if refreshed.AccessToken != "gemini-fresh" {
		t.Fatalf("unexpected access token: %s", refreshed.AccessToken)
	}
	if refreshed.Email != "gemini@example.com" {
		t.Fatalf("unexpected email: %s", refreshed.Email)
	}
	if refreshed.ProjectID != "demo-project" {
		t.Fatalf("unexpected project id: %s", refreshed.ProjectID)
	}
}

func TestAntigravityEnrichSessionAddsEmailAndProjectID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/userinfo":
			if got := r.Header.Get("Authorization"); got != "Bearer antigravity-token" {
				t.Fatalf("unexpected userinfo authorization: %s", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"email":"antigravity@example.com"}`))
		case "/v1internal:loadCodeAssist":
			if got := r.Header.Get("Authorization"); got != "Bearer antigravity-token" {
				t.Fatalf("unexpected project authorization: %s", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"cloudaicompanionProject":"project-123"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	originalAPI := defaultAntigravityAPIEndpoint
	originalUserInfo := defaultAntigravityUserInfoURL
	defaultAntigravityAPIEndpoint = server.URL
	defaultAntigravityUserInfoURL = server.URL + "/userinfo"
	t.Cleanup(func() {
		defaultAntigravityAPIEndpoint = originalAPI
		defaultAntigravityUserInfoURL = originalUserInfo
	})

	manager, err := newOAuthManager(config.ProviderConfig{
		Auth:       "oauth",
		TimeoutSec: 5,
		OAuth: config.ProviderOAuthConfig{
			Provider:       "antigravity",
			ClientID:       "client-id",
			ClientSecret:   "client-secret",
			AuthURL:        server.URL + "/oauth/authorize",
			RedirectURL:    "http://localhost:51121/oauth-callback",
			RefreshLeadSec: 300,
		},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}
	defer manager.bgCancel()

	session, err := manager.enrichSession(context.Background(), &oauthSession{
		Provider:    "antigravity",
		AccessToken: "antigravity-token",
	})
	if err != nil {
		t.Fatalf("enrich session failed: %v", err)
	}
	if session.Email != "antigravity@example.com" {
		t.Fatalf("unexpected email: %#v", session)
	}
	if session.ProjectID != "project-123" {
		t.Fatalf("expected project id enrichment, got %#v", session)
	}
}

func TestQwenDeviceFlowRequiresAccountLabelWhenEmailMissing(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"device_code":"dev-1","user_code":"user-1","verification_uri_complete":"https://chat.qwen.ai/device?code=user-1","interval":1,"expires_in":60}`))
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"qwen-token","refresh_token":"refresh-token","expires_in":3600}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	manager, err := newOAuthManager(config.ProviderConfig{
		Auth:       "oauth",
		TimeoutSec: 5,
		OAuth: config.ProviderOAuthConfig{
			Provider:       "qwen",
			AuthURL:        server.URL + "/device",
			TokenURL:       server.URL + "/token",
			CredentialFile: filepath.Join(t.TempDir(), "qwen.json"),
		},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}
	defer manager.bgCancel()

	flow, err := manager.startDeviceFlow(context.Background(), OAuthLoginOptions{})
	if err != nil {
		t.Fatalf("start device flow failed: %v", err)
	}
	_, _, err = manager.completeDeviceFlow(context.Background(), "", flow, OAuthLoginOptions{})
	if err == nil || !strings.Contains(err.Error(), "account_label") {
		t.Fatalf("expected qwen account_label error, got %v", err)
	}

	session, _, err := manager.completeDeviceFlow(context.Background(), "", flow, OAuthLoginOptions{AccountLabel: "qwen-alias"})
	if err != nil {
		t.Fatalf("complete device flow with label failed: %v", err)
	}
	if session.Email != "qwen-alias" {
		t.Fatalf("expected qwen alias persisted as email label, got %#v", session)
	}
}

func TestImportSessionEnrichesAntigravityMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/userinfo":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"email":"imported@example.com"}`))
		case "/v1internal:loadCodeAssist":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"cloudaicompanionProject":"import-project"}`))
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"g-model"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	originalAPI := defaultAntigravityAPIEndpoint
	originalUserInfo := defaultAntigravityUserInfoURL
	defaultAntigravityAPIEndpoint = server.URL
	defaultAntigravityUserInfoURL = server.URL + "/userinfo"
	t.Cleanup(func() {
		defaultAntigravityAPIEndpoint = originalAPI
		defaultAntigravityUserInfoURL = originalUserInfo
	})

	manager, err := newOAuthManager(config.ProviderConfig{
		APIBase:    server.URL + "/v1",
		Auth:       "oauth",
		TimeoutSec: 5,
		OAuth: config.ProviderOAuthConfig{
			Provider:       "antigravity",
			CredentialFile: filepath.Join(t.TempDir(), "antigravity.json"),
		},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}
	defer manager.bgCancel()

	raw := []byte(`{"access_token":"import-token","refresh_token":"refresh-token","expired":"2030-01-01T00:00:00Z"}`)
	session, models, err := manager.importSession(context.Background(), server.URL+"/v1", "auth.json", raw, OAuthLoginOptions{})
	if err != nil {
		t.Fatalf("import session failed: %v", err)
	}
	if session.Email != "imported@example.com" || session.ProjectID != "import-project" {
		t.Fatalf("expected antigravity enrichment, got %#v", session)
	}
	if len(models) != 1 || models[0] != "g-model" {
		t.Fatalf("unexpected models: %#v", models)
	}
}

func TestPersistSessionAddsGeminiTokenMetadata(t *testing.T) {
	t.Parallel()

	credFile := filepath.Join(t.TempDir(), "gemini.json")
	manager, err := newOAuthManager(config.ProviderConfig{
		Auth:       "oauth",
		TimeoutSec: 5,
		OAuth: config.ProviderOAuthConfig{
			Provider:       "gemini",
			CredentialFile: credFile,
		},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}
	defer manager.bgCancel()

	manager.mu.Lock()
	err = manager.persistSessionLocked(&oauthSession{
		Provider:     "gemini",
		AccessToken:  "gem-access",
		RefreshToken: "gem-refresh",
		Expire:       "2030-01-01T00:00:00Z",
		Email:        "gem@example.com",
		FilePath:     credFile,
	})
	manager.mu.Unlock()
	if err != nil {
		t.Fatalf("persist session failed: %v", err)
	}
	raw, err := os.ReadFile(credFile)
	if err != nil {
		t.Fatalf("read credential file failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal credential file failed: %v", err)
	}
	tokenMap, _ := payload["token"].(map[string]any)
	if tokenMap == nil {
		t.Fatalf("expected token map in persisted gemini session, got %s", string(raw))
	}
	if tokenMap["token_uri"] != defaultGeminiTokenURL {
		t.Fatalf("unexpected gemini token metadata: %#v", tokenMap)
	}
	if defaultGeminiClientID != "" && tokenMap["client_id"] != defaultGeminiClientID {
		t.Fatalf("unexpected gemini client_id metadata: %#v", tokenMap)
	}
	if defaultGeminiClientSecret != "" && tokenMap["client_secret"] != defaultGeminiClientSecret {
		t.Fatalf("unexpected gemini client_secret metadata: %#v", tokenMap)
	}
}

func TestParseImportedOAuthSessionSupportsAliasProjectAndDeviceVariants(t *testing.T) {
	t.Parallel()

	session, err := parseImportedOAuthSession("qwen", "auth.json", []byte(`{
		"refresh_token": "rt-1",
		"token": {
			"access_token": "at-1",
			"account_label": "alias-qwen",
			"projectId": "proj-1",
			"deviceId": "dev-1",
			"scopes": "openid profile"
		}
	}`))
	if err != nil {
		t.Fatalf("parse imported session failed: %v", err)
	}
	if session.Email != "alias-qwen" || session.ProjectID != "proj-1" || session.DeviceID != "dev-1" || session.Scope != "openid profile" {
		t.Fatalf("unexpected parsed session: %#v", session)
	}
}

func TestOAuthDeviceFlowQwenManualCompletes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"device_code":"dev-1","user_code":"user-1","verification_uri_complete":"https://chat.qwen.ai/device?code=user-1","interval":1,"expires_in":60}`))
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form failed: %v", err)
			}
			if got := r.Form.Get("device_code"); got != "dev-1" {
				t.Fatalf("unexpected device_code: %s", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"qwen-at","refresh_token":"qwen-rt","expires_in":3600}`))
		case "/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen-test"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	manager, err := newOAuthManager(config.ProviderConfig{
		APIBase: server.URL,
		Auth:    "oauth",
		OAuth: config.ProviderOAuthConfig{
			Provider:       "qwen",
			CredentialFile: filepath.Join(dir, "qwen.json"),
			AuthURL:        server.URL + "/device",
			TokenURL:       server.URL + "/token",
		},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}

	flow, err := manager.startDeviceFlow(context.Background(), OAuthLoginOptions{})
	if err != nil {
		t.Fatalf("start device flow failed: %v", err)
	}
	if flow.Mode != oauthFlowDevice {
		t.Fatalf("unexpected flow mode: %s", flow.Mode)
	}
	session, models, err := manager.completeDeviceFlow(context.Background(), server.URL, flow, OAuthLoginOptions{AccountLabel: "qwen-label"})
	if err != nil {
		t.Fatalf("complete device flow failed: %v", err)
	}
	if session.AccessToken != "qwen-at" {
		t.Fatalf("unexpected access token: %s", session.AccessToken)
	}
	if session.FilePath == "" {
		t.Fatalf("expected credential file path")
	}
	if session.Email != "qwen-label" {
		t.Fatalf("expected qwen label, got %#v", session)
	}
	if len(models) != 1 || models[0] != "qwen-test" {
		t.Fatalf("unexpected models: %#v", models)
	}
}

func TestHTTPProviderHybridFallsBackFromAPIKeyToOAuth(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	credFile := filepath.Join(dir, "oauth.json")
	raw, err := json.Marshal(oauthSession{
		Provider:    "codex",
		AccessToken: "oauth-token",
		Email:       "oauth@example.com",
		Expire:      time.Now().Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal session failed: %v", err)
	}
	if err := os.WriteFile(credFile, raw, 0o600); err != nil {
		t.Fatalf("write session failed: %v", err)
	}

	var apiKeyCalls int32
	var oauthCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		switch r.Header.Get("Authorization") {
		case "Bearer api-key-1":
			atomic.AddInt32(&apiKeyCalls, 1)
			w.WriteHeader(http.StatusTooManyRequests)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"error":{"code":"insufficient_quota","message":"quota exceeded"}}`))
		case "Bearer oauth-token":
			atomic.AddInt32(&oauthCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"completed","output_text":"ok-from-oauth"}`))
		default:
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
	}))
	defer server.Close()

	pc := config.ProviderConfig{
		APIBase:    server.URL + "/v1",
		APIKey:     "api-key-1",
		Auth:       "hybrid",
		TimeoutSec: 5,
		OAuth: config.ProviderOAuthConfig{
			Provider:       "codex",
			CredentialFile: credFile,
			TokenURL:       server.URL + "/oauth/token",
			AuthURL:        server.URL + "/oauth/authorize",
		},
	}
	oauth, err := newOAuthManager(pc, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}
	provider := NewHTTPProvider("test-hybrid-fallback", pc.APIKey, pc.APIBase, "gpt-test", false, "hybrid", 5*time.Second, oauth)
	resp, err := provider.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil, "gpt-test", nil)
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if resp.Content != "ok-from-oauth" {
		t.Fatalf("unexpected response content: %q", resp.Content)
	}
	if atomic.LoadInt32(&apiKeyCalls) != 1 || atomic.LoadInt32(&oauthCalls) != 1 {
		t.Fatalf("expected one api-key and one oauth attempt, got api=%d oauth=%d", apiKeyCalls, oauthCalls)
	}
}

func TestHTTPProviderHybridOAuthFirstUsesOAuthBeforeAPIKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	credFile := filepath.Join(dir, "oauth.json")
	raw, err := json.Marshal(oauthSession{
		Provider:    "codex",
		AccessToken: "oauth-token",
		Email:       "oauth@example.com",
		Expire:      time.Now().Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal session failed: %v", err)
	}
	if err := os.WriteFile(credFile, raw, 0o600); err != nil {
		t.Fatalf("write session failed: %v", err)
	}

	var apiKeyCalls int32
	var oauthCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		switch r.Header.Get("Authorization") {
		case "Bearer api-key-1":
			atomic.AddInt32(&apiKeyCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"completed","output_text":"ok-from-api"}`))
		case "Bearer oauth-token":
			atomic.AddInt32(&oauthCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"completed","output_text":"ok-from-oauth"}`))
		default:
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
	}))
	defer server.Close()

	pc := config.ProviderConfig{
		APIBase:    server.URL + "/v1",
		APIKey:     "api-key-1",
		Auth:       "hybrid",
		TimeoutSec: 5,
		OAuth: config.ProviderOAuthConfig{
			Provider:       "codex",
			CredentialFile: credFile,
			TokenURL:       server.URL + "/oauth/token",
			AuthURL:        server.URL + "/oauth/authorize",
		},
	}
	oauth, err := newOAuthManager(pc, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}
	provider := NewHTTPProvider("test-hybrid-oauth-first", pc.APIKey, pc.APIBase, "gpt-test", false, "hybrid", 5*time.Second, oauth)
	resp, err := provider.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil, "gpt-test", nil)
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if resp.Content != "ok-from-api" {
		t.Fatalf("unexpected response content: %q", resp.Content)
	}
	if atomic.LoadInt32(&apiKeyCalls) != 1 || atomic.LoadInt32(&oauthCalls) != 0 {
		t.Fatalf("expected api key first only, got api=%d oauth=%d", apiKeyCalls, oauthCalls)
	}
}

func TestOAuthManagerCooldownSkipsExhaustedAccount(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	firstFile := filepath.Join(dir, "first.json")
	secondFile := filepath.Join(dir, "second.json")
	writeSession := func(path, token string) {
		t.Helper()
		raw, err := json.Marshal(oauthSession{
			Provider:    "codex",
			AccessToken: token,
			Expire:      time.Now().Add(time.Hour).Format(time.RFC3339),
		})
		if err != nil {
			t.Fatalf("marshal session failed: %v", err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatalf("write session failed: %v", err)
		}
	}
	writeSession(firstFile, "token-a")
	writeSession(secondFile, "token-b")

	manager, err := newOAuthManager(config.ProviderConfig{
		Auth:       "oauth",
		TimeoutSec: 5,
		OAuth: config.ProviderOAuthConfig{
			Provider:        "codex",
			CredentialFile:  firstFile,
			CredentialFiles: []string{firstFile, secondFile},
			CooldownSec:     3600,
		},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}

	attempts, err := manager.prepareAttemptsLocked(context.Background())
	if err != nil {
		t.Fatalf("prepare attempts failed: %v", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(attempts))
	}
	manager.markExhausted(attempts[0].Session, oauthFailureRateLimit)
	nextAttempts, err := manager.prepareAttemptsLocked(context.Background())
	if err != nil {
		t.Fatalf("prepare attempts after cooldown failed: %v", err)
	}
	if len(nextAttempts) != 1 {
		t.Fatalf("expected 1 available attempt after cooldown, got %d", len(nextAttempts))
	}
	if nextAttempts[0].Token != "token-b" {
		t.Fatalf("unexpected token after cooldown: %s", nextAttempts[0].Token)
	}
	accounts, err := (&OAuthLoginManager{manager: manager}).ListAccounts()
	if err != nil {
		t.Fatalf("list accounts failed: %v", err)
	}
	foundCooldown := false
	for _, account := range accounts {
		if account.CredentialFile == firstFile && account.CooldownUntil != "" {
			foundCooldown = true
		}
	}
	if !foundCooldown {
		t.Fatalf("expected cooldown metadata to be exposed in account list")
	}
}

func TestOAuthManagerDisableSessionSkipsAccount(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	firstFile := filepath.Join(dir, "first.json")
	secondFile := filepath.Join(dir, "second.json")
	writeSession := func(path, token string) {
		t.Helper()
		raw, err := json.Marshal(oauthSession{
			Provider:    "codex",
			AccessToken: token,
			Expire:      time.Now().Add(time.Hour).Format(time.RFC3339),
		})
		if err != nil {
			t.Fatalf("marshal session failed: %v", err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatalf("write session failed: %v", err)
		}
	}
	writeSession(firstFile, "token-a")
	writeSession(secondFile, "token-b")

	manager, err := newOAuthManager(config.ProviderConfig{
		Auth:       "oauth",
		TimeoutSec: 5,
		OAuth: config.ProviderOAuthConfig{
			Provider:        "codex",
			CredentialFile:  firstFile,
			CredentialFiles: []string{firstFile, secondFile},
			CooldownSec:     3600,
		},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}

	attempts, err := manager.prepareAttemptsLocked(context.Background())
	if err != nil {
		t.Fatalf("prepare attempts failed: %v", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(attempts))
	}
	manager.disableSession(attempts[0].Session, oauthFailureRevoked, "oauth token revoked")

	nextAttempts, err := manager.prepareAttemptsLocked(context.Background())
	if err != nil {
		t.Fatalf("prepare attempts after disable failed: %v", err)
	}
	if len(nextAttempts) != 1 {
		t.Fatalf("expected 1 available attempt after disable, got %d", len(nextAttempts))
	}
	if nextAttempts[0].Token != "token-b" {
		t.Fatalf("unexpected token after disable: %s", nextAttempts[0].Token)
	}

	raw, err := os.ReadFile(firstFile)
	if err != nil {
		t.Fatalf("read disabled session failed: %v", err)
	}
	var saved oauthSession
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("unmarshal disabled session failed: %v", err)
	}
	if !saved.Disabled || saved.DisableReason != string(oauthFailureRevoked) {
		t.Fatalf("expected disabled session to persist, got %#v", saved)
	}
}

func TestOAuthManagerPrefersHealthierAccount(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	firstFile := filepath.Join(dir, "first.json")
	secondFile := filepath.Join(dir, "second.json")
	writeSession := func(path, token string) {
		t.Helper()
		raw, err := json.Marshal(oauthSession{
			Provider:    "codex",
			AccessToken: token,
			Expire:      time.Now().Add(time.Hour).Format(time.RFC3339),
		})
		if err != nil {
			t.Fatalf("marshal session failed: %v", err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatalf("write session failed: %v", err)
		}
	}
	writeSession(firstFile, "token-a")
	writeSession(secondFile, "token-b")

	manager, err := newOAuthManager(config.ProviderConfig{
		Auth:       "oauth",
		TimeoutSec: 5,
		OAuth: config.ProviderOAuthConfig{
			Provider:        "codex",
			CredentialFile:  firstFile,
			CredentialFiles: []string{firstFile, secondFile},
			CooldownSec:     60,
		},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}

	attempts, err := manager.prepareAttemptsLocked(context.Background())
	if err != nil {
		t.Fatalf("prepare attempts failed: %v", err)
	}
	manager.markExhausted(attempts[0].Session, oauthFailureQuota)
	delete(manager.cooldowns, attempts[0].Session.FilePath)
	attempts, err = manager.prepareAttemptsLocked(context.Background())
	if err != nil {
		t.Fatalf("prepare attempts after health drop failed: %v", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(attempts))
	}
	if attempts[0].Token != "token-b" {
		t.Fatalf("expected healthier token-b first, got %s", attempts[0].Token)
	}
}

func TestOAuthLoginManagerListAccountsIncludesCodexPlanMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	credFile := filepath.Join(dir, "codex-plan.json")
	idToken := buildTestJWT(map[string]any{
		"email": "plan@example.com",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id":                "acct-plan",
			"chatgpt_plan_type":                 "pro",
			"chatgpt_subscription_active_start": "2026-03-01T00:00:00Z",
			"chatgpt_subscription_active_until": "2026-04-01T00:00:00Z",
		},
	})
	raw, err := json.Marshal(oauthSession{
		Provider:     "codex",
		AccessToken:  "token-plan",
		RefreshToken: "refresh-plan",
		IDToken:      idToken,
		Expire:       time.Now().Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal session failed: %v", err)
	}
	if err := os.WriteFile(credFile, raw, 0o600); err != nil {
		t.Fatalf("write session failed: %v", err)
	}

	manager, err := newOAuthManager(config.ProviderConfig{
		TimeoutSec: 5,
		OAuth: config.ProviderOAuthConfig{
			Provider:       "codex",
			CredentialFile: credFile,
		},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}

	accounts, err := (&OAuthLoginManager{manager: manager}).ListAccounts()
	if err != nil {
		t.Fatalf("list accounts failed: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected one account, got %#v", accounts)
	}
	account := accounts[0]
	if account.PlanType != "pro" {
		t.Fatalf("expected plan type to be extracted, got %#v", account)
	}
	if account.BalanceLabel != "PRO" || account.SubActiveUntil != "2026-04-01T00:00:00Z" {
		t.Fatalf("expected subscription metadata in account info, got %#v", account)
	}
}

func buildTestJWT(claims map[string]any) string {
	header, _ := json.Marshal(map[string]any{"alg": "none", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	return base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
}

func TestClassifyOAuthFailureDifferentiatesReasons(t *testing.T) {
	t.Parallel()

	reason, retry := classifyOAuthFailure(http.StatusTooManyRequests, []byte(`{"error":{"code":"insufficient_quota"}}`))
	if !retry || reason != oauthFailureQuota {
		t.Fatalf("expected quota classification, got retry=%v reason=%s", retry, reason)
	}
	reason, retry = classifyOAuthFailure(http.StatusTooManyRequests, []byte(`{"error":{"message":"rate limit exceeded"}}`))
	if !retry || reason != oauthFailureRateLimit {
		t.Fatalf("expected rate-limit classification, got retry=%v reason=%s", retry, reason)
	}
	reason, retry = classifyOAuthFailure(http.StatusForbidden, []byte(`{"error":"forbidden"}`))
	if !retry || reason != oauthFailureForbidden {
		t.Fatalf("expected forbidden classification, got retry=%v reason=%s", retry, reason)
	}
}

func TestHTTPProviderHybridSkipsAPIKeyDuringCooldown(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	credFile := filepath.Join(dir, "oauth.json")
	raw, err := json.Marshal(oauthSession{
		Provider:    "codex",
		AccessToken: "oauth-token",
		Expire:      time.Now().Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal session failed: %v", err)
	}
	if err := os.WriteFile(credFile, raw, 0o600); err != nil {
		t.Fatalf("write session failed: %v", err)
	}

	providerRuntimeRegistry.mu.Lock()
	providerRuntimeRegistry.api["cooldown-provider"] = providerRuntimeState{
		API: providerAPIRuntimeState{
			TokenMasked:   "api-***",
			HealthScore:   50,
			CooldownUntil: time.Now().Add(10 * time.Minute).Format(time.RFC3339),
		},
	}
	providerRuntimeRegistry.mu.Unlock()

	manager, err := newOAuthManager(config.ProviderConfig{
		Auth: "hybrid",
		OAuth: config.ProviderOAuthConfig{
			Provider:       "codex",
			CredentialFile: credFile,
		},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}
	provider := NewHTTPProvider("cooldown-provider", "api-key-1", "https://example.com/v1", "gpt-test", false, "hybrid", 5*time.Second, manager)
	attempts, err := provider.authAttempts(context.Background())
	if err != nil {
		t.Fatalf("auth attempts failed: %v", err)
	}
	if len(attempts) != 1 || attempts[0].kind != "oauth" {
		t.Fatalf("expected only oauth attempt during api cooldown, got %#v", attempts)
	}
}

func TestClearProviderAPICooldownRestoresAPIKeyAttempt(t *testing.T) {
	t.Parallel()

	providerRuntimeRegistry.mu.Lock()
	providerRuntimeRegistry.api["clear-api-provider"] = providerRuntimeState{
		API: providerAPIRuntimeState{
			TokenMasked:   "api-***",
			HealthScore:   50,
			CooldownUntil: time.Now().Add(10 * time.Minute).Format(time.RFC3339),
		},
	}
	providerRuntimeRegistry.mu.Unlock()

	provider := NewHTTPProvider("clear-api-provider", "api-key-1", "https://example.com/v1", "gpt-test", false, "bearer", 5*time.Second, nil)
	if _, err := provider.authAttempts(context.Background()); err == nil {
		t.Fatalf("expected api key attempt to be blocked by cooldown")
	}
	ClearProviderAPICooldown("clear-api-provider")
	attempts, err := provider.authAttempts(context.Background())
	if err != nil {
		t.Fatalf("expected api key attempt after clear cooldown, got %v", err)
	}
	if len(attempts) != 1 || attempts[0].kind != "api_key" {
		t.Fatalf("unexpected attempts after clear cooldown: %#v", attempts)
	}
}

func TestOAuthLoginManagerClearCooldown(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	credFile := filepath.Join(dir, "oauth.json")
	raw, err := json.Marshal(oauthSession{
		Provider:    "codex",
		AccessToken: "oauth-token",
		Expire:      time.Now().Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal session failed: %v", err)
	}
	if err := os.WriteFile(credFile, raw, 0o600); err != nil {
		t.Fatalf("write session failed: %v", err)
	}
	manager, err := newOAuthManager(config.ProviderConfig{
		Auth: "oauth",
		OAuth: config.ProviderOAuthConfig{
			Provider:       "codex",
			CredentialFile: credFile,
			CooldownSec:    3600,
		},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}
	attempts, err := manager.prepareAttemptsLocked(context.Background())
	if err != nil || len(attempts) != 1 {
		t.Fatalf("prepare attempts failed: %v %#v", err, attempts)
	}
	manager.markExhausted(attempts[0].Session, oauthFailureRateLimit)
	loginMgr := &OAuthLoginManager{manager: manager}
	if err := loginMgr.ClearCooldown(credFile); err != nil {
		t.Fatalf("clear cooldown failed: %v", err)
	}
	next, err := manager.prepareAttemptsLocked(context.Background())
	if err != nil || len(next) != 1 {
		t.Fatalf("expected session available after cooldown clear, got err=%v attempts=%#v", err, next)
	}
}

func TestProviderRuntimeSnapshotIncludesCandidateOrderAndLastSuccess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	credFile := filepath.Join(dir, "oauth.json")
	raw, err := json.Marshal(oauthSession{
		Provider:     "codex",
		AccessToken:  "oauth-token",
		RefreshToken: "refresh-token",
		Email:        "user@example.com",
		Expire:       time.Now().Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal session failed: %v", err)
	}
	if err := os.WriteFile(credFile, raw, 0o600); err != nil {
		t.Fatalf("write session failed: %v", err)
	}

	name := "runtime-snapshot-provider"
	pc := config.ProviderConfig{
		APIKey:             "api-key-123456",
		APIBase:            "https://example.com/v1",
		Auth:               "hybrid",
		TimeoutSec:         5,
		RuntimePersist:     true,
		RuntimeHistoryFile: filepath.Join(dir, "runtime.json"),
		OAuth: config.ProviderOAuthConfig{
			Provider:       "codex",
			CredentialFile: credFile,
		},
	}
	ConfigureProviderRuntime(name, pc)
	manager, err := newOAuthManager(pc, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}
	provider := NewHTTPProvider(name, pc.APIKey, pc.APIBase, "gpt-test", false, pc.Auth, 5*time.Second, manager)
	attempts, err := provider.authAttempts(context.Background())
	if err != nil {
		t.Fatalf("auth attempts failed: %v", err)
	}
	if len(attempts) != 2 || attempts[0].kind != "api_key" || attempts[1].kind != "oauth" {
		t.Fatalf("unexpected attempts order: %#v", attempts)
	}
	provider.markAttemptSuccess(attempts[1])

	cfg := &config.Config{
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{name: pc},
		},
	}
	snapshot := GetProviderRuntimeSnapshot(cfg)
	items, _ := snapshot["items"].([]map[string]interface{})
	if len(items) == 0 {
		t.Fatalf("expected provider runtime items")
	}
	item := items[0]
	candidates, _ := item["candidate_order"].([]providerRuntimeCandidate)
	if len(candidates) < 2 {
		t.Fatalf("expected candidate order, got %#v", item["candidate_order"])
	}
	if candidates[0].Kind != "api_key" || candidates[1].Kind != "oauth" {
		t.Fatalf("unexpected candidate order: %#v", candidates)
	}
	lastSuccess, _ := item["last_success"].(*providerRuntimeEvent)
	if lastSuccess == nil || lastSuccess.Kind != "oauth" || lastSuccess.Target != "user@example.com" {
		t.Fatalf("unexpected last success: %#v", item["last_success"])
	}
	if _, err := os.Stat(pc.RuntimeHistoryFile); err != nil {
		t.Fatalf("expected runtime history file, got %v", err)
	}
}

func TestConfigureProviderRuntimeLoadsPersistedEvents(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	name := "persisted-runtime-provider"
	historyFile := filepath.Join(dir, "runtime.json")
	payload := providerRuntimeState{
		RecentHits: []providerRuntimeEvent{{
			When:   time.Now().Add(-time.Minute).Format(time.RFC3339),
			Kind:   "oauth",
			Target: "persisted@example.com",
			Reason: "ok",
		}},
		LastSuccess: &providerRuntimeEvent{
			When:   time.Now().Add(-time.Minute).Format(time.RFC3339),
			Kind:   "oauth",
			Target: "persisted@example.com",
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal runtime payload failed: %v", err)
	}
	if err := os.WriteFile(historyFile, raw, 0o600); err != nil {
		t.Fatalf("write history file failed: %v", err)
	}

	ConfigureProviderRuntime(name, config.ProviderConfig{
		APIBase:            "https://example.com/v1",
		Auth:               "bearer",
		RuntimePersist:     true,
		RuntimeHistoryFile: historyFile,
	})

	cfg := &config.Config{
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				name: {
					APIBase:            "https://example.com/v1",
					Auth:               "bearer",
					RuntimePersist:     true,
					RuntimeHistoryFile: historyFile,
				},
			},
		},
	}
	snapshot := GetProviderRuntimeSnapshot(cfg)
	items, _ := snapshot["items"].([]map[string]interface{})
	if len(items) == 0 {
		t.Fatalf("expected provider runtime item")
	}
	lastSuccess, _ := items[0]["last_success"].(*providerRuntimeEvent)
	if lastSuccess == nil || lastSuccess.Target != "persisted@example.com" {
		t.Fatalf("expected persisted last success, got %#v", items[0]["last_success"])
	}
	hits, _ := items[0]["recent_hits"].([]providerRuntimeEvent)
	if len(hits) == 0 || hits[0].Target != "persisted@example.com" {
		t.Fatalf("expected persisted recent hits, got %#v", items[0]["recent_hits"])
	}
}

func TestClearProviderRuntimeHistoryRemovesPersistedFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	name := "clear-runtime-history-provider"
	historyFile := filepath.Join(dir, "runtime.json")
	ConfigureProviderRuntime(name, config.ProviderConfig{
		APIBase:            "https://example.com/v1",
		Auth:               "bearer",
		RuntimePersist:     true,
		RuntimeHistoryFile: historyFile,
	})

	providerRuntimeRegistry.mu.Lock()
	state := providerRuntimeRegistry.api[name]
	state.RecentHits = []providerRuntimeEvent{{When: time.Now().Format(time.RFC3339), Kind: "api_key", Target: "api***"}}
	state.LastSuccess = &providerRuntimeEvent{When: time.Now().Format(time.RFC3339), Kind: "api_key", Target: "api***"}
	persistProviderRuntimeLocked(name, state)
	providerRuntimeRegistry.api[name] = state
	providerRuntimeRegistry.mu.Unlock()

	if _, err := os.Stat(historyFile); err != nil {
		t.Fatalf("expected runtime history file, got %v", err)
	}
	ClearProviderRuntimeHistory(name)
	if _, err := os.Stat(historyFile); !os.IsNotExist(err) {
		t.Fatalf("expected runtime history file removed, got %v", err)
	}

	providerRuntimeRegistry.mu.Lock()
	cleared := providerRuntimeRegistry.api[name]
	providerRuntimeRegistry.mu.Unlock()
	if len(cleared.RecentHits) != 0 || len(cleared.RecentErrors) != 0 || cleared.LastSuccess != nil {
		t.Fatalf("expected runtime history cleared, got %#v", cleared)
	}
}

func TestUpdateCandidateOrderRecordsSchedulerChange(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	credFile := filepath.Join(dir, "oauth.json")
	raw, err := json.Marshal(oauthSession{
		Provider:    "codex",
		AccessToken: "oauth-token",
		Email:       "user@example.com",
		Expire:      time.Now().Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal session failed: %v", err)
	}
	if err := os.WriteFile(credFile, raw, 0o600); err != nil {
		t.Fatalf("write session failed: %v", err)
	}

	name := "candidate-change-provider"
	pc := config.ProviderConfig{
		APIKey:     "api-key-123456",
		APIBase:    "https://example.com/v1",
		Auth:       "hybrid",
		TimeoutSec: 5,
		OAuth: config.ProviderOAuthConfig{
			Provider:       "codex",
			CredentialFile: credFile,
		},
	}
	manager, err := newOAuthManager(pc, 5*time.Second)
	if err != nil {
		t.Fatalf("new oauth manager failed: %v", err)
	}
	provider := NewHTTPProvider(name, pc.APIKey, pc.APIBase, "gpt-test", false, pc.Auth, 5*time.Second, manager)
	attempts, err := provider.authAttempts(context.Background())
	if err != nil {
		t.Fatalf("auth attempts failed: %v", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("unexpected attempts: %#v", attempts)
	}
	provider.markAPIKeyFailure(oauthFailureRateLimit)
	attempts, err = provider.authAttempts(context.Background())
	if err != nil {
		t.Fatalf("auth attempts after cooldown failed: %v", err)
	}
	if len(attempts) != 1 || attempts[0].kind != "oauth" {
		t.Fatalf("unexpected attempts after cooldown: %#v", attempts)
	}

	providerRuntimeRegistry.mu.Lock()
	state := providerRuntimeRegistry.api[name]
	providerRuntimeRegistry.mu.Unlock()
	if len(state.RecentChanges) == 0 || state.RecentChanges[0].Reason != "candidate_order_changed" {
		t.Fatalf("expected scheduler change event, got %#v", state.RecentChanges)
	}
	if !strings.Contains(state.RecentChanges[0].Detail, "top ") {
		t.Fatalf("expected candidate order detail, got %#v", state.RecentChanges[0])
	}
}

func TestGetProviderRuntimeViewFiltersEvents(t *testing.T) {
	t.Parallel()

	name := "runtime-view-provider"
	providerRuntimeRegistry.mu.Lock()
	providerRuntimeRegistry.api[name] = providerRuntimeState{
		RecentHits: []providerRuntimeEvent{
			{When: time.Now().Add(-30 * time.Minute).Format(time.RFC3339), Kind: "oauth", Target: "user@example.com", Reason: "ok"},
			{When: time.Now().Add(-3 * time.Hour).Format(time.RFC3339), Kind: "api_key", Target: "api***", Reason: "ok"},
		},
		RecentErrors: []providerRuntimeEvent{
			{When: time.Now().Add(-10 * time.Minute).Format(time.RFC3339), Kind: "oauth", Target: "user@example.com", Reason: "quota"},
		},
		RecentChanges: []providerRuntimeEvent{
			{When: time.Now().Add(-5 * time.Minute).Format(time.RFC3339), Kind: "scheduler", Target: name, Reason: "candidate_order_changed", Detail: "top api -> oauth"},
		},
	}
	providerRuntimeRegistry.mu.Unlock()

	cfg := &config.Config{
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				name: {APIBase: "https://example.com/v1", Auth: "hybrid", APIKey: "api-key"},
			},
		},
	}
	view := GetProviderRuntimeView(cfg, ProviderRuntimeQuery{
		Provider:  name,
		Window:    2 * time.Hour,
		EventKind: "oauth",
		Limit:     1,
	})
	items, _ := view["items"].([]map[string]interface{})
	if len(items) != 1 {
		t.Fatalf("expected one runtime item, got %#v", view)
	}
	hits, _ := items[0]["recent_hits"].([]providerRuntimeEvent)
	if len(hits) != 1 || hits[0].Kind != "oauth" {
		t.Fatalf("expected filtered oauth hits, got %#v", items[0]["recent_hits"])
	}
	errors, _ := items[0]["recent_errors"].([]providerRuntimeEvent)
	if len(errors) != 1 || errors[0].Reason != "quota" {
		t.Fatalf("expected filtered oauth errors, got %#v", items[0]["recent_errors"])
	}
	changes, _ := items[0]["recent_changes"].([]providerRuntimeEvent)
	if len(changes) != 0 {
		t.Fatalf("expected no scheduler changes when filtering kind=oauth, got %#v", items[0]["recent_changes"])
	}
	events, _ := items[0]["events"].([]providerRuntimeEvent)
	if len(events) != 1 {
		t.Fatalf("expected merged paged events, got %#v", items[0]["events"])
	}
}

func TestGetProviderRuntimeViewCursorPagination(t *testing.T) {
	t.Parallel()

	name := "runtime-cursor-provider"
	now := time.Now()
	providerRuntimeRegistry.mu.Lock()
	providerRuntimeRegistry.api[name] = providerRuntimeState{
		RecentHits: []providerRuntimeEvent{
			{When: now.Add(-1 * time.Minute).Format(time.RFC3339), Kind: "oauth", Target: "a", Reason: "ok"},
			{When: now.Add(-2 * time.Minute).Format(time.RFC3339), Kind: "oauth", Target: "b", Reason: "ok"},
		},
		RecentErrors: []providerRuntimeEvent{
			{When: now.Add(-3 * time.Minute).Format(time.RFC3339), Kind: "oauth", Target: "c", Reason: "quota"},
		},
	}
	providerRuntimeRegistry.mu.Unlock()
	cfg := &config.Config{
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				name: {APIBase: "https://example.com/v1", Auth: "oauth", OAuth: config.ProviderOAuthConfig{Provider: "codex"}},
			},
		},
	}
	view := GetProviderRuntimeView(cfg, ProviderRuntimeQuery{Provider: name, Limit: 2, Cursor: 0})
	items, _ := view["items"].([]map[string]interface{})
	if len(items) != 1 {
		t.Fatalf("expected one item, got %#v", view)
	}
	page1, _ := items[0]["events"].([]providerRuntimeEvent)
	if len(page1) != 2 || items[0]["next_cursor"].(int) != 2 {
		t.Fatalf("unexpected first page %#v", items[0])
	}
	view = GetProviderRuntimeView(cfg, ProviderRuntimeQuery{Provider: name, Limit: 2, Cursor: 2})
	items, _ = view["items"].([]map[string]interface{})
	page2, _ := items[0]["events"].([]providerRuntimeEvent)
	if len(page2) != 1 || items[0]["next_cursor"].(int) != 0 {
		t.Fatalf("unexpected second page %#v", items[0])
	}
}

func TestGetProviderRuntimeViewSortAscending(t *testing.T) {
	t.Parallel()

	name := "runtime-sort-provider"
	now := time.Now()
	providerRuntimeRegistry.mu.Lock()
	providerRuntimeRegistry.api[name] = providerRuntimeState{
		RecentHits: []providerRuntimeEvent{
			{When: now.Add(-1 * time.Minute).Format(time.RFC3339), Kind: "oauth", Target: "a", Reason: "ok"},
			{When: now.Add(-3 * time.Minute).Format(time.RFC3339), Kind: "oauth", Target: "b", Reason: "ok"},
		},
		RecentErrors: []providerRuntimeEvent{
			{When: now.Add(-2 * time.Minute).Format(time.RFC3339), Kind: "oauth", Target: "c", Reason: "quota"},
		},
	}
	providerRuntimeRegistry.mu.Unlock()
	cfg := &config.Config{
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				name: {APIBase: "https://example.com/v1", Auth: "oauth", OAuth: config.ProviderOAuthConfig{Provider: "codex"}},
			},
		},
	}
	view := GetProviderRuntimeView(cfg, ProviderRuntimeQuery{Provider: name, Limit: 10, Sort: "asc"})
	items, _ := view["items"].([]map[string]interface{})
	if len(items) != 1 {
		t.Fatalf("expected one item, got %#v", view)
	}
	events, _ := items[0]["events"].([]providerRuntimeEvent)
	if len(events) != 3 {
		t.Fatalf("expected three events, got %#v", items[0]["events"])
	}
	if events[0].Target != "b" || events[1].Target != "c" || events[2].Target != "a" {
		t.Fatalf("expected ascending order oldest->newest, got %#v", events)
	}
}

func TestGetProviderRuntimeViewFiltersByHealthAndCooldown(t *testing.T) {
	t.Parallel()

	name := "runtime-health-provider"
	providerRuntimeRegistry.mu.Lock()
	providerRuntimeRegistry.api[name] = providerRuntimeState{
		API: providerAPIRuntimeState{
			HealthScore:   20,
			CooldownUntil: time.Now().Add(-5 * time.Minute).Format(time.RFC3339),
		},
		CandidateOrder: []providerRuntimeCandidate{{
			Kind:          "api_key",
			Target:        "api***",
			HealthScore:   20,
			CooldownUntil: time.Now().Add(-5 * time.Minute).Format(time.RFC3339),
		}},
	}
	providerRuntimeRegistry.mu.Unlock()
	cfg := &config.Config{
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				name: {APIBase: "https://example.com/v1", Auth: "bearer", APIKey: "api-key"},
			},
		},
	}
	view := GetProviderRuntimeView(cfg, ProviderRuntimeQuery{
		Provider:       name,
		HealthBelow:    30,
		CooldownBefore: time.Now(),
	})
	items, _ := view["items"].([]map[string]interface{})
	if len(items) != 1 {
		t.Fatalf("expected one filtered runtime item, got %#v", view)
	}
}

func TestGetProviderRuntimeSummaryFlagsUnhealthyProviders(t *testing.T) {
	t.Parallel()

	name := "runtime-summary-provider"
	lastSuccessAt := time.Now().Add(-2 * time.Hour).Format(time.RFC3339)
	topChangedAt := time.Now().Add(-3 * time.Minute).Format(time.RFC3339)
	providerRuntimeRegistry.mu.Lock()
	providerRuntimeRegistry.api[name] = providerRuntimeState{
		API: providerAPIRuntimeState{
			HealthScore:   25,
			CooldownUntil: time.Now().Add(15 * time.Minute).Format(time.RFC3339),
		},
		RecentErrors: []providerRuntimeEvent{
			{When: time.Now().Add(-5 * time.Minute).Format(time.RFC3339), Kind: "api_key", Target: "api***", Reason: "quota"},
		},
		RecentChanges: []providerRuntimeEvent{
			{When: topChangedAt, Kind: "scheduler", Target: name, Reason: "candidate_order_changed", Detail: "top oauth -> api"},
		},
		LastSuccess: &providerRuntimeEvent{
			When:   lastSuccessAt,
			Kind:   "api_key",
			Target: "api***",
			Reason: "ok",
		},
		CandidateOrder: []providerRuntimeCandidate{{
			Kind:          "api_key",
			Target:        "api***",
			Available:     false,
			Status:        "cooldown",
			HealthScore:   25,
			CooldownUntil: time.Now().Add(15 * time.Minute).Format(time.RFC3339),
		}},
	}
	providerRuntimeRegistry.mu.Unlock()
	cfg := &config.Config{
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				name: {APIBase: "https://example.com/v1", Auth: "bearer", APIKey: "api-key"},
			},
		},
	}
	summary := GetProviderRuntimeSummary(cfg, ProviderRuntimeQuery{HealthBelow: 30, Window: time.Hour})
	if summary.TotalProviders != 1 || summary.InCooldown != 1 || summary.LowHealth != 1 || summary.RecentErrors != 1 {
		t.Fatalf("unexpected summary counts: %#v", summary)
	}
	if summary.Critical != 1 || summary.Degraded != 0 || summary.Healthy != 0 {
		t.Fatalf("unexpected status counts: %#v", summary)
	}
	if len(summary.Providers) != 1 || summary.Providers[0].TopCandidate == nil || summary.Providers[0].TopCandidate.Kind != "api_key" {
		t.Fatalf("unexpected provider summary items: %#v", summary.Providers)
	}
	if summary.Providers[0].Status != "critical" {
		t.Fatalf("expected critical status, got %#v", summary.Providers[0])
	}
	if summary.Providers[0].LastError == nil || summary.Providers[0].LastErrorReason != "quota" || summary.Providers[0].LastErrorAt == "" {
		t.Fatalf("expected last error details, got %#v", summary.Providers[0])
	}
	if summary.Providers[0].LastSuccessAt != lastSuccessAt || summary.Providers[0].TopCandidateChangedAt != topChangedAt {
		t.Fatalf("expected last success and top candidate timestamps, got %#v", summary.Providers[0])
	}
	if summary.Providers[0].StaleForSec < 7100 || summary.Providers[0].StaleForSec > 7300 {
		t.Fatalf("expected stale_for_sec around 2h, got %#v", summary.Providers[0].StaleForSec)
	}
}

func TestGetProviderRuntimeSummaryMarksRecentErrorsAsDegraded(t *testing.T) {
	t.Parallel()

	name := "runtime-summary-degraded-provider"
	providerRuntimeRegistry.mu.Lock()
	providerRuntimeRegistry.api[name] = providerRuntimeState{
		RecentErrors: []providerRuntimeEvent{
			{When: time.Now().Add(-10 * time.Minute).Format(time.RFC3339), Kind: "oauth", Target: "user@example.com", Reason: "rate_limit"},
		},
		CandidateOrder: []providerRuntimeCandidate{{
			Kind:        "oauth",
			Target:      "user@example.com",
			Available:   true,
			Status:      "ready",
			HealthScore: 90,
		}},
	}
	providerRuntimeRegistry.mu.Unlock()
	cfg := &config.Config{
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				name: {APIBase: "https://example.com/v1", Auth: "oauth", OAuth: config.ProviderOAuthConfig{Provider: "codex"}},
			},
		},
	}

	summary := GetProviderRuntimeSummary(cfg, ProviderRuntimeQuery{HealthBelow: 30, Window: time.Hour})
	if summary.TotalProviders != 1 || summary.Degraded != 1 || summary.Critical != 0 || summary.Healthy != 0 {
		t.Fatalf("unexpected summary counts: %#v", summary)
	}
	if len(summary.Providers) != 1 || summary.Providers[0].Status != "degraded" {
		t.Fatalf("expected degraded provider item, got %#v", summary.Providers)
	}
	if summary.Providers[0].LastErrorReason != "rate_limit" {
		t.Fatalf("expected last error reason, got %#v", summary.Providers[0])
	}
	if summary.Providers[0].StaleForSec != -1 {
		t.Fatalf("expected stale_for_sec=-1 without success event, got %#v", summary.Providers[0])
	}
}

func TestGetProviderRuntimeSummaryIncludesOAuthAccountMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	credFile := filepath.Join(dir, "qwen.json")
	raw, err := json.Marshal(oauthSession{
		Provider:     "qwen",
		AccessToken:  "qwen-token",
		RefreshToken: "refresh-token",
		Email:        "qwen-label",
		ProjectID:    "proj-9",
		DeviceID:     "device-9",
		ResourceURL:  "https://chat.qwen.ai/api",
		Expire:       time.Now().Add(time.Hour).Format(time.RFC3339),
		FilePath:     credFile,
	})
	if err != nil {
		t.Fatalf("marshal session failed: %v", err)
	}
	if err := os.WriteFile(credFile, raw, 0o600); err != nil {
		t.Fatalf("write session failed: %v", err)
	}
	cfg := &config.Config{
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				"qwen-summary": {
					APIBase:    "https://example.com/v1",
					Auth:       "oauth",
					TimeoutSec: 5,
					OAuth: config.ProviderOAuthConfig{
						Provider:       "qwen",
						CredentialFile: credFile,
					},
				},
			},
		},
	}
	summary := GetProviderRuntimeSummary(cfg, ProviderRuntimeQuery{Provider: "qwen-summary", HealthBelow: 50})
	if len(summary.Providers) != 1 {
		t.Fatalf("expected one provider, got %#v", summary)
	}
	if len(summary.Providers[0].OAuthAccounts) != 1 {
		t.Fatalf("expected oauth account metadata, got %#v", summary.Providers[0])
	}
	account := summary.Providers[0].OAuthAccounts[0]
	if account.AccountLabel != "qwen-label" || account.ProjectID != "proj-9" || account.DeviceID != "device-9" || account.ResourceURL != "https://chat.qwen.ai/api" {
		t.Fatalf("unexpected oauth account metadata: %#v", account)
	}
}

func TestRefreshProviderRuntimeNowSupportsOnlyExpiring(t *testing.T) {
	t.Parallel()

	var refreshCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(&refreshCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"refreshed-token","refresh_token":"refresh-token","expires_in":3600}`))
	}))
	defer server.Close()

	credFile := filepath.Join(t.TempDir(), "codex.json")
	raw, err := json.Marshal(oauthSession{
		Provider:     "codex",
		AccessToken:  "old-token",
		RefreshToken: "refresh-token",
		Expire:       time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal session failed: %v", err)
	}
	if err := os.WriteFile(credFile, raw, 0o600); err != nil {
		t.Fatalf("write credential file failed: %v", err)
	}

	name := "runtime-refresh-provider"
	cfg := &config.Config{
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				name: {
					APIBase:    server.URL + "/v1",
					Auth:       "oauth",
					TimeoutSec: 5,
					OAuth: config.ProviderOAuthConfig{
						Provider:       "codex",
						CredentialFile: credFile,
						ClientID:       "test-client",
						TokenURL:       server.URL + "/oauth/token",
						AuthURL:        server.URL + "/oauth/authorize",
						RefreshLeadSec: 1800,
					},
				},
			},
		},
	}

	result, err := RefreshProviderRuntimeNow(cfg, name, true)
	if err != nil {
		t.Fatalf("refresh only expiring failed: %v", err)
	}
	if result == nil || result.Refreshed != 0 || result.Skipped != 1 {
		t.Fatalf("expected skip for non-expiring session, got %#v", result)
	}
	if atomic.LoadInt32(&refreshCalls) != 0 {
		t.Fatalf("expected no refresh calls for only-expiring path, got %d", refreshCalls)
	}

	result, err = RefreshProviderRuntimeNow(cfg, name, false)
	if err != nil {
		t.Fatalf("refresh all failed: %v", err)
	}
	if result == nil || result.Refreshed != 1 {
		t.Fatalf("expected forced refresh, got %#v", result)
	}
	if atomic.LoadInt32(&refreshCalls) != 1 {
		t.Fatalf("expected one refresh call, got %d", refreshCalls)
	}
}

func TestRerankProviderRuntimeUpdatesCandidateOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	credFile := filepath.Join(dir, "oauth.json")
	raw, err := json.Marshal(oauthSession{
		Provider:    "codex",
		AccessToken: "oauth-token",
		Email:       "rerank@example.com",
		Expire:      time.Now().Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal session failed: %v", err)
	}
	if err := os.WriteFile(credFile, raw, 0o600); err != nil {
		t.Fatalf("write session failed: %v", err)
	}
	name := "rerank-runtime-provider"
	cfg := &config.Config{
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				name: {
					APIKey:     "api-key",
					APIBase:    "https://example.com/v1",
					Auth:       "hybrid",
					TimeoutSec: 5,
					OAuth: config.ProviderOAuthConfig{
						Provider:       "codex",
						CredentialFile: credFile,
					},
				},
			},
		},
	}
	order, err := RerankProviderRuntime(cfg, name)
	if err != nil {
		t.Fatalf("rerank provider runtime failed: %v", err)
	}
	if len(order) == 0 || order[0].Kind != "api_key" {
		t.Fatalf("expected api-key-first rerank result, got %#v", order)
	}
	snapshot := GetProviderRuntimeSnapshot(cfg)
	items, _ := snapshot["items"].([]map[string]interface{})
	if len(items) != 1 {
		t.Fatalf("expected one runtime item, got %#v", snapshot)
	}
	snapshotOrder, _ := items[0]["candidate_order"].([]providerRuntimeCandidate)
	if len(snapshotOrder) == 0 || snapshotOrder[0].Kind != "api_key" {
		t.Fatalf("expected api-key-first candidate order, got %#v", items[0]["candidate_order"])
	}
}
