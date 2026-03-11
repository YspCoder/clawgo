package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	cfgpkg "github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/nodes"
	"github.com/gorilla/websocket"
)

func TestHandleWebUIWhatsAppStatus(t *testing.T) {
	t.Parallel()

	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"state":        "connected",
				"connected":    true,
				"logged_in":    true,
				"bridge_addr":  "127.0.0.1:3001",
				"user_jid":     "8613012345678@s.whatsapp.net",
				"qr_available": false,
				"last_event":   "connected",
				"updated_at":   "2026-03-09T12:00:00+08:00",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer bridge.Close()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	cfg.Channels.WhatsApp.Enabled = true
	cfg.Channels.WhatsApp.BridgeURL = "ws" + strings.TrimPrefix(bridge.URL, "http") + "/ws"
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nil)
	srv.SetConfigPath(cfgPath)

	req := httptest.NewRequest(http.MethodGet, "/webui/api/whatsapp/status", nil)
	rec := httptest.NewRecorder()
	srv.handleWebUIWhatsAppStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"bridge_running":true`) {
		t.Fatalf("expected bridge_running=true, got: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"user_jid":"8613012345678@s.whatsapp.net"`) {
		t.Fatalf("expected user_jid in payload, got: %s", rec.Body.String())
	}
}

func TestHandleWebUIWhatsAppQR(t *testing.T) {
	t.Parallel()

	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"state":        "qr_ready",
				"connected":    false,
				"logged_in":    false,
				"bridge_addr":  "127.0.0.1:3001",
				"qr_available": true,
				"qr_code":      "test-qr-code",
				"last_event":   "qr_ready",
				"updated_at":   "2026-03-09T12:00:00+08:00",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer bridge.Close()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	cfg.Channels.WhatsApp.Enabled = true
	cfg.Channels.WhatsApp.BridgeURL = "ws" + strings.TrimPrefix(bridge.URL, "http") + "/ws"
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nil)
	srv.SetConfigPath(cfgPath)

	req := httptest.NewRequest(http.MethodGet, "/webui/api/whatsapp/qr.svg", nil)
	rec := httptest.NewRecorder()
	srv.handleWebUIWhatsAppQR(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "image/svg+xml") {
		t.Fatalf("expected svg content-type, got %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "<svg") {
		t.Fatalf("expected svg payload, got: %s", rec.Body.String())
	}
}

func TestHandleWebUIWhatsAppStatusWithNestedBridgePath(t *testing.T) {
	t.Parallel()

	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/whatsapp/status":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"state":        "connected",
				"connected":    true,
				"logged_in":    true,
				"bridge_addr":  "127.0.0.1:7788",
				"user_jid":     "8613012345678@s.whatsapp.net",
				"qr_available": false,
				"last_event":   "connected",
				"updated_at":   "2026-03-09T12:00:00+08:00",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer bridge.Close()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	cfg.Channels.WhatsApp.Enabled = true
	cfg.Channels.WhatsApp.BridgeURL = "ws" + strings.TrimPrefix(bridge.URL, "http") + "/whatsapp/ws"
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nil)
	srv.SetConfigPath(cfgPath)

	req := httptest.NewRequest(http.MethodGet, "/webui/api/whatsapp/status", nil)
	rec := httptest.NewRecorder()
	srv.handleWebUIWhatsAppStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"bridge_running":true`) {
		t.Fatalf("expected bridge_running=true, got: %s", rec.Body.String())
	}
}

func TestHandleWebUIWhatsAppStatusMapsLegacyBridgeURLToEmbeddedPath(t *testing.T) {
	t.Parallel()

	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/whatsapp/status":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"state":        "connected",
				"connected":    true,
				"logged_in":    true,
				"bridge_addr":  "127.0.0.1:7788",
				"user_jid":     "8613012345678@s.whatsapp.net",
				"qr_available": false,
				"last_event":   "connected",
				"updated_at":   "2026-03-09T12:00:00+08:00",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer bridge.Close()

	u, err := url.Parse(bridge.URL)
	if err != nil {
		t.Fatalf("parse bridge url: %v", err)
	}
	host, portRaw, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil {
		t.Fatalf("atoi port: %v", err)
	}

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	cfg.Gateway.Host = host
	cfg.Gateway.Port = port
	cfg.Channels.WhatsApp.Enabled = true
	cfg.Channels.WhatsApp.BridgeURL = "ws://localhost:3001"
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nil)
	srv.SetConfigPath(cfgPath)

	req := httptest.NewRequest(http.MethodGet, "/webui/api/whatsapp/status", nil)
	rec := httptest.NewRecorder()
	srv.handleWebUIWhatsAppStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"bridge_running":true`) {
		t.Fatalf("expected bridge_running=true, got: %s", rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	bridgeURL, _ := payload["bridge_url"].(string)
	if !strings.HasSuffix(bridgeURL, "/whatsapp/ws") {
		t.Fatalf("expected embedded whatsapp bridge url, got: %s", rec.Body.String())
	}
}

func TestHandleWebUIConfigRequiresConfirmForProviderAPIBaseChange(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")

	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	pc := cfg.Models.Providers["openai"]
	pc.APIBase = "https://old.example/v1"
	pc.APIKey = "test-key"
	cfg.Models.Providers["openai"] = pc
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	bodyCfg := cfgpkg.DefaultConfig()
	bodyCfg.Logging.Enabled = false
	bodyPC := bodyCfg.Models.Providers["openai"]
	bodyPC.APIBase = "https://new.example/v1"
	bodyPC.APIKey = "test-key"
	bodyCfg.Models.Providers["openai"] = bodyPC
	body, err := json.Marshal(bodyCfg)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nil)
	srv.SetConfigPath(cfgPath)

	req := httptest.NewRequest(http.MethodPost, "/webui/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleWebUIConfig(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"requires_confirm":true`) {
		t.Fatalf("expected requires_confirm response, got: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `models.providers.openai.api_base`) {
		t.Fatalf("expected models.providers.openai.api_base in changed_fields, got: %s", rec.Body.String())
	}
}

func TestHandleWebUIConfigRequiresConfirmForCustomProviderSecretChange(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")

	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	cfg.Models.Providers["backup"] = cfgpkg.ProviderConfig{
		APIBase:    "https://backup.example/v1",
		APIKey:     "old-secret",
		Models:     []string{"backup-model"},
		Auth:       "bearer",
		TimeoutSec: 30,
	}
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	bodyCfg := cfgpkg.DefaultConfig()
	bodyCfg.Logging.Enabled = false
	bodyCfg.Models.Providers["backup"] = cfgpkg.ProviderConfig{
		APIBase:    "https://backup.example/v1",
		APIKey:     "new-secret",
		Models:     []string{"backup-model"},
		Auth:       "bearer",
		TimeoutSec: 30,
	}
	body, err := json.Marshal(bodyCfg)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nil)
	srv.SetConfigPath(cfgPath)

	req := httptest.NewRequest(http.MethodPost, "/webui/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleWebUIConfig(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"requires_confirm":true`) {
		t.Fatalf("expected requires_confirm response, got: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `models.providers.backup.api_key`) {
		t.Fatalf("expected models.providers.backup.api_key in changed_fields, got: %s", rec.Body.String())
	}
}

func TestHandleWebUIConfigRunsReloadHookSynchronously(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nil)
	srv.SetConfigPath(cfgPath)
	called := false
	srv.SetConfigAfterHook(func() error {
		called = true
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/webui/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleWebUIConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatalf("expected reload hook to run")
	}
}

func TestHandleWebUIConfigReturnsReloadHookError(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nil)
	srv.SetConfigPath(cfgPath)
	srv.SetConfigAfterHook(func() error {
		return fmt.Errorf("reload boom")
	})

	req := httptest.NewRequest(http.MethodPost, "/webui/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleWebUIConfig(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "reload failed") {
		t.Fatalf("expected reload failure in body, got: %s", rec.Body.String())
	}
}

func TestHandleNodeConnectRegistersAndHeartbeatsNode(t *testing.T) {
	t.Parallel()

	mgr := nodes.NewManager()
	srv := NewServer("127.0.0.1", 0, "", mgr)
	mux := http.NewServeMux()
	mux.HandleFunc("/nodes/connect", srv.handleNodeConnect)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/nodes/connect"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	info := nodes.NodeInfo{
		ID:       "edge-dev",
		Name:     "Edge Dev",
		Endpoint: "http://edge.example:18790",
		Capabilities: nodes.Capabilities{
			Run: true, Invoke: true, Model: true,
		},
	}
	if err := conn.WriteJSON(nodes.WireMessage{Type: "register", Node: &info}); err != nil {
		t.Fatalf("write register: %v", err)
	}
	var regAck nodes.WireAck
	if err := conn.ReadJSON(&regAck); err != nil {
		t.Fatalf("read register ack: %v", err)
	}
	if !regAck.OK || regAck.Type != "registered" || regAck.ID != "edge-dev" {
		t.Fatalf("unexpected register ack: %+v", regAck)
	}

	stored, ok := mgr.Get("edge-dev")
	if !ok || !stored.Online {
		t.Fatalf("expected node to be online after register, got %+v ok=%v", stored, ok)
	}

	if err := conn.WriteJSON(nodes.WireMessage{Type: "heartbeat", ID: "edge-dev"}); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}
	var hbAck nodes.WireAck
	if err := conn.ReadJSON(&hbAck); err != nil {
		t.Fatalf("read heartbeat ack: %v", err)
	}
	if !hbAck.OK || hbAck.Type != "heartbeat" || hbAck.ID != "edge-dev" {
		t.Fatalf("unexpected heartbeat ack: %+v", hbAck)
	}
}

func TestHandleNodeConnectReconnectKeepsNewestSessionOnline(t *testing.T) {
	t.Parallel()

	mgr := nodes.NewManager()
	srv := NewServer("127.0.0.1", 0, "", mgr)
	mux := http.NewServeMux()
	mux.HandleFunc("/nodes/connect", srv.handleNodeConnect)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/nodes/connect"
	connect := func() *websocket.Conn {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("dial websocket: %v", err)
		}
		if err := conn.WriteJSON(nodes.WireMessage{Type: "register", Node: &nodes.NodeInfo{ID: "edge-dev", Name: "Edge Dev"}}); err != nil {
			t.Fatalf("write register: %v", err)
		}
		var ack nodes.WireAck
		if err := conn.ReadJSON(&ack); err != nil {
			t.Fatalf("read register ack: %v", err)
		}
		if !ack.OK {
			t.Fatalf("unexpected register ack: %+v", ack)
		}
		return conn
	}

	first := connect()
	second := connect()

	if err := first.Close(); err != nil {
		t.Fatalf("close first connection: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	got, ok := mgr.Get("edge-dev")
	if !ok || !got.Online {
		t.Fatalf("expected newest session to keep node online, got %+v ok=%v", got, ok)
	}

	_ = second.Close()
}

func TestHandleNodeConnectRelaysSignalMessages(t *testing.T) {
	t.Parallel()

	mgr := nodes.NewManager()
	srv := NewServer("127.0.0.1", 0, "", mgr)
	mux := http.NewServeMux()
	mux.HandleFunc("/nodes/connect", srv.handleNodeConnect)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/nodes/connect"
	connect := func(id string) *websocket.Conn {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("dial websocket: %v", err)
		}
		if err := conn.WriteJSON(nodes.WireMessage{Type: "register", Node: &nodes.NodeInfo{ID: id, Name: id}}); err != nil {
			t.Fatalf("write register: %v", err)
		}
		var ack nodes.WireAck
		if err := conn.ReadJSON(&ack); err != nil {
			t.Fatalf("read register ack: %v", err)
		}
		if !ack.OK {
			t.Fatalf("unexpected register ack: %+v", ack)
		}
		return conn
	}

	offerer := connect("edge-a")
	defer offerer.Close()
	answerer := connect("edge-b")
	defer answerer.Close()

	signal := nodes.WireMessage{
		Type:    "signal_offer",
		ID:      "sig-1",
		To:      "edge-b",
		Session: "sess-1",
		Payload: map[string]interface{}{"sdp": "offer-sdp"},
	}
	if err := offerer.WriteJSON(signal); err != nil {
		t.Fatalf("write signal offer: %v", err)
	}

	var relayAck nodes.WireAck
	if err := offerer.ReadJSON(&relayAck); err != nil {
		t.Fatalf("read relay ack: %v", err)
	}
	if !relayAck.OK || relayAck.Type != "relayed" || relayAck.ID != "sig-1" {
		t.Fatalf("unexpected relay ack: %+v", relayAck)
	}

	var forwarded nodes.WireMessage
	if err := answerer.ReadJSON(&forwarded); err != nil {
		t.Fatalf("read forwarded signal: %v", err)
	}
	if forwarded.Type != "signal_offer" || forwarded.From != "edge-a" || forwarded.To != "edge-b" || forwarded.Session != "sess-1" {
		t.Fatalf("unexpected forwarded signal envelope: %+v", forwarded)
	}
	if fmt.Sprintf("%v", forwarded.Payload["sdp"]) != "offer-sdp" {
		t.Fatalf("unexpected forwarded payload: %+v", forwarded.Payload)
	}
}

func TestHandleWebUISessionsHidesInternalSessionsByDefault(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "agents", "main", "sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}
	for _, name := range []string{
		"review-api.jsonl",
		"internal:heartbeat.jsonl",
		"heartbeat:default.jsonl",
		"cron:nightly.jsonl",
		"subagent:worker.jsonl",
	} {
		if err := os.WriteFile(filepath.Join(sessionsDir, name), []byte("{}\n"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	srv := NewServer("127.0.0.1", 0, "", nil)
	srv.SetWorkspacePath(filepath.Join(tmp, "workspace"))

	req := httptest.NewRequest(http.MethodGet, "/webui/api/sessions", nil)
	rec := httptest.NewRecorder()
	srv.handleWebUISessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		OK       bool `json:"ok"`
		Sessions []struct {
			Key string `json:"key"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	keys := make([]string, 0, len(payload.Sessions))
	for _, item := range payload.Sessions {
		keys = append(keys, item.Key)
	}
	if len(keys) != 1 || keys[0] != "review-api" {
		t.Fatalf("unexpected sessions: %v", keys)
	}
}

func TestHandleWebUISubagentsRuntimeLive(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	srv.SetSubagentHandler(func(ctx context.Context, action string, args map[string]interface{}) (interface{}, error) {
		switch action {
		case "thread":
			return map[string]interface{}{
				"thread": map[string]interface{}{"thread_id": "thread-1"},
				"messages": []map[string]interface{}{
					{"message_id": "msg-1", "content": "hello"},
				},
			}, nil
		case "inbox":
			return map[string]interface{}{
				"messages": []map[string]interface{}{
					{"message_id": "msg-2", "content": "reply"},
				},
			}, nil
		case "stream":
			return map[string]interface{}{
				"task": map[string]interface{}{"id": "subagent-1"},
				"items": []map[string]interface{}{
					{"kind": "event", "message": "progress"},
				},
			}, nil
		default:
			return map[string]interface{}{}, nil
		}
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/webui/api/subagents_runtime/live", srv.handleWebUISubagentsRuntimeLive)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/webui/api/subagents_runtime/live?task_id=subagent-1&preview_task_id=subagent-1"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	var msg map[string]interface{}
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read live snapshot: %v", err)
	}
	payload, _ := msg["payload"].(map[string]interface{})
	thread, _ := payload["thread"].(map[string]interface{})
	inbox, _ := payload["inbox"].(map[string]interface{})
	preview, _ := payload["preview"].(map[string]interface{})
	if thread == nil || inbox == nil || preview == nil {
		t.Fatalf("expected thread/inbox/preview payload, got: %+v", msg)
	}
}

func TestHandleWebUIChatLive(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	srv.SetChatHandler(func(ctx context.Context, sessionKey, content string) (string, error) {
		if sessionKey != "main" {
			t.Fatalf("unexpected session key: %s", sessionKey)
		}
		if content != "hello" {
			t.Fatalf("unexpected content: %s", content)
		}
		return "world", nil
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/webui/api/chat/live", srv.handleWebUIChatLive)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/webui/api/chat/live"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]interface{}{"session": "main", "message": "hello"}); err != nil {
		t.Fatalf("write chat request: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var chunk map[string]interface{}
	if err := conn.ReadJSON(&chunk); err != nil {
		t.Fatalf("read chat chunk: %v", err)
	}
	if chunk["type"] != "chat_chunk" || chunk["delta"] != "world" {
		t.Fatalf("unexpected chat chunk: %+v", chunk)
	}

	var done map[string]interface{}
	if err := conn.ReadJSON(&done); err != nil {
		t.Fatalf("read chat done: %v", err)
	}
	if done["type"] != "chat_done" {
		t.Fatalf("unexpected chat done: %+v", done)
	}
}

func TestHandleWebUILogsLive(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "app.log")
	if err := os.WriteFile(logPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write log file: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	srv.SetLogFilePath(logPath)

	mux := http.NewServeMux()
	mux.HandleFunc("/webui/api/logs/live", srv.handleWebUILogsLive)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/webui/api/logs/live"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	go func() {
		time.Sleep(200 * time.Millisecond)
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		defer f.Close()
		_, _ = f.WriteString(`{"level":"INFO","msg":"tail-ok"}` + "\n")
	}()

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var msg map[string]interface{}
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read log entry: %v", err)
	}
	entry, _ := msg["entry"].(map[string]interface{})
	if entry == nil {
		t.Fatalf("expected entry payload, got: %+v", msg)
	}
	if entry["msg"] != "tail-ok" {
		t.Fatalf("expected tail-ok entry, got: %+v", entry)
	}
}

func TestHandleWebUINodesIncludesP2PSummary(t *testing.T) {
	t.Parallel()

	mgr := nodes.NewManager()
	mgr.Upsert(nodes.NodeInfo{ID: "edge-b", Name: "Edge B"})
	mgr.MarkOffline("edge-b")
	srv := NewServer("127.0.0.1", 0, "", mgr)
	workspace := t.TempDir()
	srv.SetWorkspacePath(workspace)
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "memory", "nodes-dispatch-audit.jsonl"), []byte("{\"node\":\"edge-b\",\"used_transport\":\"webrtc\",\"fallback_from\":\"\",\"duration_ms\":12,\"artifacts\":[{\"name\":\"snap.png\",\"kind\":\"image\",\"mime_type\":\"image/png\",\"storage\":\"inline\",\"content_base64\":\"iVBORw0KGgo=\"}]}\n"), 0644); err != nil {
		t.Fatalf("write audit: %v", err)
	}
	srv.SetNodeP2PStatusHandler(func() map[string]interface{} {
		return map[string]interface{}{
			"enabled":         true,
			"transport":       "webrtc",
			"active_sessions": 2,
			"nodes": []map[string]interface{}{
				{"node": "edge-b", "status": "connecting", "retry_count": 3, "last_error": "signal timeout"},
			},
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/webui/api/nodes", nil)
	rec := httptest.NewRecorder()
	srv.handleWebUINodes(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	p2p, _ := body["p2p"].(map[string]interface{})
	if p2p == nil || p2p["transport"] != "webrtc" {
		t.Fatalf("expected p2p summary, got %+v", body)
	}
	alerts, _ := body["alerts"].([]interface{})
	if len(alerts) == 0 {
		t.Fatalf("expected node alerts, got %+v", body)
	}
	dispatches, _ := body["dispatches"].([]interface{})
	if len(dispatches) != 1 {
		t.Fatalf("expected dispatch audit rows, got %+v", body["dispatches"])
	}
	first, _ := dispatches[0].(map[string]interface{})
	artifacts, _ := first["artifacts"].([]interface{})
	if len(artifacts) != 1 {
		t.Fatalf("expected artifact previews in dispatch row, got %+v", first)
	}
}

func TestHandleWebUINodesEnrichesLocalNodeMetadata(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	srv.SetSubagentHandler(func(ctx context.Context, action string, args map[string]interface{}) (interface{}, error) {
		if action != "registry" {
			return map[string]interface{}{"items": []map[string]interface{}{}}, nil
		}
		return map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"agent_id":     "coder",
					"display_name": "Code Agent",
					"role":         "coding",
					"type":         "worker",
					"transport":    "local",
				},
			},
		}, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/webui/api/nodes", nil)
	rec := httptest.NewRecorder()
	srv.handleWebUINodes(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	items, _ := body["nodes"].([]interface{})
	if len(items) == 0 {
		t.Fatalf("expected local node in payload")
	}
	local, _ := items[0].(map[string]interface{})
	if strings.TrimSpace(fmt.Sprint(local["id"])) != "local" {
		t.Fatalf("expected first node to be local, got %+v", local)
	}
	if strings.TrimSpace(fmt.Sprint(local["os"])) == "" || strings.TrimSpace(fmt.Sprint(local["arch"])) == "" {
		t.Fatalf("expected local os/arch, got %+v", local)
	}
	actions, _ := local["actions"].([]interface{})
	if len(actions) == 0 {
		t.Fatalf("expected local actions, got %+v", local)
	}
	agents, _ := local["agents"].([]interface{})
	if len(agents) != 1 {
		t.Fatalf("expected local agents from registry, got %+v", local)
	}
}

func TestHandleWebUINodeDispatchReplay(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	srv.SetNodeDispatchHandler(func(ctx context.Context, req nodes.Request, mode string) (nodes.Response, error) {
		if req.Node != "edge-a" || req.Action != "screen_snapshot" || mode != "auto" {
			t.Fatalf("unexpected replay request: %+v mode=%s", req, mode)
		}
		if fmt.Sprint(req.Args["quality"]) != "high" {
			t.Fatalf("unexpected args: %+v", req.Args)
		}
		return nodes.Response{
			OK:     true,
			Node:   req.Node,
			Action: req.Action,
			Payload: map[string]interface{}{
				"used_transport": "webrtc",
			},
		}, nil
	})

	body := `{"node":"edge-a","action":"screen_snapshot","mode":"auto","args":{"quality":"high"}}`
	req := httptest.NewRequest(http.MethodPost, "/webui/api/node_dispatches/replay", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleWebUINodeDispatchReplay(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"used_transport":"webrtc"`) {
		t.Fatalf("expected replay result body, got: %s", rec.Body.String())
	}
}

func TestHandleWebUINodeArtifactsListAndDelete(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	workspace := t.TempDir()
	srv.SetWorkspacePath(workspace)
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	artifactPath := filepath.Join(workspace, "artifact.txt")
	if err := os.WriteFile(artifactPath, []byte("artifact-body"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	auditLine := fmt.Sprintf("{\"time\":\"2026-03-09T00:00:00Z\",\"node\":\"edge-a\",\"action\":\"run\",\"artifacts\":[{\"name\":\"artifact.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"source_path\":\"%s\",\"size_bytes\":13}]}\n", artifactPath)
	if err := os.WriteFile(filepath.Join(workspace, "memory", "nodes-dispatch-audit.jsonl"), []byte(auditLine), 0o644); err != nil {
		t.Fatalf("write audit: %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/webui/api/node_artifacts", nil)
	listRec := httptest.NewRecorder()
	srv.handleWebUINodeArtifacts(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listRec.Code)
	}
	var listBody map[string]interface{}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	items, _ := listBody["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("expected 1 artifact, got %+v", listBody)
	}
	item, _ := items[0].(map[string]interface{})
	artifactID := strings.TrimSpace(fmt.Sprint(item["id"]))
	if artifactID == "" {
		t.Fatalf("expected artifact id, got %+v", item)
	}

	deleteReq := httptest.NewRequest(http.MethodPost, "/webui/api/node_artifacts/delete", strings.NewReader(fmt.Sprintf(`{"id":"%s"}`, artifactID)))
	deleteReq.Header.Set("Content-Type", "application/json")
	deleteRec := httptest.NewRecorder()
	srv.handleWebUINodeArtifactDelete(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", deleteRec.Code, deleteRec.Body.String())
	}
	if _, err := os.Stat(artifactPath); !os.IsNotExist(err) {
		t.Fatalf("expected artifact file removed, stat err=%v", err)
	}
}

func TestHandleWebUINodeArtifactsExport(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	workspace := t.TempDir()
	srv.SetWorkspacePath(workspace)
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	auditLine := "{\"time\":\"2026-03-09T00:00:00Z\",\"node\":\"edge-a\",\"action\":\"screen_snapshot\",\"ok\":true,\"artifacts\":[{\"name\":\"shot.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"content_base64\":\"Y2FwdHVyZQ==\",\"size_bytes\":7}]}\n"
	if err := os.WriteFile(filepath.Join(workspace, "memory", "nodes-dispatch-audit.jsonl"), []byte(auditLine), 0o644); err != nil {
		t.Fatalf("write audit: %v", err)
	}
	srv.mgr.Upsert(nodes.NodeInfo{ID: "edge-a", Name: "Edge A", Online: true})

	req := httptest.NewRequest(http.MethodGet, "/webui/api/node_artifacts/export?node=edge-a&action=screen_snapshot&kind=text", nil)
	rec := httptest.NewRecorder()
	srv.handleWebUINodeArtifactsExport(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/zip") {
		t.Fatalf("expected zip response, got %q", got)
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	seen := map[string]bool{}
	for _, file := range zr.File {
		seen[file.Name] = true
	}
	for _, required := range []string{"manifest.json", "dispatches.json", "alerts.json", "artifacts.json"} {
		if !seen[required] {
			t.Fatalf("missing zip entry %q in %+v", required, seen)
		}
	}
	foundFile := false
	for _, file := range zr.File {
		if !strings.HasPrefix(file.Name, "files/") {
			continue
		}
		foundFile = true
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open artifact file: %v", err)
		}
		body, _ := io.ReadAll(rc)
		_ = rc.Close()
		if string(body) != "capture" {
			t.Fatalf("unexpected artifact payload %q", string(body))
		}
	}
	if !foundFile {
		t.Fatalf("expected exported artifact file in zip")
	}
}

func TestHandleWebUINodeArtifactsPrune(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	workspace := t.TempDir()
	srv.SetWorkspacePath(workspace)
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	auditLines := strings.Join([]string{
		"{\"time\":\"2026-03-09T00:00:00Z\",\"node\":\"edge-a\",\"action\":\"screen_snapshot\",\"ok\":true,\"artifacts\":[{\"name\":\"one.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"content_base64\":\"b25l\"}]}",
		"{\"time\":\"2026-03-09T00:01:00Z\",\"node\":\"edge-a\",\"action\":\"screen_snapshot\",\"ok\":true,\"artifacts\":[{\"name\":\"two.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"content_base64\":\"dHdv\"}]}",
		"{\"time\":\"2026-03-09T00:02:00Z\",\"node\":\"edge-a\",\"action\":\"screen_snapshot\",\"ok\":true,\"artifacts\":[{\"name\":\"three.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"content_base64\":\"dGhyZWU=\"}]}",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(workspace, "memory", "nodes-dispatch-audit.jsonl"), []byte(auditLines), 0o644); err != nil {
		t.Fatalf("write audit: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webui/api/node_artifacts/prune", strings.NewReader(`{"node":"edge-a","action":"screen_snapshot","kind":"text","keep_latest":1}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleWebUINodeArtifactPrune(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	items := srv.webUINodeArtifactsPayloadFiltered("edge-a", "screen_snapshot", "text", 10)
	if len(items) != 1 {
		t.Fatalf("expected 1 remaining artifact, got %d", len(items))
	}
	if got := fmt.Sprint(items[0]["name"]); got != "three.txt" {
		t.Fatalf("expected newest artifact to remain, got %q", got)
	}
}

func TestHandleWebUINodeArtifactsAppliesRetentionConfig(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	workspace := t.TempDir()
	srv.SetWorkspacePath(workspace)
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	cfg := cfgpkg.DefaultConfig()
	cfg.Gateway.Nodes.Artifacts.Enabled = true
	cfg.Gateway.Nodes.Artifacts.KeepLatest = 1
	cfg.Gateway.Nodes.Artifacts.PruneOnRead = true
	cfgPath := filepath.Join(workspace, "config.json")
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	srv.SetConfigPath(cfgPath)
	auditLines := strings.Join([]string{
		"{\"time\":\"2026-03-09T00:00:00Z\",\"node\":\"edge-a\",\"action\":\"screen_snapshot\",\"ok\":true,\"artifacts\":[{\"name\":\"one.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"content_base64\":\"b25l\"}]}",
		"{\"time\":\"2026-03-09T00:01:00Z\",\"node\":\"edge-a\",\"action\":\"screen_snapshot\",\"ok\":true,\"artifacts\":[{\"name\":\"two.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"content_base64\":\"dHdv\"}]}",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(workspace, "memory", "nodes-dispatch-audit.jsonl"), []byte(auditLines), 0o644); err != nil {
		t.Fatalf("write audit: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/webui/api/node_artifacts", nil)
	rec := httptest.NewRecorder()
	srv.handleWebUINodeArtifacts(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	items := srv.webUINodeArtifactsPayload(10)
	if len(items) != 1 {
		t.Fatalf("expected retention to keep 1 artifact, got %d", len(items))
	}
	if got := fmt.Sprint(items[0]["name"]); got != "two.txt" {
		t.Fatalf("expected newest artifact to remain, got %q", got)
	}
	stats := srv.artifactStatsSnapshot()
	if fmt.Sprint(stats["pruned"]) == "" || fmt.Sprint(stats["pruned"]) == "0" {
		t.Fatalf("expected retention stats to record pruned artifacts, got %+v", stats)
	}
	if fmt.Sprint(stats["keep_latest"]) != "1" {
		t.Fatalf("expected keep_latest in stats, got %+v", stats)
	}
}

func TestHandleWebUINodeArtifactsAppliesRetentionDays(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	workspace := t.TempDir()
	srv.SetWorkspacePath(workspace)
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	cfg := cfgpkg.DefaultConfig()
	cfg.Gateway.Nodes.Artifacts.Enabled = true
	cfg.Gateway.Nodes.Artifacts.KeepLatest = 10
	cfg.Gateway.Nodes.Artifacts.RetainDays = 1
	cfg.Gateway.Nodes.Artifacts.PruneOnRead = true
	cfgPath := filepath.Join(workspace, "config.json")
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	srv.SetConfigPath(cfgPath)
	oldTime := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	newTime := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	auditLines := strings.Join([]string{
		fmt.Sprintf("{\"time\":%q,\"node\":\"edge-a\",\"action\":\"screen_snapshot\",\"ok\":true,\"artifacts\":[{\"name\":\"old.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"content_base64\":\"b2xk\"}]}", oldTime),
		fmt.Sprintf("{\"time\":%q,\"node\":\"edge-a\",\"action\":\"screen_snapshot\",\"ok\":true,\"artifacts\":[{\"name\":\"fresh.txt\",\"kind\":\"text\",\"mime_type\":\"text/plain\",\"content_base64\":\"ZnJlc2g=\"}]}", newTime),
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(workspace, "memory", "nodes-dispatch-audit.jsonl"), []byte(auditLines), 0o644); err != nil {
		t.Fatalf("write audit: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/webui/api/node_artifacts", nil)
	rec := httptest.NewRecorder()
	srv.handleWebUINodeArtifacts(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	items := srv.webUINodeArtifactsPayload(10)
	if len(items) != 1 {
		t.Fatalf("expected retention days to keep 1 artifact, got %d", len(items))
	}
	if got := fmt.Sprint(items[0]["name"]); got != "fresh.txt" {
		t.Fatalf("expected fresh artifact to remain, got %q", got)
	}
}
