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
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"github.com/openai/openai-go/v3/shared/constant"
)

const (
	ProtocolChatCompletions = "chat_completions"
	ProtocolResponses       = "responses"
)

type HTTPProvider struct {
	apiKey                   string
	apiBase                  string
	protocol                 string
	defaultModel             string
	supportsResponsesCompact bool
	authMode                 string
	timeout                  time.Duration
	httpClient               *http.Client
	client                   openai.Client
}

func NewHTTPProvider(apiKey, apiBase, protocol, defaultModel string, supportsResponsesCompact bool, authMode string, timeout time.Duration) *HTTPProvider {
	normalizedBase := normalizeAPIBase(apiBase)
	resolvedProtocol := normalizeProtocol(protocol)
	resolvedDefaultModel := strings.TrimSpace(defaultModel)
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
		apiKey:                   apiKey,
		apiBase:                  normalizedBase,
		protocol:                 resolvedProtocol,
		defaultModel:             resolvedDefaultModel,
		supportsResponsesCompact: supportsResponsesCompact,
		authMode:                 authMode,
		timeout:                  timeout,
		httpClient:               httpClient,
		client:                   openai.NewClient(clientOpts...),
	}
}

func (p *HTTPProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	if p.apiBase == "" {
		return nil, fmt.Errorf("API base not configured")
	}

	logger.DebugCF("provider", "OpenAI SDK chat request", map[string]interface{}{
		"api_base":       p.apiBase,
		"protocol":       p.protocol,
		"model":          model,
		"messages_count": len(messages),
		"tools_count":    len(tools),
		"timeout":        p.timeout.String(),
	})

	if p.protocol == ProtocolResponses {
		params, err := buildResponsesParams(messages, tools, model, options)
		if err != nil {
			return nil, err
		}
		resp, err := p.client.Responses.New(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("API error: %w", err)
		}
		return mapResponsesAPIResponse(resp), nil
	}

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

func buildResponsesParams(messages []Message, tools []ToolDefinition, model string, opts map[string]interface{}) (responses.ResponseNewParams, error) {
	params := responses.ResponseNewParams{
		Model: model,
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: make(responses.ResponseInputParam, 0, len(messages)),
		},
	}

	for _, msg := range messages {
		inputItems := toResponsesInputItems(msg)
		params.Input.OfInputItemList = append(params.Input.OfInputItemList, inputItems...)
	}

	if len(tools) > 0 {
		params.Tools = make([]responses.ToolUnionParam, 0, len(tools))
		for _, t := range tools {
			tool := responses.ToolParamOfFunction(t.Function.Name, t.Function.Parameters, false)
			if t.Function.Description != "" && tool.OfFunction != nil {
				tool.OfFunction.Description = param.NewOpt(t.Function.Description)
			}
			params.Tools = append(params.Tools, tool)
		}
		params.ToolChoice.OfToolChoiceMode = param.NewOpt(responses.ToolChoiceOptionsAuto)
	}

	if maxTokens, ok := int64FromOption(opts, "max_tokens"); ok {
		params.MaxOutputTokens = param.NewOpt(maxTokens)
	}
	if temperature, ok := float64FromOption(opts, "temperature"); ok {
		params.Temperature = param.NewOpt(temperature)
	}

	return params, nil
}

func toResponsesInputItems(msg Message) []responses.ResponseInputItemUnionParam {
	role := strings.ToLower(strings.TrimSpace(msg.Role))
	switch role {
	case "system":
		return []responses.ResponseInputItemUnionParam{
			responses.ResponseInputItemParamOfMessage(msg.Content, responses.EasyInputMessageRoleSystem),
		}
	case "developer":
		return []responses.ResponseInputItemUnionParam{
			responses.ResponseInputItemParamOfMessage(msg.Content, responses.EasyInputMessageRoleDeveloper),
		}
	case "assistant":
		items := []responses.ResponseInputItemUnionParam{
			responses.ResponseInputItemParamOfMessage(msg.Content, responses.EasyInputMessageRoleAssistant),
		}
		for _, tc := range msg.ToolCalls {
			name, arguments := normalizeOutboundToolCall(tc)
			if name == "" {
				continue
			}
			callID := strings.TrimSpace(tc.ID)
			if callID == "" {
				callID = fmt.Sprintf("call_%d", len(items))
			}
			items = append(items, responses.ResponseInputItemParamOfFunctionCall(arguments, callID, name))
		}
		return items
	case "tool":
		if strings.TrimSpace(msg.ToolCallID) == "" {
			return []responses.ResponseInputItemUnionParam{
				responses.ResponseInputItemParamOfMessage(msg.Content, responses.EasyInputMessageRoleUser),
			}
		}
		return []responses.ResponseInputItemUnionParam{
			responses.ResponseInputItemParamOfFunctionCallOutput(msg.ToolCallID, msg.Content),
		}
	default:
		return []responses.ResponseInputItemUnionParam{
			responses.ResponseInputItemParamOfMessage(msg.Content, responses.EasyInputMessageRoleUser),
		}
	}
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

func mapResponsesAPIResponse(resp *responses.Response) *LLMResponse {
	if resp == nil {
		return &LLMResponse{
			Content:      "",
			FinishReason: "stop",
		}
	}

	content := resp.OutputText()
	toolCalls := make([]ToolCall, 0)
	for _, item := range resp.Output {
		if item.Type != "function_call" {
			continue
		}
		call := item.AsFunctionCall()
		if strings.TrimSpace(call.Name) == "" {
			continue
		}
		args := map[string]interface{}{}
		if call.Arguments != "" {
			if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
				args["raw"] = call.Arguments
			}
		}
		id := strings.TrimSpace(call.CallID)
		if id == "" {
			id = strings.TrimSpace(call.ID)
		}
		if id == "" {
			id = fmt.Sprintf("call_%d", len(toolCalls)+1)
		}
		toolCalls = append(toolCalls, ToolCall{
			ID:        id,
			Name:      call.Name,
			Arguments: args,
		})
	}

	if len(toolCalls) == 0 {
		compatCalls, cleanedContent := parseCompatFunctionCalls(content)
		if len(compatCalls) > 0 {
			toolCalls = compatCalls
			content = cleanedContent
		}
	}

	finishReason := strings.TrimSpace(string(resp.Status))
	if finishReason == "" || finishReason == "completed" {
		finishReason = "stop"
	}

	var usage *UsageInfo
	if resp.Usage.TotalTokens > 0 || resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0 {
		usage = &UsageInfo{
			PromptTokens:     int(resp.Usage.InputTokens),
			CompletionTokens: int(resp.Usage.OutputTokens),
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

	u.Path = strings.TrimRight(u.Path, "/")
	return strings.TrimRight(u.String(), "/")
}

func normalizeProtocol(raw string) string {
	switch strings.TrimSpace(raw) {
	case "", ProtocolChatCompletions:
		return ProtocolChatCompletions
	case ProtocolResponses:
		return ProtocolResponses
	default:
		return ProtocolChatCompletions
	}
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
	return p.defaultModel
}

func (p *HTTPProvider) SupportsResponsesCompact() bool {
	return p != nil && p.supportsResponsesCompact && p.protocol == ProtocolResponses
}

func (p *HTTPProvider) BuildSummaryViaResponsesCompact(
	ctx context.Context,
	model string,
	existingSummary string,
	messages []Message,
	maxSummaryChars int,
) (string, error) {
	if !p.SupportsResponsesCompact() {
		return "", fmt.Errorf("responses compact is not enabled for this provider")
	}

	inputItems := make(responses.ResponseInputParam, 0, len(messages)+1)
	if strings.TrimSpace(existingSummary) != "" {
		inputItems = append(inputItems, responses.ResponseInputItemParamOfMessage(
			"Existing summary:\n"+strings.TrimSpace(existingSummary),
			responses.EasyInputMessageRoleSystem,
		))
	}
	for _, msg := range messages {
		inputItems = append(inputItems, toResponsesInputItems(msg)...)
	}
	if len(inputItems) == 0 {
		return strings.TrimSpace(existingSummary), nil
	}

	compacted, err := p.client.Responses.Compact(ctx, responses.ResponseCompactParams{
		Model: responses.ResponseCompactParamsModel(model),
		Input: responses.ResponseCompactParamsInputUnion{
			OfResponseInputItemArray: inputItems,
		},
	})
	if err != nil {
		return "", fmt.Errorf("responses compact request failed: %w", err)
	}

	payload, err := json.Marshal(compacted.Output)
	if err != nil {
		return "", fmt.Errorf("failed to serialize compact output: %w", err)
	}
	compactedPayload := strings.TrimSpace(string(payload))
	if compactedPayload == "" {
		return "", fmt.Errorf("empty compact output")
	}
	if len(compactedPayload) > 12000 {
		compactedPayload = compactedPayload[:12000] + "..."
	}

	summaryPrompt := fmt.Sprintf(
		"Compacted conversation JSON:\n%s\n\nReturn a concise markdown summary with sections: Key Facts, Decisions, Open Items, Next Steps.",
		compactedPayload,
	)
	summaryResp, err := p.client.Responses.New(ctx, responses.ResponseNewParams{
		Model: model,
		Input: responses.ResponseNewParamsInputUnion{
			OfString: param.NewOpt(summaryPrompt),
		},
	})
	if err != nil {
		return "", fmt.Errorf("responses summary request failed: %w", err)
	}
	summary := strings.TrimSpace(summaryResp.OutputText())
	if summary == "" {
		return "", fmt.Errorf("empty summary after responses compact")
	}
	if maxSummaryChars > 0 && len(summary) > maxSummaryChars {
		summary = summary[:maxSummaryChars]
	}
	return summary, nil
}

func CreateProvider(cfg *config.Config) (LLMProvider, error) {
	name := strings.TrimSpace(cfg.Agents.Defaults.Proxy)
	if name == "" {
		name = "proxy"
	}
	return CreateProviderByName(cfg, name)
}

func CreateProviderByName(cfg *config.Config, name string) (LLMProvider, error) {
	pc, err := getProviderConfigByName(cfg, name)
	if err != nil {
		return nil, err
	}
	if pc.APIBase == "" {
		return nil, fmt.Errorf("no API base configured for provider %q", name)
	}
	if pc.TimeoutSec <= 0 {
		return nil, fmt.Errorf("invalid timeout_sec for provider %q: %d", name, pc.TimeoutSec)
	}
	defaultModel := ""
	if len(pc.Models) > 0 {
		defaultModel = pc.Models[0]
	}
	return NewHTTPProvider(
		pc.APIKey,
		pc.APIBase,
		pc.Protocol,
		defaultModel,
		pc.SupportsResponsesCompact,
		pc.Auth,
		time.Duration(pc.TimeoutSec)*time.Second,
	), nil
}

func CreateProviders(cfg *config.Config) (map[string]LLMProvider, error) {
	configs := getAllProviderConfigs(cfg)
	if len(configs) == 0 {
		return nil, fmt.Errorf("no providers configured")
	}
	out := make(map[string]LLMProvider, len(configs))
	for name := range configs {
		p, err := CreateProviderByName(cfg, name)
		if err != nil {
			return nil, err
		}
		out[name] = p
	}
	return out, nil
}

func GetProviderModels(cfg *config.Config, name string) []string {
	pc, err := getProviderConfigByName(cfg, name)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(pc.Models))
	seen := map[string]bool{}
	for _, m := range pc.Models {
		model := strings.TrimSpace(m)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		out = append(out, model)
	}
	return out
}

func ProviderSupportsResponsesCompact(cfg *config.Config, name string) bool {
	pc, err := getProviderConfigByName(cfg, name)
	if err != nil {
		return false
	}
	if !pc.SupportsResponsesCompact {
		return false
	}
	return normalizeProtocol(pc.Protocol) == ProtocolResponses
}

func ListProviderNames(cfg *config.Config) []string {
	configs := getAllProviderConfigs(cfg)
	if len(configs) == 0 {
		return nil
	}
	names := make([]string, 0, len(configs))
	for name := range configs {
		names = append(names, name)
	}
	return names
}

func getAllProviderConfigs(cfg *config.Config) map[string]config.ProviderConfig {
	out := map[string]config.ProviderConfig{}
	if cfg == nil {
		return out
	}
	includeLegacyProxy := len(cfg.Providers.Proxies) == 0 || strings.TrimSpace(cfg.Agents.Defaults.Proxy) == "proxy" || containsStringTrimmed(cfg.Agents.Defaults.ProxyFallbacks, "proxy")
	if includeLegacyProxy && (cfg.Providers.Proxy.APIBase != "" || cfg.Providers.Proxy.APIKey != "" || cfg.Providers.Proxy.TimeoutSec > 0) {
		out["proxy"] = cfg.Providers.Proxy
	}
	for name, pc := range cfg.Providers.Proxies {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		out[trimmed] = pc
	}
	return out
}

func containsStringTrimmed(values []string, target string) bool {
	t := strings.TrimSpace(target)
	for _, v := range values {
		if strings.TrimSpace(v) == t {
			return true
		}
	}
	return false
}

func getProviderConfigByName(cfg *config.Config, name string) (config.ProviderConfig, error) {
	if cfg == nil {
		return config.ProviderConfig{}, fmt.Errorf("nil config")
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return config.ProviderConfig{}, fmt.Errorf("empty provider name")
	}
	if trimmed == "proxy" {
		return cfg.Providers.Proxy, nil
	}
	pc, ok := cfg.Providers.Proxies[trimmed]
	if !ok {
		return config.ProviderConfig{}, fmt.Errorf("provider %q not found", trimmed)
	}
	return pc, nil
}
