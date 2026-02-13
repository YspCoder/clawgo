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
	"strconv"
	"strings"
	"syscall"

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
	"clawgo/pkg/cron"
	"clawgo/pkg/logger"
	"clawgo/pkg/providers"
	"clawgo/pkg/session"
	"clawgo/pkg/tools"
)

var errGatewayNotRunningSlash = errors.New("gateway not running")

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
	running        bool
}

func NewAgentLoop(cfg *config.Config, msgBus *bus.MessageBus, provider providers.LLMProvider, cs *cron.CronService) *AgentLoop {
	workspace := cfg.WorkspacePath()
	os.MkdirAll(workspace, 0755)

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
	subagentManager := tools.NewSubagentManager(provider, workspace, msgBus)
	spawnTool := tools.NewSpawnTool(subagentManager)
	toolsRegistry.Register(spawnTool)

	// Register edit file tool
	editFileTool := tools.NewEditFileTool(workspace)
	toolsRegistry.Register(editFileTool)

	// Register memory search tool
	memorySearchTool := tools.NewMemorySearchTool(workspace)
	toolsRegistry.Register(memorySearchTool)

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
		contextBuilder: NewContextBuilder(workspace, func() []string { return toolsRegistry.GetSummaries() }),
		tools:          toolsRegistry,
		running:        false,
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

	for al.running {
		select {
		case <-ctx.Done():
			return nil
		default:
			msg, ok := al.bus.ConsumeInbound(ctx)
			if !ok {
				continue
			}

			response, err := al.processMessage(ctx, msg)
			if err != nil {
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

	return nil
}

func (al *AgentLoop) Stop() {
	al.running = false
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
			"channel":     msg.Channel,
			"chat_id":     msg.ChatID,
			"sender_id":   msg.SenderID,
			"session_key": msg.SessionKey,
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

	iteration := 0
	var finalContent string

	for iteration < al.maxIterations {
		iteration++

		logger.DebugCF("agent", "LLM iteration",
			map[string]interface{}{
				"iteration": iteration,
				"max":       al.maxIterations,
			})

		providerToolDefs, err := buildProviderToolDefs(al.tools.GetDefinitions())
		if err != nil {
			return "", fmt.Errorf("invalid tool definition: %w", err)
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

		response, err := al.callLLMWithModelFallback(ctx, messages, providerToolDefs, map[string]interface{}{
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
		finalContent = "I've completed processing but have no response to give."
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

	if err := al.sessions.Save(al.sessions.GetOrCreate(msg.SessionKey)); err != nil {
		logger.WarnCF("agent", "Failed to save session metadata", map[string]interface{}{
			"session_key": msg.SessionKey,
			"error":       err.Error(),
		})
	}

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

	iteration := 0
	var finalContent string

	for iteration < al.maxIterations {
		iteration++

		providerToolDefs, err := buildProviderToolDefs(al.tools.GetDefinitions())
		if err != nil {
			return "", fmt.Errorf("invalid tool definition: %w", err)
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

		response, err := al.callLLMWithModelFallback(ctx, messages, providerToolDefs, map[string]interface{}{
			"max_tokens":  8192,
			"temperature": 0.7,
		})

		if err != nil {
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

	if err := al.sessions.Save(al.sessions.GetOrCreate(sessionKey)); err != nil {
		logger.WarnCF("agent", "Failed to save session metadata", map[string]interface{}{
			"session_key": sessionKey,
			"error":       err.Error(),
		})
	}

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
				"failed_model": model,
				"next_model":   candidates[idx+1],
				"error":        err.Error(),
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
		return true, "Slash commands:\n/help\n/status\n/config get <path>\n/config set <path> <value>\n/reload", nil
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
	p := strings.TrimSpace(path)
	p = strings.Trim(p, ".")
	parts := strings.Split(p, ".")
	for i, part := range parts {
		if part == "enable" {
			parts[i] = "enabled"
		}
	}
	return strings.Join(parts, ".")
}

func (al *AgentLoop) parseConfigValueForAgent(raw string) interface{} {
	v := strings.TrimSpace(raw)
	lv := strings.ToLower(v)
	if lv == "true" {
		return true
	}
	if lv == "false" {
		return false
	}
	if lv == "null" {
		return nil
	}
	if i, err := strconv.ParseInt(v, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil && strings.Contains(v, ".") {
		return f
	}
	if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
		return v[1 : len(v)-1]
	}
	return v
}

func (al *AgentLoop) loadConfigAsMapForAgent() (map[string]interface{}, error) {
	configPath := al.getConfigPathForCommands()
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			defaultCfg := config.DefaultConfig()
			defData, mErr := json.Marshal(defaultCfg)
			if mErr != nil {
				return nil, mErr
			}
			var cfgMap map[string]interface{}
			if uErr := json.Unmarshal(defData, &cfgMap); uErr != nil {
				return nil, uErr
			}
			return cfgMap, nil
		}
		return nil, err
	}
	var cfgMap map[string]interface{}
	if err := json.Unmarshal(data, &cfgMap); err != nil {
		return nil, err
	}
	return cfgMap, nil
}

func (al *AgentLoop) setMapValueByPathForAgent(root map[string]interface{}, path string, value interface{}) error {
	if path == "" {
		return fmt.Errorf("path is empty")
	}
	parts := strings.Split(path, ".")
	cur := root
	for i := 0; i < len(parts)-1; i++ {
		key := parts[i]
		if key == "" {
			return fmt.Errorf("invalid path: %s", path)
		}
		next, ok := cur[key]
		if !ok {
			child := map[string]interface{}{}
			cur[key] = child
			cur = child
			continue
		}
		child, ok := next.(map[string]interface{})
		if !ok {
			return fmt.Errorf("path segment is not object: %s", key)
		}
		cur = child
	}
	last := parts[len(parts)-1]
	if last == "" {
		return fmt.Errorf("invalid path: %s", path)
	}
	cur[last] = value
	return nil
}

func (al *AgentLoop) getMapValueByPathForAgent(root map[string]interface{}, path string) (interface{}, bool) {
	if path == "" {
		return nil, false
	}
	parts := strings.Split(path, ".")
	var cur interface{} = root
	for _, key := range parts {
		obj, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		next, ok := obj[key]
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

func (al *AgentLoop) writeConfigAtomicWithBackupForAgent(configPath string, data []byte) (string, error) {
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return "", err
	}

	backupPath := configPath + ".bak"
	if oldData, err := os.ReadFile(configPath); err == nil {
		if err := os.WriteFile(backupPath, oldData, 0644); err != nil {
			return "", fmt.Errorf("write backup failed: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read existing config failed: %w", err)
	}

	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return "", fmt.Errorf("write temp config failed: %w", err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("atomic replace config failed: %w", err)
	}
	return backupPath, nil
}

func (al *AgentLoop) rollbackConfigFromBackupForAgent(configPath, backupPath string) error {
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("read backup failed: %w", err)
	}
	tmpPath := configPath + ".rollback.tmp"
	if err := os.WriteFile(tmpPath, backupData, 0644); err != nil {
		return fmt.Errorf("write rollback temp failed: %w", err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rollback replace failed: %w", err)
	}
	return nil
}

func (al *AgentLoop) triggerGatewayReloadFromAgent() (bool, error) {
	pidPath := filepath.Join(filepath.Dir(al.getConfigPathForCommands()), "gateway.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return false, fmt.Errorf("%w (pid file not found: %s)", errGatewayNotRunningSlash, pidPath)
	}
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return true, fmt.Errorf("invalid gateway pid: %q", pidStr)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return true, fmt.Errorf("find process failed: %w", err)
	}
	if err := proc.Signal(syscall.SIGHUP); err != nil {
		return true, fmt.Errorf("send SIGHUP failed: %w", err)
	}
	return true, nil
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
