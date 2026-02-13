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
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
	"clawgo/pkg/configops"
	"clawgo/pkg/cron"
	"clawgo/pkg/logger"
	"clawgo/pkg/providers"
	"clawgo/pkg/session"
	"clawgo/pkg/tools"
)

var errGatewayNotRunningSlash = errors.New("gateway not running")

const llmCallTimeout = 90 * time.Second
const perSessionQueueSize = 64

type sessionWorker struct {
	queue    chan bus.InboundMessage
	cancelMu sync.Mutex
	cancel   context.CancelFunc
}

type AgentLoop struct {
	bus            *bus.MessageBus
	provider       providers.LLMProvider
	workspace      string
	model          string
	modelFallbacks []string
	maxIterations  int
	sessions       *session.SessionManager
	contextBuilder *ContextBuilder
	tools          *tools.ToolRegistry
	orchestrator   *tools.Orchestrator
	running        atomic.Bool
	compactionCfg  config.ContextCompactionConfig
	workersMu      sync.Mutex
	workers        map[string]*sessionWorker
}

func NewAgentLoop(cfg *config.Config, msgBus *bus.MessageBus, provider providers.LLMProvider, cs *cron.CronService) *AgentLoop {
	workspace := cfg.WorkspacePath()
	os.MkdirAll(workspace, 0755)
	logger.InfoCF("agent", "Agent workspace initialized", map[string]interface{}{
		"workspace": workspace,
	})

	toolsRegistry := tools.NewToolRegistry()
	toolsRegistry.Register(&tools.ReadFileTool{})
	toolsRegistry.Register(&tools.WriteFileTool{})
	toolsRegistry.Register(&tools.ListDirTool{})
	toolsRegistry.Register(tools.NewExecTool(cfg.Tools.Shell, workspace))

	if cs != nil {
		toolsRegistry.Register(tools.NewRemindTool(cs))
	}

	braveAPIKey := cfg.Tools.Web.Search.APIKey
	toolsRegistry.Register(tools.NewWebSearchTool(braveAPIKey, cfg.Tools.Web.Search.MaxResults))
	webFetchTool := tools.NewWebFetchTool(50000)
	toolsRegistry.Register(webFetchTool)
	toolsRegistry.Register(tools.NewParallelFetchTool(webFetchTool))

	// Register message tool
	messageTool := tools.NewMessageTool()
	messageTool.SetSendCallback(func(channel, chatID, content string) error {
		msgBus.PublishOutbound(bus.OutboundMessage{
			Channel: channel,
			ChatID:  chatID,
			Content: content,
		})
		return nil
	})
	toolsRegistry.Register(messageTool)

	// Register spawn tool
	orchestrator := tools.NewOrchestrator()
	subagentManager := tools.NewSubagentManager(provider, workspace, msgBus, orchestrator)
	spawnTool := tools.NewSpawnTool(subagentManager)
	toolsRegistry.Register(spawnTool)
	toolsRegistry.Register(tools.NewPipelineCreateTool(orchestrator))
	toolsRegistry.Register(tools.NewPipelineStatusTool(orchestrator))
	toolsRegistry.Register(tools.NewPipelineStateSetTool(orchestrator))
	toolsRegistry.Register(tools.NewPipelineDispatchTool(orchestrator, subagentManager))

	// Register edit file tool
	editFileTool := tools.NewEditFileTool(workspace)
	toolsRegistry.Register(editFileTool)

	// Register memory search tool
	memorySearchTool := tools.NewMemorySearchTool(workspace)
	toolsRegistry.Register(memorySearchTool)
	toolsRegistry.Register(tools.NewRepoMapTool(workspace))
	toolsRegistry.Register(tools.NewSkillExecTool(workspace))

	// Register parallel execution tool (leveraging Go's concurrency)
	toolsRegistry.Register(tools.NewParallelTool(toolsRegistry))

	// Register browser tool (integrated Chromium support)
	toolsRegistry.Register(tools.NewBrowserTool())

	// Register camera tool
	toolsRegistry.Register(tools.NewCameraTool(workspace))
	// Register system info tool
	toolsRegistry.Register(tools.NewSystemInfoTool())

	sessionsManager := session.NewSessionManager(filepath.Join(filepath.Dir(cfg.WorkspacePath()), "sessions"))

	loop := &AgentLoop{
		bus:            msgBus,
		provider:       provider,
		workspace:      workspace,
		model:          cfg.Agents.Defaults.Model,
		modelFallbacks: cfg.Agents.Defaults.ModelFallbacks,
		maxIterations:  cfg.Agents.Defaults.MaxToolIterations,
		sessions:       sessionsManager,
		contextBuilder: NewContextBuilder(workspace, cfg.Memory, func() []string { return toolsRegistry.GetSummaries() }),
		tools:          toolsRegistry,
		orchestrator:   orchestrator,
		compactionCfg:  cfg.Agents.Defaults.ContextCompaction,
		workers:        make(map[string]*sessionWorker),
	}

	// 注入递归运行逻辑，使 subagent 具备 full tool-calling 能力
	subagentManager.SetRunFunc(func(ctx context.Context, task, channel, chatID string) (string, error) {
		sessionKey := fmt.Sprintf("subagent:%d", os.Getpid()) // 改用 PID 或随机数，避免 sessionKey 冲突
		return loop.ProcessDirect(ctx, task, sessionKey)
	})

	return loop
}

func (al *AgentLoop) Run(ctx context.Context) error {
	al.running.Store(true)

	for al.running.Load() {
		select {
		case <-ctx.Done():
			al.stopAllWorkers()
			return nil
		default:
			msg, ok := al.bus.ConsumeInbound(ctx)
			if !ok {
				al.stopAllWorkers()
				return nil
			}

			if isStopCommand(msg.Content) {
				al.handleStopCommand(msg)
				continue
			}

			al.enqueueMessage(ctx, msg)
		}
	}

	return nil
}

func (al *AgentLoop) Stop() {
	al.running.Store(false)
	al.stopAllWorkers()
}

func isStopCommand(content string) bool {
	return strings.EqualFold(strings.TrimSpace(content), "/stop")
}

func (al *AgentLoop) handleStopCommand(msg bus.InboundMessage) {
	worker := al.getWorker(msg.SessionKey)
	if worker == nil {
		return
	}

	worker.cancelMu.Lock()
	cancel := worker.cancel
	worker.cancelMu.Unlock()

	if cancel == nil {
		return
	}

	cancel()
}

func (al *AgentLoop) enqueueMessage(ctx context.Context, msg bus.InboundMessage) {
	worker := al.getOrCreateWorker(ctx, msg.SessionKey)
	select {
	case worker.queue <- msg:
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		al.bus.PublishOutbound(bus.OutboundMessage{
			Channel: msg.Channel,
			ChatID:  msg.ChatID,
			Content: "Message queue is busy. Please try again shortly.",
		})
	}
}

func (al *AgentLoop) getWorker(sessionKey string) *sessionWorker {
	al.workersMu.Lock()
	defer al.workersMu.Unlock()
	return al.workers[sessionKey]
}

func (al *AgentLoop) getOrCreateWorker(ctx context.Context, sessionKey string) *sessionWorker {
	al.workersMu.Lock()
	defer al.workersMu.Unlock()

	if w, ok := al.workers[sessionKey]; ok {
		return w
	}

	w := &sessionWorker{
		queue: make(chan bus.InboundMessage, perSessionQueueSize),
	}
	al.workers[sessionKey] = w

	go al.runSessionWorker(ctx, sessionKey, w)
	return w
}

func (al *AgentLoop) runSessionWorker(ctx context.Context, sessionKey string, worker *sessionWorker) {
	for {
		select {
		case <-ctx.Done():
			al.clearWorkerCancel(worker)
			al.removeWorker(sessionKey, worker)
			return
		case msg := <-worker.queue:
			taskCtx, cancel := context.WithCancel(ctx)
			worker.cancelMu.Lock()
			worker.cancel = cancel
			worker.cancelMu.Unlock()

			response, err := al.processMessage(taskCtx, msg)
			cancel()
			al.clearWorkerCancel(worker)

			if err != nil {
				if errors.Is(err, context.Canceled) {
					continue
				}
				response = fmt.Sprintf("Error processing message: %v", err)
			}

			if response != "" {
				al.bus.PublishOutbound(bus.OutboundMessage{
					Channel: msg.Channel,
					ChatID:  msg.ChatID,
					Content: response,
				})
			}
		}
	}
}

func (al *AgentLoop) clearWorkerCancel(worker *sessionWorker) {
	worker.cancelMu.Lock()
	worker.cancel = nil
	worker.cancelMu.Unlock()
}

func (al *AgentLoop) removeWorker(sessionKey string, worker *sessionWorker) {
	al.workersMu.Lock()
	defer al.workersMu.Unlock()
	if cur, ok := al.workers[sessionKey]; ok && cur == worker {
		delete(al.workers, sessionKey)
	}
}

func (al *AgentLoop) stopAllWorkers() {
	al.workersMu.Lock()
	workers := make([]*sessionWorker, 0, len(al.workers))
	for _, w := range al.workers {
		workers = append(workers, w)
	}
	al.workersMu.Unlock()

	for _, w := range workers {
		w.cancelMu.Lock()
		cancel := w.cancel
		w.cancelMu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
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

func (al *AgentLoop) processMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	// Add message preview to log
	preview := truncate(msg.Content, 80)
	logger.InfoCF("agent", fmt.Sprintf("Processing message from %s:%s: %s", msg.Channel, msg.SenderID, preview),
		map[string]interface{}{
			logger.FieldChannel:  msg.Channel,
			logger.FieldChatID:   msg.ChatID,
			logger.FieldSenderID: msg.SenderID,
			"session_key":        msg.SessionKey,
		})

	// Route system messages to processSystemMessage
	if msg.Channel == "system" {
		return al.processSystemMessage(ctx, msg)
	}

	// Built-in slash commands (deterministic, no LLM roundtrip)
	if handled, result, err := al.handleSlashCommand(msg.Content); handled {
		return result, err
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

	messages := al.contextBuilder.BuildMessages(
		history,
		summary,
		msg.Content,
		nil,
		msg.Channel,
		msg.ChatID,
	)

	finalContent, iteration, err := al.runLLMToolLoop(ctx, messages, msg.SessionKey, false)
	if err != nil {
		return "", err
	}

	if finalContent == "" {
		finalContent = "Done."
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
			userContent = "Thinking process completed."
		}
	}

	al.sessions.AddMessage(msg.SessionKey, "user", msg.Content)

	// 使用 AddMessageFull 存储包含思考过程或工具调用的完整助手消息
	al.sessions.AddMessageFull(msg.SessionKey, providers.Message{
		Role:    "assistant",
		Content: userContent,
	})

	if err := al.persistSessionWithCompaction(ctx, msg.SessionKey); err != nil {
		logger.WarnCF("agent", "Failed to save session metadata", map[string]interface{}{
			"session_key":     msg.SessionKey,
			logger.FieldError: err.Error(),
		})
	}

	// Log response preview (original content)
	responsePreview := truncate(finalContent, 120)
	logger.InfoCF("agent", fmt.Sprintf("Response to %s:%s: %s", msg.Channel, msg.SenderID, responsePreview),
		map[string]interface{}{
			"iterations":                          iteration,
			logger.FieldAssistantContentLength:    len(finalContent),
			logger.FieldUserResponseContentLength: len(userContent),
		})

	return userContent, nil
}

func (al *AgentLoop) processSystemMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	// Verify this is a system message
	if msg.Channel != "system" {
		return "", fmt.Errorf("processSystemMessage called with non-system message channel: %s", msg.Channel)
	}

	logger.InfoCF("agent", "Processing system message",
		map[string]interface{}{
			logger.FieldSenderID: msg.SenderID,
			logger.FieldChatID:   msg.ChatID,
		})

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
	messages := al.contextBuilder.BuildMessages(
		history,
		summary,
		msg.Content,
		nil,
		originChannel,
		originChatID,
	)

	finalContent, iteration, err := al.runLLMToolLoop(ctx, messages, sessionKey, true)
	if err != nil {
		return "", err
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

	if err := al.persistSessionWithCompaction(ctx, sessionKey); err != nil {
		logger.WarnCF("agent", "Failed to save session metadata", map[string]interface{}{
			"session_key":     sessionKey,
			logger.FieldError: err.Error(),
		})
	}

	logger.InfoCF("agent", "System message processing completed",
		map[string]interface{}{
			"iterations":                       iteration,
			logger.FieldAssistantContentLength: len(finalContent),
		})

	return finalContent, nil
}

func (al *AgentLoop) runLLMToolLoop(
	ctx context.Context,
	messages []providers.Message,
	sessionKey string,
	systemMode bool,
) (string, int, error) {
	messages = sanitizeMessagesForToolCalling(messages)

	iteration := 0
	var finalContent string
	var lastToolResult string

	for iteration < al.maxIterations {
		iteration++

		if !systemMode {
			logger.DebugCF("agent", "LLM iteration",
				map[string]interface{}{
					"iteration": iteration,
					"max":       al.maxIterations,
				})
		}

		providerToolDefs, err := buildProviderToolDefs(al.tools.GetDefinitions())
		if err != nil {
			return "", iteration, fmt.Errorf("invalid tool definition: %w", err)
		}

		messages = sanitizeMessagesForToolCalling(messages)

		systemPromptLen := 0
		if len(messages) > 0 {
			systemPromptLen = len(messages[0].Content)
		}
		logger.DebugCF("agent", "LLM request",
			map[string]interface{}{
				"iteration":         iteration,
				"model":             al.model,
				"messages_count":    len(messages),
				"tools_count":       len(providerToolDefs),
				"max_tokens":        8192,
				"temperature":       0.7,
				"system_prompt_len": systemPromptLen,
			})
		logger.DebugCF("agent", "Full LLM request",
			map[string]interface{}{
				"iteration":     iteration,
				"messages_json": formatMessagesForLog(messages),
				"tools_json":    formatToolsForLog(providerToolDefs),
			})

		llmStart := time.Now()
		llmCtx, cancelLLM := context.WithTimeout(ctx, llmCallTimeout)
		response, err := al.callLLMWithModelFallback(llmCtx, messages, providerToolDefs, map[string]interface{}{
			"max_tokens":  8192,
			"temperature": 0.7,
		})
		cancelLLM()
		llmElapsed := time.Since(llmStart)

		if err != nil {
			errLog := "LLM call failed"
			if systemMode {
				errLog = "LLM call failed in system message"
			}
			logger.ErrorCF("agent", errLog,
				map[string]interface{}{
					"iteration":       iteration,
					logger.FieldError: err.Error(),
					"elapsed":         llmElapsed.String(),
				})
			return "", iteration, fmt.Errorf("LLM call failed: %w", err)
		}

		doneLog := "LLM call completed"
		if systemMode {
			doneLog = "LLM call completed (system message)"
		}
		logger.InfoCF("agent", doneLog,
			map[string]interface{}{
				"iteration": iteration,
				"elapsed":   llmElapsed.String(),
				"model":     al.model,
			})

		if len(response.ToolCalls) == 0 {
			finalContent = response.Content
			if !systemMode {
				logger.InfoCF("agent", "LLM response without tool calls (direct answer)",
					map[string]interface{}{
						"iteration":                        iteration,
						logger.FieldAssistantContentLength: len(finalContent),
					})
			}
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
		al.sessions.AddMessageFull(sessionKey, assistantMsg)

		for _, tc := range response.ToolCalls {
			if !systemMode {
				argsJSON, _ := json.Marshal(tc.Arguments)
				argsPreview := truncate(string(argsJSON), 200)
				logger.InfoCF("agent", fmt.Sprintf("Tool call: %s(%s)", tc.Name, argsPreview),
					map[string]interface{}{
						"tool":      tc.Name,
						"iteration": iteration,
					})
			}

			result, err := al.tools.Execute(ctx, tc.Name, tc.Arguments)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}
			lastToolResult = result

			toolResultMsg := providers.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			}
			messages = append(messages, toolResultMsg)
			al.sessions.AddMessageFull(sessionKey, toolResultMsg)
		}
	}

	// When max iterations are reached without a direct answer, ask once more without tools.
	// This avoids returning placeholder text to end users.
	if finalContent == "" && len(messages) > 0 {
		if !systemMode && iteration >= al.maxIterations {
			logger.WarnCF("agent", "Max tool iterations reached without final answer; forcing finalization pass", map[string]interface{}{
				"iteration":   iteration,
				"max":         al.maxIterations,
				"session_key": sessionKey,
			})
		}

		finalizeMessages := append([]providers.Message{}, messages...)
		finalizeMessages = append(finalizeMessages, providers.Message{
			Role:    "user",
			Content: "Now provide your final response to the user based on the completed tool results. Do not call any tools.",
		})
		finalizeMessages = sanitizeMessagesForToolCalling(finalizeMessages)

		llmCtx, cancelLLM := context.WithTimeout(ctx, llmCallTimeout)
		finalResp, err := al.callLLMWithModelFallback(llmCtx, finalizeMessages, nil, map[string]interface{}{
			"max_tokens":  1024,
			"temperature": 0.3,
		})
		cancelLLM()
		if err != nil {
			logger.WarnCF("agent", "Finalization pass failed", map[string]interface{}{
				"iteration":       iteration,
				"session_key":     sessionKey,
				logger.FieldError: err.Error(),
			})
		} else if strings.TrimSpace(finalResp.Content) != "" {
			finalContent = finalResp.Content
		}
	}

	if finalContent == "" {
		finalContent = strings.TrimSpace(lastToolResult)
	}

	return finalContent, iteration, nil
}

// sanitizeMessagesForToolCalling removes orphan tool-calling turns so provider-side
// validation won't fail when history was truncated in the middle of a tool chain.
func sanitizeMessagesForToolCalling(messages []providers.Message) []providers.Message {
	if len(messages) == 0 {
		return messages
	}

	out := make([]providers.Message, 0, len(messages))
	pendingToolIDs := map[string]struct{}{}
	lastToolCallIdx := -1

	resetPending := func() {
		pendingToolIDs = map[string]struct{}{}
		lastToolCallIdx = -1
	}

	rollbackToolCall := func() {
		if lastToolCallIdx >= 0 && lastToolCallIdx <= len(out) {
			// Drop the entire partial tool-call segment: assistant(tool_calls)
			// and any collected tool results that followed it.
			out = out[:lastToolCallIdx]
		}
		resetPending()
	}

	for _, msg := range messages {
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			continue
		}

		switch role {
		case "system":
			if len(out) == 0 {
				out = append(out, msg)
			}
		case "tool":
			if len(pendingToolIDs) == 0 || strings.TrimSpace(msg.ToolCallID) == "" {
				continue
			}
			if _, ok := pendingToolIDs[msg.ToolCallID]; !ok {
				continue
			}
			out = append(out, msg)
			delete(pendingToolIDs, msg.ToolCallID)
			if len(pendingToolIDs) == 0 {
				lastToolCallIdx = -1
			}
		case "assistant":
			if len(pendingToolIDs) > 0 {
				rollbackToolCall()
			}

			if len(msg.ToolCalls) == 0 {
				out = append(out, msg)
				continue
			}

			prevRole := ""
			for i := len(out) - 1; i >= 0; i-- {
				r := strings.TrimSpace(out[i].Role)
				if r != "" {
					prevRole = r
					break
				}
			}
			if prevRole != "user" && prevRole != "tool" {
				continue
			}

			out = append(out, msg)
			lastToolCallIdx = len(out) - 1
			pendingToolIDs = map[string]struct{}{}
			for _, tc := range msg.ToolCalls {
				id := strings.TrimSpace(tc.ID)
				if id != "" {
					pendingToolIDs[id] = struct{}{}
				}
			}
			if len(pendingToolIDs) == 0 {
				lastToolCallIdx = -1
			}
		default:
			if len(pendingToolIDs) > 0 {
				rollbackToolCall()
			}
			out = append(out, msg)
		}
	}

	if len(pendingToolIDs) > 0 {
		rollbackToolCall()
	}

	return out
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

func (al *AgentLoop) callLLMWithModelFallback(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	options map[string]interface{},
) (*providers.LLMResponse, error) {
	candidates := al.modelCandidates()
	var lastErr error

	for idx, model := range candidates {
		response, err := al.provider.Chat(ctx, messages, tools, model, options)
		if err == nil {
			if al.model != model {
				logger.WarnCF("agent", "Model switched after quota/rate-limit error", map[string]interface{}{
					"from_model": al.model,
					"to_model":   model,
				})
				al.model = model
			}
			return response, nil
		}

		lastErr = err
		if !isQuotaOrRateLimitError(err) {
			return nil, err
		}

		if idx < len(candidates)-1 {
			logger.WarnCF("agent", "Model quota/rate-limit reached, trying fallback model", map[string]interface{}{
				"failed_model":    model,
				"next_model":      candidates[idx+1],
				logger.FieldError: err.Error(),
			})
			continue
		}
	}

	return nil, fmt.Errorf("all configured models failed; last error: %w", lastErr)
}

func (al *AgentLoop) modelCandidates() []string {
	candidates := []string{}
	seen := map[string]bool{}

	add := func(model string) {
		m := strings.TrimSpace(model)
		if m == "" || seen[m] {
			return
		}
		seen[m] = true
		candidates = append(candidates, m)
	}

	add(al.model)
	for _, m := range al.modelFallbacks {
		add(m)
	}

	return candidates
}

func isQuotaOrRateLimitError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	keywords := []string{
		"status 429",
		"429",
		"insufficient_quota",
		"quota",
		"rate limit",
		"rate_limit",
		"too many requests",
		"billing",
	}

	for _, keyword := range keywords {
		if strings.Contains(msg, keyword) {
			return true
		}
	}
	return false
}

func buildProviderToolDefs(toolDefs []map[string]interface{}) ([]providers.ToolDefinition, error) {
	providerToolDefs := make([]providers.ToolDefinition, 0, len(toolDefs))
	for i, td := range toolDefs {
		toolType, ok := td["type"].(string)
		if !ok || strings.TrimSpace(toolType) == "" {
			return nil, fmt.Errorf("tool[%d] missing/invalid type", i)
		}

		fnRaw, ok := td["function"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("tool[%d] missing/invalid function object", i)
		}

		name, ok := fnRaw["name"].(string)
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("tool[%d] missing/invalid function.name", i)
		}

		description, ok := fnRaw["description"].(string)
		if !ok {
			return nil, fmt.Errorf("tool[%d] missing/invalid function.description", i)
		}

		parameters, ok := fnRaw["parameters"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("tool[%d] missing/invalid function.parameters", i)
		}

		providerToolDefs = append(providerToolDefs, providers.ToolDefinition{
			Type: toolType,
			Function: providers.ToolFunctionDefinition{
				Name:        name,
				Description: description,
				Parameters:  parameters,
			},
		})
	}
	return providerToolDefs, nil
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

func (al *AgentLoop) persistSessionWithCompaction(ctx context.Context, sessionKey string) error {
	if err := al.maybeCompactContext(ctx, sessionKey); err != nil {
		logger.WarnCF("agent", "Context compaction skipped due to error", map[string]interface{}{
			"session_key":     sessionKey,
			logger.FieldError: err.Error(),
		})
	}
	return al.sessions.Save(al.sessions.GetOrCreate(sessionKey))
}

func (al *AgentLoop) maybeCompactContext(ctx context.Context, sessionKey string) error {
	cfg := al.compactionCfg
	if !cfg.Enabled {
		return nil
	}

	messageCount := al.sessions.MessageCount(sessionKey)
	if messageCount < cfg.TriggerMessages {
		return nil
	}

	history := al.sessions.GetHistory(sessionKey)
	if len(history) < cfg.TriggerMessages {
		return nil
	}
	if cfg.KeepRecentMessages >= len(history) {
		return nil
	}

	summary := al.sessions.GetSummary(sessionKey)
	compactUntil := len(history) - cfg.KeepRecentMessages
	compactCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	newSummary, err := al.buildCompactedSummary(compactCtx, summary, history[:compactUntil], cfg.MaxTranscriptChars)
	if err != nil {
		return err
	}
	newSummary = strings.TrimSpace(newSummary)
	if newSummary == "" {
		return nil
	}
	if len(newSummary) > cfg.MaxSummaryChars {
		newSummary = truncateString(newSummary, cfg.MaxSummaryChars)
	}

	before, after, err := al.sessions.CompactHistory(sessionKey, newSummary, cfg.KeepRecentMessages)
	if err != nil {
		return err
	}

	logger.InfoCF("agent", "Context compacted automatically", map[string]interface{}{
		"session_key":      sessionKey,
		"before_messages":  before,
		"after_messages":   after,
		"kept_recent":      cfg.KeepRecentMessages,
		"summary_chars":    len(newSummary),
		"trigger_messages": cfg.TriggerMessages,
	})
	return nil
}

func (al *AgentLoop) buildCompactedSummary(
	ctx context.Context,
	existingSummary string,
	messages []providers.Message,
	maxTranscriptChars int,
) (string, error) {
	transcript := formatCompactionTranscript(messages, maxTranscriptChars)
	if strings.TrimSpace(transcript) == "" {
		return strings.TrimSpace(existingSummary), nil
	}

	systemPrompt := "You are a conversation compactor. Merge prior summary and transcript into a concise, factual memory for future turns. Keep user preferences, constraints, decisions, unresolved tasks, and key technical context. Do not include speculative content."
	userPrompt := fmt.Sprintf("Existing summary:\n%s\n\nTranscript to compact:\n%s\n\nReturn a compact markdown summary with sections: Key Facts, Decisions, Open Items, Next Steps.",
		strings.TrimSpace(existingSummary), transcript)

	resp, err := al.callLLMWithModelFallback(ctx, []providers.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}, nil, map[string]interface{}{
		"max_tokens":  1200,
		"temperature": 0.2,
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func formatCompactionTranscript(messages []providers.Message, maxChars int) string {
	if maxChars <= 0 || len(messages) == 0 {
		return ""
	}

	var sb strings.Builder
	used := 0
	for _, m := range messages {
		role := strings.TrimSpace(m.Role)
		if role == "" {
			role = "unknown"
		}
		line := fmt.Sprintf("[%s] %s\n", role, strings.TrimSpace(m.Content))
		if len(line) > 1200 {
			line = truncateString(line, 1200) + "\n"
		}
		if used+len(line) > maxChars {
			remain := maxChars - used
			if remain > 16 {
				sb.WriteString(truncateString(line, remain))
			}
			break
		}
		sb.WriteString(line)
		used += len(line)
	}
	return strings.TrimSpace(sb.String())
}

func (al *AgentLoop) handleSlashCommand(content string) (bool, string, error) {
	text := strings.TrimSpace(content)
	if !strings.HasPrefix(text, "/") {
		return false, "", nil
	}

	fields := strings.Fields(text)
	if len(fields) == 0 {
		return true, "", nil
	}

	switch fields[0] {
	case "/help":
		return true, "Slash commands:\n/help\n/status\n/config get <path>\n/config set <path> <value>\n/reload\n/pipeline list\n/pipeline status <pipeline_id>\n/pipeline ready <pipeline_id>", nil
	case "/stop":
		return true, "Stop command is handled by queue runtime. Send /stop from your channel session to interrupt current response.", nil
	case "/status":
		cfg, err := config.LoadConfig(al.getConfigPathForCommands())
		if err != nil {
			return true, "", fmt.Errorf("status failed: %w", err)
		}
		return true, fmt.Sprintf("Model: %s\nAPI Base: %s\nLogging: %v\nConfig: %s",
			cfg.Agents.Defaults.Model,
			cfg.Providers.Proxy.APIBase,
			cfg.Logging.Enabled,
			al.getConfigPathForCommands(),
		), nil
	case "/reload":
		running, err := al.triggerGatewayReloadFromAgent()
		if err != nil {
			if running {
				return true, "", err
			}
			return true, fmt.Sprintf("Hot reload not applied: %v", err), nil
		}
		return true, "Gateway hot reload signal sent", nil
	case "/config":
		if len(fields) < 2 {
			return true, "Usage: /config get <path> | /config set <path> <value>", nil
		}

		switch fields[1] {
		case "get":
			if len(fields) < 3 {
				return true, "Usage: /config get <path>", nil
			}
			cfgMap, err := al.loadConfigAsMapForAgent()
			if err != nil {
				return true, "", err
			}
			path := al.normalizeConfigPathForAgent(fields[2])
			value, ok := al.getMapValueByPathForAgent(cfgMap, path)
			if !ok {
				return true, fmt.Sprintf("Path not found: %s", path), nil
			}
			data, err := json.Marshal(value)
			if err != nil {
				return true, fmt.Sprintf("%v", value), nil
			}
			return true, string(data), nil
		case "set":
			if len(fields) < 4 {
				return true, "Usage: /config set <path> <value>", nil
			}
			cfgMap, err := al.loadConfigAsMapForAgent()
			if err != nil {
				return true, "", err
			}
			path := al.normalizeConfigPathForAgent(fields[2])
			value := al.parseConfigValueForAgent(strings.Join(fields[3:], " "))
			if err := al.setMapValueByPathForAgent(cfgMap, path, value); err != nil {
				return true, "", err
			}

			data, err := json.MarshalIndent(cfgMap, "", "  ")
			if err != nil {
				return true, "", err
			}

			configPath := al.getConfigPathForCommands()
			backupPath, err := al.writeConfigAtomicWithBackupForAgent(configPath, data)
			if err != nil {
				return true, "", err
			}

			running, err := al.triggerGatewayReloadFromAgent()
			if err != nil {
				if running {
					if rbErr := al.rollbackConfigFromBackupForAgent(configPath, backupPath); rbErr != nil {
						return true, "", fmt.Errorf("hot reload failed and rollback failed: %w", rbErr)
					}
					return true, "", fmt.Errorf("hot reload failed, config rolled back: %w", err)
				}
				return true, fmt.Sprintf("Updated %s = %v\nHot reload not applied: %v", path, value, err), nil
			}
			return true, fmt.Sprintf("Updated %s = %v\nGateway hot reload signal sent", path, value), nil
		default:
			return true, "Usage: /config get <path> | /config set <path> <value>", nil
		}
	case "/pipeline":
		if al.orchestrator == nil {
			return true, "Pipeline orchestrator not enabled.", nil
		}
		if len(fields) < 2 {
			return true, "Usage: /pipeline list | /pipeline status <pipeline_id> | /pipeline ready <pipeline_id>", nil
		}
		switch fields[1] {
		case "list":
			items := al.orchestrator.ListPipelines()
			if len(items) == 0 {
				return true, "No pipelines found.", nil
			}
			var sb strings.Builder
			sb.WriteString("Pipelines:\n")
			for _, p := range items {
				sb.WriteString(fmt.Sprintf("- %s [%s] %s\n", p.ID, p.Status, p.Label))
			}
			return true, sb.String(), nil
		case "status":
			if len(fields) < 3 {
				return true, "Usage: /pipeline status <pipeline_id>", nil
			}
			s, err := al.orchestrator.SnapshotJSON(fields[2])
			return true, s, err
		case "ready":
			if len(fields) < 3 {
				return true, "Usage: /pipeline ready <pipeline_id>", nil
			}
			ready, err := al.orchestrator.ReadyTasks(fields[2])
			if err != nil {
				return true, "", err
			}
			if len(ready) == 0 {
				return true, "No ready tasks.", nil
			}
			var sb strings.Builder
			sb.WriteString("Ready tasks:\n")
			for _, task := range ready {
				sb.WriteString(fmt.Sprintf("- %s (%s) %s\n", task.ID, task.Role, task.Goal))
			}
			return true, sb.String(), nil
		default:
			return true, "Usage: /pipeline list | /pipeline status <pipeline_id> | /pipeline ready <pipeline_id>", nil
		}
	default:
		return false, "", nil
	}
}

func (al *AgentLoop) getConfigPathForCommands() string {
	if fromEnv := strings.TrimSpace(os.Getenv("CLAWGO_CONFIG")); fromEnv != "" {
		return fromEnv
	}
	return filepath.Join(config.GetConfigDir(), "config.json")
}

func (al *AgentLoop) normalizeConfigPathForAgent(path string) string {
	return configops.NormalizeConfigPath(path)
}

func (al *AgentLoop) parseConfigValueForAgent(raw string) interface{} {
	return configops.ParseConfigValue(raw)
}

func (al *AgentLoop) loadConfigAsMapForAgent() (map[string]interface{}, error) {
	return configops.LoadConfigAsMap(al.getConfigPathForCommands())
}

func (al *AgentLoop) setMapValueByPathForAgent(root map[string]interface{}, path string, value interface{}) error {
	return configops.SetMapValueByPath(root, path, value)
}

func (al *AgentLoop) getMapValueByPathForAgent(root map[string]interface{}, path string) (interface{}, bool) {
	return configops.GetMapValueByPath(root, path)
}

func (al *AgentLoop) writeConfigAtomicWithBackupForAgent(configPath string, data []byte) (string, error) {
	return configops.WriteConfigAtomicWithBackup(configPath, data)
}

func (al *AgentLoop) rollbackConfigFromBackupForAgent(configPath, backupPath string) error {
	return configops.RollbackConfigFromBackup(configPath, backupPath)
}

func (al *AgentLoop) triggerGatewayReloadFromAgent() (bool, error) {
	return configops.TriggerGatewayReload(al.getConfigPathForCommands(), errGatewayNotRunningSlash)
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
