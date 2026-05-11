package providers

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

func parseOpenAICompatResponse(body []byte) (*LLMResponse, error) {
	var payload struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				ToolCalls        []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if len(payload.Choices) == 0 {
		return &LLMResponse{}, nil
	}
	choice := payload.Choices[0]
	resp := &LLMResponse{
		Content:          choice.Message.Content,
		ReasoningContent: choice.Message.ReasoningContent,
		FinishReason:     choice.FinishReason,
	}
	if payload.Usage.TotalTokens > 0 || payload.Usage.PromptTokens > 0 || payload.Usage.CompletionTokens > 0 {
		resp.Usage = &UsageInfo{
			PromptTokens:     payload.Usage.PromptTokens,
			CompletionTokens: payload.Usage.CompletionTokens,
			TotalTokens:      payload.Usage.TotalTokens,
		}
	}
	if len(choice.Message.ToolCalls) > 0 {
		resp.ToolCalls = make([]ToolCall, 0, len(choice.Message.ToolCalls))
		for _, tc := range choice.Message.ToolCalls {
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: &FunctionCall{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
				Name: tc.Function.Name,
			})
		}
	}
	return resp, nil
}

func (p *HTTPProvider) useCodexCompat() bool {
	if p == nil || p.oauth == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(p.oauth.cfg.Provider), defaultCodexOAuthProvider) {
		return false
	}
	base := strings.ToLower(strings.TrimSpace(p.apiBase))
	if base == "" {
		return true
	}
	return strings.Contains(base, "api.openai.com") || strings.Contains(base, "chatgpt.com/backend-api/codex")
}

func (p *HTTPProvider) codexCompatBase() string {
	if p == nil {
		return codexCompatBaseURL
	}
	base := strings.ToLower(strings.TrimSpace(p.apiBase))
	if strings.Contains(base, "chatgpt.com/backend-api/codex") {
		return normalizeAPIBase(p.apiBase)
	}
	if base != "" && !strings.Contains(base, "api.openai.com") {
		return normalizeAPIBase(p.apiBase)
	}
	return codexCompatBaseURL
}

func (p *HTTPProvider) codexCompatRequestBody(requestBody map[string]interface{}) map[string]interface{} {
	return codexCompatRequestBody(requestBody)
}

func (p *HTTPProvider) oauthProvider() string {
	if p == nil || p.oauth == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(p.oauth.cfg.Provider))
}

func (p *HTTPProvider) useOpenAICompatChatUpstream() bool {
	switch p.oauthProvider() {
	case defaultQwenOAuthProvider, defaultKimiOAuthProvider:
		return true
	default:
		return false
	}
}

func (p *HTTPProvider) useConfiguredOpenAICompatChat() bool {
	if p == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(p.responsesAPI)) {
	case "chat_completions":
		return true
	default:
		return false
	}
}

func (p *HTTPProvider) compatBase() string {
	switch p.oauthProvider() {
	case defaultQwenOAuthProvider:
		if strings.TrimSpace(p.apiBase) != "" && !strings.Contains(strings.ToLower(p.apiBase), "api.openai.com") {
			return normalizeAPIBase(p.apiBase)
		}
		return qwenCompatBaseURL
	case defaultKimiOAuthProvider:
		if strings.TrimSpace(p.apiBase) != "" && !strings.Contains(strings.ToLower(p.apiBase), "api.openai.com") {
			return normalizeAPIBase(p.apiBase)
		}
		return kimiCompatBaseURL
	default:
		return normalizeAPIBase(p.apiBase)
	}
}

func (p *HTTPProvider) compatModel(model string) string {
	trimmed := strings.TrimSpace(qwenBaseModel(model))
	if p.oauthProvider() == defaultKimiOAuthProvider && strings.HasPrefix(strings.ToLower(trimmed), "kimi-") {
		return trimmed[5:]
	}
	return trimmed
}

func (p *HTTPProvider) buildOpenAICompatChatRequest(messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) map[string]interface{} {
	requestBody := map[string]interface{}{
		"model":    p.compatModel(model),
		"messages": openAICompatMessages(messages),
	}
	if suffix := qwenModelSuffix(model); suffix != "" {
		applyOpenAICompatThinkingSuffix(requestBody, suffix)
	}
	if len(tools) > 0 {
		requestBody["tools"] = openAICompatTools(tools)
		requestBody["tool_choice"] = "auto"
		if tc, ok := rawOption(options, "tool_choice"); ok {
			requestBody["tool_choice"] = tc
		}
	}
	if maxTokens, ok := int64FromOption(options, "max_tokens"); ok {
		requestBody["max_tokens"] = maxTokens
	}
	if temperature, ok := float64FromOption(options, "temperature"); ok {
		requestBody["temperature"] = temperature
	}
	normalizeOpenAICompatThinkingMessages(requestBody)
	return requestBody
}

func openAICompatMessages(messages []Message) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(messages))
	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		content := openAICompatMessageContent(msg)
		switch role {
		case "system":
			out = append(out, map[string]interface{}{"role": "system", "content": content})
		case "developer":
			out = append(out, map[string]interface{}{"role": "user", "content": content})
		case "assistant":
			item := map[string]interface{}{"role": "assistant", "content": content}
			if reasoning := strings.TrimSpace(msg.ReasoningContent); reasoning != "" {
				item["reasoning_content"] = reasoning
			}
			if len(msg.ToolCalls) > 0 {
				toolCalls := make([]map[string]interface{}, 0, len(msg.ToolCalls))
				for _, tc := range msg.ToolCalls {
					args := ""
					if tc.Function != nil {
						args = tc.Function.Arguments
					}
					if args == "" {
						raw, _ := json.Marshal(tc.Arguments)
						args = string(raw)
					}
					name := tc.Name
					if tc.Function != nil && strings.TrimSpace(tc.Function.Name) != "" {
						name = tc.Function.Name
					}
					toolCalls = append(toolCalls, map[string]interface{}{
						"id":   tc.ID,
						"type": "function",
						"function": map[string]interface{}{
							"name":      name,
							"arguments": args,
						},
					})
				}
				item["tool_calls"] = toolCalls
			}
			out = append(out, item)
		case "tool":
			out = append(out, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": msg.ToolCallID,
				"content":      content,
			})
		default:
			out = append(out, map[string]interface{}{"role": "user", "content": content})
		}
	}
	return out
}

func normalizeOpenAICompatThinkingMessages(body map[string]interface{}) {
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
	latestReasoning := ""
	hasLatestReasoning := false
	for i := range items {
		msg := items[i]
		if !strings.EqualFold(strings.TrimSpace(fmt.Sprintf("%v", msg["role"])), "assistant") {
			continue
		}
		if raw, ok := msg["reasoning_content"]; ok {
			if reasoning := strings.TrimSpace(fmt.Sprintf("%v", raw)); reasoning != "" && reasoning != "<nil>" {
				latestReasoning = reasoning
				hasLatestReasoning = true
			}
		}
		if !assistantMessageHasToolCalls(msg) {
			continue
		}
		existingReasoning := strings.TrimSpace(fmt.Sprintf("%v", msg["reasoning_content"]))
		if existingReasoning == "" || existingReasoning == "<nil>" {
			msg["reasoning_content"] = fallbackAssistantReasoningContent(msg, hasLatestReasoning, latestReasoning)
			if reasoning := strings.TrimSpace(fmt.Sprintf("%v", msg["reasoning_content"])); reasoning != "" && reasoning != "<nil>" {
				latestReasoning = reasoning
				hasLatestReasoning = true
			}
		}
	}
}

func assistantMessageHasToolCalls(msg map[string]interface{}) bool {
	switch raw := msg["tool_calls"].(type) {
	case []interface{}:
		return len(raw) > 0
	case []map[string]interface{}:
		return len(raw) > 0
	default:
		return false
	}
}

func fallbackAssistantReasoningContent(msg map[string]interface{}, hasLatest bool, latest string) string {
	if hasLatest && strings.TrimSpace(latest) != "" {
		return latest
	}
	if text := strings.TrimSpace(fmt.Sprintf("%v", msg["content"])); text != "" && text != "<nil>" {
		return text
	}
	switch content := msg["content"].(type) {
	case []map[string]interface{}:
		return joinAssistantTextParts(content)
	case []interface{}:
		parts := make([]map[string]interface{}, 0, len(content))
		for _, raw := range content {
			part, _ := raw.(map[string]interface{})
			if part != nil {
				parts = append(parts, part)
			}
		}
		return joinAssistantTextParts(parts)
	default:
		return ""
	}
}

func joinAssistantTextParts(parts []map[string]interface{}) string {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		text := strings.TrimSpace(fmt.Sprintf("%v", part["text"]))
		if text != "" && text != "<nil>" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

func openAICompatMessageContent(msg Message) interface{} {
	if len(msg.ContentParts) == 0 {
		return msg.Content
	}
	parts := make([]map[string]interface{}, 0, len(msg.ContentParts))
	for _, part := range msg.ContentParts {
		switch strings.ToLower(strings.TrimSpace(part.Type)) {
		case "text", "input_text":
			if strings.TrimSpace(part.Text) == "" {
				continue
			}
			parts = append(parts, map[string]interface{}{
				"type": "text",
				"text": part.Text,
			})
		case "input_image", "image_url":
			imageURL := strings.TrimSpace(part.ImageURL)
			if imageURL == "" {
				continue
			}
			payload := map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]interface{}{
					"url": imageURL,
				},
			}
			if detail := strings.TrimSpace(part.Detail); detail != "" {
				payload["image_url"].(map[string]interface{})["detail"] = detail
			}
			parts = append(parts, payload)
		default:
			if strings.TrimSpace(part.Text) == "" {
				continue
			}
			parts = append(parts, map[string]interface{}{
				"type": "text",
				"text": part.Text,
			})
		}
	}
	if len(parts) == 0 {
		return msg.Content
	}
	if len(parts) == 1 && parts[0]["type"] == "text" && len(msg.ToolCalls) == 0 {
		if text, _ := parts[0]["text"].(string); text != "" {
			return text
		}
	}
	return parts
}

func openAICompatTools(tools []ToolDefinition) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        tool.Function.Name,
				"description": tool.Function.Description,
				"parameters":  tool.Function.Parameters,
			},
		})
	}
	return out
}

func codexCompatRequestBody(requestBody map[string]interface{}) map[string]interface{} {
	if requestBody == nil {
		requestBody = map[string]interface{}{}
	}
	requestBody["stream"] = true
	requestBody["store"] = false
	requestBody["parallel_tool_calls"] = true
	if _, ok := requestBody["include"]; !ok {
		requestBody["include"] = []string{"reasoning.encrypted_content"}
	}
	delete(requestBody, "max_output_tokens")
	delete(requestBody, "max_completion_tokens")
	delete(requestBody, "temperature")
	delete(requestBody, "top_p")
	delete(requestBody, "truncation")
	delete(requestBody, "user")
	if input, ok := requestBody["input"].([]map[string]interface{}); ok {
		for _, item := range input {
			if strings.EqualFold(strings.TrimSpace(fmt.Sprintf("%v", item["role"])), "system") {
				item["role"] = "developer"
			}
		}
		requestBody["input"] = input
	}
	return requestBody
}

func parseCompatFunctionCalls(content string) ([]ToolCall, string) {
	if strings.TrimSpace(content) == "" || !containsCompatFunctionCallMarkup(content) {
		return nil, content
	}
	blockRe := regexp.MustCompile(`(?is)<function_call>\s*(.*?)\s*</function_call>|<｜｜DSML｜｜tool_calls>\s*(.*?)\s*</｜｜DSML｜｜tool_calls>`)
	matches := blockRe.FindAllStringSubmatch(content, -1)
	blocks := make([]string, 0, len(matches))
	for _, match := range matches {
		switch {
		case len(match) > 1 && strings.TrimSpace(match[1]) != "":
			blocks = append(blocks, match[1])
		case len(match) > 2 && strings.TrimSpace(match[2]) != "":
			blocks = append(blocks, match[2])
		}
	}
	if len(blocks) == 0 {
		return nil, content
	}
	toolCalls := make([]ToolCall, 0, len(blocks))
	for i, raw := range blocks {
		invoke := extractTag(raw, "invoke")
		if invoke != "" {
			raw = invoke
		}
		name := extractTag(raw, "toolname")
		if strings.TrimSpace(name) == "" {
			name = extractTag(raw, "tool_name")
		}
		if strings.TrimSpace(name) == "" {
			name = extractInvokeNameAttr(raw)
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
		toolCalls = append(toolCalls, ToolCall{ID: fmt.Sprintf("compat_call_%d", i+1), Name: name, Arguments: args})
	}
	cleaned := strings.TrimSpace(blockRe.ReplaceAllString(content, ""))
	return toolCalls, cleaned
}

func containsCompatFunctionCallMarkup(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	return strings.Contains(trimmed, "<function_call>") || strings.Contains(trimmed, "<｜｜DSML｜｜tool_calls>")
}

func extractTag(src string, tag string) string {
	re := regexp.MustCompile(fmt.Sprintf(`(?is)<%s>\s*(.*?)\s*</%s>`, regexp.QuoteMeta(tag), regexp.QuoteMeta(tag)))
	m := re.FindStringSubmatch(src)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func extractInvokeNameAttr(src string) string {
	re := regexp.MustCompile(`(?is)<(?:invoke|｜｜DSML｜｜invoke)\b[^>]*\bname\s*=\s*"([^"]+)"[^>]*>`)
	m := re.FindStringSubmatch(src)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}
