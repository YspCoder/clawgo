package providers

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

const claudeBaseURL = "https://api.anthropic.com"
const claudeToolPrefix = ""

type ClaudeProvider struct {
	base *HTTPProvider
}

func NewClaudeProvider(providerName, apiKey, apiBase, defaultModel string, supportsResponsesCompact bool, authMode string, timeout time.Duration, oauth *oauthManager) *ClaudeProvider {
	return &ClaudeProvider{
		base: NewHTTPProvider(providerName, apiKey, apiBase, defaultModel, supportsResponsesCompact, authMode, timeout, oauth),
	}
}

func (p *ClaudeProvider) GetDefaultModel() string {
	if p == nil || p.base == nil {
		return ""
	}
	return p.base.GetDefaultModel()
}

func (p *ClaudeProvider) CountTokens(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*UsageInfo, error) {
	if p == nil || p.base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	body, statusCode, contentType, err := p.countTokens(ctx, p.countTokensRequestBody(messages, tools, model, options), options)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", statusCode, contentType, previewResponseBody(body))
	}
	var payload struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("invalid count_tokens response: %w", err)
	}
	return &UsageInfo{
		PromptTokens: payload.InputTokens,
		TotalTokens:  payload.InputTokens,
	}, nil
}

func (p *ClaudeProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	if p == nil || p.base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	body, statusCode, contentType, err := p.postJSON(ctx, p.requestBody(messages, tools, model, options, false), options)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", statusCode, contentType, previewResponseBody(body))
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("API error (status %d, content-type %q): non-JSON response: %s", statusCode, contentType, previewResponseBody(body))
	}
	body = stripClaudeToolPrefixFromResponse(body, claudeToolPrefix)
	return parseClaudeResponse(body)
}

func (p *ClaudeProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onDelta func(string)) (*LLMResponse, error) {
	if p == nil || p.base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	if onDelta == nil {
		onDelta = func(string) {}
	}
	body, statusCode, contentType, err := p.stream(ctx, p.requestBody(messages, tools, model, options, true), options, onDelta)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", statusCode, contentType, previewResponseBody(body))
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("API error (status %d, content-type %q): non-JSON response: %s", statusCode, contentType, previewResponseBody(body))
	}
	body = stripClaudeToolPrefixFromResponse(body, claudeToolPrefix)
	return parseClaudeResponse(body)
}

func (p *ClaudeProvider) baseURL() string {
	if p == nil || p.base == nil {
		return claudeBaseURL
	}
	base := strings.TrimSpace(p.base.apiBase)
	if base == "" || strings.Contains(strings.ToLower(base), "api.openai.com") {
		return claudeBaseURL
	}
	return normalizeAPIBase(base)
}

func (p *ClaudeProvider) requestBody(messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, stream bool) map[string]interface{} {
	systemParts := make([]string, 0)
	outMessages := make([]map[string]interface{}, 0, len(messages))
	callNames := map[string]string{}
	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		switch role {
		case "system", "developer":
			if text := claudeTextParts(msg.ContentParts); text != "" {
				systemParts = append(systemParts, text)
			}
			if text := strings.TrimSpace(msg.Content); text != "" {
				systemParts = append(systemParts, text)
			}
		case "assistant":
			content := make([]map[string]interface{}, 0, 1+len(msg.ToolCalls))
			if text := strings.TrimSpace(msg.Content); text != "" {
				content = append(content, map[string]interface{}{"type": "text", "text": text})
			}
			for _, tc := range msg.ToolCalls {
				name := strings.TrimSpace(tc.Name)
				if tc.Function != nil && strings.TrimSpace(tc.Function.Name) != "" {
					name = strings.TrimSpace(tc.Function.Name)
				}
				if name == "" {
					continue
				}
				input := map[string]interface{}{}
				if tc.Function != nil && strings.TrimSpace(tc.Function.Arguments) != "" {
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
				}
				if len(input) == 0 && len(tc.Arguments) > 0 {
					input = tc.Arguments
				}
				if strings.TrimSpace(tc.ID) != "" {
					callNames[strings.TrimSpace(tc.ID)] = name
				}
				content = append(content, map[string]interface{}{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  name,
					"input": input,
				})
			}
			if len(content) == 1 && len(msg.ToolCalls) == 0 && strings.EqualFold(asString(content[0]["type"]), "text") {
				outMessages = append(outMessages, map[string]interface{}{"role": "assistant", "content": asString(content[0]["text"])})
				continue
			}
			if len(content) > 0 {
				outMessages = append(outMessages, map[string]interface{}{"role": "assistant", "content": content})
			}
		case "tool":
			callID := strings.TrimSpace(msg.ToolCallID)
			if callID == "" {
				continue
			}
			toolResult := map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": callID,
			}
			if content := claudeToolResultContent(msg); content != nil {
				toolResult["content"] = content
			} else {
				toolResult["content"] = strings.TrimSpace(msg.Content)
			}
			if name := strings.TrimSpace(callNames[callID]); name != "" {
				toolResult["tool_name"] = name
			}
			outMessages = append(outMessages, map[string]interface{}{"role": "user", "content": []map[string]interface{}{toolResult}})
		default:
			content := claudeContentPartsForMessage(msg)
			if len(content) == 0 && strings.TrimSpace(msg.Content) != "" {
				outMessages = append(outMessages, map[string]interface{}{"role": "user", "content": strings.TrimSpace(msg.Content)})
				continue
			}
			if len(content) == 1 && strings.EqualFold(asString(content[0]["type"]), "text") {
				outMessages = append(outMessages, map[string]interface{}{"role": "user", "content": asString(content[0]["text"])})
				continue
			}
			if len(content) > 0 {
				outMessages = append(outMessages, map[string]interface{}{"role": "user", "content": content})
			}
		}
	}
	body := map[string]interface{}{
		"model":    strings.TrimSpace(model),
		"messages": outMessages,
		"stream":   stream,
	}
	if len(systemParts) > 0 {
		system := make([]map[string]interface{}, 0, len(systemParts))
		for _, text := range systemParts {
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			system = append(system, map[string]interface{}{
				"type": "text",
				"text": text,
			})
		}
		body["system"] = system
	}
	if maxTokens, ok := int64FromOption(options, "max_tokens"); ok && maxTokens > 0 {
		body["max_tokens"] = maxTokens
	} else {
		body["max_tokens"] = int64(4096)
	}
	if temperature, ok := float64FromOption(options, "temperature"); ok {
		body["temperature"] = temperature
	}
	if len(tools) > 0 {
		toolDefs := make([]map[string]interface{}, 0, len(tools))
		for _, tool := range tools {
			name := strings.TrimSpace(tool.Function.Name)
			if name == "" {
				name = strings.TrimSpace(tool.Name)
			}
			if name == "" {
				continue
			}
			schema := tool.Function.Parameters
			if len(schema) == 0 {
				schema = tool.Parameters
			}
			if len(schema) == 0 {
				schema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
			}
			toolDefs = append(toolDefs, map[string]interface{}{
				"name":         name,
				"description":  strings.TrimSpace(firstNonEmpty(tool.Function.Description, tool.Description)),
				"input_schema": schema,
			})
		}
		if len(toolDefs) > 0 {
			body["tools"] = toolDefs
			body["tool_choice"] = map[string]interface{}{"type": "auto"}
		}
	}
	if toolChoice := claudeToolChoice(options); len(toolChoice) > 0 {
		body["tool_choice"] = toolChoice
	}
	if thinking, ok := mapOption(options, "thinking"); ok && len(thinking) > 0 {
		body["thinking"] = thinking
	}
	body = enrichClaudeSystemBlocks(body, claudeStrictSystemEnabled(options))
	body = disableClaudeThinkingIfToolChoiceForced(body)
	body = ensureClaudeCacheControl(body)
	body = enforceClaudeCacheControlLimit(body, 4)
	body = normalizeClaudeCacheControlTTL(body)
	body = applyClaudeToolPrefixToBody(body, claudeToolPrefix)
	return body
}

func (p *ClaudeProvider) countTokensRequestBody(messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) map[string]interface{} {
	body := p.requestBody(messages, tools, model, options, false)
	delete(body, "stream")
	delete(body, "max_tokens")
	return body
}

func claudeContentPartsForMessage(msg Message) []map[string]interface{} {
	if len(msg.ContentParts) == 0 {
		return nil
	}
	content := make([]map[string]interface{}, 0, len(msg.ContentParts))
	for _, part := range msg.ContentParts {
		if converted := claudeContentPartFromMessagePart(part); len(converted) > 0 {
			content = append(content, converted)
		}
	}
	return content
}

func claudeTextParts(parts []MessageContentPart) string {
	if len(parts) == 0 {
		return ""
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		switch strings.ToLower(strings.TrimSpace(part.Type)) {
		case "text", "input_text":
			if text := strings.TrimSpace(part.Text); text != "" {
				texts = append(texts, text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(texts, "\n"))
}

func claudeContentPartFromMessagePart(part MessageContentPart) map[string]interface{} {
	switch strings.ToLower(strings.TrimSpace(part.Type)) {
	case "text", "input_text":
		if text := strings.TrimSpace(part.Text); text != "" {
			return map[string]interface{}{"type": "text", "text": text}
		}
	case "image", "input_image":
		return claudeImagePart(part)
	case "file", "input_file":
		return claudeDocumentPart(part)
	}
	return nil
}

func claudeToolResultContent(msg Message) interface{} {
	if len(msg.ContentParts) == 0 {
		return nil
	}
	content := make([]map[string]interface{}, 0, len(msg.ContentParts))
	for _, part := range msg.ContentParts {
		if converted := claudeContentPartFromMessagePart(part); len(converted) > 0 {
			content = append(content, converted)
		}
	}
	if len(content) == 0 {
		return nil
	}
	return content
}

func claudeImagePart(part MessageContentPart) map[string]interface{} {
	imageURL := strings.TrimSpace(part.ImageURL)
	if imageURL == "" {
		return nil
	}
	if strings.HasPrefix(imageURL, "data:") {
		mediaType, data := parseClaudeDataURL(imageURL, "application/octet-stream")
		if data == "" {
			return nil
		}
		return map[string]interface{}{
			"type": "image",
			"source": map[string]interface{}{
				"type":       "base64",
				"media_type": mediaType,
				"data":       data,
			},
		}
	}
	return map[string]interface{}{
		"type": "image",
		"source": map[string]interface{}{
			"type": "url",
			"url":  imageURL,
		},
	}
}

func claudeDocumentPart(part MessageContentPart) map[string]interface{} {
	fileData := strings.TrimSpace(part.FileData)
	if fileData == "" {
		return nil
	}
	mediaType, data := parseClaudeDataURL(fileData, firstNonEmpty(strings.TrimSpace(part.MIMEType), "application/octet-stream"))
	if data == "" {
		return nil
	}
	return map[string]interface{}{
		"type": "document",
		"source": map[string]interface{}{
			"type":       "base64",
			"media_type": mediaType,
			"data":       data,
		},
	}
}

func parseClaudeDataURL(value string, fallbackMediaType string) (string, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallbackMediaType, ""
	}
	if !strings.HasPrefix(value, "data:") {
		return fallbackMediaType, value
	}
	trimmed := strings.TrimPrefix(value, "data:")
	parts := strings.SplitN(trimmed, ";base64,", 2)
	if len(parts) != 2 {
		return fallbackMediaType, ""
	}
	mediaType := strings.TrimSpace(parts[0])
	if mediaType == "" {
		mediaType = fallbackMediaType
	}
	return mediaType, strings.TrimSpace(parts[1])
}

func enrichClaudeSystemBlocks(body map[string]interface{}, strict bool) map[string]interface{} {
	if body == nil {
		return nil
	}
	systemBlocks := buildClaudeSystemBlocks(body["system"], body, strict)
	if len(systemBlocks) == 0 {
		return body
	}
	body["system"] = systemBlocks
	return body
}

func buildClaudeSystemBlocks(system interface{}, body map[string]interface{}, strict bool) []map[string]interface{} {
	userBlocks := make([]map[string]interface{}, 0)
	switch typed := system.(type) {
	case string:
		if text := strings.TrimSpace(typed); text != "" {
			userBlocks = append(userBlocks, map[string]interface{}{
				"type":          "text",
				"text":          text,
				"cache_control": map[string]interface{}{"type": "ephemeral"},
			})
		}
	case []map[string]interface{}:
		for _, item := range typed {
			if strings.HasPrefix(strings.TrimSpace(asString(item["text"])), "x-anthropic-billing-header:") {
				return typed
			}
			clone := map[string]interface{}{}
			for k, v := range item {
				clone[k] = v
			}
			if strings.EqualFold(asString(clone["type"]), "text") && mapFromAny(clone["cache_control"]) == nil {
				if _, exists := clone["cache_control"]; !exists {
					clone["cache_control"] = map[string]interface{}{"type": "ephemeral"}
				}
			}
			userBlocks = append(userBlocks, clone)
		}
	case []interface{}:
		for _, raw := range typed {
			item := mapFromAny(raw)
			if len(item) == 0 {
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(asString(item["text"])), "x-anthropic-billing-header:") {
				return claudeMustMapSlice(typed)
			}
			clone := map[string]interface{}{}
			for k, v := range item {
				clone[k] = v
			}
			if strings.EqualFold(asString(clone["type"]), "text") {
				if _, exists := clone["cache_control"]; !exists {
					clone["cache_control"] = map[string]interface{}{"type": "ephemeral"}
				}
			}
			userBlocks = append(userBlocks, clone)
		}
	}
	systemBlocks := []map[string]interface{}{
		{"type": "text", "text": generateClaudeBillingHeader(body)},
		{"type": "text", "text": "You are a Claude agent, built on Anthropic's Claude Agent SDK."},
	}
	if strict {
		return systemBlocks
	}
	if len(userBlocks) == 0 {
		return nil
	}
	systemBlocks = append(systemBlocks, userBlocks...)
	return systemBlocks
}

func generateClaudeBillingHeader(body map[string]interface{}) string {
	raw, _ := json.Marshal(body)
	sum := sha256.Sum256(raw)
	cch := hex.EncodeToString(sum[:])[:5]
	var buildBytes [2]byte
	if _, err := rand.Read(buildBytes[:]); err != nil {
		return fmt.Sprintf("x-anthropic-billing-header: cc_version=2.1.63.000; cc_entrypoint=cli; cch=%s;", cch)
	}
	buildHash := hex.EncodeToString(buildBytes[:])[:3]
	return fmt.Sprintf("x-anthropic-billing-header: cc_version=2.1.63.%s; cc_entrypoint=cli; cch=%s;", buildHash, cch)
}

func claudeMustMapSlice(items []interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if obj := mapFromAny(item); len(obj) > 0 {
			out = append(out, obj)
		}
	}
	return out
}

func (p *ClaudeProvider) postJSON(ctx context.Context, payload map[string]interface{}, options map[string]interface{}) ([]byte, int, string, error) {
	extraBetas, payload := extractClaudeBetasFromPayload(payload)
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to marshal request: %w", err)
	}
	attempts, err := p.base.authAttempts(ctx)
	if err != nil {
		return nil, 0, "", err
	}
	var lastBody []byte
	var lastStatus int
	var lastType string
	for _, attempt := range attempts {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointFor(p.baseURL(), "/v1/messages"), bytes.NewReader(jsonData))
		if err != nil {
			return nil, 0, "", fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		applyAttemptAuth(req, attempt)
		applyAttemptProviderHeaders(req, attempt, p.base, false)
		applyClaudeCompatHeaders(req, attempt, false)
		applyClaudeBetaHeaders(req, options, extraBetas)
		body, status, ctype, err := p.doJSONAttempt(req, attempt)
		if err != nil {
			return nil, 0, "", err
		}
		reason, retry := classifyOAuthFailure(status, body)
		if !retry {
			p.base.markAttemptSuccess(attempt)
			return body, status, ctype, nil
		}
		lastBody, lastStatus, lastType = body, status, ctype
		if attempt.kind == "oauth" && attempt.session != nil && p.base.oauth != nil {
			p.base.oauth.markExhausted(attempt.session, reason)
			recordProviderOAuthError(p.base.providerName, attempt.session, reason)
		}
		if attempt.kind == "api_key" {
			p.base.markAPIKeyFailure(reason)
		}
	}
	return lastBody, lastStatus, lastType, nil
}

func (p *ClaudeProvider) stream(ctx context.Context, payload map[string]interface{}, options map[string]interface{}, onDelta func(string)) ([]byte, int, string, error) {
	extraBetas, payload := extractClaudeBetasFromPayload(payload)
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to marshal request: %w", err)
	}
	attempts, err := p.base.authAttempts(ctx)
	if err != nil {
		return nil, 0, "", err
	}
	var lastBody []byte
	var lastStatus int
	var lastType string
	for _, attempt := range attempts {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointFor(p.baseURL(), "/v1/messages"), bytes.NewReader(jsonData))
		if err != nil {
			return nil, 0, "", fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		applyAttemptAuth(req, attempt)
		applyAttemptProviderHeaders(req, attempt, p.base, true)
		applyClaudeCompatHeaders(req, attempt, true)
		applyClaudeBetaHeaders(req, options, extraBetas)
		body, status, ctype, quotaHit, err := p.consumeClaudeStream(req, attempt, onDelta)
		if err != nil {
			return nil, 0, "", err
		}
		if !quotaHit {
			p.base.markAttemptSuccess(attempt)
			return body, status, ctype, nil
		}
		lastBody, lastStatus, lastType = body, status, ctype
		if attempt.kind == "oauth" && attempt.session != nil && p.base.oauth != nil {
			reason, _ := classifyOAuthFailure(status, body)
			p.base.oauth.markExhausted(attempt.session, reason)
			recordProviderOAuthError(p.base.providerName, attempt.session, reason)
		}
		if attempt.kind == "api_key" {
			reason, _ := classifyOAuthFailure(status, body)
			p.base.markAPIKeyFailure(reason)
		}
	}
	return lastBody, lastStatus, lastType, nil
}

func (p *ClaudeProvider) countTokens(ctx context.Context, payload map[string]interface{}, options map[string]interface{}) ([]byte, int, string, error) {
	extraBetas, payload := extractClaudeBetasFromPayload(payload)
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to marshal request: %w", err)
	}
	attempts, err := p.base.authAttempts(ctx)
	if err != nil {
		return nil, 0, "", err
	}
	var lastBody []byte
	var lastStatus int
	var lastType string
	for _, attempt := range attempts {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointFor(p.baseURL(), "/v1/messages/count_tokens"), bytes.NewReader(jsonData))
		if err != nil {
			return nil, 0, "", fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		applyAttemptAuth(req, attempt)
		applyAttemptProviderHeaders(req, attempt, p.base, false)
		applyClaudeCompatHeaders(req, attempt, false)
		applyClaudeBetaHeaders(req, options, extraBetas)
		body, status, ctype, err := p.doJSONAttempt(req, attempt)
		if err != nil {
			return nil, 0, "", err
		}
		reason, retry := classifyOAuthFailure(status, body)
		if !retry {
			p.base.markAttemptSuccess(attempt)
			return body, status, ctype, nil
		}
		lastBody, lastStatus, lastType = body, status, ctype
		if attempt.kind == "oauth" && attempt.session != nil && p.base.oauth != nil {
			p.base.oauth.markExhausted(attempt.session, reason)
			recordProviderOAuthError(p.base.providerName, attempt.session, reason)
		}
		if attempt.kind == "api_key" {
			p.base.markAPIKeyFailure(reason)
		}
	}
	return lastBody, lastStatus, lastType, nil
}

func (p *ClaudeProvider) consumeClaudeStream(req *http.Request, attempt authAttempt, onDelta func(string)) ([]byte, int, string, bool, error) {
	client, err := p.base.httpClientForAttempt(attempt)
	if err != nil {
		return nil, 0, "", false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, "", false, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	ctype := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if !strings.Contains(strings.ToLower(ctype), "text/event-stream") {
		body, readErr := readClaudeBody(resp.Body, resp.Header.Get("Content-Encoding"))
		if readErr != nil {
			return nil, resp.StatusCode, ctype, false, fmt.Errorf("failed to read response: %w", readErr)
		}
		return body, resp.StatusCode, ctype, shouldRetryOAuthQuota(resp.StatusCode, body), nil
	}
	decodedBody, err := decodeClaudeResponseBody(resp.Body, resp.Header.Get("Content-Encoding"))
	if err != nil {
		return nil, resp.StatusCode, ctype, false, err
	}
	defer decodedBody.Close()
	scanner := bufio.NewScanner(decodedBody)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	var dataLines []string
	state := &claudeStreamState{}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			if len(dataLines) > 0 {
				payload := strings.Join(dataLines, "\n")
				dataLines = dataLines[:0]
				if strings.TrimSpace(payload) != "" && strings.TrimSpace(payload) != "[DONE]" {
					if delta := state.consume(stripClaudeToolPrefixFromStreamLine([]byte(payload), claudeToolPrefix)); delta != "" {
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
		return nil, resp.StatusCode, ctype, false, fmt.Errorf("failed to read stream: %w", err)
	}
	return state.finalBody(), resp.StatusCode, ctype, false, nil
}

type claudeStreamState struct {
	blocks       map[int]*claudeStreamBlock
	order        []int
	Usage        *UsageInfo
	FinishReason string
}

type claudeStreamBlock struct {
	Index     int
	Type      string
	Text      string
	Tool      *ToolCall
	ArgsRaw   string
	Finalized bool
}

func (s *claudeStreamState) consume(payload []byte) string {
	s.ensureInit()
	var event map[string]interface{}
	if err := json.Unmarshal(payload, &event); err != nil {
		return ""
	}
	switch strings.TrimSpace(asString(event["type"])) {
	case "message_start":
		message := mapFromAny(event["message"])
		usage := mapFromAny(message["usage"])
		if len(usage) > 0 {
			s.mergeUsage(usage)
		}
		if content, ok := claudeMapSlice(message["content"]); ok {
			for idx, item := range content {
				switch strings.ToLower(strings.TrimSpace(asString(item["type"]))) {
				case "text":
					s.mergeText(idx, asString(item["text"]), false)
				case "tool_use":
					name := asString(item["name"])
					args := mapFromAny(item["input"])
					raw, _ := json.Marshal(args)
					block := s.blockAt(idx, "tool_use")
					block.Tool = &ToolCall{
						ID:        asString(item["id"]),
						Name:      name,
						Arguments: args,
						Function: &FunctionCall{
							Name:      name,
							Arguments: string(raw),
						},
					}
					block.ArgsRaw = string(raw)
					block.Finalized = true
				}
			}
		}
	case "content_block_start":
		content := mapFromAny(event["content_block"])
		index := intValue(event["index"])
		switch strings.TrimSpace(asString(content["type"])) {
		case "text":
			return s.mergeText(index, asString(content["text"]), false)
		case "tool_use":
			block := s.blockAt(index, "tool_use")
			if block.Tool == nil {
				block.Tool = &ToolCall{
					ID:   asString(content["id"]),
					Name: asString(content["name"]),
					Function: &FunctionCall{
						Name: asString(content["name"]),
					},
				}
			} else {
				if block.Tool.ID == "" {
					block.Tool.ID = asString(content["id"])
				}
				if block.Tool.Name == "" {
					block.Tool.Name = asString(content["name"])
				}
				if block.Tool.Function == nil {
					block.Tool.Function = &FunctionCall{}
				}
				if block.Tool.Function.Name == "" {
					block.Tool.Function.Name = firstNonEmpty(asString(content["name"]), block.Tool.Name)
				}
			}
			input := mapFromAny(content["input"])
			if len(input) > 0 {
				raw, _ := json.Marshal(input)
				if len(raw) > 0 && raw[len(raw)-1] == '}' {
					raw = raw[:len(raw)-1]
				}
				block.ArgsRaw = string(raw)
				block.Finalized = false
			} else if block.ArgsRaw == "" && !block.Finalized {
				block.ArgsRaw = ""
			}
		}
	case "content_block_delta":
		delta := mapFromAny(event["delta"])
		index := intValue(event["index"])
		switch strings.TrimSpace(asString(delta["type"])) {
		case "text_delta":
			return s.mergeText(index, asString(delta["text"]), true)
		case "input_json_delta":
			block := s.blockAt(index, "tool_use")
			if block.Tool != nil {
				block.ArgsRaw += asString(delta["partial_json"])
			}
		}
	case "content_block_stop":
		index := intValue(event["index"])
		block := s.blockAt(index, "tool_use")
		if block.Tool != nil && !block.Finalized {
			argsRaw := strings.TrimSpace(block.ArgsRaw)
			if argsRaw != "" && !strings.HasSuffix(argsRaw, "}") {
				argsRaw += "}"
			}
			args := map[string]interface{}{}
			if argsRaw != "" {
				_ = json.Unmarshal([]byte(argsRaw), &args)
			}
			block.Tool.Function.Arguments = argsRaw
			block.Tool.Arguments = args
			block.ArgsRaw = argsRaw
			block.Finalized = true
		}
	case "message_delta":
		delta := mapFromAny(event["delta"])
		s.FinishReason = strings.TrimSpace(firstNonEmpty(asString(delta["stop_reason"]), s.FinishReason))
		usage := mapFromAny(event["usage"])
		if len(usage) > 0 {
			s.mergeUsage(usage)
		}
	case "message_stop":
		if s.FinishReason == "" {
			s.FinishReason = "stop"
		}
	}
	return ""
}

func (s *claudeStreamState) ensureInit() {
	if s.blocks == nil {
		s.blocks = map[int]*claudeStreamBlock{}
	}
}

func (s *claudeStreamState) blockAt(index int, typ string) *claudeStreamBlock {
	s.ensureInit()
	block, ok := s.blocks[index]
	if !ok {
		block = &claudeStreamBlock{Index: index, Type: typ}
		s.blocks[index] = block
		s.order = append(s.order, index)
	} else if block.Type == "" {
		block.Type = typ
	}
	return block
}

func (s *claudeStreamState) mergeText(index int, incoming string, isDelta bool) string {
	incoming = asString(incoming)
	if incoming == "" {
		return ""
	}
	block := s.blockAt(index, "text")
	if block.Type == "" {
		block.Type = "text"
	}
	if isDelta {
		if strings.HasSuffix(block.Text, incoming) {
			return ""
		}
		block.Text += incoming
		return incoming
	}
	if block.Text == "" {
		block.Text = incoming
		return incoming
	}
	if strings.HasPrefix(block.Text, incoming) {
		return ""
	}
	if strings.HasPrefix(incoming, block.Text) {
		delta := incoming[len(block.Text):]
		block.Text = incoming
		return delta
	}
	block.Text += incoming
	return incoming
}

func (s *claudeStreamState) mergeUsage(usage map[string]interface{}) {
	if len(usage) == 0 {
		return
	}
	if s.Usage == nil {
		s.Usage = &UsageInfo{}
	}
	if v := intValue(usage["input_tokens"]); v > 0 {
		s.Usage.PromptTokens = v
	}
	if v := intValue(usage["output_tokens"]); v > 0 {
		s.Usage.CompletionTokens = v
	}
	s.Usage.TotalTokens = s.Usage.PromptTokens + s.Usage.CompletionTokens
}

func (s *claudeStreamState) finalBody() []byte {
	s.ensureInit()
	content := make([]map[string]interface{}, 0, len(s.blocks))
	order := append([]int(nil), s.order...)
	sort.Ints(order)
	for _, index := range order {
		block := s.blocks[index]
		if block == nil {
			continue
		}
		switch block.Type {
		case "text":
			if txt := strings.TrimSpace(block.Text); txt != "" {
				content = append(content, map[string]interface{}{"type": "text", "text": txt})
			}
		case "tool_use":
			if block.Tool == nil {
				continue
			}
			input := block.Tool.Arguments
			if len(input) == 0 && block.Tool.Function != nil && strings.TrimSpace(block.Tool.Function.Arguments) != "" {
				_ = json.Unmarshal([]byte(block.Tool.Function.Arguments), &input)
			}
			content = append(content, map[string]interface{}{
				"type":  "tool_use",
				"id":    block.Tool.ID,
				"name":  block.Tool.Name,
				"input": input,
			})
		}
	}
	body := map[string]interface{}{
		"content":     content,
		"stop_reason": firstNonEmpty(strings.TrimSpace(s.FinishReason), "stop"),
	}
	if s.Usage != nil {
		body["usage"] = map[string]interface{}{
			"input_tokens":  s.Usage.PromptTokens,
			"output_tokens": s.Usage.CompletionTokens,
		}
	}
	raw, _ := json.Marshal(body)
	return raw
}

func parseClaudeResponse(body []byte) (*LLMResponse, error) {
	var payload struct {
		Content []struct {
			Type  string                 `json:"type"`
			Text  string                 `json:"text"`
			ID    string                 `json:"id"`
			Name  string                 `json:"name"`
			Input map[string]interface{} `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	resp := &LLMResponse{
		FinishReason: firstNonEmpty(strings.TrimSpace(payload.StopReason), "stop"),
	}
	texts := make([]string, 0)
	for _, item := range payload.Content {
		switch strings.TrimSpace(item.Type) {
		case "text":
			if strings.TrimSpace(item.Text) != "" {
				texts = append(texts, item.Text)
			}
		case "tool_use":
			raw, _ := json.Marshal(item.Input)
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{
				ID:        item.ID,
				Name:      item.Name,
				Arguments: item.Input,
				Function: &FunctionCall{
					Name:      item.Name,
					Arguments: string(raw),
				},
			})
		}
	}
	resp.Content = strings.TrimSpace(strings.Join(texts, "\n"))
	total := payload.Usage.InputTokens + payload.Usage.OutputTokens
	if total > 0 {
		resp.Usage = &UsageInfo{
			PromptTokens:     payload.Usage.InputTokens,
			CompletionTokens: payload.Usage.OutputTokens,
			TotalTokens:      total,
		}
	}
	return resp, nil
}

func (p *ClaudeProvider) doJSONAttempt(req *http.Request, attempt authAttempt) ([]byte, int, string, error) {
	client, err := p.base.httpClientForAttempt(attempt)
	if err != nil {
		return nil, 0, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	body, readErr := readClaudeBody(resp.Body, resp.Header.Get("Content-Encoding"))
	if readErr != nil {
		return nil, resp.StatusCode, strings.TrimSpace(resp.Header.Get("Content-Type")), fmt.Errorf("failed to read response: %w", readErr)
	}
	return body, resp.StatusCode, strings.TrimSpace(resp.Header.Get("Content-Type")), nil
}

func claudeToolChoice(options map[string]interface{}) map[string]interface{} {
	raw, ok := rawOption(options, "tool_choice")
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case string:
		val := strings.TrimSpace(strings.ToLower(typed))
		switch val {
		case "auto", "any":
			return map[string]interface{}{"type": val}
		case "none":
			return nil
		case "required":
			return map[string]interface{}{"type": "any"}
		default:
			if val != "" {
				return map[string]interface{}{"type": "tool", "name": typed}
			}
		}
	case map[string]interface{}:
		switch strings.ToLower(strings.TrimSpace(asString(typed["type"]))) {
		case "none":
			return nil
		case "required":
			return map[string]interface{}{"type": "any"}
		case "auto", "any":
			return map[string]interface{}{"type": strings.ToLower(strings.TrimSpace(asString(typed["type"])))}
		case "function":
			function := mapFromAny(typed["function"])
			name := strings.TrimSpace(asString(function["name"]))
			if name != "" {
				return map[string]interface{}{"type": "tool", "name": name}
			}
		}
		return typed
	}
	return nil
}

func disableClaudeThinkingIfToolChoiceForced(body map[string]interface{}) map[string]interface{} {
	if body == nil {
		return nil
	}
	toolChoice := mapFromAny(body["tool_choice"])
	switch strings.TrimSpace(strings.ToLower(asString(toolChoice["type"]))) {
	case "any", "tool":
		delete(body, "thinking")
		if outputConfig := mapFromAny(body["output_config"]); len(outputConfig) > 0 {
			delete(outputConfig, "effort")
			if len(outputConfig) == 0 {
				delete(body, "output_config")
			} else {
				body["output_config"] = outputConfig
			}
		}
	}
	return body
}

func applyClaudeCompatHeaders(req *http.Request, attempt authAttempt, stream bool) {
	if req == nil {
		return
	}
	req.Header.Set("Anthropic-Version", "2023-06-01")
	req.Header.Set("Anthropic-Dangerous-Direct-Browser-Access", "true")
	req.Header.Set("X-App", "cli")
	req.Header.Set("X-Stainless-Retry-Count", "0")
	req.Header.Set("X-Stainless-Runtime-Version", "v24.3.0")
	req.Header.Set("X-Stainless-Package-Version", "0.74.0")
	req.Header.Set("X-Stainless-Runtime", "node")
	req.Header.Set("X-Stainless-Lang", "js")
	req.Header.Set("X-Stainless-Arch", claudeStainlessArch())
	req.Header.Set("X-Stainless-Os", claudeStainlessOS())
	req.Header.Set("X-Stainless-Timeout", "600")
	req.Header.Set("User-Agent", "claude-cli/2.1.63 (external, cli)")
	req.Header.Set("Connection", "keep-alive")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Accept-Encoding", "identity")
	} else {
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	}
	// Anthropic native base should use x-api-key for api_key mode and Bearer for OAuth.
	if attempt.kind == "api_key" && req.URL != nil && strings.EqualFold(req.URL.Host, "api.anthropic.com") {
		req.Header.Del("Authorization")
		req.Header.Set("x-api-key", strings.TrimSpace(attempt.token))
	} else {
		req.Header.Del("x-api-key")
		if strings.TrimSpace(attempt.token) != "" {
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(attempt.token))
		}
	}
}

func claudeStainlessOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "MacOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	case "freebsd":
		return "FreeBSD"
	default:
		return "Other::" + runtime.GOOS
	}
}

func claudeStainlessArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "arm64":
		return "arm64"
	case "386":
		return "x86"
	default:
		return "other::" + runtime.GOARCH
	}
}

func applyClaudeBetaHeaders(req *http.Request, options map[string]interface{}, extraBetas []string) {
	if req == nil {
		return
	}
	base := strings.TrimSpace(req.Header.Get("Anthropic-Beta"))
	if base == "" {
		base = "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05"
	}
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, item := range strings.Split(base, ",") {
		beta := strings.TrimSpace(item)
		if beta == "" || seen[beta] {
			continue
		}
		seen[beta] = true
		out = append(out, beta)
	}
	for _, key := range []string{"claude_betas", "betas"} {
		values, ok := stringSliceOption(options, key)
		if !ok {
			continue
		}
		for _, beta := range values {
			beta = strings.TrimSpace(beta)
			if beta == "" || seen[beta] {
				continue
			}
			seen[beta] = true
			out = append(out, beta)
		}
	}
	for _, beta := range extraBetas {
		beta = strings.TrimSpace(beta)
		if beta == "" || seen[beta] {
			continue
		}
		seen[beta] = true
		out = append(out, beta)
	}
	if claudeContext1MEnabled(options) && !seen["context-1m-2025-08-07"] {
		out = append(out, "context-1m-2025-08-07")
	}
	req.Header.Set("Anthropic-Beta", strings.Join(out, ","))
}

func claudeStrictSystemEnabled(options map[string]interface{}) bool {
	return claudeBoolOption(options, "claude_strict_system") || claudeBoolOption(options, "cloak_strict_mode")
}

func claudeContext1MEnabled(options map[string]interface{}) bool {
	return claudeBoolOption(options, "claude_1m") || claudeBoolOption(options, "context_1m")
}

func claudeBoolOption(options map[string]interface{}, key string) bool {
	if len(options) == 0 {
		return false
	}
	raw, ok := options[key]
	if !ok {
		return false
	}
	switch typed := raw.(type) {
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

type claudeCompositeReadCloser struct {
	io.Reader
	closers []func() error
}

func (c *claudeCompositeReadCloser) Close() error {
	var firstErr error
	for _, closer := range c.closers {
		if closer == nil {
			continue
		}
		if err := closer(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

type claudePeekableBody struct {
	*bufio.Reader
	closer io.Closer
}

func (p *claudePeekableBody) Close() error {
	return p.closer.Close()
}

func decodeClaudeResponseBody(body io.ReadCloser, contentEncoding string) (io.ReadCloser, error) {
	if body == nil {
		return nil, fmt.Errorf("response body is nil")
	}
	if strings.TrimSpace(contentEncoding) == "" {
		pb := &claudePeekableBody{Reader: bufio.NewReader(body), closer: body}
		magic, peekErr := pb.Peek(4)
		if peekErr == nil || (peekErr == io.EOF && len(magic) >= 2) {
			switch {
			case len(magic) >= 2 && magic[0] == 0x1f && magic[1] == 0x8b:
				gz, err := gzip.NewReader(pb)
				if err != nil {
					_ = pb.Close()
					return nil, err
				}
				return &claudeCompositeReadCloser{Reader: gz, closers: []func() error{gz.Close, pb.Close}}, nil
			case len(magic) >= 4 && magic[0] == 0x28 && magic[1] == 0xb5 && magic[2] == 0x2f && magic[3] == 0xfd:
				decoder, err := zstd.NewReader(pb)
				if err != nil {
					_ = pb.Close()
					return nil, err
				}
				return &claudeCompositeReadCloser{Reader: decoder, closers: []func() error{func() error { decoder.Close(); return nil }, pb.Close}}, nil
			}
		}
		return pb, nil
	}
	for _, raw := range strings.Split(contentEncoding, ",") {
		switch strings.TrimSpace(strings.ToLower(raw)) {
		case "", "identity":
			continue
		case "gzip":
			gz, err := gzip.NewReader(body)
			if err != nil {
				_ = body.Close()
				return nil, err
			}
			return &claudeCompositeReadCloser{Reader: gz, closers: []func() error{gz.Close, body.Close}}, nil
		case "deflate":
			reader := flate.NewReader(body)
			return &claudeCompositeReadCloser{Reader: reader, closers: []func() error{reader.Close, body.Close}}, nil
		case "br":
			return &claudeCompositeReadCloser{Reader: brotli.NewReader(body), closers: []func() error{body.Close}}, nil
		case "zstd":
			decoder, err := zstd.NewReader(body)
			if err != nil {
				_ = body.Close()
				return nil, err
			}
			return &claudeCompositeReadCloser{Reader: decoder, closers: []func() error{func() error { decoder.Close(); return nil }, body.Close}}, nil
		}
	}
	return body, nil
}

func readClaudeBody(body io.ReadCloser, contentEncoding string) ([]byte, error) {
	decoded, err := decodeClaudeResponseBody(body, contentEncoding)
	if err != nil {
		return nil, err
	}
	defer decoded.Close()
	return io.ReadAll(decoded)
}

func ensureClaudeCacheControl(body map[string]interface{}) map[string]interface{} {
	if body == nil {
		return nil
	}
	injectClaudeToolsCacheControl(body)
	injectClaudeSystemCacheControl(body)
	injectClaudeMessagesCacheControl(body)
	return body
}

func injectClaudeToolsCacheControl(body map[string]interface{}) {
	tools, ok := claudeMapSlice(body["tools"])
	if !ok || len(tools) == 0 {
		return
	}
	for _, tool := range tools {
		if _, exists := tool["cache_control"]; exists {
			body["tools"] = tools
			return
		}
	}
	tools[len(tools)-1]["cache_control"] = map[string]interface{}{"type": "ephemeral"}
	body["tools"] = tools
}

func injectClaudeSystemCacheControl(body map[string]interface{}) {
	switch typed := body["system"].(type) {
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return
		}
		body["system"] = []map[string]interface{}{{
			"type":          "text",
			"text":          text,
			"cache_control": map[string]interface{}{"type": "ephemeral"},
		}}
	case []map[string]interface{}:
		for _, item := range typed {
			if _, exists := item["cache_control"]; exists {
				return
			}
		}
		if len(typed) > 0 {
			typed[len(typed)-1]["cache_control"] = map[string]interface{}{"type": "ephemeral"}
			body["system"] = typed
		}
	case []interface{}:
		if items, ok := claudeMapSlice(typed); ok && len(items) > 0 {
			for _, item := range items {
				if _, exists := item["cache_control"]; exists {
					body["system"] = items
					return
				}
			}
			items[len(items)-1]["cache_control"] = map[string]interface{}{"type": "ephemeral"}
			body["system"] = items
		}
	}
}

func injectClaudeMessagesCacheControl(body map[string]interface{}) {
	messages, ok := claudeMapSlice(body["messages"])
	if !ok || len(messages) == 0 {
		return
	}
	for _, msg := range messages {
		content, ok := claudeMapSlice(msg["content"])
		if !ok {
			continue
		}
		for _, item := range content {
			if _, exists := item["cache_control"]; exists {
				body["messages"] = messages
				return
			}
		}
	}
	userIdx := make([]int, 0)
	for idx, msg := range messages {
		if strings.EqualFold(asString(msg["role"]), "user") {
			userIdx = append(userIdx, idx)
		}
	}
	if len(userIdx) < 2 {
		body["messages"] = messages
		return
	}
	target := messages[userIdx[len(userIdx)-2]]
	content, ok := claudeMapSlice(target["content"])
	if ok && len(content) > 0 {
		content[len(content)-1]["cache_control"] = map[string]interface{}{"type": "ephemeral"}
		target["content"] = content
	}
	body["messages"] = messages
}

func enforceClaudeCacheControlLimit(body map[string]interface{}, maxBlocks int) map[string]interface{} {
	if body == nil || maxBlocks <= 0 {
		return body
	}
	blocks := claudeCacheBlocks(body)
	if len(blocks) <= maxBlocks {
		return body
	}
	excess := len(blocks) - maxBlocks
	system, _ := claudeMapSlice(body["system"])
	tools, _ := claudeMapSlice(body["tools"])
	messages, _ := claudeMapSlice(body["messages"])

	excess = stripClaudeCacheControlExceptLast(system, excess)
	excess = stripClaudeCacheControlExceptLast(tools, excess)
	excess = stripClaudeMessageCacheControl(messages, excess)
	excess = stripClaudeAllCacheControl(system, excess)
	excess = stripClaudeAllCacheControl(tools, excess)
	return body
}

func normalizeClaudeCacheControlTTL(body map[string]interface{}) map[string]interface{} {
	if body == nil {
		return nil
	}
	seenDefaultTTL := false
	for _, item := range claudeCacheBlocks(body) {
		cc := mapFromAny(item["cache_control"])
		if strings.TrimSpace(asString(cc["ttl"])) == "1h" {
			if seenDefaultTTL {
				delete(cc, "ttl")
				item["cache_control"] = cc
			}
			continue
		}
		seenDefaultTTL = true
	}
	return body
}

func claudeCacheBlocks(body map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0)
	if tools, ok := claudeMapSlice(body["tools"]); ok {
		for _, item := range tools {
			if _, exists := item["cache_control"]; exists {
				out = append(out, item)
			}
		}
	}
	switch typed := body["system"].(type) {
	case []map[string]interface{}:
		for _, item := range typed {
			if _, exists := item["cache_control"]; exists {
				out = append(out, item)
			}
		}
	case []interface{}:
		if items, ok := claudeMapSlice(typed); ok {
			for _, item := range items {
				if _, exists := item["cache_control"]; exists {
					out = append(out, item)
				}
			}
		}
	}
	if messages, ok := claudeMapSlice(body["messages"]); ok {
		for _, msg := range messages {
			if content, ok := claudeMapSlice(msg["content"]); ok {
				for _, item := range content {
					if _, exists := item["cache_control"]; exists {
						out = append(out, item)
					}
				}
			}
		}
	}
	return out
}

func applyClaudeToolPrefixToBody(body map[string]interface{}, prefix string) map[string]interface{} {
	if prefix == "" || body == nil {
		return body
	}
	builtinTools := map[string]bool{
		"web_search":     true,
		"code_execution": true,
		"text_editor":    true,
		"computer":       true,
	}
	if tools, ok := claudeMapSlice(body["tools"]); ok {
		for _, tool := range tools {
			name := strings.TrimSpace(asString(tool["name"]))
			if typ := strings.TrimSpace(asString(tool["type"])); typ != "" {
				if name != "" {
					builtinTools[name] = true
				}
				continue
			}
			if name != "" && !strings.HasPrefix(name, prefix) {
				tool["name"] = prefix + name
			}
		}
		body["tools"] = tools
	}
	if toolChoice := mapFromAny(body["tool_choice"]); strings.EqualFold(asString(toolChoice["type"]), "tool") {
		name := strings.TrimSpace(asString(toolChoice["name"]))
		if name != "" && !strings.HasPrefix(name, prefix) && !builtinTools[name] {
			toolChoice["name"] = prefix + name
			body["tool_choice"] = toolChoice
		}
	}
	if messages, ok := claudeMapSlice(body["messages"]); ok {
		for _, msg := range messages {
			if content, ok := claudeMapSlice(msg["content"]); ok {
				for _, item := range content {
					switch strings.ToLower(strings.TrimSpace(asString(item["type"]))) {
					case "tool_use":
						name := strings.TrimSpace(asString(item["name"]))
						if name != "" && !strings.HasPrefix(name, prefix) && !builtinTools[name] {
							item["name"] = prefix + name
						}
					case "tool_reference":
						name := strings.TrimSpace(asString(item["tool_name"]))
						if name != "" && !strings.HasPrefix(name, prefix) && !builtinTools[name] {
							item["tool_name"] = prefix + name
						}
					case "tool_result":
						if nested, ok := claudeMapSlice(item["content"]); ok {
							for _, nestedItem := range nested {
								if strings.EqualFold(asString(nestedItem["type"]), "tool_reference") {
									name := strings.TrimSpace(asString(nestedItem["tool_name"]))
									if name != "" && !strings.HasPrefix(name, prefix) && !builtinTools[name] {
										nestedItem["tool_name"] = prefix + name
									}
								}
							}
							item["content"] = nested
						}
					}
				}
				msg["content"] = content
			}
		}
		body["messages"] = messages
	}
	return body
}

func stripClaudeToolPrefixFromResponse(body []byte, prefix string) []byte {
	if prefix == "" || len(body) == 0 {
		return body
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	if content, ok := claudeMapSlice(payload["content"]); ok {
		for _, item := range content {
			switch strings.ToLower(strings.TrimSpace(asString(item["type"]))) {
			case "tool_use":
				name := strings.TrimSpace(asString(item["name"]))
				if strings.HasPrefix(name, prefix) {
					item["name"] = strings.TrimPrefix(name, prefix)
				}
			case "tool_reference":
				name := strings.TrimSpace(asString(item["tool_name"]))
				if strings.HasPrefix(name, prefix) {
					item["tool_name"] = strings.TrimPrefix(name, prefix)
				}
			case "tool_result":
				if nested, ok := claudeMapSlice(item["content"]); ok {
					for _, nestedItem := range nested {
						if strings.EqualFold(asString(nestedItem["type"]), "tool_reference") {
							name := strings.TrimSpace(asString(nestedItem["tool_name"]))
							if strings.HasPrefix(name, prefix) {
								nestedItem["tool_name"] = strings.TrimPrefix(name, prefix)
							}
						}
					}
					item["content"] = nested
				}
			}
		}
		payload["content"] = content
	}
	updated, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return updated
}

func stripClaudeToolPrefixFromStreamLine(line []byte, prefix string) []byte {
	if prefix == "" || len(line) == 0 {
		return line
	}
	trimmed := bytes.TrimSpace(line)
	hasDataPrefix := bytes.HasPrefix(trimmed, []byte("data:"))
	payloadBytes := trimmed
	if hasDataPrefix {
		payloadBytes = bytes.TrimSpace(bytes.TrimPrefix(trimmed, []byte("data:")))
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return line
	}
	contentBlock := mapFromAny(payload["content_block"])
	switch strings.ToLower(strings.TrimSpace(asString(contentBlock["type"]))) {
	case "tool_use":
		name := strings.TrimSpace(asString(contentBlock["name"]))
		if strings.HasPrefix(name, prefix) {
			contentBlock["name"] = strings.TrimPrefix(name, prefix)
			payload["content_block"] = contentBlock
		}
	case "tool_reference":
		name := strings.TrimSpace(asString(contentBlock["tool_name"]))
		if strings.HasPrefix(name, prefix) {
			contentBlock["tool_name"] = strings.TrimPrefix(name, prefix)
			payload["content_block"] = contentBlock
		}
	}
	updated, err := json.Marshal(payload)
	if err != nil {
		return line
	}
	if hasDataPrefix {
		return append([]byte("data: "), updated...)
	}
	return updated
}

func claudeMapSlice(value interface{}) ([]map[string]interface{}, bool) {
	switch typed := value.(type) {
	case []map[string]interface{}:
		return typed, true
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(typed))
		for _, item := range typed {
			obj := mapFromAny(item)
			if len(obj) > 0 {
				out = append(out, obj)
			}
		}
		return out, len(out) > 0
	default:
		return nil, false
	}
}

func extractClaudeBetasFromPayload(payload map[string]interface{}) ([]string, map[string]interface{}) {
	if payload == nil {
		return nil, nil
	}
	out := make([]string, 0)
	for _, key := range []string{"betas", "claude_betas"} {
		values, ok := stringSliceOption(payload, key)
		if ok {
			out = append(out, values...)
			delete(payload, key)
			continue
		}
		if raw, exists := payload[key]; exists {
			if beta := strings.TrimSpace(asString(raw)); beta != "" {
				out = append(out, beta)
			}
			delete(payload, key)
		}
	}
	return out, payload
}

func stripClaudeCacheControlExceptLast(items []map[string]interface{}, excess int) int {
	if excess <= 0 || len(items) == 0 {
		return excess
	}
	last := -1
	for idx := len(items) - 1; idx >= 0; idx-- {
		if _, exists := items[idx]["cache_control"]; exists {
			last = idx
			break
		}
	}
	for idx := 0; idx < len(items) && excess > 0; idx++ {
		if idx == last {
			continue
		}
		if _, exists := items[idx]["cache_control"]; exists {
			delete(items[idx], "cache_control")
			excess--
		}
	}
	return excess
}

func stripClaudeAllCacheControl(items []map[string]interface{}, excess int) int {
	if excess <= 0 {
		return excess
	}
	for _, item := range items {
		if excess <= 0 {
			return excess
		}
		if _, exists := item["cache_control"]; exists {
			delete(item, "cache_control")
			excess--
		}
	}
	return excess
}

func stripClaudeMessageCacheControl(messages []map[string]interface{}, excess int) int {
	if excess <= 0 {
		return excess
	}
	for _, msg := range messages {
		content, ok := claudeMapSlice(msg["content"])
		if !ok {
			continue
		}
		for _, item := range content {
			if excess <= 0 {
				return excess
			}
			if _, exists := item["cache_control"]; exists {
				delete(item, "cache_control")
				excess--
			}
		}
		msg["content"] = content
	}
	return excess
}
