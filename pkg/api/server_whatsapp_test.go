package api

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	cfgpkg "github.com/YspCoder/clawgo/pkg/config"
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

	req := httptest.NewRequest(http.MethodGet, "/api/whatsapp/status", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/api/whatsapp/qr.svg", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/api/whatsapp/status", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/api/whatsapp/status", nil)
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
