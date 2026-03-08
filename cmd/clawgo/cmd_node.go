package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawgo/pkg/agent"
	"clawgo/pkg/bus"
	"clawgo/pkg/config"
	"clawgo/pkg/cron"
	"clawgo/pkg/nodes"
	"clawgo/pkg/providers"
	"clawgo/pkg/runtimecfg"
	"clawgo/pkg/tools"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

type nodeRegisterOptions struct {
	GatewayBase  string
	Token        string
	NodeToken    string
	ID           string
	Name         string
	Endpoint     string
	OS           string
	Arch         string
	Version      string
	Actions      []string
	Models       []string
	Agents       []nodes.AgentInfo
	Capabilities nodes.Capabilities
	Watch        bool
	HeartbeatSec int
}

type nodeHeartbeatOptions struct {
	GatewayBase string
	Token       string
	ID          string
}

type nodeWebRTCSession struct {
	info      nodes.NodeInfo
	opts      nodeRegisterOptions
	client    *http.Client
	writeJSON func(interface{}) error

	mu sync.Mutex
	pc *webrtc.PeerConnection
	dc *webrtc.DataChannel
}

type nodeLocalExecutor struct {
	configPath string
	workspace  string
	once       sync.Once
	loop       *agent.AgentLoop
	err        error
}

var (
	nodeLocalExecutorMu      sync.Mutex
	nodeLocalExecutors       = map[string]*nodeLocalExecutor{}
	nodeProviderFactory      = providers.CreateProvider
	nodeAgentLoopFactory     = agent.NewAgentLoop
	nodeLocalExecutorFactory = newNodeLocalExecutor
	nodeCameraSnapFunc       = captureNodeCameraSnapshot
	nodeScreenSnapFunc       = captureNodeScreenSnapshot
)

const nodeArtifactInlineLimit = 512 * 1024

func nodeCmd() {
	args := os.Args[2:]
	if len(args) == 0 {
		printNodeHelp()
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "register":
		nodeRegisterCmd(args[1:])
	case "heartbeat":
		nodeHeartbeatCmd(args[1:])
	case "help", "--help", "-h":
		printNodeHelp()
	default:
		fmt.Printf("Unknown node command: %s\n", args[0])
		printNodeHelp()
	}
}

func printNodeHelp() {
	fmt.Println("Node commands:")
	fmt.Println("  clawgo node register [options]")
	fmt.Println("  clawgo node heartbeat [options]")
	fmt.Println()
	fmt.Println("Register options:")
	fmt.Println("  --gateway <url>           Gateway base URL, e.g. http://host:18790")
	fmt.Println("  --token <value>           Gateway token (optional when gateway.token is empty)")
	fmt.Println("  --node-token <value>      Bearer token for this node endpoint (optional)")
	fmt.Println("  --id <value>              Node ID (default: hostname)")
	fmt.Println("  --name <value>            Node name (default: hostname)")
	fmt.Println("  --endpoint <url>          Public endpoint of this node")
	fmt.Println("  --os <value>              Reported OS (default: current runtime)")
	fmt.Println("  --arch <value>            Reported arch (default: current runtime)")
	fmt.Println("  --version <value>         Reported node version (default: current clawgo version)")
	fmt.Println("  --actions <csv>           Supported actions, e.g. run,agent_task")
	fmt.Println("  --models <csv>            Supported models, e.g. gpt-4o-mini")
	fmt.Println("  --capabilities <csv>      Capability flags: run,invoke,model,camera,screen,location,canvas")
	fmt.Println("  --watch                   Keep a websocket connection open and send heartbeats")
	fmt.Println("  --heartbeat-sec <n>       Heartbeat interval in seconds when --watch is set (default: 30)")
	fmt.Println()
	fmt.Println("Heartbeat options:")
	fmt.Println("  --gateway <url>           Gateway base URL")
	fmt.Println("  --token <value>           Gateway token")
	fmt.Println("  --id <value>              Node ID")
}

func nodeRegisterCmd(args []string) {
	cfg, _ := loadConfig()
	opts, err := parseNodeRegisterArgs(args, cfg)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		printNodeHelp()
		os.Exit(1)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	info := buildNodeInfo(opts)
	ctx := context.Background()
	if err := postNodeRegister(ctx, client, opts.GatewayBase, opts.Token, info); err != nil {
		fmt.Printf("Error registering node: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Node registered: %s -> %s\n", info.ID, opts.GatewayBase)
	if !opts.Watch {
		return
	}
	fmt.Printf("✓ Heartbeat loop started: every %ds\n", opts.HeartbeatSec)
	if err := runNodeHeartbeatLoop(client, opts, info); err != nil {
		fmt.Printf("Heartbeat loop stopped: %v\n", err)
		os.Exit(1)
	}
}

func nodeHeartbeatCmd(args []string) {
	cfg, _ := loadConfig()
	opts, err := parseNodeHeartbeatArgs(args, cfg)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		printNodeHelp()
		os.Exit(1)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	if err := postNodeHeartbeat(context.Background(), client, opts.GatewayBase, opts.Token, opts.ID); err != nil {
		fmt.Printf("Error sending heartbeat: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Heartbeat sent: %s -> %s\n", opts.ID, opts.GatewayBase)
}

func parseNodeRegisterArgs(args []string, cfg *config.Config) (nodeRegisterOptions, error) {
	host, _ := os.Hostname()
	host = strings.TrimSpace(host)
	if host == "" {
		host = "node"
	}
	opts := nodeRegisterOptions{
		GatewayBase:  defaultGatewayBase(cfg),
		Token:        defaultGatewayToken(cfg),
		ID:           host,
		Name:         host,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		Version:      version,
		HeartbeatSec: 30,
		Capabilities: capabilitiesFromCSV("run,invoke,model"),
	}
	opts.Agents = nodeAgentsFromConfig(cfg)
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("missing value for %s", arg)
			}
			i++
			return strings.TrimSpace(args[i]), nil
		}
		switch arg {
		case "--gateway":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.GatewayBase = v
		case "--token":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.Token = v
		case "--node-token":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.NodeToken = v
		case "--id":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.ID = v
		case "--name":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.Name = v
		case "--endpoint":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.Endpoint = v
		case "--os":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.OS = v
		case "--arch":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.Arch = v
		case "--version":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.Version = v
		case "--actions":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.Actions = splitCSV(v)
		case "--models":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.Models = splitCSV(v)
		case "--capabilities":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.Capabilities = capabilitiesFromCSV(v)
		case "--watch":
			opts.Watch = true
		case "--heartbeat-sec":
			v, err := next()
			if err != nil {
				return opts, err
			}
			n, convErr := strconv.Atoi(v)
			if convErr != nil || n <= 0 {
				return opts, fmt.Errorf("invalid --heartbeat-sec: %s", v)
			}
			opts.HeartbeatSec = n
		default:
			return opts, fmt.Errorf("unknown option: %s", arg)
		}
	}
	if strings.TrimSpace(opts.GatewayBase) == "" {
		return opts, fmt.Errorf("--gateway is required")
	}
	if strings.TrimSpace(opts.ID) == "" {
		return opts, fmt.Errorf("--id is required")
	}
	opts.GatewayBase = normalizeGatewayBase(opts.GatewayBase)
	return opts, nil
}

func parseNodeHeartbeatArgs(args []string, cfg *config.Config) (nodeHeartbeatOptions, error) {
	host, _ := os.Hostname()
	host = strings.TrimSpace(host)
	if host == "" {
		host = "node"
	}
	opts := nodeHeartbeatOptions{
		GatewayBase: defaultGatewayBase(cfg),
		Token:       defaultGatewayToken(cfg),
		ID:          host,
	}
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("missing value for %s", arg)
			}
			i++
			return strings.TrimSpace(args[i]), nil
		}
		switch arg {
		case "--gateway":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.GatewayBase = v
		case "--token":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.Token = v
		case "--id":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.ID = v
		default:
			return opts, fmt.Errorf("unknown option: %s", arg)
		}
	}
	if strings.TrimSpace(opts.GatewayBase) == "" {
		return opts, fmt.Errorf("--gateway is required")
	}
	if strings.TrimSpace(opts.ID) == "" {
		return opts, fmt.Errorf("--id is required")
	}
	opts.GatewayBase = normalizeGatewayBase(opts.GatewayBase)
	return opts, nil
}

func buildNodeInfo(opts nodeRegisterOptions) nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           strings.TrimSpace(opts.ID),
		Name:         strings.TrimSpace(opts.Name),
		OS:           strings.TrimSpace(opts.OS),
		Arch:         strings.TrimSpace(opts.Arch),
		Version:      strings.TrimSpace(opts.Version),
		Endpoint:     strings.TrimSpace(opts.Endpoint),
		Token:        strings.TrimSpace(opts.NodeToken),
		Capabilities: opts.Capabilities,
		Actions:      append([]string(nil), opts.Actions...),
		Models:       append([]string(nil), opts.Models...),
		Agents:       append([]nodes.AgentInfo(nil), opts.Agents...),
	}
}

func nodeAgentsFromConfig(cfg *config.Config) []nodes.AgentInfo {
	if cfg == nil {
		return nil
	}
	items := make([]nodes.AgentInfo, 0, len(cfg.Agents.Subagents))
	for agentID, subcfg := range cfg.Agents.Subagents {
		id := strings.TrimSpace(agentID)
		if id == "" || !subcfg.Enabled {
			continue
		}
		items = append(items, nodes.AgentInfo{
			ID:            id,
			DisplayName:   strings.TrimSpace(subcfg.DisplayName),
			Role:          strings.TrimSpace(subcfg.Role),
			Type:          strings.TrimSpace(subcfg.Type),
			Transport:     strings.TrimSpace(subcfg.Transport),
			ParentAgentID: strings.TrimSpace(subcfg.ParentAgentID),
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func runNodeHeartbeatLoop(client *http.Client, opts nodeRegisterOptions, info nodes.NodeInfo) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	for {
		if err := runNodeHeartbeatSocket(ctx, opts, info); err != nil {
			if ctx.Err() != nil {
				fmt.Println("✓ Node heartbeat stopped")
				return nil
			}
			fmt.Printf("Warning: node socket closed for %s: %v\n", info.ID, err)
		}
		if ctx.Err() != nil {
			fmt.Println("✓ Node heartbeat stopped")
			return nil
		}
		if regErr := postNodeRegister(ctx, client, opts.GatewayBase, opts.Token, info); regErr != nil {
			fmt.Printf("Warning: re-register failed for %s: %v\n", info.ID, regErr)
		} else {
			fmt.Printf("✓ Node re-registered: %s\n", info.ID)
		}
		select {
		case <-ctx.Done():
			fmt.Println("✓ Node heartbeat stopped")
			return nil
		case <-time.After(2 * time.Second):
		}
	}
}

func runNodeHeartbeatSocket(ctx context.Context, opts nodeRegisterOptions, info nodes.NodeInfo) error {
	wsURL := nodeWebsocketURL(opts.GatewayBase)
	headers := http.Header{}
	if strings.TrimSpace(opts.Token) != "" {
		headers.Set("Authorization", "Bearer "+strings.TrimSpace(opts.Token))
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return err
	}
	defer conn.Close()
	var writeMu sync.Mutex
	writeJSON := func(v interface{}) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(v)
	}
	writePing := func() error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(10*time.Second))
	}

	if err := writeJSON(nodes.WireMessage{Type: "register", Node: &info}); err != nil {
		return err
	}
	acks := make(chan nodes.WireAck, 8)
	errs := make(chan error, 1)
	client := &http.Client{Timeout: 20 * time.Second}
	go readNodeSocketLoop(ctx, conn, writeJSON, client, info, opts, acks, errs)
	if err := waitNodeAck(ctx, acks, errs, "registered", info.ID); err != nil {
		return err
	}
	fmt.Printf("✓ Node socket connected: %s\n", info.ID)

	ticker := time.NewTicker(time.Duration(opts.HeartbeatSec) * time.Second)
	pingTicker := time.NewTicker(nodeSocketPingInterval(opts.HeartbeatSec))
	defer ticker.Stop()
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errs:
			if err != nil {
				return err
			}
			return nil
		case <-pingTicker.C:
			if err := writePing(); err != nil {
				return err
			}
		case <-ticker.C:
			if err := writeJSON(nodes.WireMessage{Type: "heartbeat", ID: info.ID}); err != nil {
				return err
			}
			if err := waitNodeAck(ctx, acks, errs, "heartbeat", info.ID); err != nil {
				return err
			}
			fmt.Printf("✓ Heartbeat ok: %s\n", info.ID)
		}
	}
}

func nodeSocketPingInterval(heartbeatSec int) time.Duration {
	if heartbeatSec <= 0 {
		return 25 * time.Second
	}
	interval := time.Duration(heartbeatSec) * time.Second / 2
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	if interval > 25*time.Second {
		interval = 25 * time.Second
	}
	return interval
}

func waitNodeAck(ctx context.Context, acks <-chan nodes.WireAck, errs <-chan error, expectedType, id string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errs:
			if err == nil {
				return context.Canceled
			}
			return err
		case ack := <-acks:
			if !ack.OK {
				if strings.TrimSpace(ack.Error) == "" {
					ack.Error = "unknown websocket error"
				}
				return fmt.Errorf("%s", ack.Error)
			}
			ackType := strings.ToLower(strings.TrimSpace(ack.Type))
			if expectedType != "" && ackType != strings.ToLower(strings.TrimSpace(expectedType)) {
				continue
			}
			if strings.TrimSpace(id) != "" && strings.TrimSpace(ack.ID) != "" && strings.TrimSpace(ack.ID) != strings.TrimSpace(id) {
				continue
			}
			return nil
		}
	}
}

func readNodeSocketLoop(ctx context.Context, conn *websocket.Conn, writeJSON func(interface{}) error, client *http.Client, info nodes.NodeInfo, opts nodeRegisterOptions, acks chan<- nodes.WireAck, errs chan<- error) {
	defer close(acks)
	defer close(errs)
	rtc := &nodeWebRTCSession{info: info, opts: opts, client: client, writeJSON: writeJSON}
	for {
		select {
		case <-ctx.Done():
			errs <- nil
			return
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			errs <- err
			return
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}
		if _, hasOK := raw["ok"]; hasOK {
			var ack nodes.WireAck
			if err := json.Unmarshal(data, &ack); err == nil {
				acks <- ack
			}
			continue
		}
		var msg nodes.WireMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(msg.Type)) {
		case "node_request":
			go handleNodeWireRequest(ctx, writeJSON, client, info, opts, msg)
		case "signal_offer", "signal_answer", "signal_candidate":
			if err := rtc.handleSignal(ctx, msg); err != nil {
				fmt.Printf("Warning: node webrtc signal failed: %v\n", err)
			}
		}
	}
}

func (s *nodeWebRTCSession) handleSignal(ctx context.Context, msg nodes.WireMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch strings.ToLower(strings.TrimSpace(msg.Type)) {
	case "signal_offer":
		pc, err := s.ensurePeerConnectionLocked()
		if err != nil {
			return err
		}
		var desc webrtc.SessionDescription
		if err := mapWirePayload(msg.Payload, &desc); err != nil {
			return err
		}
		if err := pc.SetRemoteDescription(desc); err != nil {
			return err
		}
		answer, err := pc.CreateAnswer(nil)
		if err != nil {
			return err
		}
		if err := pc.SetLocalDescription(answer); err != nil {
			return err
		}
		return s.writeJSON(nodes.WireMessage{
			Type:    "signal_answer",
			From:    s.info.ID,
			To:      "gateway",
			Session: strings.TrimSpace(msg.Session),
			Payload: structToWirePayload(*pc.LocalDescription()),
		})
	case "signal_candidate":
		if s.pc == nil {
			return nil
		}
		var candidate webrtc.ICECandidateInit
		if err := mapWirePayload(msg.Payload, &candidate); err != nil {
			return err
		}
		return s.pc.AddICECandidate(candidate)
	default:
		return nil
	}
}

func (s *nodeWebRTCSession) ensurePeerConnectionLocked() (*webrtc.PeerConnection, error) {
	if s.pc != nil {
		return s.pc, nil
	}
	config := webrtc.Configuration{}
	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		_ = s.writeJSON(nodes.WireMessage{
			Type:    "signal_candidate",
			From:    s.info.ID,
			To:      "gateway",
			Session: s.info.ID,
			Payload: structToWirePayload(candidate.ToJSON()),
		})
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed, webrtc.PeerConnectionStateDisconnected:
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.pc != nil {
				_ = s.pc.Close()
			}
			s.pc = nil
			s.dc = nil
		}
	})
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		s.mu.Lock()
		s.dc = dc
		s.mu.Unlock()
		dc.OnMessage(func(message webrtc.DataChannelMessage) {
			var msg nodes.WireMessage
			if err := json.Unmarshal(message.Data, &msg); err != nil {
				return
			}
			if strings.ToLower(strings.TrimSpace(msg.Type)) != "node_request" || msg.Request == nil {
				return
			}
			go s.handleDataChannelRequest(context.Background(), dc, msg)
		})
	})
	s.pc = pc
	return pc, nil
}

func (s *nodeWebRTCSession) handleDataChannelRequest(ctx context.Context, dc *webrtc.DataChannel, msg nodes.WireMessage) {
	resp := executeNodeRequest(ctx, s.client, s.info, s.opts, msg.Request)
	b, err := json.Marshal(nodes.WireMessage{
		Type:     "node_response",
		ID:       msg.ID,
		From:     s.info.ID,
		To:       "gateway",
		Session:  strings.TrimSpace(msg.Session),
		Response: &resp,
	})
	if err != nil {
		return
	}
	_ = dc.Send(b)
}

func handleNodeWireRequest(ctx context.Context, writeJSON func(interface{}) error, client *http.Client, info nodes.NodeInfo, opts nodeRegisterOptions, msg nodes.WireMessage) {
	resp := executeNodeRequest(ctx, client, info, opts, msg.Request)
	_ = writeJSON(nodes.WireMessage{
		Type:     "node_response",
		ID:       msg.ID,
		From:     info.ID,
		To:       strings.TrimSpace(msg.From),
		Session:  strings.TrimSpace(msg.Session),
		Response: &resp,
	})
}

func executeNodeRequest(ctx context.Context, client *http.Client, info nodes.NodeInfo, opts nodeRegisterOptions, req *nodes.Request) nodes.Response {
	resp := nodes.Response{
		OK:     false,
		Code:   "invalid_request",
		Node:   info.ID,
		Action: "",
		Error:  "request missing",
	}
	if req == nil {
		return resp
	}
	next := *req
	resp.Action = next.Action
	switch strings.ToLower(strings.TrimSpace(next.Action)) {
	case "agent_task":
		execResp, err := executeNodeAgentTask(ctx, info, next)
		if err == nil {
			return execResp
		}
		if strings.TrimSpace(opts.Endpoint) == "" {
			resp.Error = err.Error()
			resp.Code = "local_runtime_error"
			return resp
		}
	case "camera_snap":
		execResp, err := executeNodeCameraSnap(ctx, info, next)
		if err == nil {
			return execResp
		}
		if strings.TrimSpace(opts.Endpoint) == "" {
			resp.Error = err.Error()
			resp.Code = "local_runtime_error"
			return resp
		}
	case "screen_snapshot":
		execResp, err := executeNodeScreenSnapshot(ctx, info, next)
		if err == nil {
			return execResp
		}
		if strings.TrimSpace(opts.Endpoint) == "" {
			resp.Error = err.Error()
			resp.Code = "local_runtime_error"
			return resp
		}
	}
	if strings.TrimSpace(opts.Endpoint) == "" {
		resp.Error = "node endpoint not configured"
		resp.Code = "endpoint_missing"
		return resp
	}
	if next.Node == "" {
		next.Node = info.ID
	}
	execResp, err := nodes.DoEndpointRequest(ctx, client, opts.Endpoint, opts.NodeToken, next)
	if err != nil {
		return nodes.Response{
			OK:     false,
			Code:   "transport_error",
			Node:   info.ID,
			Action: next.Action,
			Error:  err.Error(),
		}
	}
	if strings.TrimSpace(execResp.Node) == "" {
		execResp.Node = info.ID
	}
	return execResp
}

func executeNodeAgentTask(ctx context.Context, info nodes.NodeInfo, req nodes.Request) (nodes.Response, error) {
	executor, err := getNodeLocalExecutor()
	if err != nil {
		return nodes.Response{}, err
	}
	loop, err := executor.Loop()
	if err != nil {
		return nodes.Response{}, err
	}

	remoteAgentID := strings.TrimSpace(stringArg(req.Args, "remote_agent_id"))
	if remoteAgentID == "" || strings.EqualFold(remoteAgentID, "main") {
		sessionKey := fmt.Sprintf("node:%s:main", info.ID)
		result, err := loop.ProcessDirectWithOptions(ctx, strings.TrimSpace(req.Task), sessionKey, "node", info.ID, "main", nil)
		if err != nil {
			return nodes.Response{}, err
		}
		artifacts, err := collectNodeArtifacts(executor.workspace, req.Args)
		if err != nil {
			return nodes.Response{}, err
		}
		return nodes.Response{
			OK:     true,
			Code:   "ok",
			Node:   info.ID,
			Action: req.Action,
			Payload: map[string]interface{}{
				"transport": "clawgo-local",
				"agent_id":  "main",
				"result":    strings.TrimSpace(result),
				"artifacts": artifacts,
			},
		}, nil
	}

	out, err := loop.HandleSubagentRuntime(ctx, "dispatch_and_wait", map[string]interface{}{
		"task":             strings.TrimSpace(req.Task),
		"agent_id":         remoteAgentID,
		"channel":          "node",
		"chat_id":          info.ID,
		"wait_timeout_sec": float64(120),
	})
	if err != nil {
		return nodes.Response{}, err
	}
	payload, _ := out.(map[string]interface{})
	result := strings.TrimSpace(fmt.Sprint(payload["merged"]))
	if result == "" {
		if reply, ok := payload["reply"].(*tools.RouterReply); ok {
			result = strings.TrimSpace(reply.Result)
		}
	}
	artifacts, err := collectNodeArtifacts(executor.workspace, req.Args)
	if err != nil {
		return nodes.Response{}, err
	}
	return nodes.Response{
		OK:     true,
		Code:   "ok",
		Node:   info.ID,
		Action: req.Action,
		Payload: map[string]interface{}{
			"transport": "clawgo-local",
			"agent_id":  remoteAgentID,
			"result":    result,
			"artifacts": artifacts,
		},
	}, nil
}

func executeNodeCameraSnap(ctx context.Context, info nodes.NodeInfo, req nodes.Request) (nodes.Response, error) {
	executor, err := getNodeLocalExecutor()
	if err != nil {
		return nodes.Response{}, err
	}
	outputPath, err := nodeCameraSnapFunc(ctx, executor.workspace, req.Args)
	if err != nil {
		return nodes.Response{}, err
	}
	artifact, err := buildNodeArtifact(executor.workspace, outputPath)
	if err != nil {
		return nodes.Response{}, err
	}
	return nodes.Response{
		OK:     true,
		Code:   "ok",
		Node:   info.ID,
		Action: req.Action,
		Payload: map[string]interface{}{
			"transport":  "clawgo-local",
			"media_type": "image",
			"storage":    artifact["storage"],
			"artifacts":  []map[string]interface{}{artifact},
			"meta": map[string]interface{}{
				"facing": stringArg(req.Args, "facing"),
			},
		},
	}, nil
}

func executeNodeScreenSnapshot(ctx context.Context, info nodes.NodeInfo, req nodes.Request) (nodes.Response, error) {
	executor, err := getNodeLocalExecutor()
	if err != nil {
		return nodes.Response{}, err
	}
	outputPath, err := nodeScreenSnapFunc(ctx, executor.workspace, req.Args)
	if err != nil {
		return nodes.Response{}, err
	}
	artifact, err := buildNodeArtifact(executor.workspace, outputPath)
	if err != nil {
		return nodes.Response{}, err
	}
	return nodes.Response{
		OK:     true,
		Code:   "ok",
		Node:   info.ID,
		Action: req.Action,
		Payload: map[string]interface{}{
			"transport":  "clawgo-local",
			"media_type": "image",
			"storage":    artifact["storage"],
			"artifacts":  []map[string]interface{}{artifact},
		},
	}, nil
}

func getNodeLocalExecutor() (*nodeLocalExecutor, error) {
	key := strings.TrimSpace(getConfigPath())
	if key == "" {
		return nil, fmt.Errorf("config path is required")
	}
	nodeLocalExecutorMu.Lock()
	defer nodeLocalExecutorMu.Unlock()
	if existing := nodeLocalExecutors[key]; existing != nil {
		return existing, nil
	}
	exec, err := nodeLocalExecutorFactory(key)
	if err != nil {
		return nil, err
	}
	nodeLocalExecutors[key] = exec
	return exec, nil
}

func newNodeLocalExecutor(configPath string) (*nodeLocalExecutor, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return nil, fmt.Errorf("config path is required")
	}
	return &nodeLocalExecutor{configPath: configPath}, nil
}

func (e *nodeLocalExecutor) Loop() (*agent.AgentLoop, error) {
	if e == nil {
		return nil, fmt.Errorf("node local executor is nil")
	}
	e.once.Do(func() {
		prev := globalConfigPathOverride
		globalConfigPathOverride = e.configPath
		defer func() { globalConfigPathOverride = prev }()

		cfg, err := loadConfig()
		if err != nil {
			e.err = err
			return
		}
		runtimecfg.Set(cfg)
		msgBus := bus.NewMessageBus()
		cronStorePath := filepath.Join(filepath.Dir(e.configPath), "cron", "jobs.json")
		cronService := cron.NewCronService(cronStorePath, nil)
		configureCronServiceRuntime(cronService, cfg)
		provider, err := nodeProviderFactory(cfg)
		if err != nil {
			e.err = err
			return
		}
		e.workspace = cfg.WorkspacePath()
		loop := nodeAgentLoopFactory(cfg, msgBus, provider, cronService)
		loop.SetConfigPath(e.configPath)
		e.loop = loop
	})
	if e.err != nil {
		return nil, e.err
	}
	if e.loop == nil {
		return nil, fmt.Errorf("node local executor unavailable")
	}
	return e.loop, nil
}

func stringArg(args map[string]interface{}, key string) string {
	if len(args) == 0 {
		return ""
	}
	value, ok := args[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func collectNodeArtifacts(workspace string, args map[string]interface{}) ([]map[string]interface{}, error) {
	paths := stringListArg(args, "artifact_paths")
	if len(paths) == 0 {
		return []map[string]interface{}{}, nil
	}
	root := strings.TrimSpace(workspace)
	if root == "" {
		return nil, fmt.Errorf("workspace path not configured")
	}
	out := make([]map[string]interface{}, 0, len(paths))
	for _, raw := range paths {
		artifact, err := buildNodeArtifact(root, raw)
		if err != nil {
			return nil, err
		}
		out = append(out, artifact)
	}
	return out, nil
}

func buildNodeArtifact(workspace, rawPath string) (map[string]interface{}, error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return nil, fmt.Errorf("artifact path is required")
	}
	clean := filepath.Clean(rawPath)
	fullPath := clean
	if !filepath.IsAbs(clean) {
		fullPath = filepath.Join(workspace, clean)
	}
	fullPath = filepath.Clean(fullPath)
	rel, err := filepath.Rel(workspace, fullPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return nil, fmt.Errorf("artifact path escapes workspace: %s", rawPath)
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("artifact path must be file: %s", rawPath)
	}
	artifact := map[string]interface{}{
		"name":        filepath.Base(fullPath),
		"kind":        nodeArtifactKindFromPath(fullPath),
		"source_path": filepath.ToSlash(rel),
		"size_bytes":  info.Size(),
	}
	if mimeType := mimeTypeForPath(fullPath); mimeType != "" {
		artifact["mime_type"] = mimeType
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, err
	}
	if shouldInlineAsText(fullPath, data, info.Mode()) {
		artifact["storage"] = "inline"
		artifact["content_text"] = string(data)
		return artifact, nil
	}
	artifact["storage"] = "inline"
	if len(data) > nodeArtifactInlineLimit {
		data = data[:nodeArtifactInlineLimit]
		artifact["truncated"] = true
	}
	artifact["content_base64"] = base64.StdEncoding.EncodeToString(data)
	return artifact, nil
}

func stringListArg(args map[string]interface{}, key string) []string {
	if len(args) == 0 {
		return nil
	}
	items, ok := args[key].([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(fmt.Sprint(item))
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func mimeTypeForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md":
		return "text/markdown"
	case ".txt", ".log", ".json", ".yaml", ".yml", ".xml", ".csv":
		return "text/plain"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".pdf":
		return "application/pdf"
	default:
		return ""
	}
}

func nodeArtifactKindFromPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return "image"
	case ".mp4", ".mov", ".webm":
		return "video"
	case ".pdf":
		return "document"
	default:
		return "file"
	}
}

func shouldInlineAsText(path string, data []byte, mode fs.FileMode) bool {
	if mode&fs.ModeType != 0 {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".txt", ".log", ".json", ".yaml", ".yml", ".xml", ".csv", ".go", ".ts", ".tsx", ".js", ".jsx", ".css", ".html", ".sh":
		return len(data) <= nodeArtifactInlineLimit
	default:
		return false
	}
}

func captureNodeCameraSnapshot(ctx context.Context, workspace string, args map[string]interface{}) (string, error) {
	outputPath, err := nodeMediaOutputPath(workspace, "camera", ".jpg", stringArg(args, "filename"))
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "linux":
		if _, err := os.Stat("/dev/video0"); err != nil {
			return "", fmt.Errorf("camera device /dev/video0 not found")
		}
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			return "", fmt.Errorf("ffmpeg not installed")
		}
		cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-f", "video4linux2", "-i", "/dev/video0", "-vframes", "1", "-q:v", "2", outputPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("camera capture failed: %v, output=%s", err, strings.TrimSpace(string(out)))
		}
		return outputPath, nil
	case "darwin":
		if _, err := exec.LookPath("imagesnap"); err != nil {
			return "", fmt.Errorf("imagesnap not installed")
		}
		cmd := exec.CommandContext(ctx, "imagesnap", "-q", outputPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("camera capture failed: %v, output=%s", err, strings.TrimSpace(string(out)))
		}
		return outputPath, nil
	default:
		return "", fmt.Errorf("camera_snap not supported on %s", runtime.GOOS)
	}
}

func captureNodeScreenSnapshot(ctx context.Context, workspace string, args map[string]interface{}) (string, error) {
	outputPath, err := nodeMediaOutputPath(workspace, "screen", ".png", stringArg(args, "filename"))
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.CommandContext(ctx, "screencapture", "-x", outputPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("screen capture failed: %v, output=%s", err, strings.TrimSpace(string(out)))
		}
		return outputPath, nil
	case "linux":
		candidates := [][]string{
			{"grim", outputPath},
			{"gnome-screenshot", "-f", outputPath},
			{"scrot", outputPath},
			{"import", "-window", "root", outputPath},
		}
		for _, candidate := range candidates {
			if _, err := exec.LookPath(candidate[0]); err != nil {
				continue
			}
			cmd := exec.CommandContext(ctx, candidate[0], candidate[1:]...)
			if out, err := cmd.CombinedOutput(); err == nil {
				return outputPath, nil
			} else if strings.TrimSpace(string(out)) != "" {
				continue
			}
		}
		return "", fmt.Errorf("no supported screen capture command found (grim, gnome-screenshot, scrot, import)")
	default:
		return "", fmt.Errorf("screen_snapshot not supported on %s", runtime.GOOS)
	}
}

func nodeMediaOutputPath(workspace, kind, ext, requested string) (string, error) {
	root := strings.TrimSpace(workspace)
	if root == "" {
		return "", fmt.Errorf("workspace path not configured")
	}
	baseDir := filepath.Join(root, "artifacts", "node")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", err
	}
	filename := strings.TrimSpace(requested)
	if filename == "" {
		filename = fmt.Sprintf("%s_%d%s", kind, time.Now().UnixNano(), ext)
	}
	filename = filepath.Clean(filename)
	if filepath.IsAbs(filename) {
		return "", fmt.Errorf("filename must be relative to workspace")
	}
	fullPath := filepath.Join(baseDir, filename)
	fullPath = filepath.Clean(fullPath)
	rel, err := filepath.Rel(root, fullPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("capture path escapes workspace")
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", err
	}
	return fullPath, nil
}

func structToWirePayload(v interface{}) map[string]interface{} {
	b, _ := json.Marshal(v)
	var out map[string]interface{}
	_ = json.Unmarshal(b, &out)
	if out == nil {
		out = map[string]interface{}{}
	}
	return out
}

func mapWirePayload(in map[string]interface{}, out interface{}) error {
	if len(in) == 0 {
		return fmt.Errorf("empty payload")
	}
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func postNodeRegister(ctx context.Context, client *http.Client, gatewayBase, token string, info nodes.NodeInfo) error {
	return postNodeJSON(ctx, client, gatewayBase, token, "/nodes/register", info)
}

func postNodeHeartbeat(ctx context.Context, client *http.Client, gatewayBase, token, id string) error {
	return postNodeJSON(ctx, client, gatewayBase, token, "/nodes/heartbeat", map[string]string{"id": strings.TrimSpace(id)})
}

func postNodeJSON(ctx context.Context, client *http.Client, gatewayBase, token, path string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(gatewayBase, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var out bytes.Buffer
		_, _ = out.ReadFrom(resp.Body)
		msg := strings.TrimSpace(out.String())
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("http %d: %s", resp.StatusCode, msg)
	}
	return nil
}

func defaultGatewayBase(cfg *config.Config) string {
	if raw := strings.TrimSpace(os.Getenv("CLAWGO_GATEWAY_URL")); raw != "" {
		return normalizeGatewayBase(raw)
	}
	host := "127.0.0.1"
	port := 18790
	if cfg != nil {
		if v := strings.TrimSpace(cfg.Gateway.Host); v != "" && v != "0.0.0.0" && v != "::" {
			host = v
		}
		if cfg.Gateway.Port > 0 {
			port = cfg.Gateway.Port
		}
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

func defaultGatewayToken(cfg *config.Config) string {
	if v := strings.TrimSpace(os.Getenv("CLAWGO_GATEWAY_TOKEN")); v != "" {
		return v
	}
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Gateway.Token)
}

func normalizeGatewayBase(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "http://" + raw
	}
	return strings.TrimRight(raw, "/")
}

func nodeWebsocketURL(gatewayBase string) string {
	base := normalizeGatewayBase(gatewayBase)
	base = strings.TrimPrefix(base, "http://")
	base = strings.TrimPrefix(base, "https://")
	scheme := "ws://"
	if strings.HasPrefix(strings.TrimSpace(gatewayBase), "https://") {
		scheme = "wss://"
	}
	return scheme + base + "/nodes/connect"
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func capabilitiesFromCSV(raw string) nodes.Capabilities {
	caps := nodes.Capabilities{}
	for _, item := range splitCSV(raw) {
		switch strings.ToLower(item) {
		case "run":
			caps.Run = true
		case "invoke":
			caps.Invoke = true
		case "model", "agent_task":
			caps.Model = true
		case "camera":
			caps.Camera = true
		case "screen":
			caps.Screen = true
		case "location":
			caps.Location = true
		case "canvas":
			caps.Canvas = true
		}
	}
	return caps
}
