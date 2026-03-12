package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/YspCoder/clawgo/pkg/wsrelay"
)

type AistudioProvider struct {
	base  *HTTPProvider
	relay *wsrelay.Manager
}

func NewAistudioProvider(providerName, apiKey, apiBase, defaultModel string, supportsResponsesCompact bool, authMode string, timeout time.Duration, oauth *oauthManager) *AistudioProvider {
	normalizedBase := normalizeAPIBase(apiBase)
	if normalizedBase == "" {
		normalizedBase = geminiBaseURL
	}
	return &AistudioProvider{
		base:  NewHTTPProvider(providerName, apiKey, normalizedBase, defaultModel, supportsResponsesCompact, authMode, timeout, oauth),
		relay: getAIStudioRelayManager(),
	}
}

func (p *AistudioProvider) GetDefaultModel() string {
	if p == nil || p.base == nil {
		return ""
	}
	return p.base.GetDefaultModel()
}

func (p *AistudioProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	body, status, ctype, err := p.doRequest(ctx, messages, tools, model, options, false, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", status, ctype, previewResponseBody(body))
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("API error (status %d, content-type %q): non-JSON response: %s", status, ctype, previewResponseBody(body))
	}
	return parseGeminiResponse(body)
}

func (p *AistudioProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onDelta func(string)) (*LLMResponse, error) {
	body, status, ctype, err := p.doRequest(ctx, messages, tools, model, options, true, onDelta)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", status, ctype, previewResponseBody(body))
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("API error (status %d, content-type %q): non-JSON response: %s", status, ctype, previewResponseBody(body))
	}
	return parseGeminiResponse(body)
}

func (p *AistudioProvider) CountTokens(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*UsageInfo, error) {
	requestBody := p.buildRequestBody(messages, nil, model, options, false)
	delete(requestBody, "tools")
	delete(requestBody, "toolConfig")
	delete(requestBody, "generationConfig")
	body, status, ctype, err := p.perform(ctx, p.endpoint(model, "countTokens", false), requestBody, options, false, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", status, ctype, previewResponseBody(body))
	}
	var payload struct {
		TotalTokens int `json:"totalTokens"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("invalid countTokens response: %w", err)
	}
	return &UsageInfo{PromptTokens: payload.TotalTokens, TotalTokens: payload.TotalTokens}, nil
}

func (p *AistudioProvider) doRequest(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, stream bool, onDelta func(string)) ([]byte, int, string, error) {
	requestBody := p.buildRequestBody(messages, tools, model, options, stream)
	return p.perform(ctx, p.endpoint(model, "generateContent", stream), requestBody, options, stream, onDelta)
}

func (p *AistudioProvider) perform(ctx context.Context, endpoint string, payload map[string]any, options map[string]interface{}, stream bool, onDelta func(string)) ([]byte, int, string, error) {
	if p == nil || p.base == nil {
		return nil, 0, "", fmt.Errorf("provider not configured")
	}
	if p.relay == nil {
		p.relay = getAIStudioRelayManager()
	}
	if p.relay == nil {
		return nil, 0, "", fmt.Errorf("aistudio relay not configured")
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to marshal request: %w", err)
	}
	req := &wsrelay.HTTPRequest{
		Method: http.MethodPost,
		URL:    endpoint,
		Headers: http.Header{
			"Content-Type": []string{"application/json"},
			"Accept":       []string{"application/json"},
		},
		Body: jsonData,
	}
	if stream {
		req.Headers.Set("Accept", "text/event-stream")
	}
	channelIDs := aistudioChannelCandidates(p.base.providerName, options)
	if len(channelIDs) == 0 {
		return nil, 0, "", fmt.Errorf("aistudio relay channel not specified")
	}
	if !stream {
		var lastErr error
		for _, channelID := range channelIDs {
			resp, err := p.relay.NonStream(ctx, channelID, req)
			if err != nil {
				recordAIStudioRelayFailure(channelID, err)
				lastErr = err
				continue
			}
			if resp.Status >= 200 && resp.Status < 300 {
				recordAIStudioRelaySuccess(channelID)
				return resp.Body, resp.Status, strings.TrimSpace(resp.Headers.Get("Content-Type")), nil
			}
			retryErr := fmt.Errorf("status=%d", resp.Status)
			recordAIStudioRelayFailure(channelID, retryErr)
			lastErr = retryErr
			if resp.Status < 500 && resp.Status != http.StatusTooManyRequests {
				return resp.Body, resp.Status, strings.TrimSpace(resp.Headers.Get("Content-Type")), nil
			}
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("aistudio relay request failed")
		}
		return nil, 0, "", lastErr
	}
	if onDelta == nil {
		onDelta = func(string) {}
	}
	var lastErr error
	for _, channelID := range channelIDs {
		streamCh, err := p.relay.Stream(ctx, channelID, req)
		if err != nil {
			recordAIStudioRelayFailure(channelID, err)
			lastErr = err
			continue
		}
		state := &antigravityStreamState{}
		status := http.StatusOK
		ctype := "text/event-stream"
		var full bytes.Buffer
		started := false
		retryable := false
		failed := false
		for event := range streamCh {
			if event.Err != nil {
				recordAIStudioRelayFailure(channelID, event.Err)
				lastErr = event.Err
				retryable = !started
				failed = true
				break
			}
			switch event.Type {
			case wsrelay.MessageTypeStreamStart:
				if event.Status > 0 {
					status = event.Status
				}
				if v := strings.TrimSpace(event.Headers.Get("Content-Type")); v != "" {
					ctype = v
				}
			case wsrelay.MessageTypeStreamChunk:
				if len(event.Payload) == 0 {
					continue
				}
				started = true
				full.Write(event.Payload)
				filtered := filterGeminiSSEUsageMetadata(event.Payload)
				if delta := state.consume(filtered); delta != "" {
					onDelta(delta)
				}
			case wsrelay.MessageTypeHTTPResp:
				if event.Status > 0 {
					status = event.Status
				}
				if v := strings.TrimSpace(event.Headers.Get("Content-Type")); v != "" {
					ctype = v
				}
				if len(event.Payload) > 0 {
					if status >= 200 && status < 300 {
						recordAIStudioRelaySuccess(channelID)
					} else {
						recordAIStudioRelayFailure(channelID, fmt.Errorf("status=%d", status))
					}
					if status >= 500 || status == http.StatusTooManyRequests {
						lastErr = fmt.Errorf("status=%d", status)
						retryable = !started
						failed = true
						break
					}
					return event.Payload, status, ctype, nil
				}
				if status >= 200 && status < 300 {
					recordAIStudioRelaySuccess(channelID)
				} else {
					recordAIStudioRelayFailure(channelID, fmt.Errorf("status=%d", status))
				}
				if status >= 500 || status == http.StatusTooManyRequests {
					lastErr = fmt.Errorf("status=%d", status)
					retryable = !started
					failed = true
					break
				}
				return state.finalBody(), status, ctype, nil
			case wsrelay.MessageTypeStreamEnd:
				if status >= 200 && status < 300 {
					recordAIStudioRelaySuccess(channelID)
				} else {
					recordAIStudioRelayFailure(channelID, fmt.Errorf("status=%d", status))
				}
				if status >= 500 || status == http.StatusTooManyRequests {
					lastErr = fmt.Errorf("status=%d", status)
					retryable = !started
					failed = true
					break
				}
				return state.finalBody(), status, ctype, nil
			}
		}
		if failed && started {
			break
		}
		if !failed && full.Len() > 0 {
			recordAIStudioRelaySuccess(channelID)
			return state.finalBody(), status, ctype, nil
		}
		if !retryable {
			break
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("wsrelay: stream closed")
	}
	return nil, 0, "", lastErr
}

func (p *AistudioProvider) endpoint(model, action string, stream bool) string {
	base := geminiBaseURL
	if p != nil && p.base != nil && strings.TrimSpace(p.base.apiBase) != "" && !strings.Contains(strings.ToLower(p.base.apiBase), "api.openai.com") {
		base = normalizeGeminiBaseURL(p.base.apiBase)
	}
	baseModel := strings.TrimSpace(qwenBaseModel(model))
	if stream {
		return fmt.Sprintf("%s/%s/models/%s:streamGenerateContent?alt=sse", base, geminiAPIVersion, baseModel)
	}
	return fmt.Sprintf("%s/%s/models/%s:%s", base, geminiAPIVersion, baseModel, action)
}

func (p *AistudioProvider) buildRequestBody(messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, stream bool) map[string]any {
	gemini := &GeminiProvider{base: p.base}
	return gemini.buildRequestBody(messages, tools, model, options, stream)
}
