// ClawGo - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 ClawGo contributors

package providers

import (
	"clawgo/pkg/config"
	"clawgo/pkg/logger"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
	"github.com/openai/openai-go/v3/shared/constant"
)

type HTTPProvider struct {
	apiKey     string
	apiBase    string
	authMode   string
	timeout    time.Duration
	httpClient *http.Client
	client     openai.Client
}

func NewHTTPProvider(apiKey, apiBase, authMode string, timeout time.Duration) *HTTPProvider {
	normalizedBase := normalizeAPIBase(apiBase)
	httpClient := &http.Client{Timeout: timeout}
	clientOpts := []option.RequestOption{
		option.WithBaseURL(normalizedBase),
		option.WithHTTPClient(httpClient),
	}

	if apiKey != "" {
		if authMode == "oauth" {
			clientOpts = append(clientOpts, option.WithHeader("Authorization", "Bearer "+apiKey))
		} else if strings.Contains(normalizedBase, "googleapis.com") {
			// Gemini direct API uses x-goog-api-key header.
			clientOpts = append(clientOpts, option.WithHeader("x-goog-api-key", apiKey))
		} else {
			clientOpts = append(clientOpts, option.WithAPIKey(apiKey))
		}
	}

	return &HTTPProvider{
		apiKey:     apiKey,
		apiBase:    normalizedBase,
		authMode:   authMode,
		timeout:    timeout,
		httpClient: httpClient,
		client:     openai.NewClient(clientOpts...),
	}
}

func (p *HTTPProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	if p.apiBase == "" {
		return nil, fmt.Errorf("API base not configured")
	}

	logger.DebugCF("provider", "OpenAI SDK chat request", map[string]interface{}{
		"api_base":       p.apiBase,
		"model":          model,
		"messages_count": len(messages),
		"tools_count":    len(tools),
		"timeout":        p.timeout.String(),
	})

	params, err := buildChatParams(messages, tools, model, options)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("API error: %w", err)
	}
	return mapChatCompletionResponse(resp), nil
}

func buildChatParams(messages []Message, tools []ToolDefinition, model string, opts map[string]interface{}) (openai.ChatCompletionNewParams, error) {
	params := openai.ChatCompletionNewParams{
		Model:    model,
		Messages: make([]openai.ChatCompletionMessageParamUnion, 0, len(messages)),
	}

	for i := range messages {
		paramMsg, err := toOpenAIMessage(messages[i])
		if err != nil {
			return openai.ChatCompletionNewParams{}, err
		}
		params.Messages = append(params.Messages, paramMsg)
	}

	if len(tools) > 0 {
		params.Tools = make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
		for _, t := range tools {
			fn := shared.FunctionDefinitionParam{
				Name:       t.Function.Name,
				Parameters: shared.FunctionParameters(t.Function.Parameters),
			}
			if t.Function.Description != "" {
				fn.Description = param.NewOpt(t.Function.Description)
			}
			params.Tools = append(params.Tools, openai.ChatCompletionFunctionTool(fn))
		}
		params.ToolChoice.OfAuto = param.NewOpt(string(openai.ChatCompletionToolChoiceOptionAutoAuto))
	}

	if maxTokens, ok := int64FromOption(opts, "max_tokens"); ok {
		params.MaxTokens = param.NewOpt(maxTokens)
	}
	if temperature, ok := float64FromOption(opts, "temperature"); ok {
		params.Temperature = param.NewOpt(temperature)
	}

	return params, nil
}

func toOpenAIMessage(msg Message) (openai.ChatCompletionMessageParamUnion, error) {
	role := strings.ToLower(strings.TrimSpace(msg.Role))
	switch role {
	case "system":
		return openai.SystemMessage(msg.Content), nil
	case "developer":
		return openai.DeveloperMessage(msg.Content), nil
	case "user":
		return openai.UserMessage(msg.Content), nil
	case "tool":
		if strings.TrimSpace(msg.ToolCallID) == "" {
			return openai.UserMessage(msg.Content), nil
		}
		return openai.ToolMessage(msg.Content, msg.ToolCallID), nil
	case "assistant":
		assistant := openai.ChatCompletionAssistantMessageParam{}
		if msg.Content != "" {
			assistant.Content.OfString = param.NewOpt(msg.Content)
		}
		toolCalls := toOpenAIToolCallParams(msg.ToolCalls)
		if len(toolCalls) > 0 {
			assistant.ToolCalls = toolCalls
		}
		return openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant}, nil
	default:
		return openai.UserMessage(msg.Content), nil
	}
}

func toOpenAIToolCallParams(toolCalls []ToolCall) []openai.ChatCompletionMessageToolCallUnionParam {
	if len(toolCalls) == 0 {
		return nil
	}
	result := make([]openai.ChatCompletionMessageToolCallUnionParam, 0, len(toolCalls))
	for i, tc := range toolCalls {
		name, arguments := normalizeOutboundToolCall(tc)
		if name == "" {
			continue
		}
		id := strings.TrimSpace(tc.ID)
		if id == "" {
			id = fmt.Sprintf("call_%d", i+1)
		}
		result = append(result, openai.ChatCompletionMessageToolCallUnionParam{
			OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
				ID: id,
				Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
					Name:      name,
					Arguments: arguments,
				},
				Type: constant.Function("function"),
			},
		})
	}
	return result
}

func normalizeOutboundToolCall(tc ToolCall) (string, string) {
	if tc.Function != nil {
		return strings.TrimSpace(tc.Function.Name), strings.TrimSpace(tc.Function.Arguments)
	}

	name := strings.TrimSpace(tc.Name)
	if name == "" {
		return "", ""
	}
	if len(tc.Arguments) == 0 {
		return name, "{}"
	}
	raw, err := json.Marshal(tc.Arguments)
	if err != nil {
		return name, "{}"
	}
	return name, string(raw)
}

func mapChatCompletionResponse(resp *openai.ChatCompletion) *LLMResponse {
	if resp == nil || len(resp.Choices) == 0 {
		return &LLMResponse{
			Content:      "",
			FinishReason: "stop",
		}
	}

	choice := resp.Choices[0]
	content := choice.Message.Content
	toolCalls := make([]ToolCall, 0, len(choice.Message.ToolCalls))
	for _, tc := range choice.Message.ToolCalls {
		if tc.Type != "function" {
			continue
		}
		functionCall := tc.AsFunction()
		args := map[string]interface{}{}
		if functionCall.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(functionCall.Function.Arguments), &args); err != nil {
				args["raw"] = functionCall.Function.Arguments
			}
		}
		toolCalls = append(toolCalls, ToolCall{
			ID:        functionCall.ID,
			Name:      functionCall.Function.Name,
			Arguments: args,
		})
	}

	// Compatibility fallback: some models emit tool calls as XML-like text blocks
	// instead of native `tool_calls` JSON.
	if len(toolCalls) == 0 {
		compatCalls, cleanedContent := parseCompatFunctionCalls(content)
		if len(compatCalls) > 0 {
			toolCalls = compatCalls
			content = cleanedContent
		}
	}

	finishReason := strings.TrimSpace(choice.FinishReason)
	if finishReason == "" {
		finishReason = "stop"
	}

	var usage *UsageInfo
	if resp.Usage.TotalTokens > 0 || resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 {
		usage = &UsageInfo{
			PromptTokens:     int(resp.Usage.PromptTokens),
			CompletionTokens: int(resp.Usage.CompletionTokens),
			TotalTokens:      int(resp.Usage.TotalTokens),
		}
	}

	return &LLMResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
		Usage:        usage,
	}
}

func int64FromOption(options map[string]interface{}, key string) (int64, bool) {
	if options == nil {
		return 0, false
	}
	v, ok := options[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case int:
		return int64(t), true
	case int64:
		return t, true
	case float64:
		return int64(t), true
	default:
		return 0, false
	}
}

func float64FromOption(options map[string]interface{}, key string) (float64, bool) {
	if options == nil {
		return 0, false
	}
	v, ok := options[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float32:
		return float64(t), true
	case float64:
		return t, true
	case int:
		return float64(t), true
	default:
		return 0, false
	}
}

func normalizeAPIBase(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return strings.TrimRight(trimmed, "/")
	}

	path := strings.TrimRight(u.Path, "/")
	for _, suffix := range []string{
		"/chat/completions",
		"/chat",
		"/responses",
	} {
		if strings.HasSuffix(path, suffix) {
			path = strings.TrimSuffix(path, suffix)
			break
		}
	}

	if path == "" {
		path = "/"
	}
	u.Path = path
	return strings.TrimRight(u.String(), "/")
}

func parseCompatFunctionCalls(content string) ([]ToolCall, string) {
	if strings.TrimSpace(content) == "" || !strings.Contains(content, "<function_call>") {
		return nil, content
	}

	blockRe := regexp.MustCompile(`(?is)<function_call>\s*(.*?)\s*</function_call>`)
	blocks := blockRe.FindAllStringSubmatch(content, -1)
	if len(blocks) == 0 {
		return nil, content
	}

	toolCalls := make([]ToolCall, 0, len(blocks))
	for i, block := range blocks {
		raw := block[1]
		invoke := extractTag(raw, "invoke")
		if invoke != "" {
			raw = invoke
		}

		name := extractTag(raw, "toolname")
		if strings.TrimSpace(name) == "" {
			name = extractTag(raw, "tool_name")
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		args := map[string]interface{}{}
		paramsRaw := strings.TrimSpace(extractTag(raw, "parameters"))
		if paramsRaw != "" {
			if strings.HasPrefix(paramsRaw, "{") && strings.HasSuffix(paramsRaw, "}") {
				_ = json.Unmarshal([]byte(paramsRaw), &args)
			}
			if len(args) == 0 {
				paramTagRe := regexp.MustCompile(`(?is)<([a-zA-Z0-9_:-]+)>\s*(.*?)\s*</([a-zA-Z0-9_:-]+)>`)
				matches := paramTagRe.FindAllStringSubmatch(paramsRaw, -1)
				for _, m := range matches {
					if len(m) < 4 || !strings.EqualFold(strings.TrimSpace(m[1]), strings.TrimSpace(m[3])) {
						continue
					}
					k := strings.TrimSpace(m[1])
					v := strings.TrimSpace(m[2])
					if k == "" || v == "" {
						continue
					}
					args[k] = v
				}
			}
		}

		toolCalls = append(toolCalls, ToolCall{
			ID:        fmt.Sprintf("compat_call_%d", i+1),
			Name:      name,
			Arguments: args,
		})
	}

	cleaned := strings.TrimSpace(blockRe.ReplaceAllString(content, ""))
	return toolCalls, cleaned
}

func extractTag(src string, tag string) string {
	re := regexp.MustCompile(fmt.Sprintf(`(?is)<%s>\s*(.*?)\s*</%s>`, regexp.QuoteMeta(tag), regexp.QuoteMeta(tag)))
	m := re.FindStringSubmatch(src)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func (p *HTTPProvider) GetDefaultModel() string {
	return ""
}

func CreateProvider(cfg *config.Config) (LLMProvider, error) {
	apiKey := cfg.Providers.Proxy.APIKey
	apiBase := cfg.Providers.Proxy.APIBase
	authMode := cfg.Providers.Proxy.Auth

	if apiBase == "" {
		return nil, fmt.Errorf("no API base (CLIProxyAPI) configured")
	}
	if cfg.Providers.Proxy.TimeoutSec <= 0 {
		return nil, fmt.Errorf("invalid providers.proxy.timeout_sec: %d", cfg.Providers.Proxy.TimeoutSec)
	}

	return NewHTTPProvider(apiKey, apiBase, authMode, time.Duration(cfg.Providers.Proxy.TimeoutSec)*time.Second), nil
}
