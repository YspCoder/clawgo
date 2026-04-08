package api

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/channels"
	cfgpkg "github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/providers"
	"github.com/YspCoder/clawgo/pkg/tools"
	"github.com/gorilla/websocket"
	"rsc.io/qr"
)

var websocketUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Server struct {
	addr           string
	token          string
	server         *http.Server
	gatewayVersion string
	configPath     string
	workspacePath  string
	logFilePath    string
	onChat         func(ctx context.Context, sessionKey, content string) (string, error)
	onChatHistory  func(sessionKey string) []map[string]interface{}
	onConfigAfter  func(forceRuntimeReload bool) error
	onCron         func(action string, args map[string]interface{}) (interface{}, error)
	onToolsCatalog func() interface{}
	messageBus     *bus.MessageBus
	weixinChannel  *channels.WeixinChannel
	oauthFlowMu    sync.Mutex
	oauthFlows     map[string]*providers.OAuthPendingFlow
	extraRoutesMu  sync.RWMutex
	extraRoutes    map[string]http.Handler
	eventSubsMu    sync.Mutex
	eventSubs      map[*websocket.Conn]struct{}
	draftMu        sync.RWMutex
	channelDrafts  channelDraftStore
}

type channelDraftStore struct {
	Weixin        *cfgpkg.WeixinConfig
	Telegram      *cfgpkg.TelegramConfig
	Feishu        *cfgpkg.FeishuConfig
	weixinRuntime *channels.WeixinChannel
}

func NewServer(host string, port int, token string) *Server {
	addr := strings.TrimSpace(host)
	if addr == "" {
		addr = "0.0.0.0"
	}
	if port <= 0 {
		port = 7788
	}
	return &Server{
		addr:        fmt.Sprintf("%s:%d", addr, port),
		token:       strings.TrimSpace(token),
		oauthFlows:  map[string]*providers.OAuthPendingFlow{},
		extraRoutes: map[string]http.Handler{},
		eventSubs:   map[*websocket.Conn]struct{}{},
	}
}

func (s *Server) SetConfigPath(path string)    { s.configPath = strings.TrimSpace(path) }
func (s *Server) SetWorkspacePath(path string) { s.workspacePath = strings.TrimSpace(path) }
func (s *Server) SetLogFilePath(path string)   { s.logFilePath = strings.TrimSpace(path) }
func (s *Server) SetToken(token string)        { s.token = strings.TrimSpace(token) }
func (s *Server) SetChatHandler(fn func(ctx context.Context, sessionKey, content string) (string, error)) {
	s.onChat = fn
}
func (s *Server) SetChatHistoryHandler(fn func(sessionKey string) []map[string]interface{}) {
	s.onChatHistory = fn
}
func (s *Server) SetMessageBus(mb *bus.MessageBus)                          { s.messageBus = mb }
func (s *Server) SetConfigAfterHook(fn func(forceRuntimeReload bool) error) { s.onConfigAfter = fn }
func (s *Server) SetCronHandler(fn func(action string, args map[string]interface{}) (interface{}, error)) {
	s.onCron = fn
}
func (s *Server) SetToolsCatalogHandler(fn func() interface{}) { s.onToolsCatalog = fn }
func (s *Server) SetGatewayVersion(v string)                   { s.gatewayVersion = strings.TrimSpace(v) }
func (s *Server) SetProtectedRoute(path string, handler http.Handler) {
	if s == nil {
		return
	}
	path = strings.TrimSpace(path)
	s.extraRoutesMu.Lock()
	defer s.extraRoutesMu.Unlock()
	if path == "" || handler == nil {
		delete(s.extraRoutes, path)
		return
	}
	s.extraRoutes[path] = handler
}
func (s *Server) SetWeixinChannel(ch *channels.WeixinChannel) {
	s.weixinChannel = ch
	if ch != nil {
		ch.SetPersistHook(func(source string) {
			s.broadcastEvent(map[string]interface{}{
				"type":   "config_changed",
				"source": strings.TrimSpace(source),
			})
		})
	}
}

func cloneWeixinConfig(cfg cfgpkg.WeixinConfig) cfgpkg.WeixinConfig {
	cp := cfg
	cp.AllowFrom = append([]string(nil), cfg.AllowFrom...)
	cp.Accounts = append([]cfgpkg.WeixinAccountConfig(nil), cfg.Accounts...)
	return cp
}

func cloneTelegramConfig(cfg cfgpkg.TelegramConfig) cfgpkg.TelegramConfig {
	cp := cfg
	cp.AllowFrom = append([]string(nil), cfg.AllowFrom...)
	cp.AllowChats = append([]string(nil), cfg.AllowChats...)
	return cp
}

func cloneFeishuConfig(cfg cfgpkg.FeishuConfig) cfgpkg.FeishuConfig {
	cp := cfg
	cp.AllowFrom = append([]string(nil), cfg.AllowFrom...)
	cp.AllowChats = append([]string(nil), cfg.AllowChats...)
	return cp
}

func validChannelDraftName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "weixin", "telegram", "feishu":
		return true
	default:
		return false
	}
}

func decodeMergedJSON[T any](current T, raw json.RawMessage) (T, error) {
	out := current
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	baseBytes, err := json.Marshal(current)
	if err != nil {
		return out, err
	}
	merged := map[string]interface{}{}
	if err := json.Unmarshal(baseBytes, &merged); err != nil {
		return out, err
	}
	patch := map[string]interface{}{}
	if err := json.Unmarshal(raw, &patch); err != nil {
		return out, err
	}
	merged = mergeJSONMap(merged, patch)
	mergedBytes, err := json.Marshal(merged)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(mergedBytes, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Server) syncWeixinDraftLocked() {
	if s.channelDrafts.Weixin == nil || s.channelDrafts.weixinRuntime == nil {
		return
	}
	snapshot := s.channelDrafts.weixinRuntime.SnapshotConfig()
	s.channelDrafts.Weixin = &snapshot
}

func (s *Server) replaceWeixinDraftRuntimeLocked(cfg *cfgpkg.WeixinConfig) error {
	if s.channelDrafts.weixinRuntime != nil {
		_ = s.channelDrafts.weixinRuntime.Stop(context.Background())
		s.channelDrafts.weixinRuntime = nil
	}
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	if s.messageBus == nil {
		return fmt.Errorf("message bus not configured")
	}
	ch, err := channels.NewWeixinChannel(cloneWeixinConfig(*cfg), s.messageBus)
	if err != nil {
		return err
	}
	if err := ch.Start(context.Background()); err != nil {
		return err
	}
	s.channelDrafts.weixinRuntime = ch
	return nil
}

func (s *Server) clearChannelDraftsLocked() {
	if s.channelDrafts.weixinRuntime != nil {
		_ = s.channelDrafts.weixinRuntime.Stop(context.Background())
	}
	s.channelDrafts = channelDraftStore{}
}

func (s *Server) clearChannelDrafts() {
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	s.clearChannelDraftsLocked()
}

func (s *Server) effectiveWeixinRuntime(persisted cfgpkg.WeixinConfig) (cfgpkg.WeixinConfig, *channels.WeixinChannel, bool) {
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	if s.channelDrafts.Weixin != nil {
		s.syncWeixinDraftLocked()
		effective := cloneWeixinConfig(*s.channelDrafts.Weixin)
		return effective, s.channelDrafts.weixinRuntime, true
	}
	return cloneWeixinConfig(persisted), s.weixinChannel, false
}

func (s *Server) ensureWeixinRuntimeForLogin(persisted cfgpkg.WeixinConfig) (cfgpkg.WeixinConfig, *channels.WeixinChannel, bool, error) {
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	if s.channelDrafts.Weixin != nil {
		s.syncWeixinDraftLocked()
		effective := cloneWeixinConfig(*s.channelDrafts.Weixin)
		return effective, s.channelDrafts.weixinRuntime, true, nil
	}
	if s.weixinChannel != nil {
		return cloneWeixinConfig(persisted), s.weixinChannel, false, nil
	}

	bootstrap := cloneWeixinConfig(persisted)
	bootstrap.Enabled = true
	if strings.TrimSpace(bootstrap.BaseURL) == "" {
		bootstrap.BaseURL = "https://ilinkai.weixin.qq.com"
	}
	s.channelDrafts.Weixin = &bootstrap
	if err := s.replaceWeixinDraftRuntimeLocked(&bootstrap); err != nil {
		return cloneWeixinConfig(persisted), nil, true, err
	}
	s.syncWeixinDraftLocked()
	effective := cloneWeixinConfig(*s.channelDrafts.Weixin)
	return effective, s.channelDrafts.weixinRuntime, true, nil
}

func (s *Server) currentChannelDraftPayload(cfg *cfgpkg.Config, channel string) map[string]interface{} {
	channel = strings.ToLower(strings.TrimSpace(channel))
	payload := map[string]interface{}{
		"ok":      true,
		"channel": channel,
	}
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	switch channel {
	case "weixin":
		persisted := cloneWeixinConfig(cfg.Channels.Weixin)
		var draft interface{}
		effective := persisted
		dirty := s.channelDrafts.Weixin != nil
		if dirty {
			s.syncWeixinDraftLocked()
			effective = cloneWeixinConfig(*s.channelDrafts.Weixin)
			draft = effective
		}
		payload["persisted"] = persisted
		payload["draft"] = draft
		payload["effective"] = effective
		payload["dirty"] = dirty
		payload["runtime_enabled"] = s.channelDrafts.weixinRuntime != nil && s.channelDrafts.weixinRuntime.IsRunning()
	case "telegram":
		persisted := cloneTelegramConfig(cfg.Channels.Telegram)
		var draft interface{}
		effective := persisted
		dirty := s.channelDrafts.Telegram != nil
		if dirty {
			effective = cloneTelegramConfig(*s.channelDrafts.Telegram)
			draft = effective
		}
		payload["persisted"] = persisted
		payload["draft"] = draft
		payload["effective"] = effective
		payload["dirty"] = dirty
	case "feishu":
		persisted := cloneFeishuConfig(cfg.Channels.Feishu)
		var draft interface{}
		effective := persisted
		dirty := s.channelDrafts.Feishu != nil
		if dirty {
			effective = cloneFeishuConfig(*s.channelDrafts.Feishu)
			draft = effective
		}
		payload["persisted"] = persisted
		payload["draft"] = draft
		payload["effective"] = effective
		payload["dirty"] = dirty
	}
	return payload
}

func (s *Server) applyChannelDrafts(cfg *cfgpkg.Config) {
	if cfg == nil {
		return
	}
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	s.syncWeixinDraftLocked()
	if s.channelDrafts.Weixin != nil {
		cfg.Channels.Weixin = cloneWeixinConfig(*s.channelDrafts.Weixin)
	}
	if s.channelDrafts.Telegram != nil {
		cfg.Channels.Telegram = cloneTelegramConfig(*s.channelDrafts.Telegram)
	}
	if s.channelDrafts.Feishu != nil {
		cfg.Channels.Feishu = cloneFeishuConfig(*s.channelDrafts.Feishu)
	}
}

func (s *Server) handleWebUIChannelDraft(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	cfg, err := s.loadConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodGet:
		channel := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("channel")))
		if channel == "" {
			writeJSON(w, map[string]interface{}{
				"ok": true,
				"channels": map[string]interface{}{
					"weixin":   s.currentChannelDraftPayload(cfg, "weixin"),
					"telegram": s.currentChannelDraftPayload(cfg, "telegram"),
					"feishu":   s.currentChannelDraftPayload(cfg, "feishu"),
				},
			})
			return
		}
		if !validChannelDraftName(channel) {
			http.Error(w, "unsupported channel", http.StatusBadRequest)
			return
		}
		writeJSON(w, s.currentChannelDraftPayload(cfg, channel))
	case http.MethodPost:
		var body struct {
			Channel string          `json:"channel"`
			Config  json.RawMessage `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		channel := strings.ToLower(strings.TrimSpace(body.Channel))
		if !validChannelDraftName(channel) {
			http.Error(w, "unsupported channel", http.StatusBadRequest)
			return
		}
		s.draftMu.Lock()
		switch channel {
		case "weixin":
			current := cfg.Channels.Weixin
			if s.channelDrafts.Weixin != nil {
				s.syncWeixinDraftLocked()
				current = cloneWeixinConfig(*s.channelDrafts.Weixin)
			}
			next, err := decodeMergedJSON(current, body.Config)
			if err != nil {
				s.draftMu.Unlock()
				http.Error(w, "invalid weixin config", http.StatusBadRequest)
				return
			}
			next = cloneWeixinConfig(next)
			if err := s.replaceWeixinDraftRuntimeLocked(&next); err != nil {
				s.draftMu.Unlock()
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			s.channelDrafts.Weixin = &next
		case "telegram":
			current := cfg.Channels.Telegram
			if s.channelDrafts.Telegram != nil {
				current = cloneTelegramConfig(*s.channelDrafts.Telegram)
			}
			next, err := decodeMergedJSON(current, body.Config)
			if err != nil {
				s.draftMu.Unlock()
				http.Error(w, "invalid telegram config", http.StatusBadRequest)
				return
			}
			next = cloneTelegramConfig(next)
			s.channelDrafts.Telegram = &next
		case "feishu":
			current := cfg.Channels.Feishu
			if s.channelDrafts.Feishu != nil {
				current = cloneFeishuConfig(*s.channelDrafts.Feishu)
			}
			next, err := decodeMergedJSON(current, body.Config)
			if err != nil {
				s.draftMu.Unlock()
				http.Error(w, "invalid feishu config", http.StatusBadRequest)
				return
			}
			next = cloneFeishuConfig(next)
			s.channelDrafts.Feishu = &next
		}
		s.draftMu.Unlock()
		s.broadcastEvent(map[string]interface{}{
			"type":    "channel_draft_changed",
			"channel": channel,
		})
		writeJSON(w, s.currentChannelDraftPayload(cfg, channel))
	case http.MethodDelete:
		channel := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("channel")))
		s.draftMu.Lock()
		if channel == "" {
			s.clearChannelDraftsLocked()
			s.draftMu.Unlock()
			writeJSON(w, map[string]interface{}{"ok": true, "cleared": "all"})
			return
		}
		if !validChannelDraftName(channel) {
			s.draftMu.Unlock()
			http.Error(w, "unsupported channel", http.StatusBadRequest)
			return
		}
		switch channel {
		case "weixin":
			if s.channelDrafts.weixinRuntime != nil {
				_ = s.channelDrafts.weixinRuntime.Stop(context.Background())
				s.channelDrafts.weixinRuntime = nil
			}
			s.channelDrafts.Weixin = nil
		case "telegram":
			s.channelDrafts.Telegram = nil
		case "feishu":
			s.channelDrafts.Feishu = nil
		}
		s.draftMu.Unlock()
		s.broadcastEvent(map[string]interface{}{
			"type":    "channel_draft_changed",
			"channel": channel,
		})
		writeJSON(w, map[string]interface{}{"ok": true, "channel": channel, "cleared": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWebUIChannelDraftCommit(w http.ResponseWriter, r *http.Request) {
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
	s.applyChannelDrafts(cfg)
	if err := s.persistWebUIConfig(cfg); err != nil {
		var validationErr *configValidationError
		if errors.As(err, &validationErr) {
			writeJSONStatus(w, http.StatusBadRequest, map[string]interface{}{
				"ok":     false,
				"error":  validationErr.Error(),
				"errors": validationErr.Fields,
			})
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.clearChannelDrafts()
	writeJSON(w, map[string]interface{}{"ok": true, "committed": true})
}

func (s *Server) handleWebUIEventsLive(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := websocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.eventSubsMu.Lock()
	s.eventSubs[conn] = struct{}{}
	s.eventSubsMu.Unlock()
	_ = conn.WriteJSON(map[string]interface{}{
		"type": "ready",
	})
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	s.eventSubsMu.Lock()
	delete(s.eventSubs, conn)
	s.eventSubsMu.Unlock()
	_ = conn.Close()
}

func (s *Server) broadcastEvent(payload map[string]interface{}) {
	if len(payload) == 0 {
		return
	}
	s.eventSubsMu.Lock()
	subs := make([]*websocket.Conn, 0, len(s.eventSubs))
	for conn := range s.eventSubs {
		subs = append(subs, conn)
	}
	s.eventSubsMu.Unlock()
	for _, conn := range subs {
		if conn == nil {
			continue
		}
		if err := conn.WriteJSON(payload); err != nil {
			s.eventSubsMu.Lock()
			delete(s.eventSubs, conn)
			s.eventSubsMu.Unlock()
			_ = conn.Close()
		}
	}
}

func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONStatus(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

func queryBoundedPositiveInt(r *http.Request, key string, fallback int, max int) int {
	if r == nil {
		return fallback
	}
	value := strings.TrimSpace(r.URL.Query().Get(strings.TrimSpace(key)))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return fallback
	}
	if max > 0 && n > max {
		return max
	}
	return n
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/api/config", s.handleWebUIConfig)
	mux.HandleFunc("/api/chat", s.handleWebUIChat)
	mux.HandleFunc("/api/chat/history", s.handleWebUIChatHistory)
	mux.HandleFunc("/api/chat/live", s.handleWebUIChatLive)
	mux.HandleFunc("/api/events/live", s.handleWebUIEventsLive)
	mux.HandleFunc("/api/version", s.handleWebUIVersion)
	mux.HandleFunc("/api/provider/oauth/start", s.handleWebUIProviderOAuthStart)
	mux.HandleFunc("/api/provider/oauth/complete", s.handleWebUIProviderOAuthComplete)
	mux.HandleFunc("/api/provider/oauth/import", s.handleWebUIProviderOAuthImport)
	mux.HandleFunc("/api/provider/oauth/accounts", s.handleWebUIProviderOAuthAccounts)
	mux.HandleFunc("/api/provider/models", s.handleWebUIProviderModels)
	mux.HandleFunc("/api/provider/runtime", s.handleWebUIProviderRuntime)
	mux.HandleFunc("/api/weixin/status", s.handleWebUIWeixinStatus)
	mux.HandleFunc("/api/weixin/login/start", s.handleWebUIWeixinLoginStart)
	mux.HandleFunc("/api/weixin/login/cancel", s.handleWebUIWeixinLoginCancel)
	mux.HandleFunc("/api/weixin/qr.svg", s.handleWebUIWeixinQR)
	mux.HandleFunc("/api/weixin/accounts/remove", s.handleWebUIWeixinAccountRemove)
	mux.HandleFunc("/api/weixin/accounts/default", s.handleWebUIWeixinAccountDefault)
	mux.HandleFunc("/api/upload", s.handleWebUIUpload)
	mux.HandleFunc("/api/cron", s.handleWebUICron)
	mux.HandleFunc("/api/skills", s.handleWebUISkills)
	mux.HandleFunc("/api/sessions", s.handleWebUISessions)
	mux.HandleFunc("/api/memory", s.handleWebUIMemory)
	mux.HandleFunc("/api/workspace_file", s.handleWebUIWorkspaceFile)
	mux.HandleFunc("/api/workspace_docs", s.handleWebUIWorkspaceDocs)
	mux.HandleFunc("/api/tool_allowlist_groups", s.handleWebUIToolAllowlistGroups)
	mux.HandleFunc("/api/tools", s.handleWebUITools)
	mux.HandleFunc("/api/mcp/install", s.handleWebUIMCPInstall)
	mux.HandleFunc("/api/logs/live", s.handleWebUILogsLive)
	mux.HandleFunc("/api/logs/recent", s.handleWebUILogsRecent)
	mux.HandleFunc("/api/channels/draft", s.handleWebUIChannelDraft)
	mux.HandleFunc("/api/channels/draft/commit", s.handleWebUIChannelDraftCommit)
	s.extraRoutesMu.RLock()
	for path, handler := range s.extraRoutes {
		routePath := path
		routeHandler := handler
		mux.Handle(routePath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !s.checkAuth(r) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			routeHandler.ServeHTTP(w, r)
		}))
	}
	s.extraRoutesMu.RUnlock()
	s.server = &http.Server{Addr: s.addr, Handler: s.withCORS(mux)}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.server.Shutdown(shutdownCtx)
	}()
	go func() { _ = s.server.ListenAndServe() }()
	return nil
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		allowMethods := strings.TrimSpace(r.Header.Get("Access-Control-Request-Method"))
		if allowMethods == "" {
			allowMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
		}
		w.Header().Set("Access-Control-Allow-Methods", allowMethods)
		allowHeaders := strings.TrimSpace(r.Header.Get("Access-Control-Request-Headers"))
		if allowHeaders == "" {
			allowHeaders = "Authorization, Content-Type, X-Requested-With, Accept, Origin, Cache-Control, Pragma"
		}
		w.Header().Set("Access-Control-Allow-Headers", allowHeaders)
		w.Header().Set("Access-Control-Expose-Headers", "*")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleWebUIConfig(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if strings.TrimSpace(s.configPath) == "" {
		http.Error(w, "config path not set", http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("mode")), "normalized") {
			cfg, err := cfgpkg.LoadConfig(s.configPath)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			payload := map[string]interface{}{
				"ok":         true,
				"config":     cfg.NormalizedView(),
				"raw_config": cfg,
			}
			if r.URL.Query().Get("include_hot_reload_fields") == "1" {
				info := hotReloadFieldInfo()
				paths := make([]string, 0, len(info))
				for _, it := range info {
					if p := stringFromMap(it, "path"); p != "" {
						paths = append(paths, p)
					}
				}
				payload["hot_reload_fields"] = paths
				payload["hot_reload_field_details"] = info
			}
			writeJSON(w, payload)
			return
		}
		b, err := os.ReadFile(s.configPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		cfgDefault := cfgpkg.DefaultConfig()
		defBytes, _ := json.Marshal(cfgDefault)
		var merged map[string]interface{}
		_ = json.Unmarshal(defBytes, &merged)
		var loaded map[string]interface{}
		if err := json.Unmarshal(b, &loaded); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		merged = mergeJSONMap(merged, loaded)

		if r.URL.Query().Get("include_hot_reload_fields") == "1" || strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("mode")), "hot") {
			info := hotReloadFieldInfo()
			paths := make([]string, 0, len(info))
			for _, it := range info {
				if p := stringFromMap(it, "path"); p != "" {
					paths = append(paths, p)
				}
			}
			writeJSON(w, map[string]interface{}{
				"ok":                       true,
				"config":                   merged,
				"hot_reload_fields":        paths,
				"hot_reload_field_details": info,
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		out, _ := json.MarshalIndent(merged, "", "  ")
		_, _ = w.Write(out)
	case http.MethodPost:
		if err := s.saveWebUIConfig(r); err != nil {
			var validationErr *configValidationError
			if errors.As(err, &validationErr) {
				writeJSONStatus(w, http.StatusBadRequest, map[string]interface{}{
					"ok":     false,
					"error":  validationErr.Error(),
					"errors": validationErr.Fields,
				})
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type configValidationError struct {
	Fields []string
}

func (e *configValidationError) Error() string {
	if e == nil || len(e.Fields) == 0 {
		return "invalid config"
	}
	return "invalid config: " + strings.Join(e.Fields, "; ")
}

func (s *Server) saveWebUIConfig(r *http.Request) error {
	if r == nil {
		return fmt.Errorf("request is nil")
	}
	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	switch mode {
	case "", "raw":
		cfg := cfgpkg.DefaultConfig()
		if err := json.NewDecoder(r.Body).Decode(cfg); err != nil {
			return fmt.Errorf("decode config: %w", err)
		}
		s.applyChannelDrafts(cfg)
		if err := s.persistWebUIConfig(cfg); err != nil {
			return err
		}
		s.clearChannelDrafts()
		return nil
	case "normalized":
		cfg, err := cfgpkg.LoadConfig(s.configPath)
		if err != nil {
			return err
		}
		var view cfgpkg.NormalizedConfig
		if err := json.NewDecoder(r.Body).Decode(&view); err != nil {
			return fmt.Errorf("decode normalized config: %w", err)
		}
		cfg.ApplyNormalizedView(view)
		s.applyChannelDrafts(cfg)
		if err := s.persistWebUIConfig(cfg); err != nil {
			return err
		}
		s.clearChannelDrafts()
		return nil
	default:
		return fmt.Errorf("unsupported config mode: %s", mode)
	}
}

func (s *Server) persistWebUIConfig(cfg *cfgpkg.Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	cfg.Normalize()
	if errs := cfgpkg.Validate(cfg); len(errs) > 0 {
		fields := make([]string, 0, len(errs))
		for _, err := range errs {
			if err != nil {
				fields = append(fields, err.Error())
			}
		}
		return &configValidationError{Fields: fields}
	}
	if err := cfgpkg.SaveConfig(s.configPath, cfg); err != nil {
		return err
	}
	if s.onConfigAfter != nil {
		if err := s.onConfigAfter(false); err != nil {
			return err
		}
		s.broadcastEvent(map[string]interface{}{
			"type":   "config_changed",
			"source": "webui",
		})
		return nil
	}
	if err := requestSelfReloadSignal(); err != nil {
		return err
	}
	s.broadcastEvent(map[string]interface{}{
		"type":   "config_changed",
		"source": "webui",
	})
	return nil
}

func mergeJSONMap(base, override map[string]interface{}) map[string]interface{} {
	if base == nil {
		base = map[string]interface{}{}
	}
	for k, v := range override {
		if bv, ok := base[k]; ok {
			bm, ok1 := bv.(map[string]interface{})
			om, ok2 := v.(map[string]interface{})
			if ok1 && ok2 {
				base[k] = mergeJSONMap(bm, om)
				continue
			}
		}
		base[k] = v
	}
	return base
}

func (s *Server) handleWebUIUpload(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, h, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file required", http.StatusBadRequest)
		return
	}
	defer f.Close()
	dir := filepath.Join(os.TempDir(), "clawgo_webui_uploads")
	_ = os.MkdirAll(dir, 0755)
	name := fmt.Sprintf("%d_%s", time.Now().UnixNano(), filepath.Base(h.Filename))
	path := filepath.Join(dir, name)
	out, err := os.Create(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, f); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "path": path, "name": h.Filename})
}

func (s *Server) handleWebUIProviderOAuthStart(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Provider       string                `json:"provider"`
		AccountLabel   string                `json:"account_label"`
		NetworkProxy   string                `json:"network_proxy"`
		ProviderConfig cfgpkg.ProviderConfig `json:"provider_config"`
	}
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
	} else {
		body.Provider = strings.TrimSpace(r.URL.Query().Get("provider"))
		body.AccountLabel = strings.TrimSpace(r.URL.Query().Get("account_label"))
		body.NetworkProxy = strings.TrimSpace(r.URL.Query().Get("network_proxy"))
	}
	cfg, pc, err := s.resolveProviderConfig(strings.TrimSpace(body.Provider), body.ProviderConfig)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = cfg
	timeout := pc.TimeoutSec
	if timeout <= 0 {
		timeout = 90
	}
	loginMgr, err := providers.NewOAuthLoginManager(pc, time.Duration(timeout)*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	flow, err := loginMgr.StartManualFlowWithOptions(providers.OAuthLoginOptions{
		AccountLabel: body.AccountLabel,
		NetworkProxy: firstNonEmptyString(strings.TrimSpace(body.NetworkProxy), strings.TrimSpace(pc.OAuth.NetworkProxy)),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	flowID := fmt.Sprintf("%d", time.Now().UnixNano())
	s.oauthFlowMu.Lock()
	s.oauthFlows[flowID] = flow
	s.oauthFlowMu.Unlock()
	writeJSON(w, map[string]interface{}{
		"ok":            true,
		"flow_id":       flowID,
		"mode":          flow.Mode,
		"auth_url":      flow.AuthURL,
		"user_code":     flow.UserCode,
		"instructions":  flow.Instructions,
		"account_label": strings.TrimSpace(body.AccountLabel),
		"network_proxy": strings.TrimSpace(body.NetworkProxy),
	})
}

func (s *Server) handleWebUIProviderOAuthComplete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Provider       string                `json:"provider"`
		FlowID         string                `json:"flow_id"`
		CallbackURL    string                `json:"callback_url"`
		AccountLabel   string                `json:"account_label"`
		NetworkProxy   string                `json:"network_proxy"`
		ProviderConfig cfgpkg.ProviderConfig `json:"provider_config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	cfg, pc, err := s.resolveProviderConfig(strings.TrimSpace(body.Provider), body.ProviderConfig)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	timeout := pc.TimeoutSec
	if timeout <= 0 {
		timeout = 90
	}
	loginMgr, err := providers.NewOAuthLoginManager(pc, time.Duration(timeout)*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.oauthFlowMu.Lock()
	flow := s.oauthFlows[strings.TrimSpace(body.FlowID)]
	delete(s.oauthFlows, strings.TrimSpace(body.FlowID))
	s.oauthFlowMu.Unlock()
	if flow == nil {
		http.Error(w, "oauth flow not found", http.StatusBadRequest)
		return
	}
	session, models, err := loginMgr.CompleteManualFlowWithOptions(r.Context(), pc.APIBase, flow, body.CallbackURL, providers.OAuthLoginOptions{
		AccountLabel: strings.TrimSpace(body.AccountLabel),
		NetworkProxy: firstNonEmptyString(strings.TrimSpace(body.NetworkProxy), strings.TrimSpace(pc.OAuth.NetworkProxy)),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if session.CredentialFile != "" {
		pc.OAuth.CredentialFile = session.CredentialFile
		pc.OAuth.CredentialFiles = appendUniqueStrings(pc.OAuth.CredentialFiles, session.CredentialFile)
	}
	if err := s.saveProviderConfig(cfg, body.Provider, pc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":              true,
		"account":         session.Email,
		"credential_file": session.CredentialFile,
		"network_proxy":   session.NetworkProxy,
		"models":          models,
	})
}

func (s *Server) handleWebUIProviderOAuthImport(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	providerName := strings.TrimSpace(r.FormValue("provider"))
	accountLabel := strings.TrimSpace(r.FormValue("account_label"))
	networkProxy := strings.TrimSpace(r.FormValue("network_proxy"))
	inlineCfgRaw := strings.TrimSpace(r.FormValue("provider_config"))
	var inlineCfg cfgpkg.ProviderConfig
	if inlineCfgRaw != "" {
		if err := json.Unmarshal([]byte(inlineCfgRaw), &inlineCfg); err != nil {
			http.Error(w, "invalid provider_config", http.StatusBadRequest)
			return
		}
	}
	cfg, pc, err := s.resolveProviderConfig(providerName, inlineCfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	timeout := pc.TimeoutSec
	if timeout <= 0 {
		timeout = 90
	}
	loginMgr, err := providers.NewOAuthLoginManager(pc, time.Duration(timeout)*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	session, models, err := loginMgr.ImportAuthJSONWithOptions(r.Context(), pc.APIBase, header.Filename, data, providers.OAuthLoginOptions{
		AccountLabel: accountLabel,
		NetworkProxy: firstNonEmptyString(networkProxy, strings.TrimSpace(pc.OAuth.NetworkProxy)),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if session.CredentialFile != "" {
		pc.OAuth.CredentialFile = session.CredentialFile
		pc.OAuth.CredentialFiles = appendUniqueStrings(pc.OAuth.CredentialFiles, session.CredentialFile)
	}
	if err := s.saveProviderConfig(cfg, providerName, pc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":              true,
		"account":         session.Email,
		"credential_file": session.CredentialFile,
		"network_proxy":   session.NetworkProxy,
		"models":          models,
	})
}

func (s *Server) handleWebUIProviderOAuthAccounts(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	providerName := strings.TrimSpace(r.URL.Query().Get("provider"))
	cfg, pc, err := s.loadProviderConfig(providerName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = cfg
	timeout := pc.TimeoutSec
	if timeout <= 0 {
		timeout = 90
	}
	loginMgr, err := providers.NewOAuthLoginManager(pc, time.Duration(timeout)*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		accounts, err := loginMgr.ListAccounts()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "accounts": accounts})
	case http.MethodPost:
		var body struct {
			Action         string `json:"action"`
			CredentialFile string `json:"credential_file"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		switch strings.ToLower(strings.TrimSpace(body.Action)) {
		case "refresh":
			account, err := loginMgr.RefreshAccount(r.Context(), body.CredentialFile)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, map[string]interface{}{"ok": true, "account": account})
		case "delete":
			if err := loginMgr.DeleteAccount(body.CredentialFile); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			pc.OAuth.CredentialFiles = removeStringItem(pc.OAuth.CredentialFiles, body.CredentialFile)
			if strings.TrimSpace(pc.OAuth.CredentialFile) == strings.TrimSpace(body.CredentialFile) {
				pc.OAuth.CredentialFile = ""
				if len(pc.OAuth.CredentialFiles) > 0 {
					pc.OAuth.CredentialFile = pc.OAuth.CredentialFiles[0]
				}
			}
			if err := s.saveProviderConfig(cfg, providerName, pc); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]interface{}{"ok": true, "deleted": true})
		case "clear_cooldown":
			if err := loginMgr.ClearCooldown(body.CredentialFile); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, map[string]interface{}{"ok": true, "cleared": true})
		default:
			http.Error(w, "unsupported action", http.StatusBadRequest)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWebUIProviderModels(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Provider string   `json:"provider"`
		Model    string   `json:"model"`
		Models   []string `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	cfg, pc, err := s.loadProviderConfig(strings.TrimSpace(body.Provider))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	models := make([]string, 0, len(body.Models)+1)
	for _, model := range body.Models {
		models = appendUniqueStrings(models, model)
	}
	models = appendUniqueStrings(models, body.Model)
	if len(models) == 0 {
		http.Error(w, "model required", http.StatusBadRequest)
		return
	}
	pc.Models = models
	if err := s.saveProviderConfig(cfg, body.Provider, pc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":     true,
		"models": pc.Models,
	})
}

func (s *Server) handleWebUIProviderRuntime(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet {
		cfg, err := cfgpkg.LoadConfig(s.configPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		query := providers.ProviderRuntimeQuery{
			Provider:    strings.TrimSpace(r.URL.Query().Get("provider")),
			EventKind:   strings.TrimSpace(r.URL.Query().Get("kind")),
			Reason:      strings.TrimSpace(r.URL.Query().Get("reason")),
			Target:      strings.TrimSpace(r.URL.Query().Get("target")),
			Sort:        strings.TrimSpace(r.URL.Query().Get("sort")),
			ChangesOnly: strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("changes_only")), "true"),
		}
		if secs, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("window_sec"))); secs > 0 {
			query.Window = time.Duration(secs) * time.Second
		}
		if limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit"))); limit > 0 {
			query.Limit = limit
		}
		if cursor, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("cursor"))); cursor >= 0 {
			query.Cursor = cursor
		}
		if healthBelow, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("health_below"))); healthBelow > 0 {
			query.HealthBelow = healthBelow
		}
		if secs, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("cooldown_until_before_sec"))); secs > 0 {
			query.CooldownBefore = time.Now().Add(time.Duration(secs) * time.Second)
		}
		writeJSON(w, map[string]interface{}{
			"ok":   true,
			"view": providers.GetProviderRuntimeView(cfg, query),
		})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Provider     string `json:"provider"`
		Action       string `json:"action"`
		OnlyExpiring bool   `json:"only_expiring"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	switch strings.ToLower(strings.TrimSpace(body.Action)) {
	case "clear_api_cooldown":
		cfg, providerName, err := s.loadRuntimeProviderName(strings.TrimSpace(body.Provider))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = cfg
		providers.ClearProviderAPICooldown(providerName)
		writeJSON(w, map[string]interface{}{"ok": true, "cleared": true})
	case "clear_history":
		cfg, providerName, err := s.loadRuntimeProviderName(strings.TrimSpace(body.Provider))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = cfg
		providers.ClearProviderRuntimeHistory(providerName)
		writeJSON(w, map[string]interface{}{"ok": true, "cleared": true})
	case "refresh_now":
		cfg, providerName, err := s.loadRuntimeProviderName(strings.TrimSpace(body.Provider))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		result, err := providers.RefreshProviderRuntimeNow(cfg, providerName, body.OnlyExpiring)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		order, _ := providers.RerankProviderRuntime(cfg, providerName)
		summary := providers.GetProviderRuntimeSummary(cfg, providers.ProviderRuntimeQuery{Provider: providerName, HealthBelow: 50})
		writeJSON(w, map[string]interface{}{
			"ok":              true,
			"provider":        providerName,
			"refreshed":       true,
			"result":          result,
			"candidate_order": order,
			"summary":         summary,
		})
	case "rerank":
		cfg, providerName, err := s.loadRuntimeProviderName(strings.TrimSpace(body.Provider))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		order, err := providers.RerankProviderRuntime(cfg, providerName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "provider": providerName, "reranked": true, "candidate_order": order})
	default:
		http.Error(w, "unsupported action", http.StatusBadRequest)
	}
}

func (s *Server) loadProviderConfig(name string) (*cfgpkg.Config, cfgpkg.ProviderConfig, error) {
	if strings.TrimSpace(s.configPath) == "" {
		return nil, cfgpkg.ProviderConfig{}, fmt.Errorf("config path not set")
	}
	cfg, err := cfgpkg.LoadConfig(s.configPath)
	if err != nil {
		return nil, cfgpkg.ProviderConfig{}, err
	}
	providerName := strings.TrimSpace(name)
	if providerName == "" {
		providerName = cfgpkg.PrimaryProviderName(cfg)
	}
	pc, ok := cfgpkg.ProviderConfigByName(cfg, providerName)
	if !ok {
		return nil, cfgpkg.ProviderConfig{}, fmt.Errorf("provider %q not found", providerName)
	}
	return cfg, pc, nil
}

func (s *Server) loadRuntimeProviderName(name string) (*cfgpkg.Config, string, error) {
	if strings.TrimSpace(s.configPath) == "" {
		return nil, "", fmt.Errorf("config path not set")
	}
	cfg, err := cfgpkg.LoadConfig(s.configPath)
	if err != nil {
		return nil, "", err
	}
	providerName := strings.TrimSpace(name)
	if providerName == "" {
		providerName = cfgpkg.PrimaryProviderName(cfg)
	}
	if !cfgpkg.ProviderExists(cfg, providerName) {
		return nil, "", fmt.Errorf("provider %q not found", providerName)
	}
	return cfg, providerName, nil
}

func (s *Server) resolveProviderConfig(name string, inline cfgpkg.ProviderConfig) (*cfgpkg.Config, cfgpkg.ProviderConfig, error) {
	if hasInlineProviderConfig(inline) {
		cfg, err := cfgpkg.LoadConfig(s.configPath)
		if err != nil {
			return nil, cfgpkg.ProviderConfig{}, err
		}
		return cfg, inline, nil
	}
	return s.loadProviderConfig(name)
}

func hasInlineProviderConfig(pc cfgpkg.ProviderConfig) bool {
	return strings.TrimSpace(pc.APIBase) != "" ||
		strings.TrimSpace(pc.APIKey) != "" ||
		len(pc.Models) > 0 ||
		strings.TrimSpace(pc.Auth) != "" ||
		strings.TrimSpace(pc.OAuth.Provider) != ""
}

func (s *Server) saveProviderConfig(cfg *cfgpkg.Config, name string, pc cfgpkg.ProviderConfig) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	providerName := strings.TrimSpace(name)
	if cfg.Models.Providers == nil {
		cfg.Models.Providers = map[string]cfgpkg.ProviderConfig{}
	}
	cfg.Models.Providers[providerName] = pc
	if err := cfgpkg.SaveConfig(s.configPath, cfg); err != nil {
		return err
	}
	if s.onConfigAfter != nil {
		// Provider updates can take effect through external credential files
		// even when config.json remains structurally identical.
		if err := s.onConfigAfter(true); err != nil {
			return err
		}
	} else {
		if err := requestSelfReloadSignal(); err != nil {
			return err
		}
	}
	return nil
}

func appendUniqueStrings(values []string, item string) []string {
	item = strings.TrimSpace(item)
	if item == "" {
		return values
	}
	for _, value := range values {
		if strings.TrimSpace(value) == item {
			return values
		}
	}
	return append(values, item)
}

func removeStringItem(values []string, item string) []string {
	item = strings.TrimSpace(item)
	if item == "" {
		return values
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == item {
			continue
		}
		out = append(out, value)
	}
	return out
}

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
	conn, err := websocketUpgrader.Upgrade(w, r, nil)
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
		"compiled_channels": channels.CompiledChannelKeys(),
	})
}

func (s *Server) handleWebUIWeixinStatus(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	payload, code := s.webUIWeixinStatusPayload(r.Context())
	writeJSONStatus(w, code, payload)
}

func (s *Server) handleWebUIWeixinLoginStart(w http.ResponseWriter, r *http.Request) {
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
	_, ch, _, err := s.ensureWeixinRuntimeForLogin(cfg.Channels.Weixin)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if ch == nil {
		http.Error(w, "weixin channel unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, err := ch.StartLogin(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	payload, code := s.webUIWeixinStatusPayload(r.Context())
	writeJSONStatus(w, code, payload)
}

func (s *Server) handleWebUIWeixinLoginCancel(w http.ResponseWriter, r *http.Request) {
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
	_, ch, _ := s.effectiveWeixinRuntime(cfg.Channels.Weixin)
	if ch == nil {
		http.Error(w, "weixin channel unavailable", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		LoginID string `json:"login_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if !ch.CancelPendingLogin(body.LoginID) {
		http.Error(w, "login_id not found", http.StatusNotFound)
		return
	}
	payload, code := s.webUIWeixinStatusPayload(r.Context())
	writeJSONStatus(w, code, payload)
}

func (s *Server) handleWebUIWeixinQR(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	payload, code := s.webUIWeixinStatusPayload(r.Context())
	if code != http.StatusOK {
		http.Error(w, "qr unavailable", http.StatusNotFound)
		return
	}
	qrCode := ""
	loginID := strings.TrimSpace(r.URL.Query().Get("login_id"))
	cfg, err := s.loadConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, ch, _ := s.effectiveWeixinRuntime(cfg.Channels.Weixin)
	if loginID != "" && ch != nil {
		if pending := ch.PendingLoginByID(loginID); pending != nil {
			qrCode = fallbackString(pending.QRCodeImgContent, pending.QRCode)
		}
	}
	if qrCode == "" {
		pendingItems, _ := payload["pending_logins"].([]interface{})
		if len(pendingItems) > 0 {
			if pending, ok := pendingItems[0].(map[string]interface{}); ok {
				qrCode = fallbackString(stringFromMap(pending, "qr_code_img_content"), stringFromMap(pending, "qr_code"))
			}
		}
	}
	if strings.TrimSpace(qrCode) == "" {
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

func (s *Server) handleWebUIWeixinAccountRemove(w http.ResponseWriter, r *http.Request) {
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
	_, ch, _ := s.effectiveWeixinRuntime(cfg.Channels.Weixin)
	if ch == nil {
		http.Error(w, "weixin channel unavailable", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		BotID string `json:"bot_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if err := ch.RemoveAccount(body.BotID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	payload, code := s.webUIWeixinStatusPayload(r.Context())
	writeJSONStatus(w, code, payload)
}

func (s *Server) handleWebUIWeixinAccountDefault(w http.ResponseWriter, r *http.Request) {
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
	_, ch, _ := s.effectiveWeixinRuntime(cfg.Channels.Weixin)
	if ch == nil {
		http.Error(w, "weixin channel unavailable", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		BotID string `json:"bot_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if err := ch.SetDefaultAccount(body.BotID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	payload, code := s.webUIWeixinStatusPayload(r.Context())
	writeJSONStatus(w, code, payload)
}

func (s *Server) webUIWeixinStatusPayload(ctx context.Context) (map[string]interface{}, int) {
	cfg, err := s.loadConfig()
	if err != nil {
		return map[string]interface{}{
			"ok":    false,
			"error": err.Error(),
		}, http.StatusInternalServerError
	}
	persistedCfg := cloneWeixinConfig(cfg.Channels.Weixin)
	weixinCfg, ch, usingDraft := s.effectiveWeixinRuntime(persistedCfg)
	if ch == nil {
		return map[string]interface{}{
			"ok":              false,
			"enabled":         weixinCfg.Enabled,
			"config_enabled":  persistedCfg.Enabled,
			"runtime_enabled": false,
			"draft_dirty":     usingDraft,
			"base_url":        weixinCfg.BaseURL,
			"error":           "weixin channel unavailable",
		}, http.StatusOK
	}
	pendingLogins, err := ch.RefreshLoginStatuses(ctx)
	if err != nil {
		return map[string]interface{}{
			"ok":              false,
			"enabled":         weixinCfg.Enabled,
			"config_enabled":  persistedCfg.Enabled,
			"runtime_enabled": ch.IsRunning(),
			"draft_dirty":     usingDraft,
			"base_url":        weixinCfg.BaseURL,
			"error":           err.Error(),
		}, http.StatusOK
	}
	if usingDraft {
		weixinCfg = ch.SnapshotConfig()
	}
	accounts := ch.ListAccounts()
	pendingPayload := make([]map[string]interface{}, 0, len(pendingLogins))
	for _, pending := range pendingLogins {
		pendingPayload = append(pendingPayload, map[string]interface{}{
			"login_id":            pendingString(pending, "login_id"),
			"qr_code":             pendingString(pending, "qr_code"),
			"qr_code_img_content": pendingString(pending, "qr_code_img_content"),
			"status":              pendingString(pending, "status"),
			"last_error":          pendingString(pending, "last_error"),
			"updated_at":          pendingString(pending, "updated_at"),
			"qr_available":        pending != nil && strings.TrimSpace(fallbackString(pending.QRCodeImgContent, pending.QRCode)) != "",
		})
	}
	var firstPending *channels.WeixinPendingLogin
	if len(pendingLogins) > 0 {
		firstPending = pendingLogins[0]
	}
	return map[string]interface{}{
		"ok":              true,
		"enabled":         weixinCfg.Enabled,
		"config_enabled":  persistedCfg.Enabled,
		"runtime_enabled": ch.IsRunning(),
		"draft_dirty":     usingDraft,
		"base_url":        fallbackString(weixinCfg.BaseURL, "https://ilinkai.weixin.qq.com"),
		"pending_logins":  pendingPayload,
		"pending_login": map[string]interface{}{
			"login_id":            pendingString(firstPending, "login_id"),
			"qr_code":             pendingString(firstPending, "qr_code"),
			"qr_code_img_content": pendingString(firstPending, "qr_code_img_content"),
			"status":              pendingString(firstPending, "status"),
			"last_error":          pendingString(firstPending, "last_error"),
			"updated_at":          pendingString(firstPending, "updated_at"),
			"qr_available":        firstPending != nil && strings.TrimSpace(fallbackString(firstPending.QRCodeImgContent, firstPending.QRCode)) != "",
		},
		"accounts": accounts,
	}, http.StatusOK
}

func (s *Server) loadConfig() (*cfgpkg.Config, error) {
	configPath := strings.TrimSpace(s.configPath)
	if configPath == "" {
		configPath = filepath.Join(cfgpkg.GetConfigDir(), "config.json")
	}
	cfg, err := cfgpkg.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	return cfg, nil
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

func sanitizeZipEntryName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "artifact.bin"
	}
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, name)
	if strings.Trim(name, "._") == "" {
		return "artifact.bin"
	}
	return name
}

func resolveArtifactPath(workspace, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if filepath.IsAbs(raw) {
		clean := filepath.Clean(raw)
		if info, err := os.Stat(clean); err == nil && !info.IsDir() {
			return clean
		}
		return ""
	}
	root := strings.TrimSpace(workspace)
	if root == "" {
		return ""
	}
	clean := filepath.Clean(filepath.Join(root, raw))
	if rel, err := filepath.Rel(root, clean); err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	if info, err := os.Stat(clean); err == nil && !info.IsDir() {
		return clean
	}
	return ""
}

func readArtifactBytes(workspace string, item map[string]interface{}) ([]byte, string, error) {
	if content := strings.TrimSpace(fmt.Sprint(item["content_base64"])); content != "" {
		raw, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return nil, "", err
		}
		return raw, strings.TrimSpace(fmt.Sprint(item["mime_type"])), nil
	}
	for _, rawPath := range []string{fmt.Sprint(item["source_path"]), fmt.Sprint(item["path"])} {
		if path := resolveArtifactPath(workspace, rawPath); path != "" {
			b, err := os.ReadFile(path)
			if err != nil {
				return nil, "", err
			}
			return b, strings.TrimSpace(fmt.Sprint(item["mime_type"])), nil
		}
	}
	if contentText := fmt.Sprint(item["content_text"]); strings.TrimSpace(contentText) != "" {
		return []byte(contentText), "text/plain; charset=utf-8", nil
	}
	return nil, "", fmt.Errorf("artifact content unavailable")
}

func resolveRelativeFilePath(root, raw string) (string, string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", "", fmt.Errorf("workspace not configured")
	}
	clean := filepath.Clean(strings.TrimSpace(raw))
	if clean == "." || clean == "" || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", "", fmt.Errorf("invalid path")
	}
	full := filepath.Join(root, clean)
	cleanRoot := filepath.Clean(root)
	if full != cleanRoot {
		prefix := cleanRoot + string(os.PathSeparator)
		if !strings.HasPrefix(filepath.Clean(full), prefix) {
			return "", "", fmt.Errorf("invalid path")
		}
	}
	return clean, full, nil
}

func relativeFilePathStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if err.Error() == "workspace not configured" {
		return http.StatusInternalServerError
	}
	return http.StatusBadRequest
}

func readRelativeTextFile(root, raw string) (string, string, bool, error) {
	clean, full, err := resolveRelativeFilePath(root, raw)
	if err != nil {
		return "", "", false, err
	}
	b, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return clean, "", false, nil
		}
		return clean, "", false, err
	}
	return clean, string(b), true, nil
}

func writeRelativeTextFile(root, raw string, content string, ensureDir bool) (string, error) {
	clean, full, err := resolveRelativeFilePath(root, raw)
	if err != nil {
		return "", err
	}
	if ensureDir {
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		return "", err
	}
	return clean, nil
}

func (s *Server) memoryFilePath(name string) string {
	workspace := strings.TrimSpace(s.workspacePath)
	if workspace == "" {
		return ""
	}
	return filepath.Join(workspace, "memory", strings.TrimSpace(name))
}
func (s *Server) handleWebUITools(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	toolsList := []map[string]interface{}{}
	if s.onToolsCatalog != nil {
		if items, ok := s.onToolsCatalog().([]map[string]interface{}); ok && items != nil {
			toolsList = items
		}
	}
	mcpItems := make([]map[string]interface{}, 0)
	for _, item := range toolsList {
		if strings.TrimSpace(fmt.Sprint(item["source"])) == "mcp" {
			mcpItems = append(mcpItems, item)
		}
	}
	serverChecks := []map[string]interface{}{}
	if strings.TrimSpace(s.configPath) != "" {
		if cfg, err := cfgpkg.LoadConfig(s.configPath); err == nil {
			serverChecks = buildMCPServerChecks(cfg)
		}
	}
	writeJSON(w, map[string]interface{}{
		"tools":             toolsList,
		"mcp_tools":         mcpItems,
		"mcp_server_checks": serverChecks,
	})
}

func buildMCPServerChecks(cfg *cfgpkg.Config) []map[string]interface{} {
	if cfg == nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Tools.MCP.Servers))
	for name := range cfg.Tools.MCP.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		server := cfg.Tools.MCP.Servers[name]
		transport := strings.ToLower(strings.TrimSpace(server.Transport))
		if transport == "" {
			transport = "stdio"
		}
		command := strings.TrimSpace(server.Command)
		status := "missing_command"
		message := "command is empty"
		resolved := ""
		missingCommand := false
		if !server.Enabled {
			status = "disabled"
			message = "server is disabled"
		} else if transport != "stdio" {
			status = "not_applicable"
			message = "command check not required for non-stdio transport"
		} else if command != "" {
			if filepath.IsAbs(command) {
				if info, err := os.Stat(command); err == nil && !info.IsDir() {
					status = "ok"
					message = "command found"
					resolved = command
				} else {
					status = "missing_command"
					message = fmt.Sprintf("command not found: %s", command)
					missingCommand = true
				}
			} else if path, err := exec.LookPath(command); err == nil {
				status = "ok"
				message = "command found"
				resolved = path
			} else {
				status = "missing_command"
				message = fmt.Sprintf("command not found in PATH: %s", command)
				missingCommand = true
			}
		}
		installSpec := inferMCPInstallSpec(server)
		items = append(items, map[string]interface{}{
			"name":            name,
			"enabled":         server.Enabled,
			"transport":       transport,
			"status":          status,
			"message":         message,
			"command":         command,
			"resolved":        resolved,
			"package":         installSpec.Package,
			"installer":       installSpec.Installer,
			"installable":     missingCommand && installSpec.AutoInstallSupported,
			"missing_command": missingCommand,
		})
	}
	return items
}

type mcpInstallSpec struct {
	Installer            string
	Package              string
	AutoInstallSupported bool
}

func inferMCPInstallSpec(server cfgpkg.MCPServerConfig) mcpInstallSpec {
	if pkgName := strings.TrimSpace(server.Package); pkgName != "" {
		return mcpInstallSpec{Package: pkgName, AutoInstallSupported: false}
	}
	command := strings.TrimSpace(server.Command)
	args := make([]string, 0, len(server.Args))
	for _, arg := range server.Args {
		if v := strings.TrimSpace(arg); v != "" {
			args = append(args, v)
		}
	}
	base := filepath.Base(command)
	switch base {
	case "npx":
		return mcpInstallSpec{Package: firstNonFlagArg(args), AutoInstallSupported: false}
	case "uvx":
		pkgName := firstNonFlagArg(args)
		return mcpInstallSpec{Installer: "uv", Package: pkgName, AutoInstallSupported: pkgName != ""}
	case "bunx":
		pkgName := firstNonFlagArg(args)
		return mcpInstallSpec{Installer: "bun", Package: pkgName, AutoInstallSupported: pkgName != ""}
	case "python", "python3":
		if len(args) >= 2 && args[0] == "-m" {
			return mcpInstallSpec{Installer: "pip", Package: strings.TrimSpace(args[1]), AutoInstallSupported: false}
		}
	}
	return mcpInstallSpec{}
}

func firstNonFlagArg(args []string) string {
	for _, arg := range args {
		item := strings.TrimSpace(arg)
		if item == "" || strings.HasPrefix(item, "-") {
			continue
		}
		return item
	}
	return ""
}

func (s *Server) handleWebUIMCPInstall(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Package   string `json:"package"`
		Installer string `json:"installer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	pkgName := strings.TrimSpace(body.Package)
	if pkgName == "" {
		http.Error(w, "package required", http.StatusBadRequest)
		return
	}
	out, binName, binPath, err := ensureMCPPackageInstalledWithInstaller(r.Context(), pkgName, body.Installer)
	if err != nil {
		msg := err.Error()
		if strings.TrimSpace(out) != "" {
			msg = strings.TrimSpace(out) + "\n" + msg
		}
		http.Error(w, strings.TrimSpace(msg), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":       true,
		"package":  pkgName,
		"output":   out,
		"bin_name": binName,
		"bin_path": binPath,
	})
}

func stringFromMap(item map[string]interface{}, key string) string {
	return tools.MapStringArg(item, key)
}

func rawStringFromMap(item map[string]interface{}, key string) string {
	return tools.MapRawStringArg(item, key)
}

func stringListFromMap(item map[string]interface{}, key string) []string {
	return tools.MapStringListArg(item, key)
}

func intFromMap(item map[string]interface{}, key string, fallback int) int {
	return tools.MapIntArg(item, key, fallback)
}

func fallbackString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func pendingString(item *channels.WeixinPendingLogin, key string) string {
	if item == nil {
		return ""
	}
	switch strings.TrimSpace(key) {
	case "login_id":
		return strings.TrimSpace(item.LoginID)
	case "qr_code":
		return strings.TrimSpace(item.QRCode)
	case "qr_code_img_content":
		return strings.TrimSpace(item.QRCodeImgContent)
	case "status":
		return strings.TrimSpace(item.Status)
	case "last_error":
		return strings.TrimSpace(item.LastError)
	case "updated_at":
		return strings.TrimSpace(item.UpdatedAt)
	default:
		return ""
	}
}

func (s *Server) handleWebUICron(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.onCron == nil {
		http.Error(w, "cron handler not configured", http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		action := "list"
		if id != "" {
			action = "get"
		}
		res, err := s.onCron(action, map[string]interface{}{"id": id})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if action == "list" {
			writeJSON(w, map[string]interface{}{"ok": true, "jobs": normalizeCronJobs(res)})
		} else {
			writeJSON(w, map[string]interface{}{"ok": true, "job": normalizeCronJob(res)})
		}
	case http.MethodPost:
		args := map[string]interface{}{}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&args)
		}
		if id := strings.TrimSpace(r.URL.Query().Get("id")); id != "" {
			args["id"] = id
		}
		action := "create"
		if a := tools.MapStringArg(args, "action"); a != "" {
			action = strings.ToLower(strings.TrimSpace(a))
		}
		res, err := s.onCron(action, args)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "result": normalizeCronJob(res)})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWebUISkills(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	skillsDir := filepath.Join(s.workspacePath, "skills")
	if strings.TrimSpace(skillsDir) == "" {
		http.Error(w, "workspace not configured", http.StatusInternalServerError)
		return
	}
	_ = os.MkdirAll(skillsDir, 0755)

	resolveSkillPath := func(name string) (string, error) {
		name = strings.TrimSpace(name)
		if name == "" {
			return "", fmt.Errorf("name required")
		}
		cands := []string{
			filepath.Join(skillsDir, name),
			filepath.Join(skillsDir, name+".disabled"),
			filepath.Join("/root/clawgo/workspace/skills", name),
			filepath.Join("/root/clawgo/workspace/skills", name+".disabled"),
		}
		for _, p := range cands {
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				return p, nil
			}
		}
		return "", fmt.Errorf("skill not found: %s", name)
	}

	switch r.Method {
	case http.MethodGet:
		clawhubPath := strings.TrimSpace(resolveClawHubBinary(r.Context()))
		clawhubInstalled := clawhubPath != ""
		if id := strings.TrimSpace(r.URL.Query().Get("id")); id != "" {
			skillPath, err := resolveSkillPath(id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			if strings.TrimSpace(r.URL.Query().Get("files")) == "1" {
				var files []string
				_ = filepath.WalkDir(skillPath, func(path string, d os.DirEntry, err error) error {
					if err != nil {
						return nil
					}
					if d.IsDir() {
						return nil
					}
					rel, _ := filepath.Rel(skillPath, path)
					if strings.HasPrefix(rel, "..") {
						return nil
					}
					files = append(files, filepath.ToSlash(rel))
					return nil
				})
				writeJSON(w, map[string]interface{}{"ok": true, "id": id, "files": files})
				return
			}
			if f := strings.TrimSpace(r.URL.Query().Get("file")); f != "" {
				clean, content, found, err := readRelativeTextFile(skillPath, f)
				if err != nil {
					http.Error(w, err.Error(), relativeFilePathStatus(err))
					return
				}
				if !found {
					http.Error(w, os.ErrNotExist.Error(), http.StatusInternalServerError)
					return
				}
				writeJSON(w, map[string]interface{}{"ok": true, "id": id, "file": filepath.ToSlash(clean), "content": content})
				return
			}
		}

		type skillItem struct {
			ID            string   `json:"id"`
			Name          string   `json:"name"`
			Description   string   `json:"description"`
			Tools         []string `json:"tools"`
			SystemPrompt  string   `json:"system_prompt,omitempty"`
			Enabled       bool     `json:"enabled"`
			UpdateChecked bool     `json:"update_checked"`
			RemoteFound   bool     `json:"remote_found,omitempty"`
			RemoteVersion string   `json:"remote_version,omitempty"`
			CheckError    string   `json:"check_error,omitempty"`
			Source        string   `json:"source,omitempty"`
		}
		candDirs := []string{skillsDir, filepath.Join("/root/clawgo/workspace", "skills")}
		seenDirs := map[string]struct{}{}
		seenSkills := map[string]struct{}{}
		items := make([]skillItem, 0)
		// Default off to avoid hammering clawhub search API on each UI refresh.
		// Enable explicitly with ?check_updates=1 when needed.
		checkUpdates := strings.TrimSpace(r.URL.Query().Get("check_updates")) == "1"

		for _, dir := range candDirs {
			dir = strings.TrimSpace(dir)
			if dir == "" {
				continue
			}
			if _, ok := seenDirs[dir]; ok {
				continue
			}
			seenDirs[dir] = struct{}{}
			entries, err := os.ReadDir(dir)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				name := e.Name()
				enabled := !strings.HasSuffix(name, ".disabled")
				baseName := strings.TrimSuffix(name, ".disabled")
				if _, ok := seenSkills[baseName]; ok {
					continue
				}
				seenSkills[baseName] = struct{}{}
				desc, tools, sys := readSkillMeta(filepath.Join(dir, name, "SKILL.md"))
				if desc == "" || len(tools) == 0 || sys == "" {
					d2, t2, s2 := readSkillMeta(filepath.Join(dir, baseName, "SKILL.md"))
					if desc == "" {
						desc = d2
					}
					if len(tools) == 0 {
						tools = t2
					}
					if sys == "" {
						sys = s2
					}
				}
				if tools == nil {
					tools = []string{}
				}
				it := skillItem{ID: baseName, Name: baseName, Description: desc, Tools: tools, SystemPrompt: sys, Enabled: enabled, UpdateChecked: checkUpdates && clawhubInstalled, Source: dir}
				if checkUpdates && clawhubInstalled {
					found, version, checkErr := queryClawHubSkillVersion(r.Context(), baseName)
					it.RemoteFound = found
					it.RemoteVersion = version
					if checkErr != nil {
						it.CheckError = checkErr.Error()
					}
				}
				items = append(items, it)
			}
		}
		writeJSON(w, map[string]interface{}{
			"ok":                true,
			"skills":            items,
			"source":            "clawhub",
			"clawhub_installed": clawhubInstalled,
			"clawhub_path":      clawhubPath,
		})

	case http.MethodPost:
		ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
		if strings.Contains(ct, "multipart/form-data") {
			imported, err := importSkillArchiveFromMultipart(r, skillsDir)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, map[string]interface{}{"ok": true, "imported": imported})
			return
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		action := strings.ToLower(stringFromMap(body, "action"))
		id := stringFromMap(body, "id")
		name := stringFromMap(body, "name")
		if strings.TrimSpace(name) == "" {
			name = id
		}
		name = strings.TrimSpace(name)
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		enabledPath := filepath.Join(skillsDir, name)
		disabledPath := enabledPath + ".disabled"

		switch action {
		case "install":
			clawhubPath := strings.TrimSpace(resolveClawHubBinary(r.Context()))
			if clawhubPath == "" {
				http.Error(w, "clawhub is not installed. please install clawhub first.", http.StatusPreconditionFailed)
				return
			}
			ignoreSuspicious, _ := tools.MapBoolArg(body, "ignore_suspicious")
			args := []string{"install", name}
			if ignoreSuspicious {
				args = append(args, "--force")
			}
			cmd := exec.CommandContext(r.Context(), clawhubPath, args...)
			cmd.Dir = strings.TrimSpace(s.workspacePath)
			out, err := cmd.CombinedOutput()
			if err != nil {
				outText := string(out)
				lower := strings.ToLower(outText)
				if strings.Contains(lower, "rate limit exceeded") || strings.Contains(lower, "too many requests") {
					http.Error(w, fmt.Sprintf("clawhub rate limit exceeded. please retry later or configure auth token.\n%s", outText), http.StatusTooManyRequests)
					return
				}
				http.Error(w, fmt.Sprintf("install failed: %v\n%s", err, outText), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]interface{}{"ok": true, "installed": name, "output": string(out)})
		case "enable":
			if _, err := os.Stat(disabledPath); err == nil {
				if err := os.Rename(disabledPath, enabledPath); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			writeJSON(w, map[string]interface{}{"ok": true})
		case "disable":
			if _, err := os.Stat(enabledPath); err == nil {
				if err := os.Rename(enabledPath, disabledPath); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			writeJSON(w, map[string]interface{}{"ok": true})
		case "write_file":
			skillPath, err := resolveSkillPath(name)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			content := rawStringFromMap(body, "content")
			filePath := stringFromMap(body, "file")
			clean, err := writeRelativeTextFile(skillPath, filePath, content, true)
			if err != nil {
				http.Error(w, err.Error(), relativeFilePathStatus(err))
				return
			}
			writeJSON(w, map[string]interface{}{"ok": true, "name": name, "file": filepath.ToSlash(clean)})
		case "create", "update":
			desc := rawStringFromMap(body, "description")
			sys := rawStringFromMap(body, "system_prompt")
			toolsList := stringListFromMap(body, "tools")
			if action == "create" {
				if _, err := os.Stat(enabledPath); err == nil {
					http.Error(w, "skill already exists", http.StatusBadRequest)
					return
				}
			}
			if err := os.MkdirAll(filepath.Join(enabledPath, "scripts"), 0755); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			skillMD := buildSkillMarkdown(name, desc, toolsList, sys)
			if err := os.WriteFile(filepath.Join(enabledPath, "SKILL.md"), []byte(skillMD), 0644); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]interface{}{"ok": true})
		default:
			http.Error(w, "unsupported action", http.StatusBadRequest)
		}

	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		pathA := filepath.Join(skillsDir, id)
		pathB := pathA + ".disabled"
		deleted := false
		if err := os.RemoveAll(pathA); err == nil {
			deleted = true
		}
		if err := os.RemoveAll(pathB); err == nil {
			deleted = true
		}
		writeJSON(w, map[string]interface{}{"ok": true, "deleted": deleted, "id": id})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func buildSkillMarkdown(name, desc string, tools []string, systemPrompt string) string {
	if desc == "" {
		desc = "No description provided."
	}
	if len(tools) == 0 {
		tools = []string{""}
	}
	toolLines := make([]string, 0, len(tools))
	for _, t := range tools {
		if t == "" {
			continue
		}
		toolLines = append(toolLines, "- "+t)
	}
	if len(toolLines) == 0 {
		toolLines = []string{"- (none)"}
	}
	return fmt.Sprintf(`---
name: %s
description: %s
---

# %s

%s

## Tools
%s

## System Prompt
%s
`, name, desc, name, desc, strings.Join(toolLines, "\n"), systemPrompt)
}

func readSkillMeta(path string) (desc string, tools []string, systemPrompt string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", []string{}, ""
	}
	s := string(b)
	reDesc := regexp.MustCompile(`(?m)^description:\s*(.+)$`)
	reTools := regexp.MustCompile(`(?m)^##\s*Tools\s*$`)
	rePrompt := regexp.MustCompile(`(?m)^##\s*System Prompt\s*$`)
	if m := reDesc.FindStringSubmatch(s); len(m) > 1 {
		desc = m[1]
	}
	if loc := reTools.FindStringIndex(s); loc != nil {
		block := s[loc[1]:]
		if p := rePrompt.FindStringIndex(block); p != nil {
			block = block[:p[0]]
		}
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimPrefix(line, "-")
			if line != "" {
				tools = append(tools, line)
			}
		}
	}
	if tools == nil {
		tools = []string{}
	}
	if loc := rePrompt.FindStringIndex(s); loc != nil {
		systemPrompt = s[loc[1]:]
	}
	return
}

func gatewayBuildVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi != nil {
		ver := strings.TrimSpace(bi.Main.Version)
		rev := ""
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				rev = s.Value
				break
			}
		}
		if len(rev) > 8 {
			rev = rev[:8]
		}
		if ver == "" || ver == "(devel)" {
			ver = "devel"
		}
		if rev != "" {
			return ver + "+" + rev
		}
		return ver
	}
	return "unknown"
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func detectLocalIP() string {
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, a := range addrs {
				var ip net.IP
				switch v := a.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if ip == nil || ip.IsLoopback() {
					continue
				}
				ip = ip.To4()
				if ip == nil {
					continue
				}
				return ip.String()
			}
		}
	}
	// Fallback: detect outbound source IP.
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		if ua, ok := conn.LocalAddr().(*net.UDPAddr); ok && ua.IP != nil {
			if ip := ua.IP.To4(); ip != nil {
				return ip.String()
			}
		}
	}
	return ""
}

func normalizeCronJob(v interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]interface{}{"raw": fmt.Sprintf("%v", v)}
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]interface{}{"raw": string(b)}
	}
	out := map[string]interface{}{}
	for k, val := range m {
		out[k] = val
	}
	if sch, ok := m["schedule"].(map[string]interface{}); ok {
		kind := stringFromMap(sch, "kind")
		if expr := stringFromMap(sch, "expr"); expr != "" {
			out["expr"] = expr
		} else if strings.EqualFold(strings.TrimSpace(kind), "every") {
			if every := intFromMap(sch, "everyMs", 0); every > 0 {
				out["expr"] = fmt.Sprintf("@every %s", (time.Duration(every) * time.Millisecond).String())
			}
		} else if strings.EqualFold(strings.TrimSpace(kind), "at") {
			if at := intFromMap(sch, "atMs", 0); at > 0 {
				out["expr"] = time.UnixMilli(int64(at)).Format(time.RFC3339)
			}
		}
	}
	if payload, ok := m["payload"].(map[string]interface{}); ok {
		if msg, ok := payload["message"]; ok {
			out["message"] = msg
		}
		if d, ok := payload["deliver"]; ok {
			out["deliver"] = d
		}
		if c, ok := payload["channel"]; ok {
			out["channel"] = c
		}
		if to, ok := payload["to"]; ok {
			out["to"] = to
		}
	}
	return out
}

func normalizeCronJobs(v interface{}) []map[string]interface{} {
	b, err := json.Marshal(v)
	if err != nil {
		return []map[string]interface{}{}
	}
	var arr []interface{}
	if err := json.Unmarshal(b, &arr); err != nil {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, len(arr))
	for _, it := range arr {
		out = append(out, normalizeCronJob(it))
	}
	return out
}

func queryClawHubSkillVersion(ctx context.Context, skill string) (found bool, version string, err error) {
	if skill == "" {
		return false, "", fmt.Errorf("skill empty")
	}
	clawhubPath := strings.TrimSpace(resolveClawHubBinary(ctx))
	if clawhubPath == "" {
		return false, "", fmt.Errorf("clawhub not installed")
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, clawhubPath, "search", skill, "--json")
	out, runErr := cmd.Output()
	if runErr != nil {
		return false, "", runErr
	}
	var payload interface{}
	if err := json.Unmarshal(out, &payload); err != nil {
		return false, "", err
	}
	lowerSkill := strings.ToLower(skill)
	var walk func(v interface{}) (bool, string)
	walk = func(v interface{}) (bool, string) {
		switch t := v.(type) {
		case map[string]interface{}:
			name := strings.ToLower(strings.TrimSpace(anyToString(t["name"])))
			if name == "" {
				name = strings.ToLower(strings.TrimSpace(anyToString(t["id"])))
			}
			if name == lowerSkill || strings.Contains(name, lowerSkill) {
				ver := anyToString(t["version"])
				if ver == "" {
					ver = anyToString(t["latest_version"])
				}
				return true, ver
			}
			for _, vv := range t {
				if ok, ver := walk(vv); ok {
					return ok, ver
				}
			}
		case []interface{}:
			for _, vv := range t {
				if ok, ver := walk(vv); ok {
					return ok, ver
				}
			}
		}
		return false, ""
	}
	ok, ver := walk(payload)
	return ok, ver, nil
}

func resolveClawHubBinary(ctx context.Context) string {
	if p, err := exec.LookPath("clawhub"); err == nil {
		return p
	}
	cands := []string{
		"/usr/local/bin/clawhub",
		"/opt/homebrew/bin/clawhub",
	}
	for _, cand := range cands {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	return ""
}

func runInstallCommand(ctx context.Context, cmdline string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, "sh", "-c", cmdline)
	out, err := cmd.CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if err != nil {
		if msg == "" {
			msg = err.Error()
		}
		return msg, fmt.Errorf("%s", msg)
	}
	return msg, nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func ensureMCPPackageInstalledWithInstaller(ctx context.Context, pkgName, installer string) (output string, binName string, binPath string, err error) {
	pkgName = strings.TrimSpace(pkgName)
	if pkgName == "" {
		return "", "", "", fmt.Errorf("package empty")
	}
	installer = strings.ToLower(strings.TrimSpace(installer))
	if installer == "" {
		installer = "uv"
	}
	outs := make([]string, 0, 4)
	switch installer {
	case "uv":
		if !commandExists("uv") {
			return "", "", "", fmt.Errorf("uv is not installed; install uv first to auto-install %s", pkgName)
		}
		installOut, err := runInstallCommand(ctx, "uv tool install "+shellEscapeArg(pkgName))
		if installOut != "" {
			outs = append(outs, installOut)
		}
		if err != nil {
			return strings.Join(outs, "\n"), "", "", err
		}
		binName = guessSimpleCommandName(pkgName)
	case "bun":
		if !commandExists("bun") {
			return "", "", "", fmt.Errorf("bun is not installed; install bun first to auto-install %s", pkgName)
		}
		installOut, err := runInstallCommand(ctx, "bun add -g "+shellEscapeArg(pkgName))
		if installOut != "" {
			outs = append(outs, installOut)
		}
		if err != nil {
			return strings.Join(outs, "\n"), "", "", err
		}
		binName = guessSimpleCommandName(pkgName)
	default:
		return "", "", "", fmt.Errorf("unsupported installer: %s", installer)
	}
	binPath = resolveInstalledBinary(ctx, binName)
	if strings.TrimSpace(binPath) == "" {
		return strings.Join(outs, "\n"), binName, "", fmt.Errorf("installed %s but binary %q not found in PATH", pkgName, binName)
	}
	outs = append(outs, fmt.Sprintf("installed %s via %s", pkgName, installer))
	outs = append(outs, fmt.Sprintf("resolved binary: %s", binPath))
	return strings.Join(outs, "\n"), binName, binPath, nil
}

func guessSimpleCommandName(pkgName string) string {
	pkgName = strings.TrimSpace(pkgName)
	pkgName = strings.TrimPrefix(pkgName, "@")
	if idx := strings.LastIndex(pkgName, "/"); idx >= 0 {
		pkgName = pkgName[idx+1:]
	}
	return strings.TrimSpace(pkgName)
}

func resolveInstalledBinary(ctx context.Context, binName string) string {
	_ = ctx
	binName = strings.TrimSpace(binName)
	if binName == "" {
		return ""
	}
	if p, err := exec.LookPath(binName); err == nil {
		return p
	}
	cands := []string{
		filepath.Join("/usr/local/bin", binName),
		filepath.Join("/opt/homebrew/bin", binName),
	}
	for _, cand := range cands {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	return ""
}

func shellEscapeArg(in string) string {
	if strings.TrimSpace(in) == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(in, "'", `'\''`) + "'"
}

func importSkillArchiveFromMultipart(r *http.Request, skillsDir string) ([]string, error) {
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		return nil, err
	}
	f, h, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("file required")
	}
	defer f.Close()

	uploadDir := filepath.Join(os.TempDir(), "clawgo_skill_uploads")
	_ = os.MkdirAll(uploadDir, 0755)
	archivePath := filepath.Join(uploadDir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), filepath.Base(h.Filename)))
	out, err := os.Create(archivePath)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(out, f); err != nil {
		_ = out.Close()
		_ = os.Remove(archivePath)
		return nil, err
	}
	_ = out.Close()
	defer os.Remove(archivePath)

	extractDir, err := os.MkdirTemp("", "clawgo_skill_extract_*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(extractDir)

	if err := extractArchive(archivePath, extractDir); err != nil {
		return nil, err
	}

	type candidate struct {
		name string
		dir  string
	}
	candidates := make([]candidate, 0)
	seen := map[string]struct{}{}
	err = filepath.WalkDir(extractDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), "SKILL.md") {
			dir := filepath.Dir(path)
			rel, relErr := filepath.Rel(extractDir, dir)
			if relErr != nil {
				return nil
			}
			rel = filepath.ToSlash(strings.TrimSpace(rel))
			if rel == "" {
				rel = "."
			}
			name := filepath.Base(rel)
			if rel == "." {
				name = archiveBaseName(h.Filename)
			}
			name = sanitizeSkillName(name)
			if name == "" {
				return nil
			}
			if _, ok := seen[name]; ok {
				return nil
			}
			seen[name] = struct{}{}
			candidates = append(candidates, candidate{name: name, dir: dir})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no SKILL.md found in archive")
	}

	imported := make([]string, 0, len(candidates))
	for _, c := range candidates {
		dst := filepath.Join(skillsDir, c.name)
		if _, err := os.Stat(dst); err == nil {
			return nil, fmt.Errorf("skill already exists: %s", c.name)
		}
		if _, err := os.Stat(dst + ".disabled"); err == nil {
			return nil, fmt.Errorf("disabled skill already exists: %s", c.name)
		}
		if err := copyDir(c.dir, dst); err != nil {
			return nil, err
		}
		imported = append(imported, c.name)
	}
	sort.Strings(imported)
	return imported, nil
}

func archiveBaseName(filename string) string {
	name := filepath.Base(strings.TrimSpace(filename))
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return name[:len(name)-len(".tar.gz")]
	case strings.HasSuffix(lower, ".tgz"):
		return name[:len(name)-len(".tgz")]
	case strings.HasSuffix(lower, ".zip"):
		return name[:len(name)-len(".zip")]
	case strings.HasSuffix(lower, ".tar"):
		return name[:len(name)-len(".tar")]
	default:
		ext := filepath.Ext(name)
		return strings.TrimSuffix(name, ext)
	}
}

func sanitizeSkillName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, ch := range strings.ToLower(name) {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
			b.WriteRune(ch)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" || out == "." {
		return ""
	}
	return out
}

func extractArchive(archivePath, targetDir string) error {
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archivePath, targetDir)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(archivePath, targetDir)
	case strings.HasSuffix(lower, ".tar"):
		return extractTar(archivePath, targetDir)
	default:
		return fmt.Errorf("unsupported archive format: %s", filepath.Base(archivePath))
	}
}

func extractZip(archivePath, targetDir string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if err := writeArchivedEntry(targetDir, f.Name, f.FileInfo().IsDir(), func() (io.ReadCloser, error) {
			return f.Open()
		}); err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(archivePath, targetDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	return extractTarReader(tar.NewReader(gz), targetDir)
}

func extractTar(archivePath, targetDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	return extractTarReader(tar.NewReader(f), targetDir)
}

func extractTarReader(tr *tar.Reader, targetDir string) error {
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := writeArchivedEntry(targetDir, hdr.Name, true, nil); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			name := hdr.Name
			if err := writeArchivedEntry(targetDir, name, false, func() (io.ReadCloser, error) {
				return io.NopCloser(tr), nil
			}); err != nil {
				return err
			}
		}
	}
}

func writeArchivedEntry(targetDir, name string, isDir bool, opener func() (io.ReadCloser, error)) error {
	clean := filepath.Clean(strings.TrimSpace(name))
	clean = strings.TrimPrefix(clean, string(filepath.Separator))
	clean = strings.TrimPrefix(clean, "/")
	for strings.HasPrefix(clean, "../") {
		clean = strings.TrimPrefix(clean, "../")
	}
	if clean == "." || clean == "" {
		return nil
	}
	dst := filepath.Join(targetDir, clean)
	absTarget, _ := filepath.Abs(targetDir)
	absDst, _ := filepath.Abs(dst)
	if !strings.HasPrefix(absDst, absTarget+string(filepath.Separator)) && absDst != absTarget {
		return fmt.Errorf("invalid archive entry path: %s", name)
	}
	if isDir {
		return os.MkdirAll(dst, 0755)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	rc, err := opener()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		info, err := e.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		in, err := os.Open(srcPath)
		if err != nil {
			return err
		}
		out, err := os.Create(dstPath)
		if err != nil {
			_ = in.Close()
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			_ = in.Close()
			return err
		}
		_ = out.Close()
		_ = in.Close()
	}
	return nil
}

func anyToString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		if v == nil {
			return ""
		}
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func (s *Server) handleWebUISessions(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionsDir := filepath.Join(filepath.Dir(s.workspacePath), "agents", "main", "sessions")
	_ = os.MkdirAll(sessionsDir, 0755)
	includeInternal := r.URL.Query().Get("include_internal") == "1"
	type item struct {
		Key     string `json:"key"`
		Channel string `json:"channel,omitempty"`
	}
	out := make([]item, 0, 16)
	entries, err := os.ReadDir(sessionsDir)
	if err == nil {
		seen := map[string]struct{}{}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".jsonl") || strings.Contains(name, ".deleted.") {
				continue
			}
			key := strings.TrimSuffix(name, ".jsonl")
			if strings.TrimSpace(key) == "" {
				continue
			}
			if !includeInternal && !isUserFacingSessionKey(key) {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			channel := ""
			if i := strings.Index(key, ":"); i > 0 {
				channel = key[:i]
			}
			out = append(out, item{Key: key, Channel: channel})
		}
	}
	if len(out) == 0 {
		out = append(out, item{Key: "main", Channel: "main"})
	}
	writeJSON(w, map[string]interface{}{"ok": true, "sessions": out})
}

func isUserFacingSessionKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return false
	}
	switch {
	case strings.HasPrefix(k, "subagent:"):
		return false
	case strings.HasPrefix(k, "internal:"):
		return false
	case strings.HasPrefix(k, "heartbeat:"):
		return false
	case strings.HasPrefix(k, "cron:"):
		return false
	case strings.HasPrefix(k, "hook:"):
		return false
	default:
		return true
	}
}

func (s *Server) handleWebUIToolAllowlistGroups(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":     true,
		"groups": tools.ToolAllowlistGroups(),
	})
}

func (s *Server) handleWebUIMemory(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	memoryDir := filepath.Join(s.workspacePath, "memory")
	_ = os.MkdirAll(memoryDir, 0755)
	switch r.Method {
	case http.MethodGet:
		path := strings.TrimSpace(r.URL.Query().Get("path"))
		if path == "" {
			files := make([]string, 0, 16)
			entries, err := os.ReadDir(memoryDir)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				files = append(files, e.Name())
			}
			writeJSON(w, map[string]interface{}{"ok": true, "files": files})
			return
		}
		clean, content, found, err := readRelativeTextFile(memoryDir, path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !found {
			http.Error(w, os.ErrNotExist.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "path": clean, "content": content})
	case http.MethodPost:
		var body struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		clean, err := writeRelativeTextFile(memoryDir, body.Path, body.Content, false)
		if err != nil {
			http.Error(w, err.Error(), relativeFilePathStatus(err))
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "path": clean})
	case http.MethodDelete:
		clean, full, err := resolveRelativeFilePath(memoryDir, r.URL.Query().Get("path"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := os.Remove(full); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "deleted": true, "path": clean})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWebUIWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	workspace := strings.TrimSpace(s.workspacePath)
	switch r.Method {
	case http.MethodGet:
		path := strings.TrimSpace(r.URL.Query().Get("path"))
		clean, content, found, err := readRelativeTextFile(workspace, path)
		if err != nil {
			http.Error(w, err.Error(), relativeFilePathStatus(err))
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "path": clean, "found": found, "content": content})
	case http.MethodPost:
		var body struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		clean, err := writeRelativeTextFile(workspace, body.Path, body.Content, true)
		if err != nil {
			http.Error(w, err.Error(), relativeFilePathStatus(err))
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "path": clean, "saved": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

var workspaceDocFiles = []string{
	"AGENTS.md",
	"BOOT.md",
	"BOOTSTRAP.md",
	"HEARTBEAT.md",
	"IDENTITY.md",
	"MEMORY.md",
	"SOUL.md",
	"TOOLS.md",
	"USER.md",
}

func (s *Server) handleWebUIWorkspaceDocs(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspace := strings.TrimSpace(s.workspacePath)
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path != "" {
		if !isWorkspaceDocAllowed(path) {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		clean, content, found, err := readRelativeTextFile(workspace, path)
		if err != nil {
			http.Error(w, err.Error(), relativeFilePathStatus(err))
			return
		}
		if !found {
			http.Error(w, os.ErrNotExist.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "path": clean, "content": content})
		return
	}
	files := make([]string, 0, len(workspaceDocFiles))
	for _, name := range workspaceDocFiles {
		_, _, found, err := readRelativeTextFile(workspace, name)
		if err != nil {
			http.Error(w, err.Error(), relativeFilePathStatus(err))
			return
		}
		if !found {
			continue
		}
		files = append(files, name)
	}
	writeJSON(w, map[string]interface{}{"ok": true, "files": files})
}

func isWorkspaceDocAllowed(name string) bool {
	for _, allowed := range workspaceDocFiles {
		if name == allowed {
			return true
		}
	}
	return false
}

func (s *Server) handleWebUILogsRecent(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimSpace(s.logFilePath)
	if path == "" {
		http.Error(w, "log path not configured", http.StatusInternalServerError)
		return
	}
	limit := queryBoundedPositiveInt(r, "limit", 10, 200)
	b, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	start := 0
	if len(lines) > limit {
		start = len(lines) - limit
	}
	out := make([]map[string]interface{}, 0, limit)
	for _, ln := range lines[start:] {
		if parsed, ok := parseLogLine(ln); ok {
			out = append(out, parsed)
		}
	}
	writeJSON(w, map[string]interface{}{"ok": true, "logs": out})
}

func parseLogLine(line string) (map[string]interface{}, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, false
	}
	if json.Valid([]byte(line)) {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err == nil {
			return m, true
		}
	}
	return map[string]interface{}{
		"time":  time.Now().UTC().Format(time.RFC3339),
		"level": "INFO",
		"msg":   line,
	}, true
}

func (s *Server) handleWebUILogsLive(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimSpace(s.logFilePath)
	if path == "" {
		http.Error(w, "log path not configured", http.StatusInternalServerError)
		return
	}
	conn, err := websocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	f, err := os.Open(path)
	if err != nil {
		_ = conn.WriteJSON(map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	if fi != nil {
		_, _ = f.Seek(fi.Size(), io.SeekStart)
	}
	reader := bufio.NewReader(f)
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			line, err := reader.ReadString('\n')
			if parsed, ok := parseLogLine(line); ok {
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if writeErr := conn.WriteJSON(map[string]interface{}{"ok": true, "type": "log_entry", "entry": parsed}); writeErr != nil {
					return
				}
			}
			if err != nil {
				time.Sleep(500 * time.Millisecond)
			}
		}
	}
}

func (s *Server) checkAuth(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "Bearer "+s.token {
		return true
	}
	if strings.TrimSpace(r.URL.Query().Get("token")) == s.token {
		return true
	}
	if c, err := r.Cookie("clawgo_webui_token"); err == nil && strings.TrimSpace(c.Value) == s.token {
		return true
	}
	// Browser asset fallback: allow token propagated via Referer query.
	if ref := strings.TrimSpace(r.Referer()); ref != "" {
		if u, err := url.Parse(ref); err == nil {
			if strings.TrimSpace(u.Query().Get("token")) == s.token {
				return true
			}
		}
	}
	return false
}

func hotReloadFieldInfo() []map[string]interface{} {
	return []map[string]interface{}{
		{"path": "logging.*", "name": "Logging", "description": "Log level, persistence, and related settings"},
		{"path": "sentinel.*", "name": "Sentinel", "description": "Health checks and auto-heal behavior"},
		{"path": "agents.*", "name": "Agent", "description": "Models, policies, and default behavior"},
		{"path": "models.providers.*", "name": "Providers", "description": "LLM provider registry and auth settings"},
		{"path": "tools.*", "name": "Tools", "description": "Tool toggles and runtime options"},
		{"path": "channels.*", "name": "Channels", "description": "Weixin, Feishu, and Telegram channel settings"},
		{"path": "cron.*", "name": "Cron", "description": "Global cron runtime settings"},
		{"path": "agents.defaults.heartbeat.*", "name": "Heartbeat", "description": "Heartbeat interval and prompt template"},
		{"path": "gateway.*", "name": "Gateway", "description": "Mostly hot-reloadable; host/port may require restart"},
	}
}
