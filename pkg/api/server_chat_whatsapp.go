package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/YspCoder/clawgo/pkg/channels"
	cfgpkg "github.com/YspCoder/clawgo/pkg/config"
	"rsc.io/qr"
)

func (s *Server) handleWebUIChat(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.onChat == nil {
		http.Error(w, "chat handler not configured", http.StatusInternalServerError)
		return
	}
	var body struct {
		Session string `json:"session"`
		Message string `json:"message"`
		Media   string `json:"media"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	session := body.Session
	if session == "" {
		session = r.URL.Query().Get("session")
	}
	if session == "" {
		session = "main"
	}
	prompt := body.Message
	if body.Media != "" {
		if prompt != "" {
			prompt += "\n"
		}
		prompt += "[file: " + body.Media + "]"
	}
	resp, err := s.onChat(r.Context(), session, prompt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "reply": resp, "session": session})
}

func (s *Server) handleWebUIChatHistory(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	session := r.URL.Query().Get("session")
	if session == "" {
		session = "main"
	}
	if s.onChatHistory == nil {
		writeJSON(w, map[string]interface{}{"ok": true, "session": session, "messages": []interface{}{}})
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "session": session, "messages": s.onChatHistory(session)})
}

func (s *Server) handleWebUIChatLive(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.onChat == nil {
		http.Error(w, "chat handler not configured", http.StatusInternalServerError)
		return
	}
	conn, err := nodesWebsocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var body struct {
		Session string `json:"session"`
		Message string `json:"message"`
		Media   string `json:"media"`
	}
	if err := conn.ReadJSON(&body); err != nil {
		_ = conn.WriteJSON(map[string]interface{}{"ok": false, "type": "chat_error", "error": "invalid json"})
		return
	}
	session := body.Session
	if session == "" {
		session = r.URL.Query().Get("session")
	}
	if session == "" {
		session = "main"
	}
	prompt := body.Message
	if body.Media != "" {
		if prompt != "" {
			prompt += "\n"
		}
		prompt += "[file: " + body.Media + "]"
	}
	resp, err := s.onChat(r.Context(), session, prompt)
	if err != nil {
		_ = conn.WriteJSON(map[string]interface{}{"ok": false, "type": "chat_error", "error": err.Error(), "session": session})
		return
	}
	chunk := 180
	for i := 0; i < len(resp); i += chunk {
		end := i + chunk
		if end > len(resp) {
			end = len(resp)
		}
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteJSON(map[string]interface{}{
			"ok":      true,
			"type":    "chat_chunk",
			"session": session,
			"delta":   resp[i:end],
		}); err != nil {
			return
		}
	}
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_ = conn.WriteJSON(map[string]interface{}{
		"ok":      true,
		"type":    "chat_done",
		"session": session,
	})
}

func (s *Server) handleWebUIVersion(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":                true,
		"gateway_version":   firstNonEmptyString(s.gatewayVersion, gatewayBuildVersion()),
		"webui_version":     firstNonEmptyString(s.webuiVersion, detectWebUIVersion(strings.TrimSpace(s.webUIDir))),
		"compiled_channels": channels.CompiledChannelKeys(),
	})
}

func (s *Server) handleWebUIWhatsAppStatus(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	payload, code := s.webUIWhatsAppStatusPayload(r.Context())
	writeJSONStatus(w, code, payload)
}

func (s *Server) handleWebUIWhatsAppLogout(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg, err := s.loadConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	logoutURL, err := channels.BridgeLogoutURL(s.resolveWhatsAppBridgeURL(cfg))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, logoutURL, nil)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) handleWebUIWhatsAppQR(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	payload, code := s.webUIWhatsAppStatusPayload(r.Context())
	status, _ := payload["status"].(map[string]interface{})
	qrCode := ""
	if status != nil {
		qrCode = stringFromMap(status, "qr_code")
	}
	if code != http.StatusOK || strings.TrimSpace(qrCode) == "" {
		http.Error(w, "qr unavailable", http.StatusNotFound)
		return
	}
	qrImage, err := qr.Encode(strings.TrimSpace(qrCode), qr.M)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	_, _ = io.WriteString(w, renderQRCodeSVG(qrImage, 8, 24))
}

func (s *Server) webUIWhatsAppStatusPayload(ctx context.Context) (map[string]interface{}, int) {
	cfg, err := s.loadConfig()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}, http.StatusInternalServerError
	}
	waCfg := cfg.Channels.WhatsApp
	bridgeURL := s.resolveWhatsAppBridgeURL(cfg)
	statusURL, err := channels.BridgeStatusURL(bridgeURL)
	if err != nil {
		return map[string]interface{}{
			"ok":         false,
			"enabled":    waCfg.Enabled,
			"bridge_url": bridgeURL,
			"error":      err.Error(),
		}, http.StatusBadRequest
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		return map[string]interface{}{
			"ok":             false,
			"enabled":        waCfg.Enabled,
			"bridge_url":     bridgeURL,
			"bridge_running": false,
			"error":          err.Error(),
		}, http.StatusOK
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return map[string]interface{}{
			"ok":             false,
			"enabled":        waCfg.Enabled,
			"bridge_url":     bridgeURL,
			"bridge_running": false,
			"error":          strings.TrimSpace(string(body)),
		}, http.StatusOK
	}
	var status channels.WhatsAppBridgeStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return map[string]interface{}{
			"ok":             false,
			"enabled":        waCfg.Enabled,
			"bridge_url":     bridgeURL,
			"bridge_running": false,
			"error":          err.Error(),
		}, http.StatusOK
	}
	return map[string]interface{}{
		"ok":             true,
		"enabled":        waCfg.Enabled,
		"bridge_url":     bridgeURL,
		"bridge_running": true,
		"status": map[string]interface{}{
			"state":        status.State,
			"connected":    status.Connected,
			"logged_in":    status.LoggedIn,
			"bridge_addr":  status.BridgeAddr,
			"user_jid":     status.UserJID,
			"push_name":    status.PushName,
			"platform":     status.Platform,
			"qr_available": status.QRAvailable,
			"qr_code":      status.QRCode,
			"last_event":   status.LastEvent,
			"last_error":   status.LastError,
			"updated_at":   status.UpdatedAt,
		},
	}, http.StatusOK
}

func (s *Server) loadWhatsAppConfig() (cfgpkg.WhatsAppConfig, error) {
	cfg, err := s.loadConfig()
	if err != nil {
		return cfgpkg.WhatsAppConfig{}, err
	}
	return cfg.Channels.WhatsApp, nil
}

func (s *Server) loadConfig() (*cfgpkg.Config, error) {
	configPath := strings.TrimSpace(s.configPath)
	if configPath == "" {
		configPath = filepath.Join(cfgpkg.GetConfigDir(), "config.json")
	}
	return cfgpkg.LoadConfig(configPath)
}

func (s *Server) resolveWhatsAppBridgeURL(cfg *cfgpkg.Config) string {
	if cfg == nil {
		return ""
	}
	raw := strings.TrimSpace(cfg.Channels.WhatsApp.BridgeURL)
	if raw == "" {
		return embeddedWhatsAppBridgeURL(cfg.Gateway.Host, cfg.Gateway.Port)
	}
	hostPort := comparableBridgeHostPort(raw)
	if hostPort == "" {
		return raw
	}
	if hostPort == "127.0.0.1:3001" || hostPort == "localhost:3001" {
		return embeddedWhatsAppBridgeURL(cfg.Gateway.Host, cfg.Gateway.Port)
	}
	if hostPort == comparableGatewayHostPort(cfg.Gateway.Host, cfg.Gateway.Port) {
		return embeddedWhatsAppBridgeURL(cfg.Gateway.Host, cfg.Gateway.Port)
	}
	return raw
}

func embeddedWhatsAppBridgeURL(host string, port int) string {
	host = strings.TrimSpace(host)
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return fmt.Sprintf("ws://%s:%d/whatsapp/ws", host, port)
}

func comparableBridgeHostPort(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		return strings.ToLower(raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(u.Host))
}

func comparableGatewayHostPort(host string, port int) string {
	host = strings.TrimSpace(strings.ToLower(host))
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func renderQRCodeSVG(code *qr.Code, scale, quietZone int) string {
	if code == nil || code.Size <= 0 {
		return ""
	}
	if scale <= 0 {
		scale = 8
	}
	if quietZone < 0 {
		quietZone = 0
	}
	total := (code.Size + quietZone*2) * scale
	var b strings.Builder
	b.Grow(total * 8)
	b.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, total, total))
	b.WriteString(fmt.Sprintf(`<rect width="%d" height="%d" fill="#ffffff"/>`, total, total))
	b.WriteString(`<g fill="#111111">`)
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if !code.Black(x, y) {
				continue
			}
			rx := (x + quietZone) * scale
			ry := (y + quietZone) * scale
			b.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d"/>`, rx, ry, scale, scale))
		}
	}
	b.WriteString(`</g></svg>`)
	return b.String()
}
