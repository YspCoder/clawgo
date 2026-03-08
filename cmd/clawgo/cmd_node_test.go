package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawgo/pkg/config"
	"clawgo/pkg/nodes"
)

func TestParseNodeRegisterArgsDefaults(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.Gateway.Host = "gateway.example"
	cfg.Gateway.Port = 7788
	cfg.Gateway.Token = "cfg-token"

	opts, err := parseNodeRegisterArgs([]string{"--id", "edge-dev"}, cfg)
	if err != nil {
		t.Fatalf("parseNodeRegisterArgs failed: %v", err)
	}
	if opts.GatewayBase != "http://gateway.example:7788" {
		t.Fatalf("unexpected gateway base: %s", opts.GatewayBase)
	}
	if opts.Token != "cfg-token" {
		t.Fatalf("unexpected token: %s", opts.Token)
	}
	if opts.ID != "edge-dev" {
		t.Fatalf("unexpected id: %s", opts.ID)
	}
	if !opts.Capabilities.Run || !opts.Capabilities.Invoke || !opts.Capabilities.Model {
		t.Fatalf("expected default run/invoke/model capabilities, got %+v", opts.Capabilities)
	}
}

func TestPostNodeRegisterSendsNodeInfo(t *testing.T) {
	t.Parallel()

	var gotAuth string
	var got nodes.NodeInfo
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/nodes/register" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	info := nodes.NodeInfo{
		ID:       "edge-dev",
		Name:     "Edge Dev",
		Endpoint: "http://edge.example:18790",
		Capabilities: nodes.Capabilities{
			Run: true, Invoke: true, Model: true,
		},
		Actions: []string{"run", "agent_task"},
		Models:  []string{"gpt-4o-mini"},
	}
	client := &http.Client{Timeout: 2 * time.Second}
	if err := postNodeRegister(context.Background(), client, srv.URL, "secret", info); err != nil {
		t.Fatalf("postNodeRegister failed: %v", err)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("unexpected auth header: %s", gotAuth)
	}
	if got.ID != "edge-dev" || got.Endpoint != "http://edge.example:18790" {
		t.Fatalf("unexpected node payload: %+v", got)
	}
	if len(got.Actions) != 2 || got.Actions[1] != "agent_task" {
		t.Fatalf("unexpected actions: %+v", got.Actions)
	}
}

func TestPostNodeHeartbeatSendsNodeID(t *testing.T) {
	t.Parallel()

	var body map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/nodes/heartbeat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	if err := postNodeHeartbeat(context.Background(), client, srv.URL, "", "edge-dev"); err != nil {
		t.Fatalf("postNodeHeartbeat failed: %v", err)
	}
	if strings.TrimSpace(body["id"]) != "edge-dev" {
		t.Fatalf("unexpected heartbeat body: %+v", body)
	}
}

func TestNodeWebsocketURL(t *testing.T) {
	t.Parallel()

	if got := nodeWebsocketURL("http://gateway.example:18790"); got != "ws://gateway.example:18790/nodes/connect" {
		t.Fatalf("unexpected ws url: %s", got)
	}
	if got := nodeWebsocketURL("https://gateway.example"); got != "wss://gateway.example/nodes/connect" {
		t.Fatalf("unexpected wss url: %s", got)
	}
}

func TestNodeSocketPingInterval(t *testing.T) {
	t.Parallel()

	if got := nodeSocketPingInterval(120); got != 25*time.Second {
		t.Fatalf("expected 25s cap, got %s", got)
	}
	if got := nodeSocketPingInterval(20); got != 10*time.Second {
		t.Fatalf("expected 10s floor, got %s", got)
	}
	if got := nodeSocketPingInterval(30); got != 15*time.Second {
		t.Fatalf("expected half heartbeat, got %s", got)
	}
}
