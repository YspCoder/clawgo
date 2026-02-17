// ClawGo - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 ClawGo contributors

package providers

import (
	"bytes"
	"clawgo/pkg/logger"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"clawgo/pkg/config"
)

type HTTPProvider struct {
	apiKey     string
	apiBase    string
	authMode   string
	timeout    time.Duration
	httpClient *http.Client
}

func NewHTTPProvider(apiKey, apiBase, authMode string, timeout time.Duration) *HTTPProvider {
	return &HTTPProvider{
		apiKey:   apiKey,
		apiBase:  apiBase,
		authMode: authMode,
		timeout:  timeout,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (p *HTTPProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	if p.apiBase == "" {
		return nil, fmt.Errorf("API base not configured")
	}

	logger.DebugCF("provider", "HTTP chat request", map[string]interface{}{
		"api_base":       p.apiBase,
		"model":          model,
		"messages_count": len(messages),
		"tools_count":    len(tools),
		"timeout":        p.timeout.String(),
	})

	requestBody := map[string]interface{}{
		"model":    model,
		"messages": messages,
	}

	if len(tools) > 0 {
		requestBody["tools"] = tools
		requestBody["tool_choice"] = "auto"
	}

	if maxTokens, ok := options["max_tokens"].(int); ok {
		requestBody["max_tokens"] = maxTokens
	}

	if temperature, ok := options["temperature"].(float64); ok {
		requestBody["temperature"] = temperature
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.apiBase+"/chat/completions", bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		if p.authMode == "oauth" {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		} else if strings.Contains(p.apiBase, "googleapis.com") {
			// Gemini direct API uses x-goog-api-key header or key query param
			req.Header.Set("x-goog-api-key", p.apiKey)
		} else {
			authHeader := "Bearer " + p.apiKey
			req.Header.Set("Authorization", authHeader)
		}
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", resp.StatusCode, contentType, previewResponseBody(body))
	}

	if !json.Valid(body) {
		return nil, fmt.Errorf("API error (status %d, content-type %q): non-JSON response: %s", resp.StatusCode, contentType, previewResponseBody(body))
	}

	return p.parseResponse(body)
}

func (p *HTTPProvider) parseResponse(body []byte) (*LLMResponse, error) {
	var apiResponse struct {
		Choices []struct {
			Message struct {
				Content   *string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function *struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *UsageInfo `json:"usage"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(apiResponse.Choices) == 0 {
		return &LLMResponse{
			Content:      "",
			FinishReason: "stop",
		}, nil
	}

	choice := apiResponse.Choices[0]

	toolCalls := make([]ToolCall, 0, len(choice.Message.ToolCalls))
	for i, tc := range choice.Message.ToolCalls {
		arguments := make(map[string]interface{})
		name := ""

		// Handle OpenAI format with nested function object
		if tc.Type == "function" && tc.Function != nil {
			name = tc.Function.Name
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &arguments); err != nil {
					arguments["raw"] = tc.Function.Arguments
				}
			}
		} else if tc.Function != nil {
			// Legacy format without type field
			name = tc.Function.Name
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &arguments); err != nil {
					arguments["raw"] = tc.Function.Arguments
				}
			}
		}

		if strings.TrimSpace(name) == "" {
			continue
		}

		id := strings.TrimSpace(tc.ID)
		if id == "" {
			id = fmt.Sprintf("call_%d", i+1)
		}

		toolCalls = append(toolCalls, ToolCall{
			ID:        id,
			Name:      name,
			Arguments: arguments,
		})
	}

	content := ""
	if choice.Message.Content != nil {
		content = *choice.Message.Content
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

	return &LLMResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		FinishReason: choice.FinishReason,
		Usage:        apiResponse.Usage,
	}, nil
}

func previewResponseBody(body []byte) string {
	preview := strings.TrimSpace(string(body))
	preview = strings.ReplaceAll(preview, "\n", " ")
	preview = strings.ReplaceAll(preview, "\r", " ")
	if preview == "" {
		return "<empty body>"
	}
	const maxLen = 240
	if len(preview) > maxLen {
		return preview[:maxLen] + "..."
	}
	return preview
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
