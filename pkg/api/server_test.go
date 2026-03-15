package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cfgpkg "github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/nodes"
	"github.com/gorilla/websocket"
)

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
		"agent:coder.jsonl",
	} {
		if err := os.WriteFile(filepath.Join(sessionsDir, name), []byte("{}\n"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	srv := NewServer("127.0.0.1", 0, "", nil)
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

	keys := make([]string, 0, len(payload.Sessions))
	for _, item := range payload.Sessions {
		keys = append(keys, item.Key)
	}
	if len(keys) != 1 || keys[0] != "review-api" {
		t.Fatalf("unexpected sessions: %v", keys)
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
	t.Parallel()

	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "app.log")
	if err := os.WriteFile(logPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write log file: %v", err)
	}

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
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

	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
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
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := cfgpkg.DefaultConfig()
	cfg.Agents.Agents["coder"] = cfgpkg.AgentConfig{
		Enabled: true,
		Role:    "coding",
	}
	if err := cfgpkg.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	srv.SetConfigPath(cfgPath)

	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
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
	if len(agents) != 2 {
		t.Fatalf("expected local agents from registry, got %+v", local)
	}
}

func TestBuildWebUIRuntimeSnapshotIncludesWorldPayload(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	srv.SetRuntimeAdminHandler(func(ctx context.Context, action string, args map[string]interface{}) (interface{}, error) {
		switch action {
		case "snapshot":
			return map[string]interface{}{
				"snapshot": map[string]interface{}{
					"world": map[string]interface{}{
						"world_id":    "main-world",
						"tick":        3,
						"npc_count":   2,
						"active_npcs": []string{"keeper", "merchant"},
					},
				},
			}, nil
		case "world_snapshot":
			return map[string]interface{}{
				"snapshot": map[string]interface{}{
					"world_id":  "main-world",
					"tick":      3,
					"npc_count": 2,
				},
			}, nil
		default:
			return map[string]interface{}{}, nil
		}
	})

	payload := srv.buildWebUIRuntimeSnapshot(context.Background())
	worldPayload, _ := payload["world"].(map[string]interface{})
	if strings.TrimSpace(fmt.Sprint(worldPayload["world_id"])) != "main-world" {
		t.Fatalf("expected top-level world payload, got %+v", payload)
	}
}

func TestHandleWebUIWorldReturnsWorldSnapshot(t *testing.T) {
	t.Parallel()

	srv := NewServer("127.0.0.1", 0, "", nodes.NewManager())
	srv.SetRuntimeAdminHandler(func(ctx context.Context, action string, args map[string]interface{}) (interface{}, error) {
		if action != "world_snapshot" {
			return map[string]interface{}{}, nil
		}
		return map[string]interface{}{
			"snapshot": map[string]interface{}{
				"world_id":    "main-world",
				"tick":        7,
				"npc_count":   1,
				"active_npcs": []string{"watcher"},
			},
		}, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api/world?limit=10", nil)
	rec := httptest.NewRecorder()
	srv.handleWebUIWorld(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if found, _ := body["found"].(bool); !found {
		t.Fatalf("expected found=true, got %+v", body)
	}
	worldPayload, _ := body["world"].(map[string]interface{})
	if strings.TrimSpace(fmt.Sprint(worldPayload["tick"])) != "7" {
		t.Fatalf("expected world tick 7, got %+v", body)
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
	req := httptest.NewRequest(http.MethodPost, "/api/node_dispatches/replay", strings.NewReader(body))
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
