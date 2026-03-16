package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YspCoder/clawgo/pkg/channels"
	"github.com/YspCoder/clawgo/pkg/nodes"
	"github.com/YspCoder/clawgo/pkg/providers"
	rpcpkg "github.com/YspCoder/clawgo/pkg/rpc"
	"github.com/gorilla/websocket"
)

type Server struct {
	addr             string
	token            string
	mgr              *nodes.Manager
	server           *http.Server
	nodeConnMu       sync.Mutex
	nodeConnIDs      map[string]string
	nodeSockets      map[string]*nodeSocketConn
	nodeWebRTC       *nodes.WebRTCTransport
	nodeP2PStatus    func() map[string]interface{}
	artifactStatsMu  sync.Mutex
	artifactStats    map[string]interface{}
	gatewayVersion   string
	webuiVersion     string
	configPath       string
	workspacePath    string
	logFilePath      string
	onChat           func(ctx context.Context, sessionKey, content string) (string, error)
	onChatHistory    func(sessionKey string) []map[string]interface{}
	onConfigAfter    func() error
	onCron           func(action string, args map[string]interface{}) (interface{}, error)
	onRuntimeAdmin   func(ctx context.Context, action string, args map[string]interface{}) (interface{}, error)
	onNodeDispatch   func(ctx context.Context, req nodes.Request, mode string) (nodes.Response, error)
	onToolsCatalog   func() interface{}
	webUIDir         string
	ekgCacheMu       sync.Mutex
	ekgCachePath     string
	ekgCacheStamp    time.Time
	ekgCacheSize     int64
	ekgCacheRows     []map[string]interface{}
	liveRuntimeMu    sync.Mutex
	liveRuntimeSubs  map[chan []byte]struct{}
	liveRuntimeOn    bool
	whatsAppBridge   *channels.WhatsAppBridgeService
	whatsAppBase     string
	oauthFlowMu      sync.Mutex
	oauthFlows       map[string]*providers.OAuthPendingFlow
	extraRoutesMu    sync.RWMutex
	extraRoutes      map[string]http.Handler
	nodeRPCOnce      sync.Once
	nodeRPCReg       *rpcpkg.Registry
	providerRPCOnce  sync.Once
	providerRPCReg   *rpcpkg.Registry
	workspaceRPCOnce sync.Once
	workspaceRPCReg  *rpcpkg.Registry
	configRPCOnce    sync.Once
	configRPCReg     *rpcpkg.Registry
	cronRPCOnce      sync.Once
	cronRPCReg       *rpcpkg.Registry
	skillsRPCOnce    sync.Once
	skillsRPCReg     *rpcpkg.Registry
}

func NewServer(host string, port int, token string, mgr *nodes.Manager) *Server {
	addr := strings.TrimSpace(host)
	if addr == "" {
		addr = "0.0.0.0"
	}
	if port <= 0 {
		port = 7788
	}
	return &Server{
		addr:            fmt.Sprintf("%s:%d", addr, port),
		token:           strings.TrimSpace(token),
		mgr:             mgr,
		nodeConnIDs:     map[string]string{},
		nodeSockets:     map[string]*nodeSocketConn{},
		artifactStats:   map[string]interface{}{},
		liveRuntimeSubs: map[chan []byte]struct{}{},
		oauthFlows:      map[string]*providers.OAuthPendingFlow{},
		extraRoutes:     map[string]http.Handler{},
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
func (s *Server) SetConfigAfterHook(fn func() error) { s.onConfigAfter = fn }
func (s *Server) SetCronHandler(fn func(action string, args map[string]interface{}) (interface{}, error)) {
	s.onCron = fn
}
func (s *Server) SetRuntimeAdminHandler(fn func(ctx context.Context, action string, args map[string]interface{}) (interface{}, error)) {
	s.onRuntimeAdmin = fn
}
func (s *Server) SetNodeDispatchHandler(fn func(ctx context.Context, req nodes.Request, mode string) (nodes.Response, error)) {
	s.onNodeDispatch = fn
}
func (s *Server) SetToolsCatalogHandler(fn func() interface{}) { s.onToolsCatalog = fn }
func (s *Server) SetWebUIDir(dir string)                       { s.webUIDir = strings.TrimSpace(dir) }
func (s *Server) SetGatewayVersion(v string)                   { s.gatewayVersion = strings.TrimSpace(v) }
func (s *Server) SetWebUIVersion(v string)                     { s.webuiVersion = strings.TrimSpace(v) }
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
func (s *Server) SetWhatsAppBridge(service *channels.WhatsAppBridgeService, basePath string) {
	s.whatsAppBridge = service
	s.whatsAppBase = strings.TrimSpace(basePath)
}

func (s *Server) handleWhatsAppBridgeWS(w http.ResponseWriter, r *http.Request) {
	if s.whatsAppBridge == nil {
		http.Error(w, "whatsapp bridge unavailable", http.StatusServiceUnavailable)
		return
	}
	s.whatsAppBridge.ServeWS(w, r)
}

func (s *Server) handleWhatsAppBridgeStatus(w http.ResponseWriter, r *http.Request) {
	if s.whatsAppBridge == nil {
		http.Error(w, "whatsapp bridge unavailable", http.StatusServiceUnavailable)
		return
	}
	s.whatsAppBridge.ServeStatus(w, r)
}

func (s *Server) handleWhatsAppBridgeLogout(w http.ResponseWriter, r *http.Request) {
	if s.whatsAppBridge == nil {
		http.Error(w, "whatsapp bridge unavailable", http.StatusServiceUnavailable)
		return
	}
	s.whatsAppBridge.ServeLogout(w, r)
}

func joinServerRoute(base, endpoint string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" || base == "/" {
		return "/" + strings.TrimPrefix(endpoint, "/")
	}
	return base + "/" + strings.TrimPrefix(endpoint, "/")
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
	if s.mgr == nil {
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/nodes/register", s.handleRegister)
	mux.HandleFunc("/nodes/heartbeat", s.handleHeartbeat)
	mux.HandleFunc("/nodes/connect", s.handleNodeConnect)
	mux.HandleFunc("/", s.handleWebUIAsset)
	mux.HandleFunc("/api/auth/session", s.handleWebUIAuthSession)
	mux.HandleFunc("/api/config", s.handleWebUIConfig)
	mux.HandleFunc("/api/chat", s.handleWebUIChat)
	mux.HandleFunc("/api/chat/history", s.handleWebUIChatHistory)
	mux.HandleFunc("/api/chat/live", s.handleWebUIChatLive)
	mux.HandleFunc("/api/runtime", s.handleWebUIRuntime)
	mux.HandleFunc("/api/world", s.handleWebUIWorld)
	mux.HandleFunc("/api/version", s.handleWebUIVersion)
	mux.HandleFunc("/api/provider/oauth/start", s.handleWebUIProviderOAuthStart)
	mux.HandleFunc("/api/provider/oauth/complete", s.handleWebUIProviderOAuthComplete)
	mux.HandleFunc("/api/provider/oauth/import", s.handleWebUIProviderOAuthImport)
	mux.HandleFunc("/api/provider/oauth/accounts", s.handleWebUIProviderOAuthAccounts)
	mux.HandleFunc("/api/provider/models", s.handleWebUIProviderModels)
	mux.HandleFunc("/api/provider/runtime", s.handleWebUIProviderRuntime)
	mux.HandleFunc("/api/whatsapp/status", s.handleWebUIWhatsAppStatus)
	mux.HandleFunc("/api/whatsapp/logout", s.handleWebUIWhatsAppLogout)
	mux.HandleFunc("/api/whatsapp/qr.svg", s.handleWebUIWhatsAppQR)
	mux.HandleFunc("/api/upload", s.handleWebUIUpload)
	mux.HandleFunc("/api/nodes", s.handleWebUINodes)
	mux.HandleFunc("/api/node_dispatches", s.handleWebUINodeDispatches)
	mux.HandleFunc("/api/node_dispatches/replay", s.handleWebUINodeDispatchReplay)
	mux.HandleFunc("/api/node_artifacts", s.handleWebUINodeArtifacts)
	mux.HandleFunc("/api/node_artifacts/export", s.handleWebUINodeArtifactsExport)
	mux.HandleFunc("/api/node_artifacts/download", s.handleWebUINodeArtifactDownload)
	mux.HandleFunc("/api/node_artifacts/delete", s.handleWebUINodeArtifactDelete)
	mux.HandleFunc("/api/node_artifacts/prune", s.handleWebUINodeArtifactPrune)
	mux.HandleFunc("/api/cron", s.handleWebUICron)
	mux.HandleFunc("/api/skills", s.handleWebUISkills)
	mux.HandleFunc("/api/sessions", s.handleWebUISessions)
	mux.HandleFunc("/api/memory", s.handleWebUIMemory)
	mux.HandleFunc("/api/workspace_file", s.handleWebUIWorkspaceFile)
	mux.HandleFunc("/api/rpc/node", s.handleNodeRPC)
	mux.HandleFunc("/api/rpc/provider", s.handleProviderRPC)
	mux.HandleFunc("/api/rpc/workspace", s.handleWorkspaceRPC)
	mux.HandleFunc("/api/rpc/config", s.handleConfigRPC)
	mux.HandleFunc("/api/rpc/cron", s.handleCronRPC)
	mux.HandleFunc("/api/rpc/skills", s.handleSkillsRPC)
	mux.HandleFunc("/api/runtime_admin", s.handleWebUIRuntimeAdmin)
	mux.HandleFunc("/api/tool_allowlist_groups", s.handleWebUIToolAllowlistGroups)
	mux.HandleFunc("/api/tools", s.handleWebUITools)
	mux.HandleFunc("/api/mcp/install", s.handleWebUIMCPInstall)
	mux.HandleFunc("/api/task_queue", s.handleWebUITaskQueue)
	mux.HandleFunc("/api/ekg_stats", s.handleWebUIEKGStats)
	mux.HandleFunc("/api/logs/live", s.handleWebUILogsLive)
	mux.HandleFunc("/api/logs/recent", s.handleWebUILogsRecent)
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
	base := strings.TrimRight(strings.TrimSpace(s.whatsAppBase), "/")
	if base == "" {
		base = "/whatsapp"
	}
	mux.HandleFunc(base, s.handleWhatsAppBridgeWS)
	mux.HandleFunc(joinServerRoute(base, "ws"), s.handleWhatsAppBridgeWS)
	mux.HandleFunc(joinServerRoute(base, "status"), s.handleWhatsAppBridgeStatus)
	mux.HandleFunc(joinServerRoute(base, "logout"), s.handleWhatsAppBridgeLogout)
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

func requestUsesTLS(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func canonicalOriginHost(host string, https bool) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if parsedHost, parsedPort, err := net.SplitHostPort(host); err == nil {
		return strings.ToLower(net.JoinHostPort(parsedHost, parsedPort))
	}
	port := "80"
	if https {
		port = "443"
	}
	return strings.ToLower(net.JoinHostPort(host, port))
}

func normalizeOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	switch scheme {
	case "http", "https":
	default:
		return ""
	}
	if strings.TrimSpace(u.Host) == "" {
		return ""
	}
	return scheme + "://" + canonicalOriginHost(u.Host, scheme == "https")
}

func requestOrigin(r *http.Request) string {
	if r == nil {
		return ""
	}
	scheme := "http"
	if requestUsesTLS(r) {
		scheme = "https"
	}
	return scheme + "://" + canonicalOriginHost(r.Host, scheme == "https")
}

func sameSiteOrigin(originA, originB string) bool {
	parsedA, err := url.Parse(strings.TrimSpace(originA))
	if err != nil || parsedA == nil {
		return false
	}
	parsedB, err := url.Parse(strings.TrimSpace(originB))
	if err != nil || parsedB == nil {
		return false
	}
	schemeA := strings.ToLower(strings.TrimSpace(parsedA.Scheme))
	schemeB := strings.ToLower(strings.TrimSpace(parsedB.Scheme))
	if schemeA == "" || schemeB == "" || schemeA != schemeB {
		return false
	}
	hostA := strings.ToLower(strings.TrimSpace(parsedA.Hostname()))
	hostB := strings.ToLower(strings.TrimSpace(parsedB.Hostname()))
	return hostA != "" && hostA == hostB
}

func (s *Server) isTrustedOrigin(r *http.Request) bool {
	if r == nil {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	normalizedOrigin := normalizeOrigin(origin)
	if normalizedOrigin == "" {
		return false
	}
	return true
}

func (s *Server) shouldUseCrossSiteCookie(r *http.Request) bool {
	origin := normalizeOrigin(r.Header.Get("Origin"))
	if origin == "" || !s.isTrustedOrigin(r) {
		return false
	}
	reqOrigin := requestOrigin(r)
	if origin == reqOrigin {
		return false
	}
	if sameSiteOrigin(origin, reqOrigin) {
		return false
	}
	return true
}

func (s *Server) websocketUpgrader() *websocket.Upgrader {
	return &websocket.Upgrader{
		CheckOrigin: s.isTrustedOrigin,
	}
}

func (s *Server) isBearerAuthorized(r *http.Request) bool {
	if s == nil || r == nil || strings.TrimSpace(s.token) == "" {
		return false
	}
	return strings.TrimSpace(r.Header.Get("Authorization")) == "Bearer "+s.token
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" {
			if !s.isTrustedOrigin(r) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With")
			w.Header().Set("Access-Control-Expose-Headers", "*")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var n nodes.NodeInfo
	if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	result, rpcErr := s.nodeRPCService().Register(r.Context(), rpcpkg.RegisterNodeRequest{Node: n})
	if rpcErr != nil {
		http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "id": result.ID})
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	result, rpcErr := s.nodeRPCService().Heartbeat(r.Context(), rpcpkg.HeartbeatNodeRequest{ID: body.ID})
	if rpcErr != nil {
		http.Error(w, rpcErr.Message, rpcHTTPStatus(rpcErr))
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "id": result.ID})
}

func (s *Server) checkAuth(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	if s.isBearerAuthorized(r) {
		return true
	}
	if c, err := r.Cookie("clawgo_webui_token"); err == nil && strings.TrimSpace(c.Value) == s.token {
		return true
	}
	return false
}
