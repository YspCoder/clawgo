package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YspCoder/clawgo/pkg/channels"
	cfgpkg "github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/nodes"
	"github.com/YspCoder/clawgo/pkg/providers"
	rpcpkg "github.com/YspCoder/clawgo/pkg/rpc"
	"github.com/YspCoder/clawgo/pkg/tools"
	"github.com/gorilla/websocket"
	"rsc.io/qr"
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
	onSubagents      func(ctx context.Context, action string, args map[string]interface{}) (interface{}, error)
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
	subagentRPCOnce  sync.Once
	subagentRPCReg   *rpcpkg.Registry
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
}

var nodesWebsocketUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
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

type nodeSocketConn struct {
	connID string
	conn   *websocket.Conn
	mu     sync.Mutex
}

func (c *nodeSocketConn) writeJSON(payload interface{}) error {
	if c == nil || c.conn == nil {
		return fmt.Errorf("node websocket unavailable")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.conn.WriteJSON(payload)
}

func (c *nodeSocketConn) Send(msg nodes.WireMessage) error {
	return c.writeJSON(msg)
}

func publishLiveSnapshot(subs map[chan []byte]struct{}, payload []byte) {
	for ch := range subs {
		select {
		case ch <- payload:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- payload:
			default:
			}
		}
	}
}

func (s *Server) subscribeRuntimeLive(ctx context.Context) chan []byte {
	ch := make(chan []byte, 1)
	s.liveRuntimeMu.Lock()
	s.liveRuntimeSubs[ch] = struct{}{}
	start := !s.liveRuntimeOn
	if start {
		s.liveRuntimeOn = true
	}
	s.liveRuntimeMu.Unlock()
	if start {
		go s.runtimeLiveLoop()
	}
	go func() {
		<-ctx.Done()
		s.unsubscribeRuntimeLive(ch)
	}()
	return ch
}

func (s *Server) unsubscribeRuntimeLive(ch chan []byte) {
	s.liveRuntimeMu.Lock()
	delete(s.liveRuntimeSubs, ch)
	s.liveRuntimeMu.Unlock()
}

func (s *Server) runtimeLiveLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if !s.publishRuntimeSnapshot(context.Background()) {
			s.liveRuntimeMu.Lock()
			if len(s.liveRuntimeSubs) == 0 {
				s.liveRuntimeOn = false
				s.liveRuntimeMu.Unlock()
				return
			}
			s.liveRuntimeMu.Unlock()
		}
		<-ticker.C
	}
}

func (s *Server) publishRuntimeSnapshot(ctx context.Context) bool {
	if s == nil {
		return false
	}
	payload := map[string]interface{}{
		"ok":       true,
		"type":     "runtime_snapshot",
		"snapshot": s.buildWebUIRuntimeSnapshot(ctx),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	s.liveRuntimeMu.Lock()
	defer s.liveRuntimeMu.Unlock()
	if len(s.liveRuntimeSubs) == 0 {
		return false
	}
	publishLiveSnapshot(s.liveRuntimeSubs, data)
	return true
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
func (s *Server) SetSubagentHandler(fn func(ctx context.Context, action string, args map[string]interface{}) (interface{}, error)) {
	s.onSubagents = fn
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
func (s *Server) SetNodeWebRTCTransport(t *nodes.WebRTCTransport) {
	s.nodeWebRTC = t
}
func (s *Server) SetNodeP2PStatusHandler(fn func() map[string]interface{}) {
	s.nodeP2PStatus = fn
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

func (s *Server) rememberNodeConnection(nodeID, connID string) {
	nodeID = strings.TrimSpace(nodeID)
	connID = strings.TrimSpace(connID)
	if nodeID == "" || connID == "" {
		return
	}
	s.nodeConnMu.Lock()
	defer s.nodeConnMu.Unlock()
	s.nodeConnIDs[nodeID] = connID
}

func (s *Server) bindNodeSocket(nodeID, connID string, conn *websocket.Conn) {
	nodeID = strings.TrimSpace(nodeID)
	connID = strings.TrimSpace(connID)
	if nodeID == "" || connID == "" || conn == nil {
		return
	}
	next := &nodeSocketConn{connID: connID, conn: conn}
	s.nodeConnMu.Lock()
	prev := s.nodeSockets[nodeID]
	s.nodeSockets[nodeID] = next
	s.nodeConnMu.Unlock()
	if s.mgr != nil {
		s.mgr.RegisterWireSender(nodeID, next)
	}
	if s.nodeWebRTC != nil {
		s.nodeWebRTC.BindSignaler(nodeID, next)
	}
	if prev != nil && prev.connID != connID {
		_ = prev.conn.Close()
	}
}

func (s *Server) releaseNodeConnection(nodeID, connID string) bool {
	nodeID = strings.TrimSpace(nodeID)
	connID = strings.TrimSpace(connID)
	if nodeID == "" || connID == "" {
		return false
	}
	s.nodeConnMu.Lock()
	defer s.nodeConnMu.Unlock()
	if s.nodeConnIDs[nodeID] != connID {
		return false
	}
	delete(s.nodeConnIDs, nodeID)
	if sock := s.nodeSockets[nodeID]; sock != nil && sock.connID == connID {
		delete(s.nodeSockets, nodeID)
	}
	if s.mgr != nil {
		s.mgr.RegisterWireSender(nodeID, nil)
	}
	if s.nodeWebRTC != nil {
		s.nodeWebRTC.UnbindSignaler(nodeID)
	}
	return true
}

func (s *Server) getNodeSocket(nodeID string) *nodeSocketConn {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil
	}
	s.nodeConnMu.Lock()
	defer s.nodeConnMu.Unlock()
	return s.nodeSockets[nodeID]
}

func (s *Server) sendNodeSocketMessage(nodeID string, msg nodes.WireMessage) error {
	sock := s.getNodeSocket(nodeID)
	if sock == nil || sock.conn == nil {
		return fmt.Errorf("node %s not connected", strings.TrimSpace(nodeID))
	}
	return sock.writeJSON(msg)
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
	mux.HandleFunc("/api/config", s.handleWebUIConfig)
	mux.HandleFunc("/api/chat", s.handleWebUIChat)
	mux.HandleFunc("/api/chat/history", s.handleWebUIChatHistory)
	mux.HandleFunc("/api/chat/live", s.handleWebUIChatLive)
	mux.HandleFunc("/api/runtime", s.handleWebUIRuntime)
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
	mux.HandleFunc("/api/rpc/subagent", s.handleSubagentRPC)
	mux.HandleFunc("/api/rpc/node", s.handleNodeRPC)
	mux.HandleFunc("/api/rpc/provider", s.handleProviderRPC)
	mux.HandleFunc("/api/rpc/workspace", s.handleWorkspaceRPC)
	mux.HandleFunc("/api/rpc/config", s.handleConfigRPC)
	mux.HandleFunc("/api/rpc/cron", s.handleCronRPC)
	mux.HandleFunc("/api/subagents_runtime", s.handleWebUISubagentsRuntime)
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

func (s *Server) withCORS(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With")
		w.Header().Set("Access-Control-Expose-Headers", "*")
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

func (s *Server) handleNodeConnect(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.mgr == nil {
		http.Error(w, "nodes manager unavailable", http.StatusInternalServerError)
		return
	}
	conn, err := nodesWebsocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var connectedID string
	connID := fmt.Sprintf("%d", time.Now().UnixNano())
	_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	})

	writeAck := func(ack nodes.WireAck) error {
		if strings.TrimSpace(connectedID) != "" {
			if sock := s.getNodeSocket(connectedID); sock != nil && sock.connID == connID && sock.conn == conn {
				return sock.writeJSON(ack)
			}
		}
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(ack)
	}

	defer func() {
		if strings.TrimSpace(connectedID) != "" && s.releaseNodeConnection(connectedID, connID) {
			s.mgr.MarkOffline(connectedID)
		}
	}()

	for {
		var msg nodes.WireMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		if s.mgr != nil && s.mgr.HandleWireMessage(msg) {
			continue
		}
		type nodeSocketHandler func(nodes.WireMessage) bool
		handlers := map[string]nodeSocketHandler{
			"register": func(msg nodes.WireMessage) bool {
				if msg.Node == nil || strings.TrimSpace(msg.Node.ID) == "" {
					_ = writeAck(nodes.WireAck{OK: false, Type: "register", Error: "node.id required"})
					return true
				}
				s.mgr.Upsert(*msg.Node)
				connectedID = strings.TrimSpace(msg.Node.ID)
				s.rememberNodeConnection(connectedID, connID)
				s.bindNodeSocket(connectedID, connID, conn)
				return writeAck(nodes.WireAck{OK: true, Type: "registered", ID: connectedID}) == nil
			},
			"heartbeat": func(msg nodes.WireMessage) bool {
				id := strings.TrimSpace(msg.ID)
				if id == "" {
					id = connectedID
				}
				if id == "" {
					_ = writeAck(nodes.WireAck{OK: false, Type: "heartbeat", Error: "id required"})
					return true
				}
				if msg.Node != nil && strings.TrimSpace(msg.Node.ID) != "" {
					s.mgr.Upsert(*msg.Node)
					connectedID = strings.TrimSpace(msg.Node.ID)
					s.rememberNodeConnection(connectedID, connID)
					s.bindNodeSocket(connectedID, connID, conn)
				} else if n, ok := s.mgr.Get(id); ok {
					s.mgr.Upsert(n)
					connectedID = id
					s.rememberNodeConnection(connectedID, connID)
					s.bindNodeSocket(connectedID, connID, conn)
				} else {
					_ = writeAck(nodes.WireAck{OK: false, Type: "heartbeat", ID: id, Error: "node not found"})
					return true
				}
				return writeAck(nodes.WireAck{OK: true, Type: "heartbeat", ID: connectedID}) == nil
			},
			"signal_offer":     func(msg nodes.WireMessage) bool { return s.handleNodeSignalMessage(msg, connectedID, writeAck) },
			"signal_answer":    func(msg nodes.WireMessage) bool { return s.handleNodeSignalMessage(msg, connectedID, writeAck) },
			"signal_candidate": func(msg nodes.WireMessage) bool { return s.handleNodeSignalMessage(msg, connectedID, writeAck) },
		}
		if handler := handlers[strings.ToLower(strings.TrimSpace(msg.Type))]; handler != nil {
			if !handler(msg) {
				return
			}
			continue
		}
		if err := writeAck(nodes.WireAck{OK: false, Type: msg.Type, ID: msg.ID, Error: "unsupported message type"}); err != nil {
			return
		}
	}
}

func (s *Server) handleNodeSignalMessage(msg nodes.WireMessage, connectedID string, writeAck func(nodes.WireAck) error) bool {
	targetID := strings.TrimSpace(msg.To)
	if s.nodeWebRTC != nil && (targetID == "" || strings.EqualFold(targetID, "gateway")) {
		if err := s.nodeWebRTC.HandleSignal(msg); err != nil {
			_ = writeAck(nodes.WireAck{OK: false, Type: msg.Type, ID: msg.ID, Error: err.Error()})
			return true
		}
		return writeAck(nodes.WireAck{OK: true, Type: "signaled", ID: msg.ID}) == nil
	}
	if strings.TrimSpace(connectedID) == "" {
		_ = writeAck(nodes.WireAck{OK: false, Type: msg.Type, Error: "node not registered"})
		return true
	}
	if targetID == "" {
		_ = writeAck(nodes.WireAck{OK: false, Type: msg.Type, ID: msg.ID, Error: "target node required"})
		return true
	}
	msg.From = connectedID
	if err := s.sendNodeSocketMessage(targetID, msg); err != nil {
		_ = writeAck(nodes.WireAck{OK: false, Type: msg.Type, ID: msg.ID, Error: err.Error()})
		return true
	}
	return writeAck(nodes.WireAck{OK: true, Type: "relayed", ID: msg.ID}) == nil
}

func (s *Server) handleWebUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.token != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     "clawgo_webui_token",
			Value:    s.token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   86400,
		})
	}
	if s.tryServeWebUIDist(w, r, "/index.html") {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(webUIHTML))
}

func (s *Server) handleWebUIAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == "/" {
		s.handleWebUI(w, r)
		return
	}
	if s.tryServeWebUIDist(w, r, r.URL.Path) {
		return
	}
	// SPA fallback
	if s.tryServeWebUIDist(w, r, "/index.html") {
		return
	}
	http.NotFound(w, r)
}

func (s *Server) tryServeWebUIDist(w http.ResponseWriter, r *http.Request, reqPath string) bool {
	dir := strings.TrimSpace(s.webUIDir)
	if dir == "" {
		return false
	}
	p := strings.TrimPrefix(reqPath, "/")
	if reqPath == "/" || reqPath == "/index.html" {
		p = "index.html"
	}
	p = filepath.Clean(strings.TrimPrefix(p, "/"))
	if strings.HasPrefix(p, "..") {
		return false
	}
	full := filepath.Join(dir, p)
	fi, err := os.Stat(full)
	if err != nil || fi.IsDir() {
		return false
	}
	http.ServeFile(w, r, full)
	return true
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

func getPathValue(m map[string]interface{}, path string) interface{} {
	if m == nil || strings.TrimSpace(path) == "" {
		return nil
	}
	parts := strings.Split(path, ".")
	var cur interface{} = m
	for _, p := range parts {
		node, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur = node[p]
	}
	return cur
}

func collectRiskyConfigPaths(oldMap, newMap map[string]interface{}) []string {
	paths := []string{
		"channels.telegram.token",
		"channels.telegram.allow_from",
		"channels.telegram.allow_chats",
		"models.providers.openai.api_base",
		"models.providers.openai.api_key",
		"runtime.providers.openai.api_base",
		"runtime.providers.openai.api_key",
		"gateway.token",
		"gateway.port",
	}
	seen := map[string]bool{}
	for _, path := range paths {
		seen[path] = true
	}
	for _, name := range collectProviderNames(oldMap, newMap) {
		for _, field := range []string{"api_base", "api_key"} {
			path := "models.providers." + name + "." + field
			if !seen[path] {
				paths = append(paths, path)
				seen[path] = true
			}
			normalizedPath := "runtime.providers." + name + "." + field
			if !seen[normalizedPath] {
				paths = append(paths, normalizedPath)
				seen[normalizedPath] = true
			}
		}
	}
	return paths
}

func collectProviderNames(maps ...map[string]interface{}) []string {
	seen := map[string]bool{}
	names := make([]string, 0)
	for _, root := range maps {
		models, _ := root["models"].(map[string]interface{})
		providers, _ := models["providers"].(map[string]interface{})
		for name := range providers {
			if strings.TrimSpace(name) == "" || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
		runtimeMap, _ := root["runtime"].(map[string]interface{})
		runtimeProviders, _ := runtimeMap["providers"].(map[string]interface{})
		for name := range runtimeProviders {
			if strings.TrimSpace(name) == "" || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
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
	if _, err := io.Copy(w, resp.Body); err != nil {
		return
	}
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
	qrCode = strings.TrimSpace(qrCode)
	qrImage, err := qr.Encode(qrCode, qr.M)
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
		return map[string]interface{}{
			"ok":    false,
			"error": err.Error(),
		}, http.StatusInternalServerError
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
	cfg, err := cfgpkg.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	return cfg, nil
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

func (s *Server) handleWebUIRuntime(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := nodesWebsocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx := r.Context()
	sub := s.subscribeRuntimeLive(ctx)
	initial := map[string]interface{}{
		"ok":       true,
		"type":     "runtime_snapshot",
		"snapshot": s.buildWebUIRuntimeSnapshot(ctx),
	}
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteJSON(initial); err != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case payload := <-sub:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		}
	}
}

func (s *Server) buildWebUIRuntimeSnapshot(ctx context.Context) map[string]interface{} {
	var providerPayload map[string]interface{}
	var normalizedConfig interface{}
	if strings.TrimSpace(s.configPath) != "" {
		if cfg, err := cfgpkg.LoadConfig(strings.TrimSpace(s.configPath)); err == nil {
			providerPayload = providers.GetProviderRuntimeSnapshot(cfg)
			normalizedConfig = cfg.NormalizedView()
		}
	}
	if providerPayload == nil {
		providerPayload = map[string]interface{}{"items": []interface{}{}}
	}
	runtimePayload := map[string]interface{}{}
	if s.onSubagents != nil {
		if res, err := s.onSubagents(ctx, "snapshot", map[string]interface{}{"limit": 200}); err == nil {
			if m, ok := res.(map[string]interface{}); ok {
				runtimePayload = m
			}
		}
	}
	return map[string]interface{}{
		"version":    s.webUIVersionPayload(),
		"config":     normalizedConfig,
		"runtime":    runtimePayload,
		"nodes":      s.webUINodesPayload(ctx),
		"sessions":   s.webUISessionsPayload(),
		"task_queue": s.webUITaskQueuePayload(false),
		"ekg":        s.webUIEKGSummaryPayload("24h"),
		"providers":  providerPayload,
	}
}

func (s *Server) webUIVersionPayload() map[string]interface{} {
	return map[string]interface{}{
		"gateway_version":   firstNonEmptyString(s.gatewayVersion, gatewayBuildVersion()),
		"webui_version":     firstNonEmptyString(s.webuiVersion, detectWebUIVersion(strings.TrimSpace(s.webUIDir))),
		"compiled_channels": channels.CompiledChannelKeys(),
	}
}

func (s *Server) webUINodesPayload(ctx context.Context) map[string]interface{} {
	list := []nodes.NodeInfo{}
	if s.mgr != nil {
		list = s.mgr.List()
	}
	localRegistry := s.fetchRegistryItems(ctx)
	localAgents := make([]nodes.AgentInfo, 0, len(localRegistry))
	for _, item := range localRegistry {
		agentID := strings.TrimSpace(stringFromMap(item, "agent_id"))
		if agentID == "" {
			continue
		}
		localAgents = append(localAgents, nodes.AgentInfo{
			ID:          agentID,
			DisplayName: strings.TrimSpace(stringFromMap(item, "display_name")),
			Role:        strings.TrimSpace(stringFromMap(item, "role")),
			Type:        strings.TrimSpace(stringFromMap(item, "type")),
			Transport:   fallbackString(strings.TrimSpace(stringFromMap(item, "transport")), "local"),
		})
	}
	host, _ := os.Hostname()
	local := nodes.NodeInfo{
		ID:           "local",
		Name:         "local",
		Endpoint:     "gateway",
		Version:      gatewayBuildVersion(),
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		LastSeenAt:   time.Now(),
		Online:       true,
		Capabilities: nodes.Capabilities{Run: true, Invoke: true, Model: true, Camera: true, Screen: true, Location: true, Canvas: true},
		Actions:      []string{"run", "agent_task", "camera_snap", "camera_clip", "screen_snapshot", "screen_record", "location_get", "canvas_snapshot", "canvas_action"},
		Models:       []string{"local-sim"},
		Agents:       localAgents,
	}
	if strings.TrimSpace(host) != "" {
		local.Name = host
	}
	if ip := detectLocalIP(); ip != "" {
		local.Endpoint = ip
	}
	hostLower := strings.ToLower(strings.TrimSpace(host))
	matched := false
	for i := range list {
		id := strings.ToLower(strings.TrimSpace(list[i].ID))
		name := strings.ToLower(strings.TrimSpace(list[i].Name))
		if id == "local" || name == "local" || (hostLower != "" && name == hostLower) {
			list[i].ID = "local"
			list[i].Online = true
			list[i].Version = local.Version
			if strings.TrimSpace(local.Endpoint) != "" {
				list[i].Endpoint = local.Endpoint
			}
			if strings.TrimSpace(local.Name) != "" {
				list[i].Name = local.Name
			}
			list[i].LastSeenAt = time.Now()
			matched = true
			break
		}
	}
	if !matched {
		list = append([]nodes.NodeInfo{local}, list...)
	}
	p2p := map[string]interface{}{}
	if s.nodeP2PStatus != nil {
		p2p = s.nodeP2PStatus()
	}
	dispatches := s.webUINodesDispatchPayload(12)
	return map[string]interface{}{
		"nodes":              list,
		"trees":              s.buildNodeAgentTrees(ctx, list),
		"p2p":                p2p,
		"dispatches":         dispatches,
		"alerts":             s.webUINodeAlertsPayload(list, p2p, dispatches),
		"artifact_retention": s.artifactStatsSnapshot(),
	}
}

func (s *Server) webUINodeAlertsPayload(nodeList []nodes.NodeInfo, p2p map[string]interface{}, dispatches []map[string]interface{}) []map[string]interface{} {
	alerts := make([]map[string]interface{}, 0)
	for _, node := range nodeList {
		nodeID := strings.TrimSpace(node.ID)
		if nodeID == "" || nodeID == "local" {
			continue
		}
		if !node.Online {
			alerts = append(alerts, map[string]interface{}{
				"severity": "critical",
				"kind":     "node_offline",
				"node":     nodeID,
				"title":    "Node offline",
				"detail":   fmt.Sprintf("node %s is offline", nodeID),
			})
		}
	}
	if sessions, ok := p2p["nodes"].([]map[string]interface{}); ok {
		for _, session := range sessions {
			appendNodeSessionAlert(&alerts, session)
		}
	} else if sessions, ok := p2p["nodes"].([]interface{}); ok {
		for _, raw := range sessions {
			if session, ok := raw.(map[string]interface{}); ok {
				appendNodeSessionAlert(&alerts, session)
			}
		}
	}
	failuresByNode := map[string]int{}
	for _, row := range dispatches {
		nodeID := strings.TrimSpace(fmt.Sprint(row["node"]))
		if nodeID == "" {
			continue
		}
		if ok, _ := tools.MapBoolArg(row, "ok"); ok {
			continue
		}
		failuresByNode[nodeID]++
	}
	for nodeID, count := range failuresByNode {
		if count < 2 {
			continue
		}
		alerts = append(alerts, map[string]interface{}{
			"severity": "warning",
			"kind":     "dispatch_failures",
			"node":     nodeID,
			"title":    "Repeated dispatch failures",
			"detail":   fmt.Sprintf("node %s has %d recent failed dispatches", nodeID, count),
			"count":    count,
		})
	}
	return alerts
}

func appendNodeSessionAlert(alerts *[]map[string]interface{}, session map[string]interface{}) {
	nodeID := strings.TrimSpace(fmt.Sprint(session["node"]))
	if nodeID == "" {
		return
	}
	status := strings.ToLower(strings.TrimSpace(fmt.Sprint(session["status"])))
	retryCount := int(int64Value(session["retry_count"]))
	lastError := strings.TrimSpace(fmt.Sprint(session["last_error"]))
	switch {
	case status == "failed" || status == "closed":
		*alerts = append(*alerts, map[string]interface{}{
			"severity": "critical",
			"kind":     "p2p_session_down",
			"node":     nodeID,
			"title":    "P2P session down",
			"detail":   firstNonEmptyString(lastError, fmt.Sprintf("node %s p2p session is %s", nodeID, status)),
		})
	case retryCount >= 3 || (status == "connecting" && retryCount >= 2):
		*alerts = append(*alerts, map[string]interface{}{
			"severity": "warning",
			"kind":     "p2p_session_unstable",
			"node":     nodeID,
			"title":    "P2P session unstable",
			"detail":   firstNonEmptyString(lastError, fmt.Sprintf("node %s p2p session retry_count=%d", nodeID, retryCount)),
			"count":    retryCount,
		})
	}
}

func int64Value(v interface{}) int64 {
	switch value := v.(type) {
	case int:
		return int64(value)
	case int32:
		return int64(value)
	case int64:
		return value
	case float32:
		return int64(value)
	case float64:
		return int64(value)
	case json.Number:
		if n, err := value.Int64(); err == nil {
			return n
		}
	}
	return 0
}

func (s *Server) webUINodesDispatchPayload(limit int) []map[string]interface{} {
	path := s.memoryFilePath("nodes-dispatch-audit.jsonl")
	if path == "" {
		return []map[string]interface{}{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return []map[string]interface{}{}
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 1 && strings.TrimSpace(lines[0]) == "" {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, limit)
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		row := map[string]interface{}{}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		out = append(out, row)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (s *Server) webUINodeArtifactsPayload(limit int) []map[string]interface{} {
	return s.webUINodeArtifactsPayloadFiltered("", "", "", limit)
}

func (s *Server) readNodeDispatchAuditRows() ([]map[string]interface{}, string) {
	path := s.memoryFilePath("nodes-dispatch-audit.jsonl")
	if path == "" {
		return nil, ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, path
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	rows := make([]map[string]interface{}, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		row := map[string]interface{}{}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows, path
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

func (s *Server) setArtifactStats(summary map[string]interface{}) {
	s.artifactStatsMu.Lock()
	defer s.artifactStatsMu.Unlock()
	if summary == nil {
		s.artifactStats = map[string]interface{}{}
		return
	}
	copySummary := make(map[string]interface{}, len(summary))
	for k, v := range summary {
		copySummary[k] = v
	}
	s.artifactStats = copySummary
}

func (s *Server) artifactStatsSnapshot() map[string]interface{} {
	s.artifactStatsMu.Lock()
	defer s.artifactStatsMu.Unlock()
	out := make(map[string]interface{}, len(s.artifactStats))
	for k, v := range s.artifactStats {
		out[k] = v
	}
	return out
}

func (s *Server) nodeArtifactRetentionConfig() cfgpkg.GatewayNodesArtifactsConfig {
	cfg := cfgpkg.DefaultConfig()
	if strings.TrimSpace(s.configPath) != "" {
		if loaded, err := cfgpkg.LoadConfig(s.configPath); err == nil && loaded != nil {
			cfg = loaded
		}
	}
	return cfg.Gateway.Nodes.Artifacts
}

func (s *Server) applyNodeArtifactRetention() map[string]interface{} {
	retention := s.nodeArtifactRetentionConfig()
	if !retention.Enabled || !retention.PruneOnRead || retention.KeepLatest <= 0 {
		summary := map[string]interface{}{
			"enabled":       retention.Enabled,
			"keep_latest":   retention.KeepLatest,
			"retain_days":   retention.RetainDays,
			"prune_on_read": retention.PruneOnRead,
			"pruned":        0,
			"last_run_at":   time.Now().UTC().Format(time.RFC3339),
		}
		s.setArtifactStats(summary)
		return summary
	}
	items := s.webUINodeArtifactsPayload(0)
	cutoff := time.Time{}
	if retention.RetainDays > 0 {
		cutoff = time.Now().UTC().Add(-time.Duration(retention.RetainDays) * 24 * time.Hour)
	}
	pruned := 0
	prunedByAge := 0
	prunedByCount := 0
	for index, item := range items {
		drop := false
		dropByAge := false
		if !cutoff.IsZero() {
			if tm, err := time.Parse(time.RFC3339, strings.TrimSpace(fmt.Sprint(item["time"]))); err == nil && tm.Before(cutoff) {
				drop = true
				dropByAge = true
			}
		}
		if !drop && index >= retention.KeepLatest {
			drop = true
		}
		if !drop {
			continue
		}
		_, deletedAudit, _ := s.deleteNodeArtifact(strings.TrimSpace(fmt.Sprint(item["id"])))
		if deletedAudit {
			pruned++
			if dropByAge {
				prunedByAge++
			} else {
				prunedByCount++
			}
		}
	}
	summary := map[string]interface{}{
		"enabled":         true,
		"keep_latest":     retention.KeepLatest,
		"retain_days":     retention.RetainDays,
		"prune_on_read":   retention.PruneOnRead,
		"pruned":          pruned,
		"pruned_by_age":   prunedByAge,
		"pruned_by_count": prunedByCount,
		"remaining":       len(s.webUINodeArtifactsPayload(0)),
		"last_run_at":     time.Now().UTC().Format(time.RFC3339),
	}
	s.setArtifactStats(summary)
	return summary
}

func (s *Server) deleteNodeArtifact(id string) (bool, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return false, false, fmt.Errorf("id is required")
	}
	rows, auditPath := s.readNodeDispatchAuditRows()
	if len(rows) == 0 || auditPath == "" {
		return false, false, fmt.Errorf("artifact audit is empty")
	}
	deletedFile := false
	deletedAudit := false
	for rowIndex, row := range rows {
		artifacts, _ := row["artifacts"].([]interface{})
		if len(artifacts) == 0 {
			continue
		}
		nextArtifacts := make([]interface{}, 0, len(artifacts))
		for artifactIndex, raw := range artifacts {
			artifact, ok := raw.(map[string]interface{})
			if !ok {
				nextArtifacts = append(nextArtifacts, raw)
				continue
			}
			if buildNodeArtifactID(row, artifact, artifactIndex) != id {
				nextArtifacts = append(nextArtifacts, artifact)
				continue
			}
			for _, rawPath := range []string{fmt.Sprint(artifact["source_path"]), fmt.Sprint(artifact["path"])} {
				if path := resolveArtifactPath(s.workspacePath, rawPath); path != "" {
					if err := os.Remove(path); err == nil {
						deletedFile = true
						break
					}
				}
			}
			deletedAudit = true
		}
		if deletedAudit {
			row["artifacts"] = nextArtifacts
			row["artifact_count"] = len(nextArtifacts)
			kinds := make([]string, 0, len(nextArtifacts))
			for _, raw := range nextArtifacts {
				if artifact, ok := raw.(map[string]interface{}); ok {
					if kind := strings.TrimSpace(fmt.Sprint(artifact["kind"])); kind != "" {
						kinds = append(kinds, kind)
					}
				}
			}
			if len(kinds) > 0 {
				row["artifact_kinds"] = kinds
			} else {
				delete(row, "artifact_kinds")
			}
			rows[rowIndex] = row
			break
		}
	}
	if !deletedAudit {
		return false, false, fmt.Errorf("artifact not found")
	}
	var buf bytes.Buffer
	for _, row := range rows {
		encoded, err := json.Marshal(row)
		if err != nil {
			continue
		}
		buf.Write(encoded)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(auditPath, buf.Bytes(), 0644); err != nil {
		return deletedFile, false, err
	}
	return deletedFile, true, nil
}

func (s *Server) webUISessionsPayload() map[string]interface{} {
	sessionsDir := filepath.Join(filepath.Dir(s.workspacePath), "agents", "main", "sessions")
	_ = os.MkdirAll(sessionsDir, 0755)
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
	return map[string]interface{}{"sessions": out}
}

func (s *Server) webUITaskQueuePayload(includeHeartbeat bool) map[string]interface{} {
	path := s.memoryFilePath("task-audit.jsonl")
	b, err := os.ReadFile(path)
	lines := []string{}
	if err == nil {
		lines = strings.Split(string(b), "\n")
	}
	type agg struct {
		Last     map[string]interface{}
		Logs     []string
		Attempts int
	}
	m := map[string]*agg{}
	for _, ln := range lines {
		if ln == "" {
			continue
		}
		var row map[string]interface{}
		if err := json.Unmarshal([]byte(ln), &row); err != nil {
			continue
		}
		source := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["source"])))
		if !includeHeartbeat && source == "heartbeat" {
			continue
		}
		id := fmt.Sprintf("%v", row["task_id"])
		if id == "" {
			continue
		}
		if _, ok := m[id]; !ok {
			m[id] = &agg{Last: row, Logs: []string{}, Attempts: 0}
		}
		a := m[id]
		a.Last = row
		a.Attempts++
		if lg := strings.TrimSpace(fmt.Sprintf("%v", row["log"])); lg != "" {
			if len(a.Logs) == 0 || a.Logs[len(a.Logs)-1] != lg {
				a.Logs = append(a.Logs, lg)
				if len(a.Logs) > 20 {
					a.Logs = a.Logs[len(a.Logs)-20:]
				}
			}
		}
	}
	items := make([]map[string]interface{}, 0, len(m))
	running := make([]map[string]interface{}, 0)
	for _, a := range m {
		row := a.Last
		row["logs"] = a.Logs
		row["attempts"] = a.Attempts
		items = append(items, row)
		if fmt.Sprintf("%v", row["status"]) == "running" {
			running = append(running, row)
		}
	}
	queuePath := s.memoryFilePath("task_queue.json")
	if qb, qErr := os.ReadFile(queuePath); qErr == nil {
		var q map[string]interface{}
		if json.Unmarshal(qb, &q) == nil {
			if arr, ok := q["running"].([]interface{}); ok {
				for _, it := range arr {
					if row, ok := it.(map[string]interface{}); ok {
						running = append(running, row)
					}
				}
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return fmt.Sprintf("%v", items[i]["updated_at"]) > fmt.Sprintf("%v", items[j]["updated_at"])
	})
	sort.Slice(running, func(i, j int) bool {
		return fmt.Sprintf("%v", running[i]["updated_at"]) > fmt.Sprintf("%v", running[j]["updated_at"])
	})
	if len(items) > 30 {
		items = items[:30]
	}
	return map[string]interface{}{"items": items, "running": running}
}

func (s *Server) webUIEKGSummaryPayload(window string) map[string]interface{} {
	ekgPath := s.memoryFilePath("ekg-events.jsonl")
	window = strings.ToLower(strings.TrimSpace(window))
	windowDur := 24 * time.Hour
	switch window {
	case "6h":
		windowDur = 6 * time.Hour
	case "24h", "":
		windowDur = 24 * time.Hour
	case "7d":
		windowDur = 7 * 24 * time.Hour
	}
	selectedWindow := window
	if selectedWindow == "" {
		selectedWindow = "24h"
	}
	cutoff := time.Now().UTC().Add(-windowDur)
	rows := s.loadEKGRowsCached(ekgPath, 3000)
	type kv struct {
		Key   string  `json:"key"`
		Score float64 `json:"score,omitempty"`
		Count int     `json:"count,omitempty"`
	}
	providerScore := map[string]float64{}
	providerScoreWorkload := map[string]float64{}
	errSigCount := map[string]int{}
	errSigHeartbeat := map[string]int{}
	errSigWorkload := map[string]int{}
	sourceStats := map[string]int{}
	channelStats := map[string]int{}
	for _, row := range rows {
		ts := strings.TrimSpace(fmt.Sprintf("%v", row["time"]))
		if ts != "" {
			if tm, err := time.Parse(time.RFC3339, ts); err == nil && tm.Before(cutoff) {
				continue
			}
		}
		provider := strings.TrimSpace(fmt.Sprintf("%v", row["provider"]))
		status := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["status"])))
		errSig := strings.TrimSpace(fmt.Sprintf("%v", row["errsig"]))
		source := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["source"])))
		channel := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["channel"])))
		if source == "heartbeat" {
			continue
		}
		if source == "" {
			source = "unknown"
		}
		if channel == "" {
			channel = "unknown"
		}
		sourceStats[source]++
		channelStats[channel]++
		if provider != "" {
			providerScoreWorkload[provider] += 1
			if status == "success" {
				providerScore[provider] += 1
			} else if status == "error" {
				providerScore[provider] -= 2
			}
		}
		if errSig != "" {
			errSigWorkload[errSig]++
			if source == "heartbeat" {
				errSigHeartbeat[errSig]++
			} else if status == "error" {
				errSigCount[errSig]++
			}
		}
	}
	toTopScore := func(m map[string]float64, limit int) []kv {
		out := make([]kv, 0, len(m))
		for k, v := range m {
			out = append(out, kv{Key: k, Score: v})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Score == out[j].Score {
				return out[i].Key < out[j].Key
			}
			return out[i].Score > out[j].Score
		})
		if len(out) > limit {
			out = out[:limit]
		}
		return out
	}
	toTopCount := func(m map[string]int, limit int) []kv {
		out := make([]kv, 0, len(m))
		for k, v := range m {
			out = append(out, kv{Key: k, Count: v})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Count == out[j].Count {
				return out[i].Key < out[j].Key
			}
			return out[i].Count > out[j].Count
		})
		if len(out) > limit {
			out = out[:limit]
		}
		return out
	}
	return map[string]interface{}{
		"ok":                    true,
		"window":                selectedWindow,
		"rows":                  len(rows),
		"provider_top_score":    toTopScore(providerScore, 5),
		"provider_top_workload": toTopCount(mapFromFloatCounts(providerScoreWorkload), 5),
		"errsig_top":            toTopCount(errSigCount, 5),
		"errsig_top_heartbeat":  toTopCount(errSigHeartbeat, 5),
		"errsig_top_workload":   toTopCount(errSigWorkload, 5),
		"source_top":            toTopCount(sourceStats, 5),
		"channel_top":           toTopCount(channelStats, 5),
	}
}

func mapFromFloatCounts(src map[string]float64) map[string]int {
	out := make(map[string]int, len(src))
	for k, v := range src {
		out[k] = int(v)
	}
	return out
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
		return mcpInstallSpec{Installer: "npm", Package: pkgName, AutoInstallSupported: true}
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
		return mcpInstallSpec{Installer: "npm", Package: firstNonFlagArg(args), AutoInstallSupported: firstNonFlagArg(args) != ""}
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

func (s *Server) handleWebUINodes(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		payload := s.webUINodesPayload(r.Context())
		payload["ok"] = true
		writeJSON(w, payload)
	case http.MethodPost:
		var body struct {
			Action string `json:"action"`
			ID     string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		action := strings.ToLower(body.Action)
		if action != "delete" {
			http.Error(w, "unsupported action", http.StatusBadRequest)
			return
		}
		if s.mgr == nil {
			http.Error(w, "nodes manager unavailable", http.StatusInternalServerError)
			return
		}
		id := body.ID
		ok := s.mgr.Remove(id)
		writeJSON(w, map[string]interface{}{"ok": true, "deleted": ok, "id": id})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWebUINodeDispatches(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := queryBoundedPositiveInt(r, "limit", 50, 500)
	writeJSON(w, map[string]interface{}{
		"ok":    true,
		"items": s.webUINodesDispatchPayload(limit),
	})
}

func (s *Server) buildNodeAgentTrees(ctx context.Context, nodeList []nodes.NodeInfo) []map[string]interface{} {
	trees := make([]map[string]interface{}, 0, len(nodeList))
	localRegistry := s.fetchRegistryItems(ctx)
	for _, node := range nodeList {
		nodeID := strings.TrimSpace(node.ID)
		items := []map[string]interface{}{}
		source := "unavailable"
		readonly := true
		if nodeID == "local" {
			items = localRegistry
			source = "local_runtime"
			readonly = false
		} else if remoteItems, err := s.fetchRemoteNodeRegistry(ctx, node); err == nil {
			items = remoteItems
			source = "remote_webui"
		}
		trees = append(trees, map[string]interface{}{
			"node_id":   nodeID,
			"node_name": fallbackNodeName(node),
			"online":    node.Online,
			"source":    source,
			"readonly":  readonly,
			"root":      buildAgentTreeRoot(nodeID, items),
		})
	}
	return trees
}

func (s *Server) fetchRegistryItems(ctx context.Context) []map[string]interface{} {
	if s == nil || s.onSubagents == nil {
		return nil
	}
	result, err := s.onSubagents(ctx, "registry", nil)
	if err != nil {
		return nil
	}
	payload, ok := result.(map[string]interface{})
	if !ok {
		return nil
	}
	rawItems, ok := payload["items"].([]map[string]interface{})
	if ok {
		return rawItems
	}
	list, ok := payload["items"].([]interface{})
	if !ok {
		return nil
	}
	items := make([]map[string]interface{}, 0, len(list))
	for _, item := range list {
		row, ok := item.(map[string]interface{})
		if ok {
			items = append(items, row)
		}
	}
	return items
}

func (s *Server) fetchRemoteNodeRegistry(ctx context.Context, node nodes.NodeInfo) ([]map[string]interface{}, error) {
	baseURL := nodeWebUIBaseURL(node)
	if baseURL == "" {
		return nil, fmt.Errorf("node %s endpoint missing", strings.TrimSpace(node.ID))
	}
	reqURL := baseURL + "/api/config?mode=normalized"
	if tok := strings.TrimSpace(node.Token); tok != "" {
		reqURL += "&token=" + url.QueryEscape(tok)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return s.fetchRemoteNodeRegistryLegacy(ctx, node)
	}
	var payload struct {
		OK        bool                    `json:"ok"`
		Config    cfgpkg.NormalizedConfig `json:"config"`
		RawConfig map[string]interface{}  `json:"raw_config"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return s.fetchRemoteNodeRegistryLegacy(ctx, node)
	}
	items := buildRegistryItemsFromNormalizedConfig(payload.Config)
	if len(items) > 0 {
		return items, nil
	}
	return s.fetchRemoteNodeRegistryLegacy(ctx, node)
}

func (s *Server) fetchRemoteNodeRegistryLegacy(ctx context.Context, node nodes.NodeInfo) ([]map[string]interface{}, error) {
	baseURL := nodeWebUIBaseURL(node)
	if baseURL == "" {
		return nil, fmt.Errorf("node %s endpoint missing", strings.TrimSpace(node.ID))
	}
	reqURL := baseURL + "/api/subagents_runtime?action=registry"
	if tok := strings.TrimSpace(node.Token); tok != "" {
		reqURL += "&token=" + url.QueryEscape(tok)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("remote status %d", resp.StatusCode)
	}
	var payload struct {
		OK     bool `json:"ok"`
		Result struct {
			Items []map[string]interface{} `json:"items"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Result.Items, nil
}

func buildRegistryItemsFromNormalizedConfig(view cfgpkg.NormalizedConfig) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(view.Core.Subagents))
	for agentID, subcfg := range view.Core.Subagents {
		if strings.TrimSpace(agentID) == "" {
			continue
		}
		items = append(items, map[string]interface{}{
			"agent_id":           agentID,
			"enabled":            subcfg.Enabled,
			"type":               "subagent",
			"transport":          fallbackString(strings.TrimSpace(subcfg.RuntimeClass), "local"),
			"node_id":            "",
			"parent_agent_id":    "",
			"notify_main_policy": "final_only",
			"display_name":       "",
			"role":               strings.TrimSpace(subcfg.Role),
			"description":        "",
			"system_prompt_file": strings.TrimSpace(subcfg.Prompt),
			"prompt_file_found":  false,
			"memory_namespace":   "",
			"tool_allowlist":     append([]string(nil), subcfg.ToolAllowlist...),
			"tool_visibility":    map[string]interface{}{},
			"effective_tools":    []string{},
			"inherited_tools":    []string{},
			"routing_keywords":   routeKeywordsForRegistry(view.Runtime.Router.Rules, agentID),
			"managed_by":         "config.json",
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return stringFromMap(items[i], "agent_id") < stringFromMap(items[j], "agent_id")
	})
	return items
}

func routeKeywordsForRegistry(rules []cfgpkg.AgentRouteRule, agentID string) []string {
	agentID = strings.TrimSpace(agentID)
	for _, rule := range rules {
		if strings.TrimSpace(rule.AgentID) == agentID {
			return append([]string(nil), rule.Keywords...)
		}
	}
	return nil
}

func nodeWebUIBaseURL(node nodes.NodeInfo) string {
	endpoint := strings.TrimSpace(node.Endpoint)
	if endpoint == "" || strings.EqualFold(endpoint, "gateway") {
		return ""
	}
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return strings.TrimRight(endpoint, "/")
	}
	return "http://" + strings.TrimRight(endpoint, "/")
}

func fallbackNodeName(node nodes.NodeInfo) string {
	if name := strings.TrimSpace(node.Name); name != "" {
		return name
	}
	if id := strings.TrimSpace(node.ID); id != "" {
		return id
	}
	return "node"
}

func buildAgentTreeRoot(nodeID string, items []map[string]interface{}) map[string]interface{} {
	rootID := "main"
	for _, item := range items {
		if strings.TrimSpace(stringFromMap(item, "type")) == "router" && strings.TrimSpace(stringFromMap(item, "agent_id")) != "" {
			rootID = strings.TrimSpace(stringFromMap(item, "agent_id"))
			break
		}
	}
	nodesByID := make(map[string]map[string]interface{}, len(items)+1)
	for _, item := range items {
		id := strings.TrimSpace(stringFromMap(item, "agent_id"))
		if id == "" {
			continue
		}
		nodesByID[id] = map[string]interface{}{
			"agent_id":        id,
			"display_name":    stringFromMap(item, "display_name"),
			"role":            stringFromMap(item, "role"),
			"type":            stringFromMap(item, "type"),
			"transport":       fallbackString(stringFromMap(item, "transport"), "local"),
			"managed_by":      stringFromMap(item, "managed_by"),
			"node_id":         stringFromMap(item, "node_id"),
			"parent_agent_id": stringFromMap(item, "parent_agent_id"),
			"enabled":         boolFromMap(item, "enabled"),
			"children":        []map[string]interface{}{},
		}
	}
	root, ok := nodesByID[rootID]
	if !ok {
		root = map[string]interface{}{
			"agent_id":     rootID,
			"display_name": "Main Agent",
			"role":         "orchestrator",
			"type":         "router",
			"transport":    "local",
			"managed_by":   "derived",
			"enabled":      true,
			"children":     []map[string]interface{}{},
		}
		nodesByID[rootID] = root
	}
	for _, item := range items {
		id := strings.TrimSpace(stringFromMap(item, "agent_id"))
		if id == "" || id == rootID {
			continue
		}
		parentID := strings.TrimSpace(stringFromMap(item, "parent_agent_id"))
		if parentID == "" {
			parentID = rootID
		}
		parent, ok := nodesByID[parentID]
		if !ok {
			parent = root
		}
		parent["children"] = append(parent["children"].([]map[string]interface{}), nodesByID[id])
	}
	return map[string]interface{}{
		"node_id":   nodeID,
		"agent_id":  root["agent_id"],
		"root":      root,
		"child_cnt": len(root["children"].([]map[string]interface{})),
	}
}

func stringFromMap(item map[string]interface{}, key string) string {
	return tools.MapStringArg(item, key)
}

func boolFromMap(item map[string]interface{}, key string) bool {
	if item == nil {
		return false
	}
	v, _ := tools.MapBoolArg(item, key)
	return v
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

func detectWebUIVersion(webUIDir string) string {
	_ = webUIDir
	return "dev"
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

func resolveClawHubBinary(ctx context.Context) string {
	if p, err := exec.LookPath("clawhub"); err == nil {
		return p
	}
	prefix := strings.TrimSpace(npmGlobalPrefix(ctx))
	if prefix != "" {
		cand := filepath.Join(prefix, "bin", "clawhub")
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	cands := []string{
		"/usr/local/bin/clawhub",
		"/opt/homebrew/bin/clawhub",
		filepath.Join(os.Getenv("HOME"), ".npm-global", "bin", "clawhub"),
	}
	for _, cand := range cands {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	return ""
}

func npmGlobalPrefix(ctx context.Context) string {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "npm", "config", "get", "prefix").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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

func ensureNodeRuntime(ctx context.Context) (string, error) {
	if nodePath, err := exec.LookPath("node"); err == nil {
		if _, err := exec.LookPath("npm"); err == nil {
			if major, verr := detectNodeMajor(ctx, nodePath); verr == nil && major == 22 {
				return "node@22 and npm already installed", nil
			}
		}
	}

	var output []string
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("brew"); err != nil {
			return strings.Join(output, "\n"), fmt.Errorf("nodejs/npm missing and Homebrew not found; please install Homebrew then retry")
		}
		out, err := runInstallCommand(ctx, "brew install node@22 && brew link --overwrite --force node@22")
		if out != "" {
			output = append(output, out)
		}
		if err != nil {
			return strings.Join(output, "\n"), err
		}
	case "linux":
		var out string
		var err error
		switch {
		case commandExists("apt-get"):
			if commandExists("curl") {
				out, err = runInstallCommand(ctx, "curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && apt-get install -y nodejs")
			} else if commandExists("wget") {
				out, err = runInstallCommand(ctx, "wget -qO- https://deb.nodesource.com/setup_22.x | bash - && apt-get install -y nodejs")
			} else {
				err = fmt.Errorf("missing curl/wget required for NodeSource setup_22.x")
			}
		case commandExists("dnf"):
			if commandExists("curl") {
				out, err = runInstallCommand(ctx, "curl -fsSL https://rpm.nodesource.com/setup_22.x | bash - && dnf install -y nodejs")
			} else if commandExists("wget") {
				out, err = runInstallCommand(ctx, "wget -qO- https://rpm.nodesource.com/setup_22.x | bash - && dnf install -y nodejs")
			} else {
				err = fmt.Errorf("missing curl/wget required for NodeSource setup_22.x")
			}
		case commandExists("yum"):
			if commandExists("curl") {
				out, err = runInstallCommand(ctx, "curl -fsSL https://rpm.nodesource.com/setup_22.x | bash - && yum install -y nodejs")
			} else if commandExists("wget") {
				out, err = runInstallCommand(ctx, "wget -qO- https://rpm.nodesource.com/setup_22.x | bash - && yum install -y nodejs")
			} else {
				err = fmt.Errorf("missing curl/wget required for NodeSource setup_22.x")
			}
		case commandExists("pacman"):
			out, err = runInstallCommand(ctx, "pacman -Sy --noconfirm nodejs npm")
		case commandExists("apk"):
			out, err = runInstallCommand(ctx, "apk add --no-cache nodejs npm")
		default:
			return strings.Join(output, "\n"), fmt.Errorf("nodejs/npm missing and no supported package manager found")
		}
		if out != "" {
			output = append(output, out)
		}
		if err != nil {
			return strings.Join(output, "\n"), err
		}
	default:
		return strings.Join(output, "\n"), fmt.Errorf("unsupported OS for auto install: %s", runtime.GOOS)
	}

	if _, err := exec.LookPath("node"); err != nil {
		return strings.Join(output, "\n"), fmt.Errorf("node installation completed but `node` still not found in PATH")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		return strings.Join(output, "\n"), fmt.Errorf("node installation completed but `npm` still not found in PATH")
	}
	nodePath, _ := exec.LookPath("node")
	major, err := detectNodeMajor(ctx, nodePath)
	if err != nil {
		return strings.Join(output, "\n"), fmt.Errorf("failed to detect node major version: %w", err)
	}
	if major != 22 {
		return strings.Join(output, "\n"), fmt.Errorf("node version is %d, expected 22", major)
	}
	output = append(output, "node@22/npm installed")
	return strings.Join(output, "\n"), nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func detectNodeMajor(ctx context.Context, nodePath string) (int, error) {
	nodePath = strings.TrimSpace(nodePath)
	if nodePath == "" {
		return 0, fmt.Errorf("node path empty")
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, nodePath, "-p", "process.versions.node.split('.')[0]").Output()
	if err != nil {
		return 0, err
	}
	majorStr := strings.TrimSpace(string(out))
	if majorStr == "" {
		return 0, fmt.Errorf("empty node major version")
	}
	v, err := strconv.Atoi(majorStr)
	if err != nil {
		return 0, err
	}
	return v, nil
}

func ensureMCPPackageInstalled(ctx context.Context, pkgName string) (output string, binName string, binPath string, err error) {
	return ensureMCPPackageInstalledWithInstaller(ctx, pkgName, "npm")
}

func ensureMCPPackageInstalledWithInstaller(ctx context.Context, pkgName, installer string) (output string, binName string, binPath string, err error) {
	pkgName = strings.TrimSpace(pkgName)
	if pkgName == "" {
		return "", "", "", fmt.Errorf("package empty")
	}
	installer = strings.ToLower(strings.TrimSpace(installer))
	if installer == "" {
		installer = "npm"
	}
	outs := make([]string, 0, 4)
	switch installer {
	case "npm":
		nodeOut, err := ensureNodeRuntime(ctx)
		if nodeOut != "" {
			outs = append(outs, nodeOut)
		}
		if err != nil {
			return strings.Join(outs, "\n"), "", "", err
		}
		installOut, err := runInstallCommand(ctx, "npm i -g "+shellEscapeArg(pkgName))
		if installOut != "" {
			outs = append(outs, installOut)
		}
		if err != nil {
			return strings.Join(outs, "\n"), "", "", err
		}
		binName, err = resolveNpmPackageBin(ctx, pkgName)
		if err != nil {
			return strings.Join(outs, "\n"), "", "", err
		}
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

func resolveNpmPackageBin(ctx context.Context, pkgName string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "npm", "view", pkgName, "bin", "--json")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to query npm bin for %s: %w", pkgName, err)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "null" {
		return "", fmt.Errorf("npm package %s does not expose a bin", pkgName)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(out, &obj); err == nil && len(obj) > 0 {
		keys := make([]string, 0, len(obj))
		for key := range obj {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return keys[0], nil
	}
	var text string
	if err := json.Unmarshal(out, &text); err == nil && strings.TrimSpace(text) != "" {
		return strings.TrimSpace(text), nil
	}
	return "", fmt.Errorf("unable to resolve bin for npm package %s", pkgName)
}

func resolveInstalledBinary(ctx context.Context, binName string) string {
	binName = strings.TrimSpace(binName)
	if binName == "" {
		return ""
	}
	if p, err := exec.LookPath(binName); err == nil {
		return p
	}
	prefix := strings.TrimSpace(npmGlobalPrefix(ctx))
	if prefix != "" {
		cand := filepath.Join(prefix, "bin", binName)
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	cands := []string{
		filepath.Join("/usr/local/bin", binName),
		filepath.Join("/opt/homebrew/bin", binName),
		filepath.Join(os.Getenv("HOME"), ".npm-global", "bin", binName),
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

func derefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
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
	case strings.HasPrefix(k, "node:"):
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

func (s *Server) handleWebUITaskQueue(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := s.memoryFilePath("task-audit.jsonl")
	includeHeartbeat := r.URL.Query().Get("include_heartbeat") == "1"
	b, err := os.ReadFile(path)
	lines := []string{}
	if err == nil {
		lines = strings.Split(string(b), "\n")
	}
	type agg struct {
		Last     map[string]interface{}
		Logs     []string
		Attempts int
	}
	m := map[string]*agg{}
	for _, ln := range lines {
		if ln == "" {
			continue
		}
		var row map[string]interface{}
		if err := json.Unmarshal([]byte(ln), &row); err != nil {
			continue
		}
		source := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["source"])))
		if !includeHeartbeat && source == "heartbeat" {
			continue
		}
		id := fmt.Sprintf("%v", row["task_id"])
		if id == "" {
			continue
		}
		if _, ok := m[id]; !ok {
			m[id] = &agg{Last: row, Logs: []string{}, Attempts: 0}
		}
		a := m[id]
		a.Last = row
		a.Attempts++
		if lg := strings.TrimSpace(fmt.Sprintf("%v", row["log"])); lg != "" {
			if len(a.Logs) == 0 || a.Logs[len(a.Logs)-1] != lg {
				a.Logs = append(a.Logs, lg)
				if len(a.Logs) > 20 {
					a.Logs = a.Logs[len(a.Logs)-20:]
				}
			}
		}
	}
	items := make([]map[string]interface{}, 0, len(m))
	running := make([]map[string]interface{}, 0)
	for _, a := range m {
		row := a.Last
		row["logs"] = a.Logs
		row["attempts"] = a.Attempts
		items = append(items, row)
		if fmt.Sprintf("%v", row["status"]) == "running" {
			running = append(running, row)
		}
	}

	// Merge command watchdog queue from memory/task_queue.json for visibility.
	queuePath := s.memoryFilePath("task_queue.json")
	if qb, qErr := os.ReadFile(queuePath); qErr == nil {
		var q map[string]interface{}
		if json.Unmarshal(qb, &q) == nil {
			if arr, ok := q["running"].([]interface{}); ok {
				for _, item := range arr {
					row, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					id := fmt.Sprintf("%v", row["id"])
					if strings.TrimSpace(id) == "" {
						continue
					}
					label := fmt.Sprintf("%v", row["label"])
					source := strings.TrimSpace(fmt.Sprintf("%v", row["source"]))
					if source == "" {
						source = "task_watchdog"
					}
					rec := map[string]interface{}{
						"task_id":       "cmd:" + id,
						"time":          fmt.Sprintf("%v", row["started_at"]),
						"status":        "running",
						"source":        "task_watchdog",
						"channel":       source,
						"session":       "watchdog:" + id,
						"input_preview": label,
						"duration_ms":   0,
						"attempts":      1,
						"retry_count":   0,
						"logs": []string{
							fmt.Sprintf("watchdog source=%s heavy=%v", source, row["heavy"]),
							fmt.Sprintf("next_check_at=%v stalled_rounds=%v/%v", row["next_check_at"], row["stalled_rounds"], row["stall_round_limit"]),
						},
						"idle_run": true,
					}
					items = append(items, rec)
					running = append(running, rec)
				}
			}
			if arr, ok := q["waiting"].([]interface{}); ok {
				for _, item := range arr {
					row, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					id := fmt.Sprintf("%v", row["id"])
					if strings.TrimSpace(id) == "" {
						continue
					}
					label := fmt.Sprintf("%v", row["label"])
					source := strings.TrimSpace(fmt.Sprintf("%v", row["source"]))
					if source == "" {
						source = "task_watchdog"
					}
					rec := map[string]interface{}{
						"task_id":       "cmd:" + id,
						"time":          fmt.Sprintf("%v", row["enqueued_at"]),
						"status":        "waiting",
						"source":        "task_watchdog",
						"channel":       source,
						"session":       "watchdog:" + id,
						"input_preview": label,
						"duration_ms":   0,
						"attempts":      1,
						"retry_count":   0,
						"logs": []string{
							fmt.Sprintf("watchdog source=%s heavy=%v", source, row["heavy"]),
							fmt.Sprintf("enqueued_at=%v", row["enqueued_at"]),
						},
						"idle_run": true,
					}
					items = append(items, rec)
				}
			}
			if wd, ok := q["watchdog"].(map[string]interface{}); ok {
				items = append(items, map[string]interface{}{
					"task_id":       "cmd:watchdog",
					"time":          fmt.Sprintf("%v", q["time"]),
					"status":        "running",
					"source":        "task_watchdog",
					"channel":       "watchdog",
					"session":       "watchdog:stats",
					"input_preview": "task watchdog capacity snapshot",
					"duration_ms":   0,
					"attempts":      1,
					"retry_count":   0,
					"logs": []string{
						fmt.Sprintf("cpu_total=%v usage_ratio=%v reserve_pct=%v", wd["cpu_total"], wd["usage_ratio"], wd["reserve_pct"]),
						fmt.Sprintf("active=%v/%v heavy=%v/%v waiting=%v running=%v", wd["active"], wd["max_active"], wd["active_heavy"], wd["max_heavy"], wd["waiting"], wd["running"]),
					},
					"idle_run": true,
				})
			}
		}
	}

	sort.Slice(items, func(i, j int) bool { return fmt.Sprintf("%v", items[i]["time"]) > fmt.Sprintf("%v", items[j]["time"]) })
	stats := map[string]int{"total": len(items), "running": len(running)}
	writeJSON(w, map[string]interface{}{"ok": true, "running": running, "items": items, "stats": stats})
}

func (s *Server) loadEKGRowsCached(path string, maxLines int) []map[string]interface{} {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	s.ekgCacheMu.Lock()
	defer s.ekgCacheMu.Unlock()
	if s.ekgCachePath == path && s.ekgCacheSize == fi.Size() && s.ekgCacheStamp.Equal(fi.ModTime()) && len(s.ekgCacheRows) > 0 {
		return s.ekgCacheRows
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	rows := make([]map[string]interface{}, 0, len(lines))
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		var row map[string]interface{}
		if json.Unmarshal([]byte(ln), &row) == nil {
			rows = append(rows, row)
		}
	}
	s.ekgCachePath = path
	s.ekgCacheSize = fi.Size()
	s.ekgCacheStamp = fi.ModTime()
	s.ekgCacheRows = rows
	return rows
}

func (s *Server) handleWebUIEKGStats(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ekgPath := s.memoryFilePath("ekg-events.jsonl")
	window := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("window")))
	windowDur := 24 * time.Hour
	switch window {
	case "6h":
		windowDur = 6 * time.Hour
	case "24h", "":
		windowDur = 24 * time.Hour
	case "7d":
		windowDur = 7 * 24 * time.Hour
	}
	selectedWindow := window
	if selectedWindow == "" {
		selectedWindow = "24h"
	}
	cutoff := time.Now().UTC().Add(-windowDur)
	rows := s.loadEKGRowsCached(ekgPath, 3000)
	type kv struct {
		Key   string  `json:"key"`
		Score float64 `json:"score,omitempty"`
		Count int     `json:"count,omitempty"`
	}
	providerScore := map[string]float64{}
	providerScoreWorkload := map[string]float64{}
	errSigCount := map[string]int{}
	errSigHeartbeat := map[string]int{}
	errSigWorkload := map[string]int{}
	sourceStats := map[string]int{}
	channelStats := map[string]int{}
	for _, row := range rows {
		ts := strings.TrimSpace(fmt.Sprintf("%v", row["time"]))
		if ts != "" {
			if tm, err := time.Parse(time.RFC3339, ts); err == nil {
				if tm.Before(cutoff) {
					continue
				}
			}
		}
		provider := strings.TrimSpace(fmt.Sprintf("%v", row["provider"]))
		status := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["status"])))
		errSig := strings.TrimSpace(fmt.Sprintf("%v", row["errsig"]))
		source := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["source"])))
		channel := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["channel"])))
		if source == "heartbeat" {
			continue
		}
		if source == "" {
			source = "unknown"
		}
		if channel == "" {
			channel = "unknown"
		}
		sourceStats[source]++
		channelStats[channel]++
		if provider != "" {
			switch status {
			case "success":
				providerScore[provider] += 1
				providerScoreWorkload[provider] += 1
			case "suppressed":
				providerScore[provider] += 0.2
				providerScoreWorkload[provider] += 0.2
			case "error":
				providerScore[provider] -= 1
				providerScoreWorkload[provider] -= 1
			}
		}
		if errSig != "" && status == "error" {
			errSigCount[errSig]++
			errSigWorkload[errSig]++
		}
	}
	toTopScore := func(m map[string]float64, n int) []kv {
		out := make([]kv, 0, len(m))
		for k, v := range m {
			out = append(out, kv{Key: k, Score: v})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
		if len(out) > n {
			out = out[:n]
		}
		return out
	}
	toTopCount := func(m map[string]int, n int) []kv {
		out := make([]kv, 0, len(m))
		for k, v := range m {
			out = append(out, kv{Key: k, Count: v})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
		if len(out) > n {
			out = out[:n]
		}
		return out
	}
	writeJSON(w, map[string]interface{}{
		"ok":                    true,
		"window":                selectedWindow,
		"provider_top":          toTopScore(providerScore, 5),
		"provider_top_workload": toTopScore(providerScoreWorkload, 5),
		"errsig_top":            toTopCount(errSigCount, 5),
		"errsig_top_heartbeat":  toTopCount(errSigHeartbeat, 5),
		"errsig_top_workload":   toTopCount(errSigWorkload, 5),
		"source_stats":          sourceStats,
		"channel_stats":         channelStats,
		"escalation_count":      0,
	})
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
	conn, err := nodesWebsocketUpgrader.Upgrade(w, r, nil)
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
		{"path": "channels.*", "name": "Channels", "description": "Telegram and other channel settings"},
		{"path": "cron.*", "name": "Cron", "description": "Global cron runtime settings"},
		{"path": "agents.defaults.heartbeat.*", "name": "Heartbeat", "description": "Heartbeat interval and prompt template"},
		{"path": "gateway.*", "name": "Gateway", "description": "Mostly hot-reloadable; host/port may require restart"},
	}
}

const webUIHTML = `<!doctype html>
<html><head><meta charset="utf-8"/><meta name="viewport" content="width=device-width,initial-scale=1"/>
<title>ClawGo WebUI</title>
<style>body{font-family:system-ui;margin:20px;max-width:980px}textarea{width:100%;min-height:220px}#chatlog{white-space:pre-wrap;border:1px solid #ddd;padding:12px;min-height:180px}</style>
</head><body>
<h2>ClawGo WebUI</h2>
<p>Token: <input id="token" placeholder="gateway token" style="width:320px"/></p>
<h3>Config (dynamic + hot reload)</h3>
<button onclick="loadCfg()">Load Config</button>
<button onclick="saveCfg()">Save + Reload</button>
<textarea id="cfg"></textarea>
<h3>Chat (supports media upload)</h3>
<div>Session: <input id="session" value="webui:default"/> <input id="msg" placeholder="message" style="width:420px"/> <input id="file" type="file"/> <button onclick="sendChat()">Send</button></div>
<div id="chatlog"></div>
<script>
function auth(){const t=document.getElementById('token').value.trim();return t?('?token='+encodeURIComponent(t)):''}
async function loadCfg(){let r=await fetch('/api/config'+auth());document.getElementById('cfg').value=await r.text()}
async function saveCfg(){let j=JSON.parse(document.getElementById('cfg').value);let r=await fetch('/api/config'+auth(),{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(j)});alert(await r.text())}
async function sendChat(){
 let media='';const f=document.getElementById('file').files[0];
 if(f){let fd=new FormData();fd.append('file',f);let ur=await fetch('/api/upload'+auth(),{method:'POST',body:fd});let uj=await ur.json();media=uj.path||''}
 const payload={session:document.getElementById('session').value,message:document.getElementById('msg').value,media};
 let r=await fetch('/api/chat'+auth(),{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)});let t=await r.text();
 document.getElementById('chatlog').textContent += '\nUSER> '+payload.message+(media?(' [file:'+media+']'):'')+'\nBOT> '+t+'\n';
}
loadCfg();
</script></body></html>`
