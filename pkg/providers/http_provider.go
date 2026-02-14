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

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
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

	return &LLMResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		FinishReason: choice.FinishReason,
		Usage:        apiResponse.Usage,
	}, nil
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
