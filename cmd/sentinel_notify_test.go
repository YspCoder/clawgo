package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
)

func TestBuildSentinelAlertHandlerPublishesChannelAlertAndWebhook(t *testing.T) {
	t.Parallel()

	msgBus := bus.NewMessageBus()

	webhookCh := make(chan sentinelWebhookPayload, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("unexpected content type: %s", got)
		}
		var payload sentinelWebhookPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		webhookCh <- payload
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.Sentinel.NotifyChannel = "telegram"
	cfg.Sentinel.NotifyChatID = "chat-1"
	cfg.Sentinel.WebhookURL = srv.URL

	handler := buildSentinelAlertHandler(cfg, msgBus)
	handler("disk usage high")

	outCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msg, ok := msgBus.SubscribeOutbound(outCtx)
	if !ok {
		t.Fatal("expected outbound channel alert")
	}
	if msg.Channel != "telegram" || msg.ChatID != "chat-1" {
		t.Fatalf("unexpected outbound route: %+v", msg)
	}
	if msg.Content != "[Sentinel] disk usage high" {
		t.Fatalf("unexpected outbound content: %s", msg.Content)
	}

	select {
	case payload := <-webhookCh:
		if payload.Source != "sentinel" {
			t.Fatalf("unexpected source: %s", payload.Source)
		}
		if payload.Level != "warning" {
			t.Fatalf("unexpected level: %s", payload.Level)
		}
		if payload.Message != "disk usage high" {
			t.Fatalf("unexpected message: %s", payload.Message)
		}
		if _, err := time.Parse(time.RFC3339, payload.Timestamp); err != nil {
			t.Fatalf("unexpected timestamp: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected webhook alert")
	}
}

func TestBuildSentinelAlertHandlerSkipsEmptyTargets(t *testing.T) {
	t.Parallel()

	msgBus := bus.NewMessageBus()
	cfg := config.DefaultConfig()
	handler := buildSentinelAlertHandler(cfg, msgBus)
	handler("noop")
}
