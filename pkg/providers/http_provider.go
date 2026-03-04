package providers

import (
	"bufio"
	"bytes"
	"clawgo/pkg/config"
	"clawgo/pkg/logger"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type HTTPProvider struct {
	apiKey                   string
	apiBase                  string
	defaultModel             string
	supportsResponsesCompact bool
	authMode                 string
	timeout                  time.Duration
	httpClient               *http.Client
}

func NewHTTPProvider(apiKey, apiBase, defaultModel string, supportsResponsesCompact bool, authMode string, timeout time.Duration) *HTTPProvider {
	normalizedBase := normalizeAPIBase(apiBase)
	return &HTTPProvider{
		apiKey:                   apiKey,
		apiBase:                  normalizedBase,
		defaultModel:             strings.TrimSpace(defaultModel),
		supportsResponsesCompact: supportsResponsesCompact,
		authMode:                 authMode,
		timeout:                  timeout,
		httpClient:               &http.Client{Timeout: timeout},
	}
}

func (p *HTTPProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	if p.apiBase == "" {
		return nil, fmt.Errorf("API base not configured")
	}

	logger.DebugCF("provider", logger.C0133, map[string]interface{}{
		"api_base":       p.apiBase,
		"model":          model,
		"messages_count": len(messages),
		"tools_count":    len(tools),
		"timeout":        p.timeout.String(),
	})

	body, statusCode, contentType, err := p.callResponses(ctx, messages, tools, model, options)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		preview := previewResponseBody(body)
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", statusCode, contentType, preview)
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("API error (status %d, content-type %q): non-JSON response: %s", statusCode, contentType, previewResponseBody(body))
	}
	return parseResponsesAPIResponse(body)
}

func (p *HTTPProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onDelta func(string)) (*LLMResponse, error) {
	if onDelta == nil {
		onDelta = func(string) {}
	}
	if p.apiBase == "" {
		return nil, fmt.Errorf("API base not configured")
	}
	body, status, ctype, err := p.callResponsesStream(ctx, messages, tools, model, options, onDelta)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", status, ctype, previewResponseBody(body))
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("API error (status %d, content-type %q): non-JSON response: %s", status, ctype, previewResponseBody(body))
	}
	return parseResponsesAPIResponse(body)
}

func (p *HTTPProvider) callResponses(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) ([]byte, int, string, error) {
	input := make([]map[string]interface{}, 0, len(messages))
	pendingCalls := map[string]struct{}{}
	for _, msg := range messages {
		input = append(input, toResponsesInputItemsWithState(msg, pendingCalls)...)
	}
	requestBody := map[string]interface{}{
		"model": model,
		"input": input,
	}
	responseTools := buildResponsesTools(tools, options)
	if len(responseTools) > 0 {
		requestBody["tools"] = responseTools
		requestBody["tool_choice"] = "auto"
		if tc, ok := rawOption(options, "tool_choice"); ok {
			requestBody["tool_choice"] = tc
		}
		if tc, ok := rawOption(options, "responses_tool_choice"); ok {
			requestBody["tool_choice"] = tc
		}
	}
	if maxTokens, ok := int64FromOption(options, "max_tokens"); ok {
		requestBody["max_output_tokens"] = maxTokens
	}
	if temperature, ok := float64FromOption(options, "temperature"); ok {
		requestBody["temperature"] = temperature
	}
	if include, ok := stringSliceOption(options, "responses_include"); ok && len(include) > 0 {
		requestBody["include"] = include
	}
	if metadata, ok := mapOption(options, "responses_metadata"); ok && len(metadata) > 0 {
		requestBody["metadata"] = metadata
	}
	if prevID, ok := stringOption(options, "responses_previous_response_id"); ok && prevID != "" {
		requestBody["previous_response_id"] = prevID
	}
	return p.postJSON(ctx, endpointFor(p.apiBase, "/responses"), requestBody)
}

func toResponsesInputItems(msg Message) []map[string]interface{} {
	return toResponsesInputItemsWithState(msg, nil)
}

func toResponsesInputItemsWithState(msg Message, pendingCalls map[string]struct{}) []map[string]interface{} {
	role := strings.ToLower(strings.TrimSpace(msg.Role))
	switch role {
	case "system", "developer", "user":
		if content := responsesMessageContent(msg); len(content) > 0 {
			return []map[string]interface{}{{
				"type":    "message",
				"role":    role,
				"content": content,
			}}
		}
		return []map[string]interface{}{responsesMessageItem(role, msg.Content, "input_text")}
	case "assistant":
		items := make([]map[string]interface{}, 0, 1+len(msg.ToolCalls))
		if msg.Content != "" || len(msg.ToolCalls) == 0 {
			items = append(items, responsesMessageItem(role, msg.Content, "output_text"))
		}
		for _, tc := range msg.ToolCalls {
			callID := tc.ID
			if callID == "" {
				continue
			}
			name := tc.Name
			argsRaw := ""
			if tc.Function != nil {
				if tc.Function.Name != "" {
					name = tc.Function.Name
				}
				argsRaw = tc.Function.Arguments
			}
			if name == "" {
				continue
			}
			if argsRaw == "" {
				argsJSON, err := json.Marshal(tc.Arguments)
				if err != nil {
					argsRaw = "{}"
				} else {
					argsRaw = string(argsJSON)
				}
			}
			if pendingCalls != nil {
				pendingCalls[callID] = struct{}{}
			}
			items = append(items, map[string]interface{}{
				"type":      "function_call",
				"call_id":   callID,
				"name":      name,
				"arguments": argsRaw,
			})
		}
		if len(items) == 0 {
			return []map[string]interface{}{responsesMessageItem(role, msg.Content, "output_text")}
		}
		return items
	case "tool":
		callID := msg.ToolCallID
		if callID == "" {
			return nil
		}
		if pendingCalls != nil {
			if _, ok := pendingCalls[callID]; !ok {
				// Strict pairing: drop orphan/duplicate tool outputs instead of degrading role.
				return nil
			}
			delete(pendingCalls, callID)
		}
		return []map[string]interface{}{map[string]interface{}{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  msg.Content,
		}}
	default:
		return []map[string]interface{}{responsesMessageItem("user", msg.Content, "input_text")}
	}
}

func responsesMessageContent(msg Message) []map[string]interface{} {
	content := make([]map[string]interface{}, 0, len(msg.ContentParts))
	for _, part := range msg.ContentParts {
		switch strings.ToLower(strings.TrimSpace(part.Type)) {
		case "input_text", "text":
			if part.Text == "" {
				continue
			}
			content = append(content, map[string]interface{}{
				"type": "input_text",
				"text": part.Text,
			})
		case "input_image", "image":
			entry := map[string]interface{}{
				"type": "input_image",
			}
			if part.ImageURL != "" {
				entry["image_url"] = part.ImageURL
			}
			if part.FileID != "" {
				entry["file_id"] = part.FileID
			}
			if detail := strings.TrimSpace(part.Detail); detail != "" {
				entry["detail"] = detail
			}
			if _, ok := entry["image_url"]; !ok {
				if _, ok := entry["file_id"]; !ok {
					continue
				}
			}
			content = append(content, entry)
		case "input_file", "file":
			entry := map[string]interface{}{
				"type": "input_file",
			}
			if part.FileData != "" {
				entry["file_data"] = part.FileData
			}
			if part.FileID != "" {
				entry["file_id"] = part.FileID
			}
			if part.FileURL != "" {
				entry["file_url"] = part.FileURL
			}
			if part.Filename != "" {
				entry["filename"] = part.Filename
			}
			if _, ok := entry["file_data"]; !ok {
				if _, ok := entry["file_id"]; !ok {
					if _, ok := entry["file_url"]; !ok {
						continue
					}
				}
			}
			content = append(content, entry)
		}
	}
	return content
}

func buildResponsesTools(tools []ToolDefinition, options map[string]interface{}) []map[string]interface{} {
	responseTools := make([]map[string]interface{}, 0, len(tools)+2)
	for _, t := range tools {
		typ := strings.ToLower(strings.TrimSpace(t.Type))
		if typ == "" {
			typ = "function"
		}
		if typ == "function" {
			name := strings.TrimSpace(t.Function.Name)
			if name == "" {
				name = strings.TrimSpace(t.Name)
			}
			if name == "" {
				continue
			}
			entry := map[string]interface{}{
				"type":       "function",
				"name":       name,
				"parameters": map[string]interface{}{},
			}
			if t.Function.Parameters != nil {
				entry["parameters"] = t.Function.Parameters
			} else if t.Parameters != nil {
				entry["parameters"] = t.Parameters
			}
			desc := strings.TrimSpace(t.Function.Description)
			if desc == "" {
				desc = strings.TrimSpace(t.Description)
			}
			if desc != "" {
				entry["description"] = desc
			}
			if t.Function.Strict != nil {
				entry["strict"] = *t.Function.Strict
			} else if t.Strict != nil {
				entry["strict"] = *t.Strict
			}
			responseTools = append(responseTools, entry)
			continue
		}

		// Built-in tool types (web_search, file_search, code_interpreter, etc.).
		entry := map[string]interface{}{
			"type": typ,
		}
		if name := strings.TrimSpace(t.Name); name != "" {
			entry["name"] = name
		}
		if desc := strings.TrimSpace(t.Description); desc != "" {
			entry["description"] = desc
		}
		if t.Strict != nil {
			entry["strict"] = *t.Strict
		}
		for k, v := range t.Parameters {
			entry[k] = v
		}
		responseTools = append(responseTools, entry)
	}

	if extraTools, ok := mapSliceOption(options, "responses_tools"); ok {
		responseTools = append(responseTools, extraTools...)
	}
	return responseTools
}

func rawOption(options map[string]interface{}, key string) (interface{}, bool) {
	if options == nil {
		return nil, false
	}
	v, ok := options[key]
	if !ok || v == nil {
		return nil, false
	}
	return v, true
}

func stringOption(options map[string]interface{}, key string) (string, bool) {
	v, ok := rawOption(options, key)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(s), true
}

func mapOption(options map[string]interface{}, key string) (map[string]interface{}, bool) {
	v, ok := rawOption(options, key)
	if !ok {
		return nil, false
	}
	m, ok := v.(map[string]interface{})
	return m, ok
}

func stringSliceOption(options map[string]interface{}, key string) ([]string, bool) {
	v, ok := rawOption(options, key)
	if !ok {
		return nil, false
	}
	switch t := v.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
		return out, true
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, item := range t {
			s := strings.TrimSpace(fmt.Sprintf("%v", item))
			if s != "" {
				out = append(out, s)
			}
		}
		return out, true
	}
	return nil, false
}

func mapSliceOption(options map[string]interface{}, key string) ([]map[string]interface{}, bool) {
	v, ok := rawOption(options, key)
	if !ok {
		return nil, false
	}
	switch t := v.(type) {
	case []map[string]interface{}:
		return t, true
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(t))
		for _, item := range t {
			m, ok := item.(map[string]interface{})
			if ok {
				out = append(out, m)
			}
		}
		return out, true
	}
	return nil, false
}

func responsesMessageItem(role, text, contentType string) map[string]interface{} {
	ct := contentType
	if ct == "" {
		ct = "input_text"
	}
	return map[string]interface{}{
		"type": "message",
		"role": role,
		"content": []map[string]interface{}{
			{
				"type": ct,
				"text": text,
			},
		},
	}
}

func (p *HTTPProvider) callResponsesStream(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onDelta func(string)) ([]byte, int, string, error) {
	input := make([]map[string]interface{}, 0, len(messages))
	pendingCalls := map[string]struct{}{}
	for _, msg := range messages {
		input = append(input, toResponsesInputItemsWithState(msg, pendingCalls)...)
	}
	requestBody := map[string]interface{}{
		"model":  model,
		"input":  input,
		"stream": true,
	}
	responseTools := buildResponsesTools(tools, options)
	if len(responseTools) > 0 {
		requestBody["tools"] = responseTools
		requestBody["tool_choice"] = "auto"
		if tc, ok := rawOption(options, "tool_choice"); ok {
			requestBody["tool_choice"] = tc
		}
		if tc, ok := rawOption(options, "responses_tool_choice"); ok {
			requestBody["tool_choice"] = tc
		}
	}
	if maxTokens, ok := int64FromOption(options, "max_tokens"); ok {
		requestBody["max_output_tokens"] = maxTokens
	}
	if temperature, ok := float64FromOption(options, "temperature"); ok {
		requestBody["temperature"] = temperature
	}
	if include, ok := stringSliceOption(options, "responses_include"); ok && len(include) > 0 {
		requestBody["include"] = include
	}
	if streamOpts, ok := mapOption(options, "responses_stream_options"); ok && len(streamOpts) > 0 {
		requestBody["stream_options"] = streamOpts
	}
	return p.postJSONStream(ctx, endpointFor(p.apiBase, "/responses"), requestBody, func(event string) {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(event), &obj); err != nil {
			return
		}
		typ := strings.TrimSpace(fmt.Sprintf("%v", obj["type"]))
		if typ == "response.output_text.delta" {
			if d := strings.TrimSpace(fmt.Sprintf("%v", obj["delta"])); d != "" {
				onDelta(d)
			}
			return
		}
		if delta, ok := obj["delta"].(map[string]interface{}); ok {
			if txt := strings.TrimSpace(fmt.Sprintf("%v", delta["text"])); txt != "" {
				onDelta(txt)
			}
		}
	})
}

func (p *HTTPProvider) postJSONStream(ctx context.Context, endpoint string, payload interface{}, onEvent func(string)) ([]byte, int, string, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonData))
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if p.apiKey != "" {
		if p.authMode == "oauth" {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		} else if strings.Contains(p.apiBase, "googleapis.com") {
			req.Header.Set("x-goog-api-key", p.apiKey)
		} else {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		}
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	ctype := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if !strings.Contains(strings.ToLower(ctype), "text/event-stream") {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, resp.StatusCode, ctype, fmt.Errorf("failed to read response: %w", readErr)
		}
		return body, resp.StatusCode, ctype, nil
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	var dataLines []string
	var finalJSON []byte
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			if len(dataLines) > 0 {
				payload := strings.Join(dataLines, "\n")
				dataLines = dataLines[:0]
				if strings.TrimSpace(payload) == "[DONE]" {
					continue
				}
				if onEvent != nil {
					onEvent(payload)
				}
				var obj map[string]interface{}
				if err := json.Unmarshal([]byte(payload), &obj); err == nil {
					if typ := strings.TrimSpace(fmt.Sprintf("%v", obj["type"])); typ == "response.completed" {
						if respObj, ok := obj["response"]; ok {
							if b, err := json.Marshal(respObj); err == nil {
								finalJSON = b
							}
						}
					}
					if choices, ok := obj["choices"]; ok {
						if b, err := json.Marshal(map[string]interface{}{"choices": choices, "usage": obj["usage"]}); err == nil {
							finalJSON = b
						}
					}
				}
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, resp.StatusCode, ctype, fmt.Errorf("failed to read stream: %w", err)
	}
	if len(finalJSON) == 0 {
		finalJSON = []byte("{}")
	}
	return finalJSON, resp.StatusCode, ctype, nil
}

func (p *HTTPProvider) postJSON(ctx context.Context, endpoint string, payload interface{}) ([]byte, int, string, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonData))
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		if p.authMode == "oauth" {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		} else if strings.Contains(p.apiBase, "googleapis.com") {
			req.Header.Set("x-goog-api-key", p.apiKey)
		} else {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		}
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, resp.StatusCode, strings.TrimSpace(resp.Header.Get("Content-Type")), fmt.Errorf("failed to read response: %w", readErr)
	}
	return body, resp.StatusCode, strings.TrimSpace(resp.Header.Get("Content-Type")), nil
}

func parseResponsesAPIResponse(body []byte) (*LLMResponse, error) {
	var resp struct {
		Status string `json:"status"`
		Output []struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			CallID  string `json:"call_id"`
			Name    string `json:"name"`
			ArgsRaw string `json:"arguments"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		OutputText string `json:"output_text"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	toolCalls := make([]ToolCall, 0)
	outputText := strings.TrimSpace(resp.OutputText)
	for _, item := range resp.Output {
		switch strings.TrimSpace(item.Type) {
		case "function_call":
			name := strings.TrimSpace(item.Name)
			if name == "" {
				continue
			}
			args := map[string]interface{}{}
			if strings.TrimSpace(item.ArgsRaw) != "" {
				if err := json.Unmarshal([]byte(item.ArgsRaw), &args); err != nil {
					args["raw"] = item.ArgsRaw
				}
			}
			id := strings.TrimSpace(item.CallID)
			if id == "" {
				id = strings.TrimSpace(item.ID)
			}
			if id == "" {
				id = fmt.Sprintf("call_%d", len(toolCalls)+1)
			}
			toolCalls = append(toolCalls, ToolCall{ID: id, Name: name, Arguments: args})
		case "message":
			if outputText == "" {
				texts := make([]string, 0, len(item.Content))
				for _, c := range item.Content {
					if strings.TrimSpace(c.Type) == "output_text" && strings.TrimSpace(c.Text) != "" {
						texts = append(texts, c.Text)
					}
				}
				if len(texts) > 0 {
					outputText = strings.Join(texts, "\n")
				}
			}
		}
	}

	if len(toolCalls) == 0 {
		compatCalls, cleanedContent := parseCompatFunctionCalls(outputText)
		if len(compatCalls) > 0 {
			toolCalls = compatCalls
			outputText = cleanedContent
		}
	}

	finishReason := strings.TrimSpace(resp.Status)
	if finishReason == "" || finishReason == "completed" {
		finishReason = "stop"
	}

	var usage *UsageInfo
	if resp.Usage.TotalTokens > 0 || resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0 {
		usage = &UsageInfo{PromptTokens: resp.Usage.InputTokens, CompletionTokens: resp.Usage.OutputTokens, TotalTokens: resp.Usage.TotalTokens}
	}
	return &LLMResponse{Content: strings.TrimSpace(outputText), ToolCalls: toolCalls, FinishReason: finishReason, Usage: usage}, nil
}

func previewResponseBody(body []byte) string {
	preview := strings.TrimSpace(string(body))
	preview = strings.ReplaceAll(preview, "\n", " ")
	preview = strings.ReplaceAll(preview, "\r", " ")
	if preview == "" {
		return "<empty body>"
	}
	const maxLen = 600
	if len(preview) > maxLen {
		return preview[:maxLen] + "..."
	}
	return preview
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

func endpointFor(base, relative string) string {
	b := strings.TrimRight(strings.TrimSpace(base), "/")
	if b == "" {
		return relative
	}
	if strings.HasSuffix(b, relative) {
		return b
	}
	if relative == "/responses/compact" && strings.HasSuffix(b, "/responses") {
		return b + "/compact"
	}
	if relative == "/responses" && strings.HasSuffix(b, "/responses/compact") {
		return strings.TrimSuffix(b, "/compact")
	}
	return b + relative
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
		toolCalls = append(toolCalls, ToolCall{ID: fmt.Sprintf("compat_call_%d", i+1), Name: name, Arguments: args})
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
	return p != nil && p.supportsResponsesCompact
}

func (p *HTTPProvider) BuildSummaryViaResponsesCompact(ctx context.Context, model string, existingSummary string, messages []Message, maxSummaryChars int) (string, error) {
	if !p.SupportsResponsesCompact() {
		return "", fmt.Errorf("responses compact is not enabled for this provider")
	}
	input := make([]map[string]interface{}, 0, len(messages)+1)
	if strings.TrimSpace(existingSummary) != "" {
		input = append(input, responsesMessageItem("system", "Existing summary:\n"+strings.TrimSpace(existingSummary), "input_text"))
	}
	pendingCalls := map[string]struct{}{}
	for _, msg := range messages {
		input = append(input, toResponsesInputItemsWithState(msg, pendingCalls)...)
	}
	if len(input) == 0 {
		return strings.TrimSpace(existingSummary), nil
	}

	compactReq := map[string]interface{}{"model": model, "input": input}
	compactBody, statusCode, contentType, err := p.postJSON(ctx, endpointFor(p.apiBase, "/responses/compact"), compactReq)
	if err != nil {
		return "", fmt.Errorf("responses compact request failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return "", fmt.Errorf("responses compact request failed (status %d, content-type %q): %s", statusCode, contentType, previewResponseBody(compactBody))
	}
	if !json.Valid(compactBody) {
		return "", fmt.Errorf("responses compact request failed (status %d, content-type %q): non-JSON response: %s", statusCode, contentType, previewResponseBody(compactBody))
	}

	var compactResp struct {
		Output         interface{} `json:"output"`
		CompactedInput interface{} `json:"compacted_input"`
		Compacted      interface{} `json:"compacted"`
	}
	if err := json.Unmarshal(compactBody, &compactResp); err != nil {
		return "", fmt.Errorf("responses compact request failed: invalid JSON: %w", err)
	}
	compactPayload := compactResp.Output
	if compactPayload == nil {
		compactPayload = compactResp.CompactedInput
	}
	if compactPayload == nil {
		compactPayload = compactResp.Compacted
	}
	payloadBytes, err := json.Marshal(compactPayload)
	if err != nil {
		return "", fmt.Errorf("failed to serialize compact output: %w", err)
	}
	compactedPayload := strings.TrimSpace(string(payloadBytes))
	if compactedPayload == "" || compactedPayload == "null" {
		return "", fmt.Errorf("empty compact output")
	}
	if len(compactedPayload) > 12000 {
		compactedPayload = compactedPayload[:12000] + "..."
	}

	summaryPrompt := fmt.Sprintf(
		"Compacted conversation JSON:\n%s\n\nReturn a concise markdown summary with sections: Key Facts, Decisions, Open Items, Next Steps.",
		compactedPayload,
	)
	summaryReq := map[string]interface{}{
		"model": model,
		"input": summaryPrompt,
	}
	if maxSummaryChars > 0 {
		estMaxTokens := maxSummaryChars / 3
		if estMaxTokens < 128 {
			estMaxTokens = 128
		}
		summaryReq["max_output_tokens"] = estMaxTokens
	}
	summaryBody, summaryStatus, summaryType, err := p.postJSON(ctx, endpointFor(p.apiBase, "/responses"), summaryReq)
	if err != nil {
		return "", fmt.Errorf("responses summary request failed: %w", err)
	}
	if summaryStatus != http.StatusOK {
		return "", fmt.Errorf("responses summary request failed (status %d, content-type %q): %s", summaryStatus, summaryType, previewResponseBody(summaryBody))
	}
	if !json.Valid(summaryBody) {
		return "", fmt.Errorf("responses summary request failed (status %d, content-type %q): non-JSON response: %s", summaryStatus, summaryType, previewResponseBody(summaryBody))
	}
	summaryResp, err := parseResponsesAPIResponse(summaryBody)
	if err != nil {
		return "", fmt.Errorf("responses summary request failed: %w", err)
	}
	summary := strings.TrimSpace(summaryResp.Content)
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
	return NewHTTPProvider(pc.APIKey, pc.APIBase, defaultModel, pc.SupportsResponsesCompact, pc.Auth, time.Duration(pc.TimeoutSec)*time.Second), nil
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
	return pc.SupportsResponsesCompact
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
