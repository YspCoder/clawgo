package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	antigravityDailyBaseURL   = "https://daily-cloudcode-pa.googleapis.com"
	antigravitySandboxBaseURL = "https://daily-cloudcode-pa.sandbox.googleapis.com"
)

type AntigravityProvider struct {
	base *HTTPProvider
}

func NewAntigravityProvider(providerName, apiKey, apiBase, defaultModel string, supportsResponsesCompact bool, authMode string, timeout time.Duration, oauth *oauthManager) *AntigravityProvider {
	normalizedBase := normalizeAPIBase(apiBase)
	if normalizedBase == "" {
		normalizedBase = antigravityDailyBaseURL
	}
	return &AntigravityProvider{
		base: NewHTTPProvider(providerName, apiKey, normalizedBase, defaultModel, supportsResponsesCompact, authMode, timeout, oauth),
	}
}

func (p *AntigravityProvider) GetDefaultModel() string {
	if p == nil || p.base == nil {
		return ""
	}
	return p.base.GetDefaultModel()
}

func (p *AntigravityProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
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
	return parseAntigravityResponse(body)
}

func (p *AntigravityProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onDelta func(string)) (*LLMResponse, error) {
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
	return parseAntigravityResponse(body)
}

func (p *AntigravityProvider) doRequest(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, stream bool, onDelta func(string)) ([]byte, int, string, error) {
	if p == nil || p.base == nil {
		return nil, 0, "", fmt.Errorf("provider not configured")
	}
	attempts, err := p.base.authAttempts(ctx)
	if err != nil {
		return nil, 0, "", err
	}
	var lastBody []byte
	var lastStatus int
	var lastType string
	for _, attempt := range attempts {
		for _, baseURL := range p.baseURLs() {
			requestBody := p.buildRequestBody(messages, tools, model, options, attempt.session, stream)
			endpoint := p.endpoint(baseURL, stream)
			body, status, ctype, reqErr := p.performAttempt(ctx, endpoint, requestBody, attempt, stream, onDelta)
			if reqErr != nil {
				if strings.Contains(strings.ToLower(reqErr.Error()), "context canceled") || strings.Contains(strings.ToLower(reqErr.Error()), "deadline exceeded") {
					return nil, 0, "", reqErr
				}
				lastBody, lastStatus, lastType = nil, 0, ""
				continue
			}
			lastBody, lastStatus, lastType = body, status, ctype
			if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable || status == http.StatusBadGateway {
				continue
			}
			reason, retry := classifyOAuthFailure(status, body)
			if retry {
				if attempt.kind == "oauth" && attempt.session != nil && p.base.oauth != nil {
					p.base.oauth.markExhausted(attempt.session, reason)
					recordProviderOAuthError(p.base.providerName, attempt.session, reason)
				}
				if attempt.kind == "api_key" {
					p.base.markAPIKeyFailure(reason)
				}
				break
			}
			p.base.markAttemptSuccess(attempt)
			return body, status, ctype, nil
		}
	}
	return lastBody, lastStatus, lastType, nil
}

func (p *AntigravityProvider) performAttempt(ctx context.Context, endpoint string, payload map[string]any, attempt authAttempt, stream bool, onDelta func(string)) ([]byte, int, string, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonData))
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Close = true
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", defaultAntigravityAPIUserAgent)
	req.Header.Set("X-Goog-Api-Client", defaultAntigravityAPIClient)
	req.Header.Set("Client-Metadata", defaultAntigravityClientMeta)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	applyAttemptAuth(req, attempt)
	client, err := p.base.httpClientForAttempt(attempt)
	if err != nil {
		return nil, 0, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	ctype := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if stream && strings.Contains(strings.ToLower(ctype), "text/event-stream") {
		return consumeAntigravityStream(resp, onDelta)
	}
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, resp.StatusCode, ctype, fmt.Errorf("failed to read response: %w", readErr)
	}
	return body, resp.StatusCode, ctype, nil
}

func (p *AntigravityProvider) endpoint(baseURL string, stream bool) string {
	base := normalizeAPIBase(baseURL)
	if base == "" {
		base = antigravityDailyBaseURL
	}
	path := "/" + defaultAntigravityAPIVersion + ":generateContent"
	if stream {
		path = "/" + defaultAntigravityAPIVersion + ":streamGenerateContent?alt=sse"
	}
	return base + path
}

func (p *AntigravityProvider) baseURLs() []string {
	if p == nil || p.base == nil {
		return []string{antigravityDailyBaseURL}
	}
	if custom := normalizeAPIBase(p.base.apiBase); custom != "" && !strings.Contains(strings.ToLower(custom), "api.openai.com") {
		return []string{custom}
	}
	return []string{antigravityDailyBaseURL, antigravitySandboxBaseURL, defaultAntigravityAPIEndpoint}
}

func (p *AntigravityProvider) buildRequestBody(messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, session *oauthSession, stream bool) map[string]any {
	request := map[string]any{}
	systemParts := make([]map[string]any, 0)
	contents := make([]map[string]any, 0, len(messages))
	callNames := map[string]string{}
	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		switch role {
		case "system", "developer":
			if text := antigravityMessageText(msg); text != "" {
				systemParts = append(systemParts, map[string]any{"text": text})
			}
		case "user":
			if parts := antigravityTextParts(msg); len(parts) > 0 {
				contents = append(contents, map[string]any{"role": "user", "parts": parts})
			}
		case "assistant":
			parts := antigravityAssistantParts(msg)
			for _, tc := range msg.ToolCalls {
				name := strings.TrimSpace(tc.Name)
				if tc.Function != nil && strings.TrimSpace(tc.Function.Name) != "" {
					name = strings.TrimSpace(tc.Function.Name)
				}
				if name != "" && strings.TrimSpace(tc.ID) != "" {
					callNames[strings.TrimSpace(tc.ID)] = name
				}
			}
			if len(parts) > 0 {
				contents = append(contents, map[string]any{"role": "model", "parts": parts})
			}
		case "tool":
			if part := antigravityToolResponsePart(msg, callNames); part != nil {
				contents = append(contents, map[string]any{"role": "function", "parts": []map[string]any{part}})
			}
		default:
			if text := antigravityMessageText(msg); text != "" {
				contents = append(contents, map[string]any{"role": "user", "parts": []map[string]any{{"text": text}}})
			}
		}
	}
	if len(systemParts) > 0 {
		request["systemInstruction"] = map[string]any{"parts": systemParts}
	}
	if len(contents) > 0 {
		request["contents"] = contents
	}
	if gen := antigravityGenerationConfig(options); len(gen) > 0 {
		request["generationConfig"] = gen
	}
	if toolDecls := antigravityToolDeclarations(tools); len(toolDecls) > 0 {
		request["tools"] = []map[string]any{{"function_declarations": toolDecls}}
		request["toolConfig"] = map[string]any{
			"functionCallingConfig": map[string]any{"mode": "AUTO"},
		}
	}
	projectID := ""
	if session != nil {
		projectID = firstNonEmpty(strings.TrimSpace(session.ProjectID), asString(session.Token["project_id"]), asString(session.Token["projectId"]))
	}
	if projectID == "" {
		projectID = "default-project"
	}
	requestType := "agent"
	if strings.Contains(strings.ToLower(model), "image") {
		requestType = "image_gen"
	}
	return map[string]any{
		"project":     projectID,
		"model":       strings.TrimSpace(model),
		"userAgent":   "antigravity",
		"requestType": requestType,
		"requestId":   "agent-" + randomSessionID(),
		"request":     request,
	}
}

func antigravityMessageText(msg Message) string {
	parts := antigravityTextParts(msg)
	if len(parts) == 0 {
		return strings.TrimSpace(msg.Content)
	}
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		text := strings.TrimSpace(asString(part["text"]))
		if text != "" {
			lines = append(lines, text)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func antigravityTextParts(msg Message) []map[string]any {
	if len(msg.ContentParts) == 0 {
		if text := strings.TrimSpace(msg.Content); text != "" {
			return []map[string]any{{"text": text}}
		}
		return nil
	}
	parts := make([]map[string]any, 0, len(msg.ContentParts))
	for _, part := range msg.ContentParts {
		switch strings.ToLower(strings.TrimSpace(part.Type)) {
		case "", "text", "input_text":
			if text := strings.TrimSpace(part.Text); text != "" {
				parts = append(parts, map[string]any{"text": text})
			}
		}
	}
	if len(parts) == 0 && strings.TrimSpace(msg.Content) != "" {
		return []map[string]any{{"text": strings.TrimSpace(msg.Content)}}
	}
	return parts
}

func antigravityAssistantParts(msg Message) []map[string]any {
	parts := antigravityTextParts(msg)
	for _, tc := range msg.ToolCalls {
		name := strings.TrimSpace(tc.Name)
		args := map[string]any{}
		if tc.Function != nil {
			if strings.TrimSpace(tc.Function.Name) != "" {
				name = strings.TrimSpace(tc.Function.Name)
			}
			if strings.TrimSpace(tc.Function.Arguments) != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			}
		}
		if len(args) == 0 && len(tc.Arguments) > 0 {
			args = tc.Arguments
		}
		if name == "" {
			continue
		}
		part := map[string]any{
			"functionCall": map[string]any{
				"name": name,
				"args": args,
			},
		}
		if strings.TrimSpace(tc.ID) != "" {
			part["functionCall"].(map[string]any)["id"] = strings.TrimSpace(tc.ID)
		}
		parts = append(parts, part)
	}
	return parts
}

func antigravityToolResponsePart(msg Message, callNames map[string]string) map[string]any {
	callID := strings.TrimSpace(msg.ToolCallID)
	if callID == "" {
		return nil
	}
	name := strings.TrimSpace(callNames[callID])
	if name == "" {
		name = "tool_result"
	}
	return map[string]any{
		"functionResponse": map[string]any{
			"name": name,
			"id":   callID,
			"response": map[string]any{
				"result": strings.TrimSpace(msg.Content),
			},
		},
	}
}

func antigravityToolDeclarations(tools []ToolDefinition) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Function.Name)
		if name == "" {
			name = strings.TrimSpace(tool.Name)
		}
		if name == "" {
			continue
		}
		params := tool.Function.Parameters
		if len(params) == 0 {
			params = tool.Parameters
		}
		entry := map[string]any{
			"name":                 name,
			"description":          strings.TrimSpace(firstNonEmpty(tool.Function.Description, tool.Description)),
			"parametersJsonSchema": params,
		}
		if len(params) == 0 {
			entry["parametersJsonSchema"] = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, entry)
	}
	return out
}

func antigravityGenerationConfig(options map[string]any) map[string]any {
	cfg := map[string]any{}
	if maxTokens, ok := int64FromOption(options, "max_tokens"); ok {
		cfg["maxOutputTokens"] = maxTokens
	}
	if temperature, ok := float64FromOption(options, "temperature"); ok {
		cfg["temperature"] = temperature
	}
	return cfg
}

func consumeAntigravityStream(resp *http.Response, onDelta func(string)) ([]byte, int, string, error) {
	if onDelta == nil {
		onDelta = func(string) {}
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	var dataLines []string
	state := &antigravityStreamState{}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			if len(dataLines) > 0 {
				payload := strings.Join(dataLines, "\n")
				dataLines = dataLines[:0]
				if strings.TrimSpace(payload) != "" && strings.TrimSpace(payload) != "[DONE]" {
					if delta := state.consume([]byte(payload)); delta != "" {
						onDelta(delta)
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
		return nil, resp.StatusCode, strings.TrimSpace(resp.Header.Get("Content-Type")), fmt.Errorf("failed to read stream: %w", err)
	}
	return state.finalBody(), resp.StatusCode, strings.TrimSpace(resp.Header.Get("Content-Type")), nil
}

type antigravityStreamState struct {
	Text         string
	ToolCalls    []ToolCall
	FinishReason string
	Usage        *UsageInfo
}

func (s *antigravityStreamState) consume(payload []byte) string {
	resp, err := parseAntigravityResponse(payload)
	if err != nil {
		return ""
	}
	delta := antigravityDeltaText(s.Text, resp.Content)
	if resp.Content != "" {
		if delta == resp.Content && strings.TrimSpace(s.Text) != "" && !strings.HasPrefix(resp.Content, s.Text) {
			s.Text += delta
		} else if resp.Content != s.Text {
			s.Text = resp.Content
		}
	}
	if len(resp.ToolCalls) > 0 {
		s.ToolCalls = resp.ToolCalls
	}
	if strings.TrimSpace(resp.FinishReason) != "" {
		s.FinishReason = resp.FinishReason
	}
	if resp.Usage != nil {
		s.Usage = resp.Usage
	}
	return delta
}

func (s *antigravityStreamState) finalBody() []byte {
	parts := make([]map[string]any, 0, 1+len(s.ToolCalls))
	if strings.TrimSpace(s.Text) != "" {
		parts = append(parts, map[string]any{"text": s.Text})
	}
	for _, tc := range s.ToolCalls {
		args := map[string]any{}
		if tc.Function != nil && strings.TrimSpace(tc.Function.Arguments) != "" {
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		}
		if len(args) == 0 && len(tc.Arguments) > 0 {
			args = tc.Arguments
		}
		part := map[string]any{
			"functionCall": map[string]any{
				"name": tc.Name,
				"args": args,
			},
		}
		if strings.TrimSpace(tc.ID) != "" {
			part["functionCall"].(map[string]any)["id"] = tc.ID
		}
		parts = append(parts, part)
	}
	root := map[string]any{
		"response": map[string]any{
			"candidates": []map[string]any{{
				"content": map[string]any{"parts": parts},
			}},
		},
	}
	if strings.TrimSpace(s.FinishReason) != "" {
		root["response"].(map[string]any)["candidates"].([]map[string]any)[0]["finishReason"] = s.FinishReason
	}
	if s.Usage != nil {
		root["response"].(map[string]any)["usageMetadata"] = map[string]any{
			"promptTokenCount":     s.Usage.PromptTokens,
			"candidatesTokenCount": s.Usage.CompletionTokens,
			"totalTokenCount":      s.Usage.TotalTokens,
		}
	}
	raw, _ := json.Marshal(root)
	return raw
}

func antigravityDeltaText(previous, current string) string {
	if current == "" {
		return ""
	}
	if previous == "" {
		return current
	}
	if strings.HasPrefix(current, previous) {
		return current[len(previous):]
	}
	return current
}

func parseAntigravityResponse(body []byte) (*LLMResponse, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal antigravity response: %w", err)
	}
	root := payload
	if responseMap := mapFromAny(payload["response"]); len(responseMap) > 0 {
		root = responseMap
	}
	candidatesRaw, _ := root["candidates"].([]any)
	if len(candidatesRaw) == 0 {
		return &LLMResponse{}, nil
	}
	first := mapFromAny(candidatesRaw[0])
	content := mapFromAny(first["content"])
	partsRaw, _ := content["parts"].([]any)
	texts := make([]string, 0, len(partsRaw))
	toolCalls := make([]ToolCall, 0)
	for _, item := range partsRaw {
		part := mapFromAny(item)
		if asString(part["text"]) != "" && !strings.EqualFold(asString(part["thought"]), "true") {
			texts = append(texts, asString(part["text"]))
		}
		functionCall := mapFromAny(part["functionCall"])
		if len(functionCall) == 0 {
			continue
		}
		args := map[string]any{}
		if rawArgs, ok := functionCall["args"]; ok {
			switch typed := rawArgs.(type) {
			case map[string]any:
				args = typed
			case string:
				_ = json.Unmarshal([]byte(typed), &args)
			}
		}
		id := strings.TrimSpace(firstNonEmpty(asString(functionCall["id"]), asString(functionCall["call_id"])))
		name := strings.TrimSpace(asString(functionCall["name"]))
		argJSON, _ := json.Marshal(args)
		toolCalls = append(toolCalls, ToolCall{
			ID:   id,
			Name: name,
			Function: &FunctionCall{
				Name:      name,
				Arguments: string(argJSON),
			},
			Arguments: args,
		})
	}
	finishReason := strings.TrimSpace(asString(first["finishReason"]))
	if finishReason == "" || strings.EqualFold(finishReason, "completed") {
		finishReason = "stop"
	}
	usageMeta := mapFromAny(root["usageMetadata"])
	var usage *UsageInfo
	if len(usageMeta) > 0 {
		usage = &UsageInfo{
			PromptTokens:     intValue(usageMeta["promptTokenCount"]),
			CompletionTokens: intValue(usageMeta["candidatesTokenCount"]),
			TotalTokens:      intValue(usageMeta["totalTokenCount"]),
		}
		if usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.TotalTokens == 0 {
			usage = nil
		}
	}
	return &LLMResponse{
		Content:      strings.TrimSpace(strings.Join(texts, "\n")),
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
		Usage:        usage,
	}, nil
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		if v, err := typed.Int64(); err == nil {
			return int(v)
		}
	case string:
		var num int
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%d", &num); err == nil {
			return num
		}
	}
	return 0
}
