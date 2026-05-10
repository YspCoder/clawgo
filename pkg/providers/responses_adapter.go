package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

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
	if p.useOpenAICompatChatUpstream() {
		chatBody := p.buildOpenAICompatChatRequest(messages, tools, model, options)
		return p.postJSON(ctx, endpointFor(p.compatBase(), "/chat/completions"), chatBody)
	}
	if p.useCodexCompat() {
		requestBody = p.codexCompatRequestBody(requestBody)
		return p.postJSONStream(ctx, endpointFor(p.codexCompatBase(), "/responses"), requestBody, nil)
	}
	return p.postJSON(ctx, endpointFor(p.apiBase, "/responses"), requestBody)
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
	if p.useOpenAICompatChatUpstream() {
		chatBody := p.buildOpenAICompatChatRequest(messages, tools, model, options)
		chatBody["stream"] = true
		streamOptions := map[string]interface{}{"include_usage": true}
		chatBody["stream_options"] = streamOptions
		return p.postJSONStream(ctx, endpointFor(p.compatBase(), "/chat/completions"), chatBody, func(event string) {
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
	}
	if p.useCodexCompat() {
		requestBody = p.codexCompatRequestBody(requestBody)
		return p.postJSONStream(ctx, endpointFor(p.codexCompatBase(), "/responses"), requestBody, func(event string) {
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(event), &obj); err != nil {
				return
			}
			if d := strings.TrimSpace(fmt.Sprintf("%v", obj["delta"])); d != "" {
				onDelta(d)
				return
			}
			if delta, ok := obj["delta"].(map[string]interface{}); ok {
				if txt := strings.TrimSpace(fmt.Sprintf("%v", delta["text"])); txt != "" {
					onDelta(txt)
				}
			}
		})
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
