package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/jwt"
)

const (
	vertexDefaultBaseURL = "https://aiplatform.googleapis.com"
	vertexAPIVersion     = "v1"
	vertexDefaultRegion  = "us-central1"
)

type VertexProvider struct {
	base *HTTPProvider
}

func NewVertexProvider(providerName, apiKey, apiBase, defaultModel string, supportsResponsesCompact bool, authMode string, timeout time.Duration, oauth *oauthManager) *VertexProvider {
	normalizedBase := normalizeAPIBase(apiBase)
	if normalizedBase == "" {
		normalizedBase = vertexDefaultBaseURL
	}
	return &VertexProvider{
		base: NewHTTPProvider(providerName, apiKey, normalizedBase, defaultModel, supportsResponsesCompact, authMode, timeout, oauth),
	}
}

func (p *VertexProvider) GetDefaultModel() string {
	if p == nil || p.base == nil {
		return ""
	}
	return p.base.GetDefaultModel()
}

func (p *VertexProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
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
	if isVertexImagenModel(model) {
		body = convertVertexImagenToGeminiResponse(body, strings.TrimSpace(qwenBaseModel(model)))
	}
	return parseGeminiResponse(body)
}

func (p *VertexProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onDelta func(string)) (*LLMResponse, error) {
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
	if isVertexImagenModel(model) {
		body = convertVertexImagenToGeminiResponse(body, strings.TrimSpace(qwenBaseModel(model)))
	}
	return parseGeminiResponse(body)
}

func (p *VertexProvider) CountTokens(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*UsageInfo, error) {
	if p == nil || p.base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	attempts, err := p.vertexAttempts(ctx)
	if err != nil {
		return nil, err
	}
	var lastBody []byte
	var lastStatus int
	var lastType string
	for _, attempt := range attempts {
		requestBody := p.buildRequestBody(messages, nil, model, options)
		if isVertexImagenModel(model) {
			requestBody, err = convertVertexImagenRequest(requestBody)
			if err != nil {
				return nil, err
			}
		}
		delete(requestBody, "tools")
		delete(requestBody, "toolConfig")
		delete(requestBody, "generationConfig")
		endpoint := p.endpoint(attempt, model, "countTokens", false, options)
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

func (p *VertexProvider) doRequest(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, stream bool, onDelta func(string)) ([]byte, int, string, error) {
	if p == nil || p.base == nil {
		return nil, 0, "", fmt.Errorf("provider not configured")
	}
	attempts, err := p.vertexAttempts(ctx)
	if err != nil {
		return nil, 0, "", err
	}
	var lastBody []byte
	var lastStatus int
	var lastType string
	for _, attempt := range attempts {
		requestBody := p.buildRequestBody(messages, tools, model, options)
		if isVertexImagenModel(model) {
			requestBody, err = convertVertexImagenRequest(requestBody)
			if err != nil {
				return nil, 0, "", err
			}
		}
		endpoint := p.endpoint(attempt, model, "generateContent", stream, options)
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

func (p *VertexProvider) performAttempt(ctx context.Context, endpoint string, payload map[string]any, attempt authAttempt, stream bool, onDelta func(string)) ([]byte, int, string, error) {
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	if err := p.applyVertexAttemptAuth(ctx, req, attempt); err != nil {
		return nil, 0, "", err
	}
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
	body, readErr := ioReadAll(resp)
	if readErr != nil {
		return nil, resp.StatusCode, ctype, readErr
	}
	return body, resp.StatusCode, ctype, nil
}

func (p *VertexProvider) buildRequestBody(messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) map[string]any {
	return (&GeminiProvider{base: p.base}).buildRequestBody(messages, tools, model, options, false)
}

func (p *VertexProvider) endpoint(attempt authAttempt, model, action string, stream bool, options map[string]interface{}) string {
	base := vertexBaseURLForAttempt(p.base, attempt, options)
	baseModel := strings.TrimSpace(qwenBaseModel(model))
	vertexAction := vertexAction(baseModel, stream)
	if projectID, location, ok := vertexProjectLocation(attempt, options); ok {
		if stream {
			if isVertexImagenModel(baseModel) {
				return fmt.Sprintf("%s/%s/projects/%s/locations/%s/publishers/google/models/%s:%s", base, vertexAPIVersion, projectID, location, baseModel, vertexAction)
			}
			return fmt.Sprintf("%s/%s/projects/%s/locations/%s/publishers/google/models/%s:%s?alt=sse", base, vertexAPIVersion, projectID, location, baseModel, vertexAction)
		}
		return fmt.Sprintf("%s/%s/projects/%s/locations/%s/publishers/google/models/%s:%s", base, vertexAPIVersion, projectID, location, baseModel, vertexAction)
	}
	if stream {
		if isVertexImagenModel(baseModel) {
			return fmt.Sprintf("%s/%s/publishers/google/models/%s:%s", base, vertexAPIVersion, baseModel, vertexAction)
		}
		return fmt.Sprintf("%s/%s/publishers/google/models/%s:%s?alt=sse", base, vertexAPIVersion, baseModel, vertexAction)
	}
	return fmt.Sprintf("%s/%s/publishers/google/models/%s:%s", base, vertexAPIVersion, baseModel, vertexAction)
}

func vertexBaseURLForAttempt(base *HTTPProvider, attempt authAttempt, options map[string]interface{}) string {
	customBase := ""
	if attempt.session != nil && attempt.session.Token != nil {
		if raw := firstNonEmpty(
			strings.TrimSpace(asString(attempt.session.Token["base_url"])),
			strings.TrimSpace(asString(attempt.session.Token["base-url"])),
		); raw != "" {
			customBase = normalizeVertexBaseURL(raw)
		}
	}
	if customBase == "" && base != nil && strings.TrimSpace(base.apiBase) != "" && !strings.Contains(strings.ToLower(base.apiBase), "api.openai.com") {
		customBase = normalizeVertexBaseURL(base.apiBase)
	}
	if customBase != "" && !strings.EqualFold(customBase, vertexDefaultBaseURL) {
		return customBase
	}
	location := vertexLocationForAttempt(attempt, options)
	if location != "" && !strings.EqualFold(location, "global") {
		return fmt.Sprintf("https://%s-aiplatform.googleapis.com", location)
	}
	return vertexDefaultBaseURL
}

func normalizeVertexBaseURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return vertexDefaultBaseURL
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	return normalizeAPIBase(trimmed)
}

func vertexProjectLocation(attempt authAttempt, options map[string]interface{}) (string, string, bool) {
	projectID := ""
	for _, key := range []string{"vertex_project_id", "project_id", "project"} {
		if value, ok := stringOption(options, key); ok {
			projectID = strings.TrimSpace(value)
			if projectID != "" {
				break
			}
		}
	}
	if attempt.session != nil {
		projectID = firstNonEmpty(projectID, strings.TrimSpace(attempt.session.ProjectID), asString(attempt.session.Token["project_id"]), asString(attempt.session.Token["project-id"]), asString(attempt.session.Token["projectId"]), asString(attempt.session.Token["project"]))
		if projectID == "" {
			projectID = strings.TrimSpace(asString(mapFromAny(attempt.session.Token["service_account"])["project_id"]))
		}
	}
	location := vertexLocationForAttempt(attempt, options)
	if strings.TrimSpace(projectID) == "" {
		return "", "", false
	}
	return projectID, location, true
}

func vertexLocationForAttempt(attempt authAttempt, options map[string]interface{}) string {
	location := ""
	for _, key := range []string{"vertex_location", "location", "region"} {
		if value, ok := stringOption(options, key); ok {
			location = strings.TrimSpace(value)
			if location != "" {
				break
			}
		}
	}
	if attempt.session != nil {
		location = firstNonEmpty(location, asString(attempt.session.Token["location"]), asString(attempt.session.Token["region"]), asString(mapFromAny(attempt.session.Token["service_account"])["location"]))
	}
	if strings.TrimSpace(location) == "" {
		location = vertexDefaultRegion
	}
	return location
}

func isVertexImagenModel(model string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(model)), "imagen")
}

func vertexAction(model string, stream bool) string {
	if isVertexImagenModel(model) {
		return "predict"
	}
	if stream {
		return "streamGenerateContent"
	}
	return "generateContent"
}

func convertVertexImagenRequest(payload map[string]any) (map[string]any, error) {
	prompt := strings.TrimSpace(geminiPromptTextFromPayload(payload))
	if prompt == "" {
		return nil, fmt.Errorf("imagen: no prompt found in request")
	}
	request := map[string]any{
		"instances": []map[string]any{{"prompt": prompt}},
		"parameters": map[string]any{
			"sampleCount": 1,
		},
	}
	if generationConfig := mapFromAny(payload["generationConfig"]); len(generationConfig) > 0 {
		if aspectRatio := strings.TrimSpace(asString(generationConfig["aspectRatio"])); aspectRatio != "" {
			request["parameters"].(map[string]any)["aspectRatio"] = aspectRatio
		}
		switch count := generationConfig["sampleCount"].(type) {
		case int:
			if count > 0 {
				request["parameters"].(map[string]any)["sampleCount"] = count
			}
		case float64:
			if int(count) > 0 {
				request["parameters"].(map[string]any)["sampleCount"] = int(count)
			}
		}
		if negativePrompt := strings.TrimSpace(asString(generationConfig["negativePrompt"])); negativePrompt != "" {
			request["instances"].([]map[string]any)[0]["negativePrompt"] = negativePrompt
		}
	}
	return request, nil
}

func geminiPromptTextFromPayload(payload map[string]any) string {
	contents, _ := payload["contents"].([]map[string]any)
	for _, content := range contents {
		parts, _ := content["parts"].([]map[string]any)
		for _, part := range parts {
			if text := strings.TrimSpace(asString(part["text"])); text != "" {
				return text
			}
		}
	}
	return strings.TrimSpace(asString(payload["prompt"]))
}

func convertVertexImagenToGeminiResponse(data []byte, model string) []byte {
	var raw struct {
		Predictions []struct {
			BytesBase64Encoded string `json:"bytesBase64Encoded"`
			MIMEType           string `json:"mimeType"`
		} `json:"predictions"`
	}
	if err := json.Unmarshal(data, &raw); err != nil || len(raw.Predictions) == 0 {
		return data
	}
	parts := make([]map[string]any, 0, len(raw.Predictions))
	for _, prediction := range raw.Predictions {
		if strings.TrimSpace(prediction.BytesBase64Encoded) == "" {
			continue
		}
		parts = append(parts, map[string]any{
			"inlineData": map[string]any{
				"mimeType": firstNonEmpty(strings.TrimSpace(prediction.MIMEType), "image/png"),
				"data":     prediction.BytesBase64Encoded,
			},
		})
	}
	if len(parts) == 0 {
		return data
	}
	converted := map[string]any{
		"candidates": []map[string]any{{
			"content": map[string]any{
				"parts": parts,
				"role":  "model",
			},
			"finishReason": "STOP",
		}},
		"responseId":   fmt.Sprintf("imagen-%d", time.Now().UnixNano()),
		"modelVersion": model,
		"usageMetadata": map[string]any{
			"promptTokenCount":     0,
			"candidatesTokenCount": 0,
			"totalTokenCount":      0,
		},
	}
	out, err := json.Marshal(converted)
	if err != nil {
		return data
	}
	return out
}

func ioReadAll(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	return body, nil
}

func (p *VertexProvider) vertexAttempts(ctx context.Context) ([]authAttempt, error) {
	if p == nil || p.base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	mode := strings.ToLower(strings.TrimSpace(p.base.authMode))
	if mode != "oauth" && mode != "hybrid" {
		return p.base.authAttempts(ctx)
	}
	out := make([]authAttempt, 0, 4)
	if mode == "hybrid" {
		if apiAttempt, apiReady := p.base.apiKeyAttempt(); apiReady {
			out = append(out, apiAttempt)
		}
	}
	if p.base.oauth == nil {
		if len(out) > 0 {
			p.base.updateCandidateOrder(out)
			return out, nil
		}
		return nil, fmt.Errorf("oauth is enabled but provider session manager is not configured")
	}
	manager := p.base.oauth
	manager.mu.Lock()
	sessions, err := manager.loadAllLocked()
	manager.mu.Unlock()
	if err != nil {
		return nil, err
	}
	for _, session := range sessions {
		if session == nil || manager.sessionOnCooldown(session) {
			continue
		}
		token := strings.TrimSpace(session.AccessToken)
		if token == "" && !vertexHasServiceAccount(session) {
			continue
		}
		out = append(out, authAttempt{session: session, token: token, kind: "oauth"})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("oauth session not found, run `clawgo provider login` first")
	}
	p.base.updateCandidateOrder(out)
	return out, nil
}

func (p *VertexProvider) applyVertexAttemptAuth(ctx context.Context, req *http.Request, attempt authAttempt) error {
	if req == nil {
		return nil
	}
	if attempt.kind == "api_key" {
		applyGeminiAttemptAuth(req, attempt)
		return nil
	}
	if strings.TrimSpace(attempt.token) != "" {
		applyGeminiAttemptAuth(req, attempt)
		return nil
	}
	saJSON, err := vertexServiceAccountJSON(attempt.session)
	if err != nil {
		return err
	}
	client, err := p.base.httpClientForAttempt(attempt)
	if err == nil && client != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
	}
	var serviceAccount struct {
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		TokenURI    string `json:"token_uri"`
	}
	if err := json.Unmarshal(saJSON, &serviceAccount); err != nil {
		return fmt.Errorf("vertex service account parse failed: %w", err)
	}
	cfg := &jwt.Config{
		Email:      strings.TrimSpace(serviceAccount.ClientEmail),
		PrivateKey: []byte(serviceAccount.PrivateKey),
		TokenURL:   strings.TrimSpace(serviceAccount.TokenURI),
		Scopes:     []string{"https://www.googleapis.com/auth/cloud-platform"},
	}
	tok, err := cfg.TokenSource(ctx).Token()
	if err != nil {
		return fmt.Errorf("vertex service account token failed: %w", err)
	}
	req.Header.Del("x-goog-api-key")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(tok.AccessToken))
	return nil
}

func vertexHasServiceAccount(session *oauthSession) bool {
	if session == nil || session.Token == nil {
		return false
	}
	return len(mapFromAny(session.Token["service_account"])) > 0
}

func vertexServiceAccountJSON(session *oauthSession) ([]byte, error) {
	if !vertexHasServiceAccount(session) {
		return nil, fmt.Errorf("vertex service account missing")
	}
	raw := mapFromAny(session.Token["service_account"])
	if projectID := firstNonEmpty(asString(raw["project_id"]), asString(raw["project-id"]), strings.TrimSpace(session.ProjectID), asString(session.Token["project_id"]), asString(session.Token["project-id"]), asString(session.Token["project"])); projectID != "" {
		raw["project_id"] = projectID
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("vertex service account marshal failed: %w", err)
	}
	return data, nil
}
