package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cfgpkg "clawgo/pkg/config"
	"clawgo/pkg/nodes"
	"github.com/gorilla/websocket"
)

func TestHandleWebUIConfigRequiresConfirmForProviderAPIBaseChange(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")

	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	cfg.Providers.Proxy.APIBase = "https://old.example/v1"
	cfg.Providers.Proxy.APIKey = "test-key"
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	bodyCfg := cfgpkg.DefaultConfig()
	bodyCfg.Logging.Enabled = false
	bodyCfg.Providers.Proxy.APIBase = "https://new.example/v1"
	bodyCfg.Providers.Proxy.APIKey = "test-key"
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
	if !strings.Contains(rec.Body.String(), `providers.proxy.api_base`) {
		t.Fatalf("expected providers.proxy.api_base in changed_fields, got: %s", rec.Body.String())
	}
}

func TestHandleWebUIConfigRequiresConfirmForCustomProviderSecretChange(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")

	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	cfg.Providers.Proxies["backup"] = cfgpkg.ProviderConfig{
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
	bodyCfg.Providers.Proxies["backup"] = cfgpkg.ProviderConfig{
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
	if !strings.Contains(rec.Body.String(), `providers.proxies.backup.api_key`) {
		t.Fatalf("expected providers.proxies.backup.api_key in changed_fields, got: %s", rec.Body.String())
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
