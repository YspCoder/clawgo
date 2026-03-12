package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	geminiCLIBaseURL    = "https://cloudcode-pa.googleapis.com"
	geminiCLIVersion    = "v1internal"
	geminiCLIDefaultAlt = "sse"
	geminiCLIApiClient  = "genai-cli/0 gl-go/1.0"
)

type GeminiCLIProvider struct {
	base *HTTPProvider
}

func NewGeminiCLIProvider(providerName, apiKey, apiBase, defaultModel string, supportsResponsesCompact bool, authMode string, timeout time.Duration, oauth *oauthManager) *GeminiCLIProvider {
	normalizedBase := normalizeAPIBase(apiBase)
	if normalizedBase == "" {
		normalizedBase = geminiCLIBaseURL
	}
	return &GeminiCLIProvider{
		base: NewHTTPProvider(providerName, apiKey, normalizedBase, defaultModel, supportsResponsesCompact, authMode, timeout, oauth),
	}
}

func (p *GeminiCLIProvider) GetDefaultModel() string {
	if p == nil || p.base == nil {
		return ""
	}
	return p.base.GetDefaultModel()
}

func (p *GeminiCLIProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
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

func (p *GeminiCLIProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onDelta func(string)) (*LLMResponse, error) {
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

func (p *GeminiCLIProvider) CountTokens(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*UsageInfo, error) {
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
		requestBody := p.buildRequestBody(messages, nil, model, options, false, attempt.session)
		delete(requestBody, "project")
		delete(requestBody, "model")
		request := mapFromAny(requestBody["request"])
		delete(request, "safetySettings")
		requestBody["request"] = request
		body, status, ctype, reqErr := p.performAttempt(ctx, p.endpoint("countTokens", false), requestBody, attempt, false, nil)
		if reqErr != nil {
			return nil, reqErr
		}
		lastBody, lastStatus, lastType = body, status, ctype
		reason, retry := classifyOAuthFailure(status, body)
		if retry {
			applyAttemptFailure(p.base, attempt, reason, geminiRetryAfter(body))
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

func (p *GeminiCLIProvider) doRequest(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, stream bool, onDelta func(string)) ([]byte, int, string, error) {
	attempts, err := p.base.authAttempts(ctx)
	if err != nil {
		return nil, 0, "", err
	}
	var lastBody []byte
	var lastStatus int
	var lastType string
	action := "generateContent"
	if stream {
		action = "streamGenerateContent"
	}
	for _, attempt := range attempts {
		requestBody := p.buildRequestBody(messages, tools, model, options, stream, attempt.session)
		body, status, ctype, reqErr := p.performAttempt(ctx, p.endpoint(action, stream), requestBody, attempt, stream, onDelta)
		if reqErr != nil {
			return nil, 0, "", reqErr
		}
		lastBody, lastStatus, lastType = body, status, ctype
		reason, retry := classifyOAuthFailure(status, body)
		if retry {
			applyAttemptFailure(p.base, attempt, reason, geminiRetryAfter(body))
			continue
		}
		p.base.markAttemptSuccess(attempt)
		return body, status, ctype, nil
	}
	return lastBody, lastStatus, lastType, nil
}

func (p *GeminiCLIProvider) buildRequestBody(messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, stream bool, session *oauthSession) map[string]any {
	request := map[string]any{
		"request": p.buildInnerRequest(messages, tools, model, options, stream),
		"model":   strings.TrimSpace(qwenBaseModel(model)),
	}
	if projectID := geminiCLIProjectID(options, session); projectID != "" {
		request["project"] = projectID
	}
	return request
}

func (p *GeminiCLIProvider) buildInnerRequest(messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, stream bool) map[string]any {
	request := NewGeminiProvider(p.base.providerName, p.base.apiKey, p.base.apiBase, p.base.defaultModel, p.base.supportsResponsesCompact, p.base.authMode, p.base.timeout, p.base.oauth).
		buildRequestBody(messages, tools, model, options, stream)
	if _, ok := request["safetySettings"]; !ok {
		request["safetySettings"] = []map[string]any{}
	}
	return request
}

func (p *GeminiCLIProvider) endpoint(action string, stream bool) string {
	base := normalizeAPIBase(p.base.apiBase)
	if base == "" {
		base = geminiCLIBaseURL
	}
	url := fmt.Sprintf("%s/%s:%s", base, geminiCLIVersion, action)
	if stream {
		return url + "?alt=" + geminiCLIDefaultAlt
	}
	return url
}

func (p *GeminiCLIProvider) performAttempt(ctx context.Context, endpoint string, payload map[string]any, attempt authAttempt, stream bool, onDelta func(string)) ([]byte, int, string, error) {
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
	if err := applyGeminiCLIAttemptAuth(req, attempt); err != nil {
		return nil, 0, "", err
	}
	applyGeminiCLIHeaders(req, strings.TrimSpace(asString(payload["model"])))
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
		return consumeGeminiCLIStream(resp, onDelta)
	}
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, resp.StatusCode, ctype, fmt.Errorf("failed to read response: %w", readErr)
	}
	return body, resp.StatusCode, ctype, nil
}

func applyGeminiCLIAttemptAuth(req *http.Request, attempt authAttempt) error {
	if req == nil {
		return nil
	}
	token := strings.TrimSpace(attempt.token)
	if attempt.session != nil {
		token = firstNonEmpty(strings.TrimSpace(attempt.session.AccessToken), token, asString(attempt.session.Token["access_token"]))
	}
	if token == "" {
		return fmt.Errorf("missing access token for gemini-cli")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Del("x-goog-api-key")
	return nil
}

func consumeGeminiCLIStream(resp *http.Response, onDelta func(string)) ([]byte, int, string, error) {
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

func geminiCLIProjectID(options map[string]interface{}, session *oauthSession) string {
	if value, ok := stringOption(options, "gemini_project_id"); ok {
		return value
	}
	if value, ok := stringOption(options, "project_id"); ok {
		return value
	}
	if session == nil {
		return ""
	}
	return firstNonEmpty(strings.TrimSpace(session.ProjectID), asString(session.Token["project_id"]), asString(session.Token["projectId"]), asString(session.Token["project"]))
}

func applyGeminiCLIHeaders(req *http.Request, model string) {
	if req == nil {
		return
	}
	if strings.TrimSpace(model) == "" {
		model = "unknown"
	}
	req.Header.Set("User-Agent", "GeminiCLI/"+model)
	req.Header.Set("X-Goog-Api-Client", geminiCLIApiClient)
}

func geminiRetryAfter(body []byte) *time.Duration {
	if len(body) == 0 {
		return nil
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return retryDelayFromMessage(string(body))
	}
	errRoot := mapFromAny(root["error"])
	details, _ := errRoot["details"].([]any)
	for _, raw := range details {
		detail := mapFromAny(raw)
		if asString(detail["@type"]) == "type.googleapis.com/google.rpc.RetryInfo" {
			if d, err := time.ParseDuration(strings.TrimSpace(asString(detail["retryDelay"]))); err == nil {
				return &d
			}
		}
	}
	for _, raw := range details {
		detail := mapFromAny(raw)
		if asString(detail["@type"]) == "type.googleapis.com/google.rpc.ErrorInfo" {
			metadata := mapFromAny(detail["metadata"])
			if d, err := time.ParseDuration(strings.TrimSpace(asString(metadata["quotaResetDelay"]))); err == nil {
				return &d
			}
		}
	}
	return retryDelayFromMessage(asString(errRoot["message"]))
}

func retryDelayFromMessage(message string) *time.Duration {
	re := regexp.MustCompile(`after\s+(\d+)s\.?`)
	matches := re.FindStringSubmatch(strings.TrimSpace(message))
	if len(matches) < 2 {
		return nil
	}
	seconds, err := strconv.Atoi(matches[1])
	if err != nil {
		return nil
	}
	d := time.Duration(seconds) * time.Second
	return &d
}
