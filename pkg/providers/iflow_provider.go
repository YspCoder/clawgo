package providers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	iflowCompatBaseURL   = "https://apis.iflow.cn/v1"
	iflowCompatEndpoint  = "/chat/completions"
	iflowCompatUserAgent = "iFlow-Cli"
)

type IFlowProvider struct {
	base *HTTPProvider
}

func NewIFlowProvider(providerName, apiKey, apiBase, defaultModel string, supportsResponsesCompact bool, authMode string, timeout time.Duration, oauth *oauthManager) *IFlowProvider {
	return &IFlowProvider{base: NewHTTPProvider(providerName, apiKey, apiBase, defaultModel, supportsResponsesCompact, authMode, timeout, oauth)}
}

func (p *IFlowProvider) GetDefaultModel() string { return openAICompatDefaultModel(p.base) }

func (p *IFlowProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	if p == nil || p.base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	body := buildIFlowChatRequest(p.base, messages, tools, model, options, false)
	respBody, statusCode, contentType, err := doIFlowJSONWithAttempts(ctx, p.base, body)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", statusCode, contentType, previewResponseBody(respBody))
	}
	if !json.Valid(respBody) {
		return nil, fmt.Errorf("API error (status %d, content-type %q): non-JSON response: %s", statusCode, contentType, previewResponseBody(respBody))
	}
	return parseOpenAICompatResponse(respBody)
}

func (p *IFlowProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onDelta func(string)) (*LLMResponse, error) {
	if p == nil || p.base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	if onDelta == nil {
		onDelta = func(string) {}
	}
	body := buildIFlowChatRequest(p.base, messages, tools, model, options, true)
	respBody, statusCode, contentType, err := doIFlowStreamWithAttempts(ctx, p.base, body, onDelta)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", statusCode, contentType, previewResponseBody(respBody))
	}
	if !json.Valid(respBody) {
		return nil, fmt.Errorf("API error (status %d, content-type %q): non-JSON response: %s", statusCode, contentType, previewResponseBody(respBody))
	}
	return parseOpenAICompatResponse(respBody)
}

func (p *IFlowProvider) CountTokens(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*UsageInfo, error) {
	if p == nil || p.base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	body := buildIFlowChatRequest(p.base, messages, tools, model, options, false)
	count, err := estimateOpenAICompatTokenCount(body)
	if err != nil {
		return nil, err
	}
	return &UsageInfo{
		PromptTokens: count,
		TotalTokens:  count,
	}, nil
}

func buildIFlowChatRequest(base *HTTPProvider, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, stream bool) map[string]interface{} {
	baseModel := qwenBaseModel(model)
	body := base.buildOpenAICompatChatRequest(messages, tools, baseModel, options)
	if stream {
		body["stream"] = true
		body["stream_options"] = map[string]interface{}{"include_usage": true}
		iflowEnsureToolsArray(body)
	}
	applyIFlowThinking(body, model)
	return body
}

func applyIFlowThinking(body map[string]interface{}, model string) {
	enabled, ok := iflowThinkingEnabled(model, body)
	if !ok {
		return
	}
	lowerModel := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", body["model"])))
	if strings.HasPrefix(lowerModel, "minimax") {
		body["reasoning_split"] = enabled
		return
	}
	kwargs, _ := body["chat_template_kwargs"].(map[string]interface{})
	if kwargs == nil {
		kwargs = map[string]interface{}{}
	}
	kwargs["enable_thinking"] = enabled
	delete(kwargs, "clear_thinking")
	if enabled && strings.HasPrefix(lowerModel, "glm") {
		kwargs["clear_thinking"] = false
	}
	body["chat_template_kwargs"] = kwargs
}

func iflowThinkingEnabled(model string, body map[string]interface{}) (bool, bool) {
	if suffix := strings.ToLower(strings.TrimSpace(qwenModelSuffix(model))); suffix != "" {
		switch suffix {
		case "none":
			return false, true
		default:
			return true, true
		}
	}
	if effort := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", body["reasoning_effort"]))); effort != "" {
		return effort != "none", true
	}
	if thinking, ok := body["thinking"].(map[string]interface{}); ok {
		typ := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", thinking["type"])))
		if typ == "disabled" {
			return false, true
		}
		if budget, ok := thinking["budget_tokens"]; ok {
			return intValue(budget) > 0, true
		}
		if typ != "" {
			return true, true
		}
	}
	return false, false
}

func iflowEnsureToolsArray(body map[string]interface{}) {
	if _, exists := body["tools"]; !exists {
		body["tools"] = []map[string]interface{}{}
	}
	switch tools := body["tools"].(type) {
	case []map[string]interface{}:
		if len(tools) > 0 {
			return
		}
	case []interface{}:
		if len(tools) > 0 {
			return
		}
	default:
		return
	}
	body["tools"] = []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "noop",
				"description": "Placeholder tool to stabilise streaming",
				"parameters": map[string]interface{}{
					"type": "object",
				},
			},
		},
	}
}

func doIFlowJSONWithAttempts(ctx context.Context, base *HTTPProvider, payload map[string]interface{}) ([]byte, int, string, error) {
	if base == nil {
		return nil, 0, "", fmt.Errorf("provider not configured")
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to marshal request: %w", err)
	}
	attempts, err := base.authAttempts(ctx)
	if err != nil {
		return nil, 0, "", err
	}
	var lastBody []byte
	var lastStatus int
	var lastType string
	for _, attempt := range attempts {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointFor(iflowBaseURLForAttempt(base, attempt), iflowCompatEndpoint), bytes.NewReader(jsonData))
		if err != nil {
			return nil, 0, "", fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		applyAttemptAuth(req, attempt)
		applyIFlowHeaders(req, iflowAttemptAPIKey(attempt), false)

		body, status, contentType, err := base.doJSONAttempt(req, attempt)
		if err != nil {
			return nil, 0, "", err
		}
		reason, retry := classifyOAuthFailure(status, body)
		if !retry {
			base.markAttemptSuccess(attempt)
			return body, status, contentType, nil
		}
		lastBody, lastStatus, lastType = body, status, contentType
		applyAttemptFailure(base, attempt, reason, nil)
	}
	return lastBody, lastStatus, lastType, nil
}

func doIFlowStreamWithAttempts(ctx context.Context, base *HTTPProvider, payload map[string]interface{}, onDelta func(string)) ([]byte, int, string, error) {
	if base == nil {
		return nil, 0, "", fmt.Errorf("provider not configured")
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to marshal request: %w", err)
	}
	attempts, err := base.authAttempts(ctx)
	if err != nil {
		return nil, 0, "", err
	}
	var lastBody []byte
	var lastStatus int
	var lastType string
	for _, attempt := range attempts {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointFor(iflowBaseURLForAttempt(base, attempt), iflowCompatEndpoint), bytes.NewReader(jsonData))
		if err != nil {
			return nil, 0, "", fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		applyAttemptAuth(req, attempt)
		applyIFlowHeaders(req, iflowAttemptAPIKey(attempt), true)

		body, status, contentType, quotaHit, err := base.doStreamAttempt(req, attempt, func(event string) {
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(event), &obj); err != nil {
				return
			}
			choices, _ := obj["choices"].([]interface{})
			for _, choice := range choices {
				item, _ := choice.(map[string]interface{})
				delta, _ := item["delta"].(map[string]interface{})
				if txt := strings.TrimSpace(fmt.Sprintf("%v", delta["content"])); txt != "" {
					onDelta(txt)
				}
			}
		})
		if err != nil {
			return nil, 0, "", err
		}
		if !quotaHit {
			base.markAttemptSuccess(attempt)
			return body, status, contentType, nil
		}
		lastBody, lastStatus, lastType = body, status, contentType
		reason, _ := classifyOAuthFailure(status, body)
		applyAttemptFailure(base, attempt, reason, nil)
	}
	return lastBody, lastStatus, lastType, nil
}

func iflowBaseURLForAttempt(base *HTTPProvider, attempt authAttempt) string {
	if attempt.session != nil {
		if raw := strings.TrimSpace(attempt.session.ResourceURL); raw != "" {
			return normalizeIFlowBaseURL(raw)
		}
		if attempt.session.Token != nil {
			if raw := strings.TrimSpace(asString(attempt.session.Token["base_url"])); raw != "" {
				return normalizeIFlowBaseURL(raw)
			}
			if raw := strings.TrimSpace(asString(attempt.session.Token["resource_url"])); raw != "" {
				return normalizeIFlowBaseURL(raw)
			}
		}
	}
	if base != nil && strings.TrimSpace(base.apiBase) != "" && !strings.Contains(strings.ToLower(base.apiBase), "api.openai.com") {
		return normalizeIFlowBaseURL(base.apiBase)
	}
	return iflowCompatBaseURL
}

func normalizeIFlowBaseURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return iflowCompatBaseURL
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	trimmed = normalizeAPIBase(trimmed)
	if strings.HasSuffix(strings.ToLower(trimmed), "/chat/completions") {
		trimmed = strings.TrimSuffix(trimmed, "/chat/completions")
	}
	if !strings.HasSuffix(strings.ToLower(trimmed), "/v1") {
		trimmed = strings.TrimRight(trimmed, "/") + "/v1"
	}
	return trimmed
}

func iflowAttemptAPIKey(attempt authAttempt) string {
	if attempt.session != nil && attempt.session.Token != nil {
		if v := strings.TrimSpace(asString(attempt.session.Token["api_key"])); v != "" {
			return v
		}
		if v := strings.TrimSpace(asString(attempt.session.Token["apiKey"])); v != "" {
			return v
		}
	}
	return strings.TrimSpace(attempt.token)
}

func applyIFlowHeaders(req *http.Request, apiKey string, stream bool) {
	if req == nil {
		return
	}
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}
	req.Header.Set("User-Agent", iflowCompatUserAgent)
	sessionID := "session-" + uuid.New().String()
	req.Header.Set("session-id", sessionID)
	timestamp := time.Now().UnixMilli()
	req.Header.Set("x-iflow-timestamp", fmt.Sprintf("%d", timestamp))
	if sig := createIFlowSignature(iflowCompatUserAgent, sessionID, timestamp, apiKey); sig != "" {
		req.Header.Set("x-iflow-signature", sig)
	}
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
}

func createIFlowSignature(userAgent, sessionID string, timestamp int64, apiKey string) string {
	if strings.TrimSpace(apiKey) == "" {
		return ""
	}
	payload := fmt.Sprintf("%s:%s:%d", userAgent, sessionID, timestamp)
	h := hmac.New(sha256.New, []byte(apiKey))
	_, _ = h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}
