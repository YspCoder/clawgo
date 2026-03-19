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

func TestHandleWebUIConfigPostSavesRawConfig(t *testing.T) {
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
	hookCalled := 0
	srv.SetConfigAfterHook(func(forceRuntimeReload bool) error {
		if forceRuntimeReload {
			t.Fatalf("expected raw config save to use non-forced reload")
		}
		hookCalled++
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(`{"gateway":{"host":"127.0.0.1","port":7788,"token":"abc"},"logging":{"enabled":false,"persist":false,"level":"debug","file":"logs/app.log","format":"text"},"models":{"providers":{"openai":{"api_base":"https://api.openai.com/v1","auth":"bearer","api_key":"secret","models":["gpt-5"],"timeout_sec":120}}},"tools":{"shell":{"enabled":true},"mcp":{"enabled":false}},"agents":{"defaults":{"model":{"primary":"openai/gpt-5"},"max_tool_iterations":10,"execution":{"run_state_ttl_seconds":3600,"run_state_max":128,"tool_parallel_safe_names":[],"tool_max_parallel_calls":4}},"router":{"enabled":false,"policy":{"intent_max_input_chars":2000,"max_rounds_without_user":3}},"subagents":{}},"channels":{"telegram":{"enabled":true,"token":"bot-token"}},"cron":{"enabled":false},"sentinel":{"enabled":false}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleWebUIConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if hookCalled != 1 {
		t.Fatalf("expected hook to be called once, got %d", hookCalled)
	}
	updated, err := cfgpkg.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if updated.Gateway.Host != "127.0.0.1" {
		t.Fatalf("expected updated gateway host, got %q", updated.Gateway.Host)
	}
	if !updated.Channels.Telegram.Enabled {
		t.Fatalf("expected telegram channel to remain editable")
	}
}

func TestHandleWebUIConfigPostSavesNormalizedConfig(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	cfg.Gateway.Host = "0.0.0.0"
	cfg.Gateway.Port = 7788
	cfg.Models.Providers["openai"] = cfgpkg.ProviderConfig{
		APIBase:    "https://api.openai.com/v1",
		Auth:       "bearer",
		APIKey:     "secret",
		Models:     []string{"gpt-5"},
		TimeoutSec: 120,
	}
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "")
	srv.SetConfigPath(cfgPath)
	srv.SetConfigAfterHook(func(forceRuntimeReload bool) error {
		if forceRuntimeReload {
			t.Fatalf("expected normalized config save to use non-forced reload")
		}
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/config?mode=normalized", strings.NewReader(`{"core":{"gateway":{"host":"127.0.0.1","port":18790},"tools":{"shell_enabled":false,"mcp_enabled":true}},"runtime":{"router":{"enabled":true,"strategy":"rules_first","max_hops":2,"default_timeout_sec":90},"providers":{"openai":{"api_base":"https://api.openai.com/v1","auth":"bearer","timeout_sec":150}}}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleWebUIConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	updated, err := cfgpkg.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if updated.Gateway.Host != "127.0.0.1" || updated.Gateway.Port != 18790 {
		t.Fatalf("expected normalized gateway update, got %s:%d", updated.Gateway.Host, updated.Gateway.Port)
	}
	if updated.Tools.Shell.Enabled {
		t.Fatalf("expected shell tool to be disabled by normalized save")
	}
}

func TestWithCORSEchoesPreflightHeaders(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "")
	handler := srv.withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/config", nil)
	req.Header.Set("Origin", "https://dash.clawgo.dev")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type,x-clawgo-client")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://dash.clawgo.dev" {
		t.Fatalf("unexpected allow origin: %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("unexpected allow credentials: %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "POST" {
		t.Fatalf("unexpected allow methods: %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "authorization,content-type,x-clawgo-client" {
		t.Fatalf("unexpected allow headers: %q", got)
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

func TestSaveProviderConfigForcesRuntimeReload(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := cfgpkg.DefaultConfig()
	cfg.Logging.Enabled = false
	cfg.Models.Providers["openai"] = cfgpkg.ProviderConfig{
		APIBase:    "https://api.openai.com/v1",
		Auth:       "oauth",
		Models:     []string{"gpt-5"},
		TimeoutSec: 120,
		OAuth: cfgpkg.ProviderOAuthConfig{
			Provider:       "codex",
			CredentialFile: filepath.Join(tmp, "auth.json"),
		},
	}
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "")
	srv.SetConfigPath(cfgPath)

	forced := false
	srv.SetConfigAfterHook(func(forceRuntimeReload bool) error {
		forced = forceRuntimeReload
		return nil
	})

	pc := cfg.Models.Providers["openai"]
	if err := srv.saveProviderConfig(cfg, "openai", pc); err != nil {
		t.Fatalf("save provider config: %v", err)
	}
	if !forced {
		t.Fatalf("expected provider config save to force runtime reload")
	}
}

func TestHandleWebUIMemoryListsAndReadsWorkspaceMemoryFile(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "MEMORY.md"), []byte("# long-term\n"), 0o644); err != nil {
		t.Fatalf("write workspace memory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "memory", "2026-03-19.md"), []byte("daily\n"), 0o644); err != nil {
		t.Fatalf("write daily memory: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "")
	srv.SetWorkspacePath(tmp)

	listReq := httptest.NewRequest(http.MethodGet, "/api/memory", nil)
	listRec := httptest.NewRecorder()
	srv.handleWebUIMemory(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listRec.Code, listRec.Body.String())
	}
	var listPayload struct {
		OK    bool     `json:"ok"`
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list payload: %v", err)
	}
	if len(listPayload.Files) < 2 || listPayload.Files[0] != "MEMORY.md" {
		t.Fatalf("expected MEMORY.md in memory file list, got %+v", listPayload.Files)
	}

	readReq := httptest.NewRequest(http.MethodGet, "/api/memory?path=MEMORY.md", nil)
	readRec := httptest.NewRecorder()
	srv.handleWebUIMemory(readRec, readReq)
	if readRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", readRec.Code, readRec.Body.String())
	}
	var readPayload struct {
		OK      bool   `json:"ok"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(readRec.Body.Bytes(), &readPayload); err != nil {
		t.Fatalf("decode read payload: %v", err)
	}
	if readPayload.Path != "MEMORY.md" || readPayload.Content != "# long-term\n" {
		t.Fatalf("unexpected memory payload: %+v", readPayload)
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
