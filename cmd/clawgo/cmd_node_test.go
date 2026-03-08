package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawgo/pkg/agent"
	"clawgo/pkg/config"
	"clawgo/pkg/nodes"
	"clawgo/pkg/providers"
)

type stubNodeProvider struct {
	content string
}

func (p stubNodeProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	return &providers.LLMResponse{Content: p.content, FinishReason: "stop"}, nil
}

func (p stubNodeProvider) GetDefaultModel() string {
	return "stub-model"
}

func TestParseNodeRegisterArgsDefaults(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.Gateway.Host = "gateway.example"
	cfg.Gateway.Port = 7788
	cfg.Gateway.Token = "cfg-token"

	opts, err := parseNodeRegisterArgs([]string{"--id", "edge-dev"}, cfg)
	if err != nil {
		t.Fatalf("parseNodeRegisterArgs failed: %v", err)
	}
	if opts.GatewayBase != "http://gateway.example:7788" {
		t.Fatalf("unexpected gateway base: %s", opts.GatewayBase)
	}
	if opts.Token != "cfg-token" {
		t.Fatalf("unexpected token: %s", opts.Token)
	}
	if opts.ID != "edge-dev" {
		t.Fatalf("unexpected id: %s", opts.ID)
	}
	if !opts.Capabilities.Run || !opts.Capabilities.Invoke || !opts.Capabilities.Model {
		t.Fatalf("expected default run/invoke/model capabilities, got %+v", opts.Capabilities)
	}
}

func TestPostNodeRegisterSendsNodeInfo(t *testing.T) {
	t.Parallel()

	var gotAuth string
	var got nodes.NodeInfo
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/nodes/register" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	info := nodes.NodeInfo{
		ID:       "edge-dev",
		Name:     "Edge Dev",
		Endpoint: "http://edge.example:18790",
		Capabilities: nodes.Capabilities{
			Run: true, Invoke: true, Model: true,
		},
		Actions: []string{"run", "agent_task"},
		Models:  []string{"gpt-4o-mini"},
	}
	client := &http.Client{Timeout: 2 * time.Second}
	if err := postNodeRegister(context.Background(), client, srv.URL, "secret", info); err != nil {
		t.Fatalf("postNodeRegister failed: %v", err)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("unexpected auth header: %s", gotAuth)
	}
	if got.ID != "edge-dev" || got.Endpoint != "http://edge.example:18790" {
		t.Fatalf("unexpected node payload: %+v", got)
	}
	if len(got.Actions) != 2 || got.Actions[1] != "agent_task" {
		t.Fatalf("unexpected actions: %+v", got.Actions)
	}
}

func TestPostNodeHeartbeatSendsNodeID(t *testing.T) {
	t.Parallel()

	var body map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/nodes/heartbeat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	if err := postNodeHeartbeat(context.Background(), client, srv.URL, "", "edge-dev"); err != nil {
		t.Fatalf("postNodeHeartbeat failed: %v", err)
	}
	if strings.TrimSpace(body["id"]) != "edge-dev" {
		t.Fatalf("unexpected heartbeat body: %+v", body)
	}
}

func TestNodeAgentsFromConfigCollectsEnabledAgents(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.Agents.Subagents["main"] = config.SubagentConfig{
		Enabled:     true,
		Type:        "router",
		DisplayName: "Main Agent",
		Role:        "orchestrator",
	}
	cfg.Agents.Subagents["coder"] = config.SubagentConfig{
		Enabled:     true,
		Type:        "worker",
		DisplayName: "Code Agent",
		Role:        "code",
	}
	cfg.Agents.Subagents["tester"] = config.SubagentConfig{
		Enabled:     false,
		Type:        "worker",
		DisplayName: "Test Agent",
		Role:        "test",
	}
	items := nodeAgentsFromConfig(cfg)
	if len(items) != 2 {
		t.Fatalf("expected 2 enabled agents, got %+v", items)
	}
	if items[0].ID != "coder" || items[1].ID != "main" {
		t.Fatalf("unexpected agent export order: %+v", items)
	}
}

func TestNodeWebsocketURL(t *testing.T) {
	t.Parallel()

	if got := nodeWebsocketURL("http://gateway.example:18790"); got != "ws://gateway.example:18790/nodes/connect" {
		t.Fatalf("unexpected ws url: %s", got)
	}
	if got := nodeWebsocketURL("https://gateway.example"); got != "wss://gateway.example/nodes/connect" {
		t.Fatalf("unexpected wss url: %s", got)
	}
}

func TestNodeSocketPingInterval(t *testing.T) {
	t.Parallel()

	if got := nodeSocketPingInterval(120); got != 25*time.Second {
		t.Fatalf("expected 25s cap, got %s", got)
	}
	if got := nodeSocketPingInterval(20); got != 10*time.Second {
		t.Fatalf("expected 10s floor, got %s", got)
	}
	if got := nodeSocketPingInterval(30); got != 15*time.Second {
		t.Fatalf("expected half heartbeat, got %s", got)
	}
}

func TestExecuteNodeRequestRunsLocalMainAgentTask(t *testing.T) {
	prevCfg := globalConfigPathOverride
	prevProviderFactory := nodeProviderFactory
	prevLoopFactory := nodeAgentLoopFactory
	prevExecutors := nodeLocalExecutors
	globalConfigPathOverride = filepath.Join(t.TempDir(), "config.json")
	nodeLocalExecutors = map[string]*nodeLocalExecutor{}
	nodeProviderFactory = func(cfg *config.Config) (providers.LLMProvider, error) {
		return stubNodeProvider{content: "main-local-ok"}, nil
	}
	nodeAgentLoopFactory = agent.NewAgentLoop
	defer func() {
		globalConfigPathOverride = prevCfg
		nodeProviderFactory = prevProviderFactory
		nodeAgentLoopFactory = prevLoopFactory
		nodeLocalExecutors = prevExecutors
	}()

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = filepath.Join(t.TempDir(), "workspace")
	cfg.Agents.Subagents["main"] = config.SubagentConfig{
		Enabled: true,
		Type:    "router",
		Role:    "orchestrator",
	}
	if err := config.SaveConfig(globalConfigPathOverride, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	info := nodes.NodeInfo{ID: "edge-a", Name: "Edge A"}
	resp := executeNodeRequest(context.Background(), &http.Client{Timeout: time.Second}, info, nodeRegisterOptions{}, &nodes.Request{
		Action: "agent_task",
		Task:   "say ok",
	})
	if !resp.OK {
		t.Fatalf("expected ok response, got %+v", resp)
	}
	if got := strings.TrimSpace(resp.Payload["result"].(string)); got != "main-local-ok" {
		t.Fatalf("unexpected result: %+v", resp.Payload)
	}
	if got := strings.TrimSpace(resp.Payload["agent_id"].(string)); got != "main" {
		t.Fatalf("unexpected agent id: %+v", resp.Payload)
	}
}

func TestExecuteNodeRequestRunsLocalSubagentTask(t *testing.T) {
	prevCfg := globalConfigPathOverride
	prevProviderFactory := nodeProviderFactory
	prevLoopFactory := nodeAgentLoopFactory
	prevExecutors := nodeLocalExecutors
	globalConfigPathOverride = filepath.Join(t.TempDir(), "config.json")
	nodeLocalExecutors = map[string]*nodeLocalExecutor{}
	nodeProviderFactory = func(cfg *config.Config) (providers.LLMProvider, error) {
		return stubNodeProvider{content: "coder-local-ok"}, nil
	}
	nodeAgentLoopFactory = agent.NewAgentLoop
	defer func() {
		globalConfigPathOverride = prevCfg
		nodeProviderFactory = prevProviderFactory
		nodeAgentLoopFactory = prevLoopFactory
		nodeLocalExecutors = prevExecutors
	}()

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = filepath.Join(t.TempDir(), "workspace")
	cfg.Agents.Subagents["main"] = config.SubagentConfig{
		Enabled: true,
		Type:    "router",
		Role:    "orchestrator",
	}
	cfg.Agents.Subagents["coder"] = config.SubagentConfig{
		Enabled: true,
		Type:    "worker",
		Role:    "code",
	}
	if err := os.MkdirAll(filepath.Join(cfg.Agents.Defaults.Workspace, "out"), 0755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Agents.Defaults.Workspace, "out", "result.txt"), []byte("artifact-body"), 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := config.SaveConfig(globalConfigPathOverride, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	info := nodes.NodeInfo{ID: "edge-b", Name: "Edge B"}
	resp := executeNodeRequest(context.Background(), &http.Client{Timeout: time.Second}, info, nodeRegisterOptions{}, &nodes.Request{
		Action: "agent_task",
		Task:   "write tests",
		Args:   map[string]interface{}{"remote_agent_id": "coder", "artifact_paths": []interface{}{"out/result.txt"}},
	})
	if !resp.OK {
		t.Fatalf("expected ok response, got %+v", resp)
	}
	if got := strings.TrimSpace(resp.Payload["result"].(string)); !strings.Contains(got, "coder-local-ok") {
		t.Fatalf("unexpected result: %+v", resp.Payload)
	}
	if got := strings.TrimSpace(resp.Payload["agent_id"].(string)); got != "coder" {
		t.Fatalf("unexpected agent id: %+v", resp.Payload)
	}
	artifacts, ok := resp.Payload["artifacts"].([]map[string]interface{})
	if !ok || len(artifacts) != 1 {
		t.Fatalf("expected one artifact, got %+v", resp.Payload["artifacts"])
	}
	if artifacts[0]["content_text"] != "artifact-body" {
		t.Fatalf("unexpected artifact payload: %+v", artifacts[0])
	}
}

func TestCollectNodeArtifactsRejectsPathEscape(t *testing.T) {
	t.Parallel()

	_, err := collectNodeArtifacts(t.TempDir(), map[string]interface{}{
		"artifact_paths": []interface{}{"../secret.txt"},
	})
	if err == nil || !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("expected workspace escape error, got %v", err)
	}
}

func TestExecuteNodeRequestRunsLocalCameraSnap(t *testing.T) {
	prevCfg := globalConfigPathOverride
	prevExecutors := nodeLocalExecutors
	prevCamera := nodeCameraSnapFunc
	globalConfigPathOverride = filepath.Join(t.TempDir(), "config.json")
	nodeLocalExecutors = map[string]*nodeLocalExecutor{}
	defer func() {
		globalConfigPathOverride = prevCfg
		nodeLocalExecutors = prevExecutors
		nodeCameraSnapFunc = prevCamera
	}()

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = filepath.Join(t.TempDir(), "workspace")
	if err := config.SaveConfig(globalConfigPathOverride, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	nodeCameraSnapFunc = func(ctx context.Context, workspace string, args map[string]interface{}) (string, error) {
		out := filepath.Join(workspace, "artifacts", "node", "camera-test.jpg")
		if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
			return "", err
		}
		if err := os.WriteFile(out, []byte("camera-bytes"), 0644); err != nil {
			return "", err
		}
		return out, nil
	}

	info := nodes.NodeInfo{ID: "edge-cam", Name: "Edge Cam"}
	resp := executeNodeRequest(context.Background(), &http.Client{Timeout: time.Second}, info, nodeRegisterOptions{}, &nodes.Request{
		Action: "camera_snap",
		Args:   map[string]interface{}{"facing": "front"},
	})
	if !resp.OK {
		t.Fatalf("expected ok response, got %+v", resp)
	}
	artifacts, ok := resp.Payload["artifacts"].([]map[string]interface{})
	if !ok || len(artifacts) != 1 {
		t.Fatalf("expected one artifact, got %+v", resp.Payload["artifacts"])
	}
	if artifacts[0]["name"] != "camera-test.jpg" {
		t.Fatalf("unexpected artifact: %+v", artifacts[0])
	}
}

func TestExecuteNodeRequestRunsLocalScreenSnapshot(t *testing.T) {
	prevCfg := globalConfigPathOverride
	prevExecutors := nodeLocalExecutors
	prevScreen := nodeScreenSnapFunc
	globalConfigPathOverride = filepath.Join(t.TempDir(), "config.json")
	nodeLocalExecutors = map[string]*nodeLocalExecutor{}
	defer func() {
		globalConfigPathOverride = prevCfg
		nodeLocalExecutors = prevExecutors
		nodeScreenSnapFunc = prevScreen
	}()

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = filepath.Join(t.TempDir(), "workspace")
	if err := config.SaveConfig(globalConfigPathOverride, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	nodeScreenSnapFunc = func(ctx context.Context, workspace string, args map[string]interface{}) (string, error) {
		out := filepath.Join(workspace, "artifacts", "node", "screen-test.png")
		if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
			return "", err
		}
		if err := os.WriteFile(out, []byte{0x89, 0x50, 0x4e, 0x47}, 0644); err != nil {
			return "", err
		}
		return out, nil
	}

	info := nodes.NodeInfo{ID: "edge-screen", Name: "Edge Screen"}
	resp := executeNodeRequest(context.Background(), &http.Client{Timeout: time.Second}, info, nodeRegisterOptions{}, &nodes.Request{
		Action: "screen_snapshot",
	})
	if !resp.OK {
		t.Fatalf("expected ok response, got %+v", resp)
	}
	artifacts, ok := resp.Payload["artifacts"].([]map[string]interface{})
	if !ok || len(artifacts) != 1 {
		t.Fatalf("expected one artifact, got %+v", resp.Payload["artifacts"])
	}
	if artifacts[0]["name"] != "screen-test.png" {
		t.Fatalf("unexpected artifact: %+v", artifacts[0])
	}
}
