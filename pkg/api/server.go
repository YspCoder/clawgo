package api

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
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
	"github.com/YspCoder/clawgo/pkg/tools"
	"github.com/gorilla/websocket"
	"rsc.io/qr"
)

type Server struct {
	addr            string
	token           string
	mgr             *nodes.Manager
	server          *http.Server
	nodeConnMu      sync.Mutex
	nodeConnIDs     map[string]string
	nodeSockets     map[string]*nodeSocketConn
	nodeWebRTC      *nodes.WebRTCTransport
	nodeP2PStatus   func() map[string]interface{}
	artifactStatsMu sync.Mutex
	artifactStats   map[string]interface{}
	gatewayVersion  string
	webuiVersion    string
	configPath      string
	workspacePath   string
	logFilePath     string
	onChat          func(ctx context.Context, sessionKey, content string) (string, error)
	onChatHistory   func(sessionKey string) []map[string]interface{}
	onConfigAfter   func() error
	onCron          func(action string, args map[string]interface{}) (interface{}, error)
	onSubagents     func(ctx context.Context, action string, args map[string]interface{}) (interface{}, error)
	onNodeDispatch  func(ctx context.Context, req nodes.Request, mode string) (nodes.Response, error)
	onToolsCatalog  func() interface{}
	webUIDir        string
	ekgCacheMu      sync.Mutex
	ekgCachePath    string
	ekgCacheStamp   time.Time
	ekgCacheSize    int64
	ekgCacheRows    []map[string]interface{}
	liveRuntimeMu   sync.Mutex
	liveRuntimeSubs map[chan []byte]struct{}
	liveRuntimeOn   bool
	liveSubagentMu  sync.Mutex
	liveSubagents   map[string]*liveSubagentGroup
	whatsAppBridge  *channels.WhatsAppBridgeService
	whatsAppBase    string
	oauthFlowMu     sync.Mutex
	oauthFlows      map[string]*providers.OAuthPendingFlow
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
		liveSubagents:   map[string]*liveSubagentGroup{},
		oauthFlows:      map[string]*providers.OAuthPendingFlow{},
	}
}

type nodeSocketConn struct {
	connID string
	conn   *websocket.Conn
	mu     sync.Mutex
}

type liveSubagentGroup struct {
	taskID        string
	previewTaskID string
	subs          map[chan []byte]struct{}
	stopCh        chan struct{}
}

func (c *nodeSocketConn) Send(msg nodes.WireMessage) error {
	if c == nil || c.conn == nil {
		return fmt.Errorf("node websocket unavailable")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.conn.WriteJSON(msg)
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

func buildSubagentLiveKey(taskID, previewTaskID string) string {
	return strings.TrimSpace(taskID) + "\x00" + strings.TrimSpace(previewTaskID)
}

func (s *Server) subscribeSubagentLive(ctx context.Context, taskID, previewTaskID string) chan []byte {
	ch := make(chan []byte, 1)
	key := buildSubagentLiveKey(taskID, previewTaskID)
	s.liveSubagentMu.Lock()
	group := s.liveSubagents[key]
	if group == nil {
		group = &liveSubagentGroup{
			taskID:        strings.TrimSpace(taskID),
			previewTaskID: strings.TrimSpace(previewTaskID),
			subs:          map[chan []byte]struct{}{},
			stopCh:        make(chan struct{}),
		}
		s.liveSubagents[key] = group
		go s.subagentLiveLoop(key, group)
	}
	group.subs[ch] = struct{}{}
	s.liveSubagentMu.Unlock()
	go func() {
		<-ctx.Done()
		s.unsubscribeSubagentLive(key, ch)
	}()
	return ch
}

func (s *Server) unsubscribeSubagentLive(key string, ch chan []byte) {
	s.liveSubagentMu.Lock()
	group := s.liveSubagents[key]
	if group == nil {
		s.liveSubagentMu.Unlock()
		return
	}
	delete(group.subs, ch)
	if len(group.subs) == 0 {
		delete(s.liveSubagents, key)
		close(group.stopCh)
	}
	s.liveSubagentMu.Unlock()
}

func (s *Server) subagentLiveLoop(key string, group *liveSubagentGroup) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if !s.publishSubagentLiveSnapshot(context.Background(), key, group.taskID, group.previewTaskID) {
			return
		}
		select {
		case <-group.stopCh:
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) publishSubagentLiveSnapshot(ctx context.Context, key, taskID, previewTaskID string) bool {
	if s == nil {
		return false
	}
	payload := map[string]interface{}{
		"ok":      true,
		"type":    "subagents_live",
		"payload": s.buildSubagentsLivePayload(ctx, taskID, previewTaskID),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	s.liveSubagentMu.Lock()
	defer s.liveSubagentMu.Unlock()
	group := s.liveSubagents[key]
	if group == nil || len(group.subs) == 0 {
		return false
	}
	publishLiveSnapshot(group.subs, data)
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
	sock.mu.Lock()
	defer sock.mu.Unlock()
	_ = sock.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return sock.conn.WriteJSON(msg)
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
	mux.HandleFunc("/webui", s.handleWebUI)
	mux.HandleFunc("/webui/", s.handleWebUIAsset)
	mux.HandleFunc("/webui/api/config", s.handleWebUIConfig)
	mux.HandleFunc("/webui/api/chat", s.handleWebUIChat)
	mux.HandleFunc("/webui/api/chat/history", s.handleWebUIChatHistory)
	mux.HandleFunc("/webui/api/chat/stream", s.handleWebUIChatStream)
	mux.HandleFunc("/webui/api/chat/live", s.handleWebUIChatLive)
	mux.HandleFunc("/webui/api/runtime", s.handleWebUIRuntime)
	mux.HandleFunc("/webui/api/version", s.handleWebUIVersion)
	mux.HandleFunc("/webui/api/provider/oauth/start", s.handleWebUIProviderOAuthStart)
	mux.HandleFunc("/webui/api/provider/oauth/complete", s.handleWebUIProviderOAuthComplete)
	mux.HandleFunc("/webui/api/provider/oauth/import", s.handleWebUIProviderOAuthImport)
	mux.HandleFunc("/webui/api/provider/oauth/accounts", s.handleWebUIProviderOAuthAccounts)
	mux.HandleFunc("/webui/api/provider/models", s.handleWebUIProviderModels)
	mux.HandleFunc("/webui/api/provider/runtime", s.handleWebUIProviderRuntime)
	mux.HandleFunc("/webui/api/provider/runtime/summary", s.handleWebUIProviderRuntimeSummary)
	mux.HandleFunc("/webui/api/whatsapp/status", s.handleWebUIWhatsAppStatus)
	mux.HandleFunc("/webui/api/whatsapp/logout", s.handleWebUIWhatsAppLogout)
	mux.HandleFunc("/webui/api/whatsapp/qr.svg", s.handleWebUIWhatsAppQR)
	mux.HandleFunc("/webui/api/upload", s.handleWebUIUpload)
	mux.HandleFunc("/webui/api/nodes", s.handleWebUINodes)
	mux.HandleFunc("/webui/api/node_dispatches", s.handleWebUINodeDispatches)
	mux.HandleFunc("/webui/api/node_dispatches/replay", s.handleWebUINodeDispatchReplay)
	mux.HandleFunc("/webui/api/node_artifacts", s.handleWebUINodeArtifacts)
	mux.HandleFunc("/webui/api/node_artifacts/export", s.handleWebUINodeArtifactsExport)
	mux.HandleFunc("/webui/api/node_artifacts/download", s.handleWebUINodeArtifactDownload)
	mux.HandleFunc("/webui/api/node_artifacts/delete", s.handleWebUINodeArtifactDelete)
	mux.HandleFunc("/webui/api/node_artifacts/prune", s.handleWebUINodeArtifactPrune)
	mux.HandleFunc("/webui/api/cron", s.handleWebUICron)
	mux.HandleFunc("/webui/api/skills", s.handleWebUISkills)
	mux.HandleFunc("/webui/api/sessions", s.handleWebUISessions)
	mux.HandleFunc("/webui/api/memory", s.handleWebUIMemory)
	mux.HandleFunc("/webui/api/subagent_profiles", s.handleWebUISubagentProfiles)
	mux.HandleFunc("/webui/api/subagents_runtime", s.handleWebUISubagentsRuntime)
	mux.HandleFunc("/webui/api/subagents_runtime/live", s.handleWebUISubagentsRuntimeLive)
	mux.HandleFunc("/webui/api/tool_allowlist_groups", s.handleWebUIToolAllowlistGroups)
	mux.HandleFunc("/webui/api/tools", s.handleWebUITools)
	mux.HandleFunc("/webui/api/mcp/install", s.handleWebUIMCPInstall)
	mux.HandleFunc("/webui/api/task_audit", s.handleWebUITaskAudit)
	mux.HandleFunc("/webui/api/task_queue", s.handleWebUITaskQueue)
	mux.HandleFunc("/webui/api/ekg_stats", s.handleWebUIEKGStats)
	mux.HandleFunc("/webui/api/exec_approvals", s.handleWebUIExecApprovals)
	mux.HandleFunc("/webui/api/logs/stream", s.handleWebUILogsStream)
	mux.HandleFunc("/webui/api/logs/live", s.handleWebUILogsLive)
	mux.HandleFunc("/webui/api/logs/recent", s.handleWebUILogsRecent)
	base := strings.TrimRight(strings.TrimSpace(s.whatsAppBase), "/")
	if base == "" {
		base = "/whatsapp"
	}
	mux.HandleFunc(base, s.handleWhatsAppBridgeWS)
	mux.HandleFunc(joinServerRoute(base, "ws"), s.handleWhatsAppBridgeWS)
	mux.HandleFunc(joinServerRoute(base, "status"), s.handleWhatsAppBridgeStatus)
	mux.HandleFunc(joinServerRoute(base, "logout"), s.handleWhatsAppBridgeLogout)
	s.server = &http.Server{Addr: s.addr, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.server.Shutdown(shutdownCtx)
	}()
	go func() { _ = s.server.ListenAndServe() }()
	return nil
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
	if strings.TrimSpace(n.ID) == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	s.mgr.Upsert(n)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": n.ID})
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
	n, ok := s.mgr.Get(body.ID)
	if !ok {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	n.LastSeenAt = time.Now().UTC()
	n.Online = true
	s.mgr.Upsert(n)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": body.ID})
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
		switch strings.ToLower(strings.TrimSpace(msg.Type)) {
		case "register":
			if msg.Node == nil || strings.TrimSpace(msg.Node.ID) == "" {
				_ = writeAck(nodes.WireAck{OK: false, Type: "register", Error: "node.id required"})
				continue
			}
			s.mgr.Upsert(*msg.Node)
			connectedID = strings.TrimSpace(msg.Node.ID)
			s.rememberNodeConnection(connectedID, connID)
			s.bindNodeSocket(connectedID, connID, conn)
			if err := writeAck(nodes.WireAck{OK: true, Type: "registered", ID: connectedID}); err != nil {
				return
			}
		case "heartbeat":
			id := strings.TrimSpace(msg.ID)
			if id == "" {
				id = connectedID
			}
			if id == "" {
				_ = writeAck(nodes.WireAck{OK: false, Type: "heartbeat", Error: "id required"})
				continue
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
				continue
			}
			if err := writeAck(nodes.WireAck{OK: true, Type: "heartbeat", ID: connectedID}); err != nil {
				return
			}
		case "signal_offer", "signal_answer", "signal_candidate":
			targetID := strings.TrimSpace(msg.To)
			if s.nodeWebRTC != nil && (targetID == "" || strings.EqualFold(targetID, "gateway")) {
				if err := s.nodeWebRTC.HandleSignal(msg); err != nil {
					if err := writeAck(nodes.WireAck{OK: false, Type: msg.Type, ID: msg.ID, Error: err.Error()}); err != nil {
						return
					}
				} else if err := writeAck(nodes.WireAck{OK: true, Type: "signaled", ID: msg.ID}); err != nil {
					return
				}
				continue
			}
			if strings.TrimSpace(connectedID) == "" {
				if err := writeAck(nodes.WireAck{OK: false, Type: msg.Type, Error: "node not registered"}); err != nil {
					return
				}
				continue
			}
			if targetID == "" {
				if err := writeAck(nodes.WireAck{OK: false, Type: msg.Type, ID: msg.ID, Error: "target node required"}); err != nil {
					return
				}
				continue
			}
			msg.From = connectedID
			if err := s.sendNodeSocketMessage(targetID, msg); err != nil {
				if err := writeAck(nodes.WireAck{OK: false, Type: msg.Type, ID: msg.ID, Error: err.Error()}); err != nil {
					return
				}
				continue
			}
			if err := writeAck(nodes.WireAck{OK: true, Type: "relayed", ID: msg.ID}); err != nil {
				return
			}
		default:
			if err := writeAck(nodes.WireAck{OK: false, Type: msg.Type, ID: msg.ID, Error: "unsupported message type"}); err != nil {
				return
			}
		}
	}
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
	if s.tryServeWebUIDist(w, r, "/webui/index.html") {
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
	if s.tryServeWebUIDist(w, r, r.URL.Path) {
		return
	}
	// SPA fallback
	if s.tryServeWebUIDist(w, r, "/webui/index.html") {
		return
	}
	http.NotFound(w, r)
}

func (s *Server) tryServeWebUIDist(w http.ResponseWriter, r *http.Request, reqPath string) bool {
	dir := strings.TrimSpace(s.webUIDir)
	if dir == "" {
		return false
	}
	p := strings.TrimPrefix(reqPath, "/webui/")
	if reqPath == "/webui" || reqPath == "/webui/" || reqPath == "/webui/index.html" {
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
			w.Header().Set("Content-Type", "application/json")
			info := hotReloadFieldInfo()
			paths := make([]string, 0, len(info))
			for _, it := range info {
				if p, ok := it["path"].(string); ok && strings.TrimSpace(p) != "" {
					paths = append(paths, p)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
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
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		confirmRisky, _ := body["confirm_risky"].(bool)
		delete(body, "confirm_risky")

		oldCfgRaw, _ := os.ReadFile(s.configPath)
		var oldMap map[string]interface{}
		_ = json.Unmarshal(oldCfgRaw, &oldMap)

		riskyPaths := collectRiskyConfigPaths(oldMap, body)
		changedRisky := make([]string, 0)
		for _, p := range riskyPaths {
			if fmt.Sprintf("%v", getPathValue(oldMap, p)) != fmt.Sprintf("%v", getPathValue(body, p)) {
				changedRisky = append(changedRisky, p)
			}
		}
		if len(changedRisky) > 0 && !confirmRisky {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":               false,
				"error":            "risky fields changed; confirmation required",
				"requires_confirm": true,
				"changed_fields":   changedRisky,
			})
			return
		}

		candidate, err := json.Marshal(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cfg := cfgpkg.DefaultConfig()
		dec := json.NewDecoder(bytes.NewReader(candidate))
		dec.DisallowUnknownFields()
		if err := dec.Decode(cfg); err != nil {
			http.Error(w, "config schema validation failed: "+err.Error(), http.StatusBadRequest)
			return
		}
		if errs := cfgpkg.Validate(cfg); len(errs) > 0 {
			list := make([]string, 0, len(errs))
			for _, e := range errs {
				list = append(list, e.Error())
			}
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "config validation failed", "details": list})
			return
		}

		b, err := json.MarshalIndent(body, "", "  ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		tmp := s.configPath + ".tmp"
		if err := os.WriteFile(tmp, b, 0644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.Rename(tmp, s.configPath); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if s.onConfigAfter != nil {
			if err := s.onConfigAfter(); err != nil {
				http.Error(w, "config saved but reload failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			if err := requestSelfReloadSignal(); err != nil {
				http.Error(w, "config saved but reload signal failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "reloaded": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "path": path, "name": h.Filename})
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
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
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
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
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
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accounts": accounts})
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
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "account": account})
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
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "deleted": true})
		case "clear_cooldown":
			if err := loginMgr.ClearCooldown(body.CredentialFile); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "cleared": true})
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
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
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
		providers.ClearProviderAPICooldown(strings.TrimSpace(body.Provider))
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "cleared": true})
	case "clear_history":
		providers.ClearProviderRuntimeHistory(strings.TrimSpace(body.Provider))
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "cleared": true})
	case "refresh_now":
		cfg, err := cfgpkg.LoadConfig(s.configPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		result, err := providers.RefreshProviderRuntimeNow(cfg, strings.TrimSpace(body.Provider), body.OnlyExpiring)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "refreshed": true, "result": result})
	case "rerank":
		cfg, err := cfgpkg.LoadConfig(s.configPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		order, err := providers.RerankProviderRuntime(cfg, strings.TrimSpace(body.Provider))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "reranked": true, "candidate_order": order})
	default:
		http.Error(w, "unsupported action", http.StatusBadRequest)
	}
}

func (s *Server) handleWebUIProviderRuntimeSummary(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg, err := cfgpkg.LoadConfig(s.configPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	query := providers.ProviderRuntimeQuery{
		Provider: strings.TrimSpace(r.URL.Query().Get("provider")),
		Reason:   strings.TrimSpace(r.URL.Query().Get("reason")),
		Target:   strings.TrimSpace(r.URL.Query().Get("target")),
	}
	if secs, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("window_sec"))); secs > 0 {
		query.Window = time.Duration(secs) * time.Second
	}
	if healthBelow, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("health_below"))); healthBelow > 0 {
		query.HealthBelow = healthBelow
	}
	if query.HealthBelow <= 0 {
		query.HealthBelow = 50
	}
	if secs, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("cooldown_until_before_sec"))); secs > 0 {
		query.CooldownBefore = time.Now().Add(time.Duration(secs) * time.Second)
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"summary": providers.GetProviderRuntimeSummary(cfg, query),
	})
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
	pc, ok := cfg.Models.Providers[providerName]
	if !ok {
		return nil, cfgpkg.ProviderConfig{}, fmt.Errorf("provider %q not found", providerName)
	}
	return cfg, pc, nil
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
		if err := s.onConfigAfter(); err != nil {
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
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "reply": resp, "session": session})
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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "session": session, "messages": []interface{}{}})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "session": session, "messages": s.onChatHistory(session)})
}

func (s *Server) handleWebUIChatStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Deprecation", "true")
	w.Header().Set("X-Clawgo-Replaced-By", "/webui/api/chat/live")
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
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
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

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	resp, err := s.onChat(r.Context(), session, prompt)
	if err != nil {
		_, _ = w.Write([]byte("Error: " + err.Error()))
		flusher.Flush()
		return
	}
	chunk := 180
	for i := 0; i < len(resp); i += chunk {
		end := i + chunk
		if end > len(resp) {
			end = len(resp)
		}
		_, _ = w.Write([]byte(resp[i:end]))
		flusher.Flush()
	}
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
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
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
		qrCode, _ = status["qr_code"].(string)
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
	if strings.TrimSpace(s.configPath) != "" {
		if cfg, err := cfgpkg.LoadConfig(strings.TrimSpace(s.configPath)); err == nil {
			providerPayload = providers.GetProviderRuntimeSnapshot(cfg)
		}
	}
	if providerPayload == nil {
		providerPayload = map[string]interface{}{"items": []interface{}{}}
	}
	return map[string]interface{}{
		"version":    s.webUIVersionPayload(),
		"nodes":      s.webUINodesPayload(ctx),
		"sessions":   s.webUISessionsPayload(),
		"task_queue": s.webUITaskQueuePayload(false),
		"ekg":        s.webUIEKGSummaryPayload("24h"),
		"subagents":  s.webUISubagentsRuntimePayload(ctx),
		"providers":  providerPayload,
	}
}

func (s *Server) webUISubagentsRuntimePayload(ctx context.Context) map[string]interface{} {
	if s.onSubagents == nil {
		return map[string]interface{}{
			"items":    []interface{}{},
			"registry": []interface{}{},
			"stream":   []interface{}{},
		}
	}
	call := func(action string, args map[string]interface{}) interface{} {
		res, err := s.onSubagents(ctx, action, args)
		if err != nil {
			return []interface{}{}
		}
		if m, ok := res.(map[string]interface{}); ok {
			if items, ok := m["items"]; ok {
				return items
			}
		}
		return []interface{}{}
	}
	return map[string]interface{}{
		"items":    call("list", map[string]interface{}{}),
		"registry": call("registry", map[string]interface{}{}),
		"stream":   call("stream_all", map[string]interface{}{"limit": 300, "task_limit": 36}),
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
		if ok, _ := row["ok"].(bool); ok {
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
	workspace := strings.TrimSpace(s.workspacePath)
	if workspace == "" {
		return []map[string]interface{}{}
	}
	path := filepath.Join(workspace, "memory", "nodes-dispatch-audit.jsonl")
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

func (s *Server) webUINodeArtifactsPayloadFiltered(nodeFilter, actionFilter, kindFilter string, limit int) []map[string]interface{} {
	nodeFilter = strings.TrimSpace(nodeFilter)
	actionFilter = strings.TrimSpace(actionFilter)
	kindFilter = strings.TrimSpace(kindFilter)
	rows, _ := s.readNodeDispatchAuditRows()
	if len(rows) == 0 {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, limit)
	for rowIndex := len(rows) - 1; rowIndex >= 0; rowIndex-- {
		row := rows[rowIndex]
		artifacts, _ := row["artifacts"].([]interface{})
		for artifactIndex, raw := range artifacts {
			artifact, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			item := map[string]interface{}{
				"id":             buildNodeArtifactID(row, artifact, artifactIndex),
				"time":           row["time"],
				"node":           row["node"],
				"action":         row["action"],
				"used_transport": row["used_transport"],
				"ok":             row["ok"],
				"error":          row["error"],
			}
			for _, key := range []string{"name", "kind", "mime_type", "storage", "path", "url", "content_text", "content_base64", "source_path", "size_bytes"} {
				if value, ok := artifact[key]; ok {
					item[key] = value
				}
			}
			if nodeFilter != "" && !strings.EqualFold(strings.TrimSpace(fmt.Sprint(item["node"])), nodeFilter) {
				continue
			}
			if actionFilter != "" && !strings.EqualFold(strings.TrimSpace(fmt.Sprint(item["action"])), actionFilter) {
				continue
			}
			if kindFilter != "" && !strings.EqualFold(strings.TrimSpace(fmt.Sprint(item["kind"])), kindFilter) {
				continue
			}
			out = append(out, item)
			if limit > 0 && len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func (s *Server) readNodeDispatchAuditRows() ([]map[string]interface{}, string) {
	workspace := strings.TrimSpace(s.workspacePath)
	if workspace == "" {
		return nil, ""
	}
	path := filepath.Join(workspace, "memory", "nodes-dispatch-audit.jsonl")
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

func buildNodeArtifactID(row, artifact map[string]interface{}, artifactIndex int) string {
	seed := fmt.Sprintf("%v|%v|%v|%d|%v|%v|%v",
		row["time"], row["node"], row["action"], artifactIndex,
		artifact["name"], artifact["source_path"], artifact["path"],
	)
	sum := sha1.Sum([]byte(seed))
	return fmt.Sprintf("%x", sum[:8])
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

func (s *Server) findNodeArtifactByID(id string) (map[string]interface{}, bool) {
	for _, item := range s.webUINodeArtifactsPayload(10000) {
		if strings.TrimSpace(fmt.Sprint(item["id"])) == id {
			return item, true
		}
	}
	return nil, false
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

func (s *Server) filteredNodeDispatches(nodeFilter, actionFilter string, limit int) []map[string]interface{} {
	items := s.webUINodesDispatchPayload(limit)
	if nodeFilter == "" && actionFilter == "" {
		return items
	}
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if nodeFilter != "" && !strings.EqualFold(strings.TrimSpace(fmt.Sprint(item["node"])), nodeFilter) {
			continue
		}
		if actionFilter != "" && !strings.EqualFold(strings.TrimSpace(fmt.Sprint(item["action"])), actionFilter) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func filteredNodeAlerts(alerts []map[string]interface{}, nodeFilter string) []map[string]interface{} {
	if nodeFilter == "" {
		return alerts
	}
	out := make([]map[string]interface{}, 0, len(alerts))
	for _, item := range alerts {
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(item["node"])), nodeFilter) {
			out = append(out, item)
		}
	}
	return out
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
	path := filepath.Join(strings.TrimSpace(s.workspacePath), "memory", "task-audit.jsonl")
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
	queuePath := filepath.Join(strings.TrimSpace(s.workspacePath), "memory", "task_queue.json")
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
	workspace := strings.TrimSpace(s.workspacePath)
	ekgPath := filepath.Join(workspace, "memory", "ekg-events.jsonl")
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
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
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
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
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
		_ = json.NewEncoder(w).Encode(payload)
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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "deleted": ok, "id": id})
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
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			if n > 500 {
				n = 500
			}
			limit = n
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":    true,
		"items": s.webUINodesDispatchPayload(limit),
	})
}

func (s *Server) handleWebUINodeDispatchReplay(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.onNodeDispatch == nil {
		http.Error(w, "node dispatch handler not configured", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Node   string                 `json:"node"`
		Action string                 `json:"action"`
		Mode   string                 `json:"mode"`
		Task   string                 `json:"task"`
		Model  string                 `json:"model"`
		Args   map[string]interface{} `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	req := nodes.Request{
		Node:   strings.TrimSpace(body.Node),
		Action: strings.TrimSpace(body.Action),
		Task:   body.Task,
		Model:  body.Model,
		Args:   body.Args,
	}
	if req.Node == "" || req.Action == "" {
		http.Error(w, "node and action are required", http.StatusBadRequest)
		return
	}
	resp, err := s.onNodeDispatch(r.Context(), req, strings.TrimSpace(body.Mode))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"result": resp,
	})
}

func (s *Server) handleWebUINodeArtifacts(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			if n > 1000 {
				n = 1000
			}
			limit = n
		}
	}
	retentionSummary := s.applyNodeArtifactRetention()
	nodeFilter := strings.TrimSpace(r.URL.Query().Get("node"))
	actionFilter := strings.TrimSpace(r.URL.Query().Get("action"))
	kindFilter := strings.TrimSpace(r.URL.Query().Get("kind"))
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":                 true,
		"items":              s.webUINodeArtifactsPayloadFiltered(nodeFilter, actionFilter, kindFilter, limit),
		"artifact_retention": retentionSummary,
	})
}

func (s *Server) handleWebUINodeArtifactsExport(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	retentionSummary := s.applyNodeArtifactRetention()
	limit := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			if n > 1000 {
				n = 1000
			}
			limit = n
		}
	}
	nodeFilter := strings.TrimSpace(r.URL.Query().Get("node"))
	actionFilter := strings.TrimSpace(r.URL.Query().Get("action"))
	kindFilter := strings.TrimSpace(r.URL.Query().Get("kind"))
	artifacts := s.webUINodeArtifactsPayloadFiltered(nodeFilter, actionFilter, kindFilter, limit)
	dispatches := s.filteredNodeDispatches(nodeFilter, actionFilter, limit)
	payload := s.webUINodesPayload(r.Context())
	nodeList, _ := payload["nodes"].([]nodes.NodeInfo)
	p2p, _ := payload["p2p"].(map[string]interface{})
	alerts := filteredNodeAlerts(s.webUINodeAlertsPayload(nodeList, p2p, dispatches), nodeFilter)

	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	writeJSON := func(name string, value interface{}) error {
		entry, err := zw.Create(name)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(entry)
		enc.SetIndent("", "  ")
		return enc.Encode(value)
	}
	manifest := map[string]interface{}{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"filters": map[string]interface{}{
			"node":   nodeFilter,
			"action": actionFilter,
			"kind":   kindFilter,
			"limit":  limit,
		},
		"artifact_count": len(artifacts),
		"dispatch_count": len(dispatches),
		"alert_count":    len(alerts),
		"retention":      retentionSummary,
	}
	if err := writeJSON("manifest.json", manifest); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := writeJSON("dispatches.json", dispatches); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := writeJSON("alerts.json", alerts); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := writeJSON("artifacts.json", artifacts); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, item := range artifacts {
		name := sanitizeZipEntryName(firstNonEmptyString(
			fmt.Sprint(item["name"]),
			fmt.Sprint(item["source_path"]),
			fmt.Sprint(item["path"]),
			fmt.Sprintf("%s.bin", fmt.Sprint(item["id"])),
		))
		raw, _, err := readArtifactBytes(s.workspacePath, item)
		entryName := filepath.ToSlash(filepath.Join("files", fmt.Sprintf("%s-%s", fmt.Sprint(item["id"]), name)))
		if err != nil || len(raw) == 0 {
			entryName = filepath.ToSlash(filepath.Join("files", fmt.Sprintf("%s-metadata.json", fmt.Sprint(item["id"]))))
			raw, err = json.MarshalIndent(item, "", "  ")
			if err != nil {
				continue
			}
		}
		entry, err := zw.Create(entryName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := entry.Write(raw); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := zw.Close(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	filename := "node-artifacts-export.zip"
	if nodeFilter != "" {
		filename = fmt.Sprintf("node-artifacts-%s.zip", sanitizeZipEntryName(nodeFilter))
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(archive.Bytes())
}

func (s *Server) handleWebUINodeArtifactDownload(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	item, ok := s.findNodeArtifactByID(id)
	if !ok {
		http.Error(w, "artifact not found", http.StatusNotFound)
		return
	}
	name := strings.TrimSpace(fmt.Sprint(item["name"]))
	if name == "" {
		name = "artifact"
	}
	mimeType := strings.TrimSpace(fmt.Sprint(item["mime_type"]))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if contentB64 := strings.TrimSpace(fmt.Sprint(item["content_base64"])); contentB64 != "" {
		payload, err := base64.StdEncoding.DecodeString(contentB64)
		if err != nil {
			http.Error(w, "invalid inline artifact payload", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", mimeType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		_, _ = w.Write(payload)
		return
	}
	for _, rawPath := range []string{fmt.Sprint(item["source_path"]), fmt.Sprint(item["path"])} {
		if path := resolveArtifactPath(s.workspacePath, rawPath); path != "" {
			http.ServeFile(w, r, path)
			return
		}
	}
	if contentText := fmt.Sprint(item["content_text"]); strings.TrimSpace(contentText) != "" {
		w.Header().Set("Content-Type", mimeType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		_, _ = w.Write([]byte(contentText))
		return
	}
	http.Error(w, "artifact content unavailable", http.StatusNotFound)
}

func (s *Server) handleWebUINodeArtifactDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	deletedFile, deletedAudit, err := s.deleteNodeArtifact(strings.TrimSpace(body.ID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":            true,
		"id":            strings.TrimSpace(body.ID),
		"deleted_file":  deletedFile,
		"deleted_audit": deletedAudit,
	})
}

func (s *Server) handleWebUINodeArtifactPrune(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Node       string `json:"node"`
		Action     string `json:"action"`
		Kind       string `json:"kind"`
		KeepLatest int    `json:"keep_latest"`
		Limit      int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	limit := body.Limit
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	keepLatest := body.KeepLatest
	if keepLatest < 0 {
		keepLatest = 0
	}
	items := s.webUINodeArtifactsPayloadFiltered(strings.TrimSpace(body.Node), strings.TrimSpace(body.Action), strings.TrimSpace(body.Kind), limit)
	pruned := 0
	deletedFiles := 0
	for index, item := range items {
		if index < keepLatest {
			continue
		}
		deletedFile, deletedAudit, err := s.deleteNodeArtifact(strings.TrimSpace(fmt.Sprint(item["id"])))
		if err != nil || !deletedAudit {
			continue
		}
		pruned++
		if deletedFile {
			deletedFiles++
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":            true,
		"pruned":        pruned,
		"deleted_files": deletedFiles,
		"kept":          keepLatest,
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
	reqURL := baseURL + "/webui/api/subagents_runtime?action=registry"
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
	if item == nil {
		return ""
	}
	v, _ := item[key].(string)
	return strings.TrimSpace(v)
}

func boolFromMap(item map[string]interface{}, key string) bool {
	if item == nil {
		return false
	}
	v, _ := item[key].(bool)
	return v
}

func fallbackString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
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
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "jobs": normalizeCronJobs(res)})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "job": normalizeCronJob(res)})
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
		if a, ok := args["action"].(string); ok && strings.TrimSpace(a) != "" {
			action = strings.ToLower(strings.TrimSpace(a))
		}
		res, err := s.onCron(action, args)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "result": normalizeCronJob(res)})
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
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": id, "files": files})
				return
			}
			if f := strings.TrimSpace(r.URL.Query().Get("file")); f != "" {
				clean := filepath.Clean(f)
				if strings.HasPrefix(clean, "..") {
					http.Error(w, "invalid file path", http.StatusBadRequest)
					return
				}
				full := filepath.Join(skillPath, clean)
				b, err := os.ReadFile(full)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": id, "file": filepath.ToSlash(clean), "content": string(b)})
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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
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
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "imported": imported})
			return
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		action, _ := body["action"].(string)
		action = strings.ToLower(strings.TrimSpace(action))
		if action == "install_clawhub" {
			output, err := ensureClawHubReady(r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":           true,
				"output":       output,
				"installed":    true,
				"clawhub_path": resolveClawHubBinary(r.Context()),
			})
			return
		}
		id, _ := body["id"].(string)
		name, _ := body["name"].(string)
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
			ignoreSuspicious := false
			switch v := body["ignore_suspicious"].(type) {
			case bool:
				ignoreSuspicious = v
			case string:
				ignoreSuspicious = strings.EqualFold(strings.TrimSpace(v), "true") || strings.TrimSpace(v) == "1"
			}
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
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "installed": name, "output": string(out)})
		case "enable":
			if _, err := os.Stat(disabledPath); err == nil {
				if err := os.Rename(disabledPath, enabledPath); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		case "disable":
			if _, err := os.Stat(enabledPath); err == nil {
				if err := os.Rename(enabledPath, disabledPath); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		case "write_file":
			skillPath, err := resolveSkillPath(name)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			filePath, _ := body["file"].(string)
			clean := filepath.Clean(strings.TrimSpace(filePath))
			if clean == "" || strings.HasPrefix(clean, "..") {
				http.Error(w, "invalid file path", http.StatusBadRequest)
				return
			}
			content, _ := body["content"].(string)
			full := filepath.Join(skillPath, clean)
			if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := os.WriteFile(full, []byte(content), 0644); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "name": name, "file": filepath.ToSlash(clean)})
		case "create", "update":
			desc, _ := body["description"].(string)
			sys, _ := body["system_prompt"].(string)
			var toolsList []string
			if arr, ok := body["tools"].([]interface{}); ok {
				for _, v := range arr {
					if sv, ok := v.(string); ok && strings.TrimSpace(sv) != "" {
						toolsList = append(toolsList, strings.TrimSpace(sv))
					}
				}
			}
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
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "deleted": deleted, "id": id})

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
		kind, _ := sch["kind"].(string)
		if expr, ok := sch["expr"].(string); ok && expr != "" {
			out["expr"] = expr
		} else if strings.EqualFold(strings.TrimSpace(kind), "every") {
			if every, ok := sch["everyMs"].(float64); ok && every > 0 {
				out["expr"] = fmt.Sprintf("@every %s", (time.Duration(int64(every)) * time.Millisecond).String())
			}
		} else if strings.EqualFold(strings.TrimSpace(kind), "at") {
			if at, ok := sch["atMs"].(float64); ok && at > 0 {
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

func ensureClawHubReady(ctx context.Context) (string, error) {
	outs := make([]string, 0, 4)
	if p := resolveClawHubBinary(ctx); p != "" {
		return "clawhub already installed at: " + p, nil
	}
	nodeOut, err := ensureNodeRuntime(ctx)
	if nodeOut != "" {
		outs = append(outs, nodeOut)
	}
	if err != nil {
		return strings.Join(outs, "\n"), err
	}
	clawOut, err := runInstallCommand(ctx, "npm i -g clawhub")
	if clawOut != "" {
		outs = append(outs, clawOut)
	}
	if err != nil {
		return strings.Join(outs, "\n"), err
	}
	if p := resolveClawHubBinary(ctx); p != "" {
		outs = append(outs, "clawhub installed at: "+p)
		return strings.Join(outs, "\n"), nil
	}
	return strings.Join(outs, "\n"), fmt.Errorf("installed clawhub but executable still not found in PATH")
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
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "sessions": out})
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

func (s *Server) handleWebUISubagentProfiles(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	workspace := strings.TrimSpace(s.workspacePath)
	if workspace == "" {
		http.Error(w, "workspace path not set", http.StatusInternalServerError)
		return
	}
	store := tools.NewSubagentProfileStore(workspace)

	switch r.Method {
	case http.MethodGet:
		agentID := strings.TrimSpace(r.URL.Query().Get("agent_id"))
		if agentID != "" {
			profile, ok, err := store.Get(agentID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "found": ok, "profile": profile})
			return
		}
		profiles, err := store.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "profiles": profiles})
	case http.MethodDelete:
		agentID := strings.TrimSpace(r.URL.Query().Get("agent_id"))
		if agentID == "" {
			http.Error(w, "agent_id required", http.StatusBadRequest)
			return
		}
		if err := store.Delete(agentID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "deleted": true, "agent_id": agentID})
	case http.MethodPost:
		var body struct {
			Action           string   `json:"action"`
			AgentID          string   `json:"agent_id"`
			Name             string   `json:"name"`
			Role             string   `json:"role"`
			SystemPromptFile string   `json:"system_prompt_file"`
			MemoryNamespace  string   `json:"memory_namespace"`
			Status           string   `json:"status"`
			ToolAllowlist    []string `json:"tool_allowlist"`
			MaxRetries       *int     `json:"max_retries"`
			RetryBackoffMS   *int     `json:"retry_backoff_ms"`
			TimeoutSec       *int     `json:"timeout_sec"`
			MaxTaskChars     *int     `json:"max_task_chars"`
			MaxResultChars   *int     `json:"max_result_chars"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		action := strings.ToLower(strings.TrimSpace(body.Action))
		if action == "" {
			action = "upsert"
		}
		agentID := strings.TrimSpace(body.AgentID)
		if agentID == "" {
			http.Error(w, "agent_id required", http.StatusBadRequest)
			return
		}

		switch action {
		case "create":
			if _, ok, err := store.Get(agentID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			} else if ok {
				http.Error(w, "subagent profile already exists", http.StatusConflict)
				return
			}
			profile, err := store.Upsert(tools.SubagentProfile{
				AgentID:          agentID,
				Name:             body.Name,
				Role:             body.Role,
				SystemPromptFile: body.SystemPromptFile,
				MemoryNamespace:  body.MemoryNamespace,
				Status:           body.Status,
				ToolAllowlist:    body.ToolAllowlist,
				MaxRetries:       derefInt(body.MaxRetries),
				RetryBackoff:     derefInt(body.RetryBackoffMS),
				TimeoutSec:       derefInt(body.TimeoutSec),
				MaxTaskChars:     derefInt(body.MaxTaskChars),
				MaxResultChars:   derefInt(body.MaxResultChars),
			})
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "profile": profile})
		case "update":
			existing, ok, err := store.Get(agentID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !ok || existing == nil {
				http.Error(w, "subagent profile not found", http.StatusNotFound)
				return
			}
			next := *existing
			next.Name = body.Name
			next.Role = body.Role
			next.SystemPromptFile = body.SystemPromptFile
			next.MemoryNamespace = body.MemoryNamespace
			if body.Status != "" {
				next.Status = body.Status
			}
			if body.ToolAllowlist != nil {
				next.ToolAllowlist = body.ToolAllowlist
			}
			if body.MaxRetries != nil {
				next.MaxRetries = *body.MaxRetries
			}
			if body.RetryBackoffMS != nil {
				next.RetryBackoff = *body.RetryBackoffMS
			}
			if body.TimeoutSec != nil {
				next.TimeoutSec = *body.TimeoutSec
			}
			if body.MaxTaskChars != nil {
				next.MaxTaskChars = *body.MaxTaskChars
			}
			if body.MaxResultChars != nil {
				next.MaxResultChars = *body.MaxResultChars
			}
			profile, err := store.Upsert(next)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "profile": profile})
		case "enable", "disable":
			existing, ok, err := store.Get(agentID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !ok || existing == nil {
				http.Error(w, "subagent profile not found", http.StatusNotFound)
				return
			}
			if action == "enable" {
				existing.Status = "active"
			} else {
				existing.Status = "disabled"
			}
			profile, err := store.Upsert(*existing)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "profile": profile})
		case "delete":
			if err := store.Delete(agentID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "deleted": true, "agent_id": agentID})
		case "upsert":
			profile, err := store.Upsert(tools.SubagentProfile{
				AgentID:          agentID,
				Name:             body.Name,
				Role:             body.Role,
				SystemPromptFile: body.SystemPromptFile,
				MemoryNamespace:  body.MemoryNamespace,
				Status:           body.Status,
				ToolAllowlist:    body.ToolAllowlist,
				MaxRetries:       derefInt(body.MaxRetries),
				RetryBackoff:     derefInt(body.RetryBackoffMS),
				TimeoutSec:       derefInt(body.TimeoutSec),
				MaxTaskChars:     derefInt(body.MaxTaskChars),
				MaxResultChars:   derefInt(body.MaxResultChars),
			})
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "profile": profile})
		default:
			http.Error(w, "unsupported action", http.StatusBadRequest)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"groups": tools.ToolAllowlistGroups(),
	})
}

func (s *Server) handleWebUISubagentsRuntime(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.onSubagents == nil {
		http.Error(w, "subagent runtime handler not configured", http.StatusServiceUnavailable)
		return
	}

	action := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("action")))
	args := map[string]interface{}{}
	switch r.Method {
	case http.MethodGet:
		if action == "" {
			action = "list"
		}
		for key, values := range r.URL.Query() {
			if key == "action" || key == "token" || len(values) == 0 {
				continue
			}
			args[key] = strings.TrimSpace(values[0])
		}
	case http.MethodPost:
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if body == nil {
			body = map[string]interface{}{}
		}
		if action == "" {
			if raw, _ := body["action"].(string); raw != "" {
				action = strings.ToLower(strings.TrimSpace(raw))
			}
		}
		delete(body, "action")
		args = body
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result, err := s.onSubagents(r.Context(), action, args)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "result": result})
}

func (s *Server) handleWebUISubagentsRuntimeLive(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.onSubagents == nil {
		http.Error(w, "subagent runtime handler not configured", http.StatusServiceUnavailable)
		return
	}
	conn, err := nodesWebsocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx := r.Context()
	taskID := strings.TrimSpace(r.URL.Query().Get("task_id"))
	previewTaskID := strings.TrimSpace(r.URL.Query().Get("preview_task_id"))
	sub := s.subscribeSubagentLive(ctx, taskID, previewTaskID)
	initial := map[string]interface{}{
		"ok":      true,
		"type":    "subagents_live",
		"payload": s.buildSubagentsLivePayload(ctx, taskID, previewTaskID),
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

func (s *Server) buildSubagentsLivePayload(ctx context.Context, taskID, previewTaskID string) map[string]interface{} {
	call := func(action string, args map[string]interface{}) map[string]interface{} {
		res, err := s.onSubagents(ctx, action, args)
		if err != nil {
			return map[string]interface{}{}
		}
		if m, ok := res.(map[string]interface{}); ok {
			return m
		}
		return map[string]interface{}{}
	}
	payload := map[string]interface{}{}
	taskID = strings.TrimSpace(taskID)
	previewTaskID = strings.TrimSpace(previewTaskID)
	if taskID != "" {
		payload["thread"] = call("thread", map[string]interface{}{"id": taskID, "limit": 50})
		payload["inbox"] = call("inbox", map[string]interface{}{"id": taskID, "limit": 50})
	}
	if previewTaskID != "" {
		payload["preview"] = call("stream", map[string]interface{}{"id": previewTaskID, "limit": 12})
	}
	return payload
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
			entries, err := os.ReadDir(memoryDir)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			files := make([]string, 0, len(entries))
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				files = append(files, e.Name())
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "files": files})
			return
		}
		clean := filepath.Clean(path)
		if strings.HasPrefix(clean, "..") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		full := filepath.Join(memoryDir, clean)
		b, err := os.ReadFile(full)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "path": clean, "content": string(b)})
	case http.MethodPost:
		var body struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		clean := filepath.Clean(body.Path)
		if clean == "" || strings.HasPrefix(clean, "..") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		full := filepath.Join(memoryDir, clean)
		if err := os.WriteFile(full, []byte(body.Content), 0644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "path": clean})
	case http.MethodDelete:
		path := filepath.Clean(r.URL.Query().Get("path"))
		if path == "" || strings.HasPrefix(path, "..") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		full := filepath.Join(memoryDir, path)
		if err := os.Remove(full); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "deleted": true, "path": path})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWebUITaskAudit(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := filepath.Join(strings.TrimSpace(s.workspacePath), "memory", "task-audit.jsonl")
	includeHeartbeat := r.URL.Query().Get("include_heartbeat") == "1"
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 500 {
				n = 500
			}
			limit = n
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "items": []map[string]interface{}{}})
		return
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	items := make([]map[string]interface{}, 0, len(lines))
	for _, ln := range lines {
		if ln == "" {
			continue
		}
		var row map[string]interface{}
		if err := json.Unmarshal([]byte(ln), &row); err == nil {
			source := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["source"])))
			if !includeHeartbeat && source == "heartbeat" {
				continue
			}
			items = append(items, row)
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "items": items})
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
	path := filepath.Join(strings.TrimSpace(s.workspacePath), "memory", "task-audit.jsonl")
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
	queuePath := filepath.Join(strings.TrimSpace(s.workspacePath), "memory", "task_queue.json")
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
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "running": running, "items": items, "stats": stats})
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
	workspace := strings.TrimSpace(s.workspacePath)
	ekgPath := filepath.Join(workspace, "memory", "ekg-events.jsonl")
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
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
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

func (s *Server) handleWebUIExecApprovals(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if strings.TrimSpace(s.configPath) == "" {
		http.Error(w, "config path not set", http.StatusInternalServerError)
		return
	}
	b, err := os.ReadFile(s.configPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(b, &cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.Method == http.MethodGet {
		toolsMap, _ := cfg["tools"].(map[string]interface{})
		shellMap, _ := toolsMap["shell"].(map[string]interface{})
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "exec_approvals": shellMap})
		return
	}
	if r.Method == http.MethodPost {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		toolsMap, _ := cfg["tools"].(map[string]interface{})
		if toolsMap == nil {
			toolsMap = map[string]interface{}{}
			cfg["tools"] = toolsMap
		}
		shellMap, _ := toolsMap["shell"].(map[string]interface{})
		if shellMap == nil {
			shellMap = map[string]interface{}{}
			toolsMap["shell"] = shellMap
		}
		for k, v := range body {
			shellMap[k] = v
		}
		out, _ := json.MarshalIndent(cfg, "", "  ")
		tmp := s.configPath + ".tmp"
		if err := os.WriteFile(tmp, out, 0644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.Rename(tmp, s.configPath); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if s.onConfigAfter != nil {
			if err := s.onConfigAfter(); err != nil {
				http.Error(w, "config saved but reload failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "reloaded": true})
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
	limit := 10
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
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
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "logs": out})
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

func (s *Server) handleWebUILogsStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Deprecation", "true")
	w.Header().Set("X-Clawgo-Replaced-By", "/webui/api/logs/live")
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
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	if fi != nil {
		_, _ = f.Seek(fi.Size(), io.SeekStart)
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	reader := bufio.NewReader(f)
	for {
		select {
		case <-r.Context().Done():
			return
		default:
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				if parsed, ok := parseLogLine(line); ok {
					b, _ := json.Marshal(parsed)
					_, _ = w.Write(append(b, '\n'))
					flusher.Flush()
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
async function loadCfg(){let r=await fetch('/webui/api/config'+auth());document.getElementById('cfg').value=await r.text()}
async function saveCfg(){let j=JSON.parse(document.getElementById('cfg').value);let r=await fetch('/webui/api/config'+auth(),{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(j)});alert(await r.text())}
async function sendChat(){
 let media='';const f=document.getElementById('file').files[0];
 if(f){let fd=new FormData();fd.append('file',f);let ur=await fetch('/webui/api/upload'+auth(),{method:'POST',body:fd});let uj=await ur.json();media=uj.path||''}
 const payload={session:document.getElementById('session').value,message:document.getElementById('msg').value,media};
 let r=await fetch('/webui/api/chat'+auth(),{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)});let t=await r.text();
 document.getElementById('chatlog').textContent += '\nUSER> '+payload.message+(media?(' [file:'+media+']'):'')+'\nBOT> '+t+'\n';
}
loadCfg();
</script></body></html>`
