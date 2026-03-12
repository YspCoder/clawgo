package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type KimiProvider struct {
	base *HTTPProvider
}

func NewKimiProvider(providerName, apiKey, apiBase, defaultModel string, supportsResponsesCompact bool, authMode string, timeout time.Duration, oauth *oauthManager) *KimiProvider {
	return &KimiProvider{base: NewHTTPProvider(providerName, apiKey, apiBase, defaultModel, supportsResponsesCompact, authMode, timeout, oauth)}
}

func (p *KimiProvider) GetDefaultModel() string { return openAICompatDefaultModel(p.base) }

func (p *KimiProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	if p == nil || p.base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	body := buildKimiChatRequest(p.base, messages, tools, model, options, false)
	respBody, statusCode, contentType, err := doOpenAICompatJSONWithAttempts(ctx, p.base, "/chat/completions", body, kimiProviderHooks{})
	if err != nil {
		return nil, err
	}
	if statusCode != 200 {
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", statusCode, contentType, previewResponseBody(respBody))
	}
	if !json.Valid(respBody) {
		return nil, fmt.Errorf("API error (status %d, content-type %q): non-JSON response: %s", statusCode, contentType, previewResponseBody(respBody))
	}
	return parseOpenAICompatResponse(respBody)
}

func (p *KimiProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onDelta func(string)) (*LLMResponse, error) {
	if p == nil || p.base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	if onDelta == nil {
		onDelta = func(string) {}
	}
	body := buildKimiChatRequest(p.base, messages, tools, model, options, true)
	respBody, statusCode, contentType, err := doOpenAICompatStreamWithAttempts(ctx, p.base, "/chat/completions", body, onDelta, kimiProviderHooks{})
	if err != nil {
		return nil, err
	}
	if statusCode != 200 {
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", statusCode, contentType, previewResponseBody(respBody))
	}
	if !json.Valid(respBody) {
		return nil, fmt.Errorf("API error (status %d, content-type %q): non-JSON response: %s", statusCode, contentType, previewResponseBody(respBody))
	}
	return parseOpenAICompatResponse(respBody)
}

func (p *KimiProvider) CountTokens(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*UsageInfo, error) {
	if p == nil || p.base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	body := buildKimiChatRequest(p.base, messages, tools, model, options, false)
	count, err := estimateOpenAICompatTokenCount(body)
	if err != nil {
		return nil, err
	}
	return &UsageInfo{
		PromptTokens: count,
		TotalTokens:  count,
	}, nil
}

type kimiProviderHooks struct{}

func (kimiProviderHooks) beforeAttempt(authAttempt) (int, []byte, string, bool) {
	return 0, nil, "", false
}

func (kimiProviderHooks) endpoint(base *HTTPProvider, attempt authAttempt, path string) string {
	return endpointFor(kimiBaseURLForAttempt(base, attempt), path)
}

func (kimiProviderHooks) classifyFailure(status int, body []byte) (int, oauthFailureReason, bool, *time.Duration) {
	reason, retry := classifyOAuthFailure(status, body)
	return status, reason, retry, nil
}

func (kimiProviderHooks) afterFailure(base *HTTPProvider, attempt authAttempt, reason oauthFailureReason, retryAfter *time.Duration) {
	applyAttemptFailure(base, attempt, reason, retryAfter)
}

func buildKimiChatRequest(base *HTTPProvider, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, stream bool) map[string]interface{} {
	baseModel := stripKimiPrefix(qwenBaseModel(model))
	body := base.buildOpenAICompatChatRequest(messages, tools, baseModel, options)
	if stream {
		body["stream"] = true
		body["stream_options"] = map[string]interface{}{"include_usage": true}
	}
	applyKimiThinking(body, model)
	normalizeKimiToolMessages(body)
	return body
}

func stripKimiPrefix(model string) string {
	trimmed := strings.TrimSpace(model)
	if strings.HasPrefix(strings.ToLower(trimmed), "kimi-") {
		return trimmed[5:]
	}
	return trimmed
}

func applyKimiThinking(body map[string]interface{}, model string) {
	suffix := qwenModelSuffix(model)
	if suffix == "" {
		return
	}
	suffix = strings.ToLower(strings.TrimSpace(suffix))
	switch suffix {
	case "low", "medium", "high", "auto":
		body["reasoning_effort"] = suffix
		delete(body, "thinking")
	case "none":
		delete(body, "reasoning_effort")
		body["thinking"] = map[string]interface{}{"type": "disabled"}
	default:
		if budget, err := parsePositiveInt(suffix); err == nil && budget > 0 {
			delete(body, "reasoning_effort")
			body["thinking"] = map[string]interface{}{
				"type":          "enabled",
				"budget_tokens": budget,
			}
		}
	}
}

func parsePositiveInt(raw string) (int, error) {
	var out int
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("non-digit")
		}
		out = out*10 + int(ch-'0')
	}
	if out <= 0 {
		return 0, fmt.Errorf("not positive")
	}
	return out, nil
}

func normalizeKimiToolMessages(body map[string]interface{}) {
	var items []map[string]interface{}
	switch raw := body["messages"].(type) {
	case []map[string]interface{}:
		items = raw
	case []interface{}:
		items = make([]map[string]interface{}, 0, len(raw))
		for _, item := range raw {
			msg, _ := item.(map[string]interface{})
			if msg != nil {
				items = append(items, msg)
			}
		}
	}
	if len(items) == 0 {
		return
	}
	pending := make([]string, 0)
	latestReasoning := ""
	hasLatestReasoning := false
	for i := range items {
		msg := items[i]
		role := strings.TrimSpace(fmt.Sprintf("%v", msg["role"]))
		switch role {
		case "assistant":
			if raw, ok := msg["reasoning_content"]; ok {
				if reasoning := strings.TrimSpace(fmt.Sprintf("%v", raw)); reasoning != "" && reasoning != "<nil>" {
					latestReasoning = reasoning
					hasLatestReasoning = true
				}
			}
			var toolCallIDs []string
			switch raw := msg["tool_calls"].(type) {
			case []interface{}:
				for _, item := range raw {
					tc, _ := item.(map[string]interface{})
					if id := strings.TrimSpace(fmt.Sprintf("%v", tc["id"])); id != "" {
						toolCallIDs = append(toolCallIDs, id)
					}
				}
			case []map[string]interface{}:
				for _, tc := range raw {
					if id := strings.TrimSpace(fmt.Sprintf("%v", tc["id"])); id != "" {
						toolCallIDs = append(toolCallIDs, id)
					}
				}
			}
			if len(toolCallIDs) == 0 {
				continue
			}
			existingReasoning := ""
			if raw, ok := msg["reasoning_content"]; ok {
				existingReasoning = strings.TrimSpace(fmt.Sprintf("%v", raw))
			}
			if existingReasoning == "" || existingReasoning == "<nil>" {
				msg["reasoning_content"] = fallbackKimiAssistantReasoning(msg, hasLatestReasoning, latestReasoning)
			}
			for _, id := range toolCallIDs {
				pending = append(pending, id)
			}
		case "tool":
			if raw, ok := msg["tool_call_id"]; ok {
				if id := strings.TrimSpace(fmt.Sprintf("%v", raw)); id != "" && id != "<nil>" {
					pending = removePendingToolID(pending, id)
					continue
				}
			}
			if raw, ok := msg["call_id"]; ok {
				if callID := strings.TrimSpace(fmt.Sprintf("%v", raw)); callID != "" && callID != "<nil>" {
					msg["tool_call_id"] = callID
					pending = removePendingToolID(pending, callID)
					continue
				}
			}
			if len(pending) == 1 {
				msg["tool_call_id"] = pending[0]
				pending = pending[:0]
			}
		}
	}
}

func removePendingToolID(pending []string, want string) []string {
	for i := range pending {
		if pending[i] == want {
			return append(pending[:i], pending[i+1:]...)
		}
	}
	return pending
}

func fallbackKimiAssistantReasoning(msg map[string]interface{}, hasLatest bool, latest string) string {
	if hasLatest && strings.TrimSpace(latest) != "" {
		return latest
	}
	if text := strings.TrimSpace(fmt.Sprintf("%v", msg["content"])); text != "" {
		return text
	}
	parts := make([]string, 0)
	switch content := msg["content"].(type) {
	case []map[string]interface{}:
		for _, part := range content {
			text := strings.TrimSpace(fmt.Sprintf("%v", part["text"]))
			if text != "" {
				parts = append(parts, text)
			}
		}
	case []interface{}:
		for _, raw := range content {
			part, _ := raw.(map[string]interface{})
			text := strings.TrimSpace(fmt.Sprintf("%v", part["text"]))
			if text != "" {
				parts = append(parts, text)
			}
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	return "[reasoning unavailable]"
}

func kimiBaseURLForAttempt(base *HTTPProvider, attempt authAttempt) string {
	if base == nil {
		return kimiCompatBaseURL
	}
	if strings.TrimSpace(base.apiBase) != "" && !strings.Contains(strings.ToLower(base.apiBase), "api.openai.com") {
		return normalizeAPIBase(base.apiBase)
	}
	if attempt.session != nil && strings.TrimSpace(attempt.session.ResourceURL) != "" {
		return normalizeKimiResourceURL(attempt.session.ResourceURL)
	}
	return kimiCompatBaseURL
}

func normalizeKimiResourceURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return kimiCompatBaseURL
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasSuffix(lower, "/v1"):
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			return normalizeAPIBase(trimmed)
		}
		return normalizeAPIBase("https://" + trimmed)
	case strings.HasSuffix(lower, "/coding"):
		base := trimmed + "/v1"
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			return normalizeAPIBase(base)
		}
		return normalizeAPIBase("https://" + base)
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		return normalizeAPIBase(trimmed + "/v1")
	default:
		return normalizeAPIBase("https://" + trimmed + "/v1")
	}
}

func kimiDeviceName() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return "clawgo"
	}
	return hostname
}

func kimiDeviceModel() string {
	return runtime.GOOS + " " + runtime.GOARCH
}

func kimiDeviceID(session *oauthSession) string {
	if session != nil && strings.TrimSpace(session.DeviceID) != "" {
		return strings.TrimSpace(session.DeviceID)
	}
	if homeDir, err := os.UserHomeDir(); err == nil && strings.TrimSpace(homeDir) != "" {
		var base string
		switch runtime.GOOS {
		case "darwin":
			base = filepath.Join(homeDir, "Library", "Application Support", "kimi")
		case "windows":
			appData := os.Getenv("APPDATA")
			if appData == "" {
				appData = filepath.Join(homeDir, "AppData", "Roaming")
			}
			base = filepath.Join(appData, "kimi")
		default:
			base = filepath.Join(homeDir, ".local", "share", "kimi")
		}
		if data, err := os.ReadFile(filepath.Join(base, "device_id")); err == nil {
			if id := strings.TrimSpace(string(data)); id != "" {
				return id
			}
		}
	}
	return "clawgo-device"
}
