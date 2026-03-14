// ClawGo - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 ClawGo contributors

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/cron"
	"github.com/YspCoder/clawgo/pkg/ekg"
	"github.com/YspCoder/clawgo/pkg/logger"
	"github.com/YspCoder/clawgo/pkg/nodes"
	"github.com/YspCoder/clawgo/pkg/providers"
	"github.com/YspCoder/clawgo/pkg/runtimecfg"
	"github.com/YspCoder/clawgo/pkg/scheduling"
	"github.com/YspCoder/clawgo/pkg/session"
	"github.com/YspCoder/clawgo/pkg/tools"
)

type AgentLoop struct {
	bus                  *bus.MessageBus
	cfg                  *config.Config
	provider             providers.LLMProvider
	workspace            string
	model                string
	maxIterations        int
	sessions             *session.SessionManager
	contextBuilder       *ContextBuilder
	tools                *tools.ToolRegistry
	compactionEnabled    bool
	compactionTrigger    int
	compactionKeepRecent int
	heartbeatAckMaxChars int
	heartbeatAckToken    string
	audit                *triggerAudit
	running              bool
	sessionScheduler     *SessionScheduler
	providerChain        []providerCandidate
	providerNames        []string
	providerPool         map[string]providers.LLMProvider
	providerResponses    map[string]config.ProviderResponsesConfig
	telegramStreaming    bool
	ekg                  *ekg.Engine
	providerMu           sync.RWMutex
	sessionProvider      map[string]string
	streamMu             sync.Mutex
	sessionStreamed      map[string]bool
	subagentManager      *tools.SubagentManager
	subagentRouter       *tools.SubagentRouter
	subagentConfigTool   *tools.SubagentConfigTool
	nodeRouter           *nodes.Router
	configPath           string
	subagentDigestMu     sync.Mutex
	subagentDigestDelay  time.Duration
	subagentDigests      map[string]*subagentDigestState
}

type providerCandidate struct {
	ref   string
	name  string
	model string
}

type subagentDigestItem struct {
	agentID       string
	reason        string
	status        string
	taskSummary   string
	resultSummary string
}

type subagentDigestState struct {
	channel string
	chatID  string
	items   map[string]subagentDigestItem
	dueAt   time.Time
}

func (al *AgentLoop) SetConfigPath(path string) {
	if al == nil {
		return
	}
	al.configPath = strings.TrimSpace(path)
	if al.subagentConfigTool != nil {
		al.subagentConfigTool.SetConfigPath(al.configPath)
	}
}

func (al *AgentLoop) SetNodeP2PTransport(t nodes.Transport) {
	if al == nil || al.nodeRouter == nil {
		return
	}
	al.nodeRouter.P2P = t
}

func (al *AgentLoop) DispatchNodeRequest(ctx context.Context, req nodes.Request, mode string) (nodes.Response, error) {
	if al == nil || al.tools == nil {
		return nodes.Response{}, fmt.Errorf("agent loop not ready")
	}
	args := map[string]interface{}{
		"action": req.Action,
		"node":   req.Node,
		"mode":   mode,
		"task":   req.Task,
		"model":  req.Model,
	}
	if len(req.Args) > 0 {
		args["args"] = req.Args
	}
	out, err := al.tools.Execute(ctx, "nodes", args)
	if err != nil {
		return nodes.Response{}, err
	}
	var resp nodes.Response
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return nodes.Response{}, err
	}
	return resp, nil
}

// StartupCompactionReport provides startup memory/session maintenance stats.
type StartupCompactionReport struct {
	TotalSessions     int `json:"total_sessions"`
	CompactedSessions int `json:"compacted_sessions"`
}

func NewAgentLoop(cfg *config.Config, msgBus *bus.MessageBus, provider providers.LLMProvider, cs *cron.CronService) *AgentLoop {
	workspace := cfg.WorkspacePath()
	os.MkdirAll(workspace, 0755)

	sessionsManager := session.NewSessionManager(filepath.Join(filepath.Dir(cfg.WorkspacePath()), "agents", "main", "sessions"))

	toolsRegistry := tools.NewToolRegistry()
	processManager := tools.NewProcessManager(workspace)
	readTool := tools.NewReadFileTool(workspace)
	writeTool := tools.NewWriteFileTool(workspace)
	listTool := tools.NewListDirTool(workspace)
	toolsRegistry.Register(readTool)
	toolsRegistry.Register(writeTool)
	toolsRegistry.Register(listTool)
	// OpenClaw-compatible aliases
	toolsRegistry.Register(tools.NewAliasTool("read", "Read file content (OpenClaw-compatible alias of read_file)", readTool, map[string]string{"file_path": "path"}))
	toolsRegistry.Register(tools.NewAliasTool("write", "Write file content (OpenClaw-compatible alias of write_file)", writeTool, map[string]string{"file_path": "path"}))
	toolsRegistry.Register(tools.NewAliasTool("edit", "Edit file content (OpenClaw-compatible alias of edit_file)", tools.NewEditFileTool(workspace), map[string]string{"file_path": "path", "old_string": "oldText", "new_string": "newText"}))
	toolsRegistry.Register(tools.NewExecTool(cfg.Tools.Shell, workspace, processManager))
	toolsRegistry.Register(tools.NewProcessTool(processManager))
	toolsRegistry.Register(tools.NewSkillExecTool(workspace))
	nodesManager := nodes.DefaultManager()
	nodesManager.SetAuditPath(filepath.Join(workspace, "memory", "nodes-audit.jsonl"))
	nodesManager.SetStatePath(filepath.Join(workspace, "memory", "nodes-state.json"))
	nodesManager.Upsert(nodes.NodeInfo{ID: "local", Name: "local", Capabilities: nodes.Capabilities{Run: true, Invoke: true, Model: true, Camera: true, Screen: true, Location: true, Canvas: true}, Models: []string{"local-sim"}, Online: true})
	nodesManager.RegisterHandler("local", func(req nodes.Request) nodes.Response {
		switch req.Action {
		case "run":
			payload := map[string]interface{}{"transport": "relay-local", "simulated": true}
			if cmdRaw, ok := req.Args["command"].([]interface{}); ok && len(cmdRaw) > 0 {
				parts := make([]string, 0, len(cmdRaw))
				for _, x := range cmdRaw {
					parts = append(parts, fmt.Sprint(x))
				}
				payload["command"] = parts
			}
			return nodes.Response{OK: true, Code: "ok", Node: "local", Action: req.Action, Payload: payload}
		case "agent_task":
			return nodes.Response{OK: true, Code: "ok", Node: "local", Action: req.Action, Payload: map[string]interface{}{"transport": "relay-local", "simulated": true, "model": req.Model, "task": req.Task, "result": "local child-model simulated execution completed"}}
		case "camera_snap":
			return nodes.Response{OK: true, Code: "ok", Node: "local", Action: req.Action, Payload: map[string]interface{}{"transport": "relay-local", "media_type": "image", "storage": "inline", "facing": req.Args["facing"], "simulated": true, "meta": map[string]interface{}{"width": 1280, "height": 720}}}
		case "camera_clip":
			return nodes.Response{OK: true, Code: "ok", Node: "local", Action: req.Action, Payload: map[string]interface{}{"transport": "relay-local", "media_type": "video", "storage": "path", "path": "/tmp/camera_clip.mp4", "duration_ms": req.Args["duration_ms"], "simulated": true, "meta": map[string]interface{}{"fps": 30}}}
		case "screen_snapshot":
			return nodes.Response{OK: true, Code: "ok", Node: "local", Action: req.Action, Payload: map[string]interface{}{"transport": "relay-local", "media_type": "image", "storage": "inline", "simulated": true, "meta": map[string]interface{}{"width": 1920, "height": 1080}}}
		case "screen_record":
			return nodes.Response{OK: true, Code: "ok", Node: "local", Action: req.Action, Payload: map[string]interface{}{"transport": "relay-local", "media_type": "video", "storage": "path", "path": "/tmp/screen_record.mp4", "duration_ms": req.Args["duration_ms"], "simulated": true, "meta": map[string]interface{}{"fps": 30}}}
		case "location_get":
			return nodes.Response{OK: true, Code: "ok", Node: "local", Action: req.Action, Payload: map[string]interface{}{"transport": "relay-local", "lat": 0.0, "lng": 0.0, "accuracy": "simulated", "meta": map[string]interface{}{"provider": "simulated"}}}
		case "canvas_snapshot":
			return nodes.Response{OK: true, Code: "ok", Node: "local", Action: req.Action, Payload: map[string]interface{}{"transport": "relay-local", "image": "data:image/png;base64,<simulated>", "media_type": "image", "storage": "inline", "simulated": true, "meta": map[string]interface{}{"width": 1280, "height": 720}}}
		case "canvas_action":
			return nodes.Response{OK: true, Code: "ok", Node: "local", Action: req.Action, Payload: map[string]interface{}{"transport": "relay-local", "applied": true, "simulated": true, "args": req.Args}}
		default:
			return nodes.Response{OK: false, Code: "unsupported_action", Node: "local", Action: req.Action, Error: "unsupported local simulated action"}
		}
	})
	nodeDispatchPolicy := nodes.DispatchPolicy{
		PreferLocal:        cfg.Gateway.Nodes.Dispatch.PreferLocal,
		PreferP2P:          cfg.Gateway.Nodes.Dispatch.PreferP2P,
		AllowRelayFallback: cfg.Gateway.Nodes.Dispatch.AllowRelayFallback,
		ActionTags:         cfg.Gateway.Nodes.Dispatch.ActionTags,
		AgentTags:          cfg.Gateway.Nodes.Dispatch.AgentTags,
		AllowActions:       cfg.Gateway.Nodes.Dispatch.AllowActions,
		DenyActions:        cfg.Gateway.Nodes.Dispatch.DenyActions,
		AllowAgents:        cfg.Gateway.Nodes.Dispatch.AllowAgents,
		DenyAgents:         cfg.Gateway.Nodes.Dispatch.DenyAgents,
	}
	nodesManager.SetDispatchPolicy(nodeDispatchPolicy)
	var nodeP2P nodes.Transport
	if cfg.Gateway.Nodes.P2P.Enabled {
		switch strings.ToLower(strings.TrimSpace(cfg.Gateway.Nodes.P2P.Transport)) {
		case "", "websocket_tunnel":
			nodeP2P = &nodes.WebsocketP2PTransport{Manager: nodesManager}
		case "webrtc":
			// Keep the mode explicit but non-default until a direct data channel is production-ready.
			nodeP2P = &nodes.WebsocketP2PTransport{Manager: nodesManager}
		}
	}
	nodesRouter := &nodes.Router{P2P: nodeP2P, Relay: &nodes.HTTPRelayTransport{Manager: nodesManager}, Policy: nodesManager.DispatchPolicy()}
	toolsRegistry.Register(tools.NewNodesTool(nodesManager, nodesRouter, filepath.Join(workspace, "memory", "nodes-dispatch-audit.jsonl")))

	if cs != nil {
		toolsRegistry.Register(tools.NewRemindTool(cs))
		toolsRegistry.Register(tools.NewCronTool(cs))
	}

	maxParallelCalls := cfg.Agents.Defaults.Execution.ToolMaxParallelCalls
	if maxParallelCalls <= 0 {
		maxParallelCalls = 4
	}
	parallelSafe := make(map[string]struct{})
	for _, name := range cfg.Agents.Defaults.Execution.ToolParallelSafeNames {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			parallelSafe[trimmed] = struct{}{}
		}
	}

	braveAPIKey := cfg.Tools.Web.Search.APIKey
	toolsRegistry.Register(tools.NewWebSearchTool(braveAPIKey, cfg.Tools.Web.Search.MaxResults))
	webFetchTool := tools.NewWebFetchTool(50000)
	toolsRegistry.Register(webFetchTool)
	toolsRegistry.Register(tools.NewParallelFetchTool(webFetchTool, maxParallelCalls, parallelSafe))
	if cfg.Tools.MCP.Enabled {
		mcpTool := tools.NewMCPTool(workspace, cfg.Tools.MCP)
		toolsRegistry.Register(mcpTool)
		discoveryCtx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Tools.MCP.RequestTimeoutSec)*time.Second)
		for _, remoteTool := range mcpTool.DiscoverTools(discoveryCtx) {
			toolsRegistry.Register(remoteTool)
		}
		cancel()
	}

	// Register message tool
	messageTool := tools.NewMessageTool()
	messageTool.SetSendCallback(func(channel, chatID, action, content, media, messageID, emoji string, buttons [][]bus.Button) error {
		msgBus.PublishOutbound(bus.OutboundMessage{
			Channel:   channel,
			ChatID:    chatID,
			Content:   content,
			Media:     media,
			Buttons:   buttons,
			Action:    action,
			MessageID: messageID,
			Emoji:     emoji,
		})
		return nil
	})
	toolsRegistry.Register(messageTool)

	// Register spawn tool
	subagentManager := tools.NewSubagentManager(provider, workspace, msgBus)
	subagentRouter := tools.NewSubagentRouter(subagentManager)
	subagentConfigTool := tools.NewSubagentConfigTool("")
	spawnTool := tools.NewSpawnTool(subagentManager)
	toolsRegistry.Register(spawnTool)
	toolsRegistry.Register(tools.NewSubagentsTool(subagentManager))
	toolsRegistry.Register(subagentConfigTool)
	if store := subagentManager.ProfileStore(); store != nil {
		toolsRegistry.Register(tools.NewSubagentProfileTool(store))
	}
	toolsRegistry.Register(tools.NewSessionsTool(
		func(limit int) []tools.SessionInfo {
			sessions := alSessionListForTool(sessionsManager, limit)
			return sessions
		},
		func(key string, limit int) []providers.Message {
			h := sessionsManager.GetHistory(key)
			if limit > 0 && len(h) > limit {
				return h[len(h)-limit:]
			}
			return h
		},
	))

	// Register edit file tool
	editFileTool := tools.NewEditFileTool(workspace)
	toolsRegistry.Register(editFileTool)

	// Register memory tools
	memorySearchTool := tools.NewMemorySearchTool(workspace)
	toolsRegistry.Register(memorySearchTool)
	toolsRegistry.Register(tools.NewMemoryGetTool(workspace))
	toolsRegistry.Register(tools.NewMemoryWriteTool(workspace))

	// Register parallel execution tool (leveraging Go's concurrency)
	toolsRegistry.Register(tools.NewParallelTool(toolsRegistry, maxParallelCalls, parallelSafe))

	// Register browser tool (integrated Chromium support)
	toolsRegistry.Register(tools.NewBrowserTool())

	// Register camera tool
	toolsRegistry.Register(tools.NewCameraTool(workspace))
	// Register system info tool
	toolsRegistry.Register(tools.NewSystemInfoTool())

	loop := &AgentLoop{
		bus:                  msgBus,
		cfg:                  cfg,
		provider:             provider,
		workspace:            workspace,
		model:                provider.GetDefaultModel(),
		maxIterations:        cfg.Agents.Defaults.MaxToolIterations,
		sessions:             sessionsManager,
		contextBuilder:       NewContextBuilder(workspace, func() []string { return toolsRegistry.GetSummaries() }),
		tools:                toolsRegistry,
		compactionEnabled:    cfg.Agents.Defaults.ContextCompaction.Enabled,
		compactionTrigger:    cfg.Agents.Defaults.ContextCompaction.TriggerMessages,
		compactionKeepRecent: cfg.Agents.Defaults.ContextCompaction.KeepRecentMessages,
		heartbeatAckMaxChars: cfg.Agents.Defaults.Heartbeat.AckMaxChars,
		heartbeatAckToken:    loadHeartbeatAckToken(workspace),
		audit:                newTriggerAudit(workspace),
		running:              false,
		sessionScheduler:     NewSessionScheduler(0),
		ekg:                  ekg.New(workspace),
		sessionProvider:      map[string]string{},
		sessionStreamed:      map[string]bool{},
		providerResponses:    map[string]config.ProviderResponsesConfig{},
		telegramStreaming:    cfg.Channels.Telegram.Streaming,
		subagentManager:      subagentManager,
		subagentRouter:       subagentRouter,
		subagentConfigTool:   subagentConfigTool,
		nodeRouter:           nodesRouter,
		subagentDigestDelay:  5 * time.Second,
		subagentDigests:      map[string]*subagentDigestState{},
	}
	if _, primaryModel := config.ParseProviderModelRef(cfg.Agents.Defaults.Model.Primary); strings.TrimSpace(primaryModel) != "" {
		loop.model = strings.TrimSpace(primaryModel)
	}
	go loop.runSubagentDigestTicker()
	// Initialize provider fallback chain (primary + inferred providers).
	loop.providerChain = []providerCandidate{}
	loop.providerPool = map[string]providers.LLMProvider{}
	loop.providerNames = []string{}
	primaryName := config.PrimaryProviderName(cfg)
	primaryRef := strings.TrimSpace(cfg.Agents.Defaults.Model.Primary)
	if primaryRef == "" {
		primaryRef = primaryName + "/" + loop.model
	}
	loop.providerPool[primaryName] = provider
	loop.providerChain = append(loop.providerChain, providerCandidate{
		ref:   primaryRef,
		name:  primaryName,
		model: loop.model,
	})
	loop.providerNames = append(loop.providerNames, primaryName)
	if pc, ok := config.ProviderConfigByName(cfg, primaryName); ok {
		loop.providerResponses[primaryName] = pc.Responses
	}
	seenProviders := map[string]struct{}{primaryName: {}}
	providerConfigs := config.AllProviderConfigs(cfg)
	providerOrder := make([]string, 0, len(providerConfigs))
	for name := range providerConfigs {
		normalized := strings.TrimSpace(name)
		if normalized == "" {
			continue
		}
		providerOrder = append(providerOrder, normalized)
	}
	sort.SliceStable(providerOrder, func(i, j int) bool {
		ni := normalizeFallbackProviderName(providerOrder[i])
		nj := normalizeFallbackProviderName(providerOrder[j])
		pi := automaticFallbackPriority(ni)
		pj := automaticFallbackPriority(nj)
		if pi == pj {
			return ni < nj
		}
		return pi < pj
	})
	for _, rawName := range providerOrder {
		providerName := strings.TrimSpace(rawName)
		if providerName == "" {
			continue
		}
		providerName, _ = config.ParseProviderModelRef(providerName + "/_")
		if providerName == "" {
			continue
		}
		if _, dup := seenProviders[providerName]; dup {
			continue
		}
		modelName := ""
		if pc, ok := config.ProviderConfigByName(cfg, providerName); ok {
			if len(pc.Models) > 0 {
				modelName = strings.TrimSpace(pc.Models[0])
			}
			loop.providerResponses[providerName] = pc.Responses
		}
		seenProviders[providerName] = struct{}{}
		loop.providerNames = append(loop.providerNames, providerName)
		ref := providerName
		if modelName != "" {
			ref += "/" + modelName
		}
		loop.providerChain = append(loop.providerChain, providerCandidate{
			ref:   ref,
			name:  providerName,
			model: modelName,
		})
	}

	// Inject recursive run logic so subagents can use full tool-calling flows.
	subagentManager.SetRunFunc(func(ctx context.Context, task *tools.SubagentTask) (string, error) {
		if task == nil {
			return "", fmt.Errorf("subagent task is nil")
		}
		if strings.EqualFold(strings.TrimSpace(task.Transport), "node") {
			return loop.dispatchNodeSubagentTask(ctx, task)
		}
		sessionKey := strings.TrimSpace(task.SessionKey)
		if sessionKey == "" {
			sessionKey = fmt.Sprintf("subagent:%s", strings.TrimSpace(task.ID))
		}
		taskInput := loop.buildSubagentTaskInput(task)
		ns := normalizeMemoryNamespace(task.MemoryNS)
		ctx = withMemoryNamespaceContext(ctx, ns)
		ctx = withToolAllowlistContext(ctx, task.ToolAllowlist)
		channel := strings.TrimSpace(task.OriginChannel)
		if channel == "" {
			channel = "cli"
		}
		chatID := strings.TrimSpace(task.OriginChatID)
		if chatID == "" {
			chatID = "direct"
		}
		msg := bus.InboundMessage{
			Channel:    channel,
			SenderID:   "subagent",
			ChatID:     chatID,
			Content:    taskInput,
			SessionKey: sessionKey,
			Metadata: map[string]string{
				"memory_namespace": ns,
				"memory_ns":        ns,
				"disable_planning": "true",
				"trigger":          "subagent",
			},
		}
		return loop.processMessage(ctx, msg)
	})

	return loop
}

func (al *AgentLoop) dispatchNodeSubagentTask(ctx context.Context, task *tools.SubagentTask) (string, error) {
	if al == nil || task == nil {
		return "", fmt.Errorf("node subagent task is nil")
	}
	if al.nodeRouter == nil {
		return "", fmt.Errorf("node router is not configured")
	}
	nodeID := strings.TrimSpace(task.NodeID)
	if nodeID == "" {
		return "", fmt.Errorf("node-backed subagent %q missing node_id", task.AgentID)
	}
	taskInput := loopTaskInputForNode(task)
	reqArgs := map[string]interface{}{}
	if remoteAgentID := remoteAgentIDForNodeBranch(task.AgentID, nodeID); remoteAgentID != "" {
		reqArgs["remote_agent_id"] = remoteAgentID
	}
	resp, err := al.nodeRouter.Dispatch(ctx, nodes.Request{
		Action: "agent_task",
		Node:   nodeID,
		Task:   taskInput,
		Args:   reqArgs,
	}, "auto")
	if err != nil {
		return "", err
	}
	if !resp.OK {
		if strings.TrimSpace(resp.Error) != "" {
			return "", fmt.Errorf("node %s agent_task failed: %s", nodeID, strings.TrimSpace(resp.Error))
		}
		return "", fmt.Errorf("node %s agent_task failed", nodeID)
	}
	if result := nodeAgentTaskResult(resp.Payload); result != "" {
		return result, nil
	}
	return fmt.Sprintf("node %s completed agent_task", nodeID), nil
}

func remoteAgentIDForNodeBranch(agentID, nodeID string) string {
	agentID = strings.TrimSpace(agentID)
	nodeID = strings.TrimSpace(nodeID)
	if agentID == "" || nodeID == "" {
		return ""
	}
	prefix := "node." + nodeID + "."
	if !strings.HasPrefix(agentID, prefix) {
		return ""
	}
	remote := strings.TrimPrefix(agentID, prefix)
	if strings.TrimSpace(remote) == "" {
		return ""
	}
	return remote
}

func loopTaskInputForNode(task *tools.SubagentTask) string {
	if task == nil {
		return ""
	}
	if parent := strings.TrimSpace(task.ParentAgentID); parent != "" {
		return fmt.Sprintf("Parent Agent: %s\nSubtree Branch: %s\n\nTask:\n%s", parent, task.AgentID, strings.TrimSpace(task.Task))
	}
	return strings.TrimSpace(task.Task)
}

func nodeAgentTaskResult(payload map[string]interface{}) string {
	if len(payload) == 0 {
		return ""
	}
	if result := tools.MapStringArg(payload, "result"); result != "" {
		return result
	}
	if content := tools.MapStringArg(payload, "content"); content != "" {
		return content
	}
	return ""
}

func (al *AgentLoop) buildSubagentTaskInput(task *tools.SubagentTask) string {
	if task == nil {
		return ""
	}
	taskText := strings.TrimSpace(task.Task)
	if promptFile := strings.TrimSpace(task.SystemPromptFile); promptFile != "" {
		if promptText := al.readSubagentPromptFile(promptFile); promptText != "" {
			return fmt.Sprintf("Role Profile Policy (%s):\n%s\n\nTask:\n%s", promptFile, promptText, taskText)
		}
	}
	return taskText
}

func (al *AgentLoop) readSubagentPromptFile(relPath string) string {
	if al == nil {
		return ""
	}
	workspace := strings.TrimSpace(al.workspace)
	relPath = strings.TrimSpace(relPath)
	if workspace == "" || relPath == "" || filepath.IsAbs(relPath) {
		return ""
	}
	fullPath := filepath.Clean(filepath.Join(workspace, relPath))
	relToWorkspace, err := filepath.Rel(workspace, fullPath)
	if err != nil || strings.HasPrefix(relToWorkspace, "..") {
		return ""
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (al *AgentLoop) Run(ctx context.Context) error {
	al.running = true

	shards := al.buildSessionShards(ctx)
	defer func() {
		for _, ch := range shards {
			close(ch)
		}
	}()

	for al.running {
		msg, ok := al.bus.ConsumeInbound(ctx)
		if !ok {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		idx := sessionShardIndex(msg.SessionKey, len(shards))
		select {
		case shards[idx] <- msg:
		case <-ctx.Done():
			return nil
		}
	}

	return nil
}

func (al *AgentLoop) Stop() {
	al.running = false
}

func (al *AgentLoop) buildSessionShards(ctx context.Context) []chan bus.InboundMessage {
	count := sessionShardCount()
	shards := make([]chan bus.InboundMessage, count)
	for i := 0; i < count; i++ {
		shards[i] = make(chan bus.InboundMessage, 64)
		go func(ch <-chan bus.InboundMessage) {
			for msg := range ch {
				al.processInbound(ctx, msg)
			}
		}(shards[i])
	}
	logger.InfoCF("agent", logger.C0149, map[string]interface{}{"shards": count})
	return shards
}

func (al *AgentLoop) tryFallbackProviders(ctx context.Context, msg bus.InboundMessage, messages []providers.Message, toolDefs []providers.ToolDefinition, options map[string]interface{}, primaryErr error) (*providers.LLMResponse, string, error) {
	if len(al.providerChain) <= 1 {
		return nil, "", primaryErr
	}
	lastErr := primaryErr
	candidateNames := make([]string, 0, len(al.providerChain)-1)
	for _, candidate := range al.providerChain[1:] {
		candidateNames = append(candidateNames, candidate.name)
	}
	if al.ekg != nil {
		errSig := ""
		if primaryErr != nil {
			errSig = primaryErr.Error()
		}
		candidateNames = al.ekg.RankProvidersForError(candidateNames, errSig)
	}
	ranked := make([]providerCandidate, 0, len(al.providerChain)-1)
	used := make([]bool, len(al.providerChain)-1)
	for _, name := range candidateNames {
		for idx, candidate := range al.providerChain[1:] {
			if used[idx] || candidate.name != name {
				continue
			}
			used[idx] = true
			ranked = append(ranked, candidate)
		}
	}
	for idx, candidate := range al.providerChain[1:] {
		if !used[idx] {
			ranked = append(ranked, candidate)
		}
	}
	for _, candidate := range ranked {
		p, candidateModel, err := al.ensureProviderCandidate(candidate)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := p.Chat(ctx, messages, toolDefs, candidateModel, options)
		if al.ekg != nil {
			st := "success"
			lg := "fallback provider success"
			errSig := ""
			if err != nil {
				st = "error"
				lg = err.Error()
				errSig = err.Error()
			}
			al.ekg.Record(ekg.Event{Session: msg.SessionKey, Channel: msg.Channel, Source: "provider_fallback", Status: st, Provider: candidate.name, Model: candidateModel, ErrSig: errSig, Log: lg})
		}
		if err == nil {
			logger.WarnCF("agent", logger.C0150, map[string]interface{}{"provider": candidate.name, "model": candidateModel, "ref": candidate.ref})
			return resp, candidate.name, nil
		}
		lastErr = err
	}
	return nil, "", lastErr
}

func (al *AgentLoop) ensureProviderCandidate(candidate providerCandidate) (providers.LLMProvider, string, error) {
	if al == nil {
		return nil, "", fmt.Errorf("agent loop is nil")
	}
	name := strings.TrimSpace(candidate.name)
	if name == "" {
		return nil, "", fmt.Errorf("fallback provider name is empty")
	}
	al.providerMu.RLock()
	existing := al.providerPool[name]
	al.providerMu.RUnlock()
	if existing != nil {
		model := strings.TrimSpace(candidate.model)
		if model == "" {
			model = strings.TrimSpace(existing.GetDefaultModel())
		}
		if model == "" {
			return nil, "", fmt.Errorf("fallback provider %q has no model configured", name)
		}
		return existing, model, nil
	}
	if al.cfg == nil {
		return nil, "", fmt.Errorf("config not available for fallback provider %q", name)
	}
	created, err := providers.CreateProviderByName(al.cfg, name)
	if err != nil {
		return nil, "", err
	}
	model := strings.TrimSpace(candidate.model)
	if model == "" {
		model = strings.TrimSpace(created.GetDefaultModel())
	}
	if model == "" {
		return nil, "", fmt.Errorf("fallback provider %q has no model configured", name)
	}
	al.providerMu.Lock()
	if existing := al.providerPool[name]; existing != nil {
		al.providerMu.Unlock()
		return existing, model, nil
	}
	al.providerPool[name] = created
	al.providerMu.Unlock()
	return created, model, nil
}

func automaticFallbackPriority(name string) int {
	switch normalizeFallbackProviderName(name) {
	case "claude":
		return 10
	case "codex":
		return 20
	case "gemini":
		return 30
	case "gemini-cli":
		return 40
	case "aistudio":
		return 50
	case "vertex":
		return 60
	case "antigravity":
		return 70
	case "qwen":
		return 80
	case "kimi":
		return 90
	case "iflow":
		return 100
	case "openai-compatibility":
		return 110
	default:
		return 1000
	}
}

func normalizeFallbackProviderName(name string) string {
	normalized, _ := config.ParseProviderModelRef(strings.TrimSpace(name) + "/_")
	return strings.TrimSpace(normalized)
}

func (al *AgentLoop) setSessionProvider(sessionKey, provider string) {
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return
	}
	al.providerMu.Lock()
	al.sessionProvider[key] = provider
	al.providerMu.Unlock()
}

func (al *AgentLoop) getSessionProvider(sessionKey string) string {
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return ""
	}
	al.providerMu.RLock()
	v := al.sessionProvider[key]
	al.providerMu.RUnlock()
	return v
}

func (al *AgentLoop) syncSessionDefaultProvider(sessionKey string) {
	if al == nil || len(al.providerNames) == 0 {
		return
	}
	al.setSessionProvider(sessionKey, al.providerNames[0])
}

func (al *AgentLoop) markSessionStreamed(sessionKey string) {
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return
	}
	al.streamMu.Lock()
	al.sessionStreamed[key] = true
	al.streamMu.Unlock()
}

func (al *AgentLoop) consumeSessionStreamed(sessionKey string) bool {
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return false
	}
	al.streamMu.Lock()
	defer al.streamMu.Unlock()
	v := al.sessionStreamed[key]
	delete(al.sessionStreamed, key)
	return v
}

func (al *AgentLoop) processInbound(ctx context.Context, msg bus.InboundMessage) {
	taskID := buildAuditTaskID(msg)
	started := time.Now()
	al.appendTaskAuditEvent(taskID, msg, "running", started, 0, "started", false)

	response, err := al.processPlannedMessage(ctx, msg)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			al.audit.Record(al.getTrigger(msg), msg.Channel, msg.SessionKey, true, err)
			al.appendTaskAudit(taskID, msg, started, err, true)
			return
		}
		response = fmt.Sprintf("Error processing message: %v", err)
	}

	trigger := al.getTrigger(msg)
	suppressed := false
	if response != "" {
		if msg.Channel == "telegram" && al.telegramStreaming && al.consumeSessionStreamed(msg.SessionKey) {
			suppressed = true
		} else if outbound, ok := al.prepareOutbound(msg, response); ok {
			al.bus.PublishOutbound(outbound)
		} else {
			suppressed = true
		}
	}
	if msg.Channel == "telegram" && suppressed {
		replyID := ""
		if msg.Metadata != nil {
			replyID = msg.Metadata["message_id"]
		}
		// Final pass uses full formatted content to stabilize rendering after plain streaming.
		al.bus.PublishOutbound(bus.OutboundMessage{Channel: msg.Channel, ChatID: msg.ChatID, Action: "finalize", Content: response, ReplyToID: replyID})
	}
	al.audit.Record(trigger, msg.Channel, msg.SessionKey, suppressed, err)
	al.appendTaskAudit(taskID, msg, started, err, suppressed)
}

func shortSessionKey(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}

func buildAuditTaskID(msg bus.InboundMessage) string {
	trigger := ""
	if msg.Metadata != nil {
		trigger = strings.ToLower(strings.TrimSpace(msg.Metadata["trigger"]))
	}
	sessionPart := shortSessionKey(msg.SessionKey)
	switch trigger {
	case "heartbeat":
		if sessionPart == "" {
			sessionPart = "default"
		}
		return "heartbeat:" + sessionPart
	default:
		return fmt.Sprintf("%s-%d", sessionPart, time.Now().Unix()%100000)
	}
}

func (al *AgentLoop) appendTaskAudit(taskID string, msg bus.InboundMessage, started time.Time, runErr error, suppressed bool) {
	status := "success"
	logText := "completed"
	if runErr != nil {
		status = "error"
		logText = runErr.Error()
	} else if suppressed {
		status = "suppressed"
		logText = "suppressed"
	}
	al.appendTaskAuditEvent(taskID, msg, status, started, int(time.Since(started).Milliseconds()), logText, suppressed)
}

func (al *AgentLoop) appendTaskAuditEvent(taskID string, msg bus.InboundMessage, status string, started time.Time, durationMs int, logText string, suppressed bool) {
	if al.workspace == "" {
		return
	}
	path := filepath.Join(al.workspace, "memory", "task-audit.jsonl")
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	source := "direct"
	if msg.Metadata != nil && msg.Metadata["trigger"] != "" {
		source = msg.Metadata["trigger"]
	}
	row := map[string]interface{}{
		"task_id":       taskID,
		"time":          time.Now().UTC().Format(time.RFC3339),
		"channel":       msg.Channel,
		"session":       msg.SessionKey,
		"chat_id":       msg.ChatID,
		"sender_id":     msg.SenderID,
		"status":        status,
		"source":        source,
		"idle_run":      false,
		"duration_ms":   durationMs,
		"suppressed":    suppressed,
		"retry_count":   0,
		"log":           logText,
		"input_preview": truncate(strings.ReplaceAll(msg.Content, "\n", " "), 180),
		"media_count":   len(msg.MediaItems),
		"media_items":   msg.MediaItems,
		"provider":      al.getSessionProvider(msg.SessionKey),
		"model":         al.model,
	}
	if msg.Metadata != nil {
		if v := strings.TrimSpace(msg.Metadata["context_extra_chars"]); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				row["context_extra_chars"] = n
			}
		}
		if v := strings.TrimSpace(msg.Metadata["context_ekg_chars"]); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				row["context_ekg_chars"] = n
			}
		}
		if v := strings.TrimSpace(msg.Metadata["context_memory_chars"]); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				row["context_memory_chars"] = n
			}
		}
	}
	if al.ekg != nil {
		al.ekg.Record(ekg.Event{
			TaskID:  taskID,
			Session: msg.SessionKey,
			Channel: msg.Channel,
			Source:  source,
			Status:  status,
			Log:     logText,
		})
	}

	b, _ := json.Marshal(row)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func sessionShardCount() int {
	// Keep ~20% CPU headroom for system/background work, then use a
	// sub-linear curve to avoid oversharding on high-core machines.
	n := runtime.GOMAXPROCS(0)
	if n <= 0 {
		n = runtime.NumCPU()
	}
	if n <= 0 {
		return 2
	}
	budget := int(math.Floor(float64(n) * 0.8))
	if budget < 1 {
		budget = 1
	}
	shards := int(math.Round(math.Sqrt(float64(budget)) * 2.2))
	if shards < 2 {
		shards = 2
	}
	if shards > 12 {
		shards = 12
	}
	return shards
}

func sessionShardIndex(sessionKey string, shardCount int) int {
	if shardCount <= 1 {
		return 0
	}
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		key = "default"
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() % uint32(shardCount))
}

func (al *AgentLoop) getTrigger(msg bus.InboundMessage) string {
	if msg.Metadata != nil {
		if t := strings.TrimSpace(msg.Metadata["trigger"]); t != "" {
			return strings.ToLower(t)
		}
	}
	if msg.Channel == "system" {
		sid := strings.ToLower(strings.TrimSpace(msg.SenderID))
		if sid != "" {
			return sid
		}
		return "system"
	}
	return "user"
}

func (al *AgentLoop) shouldSuppressOutbound(msg bus.InboundMessage, response string) bool {
	if msg.Metadata == nil {
		return false
	}
	trigger := strings.ToLower(strings.TrimSpace(msg.Metadata["trigger"]))
	if trigger != "heartbeat" {
		return false
	}

	r := strings.TrimSpace(response)
	ackToken := strings.TrimSpace(al.heartbeatAckToken)
	if ackToken == "" {
		return false
	}
	if !strings.HasPrefix(r, ackToken) {
		return false
	}

	maxChars := al.heartbeatAckMaxChars
	if maxChars <= 0 {
		maxChars = 64
	}
	return len(r) <= maxChars
}

func loadHeartbeatAckToken(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return ""
	}
	parse := func(text string) string {
		for _, line := range strings.Split(text, "\n") {
			t := strings.TrimSpace(line)
			if t == "" {
				continue
			}
			raw := strings.TrimLeft(t, "-*# ")
			lower := strings.ToLower(raw)
			if !strings.HasPrefix(lower, "heartbeat_ack_token:") {
				continue
			}
			v := strings.TrimSpace(raw[len("heartbeat_ack_token:"):])
			v = strings.Trim(v, "`\"' ")
			if v != "" {
				return v
			}
		}
		return ""
	}
	if b, err := os.ReadFile(filepath.Join(workspace, "AGENTS.md")); err == nil {
		if token := parse(string(b)); token != "" {
			return token
		}
	}
	if b, err := os.ReadFile(filepath.Join(workspace, "HEARTBEAT.md")); err == nil {
		if token := parse(string(b)); token != "" {
			return token
		}
	}
	return ""
}

func (al *AgentLoop) prepareOutbound(msg bus.InboundMessage, response string) (bus.OutboundMessage, bool) {
	if shouldDropNoReply(response) {
		if fallback, ok := fallbackSubagentNotification(msg); ok {
			response = fallback
		} else {
			return bus.OutboundMessage{}, false
		}
	}
	currentMsgID := ""
	if msg.Metadata != nil {
		currentMsgID = msg.Metadata["message_id"]
	}
	clean, replyToID := parseReplyTag(response, currentMsgID)
	clean = strings.TrimSpace(clean)
	if clean == "" {
		if fallback, ok := fallbackSubagentNotification(msg); ok {
			clean = fallback
		} else {
			return bus.OutboundMessage{}, false
		}
	}
	if al.shouldSuppressOutbound(msg, clean) {
		return bus.OutboundMessage{}, false
	}
	outbound := bus.OutboundMessage{
		Channel:       msg.Channel,
		ChatID:        msg.ChatID,
		Content:       clean,
		ReplyToID:     strings.TrimSpace(replyToID),
		ReplyToSender: strings.TrimSpace(msg.SenderID),
	}
	if msg.Channel == "system" {
		outbound.Channel, outbound.ChatID = resolveSystemOrigin(msg.ChatID)
	}
	return outbound, true
}

func (al *AgentLoop) ProcessDirect(ctx context.Context, content, sessionKey string) (string, error) {
	return al.ProcessDirectWithOptions(ctx, content, sessionKey, "cli", "direct", "main", nil)
}

func (al *AgentLoop) ProcessDirectWithOptions(ctx context.Context, content, sessionKey, channel, chatID, memoryNamespace string, toolAllowlist []string) (string, error) {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		channel = "cli"
	}
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		chatID = "direct"
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		sessionKey = "main"
	}
	al.syncSessionDefaultProvider(sessionKey)
	ns := normalizeMemoryNamespace(memoryNamespace)
	var metadata map[string]string
	if ns != "main" {
		metadata = map[string]string{
			"memory_namespace": ns,
			"memory_ns":        ns,
		}
	}
	ctx = withMemoryNamespaceContext(ctx, ns)
	ctx = withToolAllowlistContext(ctx, toolAllowlist)

	msg := bus.InboundMessage{
		Channel:    channel,
		SenderID:   "user",
		ChatID:     chatID,
		Content:    content,
		SessionKey: sessionKey,
		Metadata:   metadata,
	}

	return al.processPlannedMessage(ctx, msg)
}

func (al *AgentLoop) GetSessionHistory(sessionKey string) []providers.Message {
	return al.sessions.GetHistory(sessionKey)
}

func (al *AgentLoop) processMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	if msg.SessionKey == "" {
		msg.SessionKey = "main"
	}
	memoryNamespace := resolveInboundMemoryNamespace(msg)
	ctx = withMemoryNamespaceContext(ctx, memoryNamespace)
	release, err := al.acquireSessionResources(ctx, &msg)
	if err != nil {
		return "", err
	}
	defer release()
	al.syncSessionDefaultProvider(msg.SessionKey)
	// Add message preview to log
	preview := truncate(msg.Content, 80)
	logger.InfoCF("agent", logger.C0171,
		map[string]interface{}{
			"channel":     msg.Channel,
			"chat_id":     msg.ChatID,
			"sender_id":   msg.SenderID,
			"session_key": msg.SessionKey,
			"preview":     preview,
		})

	// Route system messages to processSystemMessage
	if msg.Channel == "system" {
		return al.processSystemMessage(ctx, msg)
	}
	specTaskRef := specCodingTaskRef{}
	if err := al.maybeEnsureSpecCodingDocs(msg.Content); err != nil {
		logger.WarnCF("agent", logger.C0172, map[string]interface{}{
			"session_key": msg.SessionKey,
			"error":       err.Error(),
		})
	}
	if taskRef, err := al.maybeStartSpecCodingTask(msg.Content); err != nil {
		logger.WarnCF("agent", logger.C0172, map[string]interface{}{
			"session_key": msg.SessionKey,
			"error":       err.Error(),
		})
	} else {
		specTaskRef = normalizeSpecCodingTaskRef(taskRef)
	}
	if routed, ok, routeErr := al.maybeAutoRoute(ctx, msg); ok {
		if routeErr != nil && specTaskRef.Summary != "" {
			if err := al.maybeReopenSpecCodingTask(specTaskRef, msg.Content, routeErr.Error()); err != nil {
				logger.WarnCF("agent", logger.C0172, map[string]interface{}{
					"session_key": msg.SessionKey,
					"error":       err.Error(),
				})
			}
		}
		if routeErr == nil && specTaskRef.Summary != "" {
			if err := al.maybeCompleteSpecCodingTask(specTaskRef, routed); err != nil {
				logger.WarnCF("agent", logger.C0172, map[string]interface{}{
					"session_key": msg.SessionKey,
					"error":       err.Error(),
				})
			}
		}
		return routed, routeErr
	}

	history := al.sessions.GetHistory(msg.SessionKey)
	summary := al.sessions.GetSummary(msg.SessionKey)
	if explicitPref := ExtractLanguagePreference(msg.Content); explicitPref != "" {
		al.sessions.SetPreferredLanguage(msg.SessionKey, explicitPref)
	}
	preferredLang, lastLang := al.sessions.GetLanguagePreferences(msg.SessionKey)
	responseLang := DetectResponseLanguage(msg.Content, preferredLang, lastLang)

	messages := al.contextBuilder.BuildMessagesWithMemoryNamespace(
		history,
		summary,
		msg.Content,
		nil,
		msg.Channel,
		msg.ChatID,
		responseLang,
		memoryNamespace,
	)

	iteration := 0
	var finalContent string
	hasToolActivity := false
	lastToolOutputs := make([]string, 0, 4)
	maxAllowed := al.maxIterations
	if maxAllowed < 1 {
		maxAllowed = 1
	}
	for iteration < maxAllowed {
		iteration++

		logger.DebugCF("agent", logger.C0151,
			map[string]interface{}{
				"iteration": iteration,
				"max":       al.maxIterations,
			})

		toolDefs := al.filteredToolDefinitionsForContext(ctx)
		providerToolDefs := al.buildProviderToolDefs(toolDefs)

		// Log LLM request details
		logger.DebugCF("agent", logger.C0152,
			map[string]interface{}{
				"iteration":         iteration,
				"model":             al.model,
				"messages_count":    len(messages),
				"tools_count":       len(providerToolDefs),
				"max_tokens":        8192,
				"temperature":       0.7,
				"system_prompt_len": len(messages[0].Content),
			})

		// Log full messages (detailed)
		logger.DebugCF("agent", logger.C0153,
			map[string]interface{}{
				"iteration":     iteration,
				"messages_json": formatMessagesForLog(messages),
				"tools_json":    formatToolsForLog(providerToolDefs),
			})

		messages = injectResponsesMediaParts(messages, msg.Media, msg.MediaItems)
		options := al.buildResponsesOptions(msg.SessionKey, 8192, 0.7)
		var response *providers.LLMResponse
		var err error
		if msg.Channel == "telegram" && al.telegramStreaming {
			if sp, ok := al.provider.(providers.StreamingLLMProvider); ok {
				streamText := ""
				lastPush := time.Now().Add(-time.Second)
				response, err = sp.ChatStream(ctx, messages, providerToolDefs, al.model, options, func(delta string) {
					if strings.TrimSpace(delta) == "" {
						return
					}
					streamText += delta
					if time.Since(lastPush) < 450*time.Millisecond {
						return
					}
					if !shouldFlushTelegramStreamSnapshot(streamText) {
						return
					}
					lastPush = time.Now()
					replyID := ""
					if msg.Metadata != nil {
						replyID = msg.Metadata["message_id"]
					}
					// Stream with formatted rendering once snapshot is syntactically safe.
					al.bus.PublishOutbound(bus.OutboundMessage{Channel: msg.Channel, ChatID: msg.ChatID, Content: streamText, Action: "stream", ReplyToID: replyID})
					al.markSessionStreamed(msg.SessionKey)
				})
			} else {
				response, err = al.provider.Chat(ctx, messages, providerToolDefs, al.model, options)
			}
		} else {
			response, err = al.provider.Chat(ctx, messages, providerToolDefs, al.model, options)
		}

		if err != nil {
			if fb, _, ferr := al.tryFallbackProviders(ctx, msg, messages, providerToolDefs, options, err); ferr == nil && fb != nil {
				response = fb
				err = nil
			} else {
				err = ferr
			}
		}
		if err != nil {
			logger.ErrorCF("agent", logger.C0155,
				map[string]interface{}{
					"iteration": iteration,
					"error":     err.Error(),
				})
			if specTaskRef.Summary != "" {
				if rerr := al.maybeReopenSpecCodingTask(specTaskRef, msg.Content, err.Error()); rerr != nil {
					logger.WarnCF("agent", logger.C0172, map[string]interface{}{
						"session_key": msg.SessionKey,
						"error":       rerr.Error(),
					})
				}
			}
			return "", fmt.Errorf("LLM call failed: %w", err)
		}

		if len(response.ToolCalls) == 0 {
			finalContent = response.Content
			logger.InfoCF("agent", logger.C0156,
				map[string]interface{}{
					"iteration":     iteration,
					"content_chars": len(finalContent),
				})
			break
		}

		toolNames := make([]string, 0, len(response.ToolCalls))
		for _, tc := range response.ToolCalls {
			toolNames = append(toolNames, tc.Name)
		}
		logger.InfoCF("agent", logger.C0157,
			map[string]interface{}{
				"tools":     toolNames,
				"count":     len(toolNames),
				"iteration": iteration,
			})

		assistantMsg := providers.Message{
			Role:    "assistant",
			Content: response.Content,
		}

		for _, tc := range response.ToolCalls {
			argumentsJSON, _ := json.Marshal(tc.Arguments)
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, providers.ToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: &providers.FunctionCall{
					Name:      tc.Name,
					Arguments: string(argumentsJSON),
				},
			})
		}
		messages = append(messages, assistantMsg)
		// Persist assistant message with tool calls.
		al.sessions.AddMessageFull(msg.SessionKey, assistantMsg)

		hasToolActivity = true
		// Extend rolling window as long as tools keep chaining.
		if maxAllowed < iteration+al.maxIterations {
			maxAllowed = iteration + al.maxIterations
		}
		for _, tc := range response.ToolCalls {
			// Log tool call with arguments preview
			argsJSON, _ := json.Marshal(tc.Arguments)
			argsPreview := truncate(string(argsJSON), 200)
			logger.InfoCF("agent", logger.C0172,
				map[string]interface{}{
					"tool":      tc.Name,
					"args":      argsPreview,
					"iteration": iteration,
				})

			execArgs := withToolContextArgs(tc.Name, tc.Arguments, msg.Channel, msg.ChatID)
			result, err := al.executeToolCall(ctx, tc.Name, execArgs, msg.Channel, msg.ChatID)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}
			if len(lastToolOutputs) < 4 {
				lastToolOutputs = append(lastToolOutputs, fmt.Sprintf("%s: %s", tc.Name, truncate(strings.ReplaceAll(result, "\n", " "), 180)))
			}
			toolResultMsg := providers.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			}
			messages = append(messages, toolResultMsg)
			// Persist tool result message.
			al.sessions.AddMessageFull(msg.SessionKey, toolResultMsg)
		}
	}

	if finalContent == "" && hasToolActivity {
		forced, ferr := al.provider.Chat(ctx, messages, nil, al.model, map[string]interface{}{"max_tokens": 8192, "temperature": 0.2})
		if ferr == nil && forced != nil && forced.Content != "" {
			finalContent = forced.Content
		}
	}

	// Filter out <think>...</think> content from user-facing response
	// Keep full content in debug logs if needed, but remove from final output
	re := regexp.MustCompile(`(?s)<think>.*?</think>`)
	userContent := re.ReplaceAllString(finalContent, "")
	if userContent == "" && finalContent != "" {
		// If only thoughts were present, maybe provide a generic "Done" or keep something?
		// For now, let's assume thoughts are auxiliary and empty response is okay if tools did work.
		// If no tools ran and only thoughts, user might be confused.
		if iteration == 1 {
			userContent = "Thinking process completed."
		}
	}

	al.sessions.AddMessage(msg.SessionKey, "user", msg.Content)

	// Persist full assistant response (including reasoning/tool flow outcomes when present).
	al.sessions.AddMessageFull(msg.SessionKey, providers.Message{
		Role:    "assistant",
		Content: userContent,
	})
	al.sessions.SetLastLanguage(msg.SessionKey, responseLang)
	al.compactSessionIfNeeded(msg.SessionKey)

	al.sessions.Save(al.sessions.GetOrCreate(msg.SessionKey))

	// Log response preview (original content)
	responsePreview := truncate(finalContent, 120)
	logger.InfoCF("agent", logger.C0173,
		map[string]interface{}{
			"channel":      msg.Channel,
			"sender_id":    msg.SenderID,
			"preview":      responsePreview,
			"iterations":   iteration,
			"final_length": len(finalContent),
			"user_length":  len(userContent),
		})

	if specTaskRef.Summary != "" {
		if err := al.maybeCompleteSpecCodingTask(specTaskRef, userContent); err != nil {
			logger.WarnCF("agent", logger.C0172, map[string]interface{}{
				"session_key": msg.SessionKey,
				"error":       err.Error(),
			})
		}
	}
	al.appendDailySummaryLog(msg, userContent)
	return userContent, nil
}

func (al *AgentLoop) acquireSessionResources(ctx context.Context, msg *bus.InboundMessage) (func(), error) {
	if al == nil || msg == nil || al.sessionScheduler == nil {
		return func() {}, nil
	}
	keys, cleaned := al.resolveMessageResourceKeys(msg)
	msg.Content = cleaned
	return al.sessionScheduler.Acquire(ctx, msg.SessionKey, keys)
}

func (al *AgentLoop) resolveMessageResourceKeys(msg *bus.InboundMessage) ([]string, string) {
	if msg == nil {
		return nil, ""
	}
	content := msg.Content
	if msg.Metadata != nil {
		if raw := strings.TrimSpace(msg.Metadata["resource_keys"]); raw != "" {
			if explicit := scheduling.ParseResourceKeyList(raw); len(explicit) > 0 {
				return explicit, content
			}
		}
	}
	if explicit, cleaned, ok := scheduling.ExtractResourceKeysDirective(content); ok {
		if strings.TrimSpace(cleaned) != "" {
			content = cleaned
		}
		return explicit, content
	}
	return scheduling.DeriveResourceKeys(content), content
}

func (al *AgentLoop) appendDailySummaryLog(msg bus.InboundMessage, response string) {
	if strings.TrimSpace(al.workspace) == "" {
		return
	}
	respText := strings.TrimSpace(response)
	if respText == "" {
		return
	}
	// Avoid noisy heartbeat/system boilerplate.
	lc := strings.ToLower(strings.TrimSpace(msg.Content))
	if strings.Contains(lc, "heartbeat") && strings.Contains(strings.ToLower(respText), "heartbeat_ok") {
		return
	}
	namespace := resolveInboundMemoryNamespace(msg)
	detailLine := al.buildDailySummaryEntry(msg, namespace, respText)
	ms := NewMemoryStoreWithNamespace(al.workspace, namespace)
	if err := ms.AppendToday(detailLine); err != nil {
		logger.WarnCF("agent", logger.C0158, map[string]interface{}{logger.FieldError: err.Error()})
	}
	if namespace != "main" {
		mainLine := al.buildMainMemorySubagentEntry(msg, namespace, respText)
		if err := NewMemoryStore(al.workspace).AppendToday(mainLine); err != nil {
			logger.WarnCF("agent", logger.C0158, map[string]interface{}{"target": "main", logger.FieldError: err.Error()})
		}
	}
}

func (al *AgentLoop) buildDailySummaryEntry(msg bus.InboundMessage, namespace, response string) string {
	title := al.dailySummaryTitle(msg, namespace)
	return fmt.Sprintf("## %s %s\n\n- Did: %s\n- Channel: %s\n- Session: %s",
		time.Now().Format("15:04"),
		title,
		truncate(strings.ReplaceAll(strings.TrimSpace(response), "\n", " "), 320),
		strings.TrimSpace(msg.Channel),
		strings.TrimSpace(msg.SessionKey),
	)
}

func (al *AgentLoop) buildMainMemorySubagentEntry(msg bus.InboundMessage, namespace, response string) string {
	title := al.dailySummaryTitle(msg, namespace)
	return fmt.Sprintf("## %s %s\n\n- Subagent: %s\n- Did: %s\n- Session: %s",
		time.Now().Format("15:04"),
		title,
		al.memoryAgentTitle(namespace),
		truncate(strings.ReplaceAll(strings.TrimSpace(response), "\n", " "), 220),
		strings.TrimSpace(msg.SessionKey),
	)
}

func (al *AgentLoop) dailySummaryTitle(msg bus.InboundMessage, namespace string) string {
	agentName := al.memoryAgentTitle(namespace)
	taskTitle := summarizeMemoryTaskTitle(msg.Content)
	if taskTitle == "" {
		return agentName
	}
	return fmt.Sprintf("%s | %s", agentName, taskTitle)
}

func (al *AgentLoop) memoryAgentTitle(namespace string) string {
	ns := normalizeMemoryNamespace(namespace)
	if ns == "main" {
		return "Main Agent"
	}
	cfg := runtimecfg.Get()
	if cfg != nil {
		if subcfg, ok := cfg.Agents.Subagents[ns]; ok {
			if name := strings.TrimSpace(subcfg.DisplayName); name != "" {
				return name
			}
		}
	}
	return strings.ReplaceAll(ns, "-", " ")
}

func summarizeMemoryTaskTitle(content string) string {
	text := strings.TrimSpace(content)
	if text == "" {
		return ""
	}
	if idx := strings.Index(text, "Task:\n"); idx >= 0 {
		text = strings.TrimSpace(text[idx+len("Task:\n"):])
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if idx := strings.Index(text, "\n"); idx >= 0 {
		text = text[:idx]
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if len(text) > 72 {
		return strings.TrimSpace(text[:69]) + "..."
	}
	return text
}

func (al *AgentLoop) processSystemMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	// Verify this is a system message
	if msg.Channel != "system" {
		return "", fmt.Errorf("processSystemMessage called with non-system message channel: %s", msg.Channel)
	}

	logger.InfoCF("agent", logger.C0159,
		map[string]interface{}{
			"sender_id": msg.SenderID,
			"chat_id":   msg.ChatID,
		})

	if al.handleSubagentSystemMessage(msg) {
		return "", nil
	}

	originChannel, originChatID := resolveSystemOrigin(msg.ChatID)

	// Use the origin session for context
	sessionKey := fmt.Sprintf("%s:%s", originChannel, originChatID)

	// Build messages with the announce content
	history := al.sessions.GetHistory(sessionKey)
	summary := al.sessions.GetSummary(sessionKey)
	preferredLang, lastLang := al.sessions.GetLanguagePreferences(sessionKey)
	responseLang := DetectResponseLanguage(msg.Content, preferredLang, lastLang)
	messages := al.contextBuilder.BuildMessages(
		history,
		summary,
		msg.Content,
		nil,
		originChannel,
		originChatID,
		responseLang,
	)

	iteration := 0
	var finalContent string

	for iteration < al.maxIterations {
		iteration++

		toolDefs := al.filteredToolDefinitionsForContext(ctx)
		providerToolDefs := al.buildProviderToolDefs(toolDefs)

		// Log LLM request details
		logger.DebugCF("agent", logger.C0152,
			map[string]interface{}{
				"iteration":         iteration,
				"model":             al.model,
				"messages_count":    len(messages),
				"tools_count":       len(providerToolDefs),
				"max_tokens":        8192,
				"temperature":       0.7,
				"system_prompt_len": len(messages[0].Content),
			})

		// Log full messages (detailed)
		logger.DebugCF("agent", logger.C0153,
			map[string]interface{}{
				"iteration":     iteration,
				"messages_json": formatMessagesForLog(messages),
				"tools_json":    formatToolsForLog(providerToolDefs),
			})

		options := al.buildResponsesOptions(sessionKey, 8192, 0.7)
		response, err := al.provider.Chat(ctx, messages, providerToolDefs, al.model, options)

		if err != nil {
			if fb, _, ferr := al.tryFallbackProviders(ctx, msg, messages, providerToolDefs, options, err); ferr == nil && fb != nil {
				response = fb
				err = nil
			} else {
				err = ferr
			}
		}
		if err != nil {
			logger.ErrorCF("agent", logger.C0162,
				map[string]interface{}{
					"iteration": iteration,
					"error":     err.Error(),
				})
			return "", fmt.Errorf("LLM call failed: %w", err)
		}

		if len(response.ToolCalls) == 0 {
			finalContent = response.Content
			break
		}

		assistantMsg := providers.Message{
			Role:    "assistant",
			Content: response.Content,
		}

		for _, tc := range response.ToolCalls {
			argumentsJSON, _ := json.Marshal(tc.Arguments)
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, providers.ToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: &providers.FunctionCall{
					Name:      tc.Name,
					Arguments: string(argumentsJSON),
				},
			})
		}
		messages = append(messages, assistantMsg)
		// Persist assistant message with tool calls.
		al.sessions.AddMessageFull(sessionKey, assistantMsg)

		for _, tc := range response.ToolCalls {
			execArgs := withToolContextArgs(tc.Name, tc.Arguments, originChannel, originChatID)
			result, err := al.executeToolCall(ctx, tc.Name, execArgs, originChannel, originChatID)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}

			toolResultMsg := providers.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			}
			messages = append(messages, toolResultMsg)
			// Persist tool result message.
			al.sessions.AddMessageFull(sessionKey, toolResultMsg)
		}
	}

	if finalContent == "" {
		finalContent = "Background task completed."
	}

	// Save to session with system message marker
	al.sessions.AddMessage(sessionKey, "user", fmt.Sprintf("[System: %s] %s", msg.SenderID, msg.Content))

	// If finalContent has no tool calls (last LLM turn is direct text),
	// earlier steps were already persisted in-loop; this stores the final reply.
	al.sessions.AddMessageFull(sessionKey, providers.Message{
		Role:    "assistant",
		Content: finalContent,
	})
	al.sessions.SetLastLanguage(sessionKey, responseLang)
	al.compactSessionIfNeeded(sessionKey)

	al.sessions.Save(al.sessions.GetOrCreate(sessionKey))

	logger.InfoCF("agent", logger.C0163,
		map[string]interface{}{
			"iterations":   iteration,
			"final_length": len(finalContent),
		})

	return finalContent, nil
}

func (al *AgentLoop) handleSubagentSystemMessage(msg bus.InboundMessage) bool {
	if !isSubagentSystemMessage(msg) {
		return false
	}
	reason := ""
	agentID := ""
	status := ""
	taskSummary := ""
	resultSummary := ""
	if msg.Metadata != nil {
		reason = strings.ToLower(strings.TrimSpace(msg.Metadata["notify_reason"]))
		agentID = strings.TrimSpace(msg.Metadata["agent_id"])
		status = strings.TrimSpace(msg.Metadata["status"])
	}
	if agentID == "" {
		agentID = strings.TrimSpace(strings.TrimPrefix(msg.SenderID, "subagent:"))
	}
	if taskSummary == "" || resultSummary == "" {
		taskSummary, resultSummary = parseSubagentSystemContent(msg.Content)
	}
	al.enqueueSubagentDigest(msg, subagentDigestItem{
		agentID:       agentID,
		reason:        reason,
		status:        status,
		taskSummary:   taskSummary,
		resultSummary: resultSummary,
	})
	return true
}

// truncate returns a truncated version of s with at most maxLen characters.
// If the string is truncated, "..." is appended to indicate truncation.
// If the string fits within maxLen, it is returned unchanged.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Reserve 3 chars for "..."
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func (al *AgentLoop) buildProviderToolDefs(toolDefs []map[string]interface{}) []providers.ToolDefinition {
	providerToolDefs := make([]providers.ToolDefinition, 0, len(toolDefs))
	for _, td := range toolDefs {
		fnRaw, ok := td["function"].(map[string]interface{})
		if !ok {
			continue
		}
		name := tools.MapStringArg(fnRaw, "name")
		description := tools.MapStringArg(fnRaw, "description")
		params, _ := fnRaw["parameters"].(map[string]interface{})
		if strings.TrimSpace(name) == "" {
			continue
		}
		if params == nil {
			params = map[string]interface{}{}
		}
		providerToolDefs = append(providerToolDefs, providers.ToolDefinition{
			Type: "function",
			Function: providers.ToolFunctionDefinition{
				Name:        name,
				Description: description,
				Parameters:  params,
			},
		})
	}
	return providerToolDefs
}

func (al *AgentLoop) filteredToolDefinitionsForContext(ctx context.Context) []map[string]interface{} {
	if al == nil || al.tools == nil {
		return nil
	}
	return filterToolDefinitionsByContext(ctx, al.tools.GetDefinitions())
}

func filterToolDefinitionsByContext(ctx context.Context, toolDefs []map[string]interface{}) []map[string]interface{} {
	allow := toolAllowlistFromContext(ctx)
	if len(allow) == 0 {
		return toolDefs
	}

	filtered := make([]map[string]interface{}, 0, len(toolDefs))
	for _, td := range toolDefs {
		fnRaw, ok := td["function"].(map[string]interface{})
		if !ok {
			continue
		}
		name := strings.ToLower(tools.MapStringArg(fnRaw, "name"))
		if name == "" {
			continue
		}
		if isToolNameAllowed(allow, name) || isImplicitlyAllowedSubagentTool(name) {
			filtered = append(filtered, td)
		}
	}
	return filtered
}

func (al *AgentLoop) buildResponsesOptions(sessionKey string, maxTokens int64, temperature float64) map[string]interface{} {
	options := map[string]interface{}{
		"max_tokens":  maxTokens,
		"temperature": temperature,
	}
	if strings.EqualFold(strings.TrimSpace(al.getSessionProvider(sessionKey)), "codex") {
		if key := strings.TrimSpace(sessionKey); key != "" {
			options["codex_execution_session"] = key
		}
	}
	responsesCfg := al.responsesConfigForSession(sessionKey)
	responseTools := make([]map[string]interface{}, 0, 2)
	if responsesCfg.WebSearchEnabled {
		webTool := map[string]interface{}{"type": "web_search"}
		if contextSize := strings.TrimSpace(responsesCfg.WebSearchContextSize); contextSize != "" {
			webTool["search_context_size"] = contextSize
		}
		responseTools = append(responseTools, webTool)
	}
	if len(responsesCfg.FileSearchVectorStoreIDs) > 0 {
		fileSearch := map[string]interface{}{
			"type":             "file_search",
			"vector_store_ids": responsesCfg.FileSearchVectorStoreIDs,
		}
		if responsesCfg.FileSearchMaxNumResults > 0 {
			fileSearch["max_num_results"] = responsesCfg.FileSearchMaxNumResults
		}
		responseTools = append(responseTools, fileSearch)
	}
	if len(responseTools) > 0 {
		options["responses_tools"] = responseTools
	}
	if len(responsesCfg.Include) > 0 {
		options["responses_include"] = responsesCfg.Include
	}
	if responsesCfg.StreamIncludeUsage {
		options["responses_stream_options"] = map[string]interface{}{"include_usage": true}
	}
	return options
}

func (al *AgentLoop) responsesConfigForSession(sessionKey string) config.ProviderResponsesConfig {
	if al == nil {
		return config.ProviderResponsesConfig{}
	}
	name := strings.TrimSpace(al.getSessionProvider(sessionKey))
	if name == "" && len(al.providerNames) > 0 {
		name = al.providerNames[0]
	}
	if name == "" {
		return config.ProviderResponsesConfig{}
	}
	if cfg, ok := al.providerResponses[name]; ok {
		return cfg
	}
	return config.ProviderResponsesConfig{}
}

func injectResponsesMediaParts(messages []providers.Message, media []string, mediaItems []bus.MediaItem) []providers.Message {
	if len(messages) == 0 || (len(media) == 0 && len(mediaItems) == 0) {
		return messages
	}
	last := len(messages) - 1
	if strings.ToLower(strings.TrimSpace(messages[last].Role)) != "user" {
		return messages
	}

	parts := make([]providers.MessageContentPart, 0, 1+len(media)+len(mediaItems))
	if strings.TrimSpace(messages[last].Content) != "" {
		parts = append(parts, providers.MessageContentPart{
			Type: "input_text",
			Text: messages[last].Content,
		})
	}

	// Fallback-only handling for raw media refs. Prefer structured media_items when present.
	if len(mediaItems) == 0 {
		for _, ref := range media {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			if isResponsesFileID(ref) {
				parts = append(parts, providers.MessageContentPart{
					Type:   "input_image",
					FileID: ref,
				})
				continue
			}
			if !isRemoteReference(ref) {
				continue
			}
			parts = append(parts, providers.MessageContentPart{
				Type:     "input_image",
				ImageURL: ref,
			})
		}
	}

	for _, item := range mediaItems {
		typ := strings.ToLower(strings.TrimSpace(item.Type))
		ref := strings.TrimSpace(item.Ref)
		path := strings.TrimSpace(item.Path)
		src := ref
		if src == "" {
			src = path
		}
		switch {
		case strings.Contains(typ, "image"):
			part := providers.MessageContentPart{Type: "input_image"}
			if isResponsesFileID(src) {
				part.FileID = src
			} else if isRemoteReference(src) {
				part.ImageURL = src
			}
			if part.FileID != "" || part.ImageURL != "" {
				parts = append(parts, part)
			}
		case strings.Contains(typ, "file"), strings.Contains(typ, "document"), strings.Contains(typ, "audio"), strings.Contains(typ, "video"):
			part := providers.MessageContentPart{Type: "input_file"}
			if isResponsesFileID(src) {
				part.FileID = src
			} else if isRemoteReference(src) {
				part.FileURL = src
			}
			if part.FileID != "" || part.FileURL != "" {
				parts = append(parts, part)
			}
		}
	}

	if len(parts) == 0 {
		return messages
	}
	messages[last].ContentParts = parts
	return messages
}

func isResponsesFileID(ref string) bool {
	return strings.HasPrefix(strings.TrimSpace(ref), "file_")
}

func isRemoteReference(ref string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(ref))
	return strings.HasPrefix(trimmed, "http://") ||
		strings.HasPrefix(trimmed, "https://") ||
		strings.HasPrefix(trimmed, "data:")
}

// GetStartupInfo returns information about loaded tools and skills for logging.
func (al *AgentLoop) compactSessionIfNeeded(sessionKey string) {
	if !al.compactionEnabled {
		return
	}
	trigger := al.compactionTrigger
	if trigger <= 0 {
		trigger = 60
	}
	keepRecent := al.compactionKeepRecent
	if keepRecent <= 0 || keepRecent >= trigger {
		keepRecent = trigger / 2
		if keepRecent < 10 {
			keepRecent = 10
		}
	}
	h := al.sessions.GetHistory(sessionKey)
	if len(h) <= trigger {
		return
	}
	removed := len(h) - keepRecent
	tpl := "[runtime-compaction] removed %d old messages, kept %d recent messages"
	note := fmt.Sprintf(tpl, removed, keepRecent)
	if al.sessions.CompactSession(sessionKey, keepRecent, note) {
		_ = al.sessions.Save(al.sessions.GetOrCreate(sessionKey))
	}
}

// RunStartupSelfCheckAllSessions runs startup compaction checks across loaded sessions.
func (al *AgentLoop) RunStartupSelfCheckAllSessions(ctx context.Context) StartupCompactionReport {
	report := StartupCompactionReport{TotalSessions: al.sessions.Count()}
	if !al.compactionEnabled {
		return report
	}

	trigger := al.compactionTrigger
	if trigger <= 0 {
		trigger = 60
	}
	keepRecent := al.compactionKeepRecent
	if keepRecent <= 0 || keepRecent >= trigger {
		keepRecent = trigger / 2
		if keepRecent < 10 {
			keepRecent = 10
		}
	}

	for _, key := range al.sessions.Keys() {
		select {
		case <-ctx.Done():
			return report
		default:
		}

		history := al.sessions.GetHistory(key)
		if len(history) <= trigger {
			continue
		}

		removed := len(history) - keepRecent
		tpl := "[startup-compaction] removed %d old messages, kept %d recent messages"
		note := fmt.Sprintf(tpl, removed, keepRecent)
		if al.sessions.CompactSession(key, keepRecent, note) {
			al.sessions.Save(al.sessions.GetOrCreate(key))
			report.CompactedSessions++
		}
	}

	return report
}

func (al *AgentLoop) GetStartupInfo() map[string]interface{} {
	info := make(map[string]interface{})

	// Tools info
	_tools := al.tools.List()
	info["tools"] = map[string]interface{}{
		"count": len(_tools),
		"names": _tools,
	}

	// Skills info
	info["skills"] = al.contextBuilder.GetSkillsInfo()

	return info
}

func (al *AgentLoop) GetToolCatalog() []map[string]interface{} {
	if al == nil || al.tools == nil {
		return nil
	}
	items := al.tools.Catalog()
	sort.Slice(items, func(i, j int) bool {
		return fmt.Sprint(items[i]["name"]) < fmt.Sprint(items[j]["name"])
	})
	for _, item := range items {
		if fmt.Sprint(item["source"]) != "mcp" {
			item["source"] = "local"
		}
		name := strings.TrimSpace(fmt.Sprint(item["name"]))
		item["visibility"] = map[string]interface{}{
			"main_agent": true,
			"subagent":   subagentToolVisibilityMode(name),
		}
	}
	return items
}

func subagentToolVisibilityMode(name string) string {
	if isImplicitlyAllowedSubagentTool(name) {
		return "inherited"
	}
	return "allowlist"
}

// formatMessagesForLog formats messages for logging
func formatMessagesForLog(messages []providers.Message) string {
	if len(messages) == 0 {
		return "[]"
	}

	var result string
	result += "[\n"
	for i, msg := range messages {
		result += fmt.Sprintf("  [%d] Role: %s\n", i, msg.Role)
		if msg.ToolCalls != nil && len(msg.ToolCalls) > 0 {
			result += "  ToolCalls:\n"
			for _, tc := range msg.ToolCalls {
				result += fmt.Sprintf("    - ID: %s, Type: %s, Name: %s\n", tc.ID, tc.Type, tc.Name)
				if tc.Function != nil {
					result += fmt.Sprintf("      Arguments: %s\n", truncateString(tc.Function.Arguments, 200))
				}
			}
		}
		if msg.Content != "" {
			content := truncateString(msg.Content, 200)
			result += fmt.Sprintf("  Content: %s\n", content)
		}
		if msg.ToolCallID != "" {
			result += fmt.Sprintf("  ToolCallID: %s\n", msg.ToolCallID)
		}
		result += "\n"
	}
	result += "]"
	return result
}

// formatToolsForLog formats tool definitions for logging
func formatToolsForLog(tools []providers.ToolDefinition) string {
	if len(tools) == 0 {
		return "[]"
	}

	var result string
	result += "[\n"
	for i, tool := range tools {
		result += fmt.Sprintf("  [%d] Type: %s, Name: %s\n", i, tool.Type, tool.Function.Name)
		result += fmt.Sprintf("      Description: %s\n", tool.Function.Description)
		if len(tool.Function.Parameters) > 0 {
			result += fmt.Sprintf("      Parameters: %s\n", truncateString(fmt.Sprintf("%v", tool.Function.Parameters), 200))
		}
	}
	result += "]"
	return result
}

// truncateString truncates a string to max length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func withToolContextArgs(toolName string, args map[string]interface{}, channel, chatID string) map[string]interface{} {
	if channel == "" || chatID == "" {
		return args
	}
	switch toolName {
	case "message", "spawn", "remind":
	default:
		return args
	}

	next := make(map[string]interface{}, len(args)+2)
	for k, v := range args {
		next[k] = v
	}

	if toolName == "message" {
		if _, ok := next["channel"]; !ok {
			next["channel"] = channel
		}
		if _, hasChat := next["chat_id"]; !hasChat {
			if _, hasTo := next["to"]; !hasTo {
				next["chat_id"] = chatID
			}
		}
		return next
	}

	if _, ok := next["channel"]; !ok {
		next["channel"] = channel
	}
	if _, ok := next["chat_id"]; !ok {
		next["chat_id"] = chatID
	}
	return next
}

func withToolRuntimeArgs(ctx context.Context, toolName string, args map[string]interface{}) map[string]interface{} {
	if strings.TrimSpace(toolName) != "skill_exec" {
		return args
	}
	ns := normalizeMemoryNamespace(memoryNamespaceFromContext(ctx))
	callerAgent := ns
	callerScope := "subagent"
	if callerAgent == "" || callerAgent == "main" {
		callerAgent = "main"
		callerScope = "main_agent"
	}
	next := make(map[string]interface{}, len(args)+2)
	for k, v := range args {
		next[k] = v
	}
	next["caller_agent"] = callerAgent
	next["caller_scope"] = callerScope
	return next
}

func (al *AgentLoop) executeToolCall(ctx context.Context, toolName string, args map[string]interface{}, currentChannel, currentChatID string) (string, error) {
	if err := ensureToolAllowedByContext(ctx, toolName, args); err != nil {
		return "", err
	}
	args = withToolMemoryNamespaceArgs(toolName, args, memoryNamespaceFromContext(ctx))
	args = withToolRuntimeArgs(ctx, toolName, args)
	if shouldSuppressSelfMessageSend(toolName, args, currentChannel, currentChatID) {
		return "Suppressed message tool self-send in current chat; assistant will reply via normal outbound.", nil
	}
	return al.tools.Execute(ctx, toolName, args)
}

func withToolMemoryNamespaceArgs(toolName string, args map[string]interface{}, namespace string) map[string]interface{} {
	ns := normalizeMemoryNamespace(namespace)
	if ns == "main" {
		return args
	}
	switch strings.TrimSpace(toolName) {
	case "memory_search", "memory_get", "memory_write":
	default:
		return args
	}

	if raw := tools.MapStringArg(args, "namespace"); raw != "" {
		return args
	}
	next := make(map[string]interface{}, len(args)+1)
	for k, v := range args {
		next[k] = v
	}
	next["namespace"] = ns
	return next
}

type agentContextKey string

const memoryNamespaceContextKey agentContextKey = "memory_namespace"
const toolAllowlistContextKey agentContextKey = "tool_allowlist"

func withMemoryNamespaceContext(ctx context.Context, namespace string) context.Context {
	ns := normalizeMemoryNamespace(namespace)
	if ns == "main" {
		return ctx
	}
	return context.WithValue(ctx, memoryNamespaceContextKey, ns)
}

func memoryNamespaceFromContext(ctx context.Context) string {
	if ctx == nil {
		return "main"
	}
	raw, _ := ctx.Value(memoryNamespaceContextKey).(string)
	return normalizeMemoryNamespace(raw)
}

func withToolAllowlistContext(ctx context.Context, allowlist []string) context.Context {
	normalized := normalizeToolAllowlist(allowlist)
	if len(normalized) == 0 {
		return ctx
	}
	return context.WithValue(ctx, toolAllowlistContextKey, normalized)
}

func toolAllowlistFromContext(ctx context.Context) map[string]struct{} {
	if ctx == nil {
		return nil
	}
	raw, _ := ctx.Value(toolAllowlistContextKey).(map[string]struct{})
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func ensureToolAllowedByContext(ctx context.Context, toolName string, args map[string]interface{}) error {
	allow := toolAllowlistFromContext(ctx)
	if len(allow) == 0 {
		return nil
	}

	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "" {
		return fmt.Errorf("tool name is empty")
	}
	if !isToolNameAllowed(allow, name) && !isImplicitlyAllowedSubagentTool(name) {
		return fmt.Errorf("tool '%s' is not allowed by subagent profile", toolName)
	}

	if name == "parallel" {
		if err := validateParallelAllowlistArgs(allow, args); err != nil {
			return err
		}
	}
	return nil
}

func validateParallelAllowlistArgs(allow map[string]struct{}, args map[string]interface{}) error {
	callsRaw, ok := args["calls"].([]interface{})
	if !ok {
		return nil
	}
	for i, call := range callsRaw {
		m, ok := call.(map[string]interface{})
		if !ok {
			continue
		}
		tool := tools.MapStringArg(m, "tool")
		name := strings.ToLower(strings.TrimSpace(tool))
		if name == "" {
			continue
		}
		if !isToolNameAllowed(allow, name) && !isImplicitlyAllowedSubagentTool(name) {
			return fmt.Errorf("tool 'parallel' contains disallowed call[%d]: %s", i, tool)
		}
	}
	return nil
}

func isImplicitlyAllowedSubagentTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "skill_exec":
		return true
	default:
		return false
	}
}

func normalizeToolAllowlist(in []string) map[string]struct{} {
	expanded := tools.ExpandToolAllowlistEntries(in)
	if len(expanded) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(expanded))
	for _, item := range expanded {
		name := strings.ToLower(strings.TrimSpace(item))
		if name == "" {
			continue
		}
		out[name] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isToolNameAllowed(allow map[string]struct{}, name string) bool {
	if len(allow) == 0 {
		return true
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	if _, ok := allow["*"]; ok {
		return true
	}
	if _, ok := allow["all"]; ok {
		return true
	}
	_, ok := allow[name]
	return ok
}

func resolveInboundMemoryNamespace(msg bus.InboundMessage) string {
	if msg.Channel == "system" {
		return "main"
	}
	if msg.Metadata == nil {
		return "main"
	}
	if v := strings.TrimSpace(msg.Metadata["memory_namespace"]); v != "" {
		return normalizeMemoryNamespace(v)
	}
	if v := strings.TrimSpace(msg.Metadata["memory_ns"]); v != "" {
		return normalizeMemoryNamespace(v)
	}
	return "main"
}

func shouldSuppressSelfMessageSend(toolName string, args map[string]interface{}, currentChannel, currentChatID string) bool {
	if strings.TrimSpace(toolName) != "message" {
		return false
	}
	action := strings.ToLower(tools.MapStringArg(args, "action"))
	if action == "" {
		action = "send"
	}
	if action != "send" {
		return false
	}

	targetChannel, targetChat := resolveMessageToolTarget(args, currentChannel, currentChatID)
	return targetChannel == strings.TrimSpace(currentChannel) && targetChat == strings.TrimSpace(currentChatID)
}

func resolveMessageToolTarget(args map[string]interface{}, fallbackChannel, fallbackChatID string) (string, string) {
	channel := tools.MapStringArg(args, "channel")
	if channel == "" {
		channel = strings.TrimSpace(fallbackChannel)
	}

	chatID := tools.MapStringArg(args, "chat_id")
	if to := tools.MapStringArg(args, "to"); to != "" {
		chatID = to
	}
	if chatID == "" {
		chatID = strings.TrimSpace(fallbackChatID)
	}
	return channel, chatID
}

func extractFirstSourceLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(t), "source:") {
			return t
		}
	}
	return ""
}

func shouldDropNoReply(text string) bool {
	t := strings.TrimSpace(text)
	return strings.EqualFold(t, "NO_REPLY")
}

func resolveSystemOrigin(chatID string) (string, string) {
	raw := strings.TrimSpace(chatID)
	if raw == "" {
		return "cli", "direct"
	}
	originChannel, originChatID, ok := strings.Cut(raw, ":")
	if !ok {
		return "cli", raw
	}
	originChannel = strings.TrimSpace(originChannel)
	originChatID = strings.TrimSpace(originChatID)
	switch {
	case originChannel == "" && originChatID == "":
		return "cli", "direct"
	case originChannel == "":
		return "cli", originChatID
	case originChatID == "":
		return originChannel, "direct"
	default:
		return originChannel, originChatID
	}
}

func isSubagentSystemMessage(msg bus.InboundMessage) bool {
	if msg.Channel != "system" {
		return false
	}
	if msg.Metadata != nil && strings.EqualFold(strings.TrimSpace(msg.Metadata["trigger"]), "subagent") {
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(msg.SenderID)), "subagent:")
}

func fallbackSubagentNotification(msg bus.InboundMessage) (string, bool) {
	if !isSubagentSystemMessage(msg) {
		return "", false
	}
	content := strings.TrimSpace(msg.Content)
	if content == "" {
		id := strings.TrimSpace(strings.TrimPrefix(msg.SenderID, "subagent:"))
		if id == "" {
			id = "unknown"
		}
		content = fmt.Sprintf("Subagent %s completed.", id)
	}
	return content, true
}

func parseSubagentSystemContent(content string) (string, string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", ""
	}
	lines := strings.Split(content, "\n")
	taskSummary := ""
	resultSummary := ""
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		lower := strings.ToLower(t)
		switch {
		case strings.HasPrefix(lower, "task:"):
			taskSummary = strings.TrimSpace(t[len("task:"):])
		case strings.HasPrefix(lower, "summary:"):
			resultSummary = strings.TrimSpace(t[len("summary:"):])
		}
	}
	if taskSummary == "" && len(lines) > 0 {
		taskSummary = summarizeSystemNotificationText(content, 120)
	}
	return taskSummary, resultSummary
}

func summarizeSystemNotificationText(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max-3]) + "..."
	}
	return s
}

func (al *AgentLoop) enqueueSubagentDigest(msg bus.InboundMessage, item subagentDigestItem) {
	if al == nil || al.bus == nil {
		return
	}
	originChannel, originChatID := resolveSystemOrigin(msg.ChatID)
	key := originChannel + "\x00" + originChatID
	delay := al.subagentDigestDelay
	if delay <= 0 {
		delay = 5 * time.Second
	}

	al.subagentDigestMu.Lock()
	state, ok := al.subagentDigests[key]
	if !ok || state == nil {
		state = &subagentDigestState{
			channel: originChannel,
			chatID:  originChatID,
			items:   map[string]subagentDigestItem{},
		}
		al.subagentDigests[key] = state
	}
	itemKey := subagentDigestItemKey(item)
	state.items[itemKey] = item
	state.dueAt = time.Now().Add(delay)
	al.subagentDigestMu.Unlock()
}

func subagentDigestItemKey(item subagentDigestItem) string {
	agentID := strings.ToLower(strings.TrimSpace(item.agentID))
	reason := strings.ToLower(strings.TrimSpace(item.reason))
	task := strings.ToLower(strings.TrimSpace(item.taskSummary))
	if agentID == "" {
		agentID = "subagent"
	}
	return agentID + "\x00" + reason + "\x00" + task
}

func (al *AgentLoop) flushSubagentDigest(key string) {
	if al == nil || al.bus == nil {
		return
	}
	al.subagentDigestMu.Lock()
	state := al.subagentDigests[key]
	delete(al.subagentDigests, key)
	al.subagentDigestMu.Unlock()
	if state == nil || len(state.items) == 0 {
		return
	}
	content := formatSubagentDigestSummary(state.items)
	if strings.TrimSpace(content) == "" {
		return
	}
	al.bus.PublishOutbound(bus.OutboundMessage{
		Channel: state.channel,
		ChatID:  state.chatID,
		Content: content,
	})
}

func (al *AgentLoop) runSubagentDigestTicker() {
	if al == nil {
		return
	}
	tick := tools.GlobalWatchdogTick
	if tick <= 0 {
		tick = time.Second
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for now := range ticker.C {
		al.flushDueSubagentDigests(now)
	}
}

func (al *AgentLoop) flushDueSubagentDigests(now time.Time) {
	if al == nil || al.bus == nil {
		return
	}
	dueKeys := make([]string, 0, 4)
	al.subagentDigestMu.Lock()
	for key, state := range al.subagentDigests {
		if state == nil || state.dueAt.IsZero() || now.Before(state.dueAt) {
			continue
		}
		dueKeys = append(dueKeys, key)
	}
	al.subagentDigestMu.Unlock()
	for _, key := range dueKeys {
		al.flushSubagentDigest(key)
	}
}

func formatSubagentDigestSummary(items map[string]subagentDigestItem) string {
	if len(items) == 0 {
		return ""
	}
	list := make([]subagentDigestItem, 0, len(items))
	completed := 0
	blocked := 0
	milestone := 0
	failed := 0
	for _, item := range items {
		list = append(list, item)
		switch {
		case strings.EqualFold(strings.TrimSpace(item.reason), "blocked"):
			blocked++
		case strings.EqualFold(strings.TrimSpace(item.reason), "milestone"):
			milestone++
		case strings.EqualFold(strings.TrimSpace(item.status), "failed"):
			failed++
		default:
			completed++
		}
	}
	sort.Slice(list, func(i, j int) bool {
		left := strings.TrimSpace(list[i].agentID)
		right := strings.TrimSpace(list[j].agentID)
		if left == right {
			return strings.TrimSpace(list[i].taskSummary) < strings.TrimSpace(list[j].taskSummary)
		}
		return left < right
	})
	var sb strings.Builder
	sb.WriteString("阶段总结")
	stats := make([]string, 0, 4)
	if completed > 0 {
		stats = append(stats, fmt.Sprintf("完成 %d", completed))
	}
	if blocked > 0 {
		stats = append(stats, fmt.Sprintf("受阻 %d", blocked))
	}
	if failed > 0 {
		stats = append(stats, fmt.Sprintf("失败 %d", failed))
	}
	if milestone > 0 {
		stats = append(stats, fmt.Sprintf("进展 %d", milestone))
	}
	if len(stats) > 0 {
		sb.WriteString("（" + strings.Join(stats, "，") + "）")
	}
	sb.WriteString("\n")
	for _, item := range list {
		agentLabel := strings.TrimSpace(item.agentID)
		if agentLabel == "" {
			agentLabel = "subagent"
		}
		statusText := "已完成"
		switch {
		case strings.EqualFold(strings.TrimSpace(item.reason), "blocked"):
			statusText = "受阻"
		case strings.EqualFold(strings.TrimSpace(item.reason), "milestone"):
			statusText = "有进展"
		case strings.EqualFold(strings.TrimSpace(item.status), "failed"):
			statusText = "失败"
		}
		sb.WriteString("- " + agentLabel + "：" + statusText)
		if task := strings.TrimSpace(item.taskSummary); task != "" {
			sb.WriteString("，任务：" + task)
		}
		if summary := strings.TrimSpace(item.resultSummary); summary != "" {
			label := "摘要"
			if statusText == "受阻" {
				label = "原因"
			} else if statusText == "有进展" {
				label = "进度"
			}
			sb.WriteString("，" + label + "：" + summary)
		}
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

func shouldFlushTelegramStreamSnapshot(s string) bool {
	s = strings.TrimRight(s, " \t")
	if s == "" {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(s)
	switch last {
	case '\n', '。', '！', '？', '.', '!', '?', ';', '；', ':', '：':
	default:
		return false
	}
	// Avoid flushing while code fences are still unbalanced.
	if strings.Count(s, "```")%2 == 1 {
		return false
	}
	// Avoid flushing while common inline markdown markers are unbalanced.
	if strings.Count(s, "**")%2 == 1 || strings.Count(s, "__")%2 == 1 || strings.Count(s, "~~")%2 == 1 {
		return false
	}
	// Rough guard for links/images: require bracket balance before flushing.
	if strings.Count(s, "[") != strings.Count(s, "]") || strings.Count(s, "(") != strings.Count(s, ")") {
		return false
	}
	return true
}

func parseReplyTag(text string, currentMessageID string) (content string, replyToID string) {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "[[") {
		return text, ""
	}
	end := strings.Index(t, "]]")
	if end <= 0 {
		return text, ""
	}
	rawTag := strings.TrimSpace(t[2:end])
	tag := strings.ToLower(rawTag)
	if strings.HasPrefix(tag, "reply_to_current") || strings.HasPrefix(tag, "reply_to") {
		content = strings.TrimSpace(t[end+2:])
		if strings.HasPrefix(tag, "reply_to:") {
			replyToID = strings.TrimSpace(rawTag[len("reply_to:"):])
		} else if strings.HasPrefix(tag, "reply_to_current") {
			replyToID = strings.TrimSpace(currentMessageID)
		}
		return content, replyToID
	}
	return text, ""
}

func alSessionListForTool(sm *session.SessionManager, limit int) []tools.SessionInfo {
	items := sm.List(limit)
	out := make([]tools.SessionInfo, 0, len(items))
	for i := range items {
		s := &items[i]
		out = append(out, tools.SessionInfo{
			Key:             s.Key,
			Kind:            s.Kind,
			Summary:         s.Summary,
			CompactionCount: s.CompactionCount,
			UpdatedAt:       s.Updated,
		})
	}
	return out
}
