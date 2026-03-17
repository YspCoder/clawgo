package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/wsrelay"
	"github.com/gorilla/websocket"
)

func TestCreateProviderByNameRoutesAIStudioProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Models.Providers["aistudio"] = config.ProviderConfig{
		Auth:       "none",
		TimeoutSec: 90,
		Models:     []string{"gemini-2.5-pro"},
	}
	provider, err := CreateProviderByName(cfg, "aistudio")
	if err != nil {
		t.Fatalf("CreateProviderByName() error = %v", err)
	}
	if _, ok := provider.(*AistudioProvider); !ok {
		t.Fatalf("expected *AistudioProvider, got %T", provider)
	}
}

func TestAistudioProviderChatUsesRelay(t *testing.T) {
	manager, serverURL, cleanup := startRelayTestServer(t)
	defer cleanup()
	SetAIStudioRelayManager(manager)

	reqCh := make(chan string, 1)
	stopClient := connectRelayClient(t, serverURL, "aistudio", func(msg wsrelay.Message) []wsrelay.Message {
		reqCh <- fmt.Sprintf("%v", msg.Payload["url"])
		return []wsrelay.Message{{
			ID:   msg.ID,
			Type: wsrelay.MessageTypeHTTPResp,
			Payload: map[string]any{
				"status": 200,
				"headers": map[string]any{
					"Content-Type": []string{"application/json"},
				},
				"body": `{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`,
			},
		}}
	})
	defer stopClient()

	provider := NewAistudioProvider("aistudio", "", "", "gemini-2.5-pro", false, "none", 30*time.Second, nil)
	resp, err := provider.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "gemini-2.5-pro", nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("expected content ok, got %q", resp.Content)
	}
	select {
	case raw := <-reqCh:
		if !strings.Contains(raw, ":generateContent") {
			t.Fatalf("expected generateContent endpoint, got %q", raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for relay request")
	}
}

func TestAistudioProviderCountTokensUsesRelay(t *testing.T) {
	manager, serverURL, cleanup := startRelayTestServer(t)
	defer cleanup()
	SetAIStudioRelayManager(manager)

	stopClient := connectRelayClient(t, serverURL, "aistudio-count", func(msg wsrelay.Message) []wsrelay.Message {
		return []wsrelay.Message{{
			ID:   msg.ID,
			Type: wsrelay.MessageTypeHTTPResp,
			Payload: map[string]any{
				"status": 200,
				"headers": map[string]any{
					"Content-Type": []string{"application/json"},
				},
				"body": `{"totalTokens":42}`,
			},
		}}
	})
	defer stopClient()

	provider := NewAistudioProvider("aistudio", "", "", "gemini-2.5-pro", false, "none", 30*time.Second, nil)
	usage, err := provider.CountTokens(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "gemini-2.5-pro", map[string]interface{}{"aistudio_channel": "aistudio-count"})
	if err != nil {
		t.Fatalf("CountTokens() error = %v", err)
	}
	if usage.TotalTokens != 42 {
		t.Fatalf("expected total tokens 42, got %d", usage.TotalTokens)
	}
}

func TestAIStudioRelayRuntimeTracksConnectedChannels(t *testing.T) {
	providerRuntimeRegistry.mu.Lock()
	delete(providerRuntimeRegistry.api, "aistudio")
	providerRuntimeRegistry.mu.Unlock()

	NotifyAIStudioRelayConnected("aistudio")
	NotifyAIStudioRelayConnected("aistudio-alt")

	providerRuntimeRegistry.mu.Lock()
	state := providerRuntimeRegistry.api["aistudio"]
	providerRuntimeRegistry.mu.Unlock()

	if len(state.CandidateOrder) != 2 {
		t.Fatalf("expected 2 relay candidates, got %d", len(state.CandidateOrder))
	}
	if state.CandidateOrder[0].Kind != "relay" {
		t.Fatalf("expected relay candidate kind, got %q", state.CandidateOrder[0].Kind)
	}

	NotifyAIStudioRelayDisconnected("aistudio-alt", nil)

	providerRuntimeRegistry.mu.Lock()
	state = providerRuntimeRegistry.api["aistudio"]
	providerRuntimeRegistry.mu.Unlock()

	if len(state.CandidateOrder) != 1 || state.CandidateOrder[0].Target != "aistudio" {
		t.Fatalf("unexpected candidate order after disconnect: %+v", state.CandidateOrder)
	}

	NotifyAIStudioRelayDisconnected("aistudio", nil)
}

func TestGetProviderRuntimeSnapshotIncludesAIStudioRelayAccounts(t *testing.T) {
	providerRuntimeRegistry.mu.Lock()
	delete(providerRuntimeRegistry.api, "aistudio")
	providerRuntimeRegistry.mu.Unlock()

	NotifyAIStudioRelayConnected("aistudio")
	defer NotifyAIStudioRelayDisconnected("aistudio", nil)

	cfg := config.DefaultConfig()
	cfg.Models.Providers["aistudio"] = config.ProviderConfig{
		TimeoutSec: 30,
		Models:     []string{"gemini-2.5-pro"},
	}

	snapshot := GetProviderRuntimeSnapshot(cfg)
	items, _ := snapshot["items"].([]map[string]interface{})
	if len(items) == 0 {
		t.Fatal("expected snapshot items")
	}

	var found map[string]interface{}
	for _, item := range items {
		if strings.TrimSpace(fmt.Sprintf("%v", item["name"])) == "aistudio" {
			found = item
			break
		}
	}
	if found == nil {
		t.Fatal("expected aistudio snapshot item")
	}

	accounts, _ := found["oauth_accounts"].([]OAuthAccountInfo)
	if len(accounts) != 1 || accounts[0].AccountLabel != "aistudio" {
		t.Fatalf("unexpected relay accounts: %+v", accounts)
	}
}

func TestAIStudioRelayAccountsUseLastSuccessAsRefresh(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	aistudioRelayRegistry.mu.Lock()
	aistudioRelayRegistry.connected = map[string]time.Time{"aistudio": now.Add(-10 * time.Minute)}
	aistudioRelayRegistry.succeeded = map[string]time.Time{"aistudio": now}
	aistudioRelayRegistry.mu.Unlock()

	accounts := listAIStudioRelayAccounts()
	if len(accounts) != 1 {
		t.Fatalf("expected one account, got %d", len(accounts))
	}
	if accounts[0].LastRefresh != now.Format(time.RFC3339) {
		t.Fatalf("expected last refresh %s, got %s", now.Format(time.RFC3339), accounts[0].LastRefresh)
	}
}

func TestAIStudioRelayRuntimeRecordsFailureAndRecovery(t *testing.T) {
	providerRuntimeRegistry.mu.Lock()
	delete(providerRuntimeRegistry.api, "aistudio")
	providerRuntimeRegistry.mu.Unlock()

	NotifyAIStudioRelayConnected("aistudio")
	defer NotifyAIStudioRelayDisconnected("aistudio", nil)

	recordAIStudioRelayFailure("aistudio", fmt.Errorf("boom"))

	providerRuntimeRegistry.mu.Lock()
	state := providerRuntimeRegistry.api["aistudio"]
	providerRuntimeRegistry.mu.Unlock()

	if state.API.FailureCount == 0 {
		t.Fatal("expected api failure count to increase")
	}
	if len(state.RecentErrors) == 0 {
		t.Fatal("expected recent errors to be recorded")
	}
	if len(state.CandidateOrder) == 0 || state.CandidateOrder[0].Status != "cooldown" {
		t.Fatalf("expected relay candidate cooldown, got %+v", state.CandidateOrder)
	}

	recordAIStudioRelaySuccess("aistudio")

	providerRuntimeRegistry.mu.Lock()
	state = providerRuntimeRegistry.api["aistudio"]
	providerRuntimeRegistry.mu.Unlock()

	if state.LastSuccess == nil {
		t.Fatal("expected last success event")
	}
	if len(state.CandidateOrder) == 0 || state.CandidateOrder[0].Status != "ready" {
		t.Fatalf("expected relay candidate recovery, got %+v", state.CandidateOrder)
	}
}

func TestAIStudioChannelIDPrefersHealthyAvailableRelay(t *testing.T) {
	providerRuntimeRegistry.mu.Lock()
	providerRuntimeRegistry.api["aistudio"] = providerRuntimeState{
		CandidateOrder: []providerRuntimeCandidate{
			{Kind: "relay", Target: "aistudio-bad", Available: false, Status: "cooldown", CooldownUntil: time.Now().Add(5 * time.Minute).Format(time.RFC3339), HealthScore: 90},
			{Kind: "relay", Target: "aistudio-good", Available: true, Status: "ready", HealthScore: 100},
		},
	}
	providerRuntimeRegistry.mu.Unlock()

	got := aistudioChannelCandidates("aistudio", nil)[0]
	if got != "aistudio-good" {
		t.Fatalf("expected aistudio-good, got %q", got)
	}
}

func TestAIStudioChannelIDExplicitOptionWins(t *testing.T) {
	providerRuntimeRegistry.mu.Lock()
	providerRuntimeRegistry.api["aistudio"] = providerRuntimeState{
		CandidateOrder: []providerRuntimeCandidate{
			{Kind: "relay", Target: "aistudio-good", Available: true, Status: "ready", HealthScore: 100},
		},
	}
	providerRuntimeRegistry.mu.Unlock()

	got := aistudioChannelCandidates("aistudio", map[string]interface{}{"aistudio_channel": "manual"})[0]
	if got != "manual" {
		t.Fatalf("expected explicit channel manual, got %q", got)
	}
}

func TestAIStudioChannelIDPrefersMostRecentSuccessfulRelay(t *testing.T) {
	providerRuntimeRegistry.mu.Lock()
	providerRuntimeRegistry.api["aistudio"] = providerRuntimeState{
		CandidateOrder: []providerRuntimeCandidate{
			{Kind: "relay", Target: "aistudio-a", Available: true, Status: "ready", HealthScore: 100},
			{Kind: "relay", Target: "aistudio-b", Available: true, Status: "ready", HealthScore: 100},
		},
	}
	providerRuntimeRegistry.mu.Unlock()

	aistudioRelayRegistry.mu.Lock()
	if aistudioRelayRegistry.succeeded == nil {
		aistudioRelayRegistry.succeeded = map[string]time.Time{}
	}
	aistudioRelayRegistry.succeeded["aistudio-a"] = time.Now().Add(-1 * time.Minute)
	aistudioRelayRegistry.succeeded["aistudio-b"] = time.Now()
	aistudioRelayRegistry.mu.Unlock()

	got := aistudioChannelCandidates("aistudio", nil)[0]
	if got != "aistudio-b" {
		t.Fatalf("expected most recent successful relay aistudio-b, got %q", got)
	}
}

func TestAistudioProviderChatFailsOverToNextRelay(t *testing.T) {
	manager, serverURL, cleanup := startRelayTestServer(t)
	defer cleanup()
	SetAIStudioRelayManager(manager)
	aistudioRelayRegistry.mu.Lock()
	aistudioRelayRegistry.succeeded = map[string]time.Time{}
	aistudioRelayRegistry.mu.Unlock()

	providerRuntimeRegistry.mu.Lock()
	providerRuntimeRegistry.api["aistudio"] = providerRuntimeState{
		CandidateOrder: []providerRuntimeCandidate{
			{Kind: "relay", Target: "aistudio-first", Available: true, Status: "ready", HealthScore: 100},
			{Kind: "relay", Target: "aistudio-second", Available: true, Status: "ready", HealthScore: 90},
		},
	}
	providerRuntimeRegistry.mu.Unlock()

	connectRelayClient(t, serverURL, "aistudio-first", func(msg wsrelay.Message) []wsrelay.Message {
		return []wsrelay.Message{{
			ID:   msg.ID,
			Type: wsrelay.MessageTypeHTTPResp,
			Payload: map[string]any{
				"status": 503,
				"headers": map[string]any{
					"Content-Type": []string{"application/json"},
				},
				"body": `{"error":"no capacity"}`,
			},
		}}
	})
	stopSecond := connectRelayClient(t, serverURL, "aistudio-second", func(msg wsrelay.Message) []wsrelay.Message {
		return []wsrelay.Message{{
			ID:   msg.ID,
			Type: wsrelay.MessageTypeHTTPResp,
			Payload: map[string]any{
				"status": 200,
				"headers": map[string]any{
					"Content-Type": []string{"application/json"},
				},
				"body": `{"candidates":[{"content":{"parts":[{"text":"ok-2"}]},"finishReason":"STOP"}]}`,
			},
		}}
	})
	defer stopSecond()

	provider := NewAistudioProvider("aistudio", "", "", "gemini-2.5-pro", false, "none", 30*time.Second, nil)
	resp, err := provider.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "gemini-2.5-pro", nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "ok-2" {
		t.Fatalf("expected failover response ok-2, got %q", resp.Content)
	}
}

func TestAistudioProviderChatExplicitChannelDoesNotFailOver(t *testing.T) {
	manager, serverURL, cleanup := startRelayTestServer(t)
	defer cleanup()
	SetAIStudioRelayManager(manager)
	aistudioRelayRegistry.mu.Lock()
	aistudioRelayRegistry.succeeded = map[string]time.Time{}
	aistudioRelayRegistry.mu.Unlock()

	stopFirst := connectRelayClient(t, serverURL, "manual", func(msg wsrelay.Message) []wsrelay.Message {
		return []wsrelay.Message{{
			ID:   msg.ID,
			Type: wsrelay.MessageTypeHTTPResp,
			Payload: map[string]any{
				"status": 503,
				"headers": map[string]any{
					"Content-Type": []string{"application/json"},
				},
				"body": `{"error":"no capacity"}`,
			},
		}}
	})
	defer stopFirst()

	stopSecond := connectRelayClient(t, serverURL, "aistudio-second", func(msg wsrelay.Message) []wsrelay.Message {
		return []wsrelay.Message{{
			ID:   msg.ID,
			Type: wsrelay.MessageTypeHTTPResp,
			Payload: map[string]any{
				"status": 200,
				"headers": map[string]any{
					"Content-Type": []string{"application/json"},
				},
				"body": `{"candidates":[{"content":{"parts":[{"text":"ok-2"}]},"finishReason":"STOP"}]}`,
			},
		}}
	})
	defer stopSecond()

	provider := NewAistudioProvider("aistudio", "", "", "gemini-2.5-pro", false, "none", 30*time.Second, nil)
	_, err := provider.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "gemini-2.5-pro", map[string]interface{}{"aistudio_channel": "manual"})
	if err == nil {
		t.Fatal("expected explicit relay failure without failover")
	}
}

func TestAistudioProviderStreamFailsOverBeforeFirstChunk(t *testing.T) {
	manager, serverURL, cleanup := startRelayTestServer(t)
	defer cleanup()
	SetAIStudioRelayManager(manager)
	aistudioRelayRegistry.mu.Lock()
	aistudioRelayRegistry.succeeded = map[string]time.Time{}
	aistudioRelayRegistry.mu.Unlock()

	providerRuntimeRegistry.mu.Lock()
	providerRuntimeRegistry.api["aistudio"] = providerRuntimeState{
		CandidateOrder: []providerRuntimeCandidate{
			{Kind: "relay", Target: "aistudio-first", Available: true, Status: "ready", HealthScore: 100},
			{Kind: "relay", Target: "aistudio-second", Available: true, Status: "ready", HealthScore: 90},
		},
	}
	providerRuntimeRegistry.mu.Unlock()

	stopFirst := connectRelayClient(t, serverURL, "aistudio-first", func(msg wsrelay.Message) []wsrelay.Message {
		return []wsrelay.Message{
			{
				ID:   msg.ID,
				Type: wsrelay.MessageTypeStreamStart,
				Payload: map[string]any{
					"status": 503,
					"headers": map[string]any{
						"Content-Type": []string{"text/event-stream"},
					},
				},
			},
			{
				ID:   msg.ID,
				Type: wsrelay.MessageTypeStreamEnd,
			},
		}
	})
	defer stopFirst()
	stopSecond := connectRelayClient(t, serverURL, "aistudio-second", func(msg wsrelay.Message) []wsrelay.Message {
		return []wsrelay.Message{
			{
				ID:   msg.ID,
				Type: wsrelay.MessageTypeStreamStart,
				Payload: map[string]any{
					"status": 200,
					"headers": map[string]any{
						"Content-Type": []string{"text/event-stream"},
					},
				},
			},
			{
				ID:   msg.ID,
				Type: wsrelay.MessageTypeStreamChunk,
				Payload: map[string]any{
					"data": `{"candidates":[{"content":{"parts":[{"text":"ok-stream"}]}}]}`,
				},
			},
			{
				ID:   msg.ID,
				Type: wsrelay.MessageTypeStreamEnd,
			},
		}
	})
	defer stopSecond()

	provider := NewAistudioProvider("aistudio", "", "", "gemini-2.5-pro", false, "none", 30*time.Second, nil)
	var deltas []string
	resp, err := provider.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "gemini-2.5-pro", nil, func(delta string) {
		deltas = append(deltas, delta)
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if resp.Content != "ok-stream" {
		t.Fatalf("expected failover stream content ok-stream, got %q", resp.Content)
	}
	if len(deltas) == 0 {
		t.Fatal("expected stream deltas after failover")
	}
}

func TestAistudioProviderStreamDoesNotFailOverAfterChunk(t *testing.T) {
	manager, serverURL, cleanup := startRelayTestServer(t)
	defer cleanup()
	SetAIStudioRelayManager(manager)
	aistudioRelayRegistry.mu.Lock()
	aistudioRelayRegistry.succeeded = map[string]time.Time{}
	aistudioRelayRegistry.mu.Unlock()

	providerRuntimeRegistry.mu.Lock()
	providerRuntimeRegistry.api["aistudio"] = providerRuntimeState{
		CandidateOrder: []providerRuntimeCandidate{
			{Kind: "relay", Target: "aistudio-first", Available: true, Status: "ready", HealthScore: 100},
			{Kind: "relay", Target: "aistudio-second", Available: true, Status: "ready", HealthScore: 90},
		},
	}
	providerRuntimeRegistry.mu.Unlock()

	stopFirst := connectRelayClient(t, serverURL, "aistudio-first", func(msg wsrelay.Message) []wsrelay.Message {
		return []wsrelay.Message{
			{
				ID:   msg.ID,
				Type: wsrelay.MessageTypeStreamStart,
				Payload: map[string]any{
					"status": 200,
					"headers": map[string]any{
						"Content-Type": []string{"text/event-stream"},
					},
				},
			},
			{
				ID:   msg.ID,
				Type: wsrelay.MessageTypeStreamChunk,
				Payload: map[string]any{
					"data": `{"candidates":[{"content":{"parts":[{"text":"partial"}]}}]}`,
				},
			},
			{
				ID:      msg.ID,
				Type:    wsrelay.MessageTypeError,
				Payload: map[string]any{"error": "stream broke", "status": 502.0},
			},
		}
	})
	defer stopFirst()
	stopSecond := connectRelayClient(t, serverURL, "aistudio-second", func(msg wsrelay.Message) []wsrelay.Message {
		return []wsrelay.Message{
			{
				ID:   msg.ID,
				Type: wsrelay.MessageTypeStreamStart,
				Payload: map[string]any{
					"status": 200,
					"headers": map[string]any{
						"Content-Type": []string{"text/event-stream"},
					},
				},
			},
			{
				ID:   msg.ID,
				Type: wsrelay.MessageTypeStreamChunk,
				Payload: map[string]any{
					"data": `{"candidates":[{"content":{"parts":[{"text":"should-not-use"}]}}]}`,
				},
			},
			{
				ID:   msg.ID,
				Type: wsrelay.MessageTypeStreamEnd,
			},
		}
	})
	defer stopSecond()

	provider := NewAistudioProvider("aistudio", "", "", "gemini-2.5-pro", false, "none", 30*time.Second, nil)
	_, err := provider.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "gemini-2.5-pro", nil, nil)
	if err == nil {
		t.Fatal("expected stream error without mid-stream failover")
	}
	if strings.Contains(err.Error(), "should-not-use") {
		t.Fatalf("unexpected failover after stream started: %v", err)
	}
}

func startRelayTestServer(t *testing.T) (*wsrelay.Manager, string, func()) {
	t.Helper()
	manager := wsrelay.NewManager(wsrelay.Options{
		Path: "/v1/ws",
		ProviderFactory: func(r *http.Request) (string, error) {
			return strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider"))), nil
		},
	})
	srv := httptest.NewServer(manager.Handler())
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + manager.Path()
	cleanup := func() {
		_ = manager.Stop(context.Background())
		srv.Close()
		SetAIStudioRelayManager(nil)
	}
	return manager, wsURL, cleanup
}

func connectRelayClient(t *testing.T, wsURL, provider string, handle func(wsrelay.Message) []wsrelay.Message) func() {
	t.Helper()
	u, err := url.Parse(wsURL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	q := u.Query()
	q.Set("provider", provider)
	u.RawQuery = q.Encode()
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var msg wsrelay.Message
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			for _, out := range handle(msg) {
				if err := conn.WriteJSON(out); err != nil {
					return
				}
			}
		}
	}()
	return func() {
		_ = conn.Close()
		<-done
	}
}
