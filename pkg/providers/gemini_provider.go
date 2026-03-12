package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	geminiBaseURL           = "https://generativelanguage.googleapis.com"
	geminiAPIVersion        = "v1beta"
	geminiImagePreviewModel = "gemini-2.5-flash-image-preview"
)

var geminiWhitePNGBase64 = base64.StdEncoding.EncodeToString([]byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9C, 0x63, 0xF8, 0xFF, 0xFF, 0x3F,
	0x00, 0x05, 0xFE, 0x02, 0xFE, 0xDC, 0xCC, 0x59,
	0xE7, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E,
	0x44, 0xAE, 0x42, 0x60, 0x82,
})

type GeminiProvider struct {
	base *HTTPProvider
}

func NewGeminiProvider(providerName, apiKey, apiBase, defaultModel string, supportsResponsesCompact bool, authMode string, timeout time.Duration, oauth *oauthManager) *GeminiProvider {
	normalizedBase := normalizeAPIBase(apiBase)
	if normalizedBase == "" {
		normalizedBase = geminiBaseURL
	}
	return &GeminiProvider{
		base: NewHTTPProvider(providerName, apiKey, normalizedBase, defaultModel, supportsResponsesCompact, authMode, timeout, oauth),
	}
}

func (p *GeminiProvider) GetDefaultModel() string {
	if p == nil || p.base == nil {
		return ""
	}
	return p.base.GetDefaultModel()
}

func (p *GeminiProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
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

func (p *GeminiProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onDelta func(string)) (*LLMResponse, error) {
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

func (p *GeminiProvider) CountTokens(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*UsageInfo, error) {
	if p == nil || p.base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	attempts, err := p.base.authAttempts(ctx)
	if err != nil {
		return nil, err
	}
	var lastBody []byte
	var lastStatus int
	var lastType string
	for _, attempt := range attempts {
		requestBody := p.buildRequestBody(messages, nil, model, options, false)
		delete(requestBody, "tools")
		delete(requestBody, "toolConfig")
		delete(requestBody, "generationConfig")
		endpoint := p.endpoint(attempt, model, "countTokens", false)
		body, status, ctype, reqErr := p.performAttempt(ctx, endpoint, requestBody, attempt, false, nil)
		if reqErr != nil {
			return nil, reqErr
		}
		lastBody, lastStatus, lastType = body, status, ctype
		reason, retry := classifyOAuthFailure(status, body)
		if retry {
			applyAttemptFailure(p.base, attempt, reason, nil)
			continue
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
		p.base.markAttemptSuccess(attempt)
		return &UsageInfo{PromptTokens: payload.TotalTokens, TotalTokens: payload.TotalTokens}, nil
	}
	return nil, fmt.Errorf("API error (status %d, content-type %q): %s", lastStatus, lastType, previewResponseBody(lastBody))
}

func (p *GeminiProvider) doRequest(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, stream bool, onDelta func(string)) ([]byte, int, string, error) {
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
		requestBody := p.buildRequestBody(messages, tools, model, options, stream)
		endpoint := p.endpoint(attempt, model, "generateContent", stream)
		body, status, ctype, reqErr := p.performAttempt(ctx, endpoint, requestBody, attempt, stream, onDelta)
		if reqErr != nil {
			return nil, 0, "", reqErr
		}
		lastBody, lastStatus, lastType = body, status, ctype
		reason, retry := classifyOAuthFailure(status, body)
		if retry {
			applyAttemptFailure(p.base, attempt, reason, nil)
			continue
		}
		p.base.markAttemptSuccess(attempt)
		return body, status, ctype, nil
	}
	return lastBody, lastStatus, lastType, nil
}

func (p *GeminiProvider) performAttempt(ctx context.Context, endpoint string, payload map[string]any, attempt authAttempt, stream bool, onDelta func(string)) ([]byte, int, string, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonData))
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	applyGeminiAttemptAuth(req, attempt)
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
		return consumeGeminiStream(resp, onDelta)
	}
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, resp.StatusCode, ctype, fmt.Errorf("failed to read response: %w", readErr)
	}
	return body, resp.StatusCode, ctype, nil
}

func (p *GeminiProvider) endpoint(attempt authAttempt, model, action string, stream bool) string {
	base := geminiBaseURLForAttempt(p.base, attempt)
	baseModel := strings.TrimSpace(qwenBaseModel(model))
	if stream {
		return fmt.Sprintf("%s/%s/models/%s:streamGenerateContent?alt=sse", base, geminiAPIVersion, baseModel)
	}
	return fmt.Sprintf("%s/%s/models/%s:%s", base, geminiAPIVersion, baseModel, action)
}

func (p *GeminiProvider) buildRequestBody(messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, stream bool) map[string]any {
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
			if parts := geminiTextParts(msg); len(parts) > 0 {
				contents = append(contents, map[string]any{"role": "user", "parts": parts})
			}
		case "assistant":
			parts := geminiAssistantParts(msg)
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
	if extra, ok := mapOption(options, "gemini_generation_config"); ok && len(extra) > 0 {
		gen := mapFromAny(request["generationConfig"])
		if gen == nil {
			gen = map[string]any{}
		}
		for k, v := range extra {
			gen[k] = v
		}
		request["generationConfig"] = gen
	}
	if toolDecls := antigravityToolDeclarations(tools); len(toolDecls) > 0 {
		request["tools"] = []map[string]any{{"function_declarations": toolDecls}}
		request["toolConfig"] = map[string]any{
			"functionCallingConfig": map[string]any{"mode": "AUTO"},
		}
	}
	applyGeminiThinkingSuffix(request, model)
	return fixGeminiImageAspectRatio(strings.TrimSpace(qwenBaseModel(model)), request)
}

func applyGeminiThinkingSuffix(request map[string]any, model string) {
	suffix := qwenModelSuffix(model)
	if strings.TrimSpace(suffix) == "" {
		return
	}
	baseModel := strings.TrimSpace(qwenBaseModel(model))
	gen := mapFromAny(request["generationConfig"])
	if gen == nil {
		gen = map[string]any{}
	}
	thinkingConfig := mapFromAny(gen["thinkingConfig"])
	if thinkingConfig == nil {
		thinkingConfig = map[string]any{}
	}
	includeThoughts, userSetIncludeThoughts := geminiExistingIncludeThoughts(thinkingConfig)
	delete(thinkingConfig, "thinkingBudget")
	delete(thinkingConfig, "thinking_budget")
	delete(thinkingConfig, "thinkingLevel")
	delete(thinkingConfig, "thinking_level")
	delete(thinkingConfig, "include_thoughts")

	setIncludeThoughts := func(defaultValue bool, force bool) {
		if force || !userSetIncludeThoughts {
			includeThoughts = defaultValue
		}
		thinkingConfig["includeThoughts"] = includeThoughts
	}

	lower := strings.ToLower(strings.TrimSpace(suffix))
	switch {
	case lower == "auto" || lower == "-1":
		thinkingConfig["thinkingBudget"] = -1
		setIncludeThoughts(true, false)
	case lower == "none":
		if geminiUsesThinkingLevels(baseModel) {
			thinkingConfig["thinkingLevel"] = "low"
		} else {
			thinkingConfig["thinkingBudget"] = 128
		}
		setIncludeThoughts(false, true)
	case isGeminiThinkingLevel(lower):
		if geminiUsesThinkingLevels(baseModel) {
			thinkingConfig["thinkingLevel"] = normalizeGeminiThinkingLevel(lower)
		} else {
			thinkingConfig["thinkingBudget"] = geminiThinkingBudgetForLevel(lower)
		}
		setIncludeThoughts(true, false)
	default:
		if budget, err := strconv.Atoi(lower); err == nil {
			if budget < 0 {
				thinkingConfig["thinkingBudget"] = -1
				setIncludeThoughts(true, false)
			} else if budget == 0 {
				thinkingConfig["thinkingBudget"] = 128
				setIncludeThoughts(false, true)
			} else {
				thinkingConfig["thinkingBudget"] = budget
				setIncludeThoughts(true, false)
			}
		}
	}
	if len(thinkingConfig) == 0 {
		return
	}
	gen["thinkingConfig"] = thinkingConfig
	request["generationConfig"] = gen
}

func geminiExistingIncludeThoughts(thinkingConfig map[string]any) (bool, bool) {
	if thinkingConfig == nil {
		return false, false
	}
	if value, ok := thinkingConfig["includeThoughts"]; ok {
		return geminiBoolValue(value), true
	}
	if value, ok := thinkingConfig["include_thoughts"]; ok {
		return geminiBoolValue(value), true
	}
	return false, false
}

func geminiBoolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "on":
			return true
		}
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	}
	return false
}

func geminiUsesThinkingLevels(model string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(trimmed, "gemini-3")
}

func isGeminiThinkingLevel(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "minimal", "low", "medium", "high", "xhigh", "max":
		return true
	default:
		return false
	}
}

func normalizeGeminiThinkingLevel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "xhigh", "max":
		return "high"
	case "minimal":
		return "low"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func geminiThinkingBudgetForLevel(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "minimal":
		return 128
	case "low":
		return 1024
	case "medium":
		return 8192
	case "high":
		return 24576
	case "xhigh", "max":
		return 32768
	default:
		return 8192
	}
}

func geminiTextParts(msg Message) []map[string]any {
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
		case "input_image", "image_url":
			if inline := geminiInlineDataPart(firstNonEmpty(part.FileData, part.ImageURL), part.MIMEType); inline != nil {
				parts = append(parts, inline)
				continue
			}
			if url := strings.TrimSpace(firstNonEmpty(part.ImageURL, part.FileURL)); url != "" {
				parts = append(parts, map[string]any{
					"fileData": map[string]any{
						"mimeType": firstNonEmpty(strings.TrimSpace(part.MIMEType), "image/png"),
						"fileUri":  url,
					},
				})
			}
		case "input_file", "file":
			if inline := geminiInlineDataPart(firstNonEmpty(part.FileData, part.FileURL), part.MIMEType); inline != nil {
				parts = append(parts, inline)
				continue
			}
			if url := strings.TrimSpace(part.FileURL); url != "" {
				parts = append(parts, map[string]any{
					"fileData": map[string]any{
						"mimeType": firstNonEmpty(strings.TrimSpace(part.MIMEType), "application/octet-stream"),
						"fileUri":  url,
					},
				})
			}
		}
	}
	if len(parts) == 0 && strings.TrimSpace(msg.Content) != "" {
		return []map[string]any{{"text": strings.TrimSpace(msg.Content)}}
	}
	return parts
}

func geminiAssistantParts(msg Message) []map[string]any {
	parts := geminiTextParts(msg)
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

func consumeGeminiStream(resp *http.Response, onDelta func(string)) ([]byte, int, string, error) {
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
					filtered := filterGeminiSSEUsageMetadata([]byte(payload))
					if delta := state.consume(filtered); delta != "" {
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

func parseGeminiResponse(body []byte) (*LLMResponse, error) {
	return parseAntigravityResponse(body)
}

func geminiBaseURLForAttempt(base *HTTPProvider, attempt authAttempt) string {
	if attempt.session != nil {
		if raw := strings.TrimSpace(attempt.session.ResourceURL); raw != "" {
			return normalizeGeminiBaseURL(raw)
		}
		if attempt.session.Token != nil {
				if raw := firstNonEmpty(
				strings.TrimSpace(asString(attempt.session.Token["base_url"])),
				strings.TrimSpace(asString(attempt.session.Token["base-url"])),
			); raw != "" {
				return normalizeGeminiBaseURL(raw)
			}
			if raw := firstNonEmpty(
				strings.TrimSpace(asString(attempt.session.Token["resource_url"])),
				strings.TrimSpace(asString(attempt.session.Token["resource-url"])),
			); raw != "" {
				return normalizeGeminiBaseURL(raw)
			}
		}
	}
	if base != nil && strings.TrimSpace(base.apiBase) != "" && !strings.Contains(strings.ToLower(base.apiBase), "api.openai.com") {
		return normalizeGeminiBaseURL(base.apiBase)
	}
	return geminiBaseURL
}

func normalizeGeminiBaseURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return geminiBaseURL
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	trimmed = normalizeAPIBase(trimmed)
	if strings.Contains(trimmed, "/models/") {
		if idx := strings.Index(trimmed, "/models/"); idx > 0 {
			trimmed = trimmed[:idx]
		}
	}
	if strings.HasSuffix(trimmed, "/models") {
		trimmed = strings.TrimSuffix(trimmed, "/models")
	}
	if strings.HasSuffix(trimmed, "/"+geminiAPIVersion) {
		trimmed = strings.TrimSuffix(trimmed, "/"+geminiAPIVersion)
	}
	return trimmed
}

func geminiInlineDataPart(raw, mimeType string) map[string]any {
	data, mt, ok := parseDataURL(raw)
	if !ok {
		return nil
	}
	return map[string]any{
		"inlineData": map[string]any{
			"mimeType": firstNonEmpty(strings.TrimSpace(mimeType), mt),
			"data":     data,
		},
	}
}

func parseDataURL(raw string) (data, mimeType string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "data:") {
		return "", "", false
	}
	comma := strings.Index(trimmed, ",")
	if comma <= len("data:") {
		return "", "", false
	}
	meta := trimmed[len("data:"):comma]
	payload := trimmed[comma+1:]
	if !strings.HasSuffix(strings.ToLower(meta), ";base64") {
		return "", "", false
	}
	mimeType = strings.TrimSuffix(meta, ";base64")
	if strings.TrimSpace(mimeType) == "" {
		mimeType = "application/octet-stream"
	}
	return payload, mimeType, true
}

func fixGeminiImageAspectRatio(modelName string, request map[string]any) map[string]any {
	if strings.TrimSpace(modelName) != geminiImagePreviewModel || request == nil {
		return request
	}
	generationConfig := mapFromAny(request["generationConfig"])
	if len(generationConfig) == 0 {
		return request
	}
	imageConfig := mapFromAny(generationConfig["imageConfig"])
	aspectRatio := strings.TrimSpace(asString(imageConfig["aspectRatio"]))
	if aspectRatio == "" {
		return request
	}
	contents, _ := request["contents"].([]map[string]any)
	hasInlineData := false
	for _, content := range contents {
		parts, _ := content["parts"].([]map[string]any)
		for _, part := range parts {
			if len(mapFromAny(part["inlineData"])) > 0 {
				hasInlineData = true
				break
			}
		}
		if hasInlineData {
			break
		}
	}
	if !hasInlineData && len(contents) > 0 {
		parts, _ := contents[0]["parts"].([]map[string]any)
		prefixed := []map[string]any{
			{"text": "Based on the following requirements, create an image within the uploaded picture. The new content must completely cover the entire area of the original picture, maintaining its exact proportions, and no blank areas should appear."},
			{"inlineData": map[string]any{"mimeType": "image/png", "data": geminiWhitePNGBase64}},
		}
		contents[0]["parts"] = append(prefixed, parts...)
		request["contents"] = contents
		generationConfig["responseModalities"] = []any{"IMAGE", "TEXT"}
	}
	delete(generationConfig, "imageConfig")
	request["generationConfig"] = generationConfig
	return request
}

func filterGeminiSSEUsageMetadata(payload []byte) []byte {
	if len(payload) == 0 {
		return payload
	}
	var root map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(payload), &root); err != nil {
		return payload
	}
	if geminiPayloadHasFinishReason(root) {
		out, err := json.Marshal(root)
		if err != nil {
			return payload
		}
		return out
	}
	delete(root, "usageMetadata")
	delete(root, "usage_metadata")
	if response := mapFromAny(root["response"]); len(response) > 0 {
		delete(response, "usageMetadata")
		root["response"] = response
	}
	out, err := json.Marshal(root)
	if err != nil {
		return payload
	}
	return out
}

func geminiPayloadHasFinishReason(root map[string]any) bool {
	if candidateHasFinishReason(root["candidates"]) {
		return true
	}
	if response := mapFromAny(root["response"]); len(response) > 0 {
		return candidateHasFinishReason(response["candidates"])
	}
	return false
}

func candidateHasFinishReason(raw any) bool {
	switch typed := raw.(type) {
	case []any:
		if len(typed) == 0 {
			return false
		}
		candidate := mapFromAny(typed[0])
		return strings.TrimSpace(asString(candidate["finishReason"])) != ""
	case []map[string]any:
		if len(typed) == 0 {
			return false
		}
		return strings.TrimSpace(asString(typed[0]["finishReason"])) != ""
	default:
		return false
	}
}

func applyGeminiAttemptAuth(req *http.Request, attempt authAttempt) {
	if req == nil {
		return
	}
	token := strings.TrimSpace(attempt.token)
	if token == "" {
		return
	}
	req.Header.Del("Authorization")
	req.Header.Del("x-goog-api-key")
	if attempt.kind == "api_key" {
		req.Header.Set("x-goog-api-key", token)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
}
