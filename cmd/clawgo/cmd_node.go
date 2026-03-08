package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawgo/pkg/config"
	"clawgo/pkg/nodes"
	"github.com/gorilla/websocket"
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
	Capabilities nodes.Capabilities
	Watch        bool
	HeartbeatSec int
}

type nodeHeartbeatOptions struct {
	GatewayBase string
	Token       string
	ID          string
}

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
	}
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
			fmt.Printf("ℹ Signal received: type=%s from=%s session=%s\n", msg.Type, strings.TrimSpace(msg.From), strings.TrimSpace(msg.Session))
		}
	}
}

func handleNodeWireRequest(ctx context.Context, writeJSON func(interface{}) error, client *http.Client, info nodes.NodeInfo, opts nodeRegisterOptions, msg nodes.WireMessage) {
	resp := nodes.Response{
		OK:     false,
		Code:   "invalid_request",
		Node:   info.ID,
		Action: "",
		Error:  "request missing",
	}
	if msg.Request != nil {
		req := *msg.Request
		resp.Action = req.Action
		if strings.TrimSpace(opts.Endpoint) == "" {
			resp.Error = "node endpoint not configured"
			resp.Code = "endpoint_missing"
		} else {
			if req.Node == "" {
				req.Node = info.ID
			}
			execResp, err := nodes.DoEndpointRequest(ctx, client, opts.Endpoint, opts.NodeToken, req)
			if err != nil {
				resp = nodes.Response{
					OK:     false,
					Code:   "transport_error",
					Node:   info.ID,
					Action: req.Action,
					Error:  err.Error(),
				}
			} else {
				resp = execResp
				if strings.TrimSpace(resp.Node) == "" {
					resp.Node = info.ID
				}
			}
		}
	}
	_ = writeJSON(nodes.WireMessage{
		Type:     "node_response",
		ID:       msg.ID,
		From:     info.ID,
		To:       strings.TrimSpace(msg.From),
		Session:  strings.TrimSpace(msg.Session),
		Response: &resp,
	})
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
