package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type CodexProvider struct {
	base      *HTTPProvider
	sessionMu sync.Mutex
	sessions  map[string]*codexExecutionSession
}

type codexPromptCacheEntry struct {
	ID     string
	Expire time.Time
}

type codexExecutionSession struct {
	mu    sync.Mutex
	reqMu sync.Mutex
	conn  *websocket.Conn
	wsURL string
}

const (
	codexResponsesWebsocketBetaHeaderValue = "responses_websockets=2026-02-06"
	codexResponsesWebsocketHandshakeTO     = 30 * time.Second
	codexResponsesWebsocketIdleTimeout     = 5 * time.Minute
)

var codexPromptCacheStore = struct {
	mu    sync.Mutex
	items map[string]codexPromptCacheEntry
}{items: map[string]codexPromptCacheEntry{}}

func NewCodexProvider(providerName, apiKey, apiBase, defaultModel string, supportsResponsesCompact bool, authMode string, timeout time.Duration, oauth *oauthManager) *CodexProvider {
	return &CodexProvider{
		base: NewHTTPProvider(providerName, apiKey, apiBase, defaultModel, supportsResponsesCompact, authMode, timeout, oauth),
	}
}

func (p *CodexProvider) GetDefaultModel() string {
	if p == nil || p.base == nil {
		return ""
	}
	return p.base.GetDefaultModel()
}

func (p *CodexProvider) SupportsResponsesCompact() bool {
	return p != nil && p.base != nil && p.base.SupportsResponsesCompact()
}

func (p *CodexProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	if p == nil || p.base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	body, statusCode, contentType, err := p.postWebsocketStream(ctx, endpointFor(p.codexCompatBase(), "/responses"), p.requestBody(messages, tools, model, options, false, true), options, nil)
	if err != nil {
		body, statusCode, contentType, err = p.postJSONStream(ctx, endpointFor(p.codexCompatBase(), "/responses"), p.requestBody(messages, tools, model, options, false, false), nil)
	}
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", statusCode, contentType, previewResponseBody(body))
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("API error (status %d, content-type %q): non-JSON response: %s", statusCode, contentType, previewResponseBody(body))
	}
	return parseResponsesAPIResponse(body)
}

func (p *CodexProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onDelta func(string)) (*LLMResponse, error) {
	if p == nil || p.base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	if onDelta == nil {
		onDelta = func(string) {}
	}
	body, statusCode, contentType, err := p.postWebsocketStream(ctx, endpointFor(p.codexCompatBase(), "/responses"), p.requestBody(messages, tools, model, options, true, true), options, onDelta)
	if err != nil {
		body, statusCode, contentType, err = p.postJSONStream(ctx, endpointFor(p.codexCompatBase(), "/responses"), p.requestBody(messages, tools, model, options, true, false), func(event string) {
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
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", statusCode, contentType, previewResponseBody(body))
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("API error (status %d, content-type %q): non-JSON response: %s", statusCode, contentType, previewResponseBody(body))
	}
	return parseResponsesAPIResponse(body)
}

func (p *CodexProvider) BuildSummaryViaResponsesCompact(ctx context.Context, model string, existingSummary string, messages []Message, maxSummaryChars int) (string, error) {
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
	compactBody, statusCode, contentType, err := p.base.postJSON(ctx, endpointFor(p.codexCompatBase(), "/responses/compact"), compactReq)
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
	resp, err := p.Chat(ctx, []Message{{Role: "user", Content: summaryPrompt}}, nil, model, nil)
	if err != nil {
		return "", fmt.Errorf("responses summary request failed: %w", err)
	}
	summary := strings.TrimSpace(resp.Content)
	if summary == "" {
		return "", fmt.Errorf("empty summary after responses compact")
	}
	if maxSummaryChars > 0 && len(summary) > maxSummaryChars {
		summary = summary[:maxSummaryChars]
	}
	return summary, nil
}

func (p *CodexProvider) requestBody(messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, stream bool, preservePreviousResponseID bool) map[string]interface{} {
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
	if stream {
		if streamOpts, ok := mapOption(options, "responses_stream_options"); ok && len(streamOpts) > 0 {
			requestBody["stream_options"] = streamOpts
		}
	}
	return normalizeCodexRequestBody(requestBody, preservePreviousResponseID)
}

func (p *CodexProvider) codexCompatBase() string {
	if p == nil || p.base == nil {
		return codexCompatBaseURL
	}
	base := strings.ToLower(strings.TrimSpace(p.base.apiBase))
	if strings.Contains(base, "chatgpt.com/backend-api/codex") {
		return normalizeAPIBase(p.base.apiBase)
	}
	if base != "" && !strings.Contains(base, "api.openai.com") {
		return normalizeAPIBase(p.base.apiBase)
	}
	return codexCompatBaseURL
}

func normalizeCodexRequestBody(requestBody map[string]interface{}, preservePreviousResponseID bool) map[string]interface{} {
	if requestBody == nil {
		requestBody = map[string]interface{}{}
	}
	requestBody["stream"] = true
	requestBody["store"] = false
	requestBody["parallel_tool_calls"] = true
	if _, ok := requestBody["instructions"]; !ok {
		requestBody["instructions"] = ""
	}
	include := appendCodexInclude(nil, requestBody["include"])
	requestBody["include"] = include
	delete(requestBody, "max_output_tokens")
	delete(requestBody, "max_completion_tokens")
	delete(requestBody, "temperature")
	delete(requestBody, "top_p")
	delete(requestBody, "truncation")
	delete(requestBody, "user")
	if !preservePreviousResponseID {
		delete(requestBody, "previous_response_id")
	}
	delete(requestBody, "prompt_cache_retention")
	delete(requestBody, "safety_identifier")
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

func appendCodexInclude(dst []string, raw interface{}) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 2)
	appendOne := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	for _, v := range dst {
		appendOne(v)
	}
	switch vals := raw.(type) {
	case []string:
		for _, v := range vals {
			appendOne(v)
		}
	case []interface{}:
		for _, v := range vals {
			appendOne(fmt.Sprintf("%v", v))
		}
	case string:
		appendOne(vals)
	}
	appendOne("reasoning.encrypted_content")
	return out
}

func (p *CodexProvider) postJSONStream(ctx context.Context, endpoint string, payload map[string]interface{}, onEvent func(string)) ([]byte, int, string, error) {
	attempts, err := p.base.authAttempts(ctx)
	if err != nil {
		return nil, 0, "", err
	}
	var lastBody []byte
	var lastStatus int
	var lastType string
	for _, attempt := range attempts {
		attemptPayload := codexPayloadForAttempt(payload, attempt)
		jsonData, err := json.Marshal(attemptPayload)
		if err != nil {
			return nil, 0, "", fmt.Errorf("failed to marshal request: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonData))
		if err != nil {
			return nil, 0, "", fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		applyAttemptAuth(req, attempt)
		applyAttemptProviderHeaders(req, attempt, p.base, true)
		applyCodexCacheHeaders(req, attemptPayload)

		body, status, ctype, quotaHit, err := p.doStreamAttempt(req, attempt, onEvent)
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

func codexPayloadForAttempt(payload map[string]interface{}, attempt authAttempt) map[string]interface{} {
	if payload == nil {
		return nil
	}
	out := cloneCodexMap(payload)
	cacheKey, hasCacheKey := out["prompt_cache_key"]
	if hasCacheKey && strings.TrimSpace(fmt.Sprintf("%v", cacheKey)) != "" {
		return out
	}
	if userCacheKey := codexPromptCacheKeyForUser(out); userCacheKey != "" {
		out["prompt_cache_key"] = userCacheKey
		return out
	}
	if attempt.kind == "api_key" {
		token := strings.TrimSpace(attempt.token)
		if token != "" {
			out["prompt_cache_key"] = uuid.NewSHA1(uuid.NameSpaceOID, []byte("cli-proxy-api:codex:prompt-cache:"+token)).String()
		}
	}
	return out
}

func codexPromptCacheKeyForUser(payload map[string]interface{}) string {
	metadata := mapFromAny(payload["metadata"])
	userID := strings.TrimSpace(asString(metadata["user_id"]))
	model := strings.TrimSpace(asString(payload["model"]))
	if userID == "" || model == "" {
		return ""
	}
	key := model + "-" + userID
	now := time.Now()
	codexPromptCacheStore.mu.Lock()
	defer codexPromptCacheStore.mu.Unlock()
	if entry, ok := codexPromptCacheStore.items[key]; ok && entry.ID != "" && entry.Expire.After(now) {
		return entry.ID
	}
	entry := codexPromptCacheEntry{
		ID:     uuid.New().String(),
		Expire: now.Add(time.Hour),
	}
	codexPromptCacheStore.items[key] = entry
	return entry.ID
}

func cloneCodexMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	out := make(map[string]interface{}, len(src))
	for k, v := range src {
		out[k] = cloneCodexValue(v)
	}
	return out
}

func cloneCodexValue(v interface{}) interface{} {
	switch typed := v.(type) {
	case map[string]interface{}:
		return cloneCodexMap(typed)
	case []map[string]interface{}:
		out := make([]map[string]interface{}, len(typed))
		for i := range typed {
			out[i] = cloneCodexMap(typed[i])
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i := range typed {
			out[i] = cloneCodexValue(typed[i])
		}
		return out
	default:
		return v
	}
}

func applyCodexCacheHeaders(req *http.Request, payload map[string]interface{}) {
	if req == nil || payload == nil {
		return
	}
	key := strings.TrimSpace(fmt.Sprintf("%v", payload["prompt_cache_key"]))
	if key == "" {
		return
	}
	req.Header.Set("Conversation_id", key)
	req.Header.Set("Session_id", key)
}

func (p *CodexProvider) doStreamAttempt(req *http.Request, attempt authAttempt, onEvent func(string)) ([]byte, int, string, bool, error) {
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
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, resp.StatusCode, ctype, false, fmt.Errorf("failed to read response: %w", readErr)
		}
		return body, resp.StatusCode, ctype, shouldRetryOAuthQuota(resp.StatusCode, body), nil
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	var dataLines []string
	var finalJSON []byte
	completed := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			if len(dataLines) == 0 {
				continue
			}
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
					completed = true
					if respObj, ok := obj["response"]; ok {
						if b, err := json.Marshal(respObj); err == nil {
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
		return nil, resp.StatusCode, ctype, false, fmt.Errorf("failed to read stream: %w", err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && !completed {
		return nil, resp.StatusCode, ctype, false, fmt.Errorf("stream error: stream disconnected before completion: stream closed before response.completed")
	}
	if len(finalJSON) == 0 {
		finalJSON = []byte("{}")
	}
	return finalJSON, resp.StatusCode, ctype, false, nil
}

func (p *CodexProvider) postWebsocketStream(ctx context.Context, endpoint string, payload map[string]interface{}, options map[string]interface{}, onDelta func(string)) ([]byte, int, string, error) {
	attempts, err := p.base.authAttempts(ctx)
	if err != nil {
		return nil, 0, "", err
	}
	var lastBody []byte
	var lastStatus int
	var lastType string
	var lastErr error
	for _, attempt := range attempts {
		body, status, ctype, err := p.doWebsocketAttempt(ctx, endpoint, payload, attempt, options, onDelta)
		if err == nil {
			p.base.markAttemptSuccess(attempt)
			return body, status, ctype, nil
		}
		lastBody, lastStatus, lastType = body, status, ctype
		p.handleAttemptFailure(attempt, status, body)
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("websocket unavailable")
	}
	return lastBody, lastStatus, lastType, lastErr
}

func (p *CodexProvider) handleAttemptFailure(attempt authAttempt, status int, body []byte) {
	reason, retry := classifyOAuthFailure(status, body)
	if !retry {
		return
	}
	if attempt.kind == "oauth" && attempt.session != nil && p.base != nil && p.base.oauth != nil {
		p.base.oauth.markExhausted(attempt.session, reason)
		recordProviderOAuthError(p.base.providerName, attempt.session, reason)
	}
	if attempt.kind == "api_key" && p.base != nil {
		p.base.markAPIKeyFailure(reason)
	}
}

func (p *CodexProvider) doWebsocketAttempt(ctx context.Context, endpoint string, payload map[string]interface{}, attempt authAttempt, options map[string]interface{}, onDelta func(string)) ([]byte, int, string, error) {
	wsURL, err := buildCodexResponsesWebsocketURL(endpoint)
	if err != nil {
		return nil, 0, "", err
	}
	attemptPayload := codexPayloadForAttempt(payload, attempt)
	wsBody, err := json.Marshal(buildCodexWebsocketRequestBody(attemptPayload))
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to marshal websocket request: %w", err)
	}
	headers := applyCodexWebsocketHeaders(http.Header{}, attempt, options)
	applyCodexCacheHeadersToHeader(headers, attemptPayload)

	session := p.getExecutionSession(codexExecutionSessionID(options))
	if session != nil {
		session.reqMu.Lock()
		defer session.reqMu.Unlock()
	}
	conn, status, ctype, cleanup, err := p.prepareWebsocketConn(ctx, session, wsURL, headers, attempt)
	if err != nil {
		return nil, status, ctype, err
	}
	if cleanup != nil {
		defer cleanup()
	}
	if err := conn.WriteMessage(websocket.TextMessage, wsBody); err != nil {
		if session != nil {
			p.invalidateExecutionSession(session, conn)
			conn, status, ctype, cleanup, err = p.prepareWebsocketConn(ctx, session, wsURL, headers, attempt)
			if err != nil {
				return nil, status, ctype, err
			}
			if cleanup != nil {
				defer cleanup()
			}
			if err := conn.WriteMessage(websocket.TextMessage, wsBody); err != nil {
				p.invalidateExecutionSession(session, conn)
				return nil, 0, "", err
			}
		} else {
			return nil, 0, "", err
		}
	}
	for {
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			p.invalidateExecutionSession(session, conn)
			return nil, http.StatusOK, "application/json", err
		}
		if msgType != websocket.TextMessage {
			continue
		}
		msg = bytes.TrimSpace(msg)
		if len(msg) == 0 {
			continue
		}
		if wsErr, status, _, ok := parseCodexWebsocketError(msg); ok {
			p.invalidateExecutionSession(session, conn)
			return msg, status, "application/json", wsErr
		}
		msg = normalizeCodexWebsocketCompletion(msg)
		var event map[string]interface{}
		if err := json.Unmarshal(msg, &event); err != nil {
			continue
		}
		switch strings.TrimSpace(fmt.Sprintf("%v", event["type"])) {
		case "response.output_text.delta":
			if d := strings.TrimSpace(fmt.Sprintf("%v", event["delta"])); d != "" {
				onDelta(d)
			}
		case "response.completed":
			if respObj, ok := event["response"]; ok {
				b, _ := json.Marshal(respObj)
				return b, http.StatusOK, "application/json", nil
			}
			return msg, http.StatusOK, "application/json", nil
		}
	}
}

func codexExecutionSessionID(options map[string]interface{}) string {
	if value, ok := stringOption(options, "codex_execution_session"); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func (p *CodexProvider) getExecutionSession(id string) *codexExecutionSession {
	id = strings.TrimSpace(id)
	if p == nil || id == "" {
		return nil
	}
	p.sessionMu.Lock()
	defer p.sessionMu.Unlock()
	if p.sessions == nil {
		p.sessions = map[string]*codexExecutionSession{}
	}
	if sess, ok := p.sessions[id]; ok && sess != nil {
		return sess
	}
	sess := &codexExecutionSession{}
	p.sessions[id] = sess
	return sess
}

func (p *CodexProvider) prepareWebsocketConn(ctx context.Context, session *codexExecutionSession, wsURL string, headers http.Header, attempt authAttempt) (*websocket.Conn, int, string, func(), error) {
	if session == nil {
		conn, status, ctype, err := p.dialWebsocket(ctx, wsURL, headers, attempt)
		if err != nil {
			return nil, status, ctype, nil, err
		}
		return conn, status, ctype, func() { _ = conn.Close() }, nil
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	if session.conn != nil && session.wsURL == wsURL {
		return session.conn, http.StatusOK, "application/json", nil, nil
	}
	if session.conn != nil {
		_ = session.conn.Close()
		session.conn = nil
	}
	conn, status, ctype, err := p.dialWebsocket(ctx, wsURL, headers, attempt)
	if err != nil {
		return nil, status, ctype, nil, err
	}
	session.conn = conn
	session.wsURL = wsURL
	return conn, status, ctype, nil, nil
}

func (p *CodexProvider) invalidateExecutionSession(session *codexExecutionSession, conn *websocket.Conn) {
	if session == nil || conn == nil {
		return
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.conn == conn {
		_ = session.conn.Close()
		session.conn = nil
		session.wsURL = ""
	}
}

func (p *CodexProvider) CloseExecutionSession(sessionID string) {
	if p == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	p.sessionMu.Lock()
	session := p.sessions[sessionID]
	delete(p.sessions, sessionID)
	p.sessionMu.Unlock()
	if session == nil {
		return
	}
	session.mu.Lock()
	conn := session.conn
	session.conn = nil
	session.wsURL = ""
	session.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (p *CodexProvider) dialWebsocket(ctx context.Context, wsURL string, headers http.Header, attempt authAttempt) (*websocket.Conn, int, string, error) {
	conn, resp, err := p.websocketDialer(attempt).DialContext(ctx, wsURL, headers)
	if err != nil {
		status := 0
		ctype := ""
		if resp != nil {
			status = resp.StatusCode
			ctype = strings.TrimSpace(resp.Header.Get("Content-Type"))
		}
		if resp != nil && resp.Body != nil {
			_, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
		}
		return nil, status, ctype, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(codexResponsesWebsocketIdleTimeout))
	conn.EnableWriteCompression(false)
	return conn, http.StatusOK, "application/json", nil
}

func (p *CodexProvider) websocketDialer(attempt authAttempt) *websocket.Dialer {
	dialer := &websocket.Dialer{
		HandshakeTimeout:  codexResponsesWebsocketHandshakeTO,
		EnableCompression: true,
		Proxy:             http.ProxyFromEnvironment,
	}
	proxyRaw := ""
	if attempt.session != nil {
		proxyRaw = strings.TrimSpace(attempt.session.NetworkProxy)
	}
	if proxyRaw == "" {
		return dialer
	}
	parsed, err := url.Parse(proxyRaw)
	if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		dialer.Proxy = http.ProxyURL(parsed)
		return dialer
	}
	dialContext, err := proxyDialContext(proxyRaw)
	if err == nil {
		dialer.Proxy = nil
		dialer.NetDialContext = dialContext
	}
	return dialer
}

func buildCodexWebsocketRequestBody(body map[string]interface{}) map[string]interface{} {
	if body == nil {
		return nil
	}
	out := cloneCodexMap(body)
	out["type"] = "response.create"
	return out
}

func buildCodexResponsesWebsocketURL(httpURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(httpURL))
	if err != nil {
		return "", err
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	}
	return parsed.String(), nil
}

func applyCodexWebsocketHeaders(headers http.Header, attempt authAttempt, options map[string]interface{}) http.Header {
	if headers == nil {
		headers = http.Header{}
	}
	if token := strings.TrimSpace(attempt.token); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	headers.Set("x-codex-beta-features", "")
	headers.Set("x-codex-turn-state", codexHeaderOption(options, "codex_turn_state", "turn_state"))
	headers.Set("x-codex-turn-metadata", codexHeaderOption(options, "codex_turn_metadata", "turn_metadata"))
	headers.Set("x-responsesapi-include-timing-metrics", "")
	headers.Set("Version", codexClientVersion)
	betaHeader := strings.TrimSpace(headers.Get("OpenAI-Beta"))
	if betaHeader == "" || !strings.Contains(betaHeader, "responses_websockets=") {
		betaHeader = codexResponsesWebsocketBetaHeaderValue
	}
	headers.Set("OpenAI-Beta", betaHeader)
	if strings.TrimSpace(headers.Get("Session_id")) == "" {
		headers.Set("Session_id", randomSessionID())
	}
	headers.Set("User-Agent", codexCompatUserAgent)
	if attempt.kind != "api_key" {
		headers.Set("Originator", "codex_cli_rs")
		if attempt.session != nil {
			accountID := firstNonEmpty(
				strings.TrimSpace(attempt.session.AccountID),
				strings.TrimSpace(asString(attempt.session.Token["account_id"])),
				strings.TrimSpace(asString(attempt.session.Token["account-id"])),
			)
			if accountID != "" {
				headers.Set("Chatgpt-Account-Id", accountID)
			}
		}
	}
	return headers
}

func codexHeaderOption(options map[string]interface{}, directKey, streamKey string) string {
	if value, ok := stringOption(options, directKey); ok {
		return strings.TrimSpace(value)
	}
	streamOpts, ok := mapOption(options, "responses_stream_options")
	if !ok {
		return ""
	}
	value := strings.TrimSpace(asString(streamOpts[streamKey]))
	return value
}

func applyCodexCacheHeadersToHeader(headers http.Header, payload map[string]interface{}) {
	if headers == nil || payload == nil {
		return
	}
	key := strings.TrimSpace(fmt.Sprintf("%v", payload["prompt_cache_key"]))
	if key == "" {
		return
	}
	headers.Set("Conversation_id", key)
	headers.Set("Session_id", key)
}

func normalizeCodexWebsocketCompletion(payload []byte) []byte {
	root := mustJSONMap(payload)
	if strings.TrimSpace(asString(root["type"])) == "response.done" {
		updated, err := json.Marshal(map[string]interface{}{
			"type":     "response.completed",
			"response": root["response"],
		})
		if err == nil {
			return updated
		}
	}
	return payload
}

func mustJSONMap(payload []byte) map[string]interface{} {
	var out map[string]interface{}
	_ = json.Unmarshal(payload, &out)
	return out
}

func parseCodexWebsocketError(payload []byte) (error, int, http.Header, bool) {
	root := mustJSONMap(payload)
	if strings.TrimSpace(asString(root["type"])) != "error" {
		return nil, 0, nil, false
	}
	status := intValue(root["status"])
	if status == 0 {
		status = intValue(root["status_code"])
	}
	if status <= 0 {
		status = http.StatusBadGateway
	}
	headers := parseCodexWebsocketErrorHeaders(root["headers"])
	errNode := root["error"]
	if errMap := mapFromAny(errNode); len(errMap) > 0 {
		msg := strings.TrimSpace(asString(errMap["message"]))
		if msg == "" {
			msg = http.StatusText(status)
		}
		return fmt.Errorf("codex websocket upstream error (%d): %s", status, msg), status, headers, true
	}
	if msg := strings.TrimSpace(asString(errNode)); msg != "" {
		return fmt.Errorf("codex websocket upstream error (%d): %s", status, msg), status, headers, true
	}
	return fmt.Errorf("codex websocket upstream error (%d)", status), status, headers, true
}

func parseCodexWebsocketErrorHeaders(raw interface{}) http.Header {
	headersMap := mapFromAny(raw)
	if len(headersMap) == 0 {
		return nil
	}
	headers := make(http.Header)
	for key, value := range headersMap {
		name := strings.TrimSpace(key)
		if name == "" {
			continue
		}
		switch typed := value.(type) {
		case string:
			if v := strings.TrimSpace(typed); v != "" {
				headers.Set(name, v)
			}
		case float64, bool, int, int64:
			headers.Set(name, strings.TrimSpace(fmt.Sprintf("%v", typed)))
		}
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}
