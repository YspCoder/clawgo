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

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
	"clawgo/pkg/cron"
	"clawgo/pkg/logger"
	"clawgo/pkg/nodes"
	"clawgo/pkg/providers"
	"clawgo/pkg/session"
	"clawgo/pkg/tools"
)

type AgentLoop struct {
	bus                    *bus.MessageBus
	provider               providers.LLMProvider
	workspace              string
	model                  string
	maxIterations          int
	sessions               *session.SessionManager
	contextBuilder         *ContextBuilder
	tools                  *tools.ToolRegistry
	compactionEnabled      bool
	compactionTrigger      int
	compactionKeepRecent   int
	heartbeatAckMaxChars    int
	memoryRecallKeywords    []string
	noResponseFallback      string
	thinkOnlyFallback       string
	langUsage               string
	langInvalid             string
	langUpdatedTemplate     string
	runtimeCompactionNote   string
	startupCompactionNote   string
	systemRewriteTemplate   string
	audit                   *triggerAudit
	running                 bool
	intentMu                sync.RWMutex
	intentHints             map[string]string
	sessionRunMu            sync.Mutex
	sessionRunLocks         map[string]*sync.Mutex
}

type executionTxnState struct {
	commitIntent bool
	commitSeen   bool
	pushSeen     bool
	pushOK       bool
}
// StartupCompactionReport provides startup memory/session maintenance stats.
type StartupCompactionReport struct {
	TotalSessions     int `json:"total_sessions"`
	CompactedSessions int `json:"compacted_sessions"`
}

func NewAgentLoop(cfg *config.Config, msgBus *bus.MessageBus, provider providers.LLMProvider, cs *cron.CronService) *AgentLoop {
	workspace := cfg.WorkspacePath()
	os.MkdirAll(workspace, 0755)

	sessionsManager := session.NewSessionManager(filepath.Join(filepath.Dir(cfg.WorkspacePath()), "sessions"))

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
		bus:                  msgBus,
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
		sessionRunLocks:       map[string]*sync.Mutex{},
	}

	// 注入递归运行逻辑，使 subagent 具备 full tool-calling 能力
	subagentManager.SetRunFunc(func(ctx context.Context, task, channel, chatID string) (string, error) {
		sessionKey := fmt.Sprintf("subagent:%d", os.Getpid()) // 改用 PID 或随机数，避免 sessionKey 冲突
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
	logger.InfoCF("agent", "Session-sharded dispatcher enabled", map[string]interface{}{"shards": count})
	return shards
}

func (al *AgentLoop) lockSessionRun(sessionKey string) func() {
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		key = "default"
	}
	al.sessionRunMu.Lock()
	mu, ok := al.sessionRunLocks[key]
	if !ok {
		mu = &sync.Mutex{}
		al.sessionRunLocks[key] = mu
	}
	al.sessionRunMu.Unlock()
	mu.Lock()
	return func() { mu.Unlock() }
}

func (al *AgentLoop) processInbound(ctx context.Context, msg bus.InboundMessage) {
	response, err := al.processMessage(ctx, msg)
	if err != nil {
		response = fmt.Sprintf("Error processing message: %v", err)
	}

	trigger := al.getTrigger(msg)
	suppressed := false
	if response != "" {
		if outbound, ok := al.prepareOutbound(msg, response); ok {
			al.bus.PublishOutbound(outbound)
		} else {
			suppressed = true
		}
	}
	al.audit.Record(trigger, msg.Channel, msg.SessionKey, suppressed, err)
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
	return bus.OutboundMessage{
		Channel:   msg.Channel,
		ChatID:    msg.ChatID,
		Content:   clean,
		ReplyToID: strings.TrimSpace(replyToID),
	}, true
}

func (al *AgentLoop) ProcessDirect(ctx context.Context, content, sessionKey string) (string, error) {
	msg := bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "user",
		ChatID:     "direct",
		Content:    content,
		SessionKey: sessionKey,
	}

	return al.processMessage(ctx, msg)
}

func (al *AgentLoop) GetSessionHistory(sessionKey string) []providers.Message {
	return al.sessions.GetHistory(sessionKey)
}

func (al *AgentLoop) processMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	unlock := al.lockSessionRun(msg.SessionKey)
	defer unlock()
	// Add message preview to log
	preview := truncate(msg.Content, 80)
	logger.InfoCF("agent", fmt.Sprintf("Processing message from %s:%s: %s", msg.Channel, msg.SenderID, preview),
		map[string]interface{}{
			"channel":     msg.Channel,
			"chat_id":     msg.ChatID,
			"sender_id":   msg.SenderID,
			"session_key": msg.SessionKey,
		})

	// Route system messages to processSystemMessage
	if msg.Channel == "system" {
		return al.processSystemMessage(ctx, msg)
	}

	// Explicit language command: /lang <code>
	if strings.HasPrefix(strings.TrimSpace(msg.Content), "/lang") {
		parts := strings.Fields(strings.TrimSpace(msg.Content))
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
	txn := executionTxnState{commitIntent: isCommitPushIntent(effectiveUserContent)}

	for iteration < al.maxIterations {
		iteration++

		logger.DebugCF("agent", "LLM iteration",
			map[string]interface{}{
				"iteration": iteration,
				"max":       al.maxIterations,
			})

		toolDefs := al.tools.GetDefinitions()
		providerToolDefs := make([]providers.ToolDefinition, 0, len(toolDefs))
		for _, td := range toolDefs {
			providerToolDefs = append(providerToolDefs, providers.ToolDefinition{
				Type: td["type"].(string),
				Function: providers.ToolFunctionDefinition{
					Name:        td["function"].(map[string]interface{})["name"].(string),
					Description: td["function"].(map[string]interface{})["description"].(string),
					Parameters:  td["function"].(map[string]interface{})["parameters"].(map[string]interface{}),
				},
			})
		}

		// Log LLM request details
		logger.DebugCF("agent", "LLM request",
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
		logger.DebugCF("agent", "Full LLM request",
			map[string]interface{}{
				"iteration":     iteration,
				"messages_json": formatMessagesForLog(messages),
				"tools_json":    formatToolsForLog(providerToolDefs),
			})

		response, err := al.provider.Chat(ctx, messages, providerToolDefs, al.model, map[string]interface{}{
			"max_tokens":  8192,
			"temperature": 0.7,
		})

		if err != nil {
			logger.ErrorCF("agent", "LLM call failed",
				map[string]interface{}{
					"iteration": iteration,
					"error":     err.Error(),
				})
			return "", fmt.Errorf("LLM call failed: %w", err)
		}

		if len(response.ToolCalls) == 0 {
			finalContent = response.Content
			logger.InfoCF("agent", "LLM response without tool calls (direct answer)",
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
		logger.InfoCF("agent", "LLM requested tool calls",
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
		// 持久化包含 ToolCalls 的助手消息
		al.sessions.AddMessageFull(msg.SessionKey, assistantMsg)

		for _, tc := range response.ToolCalls {
			// Log tool call with arguments preview
			argsJSON, _ := json.Marshal(tc.Arguments)
			argsPreview := truncate(string(argsJSON), 200)
			logger.InfoCF("agent", fmt.Sprintf("Tool call: %s(%s)", tc.Name, argsPreview),
				map[string]interface{}{
					"tool":      tc.Name,
					"iteration": iteration,
				})

			result, err := al.tools.Execute(ctx, tc.Name, tc.Arguments)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}
			updateTxnStateFromToolCall(&txn, tc.Name, tc.Arguments, result)

			toolResultMsg := providers.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			}
			messages = append(messages, toolResultMsg)
			// 持久化工具返回结果
			al.sessions.AddMessageFull(msg.SessionKey, toolResultMsg)
		}
	}

	if finalContent == "" {
		finalContent = ""
	}

	// Filter out <think>...</think> content from user-facing response
	// Keep full content in debug logs if needed, but remove from final output
	re := regexp.MustCompile(`(?s)<think>.*?</think>`)
	userContent := re.ReplaceAllString(finalContent, "")
	userContent = strings.TrimSpace(userContent)
	if userContent == "" && finalContent != "" {
		// If only thoughts were present, maybe provide a generic "Done" or keep something?
		// For now, let's assume thoughts are auxiliary and empty response is okay if tools did work.
		// If no tools ran and only thoughts, user might be confused.
		if iteration == 1 {
			fallback := strings.TrimSpace(al.thinkOnlyFallback)
			if fallback == "" {
				fallback = "已完成思考流程。"
			}
			userContent = fallback
		}
	}

	if memoryRecallUsed && !strings.Contains(strings.ToLower(userContent), "source:") {
		if src := extractFirstSourceLine(memoryRecallText); src != "" {
			userContent = strings.TrimSpace(userContent + "\n\n" + src)
		}
	}
	userContent = enforceExecutionReceiptTemplate(effectiveUserContent, userContent, txn)

	al.sessions.AddMessage(msg.SessionKey, "user", msg.Content)

	// 使用 AddMessageFull 存储包含思考过程或工具调用的完整助手消息
	al.sessions.AddMessageFull(msg.SessionKey, providers.Message{
		Role:    "assistant",
		Content: userContent,
	})
	al.sessions.SetLastLanguage(msg.SessionKey, responseLang)
	al.compactSessionIfNeeded(msg.SessionKey)

	al.sessions.Save(al.sessions.GetOrCreate(msg.SessionKey))

	// Log response preview (original content)
	responsePreview := truncate(finalContent, 120)
	logger.InfoCF("agent", fmt.Sprintf("Response to %s:%s: %s", msg.Channel, msg.SenderID, responsePreview),
		map[string]interface{}{
			"iterations":   iteration,
			"final_length": len(finalContent),
			"user_length":  len(userContent),
		})

	return userContent, nil
}

func (al *AgentLoop) updateIntentHint(sessionKey, content string) {
	content = strings.TrimSpace(content)
	if sessionKey == "" || content == "" {
		return
	}
	lower := strings.ToLower(content)

	// Cron natural-language intent: avoid searching project files for user timer ops.
	if strings.Contains(lower, "定时") || strings.Contains(lower, "定时任务") || strings.Contains(lower, "cron") || strings.Contains(lower, "schedule") {
		hint := "优先使用 cron 工具处理定时任务：查看=action=list；删除=action=delete(id)；启停=action=enable/disable。不要改为在项目目录中搜索 cron 文本。"
		al.intentMu.Lock()
		al.intentHints[sessionKey] = hint + " 用户补充=" + content
		al.intentMu.Unlock()
		return
	}

	if !strings.Contains(lower, "提交") && !strings.Contains(lower, "推送") && !strings.Contains(lower, "commit") && !strings.Contains(lower, "push") {
		if strings.HasPrefix(content, "1.") || strings.HasPrefix(content, "2.") {
			al.intentMu.Lock()
			if prev := strings.TrimSpace(al.intentHints[sessionKey]); prev != "" {
				al.intentHints[sessionKey] = prev + " | " + content
			}
			al.intentMu.Unlock()
		}
		return
	}
	hint := "执行事务: commit+push 一次闭环，包含分支/范围确认。"
	if strings.Contains(lower, "所有分支") || strings.Contains(lower, "all branches") {
		hint += " 范围=所有分支。"
	}
	al.intentMu.Lock()
	al.intentHints[sessionKey] = hint + " 用户补充=" + content
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
	if strings.Contains(lower, "提交") || strings.Contains(lower, "推送") || strings.HasPrefix(content, "1.") || strings.HasPrefix(content, "2.") || strings.Contains(lower, "定时") || strings.Contains(lower, "cron") || strings.Contains(lower, "schedule") {
		return "[Intent Slot]\n" + hint + "\n\n[User Message]\n" + content
	}
	return content
}

func enforceExecutionReceiptTemplate(userInput, output string, txn executionTxnState) string {
	l := strings.ToLower(userInput)
	if !(strings.Contains(l, "提交") || strings.Contains(l, "推送") || strings.Contains(l, "commit") || strings.Contains(l, "push")) {
		return output
	}
	clean := strings.TrimSpace(output)
	if clean == "" {
		return clean
	}
	if strings.Contains(clean, "已理解") && strings.Contains(clean, "已完成") {
		return clean
	}
	status := "事务状态：未检测到完整 commit+push。"
	if txn.commitSeen && txn.pushSeen && txn.pushOK {
		status = "事务状态：已检测到 commit+push 成功。"
	}
	return "已理解：按你的要求执行提交/推送事务。\n正在执行：整理改动并完成 commit + push。\n" + status + "\n已完成：\n" + clean
}

func isCommitPushIntent(content string) bool {
	l := strings.ToLower(strings.TrimSpace(content))
	return strings.Contains(l, "提交") || strings.Contains(l, "推送") || strings.Contains(l, "commit") || strings.Contains(l, "push")
}

func updateTxnStateFromToolCall(txn *executionTxnState, toolName string, args map[string]interface{}, result string) {
	if txn == nil || strings.ToLower(strings.TrimSpace(toolName)) != "exec" {
		return
	}
	cmd, _ := args["command"].(string)
	lcmd := strings.ToLower(cmd)
	lres := strings.ToLower(strings.TrimSpace(result))
	if strings.Contains(lcmd, "git commit") {
		txn.commitSeen = true
	}
	if strings.Contains(lcmd, "git push") {
		txn.pushSeen = true
		if !strings.HasPrefix(lres, "error:") && (strings.Contains(lres, "->") || strings.Contains(lres, "everything up-to-date") || strings.Contains(lres, "done")) {
			txn.pushOK = true
		}
	}
}

func (al *AgentLoop) processSystemMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	// Verify this is a system message
	if msg.Channel != "system" {
		return "", fmt.Errorf("processSystemMessage called with non-system message channel: %s", msg.Channel)
	}

	logger.InfoCF("agent", "Processing system message",
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
		providerToolDefs := make([]providers.ToolDefinition, 0, len(toolDefs))
		for _, td := range toolDefs {
			providerToolDefs = append(providerToolDefs, providers.ToolDefinition{
				Type: td["type"].(string),
				Function: providers.ToolFunctionDefinition{
					Name:        td["function"].(map[string]interface{})["name"].(string),
					Description: td["function"].(map[string]interface{})["description"].(string),
					Parameters:  td["function"].(map[string]interface{})["parameters"].(map[string]interface{}),
				},
			})
		}

		// Log LLM request details
		logger.DebugCF("agent", "LLM request",
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
		logger.DebugCF("agent", "Full LLM request",
			map[string]interface{}{
				"iteration":     iteration,
				"messages_json": formatMessagesForLog(messages),
				"tools_json":    formatToolsForLog(providerToolDefs),
			})

		response, err := al.provider.Chat(ctx, messages, providerToolDefs, al.model, map[string]interface{}{
			"max_tokens":  8192,
			"temperature": 0.7,
		})

		if err != nil {
			errMsg := strings.ToLower(err.Error())
			if strings.Contains(errMsg, "no tool call found for function call output") {
				logger.WarnCF("agent", "System message hit orphan tool-call chain, retry with fresh context", map[string]interface{}{"iteration": iteration, "session": sessionKey})
				messages = al.contextBuilder.BuildMessages(
					nil,
					"",
					msg.Content,
					nil,
					originChannel,
					originChatID,
					responseLang,
				)
				continue
			}
			logger.ErrorCF("agent", "LLM call failed in system message",
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
		// 持久化包含 ToolCalls 的助手消息
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
			// 持久化工具返回结果
			al.sessions.AddMessageFull(sessionKey, toolResultMsg)
		}
	}

	if finalContent == "" {
		finalContent = "Background task completed."
	}

	// Save to session with system message marker
	al.sessions.AddMessage(sessionKey, "user", fmt.Sprintf("[System: %s] %s", msg.SenderID, msg.Content))

	// 如果 finalContent 中没有包含 tool calls (即最后一次 LLM 返回的结果)
	// 我们已经通过循环内部的 AddMessageFull 存储了前面的步骤
	// 这里的 AddMessageFull 会存储最终回复
	al.sessions.AddMessageFull(sessionKey, providers.Message{
		Role:    "assistant",
		Content: finalContent,
	})
	al.sessions.SetLastLanguage(sessionKey, responseLang)
	al.compactSessionIfNeeded(sessionKey)

	al.sessions.Save(al.sessions.GetOrCreate(sessionKey))

	logger.InfoCF("agent", "System message processing completed",
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
		keywords = []string{"remember", "记得", "上次", "之前", "偏好", "preference", "todo", "待办", "决定", "decision", "日期", "when did", "what did we"}
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
