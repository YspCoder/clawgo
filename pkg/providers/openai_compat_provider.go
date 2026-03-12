package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type QwenProvider struct {
	base *HTTPProvider
}

type KimiProvider struct {
	base *HTTPProvider
}

func NewQwenProvider(providerName, apiKey, apiBase, defaultModel string, supportsResponsesCompact bool, authMode string, timeout time.Duration, oauth *oauthManager) *QwenProvider {
	return &QwenProvider{base: NewHTTPProvider(providerName, apiKey, apiBase, defaultModel, supportsResponsesCompact, authMode, timeout, oauth)}
}

func NewKimiProvider(providerName, apiKey, apiBase, defaultModel string, supportsResponsesCompact bool, authMode string, timeout time.Duration, oauth *oauthManager) *KimiProvider {
	return &KimiProvider{base: NewHTTPProvider(providerName, apiKey, apiBase, defaultModel, supportsResponsesCompact, authMode, timeout, oauth)}
}

func (p *QwenProvider) GetDefaultModel() string { return openAICompatDefaultModel(p.base) }
func (p *KimiProvider) GetDefaultModel() string { return openAICompatDefaultModel(p.base) }

func (p *QwenProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	return runOpenAICompatChat(ctx, p.base, messages, tools, model, options)
}

func (p *QwenProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onDelta func(string)) (*LLMResponse, error) {
	return runOpenAICompatChatStream(ctx, p.base, messages, tools, model, options, onDelta)
}

func (p *KimiProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	return runOpenAICompatChat(ctx, p.base, messages, tools, model, options)
}

func (p *KimiProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onDelta func(string)) (*LLMResponse, error) {
	return runOpenAICompatChatStream(ctx, p.base, messages, tools, model, options, onDelta)
}

func openAICompatDefaultModel(base *HTTPProvider) string {
	if base == nil {
		return ""
	}
	return base.GetDefaultModel()
}

func runOpenAICompatChat(ctx context.Context, base *HTTPProvider, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	if base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	body, statusCode, contentType, err := base.postJSON(ctx, endpointFor(base.compatBase(), "/chat/completions"), base.buildOpenAICompatChatRequest(messages, tools, model, options))
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", statusCode, contentType, previewResponseBody(body))
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("API error (status %d, content-type %q): non-JSON response: %s", statusCode, contentType, previewResponseBody(body))
	}
	return parseOpenAICompatResponse(body)
}

func runOpenAICompatChatStream(ctx context.Context, base *HTTPProvider, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onDelta func(string)) (*LLMResponse, error) {
	if base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	if onDelta == nil {
		onDelta = func(string) {}
	}
	chatBody := base.buildOpenAICompatChatRequest(messages, tools, model, options)
	chatBody["stream"] = true
	chatBody["stream_options"] = map[string]interface{}{"include_usage": true}
	body, statusCode, contentType, err := base.postJSONStream(ctx, endpointFor(base.compatBase(), "/chat/completions"), chatBody, func(event string) {
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
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", statusCode, contentType, previewResponseBody(body))
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("API error (status %d, content-type %q): non-JSON response: %s", statusCode, contentType, previewResponseBody(body))
	}
	return parseOpenAICompatResponse(body)
}
