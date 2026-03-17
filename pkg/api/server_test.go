package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	cfgpkg "github.com/YspCoder/clawgo/pkg/config"
	"github.com/gorilla/websocket"
)

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

	srv := NewServer("127.0.0.1", 0, "")
	srv.SetConfigPath(cfgPath)

	req := httptest.NewRequest(http.MethodGet, "/api/whatsapp/status", nil)
	rec := httptest.NewRecorder()
	srv.handleWebUIWhatsAppStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
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

func TestHandleWebUIConfigPostIsDisabled(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "")
	srv.SetConfigPath(cfgPath)

	req := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(`{"gateway":{"host":"127.0.0.1"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleWebUIConfig(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "webui config editing is disabled") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestHandleWebUISessionsHidesInternalSessionsByDefault(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "agents", "main", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}
	for _, name := range []string{
		"review-api.jsonl",
		"internal:heartbeat.jsonl",
		"heartbeat:default.jsonl",
		"cron:nightly.jsonl",
		"subagent:worker.jsonl",
	} {
		if err := os.WriteFile(filepath.Join(sessionsDir, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	srv := NewServer("127.0.0.1", 0, "")
	srv.SetWorkspacePath(filepath.Join(tmp, "workspace"))

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
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
	if len(payload.Sessions) != 1 || payload.Sessions[0].Key != "review-api" {
		t.Fatalf("unexpected sessions: %+v", payload.Sessions)
	}
}

func TestHandleWebUIChatLive(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "")
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
	mux.HandleFunc("/api/chat/live", srv.handleWebUIChatLive)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/api/chat/live"
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
	if runtime.GOOS == "windows" {
		t.Skip("websocket log tail test is flaky on Windows due file-handle release timing")
	}

	f, err := os.CreateTemp("", "clawgo-logs-live-*.log")
	if err != nil {
		t.Fatalf("create temp log file: %v", err)
	}
	logPath := f.Name()
	if err := f.Close(); err != nil {
		t.Fatalf("close temp log file: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(logPath)
	})
	if err := os.WriteFile(logPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write log file: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "")
	srv.SetLogFilePath(logPath)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/logs/live", srv.handleWebUILogsLive)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/api/logs/live"
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
	if entry == nil || entry["msg"] != "tail-ok" {
		t.Fatalf("unexpected entry payload: %+v", msg)
	}
}
