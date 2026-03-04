// ClawGo - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 ClawGo contributors

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
	"clawgo/pkg/cron"
	"clawgo/pkg/ekg"
	"clawgo/pkg/logger"
	"clawgo/pkg/nodes"
	"clawgo/pkg/providers"
	"clawgo/pkg/scheduling"
	"clawgo/pkg/session"
	"clawgo/pkg/tools"
)

type AgentLoop struct {
	bus                   *bus.MessageBus
	provider              providers.LLMProvider
	workspace             string
	model                 string
	maxIterations         int
	sessions              *session.SessionManager
	contextBuilder        *ContextBuilder
	tools                 *tools.ToolRegistry
	compactionEnabled     bool
	compactionTrigger     int
	compactionKeepRecent  int
	heartbeatAckMaxChars  int
	memoryRecallKeywords  []string
	noResponseFallback    string
	thinkOnlyFallback     string
	langUsage             string
	langInvalid           string
	langUpdatedTemplate   string
	runtimeCompactionNote string
	startupCompactionNote string
	systemRewriteTemplate string
	audit                 *triggerAudit
	running               bool
	intentMu              sync.RWMutex
	intentHints           map[string]string
	sessionScheduler      *SessionScheduler
	providerNames         []string
	providerPool          map[string]providers.LLMProvider
	providerResponses     map[string]config.ProviderResponsesConfig
	telegramStreaming     bool
	ekg                   *ekg.Engine
	providerMu            sync.RWMutex
	sessionProvider       map[string]string
	streamMu              sync.Mutex
	sessionStreamed       map[string]bool
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
	nodesRouter := &nodes.Router{P2P: &nodes.StubP2PTransport{}, Relay: &nodes.HTTPRelayTransport{Manager: nodesManager}}
	toolsRegistry.Register(tools.NewNodesTool(nodesManager, nodesRouter, filepath.Join(workspace, "memory", "nodes-dispatch-audit.jsonl")))

	if cs != nil {
		toolsRegistry.Register(tools.NewRemindTool(cs))
		toolsRegistry.Register(tools.NewCronTool(cs))
	}

	maxParallelCalls := cfg.Agents.Defaults.RuntimeControl.ToolMaxParallelCalls
	if maxParallelCalls <= 0 {
		maxParallelCalls = 4
	}
	parallelSafe := make(map[string]struct{})
	for _, name := range cfg.Agents.Defaults.RuntimeControl.ToolParallelSafeNames {
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
	orchestrator := tools.NewOrchestrator()
	subagentManager := tools.NewSubagentManager(provider, workspace, msgBus, orchestrator)
	spawnTool := tools.NewSpawnTool(subagentManager)
	toolsRegistry.Register(spawnTool)
	toolsRegistry.Register(tools.NewSubagentsTool(subagentManager, cfg.Agents.Defaults.Texts.SubagentsNone, cfg.Agents.Defaults.Texts.UnsupportedAction))
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
		cfg.Agents.Defaults.Texts.SessionsNone,
		cfg.Agents.Defaults.Texts.UnsupportedAction,
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
		bus:                   msgBus,
		provider:              provider,
		workspace:             workspace,
		model:                 provider.GetDefaultModel(),
		maxIterations:         cfg.Agents.Defaults.MaxToolIterations,
		sessions:              sessionsManager,
		contextBuilder:        NewContextBuilder(workspace, func() []string { return toolsRegistry.GetSummaries() }),
		tools:                 toolsRegistry,
		compactionEnabled:     cfg.Agents.Defaults.ContextCompaction.Enabled,
		compactionTrigger:     cfg.Agents.Defaults.ContextCompaction.TriggerMessages,
		compactionKeepRecent:  cfg.Agents.Defaults.ContextCompaction.KeepRecentMessages,
		heartbeatAckMaxChars:  cfg.Agents.Defaults.Heartbeat.AckMaxChars,
		memoryRecallKeywords:  cfg.Agents.Defaults.Texts.MemoryRecallKeywords,
		noResponseFallback:    cfg.Agents.Defaults.Texts.NoResponseFallback,
		thinkOnlyFallback:     cfg.Agents.Defaults.Texts.ThinkOnlyFallback,
		langUsage:             cfg.Agents.Defaults.Texts.LangUsage,
		langInvalid:           cfg.Agents.Defaults.Texts.LangInvalid,
		langUpdatedTemplate:   cfg.Agents.Defaults.Texts.LangUpdatedTemplate,
		runtimeCompactionNote: cfg.Agents.Defaults.Texts.RuntimeCompactionNote,
		startupCompactionNote: cfg.Agents.Defaults.Texts.StartupCompactionNote,
		systemRewriteTemplate: cfg.Agents.Defaults.Texts.SystemRewriteTemplate,
		audit:                 newTriggerAudit(workspace),
		running:               false,
		intentHints:           map[string]string{},
		sessionScheduler:      NewSessionScheduler(0),
		ekg:                   ekg.New(workspace),
		sessionProvider:       map[string]string{},
		sessionStreamed:       map[string]bool{},
		providerResponses:     map[string]config.ProviderResponsesConfig{},
		telegramStreaming:     cfg.Channels.Telegram.Streaming,
	}
	// Initialize provider fallback chain (primary + proxy_fallbacks).
	loop.providerPool = map[string]providers.LLMProvider{}
	loop.providerNames = []string{}
	primaryName := cfg.Agents.Defaults.Proxy
	if primaryName == "" {
		primaryName = "proxy"
	}
	loop.providerPool[primaryName] = provider
	loop.providerNames = append(loop.providerNames, primaryName)
	if strings.TrimSpace(primaryName) == "proxy" {
		loop.providerResponses[primaryName] = cfg.Providers.Proxy.Responses
	} else if pc, ok := cfg.Providers.Proxies[primaryName]; ok {
		loop.providerResponses[primaryName] = pc.Responses
	}
	for _, name := range cfg.Agents.Defaults.ProxyFallbacks {
		if name == "" {
			continue
		}
		dup := false
		for _, existing := range loop.providerNames {
			if existing == name {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		if p2, err := providers.CreateProviderByName(cfg, name); err == nil {
			loop.providerPool[name] = p2
			loop.providerNames = append(loop.providerNames, name)
			if pc, ok := cfg.Providers.Proxies[name]; ok {
				loop.providerResponses[name] = pc.Responses
			}
		}
	}

	// Inject recursive run logic so subagents can use full tool-calling flows.
	subagentManager.SetRunFunc(func(ctx context.Context, task, channel, chatID string) (string, error) {
		sessionKey := fmt.Sprintf("subagent:%d", os.Getpid()) // Use PID/randomized key to reduce session key collisions.
		return loop.ProcessDirect(ctx, task, sessionKey)
	})

	return loop
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
	if len(al.providerNames) <= 1 {
		return nil, "", primaryErr
	}
	lastErr := primaryErr
	candidates := append([]string(nil), al.providerNames[1:]...)
	if al.ekg != nil {
		errSig := ""
		if primaryErr != nil {
			errSig = primaryErr.Error()
		}
		candidates = al.ekg.RankProvidersForError(candidates, errSig)
	}
	for _, name := range candidates {
		p, ok := al.providerPool[name]
		if !ok || p == nil {
			continue
		}
		resp, err := p.Chat(ctx, messages, toolDefs, al.model, options)
		if al.ekg != nil {
			st := "success"
			lg := "fallback provider success"
			errSig := ""
			if err != nil {
				st = "error"
				lg = err.Error()
				errSig = err.Error()
			}
			al.ekg.Record(ekg.Event{Session: msg.SessionKey, Channel: msg.Channel, Source: "provider_fallback", Status: st, Provider: name, Model: al.model, ErrSig: errSig, Log: lg})
		}
		if err == nil {
			logger.WarnCF("agent", logger.C0150, map[string]interface{}{"provider": name})
			return resp, name, nil
		}
		lastErr = err
	}
	return nil, "", lastErr
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
	case "autonomy":
		norm := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(msg.Content, "\n", " ")))
		if len(norm) > 180 {
			norm = norm[:180]
		}
		h := fnv.New32a()
		_, _ = h.Write([]byte(msg.SessionKey + "|" + norm))
		return fmt.Sprintf("autonomy:%08x", h.Sum32())
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
		"idle_run":      source == "autonomy",
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
	if v := strings.TrimSpace(os.Getenv("CLAWGO_SESSION_SHARDS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 64 {
				return 64
			}
			return n
		}
	}
	n := runtime.NumCPU()
	if n < 2 {
		n = 2
	}
	if n > 16 {
		n = 16
	}
	return n
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
	if !strings.HasPrefix(r, "HEARTBEAT_OK") {
		return false
	}

	maxChars := al.heartbeatAckMaxChars
	if maxChars <= 0 {
		maxChars = 64
	}
	return len(r) <= maxChars
}

func (al *AgentLoop) prepareOutbound(msg bus.InboundMessage, response string) (bus.OutboundMessage, bool) {
	if shouldDropNoReply(response) {
		return bus.OutboundMessage{}, false
	}
	currentMsgID := ""
	if msg.Metadata != nil {
		currentMsgID = msg.Metadata["message_id"]
	}
	clean, replyToID := parseReplyTag(response, currentMsgID)
	clean = strings.TrimSpace(clean)
	if clean == "" {
		return bus.OutboundMessage{}, false
	}
	if al.shouldSuppressOutbound(msg, clean) {
		return bus.OutboundMessage{}, false
	}
	outbound := bus.OutboundMessage{Channel: msg.Channel, ChatID: msg.ChatID, Content: clean, ReplyToID: strings.TrimSpace(replyToID)}
	if msg.Channel == "system" {
		if originChannel, originChatID, ok := strings.Cut(msg.ChatID, ":"); ok && strings.TrimSpace(originChannel) != "" {
			outbound.Channel = originChannel
			outbound.ChatID = originChatID
		} else {
			outbound.Channel = "cli"
		}
	}
	return outbound, true
}

func (al *AgentLoop) ProcessDirect(ctx context.Context, content, sessionKey string) (string, error) {
	msg := bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "user",
		ChatID:     "direct",
		Content:    content,
		SessionKey: sessionKey,
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
	release, err := al.acquireSessionResources(ctx, &msg)
	if err != nil {
		return "", err
	}
	defer release()
	if len(al.providerNames) > 0 {
		al.setSessionProvider(msg.SessionKey, al.providerNames[0])
	}
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

	// Explicit language command: /lang <code>
	if strings.HasPrefix(msg.Content, "/lang") {
		parts := strings.Fields(msg.Content)
		if len(parts) < 2 {
			preferred, last := al.sessions.GetLanguagePreferences(msg.SessionKey)
			if preferred == "" {
				preferred = "(auto)"
			}
			if last == "" {
				last = "(none)"
			}
			usage := strings.TrimSpace(al.langUsage)
			if usage == "" {
				usage = "Usage: /lang <code>"
			}
			return fmt.Sprintf("%s\nCurrent preferred: %s\nLast detected: %s", usage, preferred, last), nil
		}
		lang := normalizeLang(parts[1])
		if lang == "" {
			invalid := strings.TrimSpace(al.langInvalid)
			if invalid == "" {
				invalid = "Invalid language code."
			}
			return invalid, nil
		}
		al.sessions.SetPreferredLanguage(msg.SessionKey, lang)
		al.sessions.Save(al.sessions.GetOrCreate(msg.SessionKey))
		tpl := strings.TrimSpace(al.langUpdatedTemplate)
		if tpl == "" {
			tpl = "Language preference updated to %s"
		}
		return fmt.Sprintf(tpl, lang), nil
	}

	// Update tool contexts
	if tool, ok := al.tools.Get("message"); ok {
		if mt, ok := tool.(*tools.MessageTool); ok {
			mt.SetContext(msg.Channel, msg.ChatID)
		}
	}
	if tool, ok := al.tools.Get("spawn"); ok {
		if st, ok := tool.(*tools.SpawnTool); ok {
			st.SetContext(msg.Channel, msg.ChatID)
		}
	}
	if tool, ok := al.tools.Get("remind"); ok {
		if rt, ok := tool.(*tools.RemindTool); ok {
			rt.SetContext(msg.Channel, msg.ChatID)
		}
	}

	history := al.sessions.GetHistory(msg.SessionKey)
	summary := al.sessions.GetSummary(msg.SessionKey)
	memoryRecallUsed := false
	memoryRecallText := ""
	if shouldRecallMemory(msg.Content, al.memoryRecallKeywords) {
		if recall, err := al.tools.Execute(ctx, "memory_search", map[string]interface{}{"query": msg.Content, "maxResults": 3}); err == nil && strings.TrimSpace(recall) != "" {
			memoryRecallUsed = true
			memoryRecallText = strings.TrimSpace(recall)
			summary = strings.TrimSpace(summary + "\n\n[Memory Recall]\n" + memoryRecallText)
		}
	}
	if explicitPref := ExtractLanguagePreference(msg.Content); explicitPref != "" {
		al.sessions.SetPreferredLanguage(msg.SessionKey, explicitPref)
	}
	preferredLang, lastLang := al.sessions.GetLanguagePreferences(msg.SessionKey)
	responseLang := DetectResponseLanguage(msg.Content, preferredLang, lastLang)

	al.updateIntentHint(msg.SessionKey, msg.Content)
	effectiveUserContent := al.applyIntentHint(msg.SessionKey, msg.Content)

	messages := al.contextBuilder.BuildMessages(
		history,
		summary,
		effectiveUserContent,
		nil,
		msg.Channel,
		msg.ChatID,
		responseLang,
	)

	iteration := 0
	var finalContent string
	hasToolActivity := false
	lastToolOutputs := make([]string, 0, 4)
	maxAllowed := al.maxIterations
	if maxAllowed < 1 {
		maxAllowed = 1
	}
	// CLAWGO_MAX_TOOL_ITERATIONS:
	//   0 or unset => no fixed cap, keep extending while tool chain progresses
	//   >0         => explicit ceiling
	hardCap := 0
	if v := os.Getenv("CLAWGO_MAX_TOOL_ITERATIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			hardCap = n
		}
	}
	for iteration < maxAllowed {
		iteration++

		logger.DebugCF("agent", logger.C0151,
			map[string]interface{}{
				"iteration": iteration,
				"max":       al.maxIterations,
			})

		toolDefs := al.tools.GetDefinitions()
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
			if fb, fbProvider, ferr := al.tryFallbackProviders(ctx, msg, messages, providerToolDefs, options, err); ferr == nil && fb != nil {
				response = fb
				err = nil
				if fbProvider != "" {
					al.setSessionProvider(msg.SessionKey, fbProvider)
				}
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
		if hardCap > 0 {
			if maxAllowed < hardCap {
				maxAllowed = hardCap
			}
		} else {
			// No fixed cap: extend rolling window as long as tools keep chaining.
			if maxAllowed < iteration+al.maxIterations {
				maxAllowed = iteration + al.maxIterations
			}
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

			result, err := al.tools.Execute(ctx, tc.Name, tc.Arguments)
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
	if finalContent == "" {
		if hasToolActivity && len(lastToolOutputs) > 0 {
			finalContent = "我已执行完成，关键信息如下：\n- " + strings.Join(lastToolOutputs, "\n- ")
		} else {
			fallback := strings.TrimSpace(al.noResponseFallback)
			if fallback == "" {
				fallback = "在的，我刚刚这条回复丢了。请再说一次，我马上处理。"
			}
			finalContent = fallback
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
			fallback := strings.TrimSpace(al.thinkOnlyFallback)
			if fallback == "" {
				fallback = "Thinking process completed."
			}
			userContent = fallback
		}
	}

	if memoryRecallUsed && !strings.Contains(strings.ToLower(userContent), "source:") {
		if src := extractFirstSourceLine(memoryRecallText); src != "" {
			userContent = strings.TrimSpace(userContent + "\n\n" + src)
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
	userText := strings.TrimSpace(msg.Content)
	respText := strings.TrimSpace(response)
	if userText == "" && respText == "" {
		return
	}
	// Avoid noisy heartbeat/system boilerplate.
	lc := strings.ToLower(userText)
	if strings.Contains(lc, "heartbeat") && strings.Contains(strings.ToLower(respText), "heartbeat_ok") {
		return
	}
	ms := NewMemoryStore(al.workspace)
	line := fmt.Sprintf("- [%s] channel=%s session=%s\n  - user: %s\n  - result: %s",
		time.Now().Format("15:04"),
		strings.TrimSpace(msg.Channel),
		strings.TrimSpace(msg.SessionKey),
		truncate(strings.ReplaceAll(userText, "\n", " "), 180),
		truncate(strings.ReplaceAll(respText, "\n", " "), 220),
	)
	if err := ms.AppendToday(line); err != nil {
		logger.WarnCF("agent", logger.C0158, map[string]interface{}{logger.FieldError: err.Error()})
	}
}

func (al *AgentLoop) updateIntentHint(sessionKey, content string) {
	content = strings.TrimSpace(content)
	if sessionKey == "" || content == "" {
		return
	}
	lower := strings.ToLower(content)

	// Cron natural-language intent: avoid searching project files for user timer ops.
	if strings.Contains(lower, "cron") || strings.Contains(lower, "schedule") || strings.Contains(lower, "timer") || strings.Contains(lower, "reminder") {
		hint := "Prioritize the cron tool for timer operations: list=action=list; delete=action=delete(id); enable/disable=action=enable/disable. Do not switch to grepping project files for cron text."
		al.intentMu.Lock()
		al.intentHints[sessionKey] = hint + " User details=" + content
		al.intentMu.Unlock()
		return
	}

	if !strings.Contains(lower, "commit") && !strings.Contains(lower, "push") {
		if strings.HasPrefix(content, "1.") || strings.HasPrefix(content, "2.") {
			al.intentMu.Lock()
			if prev := strings.TrimSpace(al.intentHints[sessionKey]); prev != "" {
				al.intentHints[sessionKey] = prev + " | " + content
			}
			al.intentMu.Unlock()
		}
		return
	}
	hint := "Execute as one transaction: complete commit+push in one pass with branch/scope confirmation."
	if strings.Contains(lower, "all branches") {
		hint += " Scope=all branches."
	}
	al.intentMu.Lock()
	al.intentHints[sessionKey] = hint + " User details=" + content
	al.intentMu.Unlock()
}

func (al *AgentLoop) applyIntentHint(sessionKey, content string) string {
	al.intentMu.RLock()
	hint := strings.TrimSpace(al.intentHints[sessionKey])
	al.intentMu.RUnlock()
	if hint == "" {
		return content
	}
	lower := strings.ToLower(strings.TrimSpace(content))
	if strings.Contains(lower, "commit") || strings.Contains(lower, "push") || strings.HasPrefix(content, "1.") || strings.HasPrefix(content, "2.") || strings.Contains(lower, "cron") || strings.Contains(lower, "schedule") || strings.Contains(lower, "timer") || strings.Contains(lower, "reminder") {
		return "[Intent Slot]\n" + hint + "\n\n[User Message]\n" + content
	}
	return content
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

	msg.Content = rewriteSystemMessageContent(msg.Content, al.systemRewriteTemplate)

	// Parse origin from chat_id (format: "channel:chat_id")
	var originChannel, originChatID string
	if idx := strings.Index(msg.ChatID, ":"); idx > 0 {
		originChannel = msg.ChatID[:idx]
		originChatID = msg.ChatID[idx+1:]
	} else {
		// Fallback
		originChannel = "cli"
		originChatID = msg.ChatID
	}

	// Use the origin session for context
	sessionKey := fmt.Sprintf("%s:%s", originChannel, originChatID)

	// Update tool contexts to original channel/chatID
	if tool, ok := al.tools.Get("message"); ok {
		if mt, ok := tool.(*tools.MessageTool); ok {
			mt.SetContext(originChannel, originChatID)
		}
	}
	if tool, ok := al.tools.Get("spawn"); ok {
		if st, ok := tool.(*tools.SpawnTool); ok {
			st.SetContext(originChannel, originChatID)
		}
	}

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

		toolDefs := al.tools.GetDefinitions()
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
			if fb, fbProvider, ferr := al.tryFallbackProviders(ctx, msg, messages, providerToolDefs, options, err); ferr == nil && fb != nil {
				response = fb
				err = nil
				if fbProvider != "" {
					al.setSessionProvider(msg.SessionKey, fbProvider)
				}
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
			result, err := al.tools.Execute(ctx, tc.Name, tc.Arguments)
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
		name, _ := fnRaw["name"].(string)
		description, _ := fnRaw["description"].(string)
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

func (al *AgentLoop) buildResponsesOptions(sessionKey string, maxTokens int64, temperature float64) map[string]interface{} {
	options := map[string]interface{}{
		"max_tokens":  maxTokens,
		"temperature": temperature,
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
	tpl := strings.TrimSpace(al.runtimeCompactionNote)
	if tpl == "" {
		tpl = "[runtime-compaction] removed %d old messages, kept %d recent messages"
	}
	note := fmt.Sprintf(tpl, removed, keepRecent)
	if al.sessions.CompactSession(sessionKey, keepRecent, note) {
		al.sessions.Save(al.sessions.GetOrCreate(sessionKey))
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
		tpl := strings.TrimSpace(al.startupCompactionNote)
		if tpl == "" {
			tpl = "[startup-compaction] removed %d old messages, kept %d recent messages"
		}
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
	tools := al.tools.List()
	info["tools"] = map[string]interface{}{
		"count": len(tools),
		"names": tools,
	}

	// Skills info
	info["skills"] = al.contextBuilder.GetSkillsInfo()

	return info
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

func shouldRecallMemory(text string, keywords []string) bool {
	s := strings.ToLower(strings.TrimSpace(text))
	if s == "" {
		return false
	}
	if len(keywords) == 0 {
		keywords = []string{"remember", "preference", "todo", "decision", "date", "when did", "what did we"}
	}
	for _, k := range keywords {
		kk := strings.ToLower(strings.TrimSpace(k))
		if kk != "" && strings.Contains(s, kk) {
			return true
		}
	}
	return false
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

func rewriteSystemMessageContent(content, template string) string {
	c := strings.TrimSpace(content)
	if !strings.HasPrefix(c, "[System Message]") {
		return content
	}
	body := strings.TrimSpace(strings.TrimPrefix(c, "[System Message]"))
	if body == "" {
		return "Please summarize the system event in concise user-facing language."
	}
	tpl := strings.TrimSpace(template)
	if tpl == "" {
		tpl = "Rewrite the following internal system update in concise user-facing language:\n\n%s"
	}
	if strings.Contains(tpl, "%s") {
		return fmt.Sprintf(tpl, body)
	}
	return tpl + "\n\n" + body
}

func alSessionListForTool(sm *session.SessionManager, limit int) []tools.SessionInfo {
	items := sm.List(limit)
	out := make([]tools.SessionInfo, 0, len(items))
	for _, s := range items {
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
