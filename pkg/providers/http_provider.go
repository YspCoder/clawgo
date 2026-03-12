package providers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/logger"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	codexCompatBaseURL   = "https://chatgpt.com/backend-api/codex"
	codexClientVersion   = "0.101.0"
	codexCompatUserAgent = "codex_cli_rs/0.101.0 (Mac OS 26.0.1; arm64) Apple_Terminal/464"
	qwenCompatBaseURL    = "https://portal.qwen.ai/v1"
	qwenCompatUserAgent  = "QwenCode/0.10.3 (darwin; arm64)"
	kimiCompatBaseURL    = "https://api.kimi.com/coding/v1"
	kimiCompatUserAgent  = "KimiCLI/1.10.6"
)

type providerAPIRuntimeState struct {
	TokenMasked   string `json:"token_masked,omitempty"`
	CooldownUntil string `json:"cooldown_until,omitempty"`
	FailureCount  int    `json:"failure_count,omitempty"`
	LastFailure   string `json:"last_failure,omitempty"`
	HealthScore   int    `json:"health_score,omitempty"`
}

type providerRuntimeEvent struct {
	When   string `json:"when,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Target string `json:"target,omitempty"`
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
}

func recordProviderRuntimeChange(providerName, kind, target, reason, detail string) {
	name := strings.TrimSpace(providerName)
	if name == "" || strings.TrimSpace(reason) == "" {
		return
	}
	providerRuntimeRegistry.mu.Lock()
	defer providerRuntimeRegistry.mu.Unlock()
	state := providerRuntimeRegistry.api[name]
	state.RecentChanges = appendRuntimeEvent(state.RecentChanges, providerRuntimeEvent{
		When:   time.Now().Format(time.RFC3339),
		Kind:   strings.TrimSpace(kind),
		Target: strings.TrimSpace(target),
		Reason: strings.TrimSpace(reason),
		Detail: strings.TrimSpace(detail),
	}, runtimeEventLimit(state))
	persistProviderRuntimeLocked(name, state)
	providerRuntimeRegistry.api[name] = state
}

type providerRuntimeCandidate struct {
	Kind          string `json:"kind,omitempty"`
	Target        string `json:"target,omitempty"`
	Available     bool   `json:"available"`
	Status        string `json:"status,omitempty"`
	CooldownUntil string `json:"cooldown_until,omitempty"`
	HealthScore   int    `json:"health_score,omitempty"`
	FailureCount  int    `json:"failure_count,omitempty"`
}

type providerRuntimePersistConfig struct {
	Enabled     bool
	File        string
	MaxEvents   int
	Loaded      bool
	LoadAttempt bool
}

type ProviderRuntimeQuery struct {
	Provider       string
	Window         time.Duration
	EventKind      string
	Reason         string
	Target         string
	Limit          int
	Cursor         int
	HealthBelow    int
	CooldownBefore time.Time
	Sort           string
	ChangesOnly    bool
}

type ProviderRefreshAccountResult struct {
	Target string `json:"target,omitempty"`
	Status string `json:"status,omitempty"`
	Detail string `json:"detail,omitempty"`
	Expire string `json:"expire,omitempty"`
}

type ProviderRefreshResult struct {
	Provider  string                         `json:"provider,omitempty"`
	Checked   int                            `json:"checked,omitempty"`
	Refreshed int                            `json:"refreshed,omitempty"`
	Skipped   int                            `json:"skipped,omitempty"`
	Failed    int                            `json:"failed,omitempty"`
	Accounts  []ProviderRefreshAccountResult `json:"accounts,omitempty"`
}

type ProviderRuntimeSummaryItem struct {
	Name                  string                     `json:"name,omitempty"`
	Auth                  string                     `json:"auth,omitempty"`
	Status                string                     `json:"status,omitempty"`
	APIState              providerAPIRuntimeState    `json:"api_state,omitempty"`
	OAuthAccounts         []OAuthAccountInfo         `json:"oauth_accounts,omitempty"`
	CandidateOrder        []providerRuntimeCandidate `json:"candidate_order,omitempty"`
	LastSuccess           *providerRuntimeEvent      `json:"last_success,omitempty"`
	LastSuccessAt         string                     `json:"last_success_at,omitempty"`
	LastError             *providerRuntimeEvent      `json:"last_error,omitempty"`
	LastErrorAt           string                     `json:"last_error_at,omitempty"`
	LastErrorReason       string                     `json:"last_error_reason,omitempty"`
	TopCandidateChangedAt string                     `json:"top_candidate_changed_at,omitempty"`
	StaleForSec           int64                      `json:"stale_for_sec,omitempty"`
	InCooldown            bool                       `json:"in_cooldown"`
	LowHealth             bool                       `json:"low_health"`
	HasRecentErrors       bool                       `json:"has_recent_errors"`
	TopCandidate          *providerRuntimeCandidate  `json:"top_candidate,omitempty"`
}

type ProviderRuntimeSummary struct {
	TotalProviders int                          `json:"total_providers"`
	Healthy        int                          `json:"healthy"`
	Degraded       int                          `json:"degraded"`
	Critical       int                          `json:"critical"`
	InCooldown     int                          `json:"in_cooldown"`
	LowHealth      int                          `json:"low_health"`
	RecentErrors   int                          `json:"recent_errors"`
	Providers      []ProviderRuntimeSummaryItem `json:"providers,omitempty"`
}

type providerRuntimeState struct {
	API            providerAPIRuntimeState      `json:"api_state,omitempty"`
	RecentHits     []providerRuntimeEvent       `json:"recent_hits,omitempty"`
	RecentErrors   []providerRuntimeEvent       `json:"recent_errors,omitempty"`
	RecentChanges  []providerRuntimeEvent       `json:"recent_changes,omitempty"`
	LastSuccess    *providerRuntimeEvent        `json:"last_success,omitempty"`
	CandidateOrder []providerRuntimeCandidate   `json:"candidate_order,omitempty"`
	Persist        providerRuntimePersistConfig `json:"-"`
}

var providerRuntimeRegistry = struct {
	mu  sync.Mutex
	api map[string]providerRuntimeState
}{api: map[string]providerRuntimeState{}}

type HTTPProvider struct {
	providerName             string
	apiKey                   string
	apiBase                  string
	defaultModel             string
	supportsResponsesCompact bool
	authMode                 string
	timeout                  time.Duration
	httpClient               *http.Client
	oauth                    *oauthManager
}

func NewHTTPProvider(providerName, apiKey, apiBase, defaultModel string, supportsResponsesCompact bool, authMode string, timeout time.Duration, oauth *oauthManager) *HTTPProvider {
	normalizedBase := normalizeAPIBase(apiBase)
	if oauth != nil {
		oauth.providerName = strings.TrimSpace(providerName)
	}
	return &HTTPProvider{
		providerName:             strings.TrimSpace(providerName),
		apiKey:                   apiKey,
		apiBase:                  normalizedBase,
		defaultModel:             strings.TrimSpace(defaultModel),
		supportsResponsesCompact: supportsResponsesCompact,
		authMode:                 authMode,
		timeout:                  timeout,
		httpClient:               &http.Client{Timeout: timeout},
		oauth:                    oauth,
	}
}

func ConfigureProviderRuntime(providerName string, pc config.ProviderConfig) {
	name := strings.TrimSpace(providerName)
	if name == "" {
		return
	}
	providerRuntimeRegistry.mu.Lock()
	defer providerRuntimeRegistry.mu.Unlock()
	state := providerRuntimeRegistry.api[name]
	state.Persist = providerRuntimePersistConfig{
		Enabled:     pc.RuntimePersist,
		File:        runtimeHistoryFile(name, pc),
		MaxEvents:   runtimeHistoryMax(pc),
		Loaded:      state.Persist.Loaded,
		LoadAttempt: state.Persist.LoadAttempt,
	}
	if state.Persist.Enabled && !state.Persist.LoadAttempt {
		state.Persist.LoadAttempt = true
		loadPersistedProviderRuntimeLocked(name, &state)
	}
	providerRuntimeRegistry.api[name] = state
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
	if p.useOpenAICompatChatUpstream() {
		return parseOpenAICompatResponse(body)
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
	if p.useOpenAICompatChatUpstream() {
		return parseOpenAICompatResponse(body)
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

func (p *HTTPProvider) postJSONStream(ctx context.Context, endpoint string, payload interface{}, onEvent func(string)) ([]byte, int, string, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to marshal request: %w", err)
	}
	attempts, err := p.authAttempts(ctx)
	if err != nil {
		return nil, 0, "", err
	}
	var lastBody []byte
	var lastStatus int
	var lastType string
	for _, attempt := range attempts {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonData))
		if err != nil {
			return nil, 0, "", fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		applyAttemptAuth(req, attempt)
		applyAttemptProviderHeaders(req, attempt, p, true)

		body, status, ctype, quotaHit, err := p.doStreamAttempt(req, attempt, onEvent)
		if err != nil {
			return nil, 0, "", err
		}
		if !quotaHit {
			p.markAttemptSuccess(attempt)
			return body, status, ctype, nil
		}
		lastBody, lastStatus, lastType = body, status, ctype
		if attempt.kind == "oauth" && attempt.session != nil && p.oauth != nil {
			reason, _ := classifyOAuthFailure(status, body)
			p.oauth.markExhausted(attempt.session, reason)
			recordProviderOAuthError(p.providerName, attempt.session, reason)
		}
		if attempt.kind == "api_key" {
			reason, _ := classifyOAuthFailure(status, body)
			p.markAPIKeyFailure(reason)
		}
	}
	return lastBody, lastStatus, lastType, nil
}

func (p *HTTPProvider) postJSON(ctx context.Context, endpoint string, payload interface{}) ([]byte, int, string, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to marshal request: %w", err)
	}
	attempts, err := p.authAttempts(ctx)
	if err != nil {
		return nil, 0, "", err
	}
	var lastBody []byte
	var lastStatus int
	var lastType string
	for _, attempt := range attempts {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonData))
		if err != nil {
			return nil, 0, "", fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		applyAttemptAuth(req, attempt)
		applyAttemptProviderHeaders(req, attempt, p, false)

		body, status, ctype, err := p.doJSONAttempt(req, attempt)
		if err != nil {
			return nil, 0, "", err
		}
		reason, retry := classifyOAuthFailure(status, body)
		if !retry {
			p.markAttemptSuccess(attempt)
			return body, status, ctype, nil
		}
		lastBody, lastStatus, lastType = body, status, ctype
		if attempt.kind == "oauth" && attempt.session != nil && p.oauth != nil {
			p.oauth.markExhausted(attempt.session, reason)
			recordProviderOAuthError(p.providerName, attempt.session, reason)
		}
		if attempt.kind == "api_key" {
			p.markAPIKeyFailure(reason)
		}
	}
	return lastBody, lastStatus, lastType, nil
}

type authAttempt struct {
	session *oauthSession
	token   string
	kind    string
}

func (p *HTTPProvider) authAttempts(ctx context.Context) ([]authAttempt, error) {
	mode := strings.ToLower(strings.TrimSpace(p.authMode))
	if mode == "oauth" || mode == "hybrid" {
		out := make([]authAttempt, 0, 1)
		apiAttempt, apiReady := p.apiKeyAttempt()
		if p.oauth == nil {
			if mode == "hybrid" && apiReady {
				return []authAttempt{apiAttempt}, nil
			}
			return nil, fmt.Errorf("oauth is enabled but provider session manager is not configured")
		}
		attempts, err := p.oauth.prepareAttemptsLocked(ctx)
		if err != nil {
			return nil, err
		}
		oauthAttempts := make([]authAttempt, 0, len(attempts))
		for _, attempt := range attempts {
			oauthAttempts = append(oauthAttempts, authAttempt{session: attempt.Session, token: attempt.Token, kind: "oauth"})
		}
		if mode == "hybrid" && apiReady {
			out = append(out, apiAttempt)
		}
		if len(attempts) == 0 {
			if len(out) > 0 {
				p.updateCandidateOrder(out)
				return out, nil
			}
			return nil, fmt.Errorf("oauth session not found, run `clawgo provider login` first")
		}
		out = append(out, oauthAttempts...)
		p.updateCandidateOrder(out)
		return out, nil
	}
	apiAttempt, apiReady := p.apiKeyAttempt()
	if !apiReady {
		return nil, fmt.Errorf("api key temporarily unavailable")
	}
	out := []authAttempt{apiAttempt}
	p.updateCandidateOrder(out)
	return out, nil
}

func (p *HTTPProvider) updateCandidateOrder(attempts []authAttempt) {
	name := strings.TrimSpace(p.providerName)
	if name == "" {
		return
	}
	candidates := make([]providerRuntimeCandidate, 0, len(attempts))
	for _, attempt := range attempts {
		candidate := providerRuntimeCandidate{
			Kind:      attempt.kind,
			Available: true,
			Status:    "ready",
		}
		if attempt.kind == "api_key" {
			candidate.Target = maskToken(p.apiKey)
			candidate.HealthScore = providerAPIHealth(name)
		} else if attempt.session != nil {
			candidate.Target = firstNonEmpty(attempt.session.Email, attempt.session.AccountID, attempt.session.FilePath)
			candidate.HealthScore = sessionHealthScore(attempt.session)
			candidate.FailureCount = attempt.session.FailureCount
			candidate.CooldownUntil = attempt.session.CooldownUntil
		}
		candidates = append(candidates, candidate)
	}
	providerRuntimeRegistry.mu.Lock()
	defer providerRuntimeRegistry.mu.Unlock()
	state := providerRuntimeRegistry.api[name]
	if !providerCandidatesEqual(state.CandidateOrder, candidates) {
		state.RecentChanges = appendRuntimeEvent(state.RecentChanges, providerRuntimeEvent{
			When:   time.Now().Format(time.RFC3339),
			Kind:   "scheduler",
			Target: name,
			Reason: "candidate_order_changed",
			Detail: candidateOrderChangeDetail(state.CandidateOrder, candidates),
		}, runtimeEventLimit(state))
	}
	state.CandidateOrder = candidates
	persistProviderRuntimeLocked(name, state)
	providerRuntimeRegistry.api[name] = state
}

func applyAttemptAuth(req *http.Request, attempt authAttempt) {
	if req == nil {
		return
	}
	if strings.TrimSpace(attempt.token) == "" {
		return
	}
	if attempt.kind == "api_key" && strings.Contains(req.URL.Host, "googleapis.com") {
		req.Header.Set("x-goog-api-key", attempt.token)
		req.Header.Del("Authorization")
		return
	}
	req.Header.Del("x-goog-api-key")
	req.Header.Set("Authorization", "Bearer "+attempt.token)
}

func applyAttemptProviderHeaders(req *http.Request, attempt authAttempt, provider *HTTPProvider, stream bool) {
	if req == nil || provider == nil {
		return
	}
	switch provider.oauthProvider() {
	case defaultClaudeOAuthProvider:
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Anthropic-Version", "2023-06-01")
		req.Header.Set("Anthropic-Beta", "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05")
		req.Header.Set("Anthropic-Dangerous-Direct-Browser-Access", "true")
		req.Header.Set("X-App", "cli")
		req.Header.Set("X-Stainless-Retry-Count", "0")
		req.Header.Set("X-Stainless-Runtime-Version", "v24.3.0")
		req.Header.Set("X-Stainless-Package-Version", "0.74.0")
		req.Header.Set("X-Stainless-Runtime", "node")
		req.Header.Set("X-Stainless-Lang", "js")
		req.Header.Set("X-Stainless-Arch", "arm64")
		req.Header.Set("X-Stainless-Os", "macos")
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
		if attempt.kind == "api_key" {
			req.Header.Del("Authorization")
			req.Header.Set("x-api-key", strings.TrimSpace(attempt.token))
		} else {
			req.Header.Del("x-api-key")
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(attempt.token))
		}
		return
	case defaultQwenOAuthProvider:
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(attempt.token))
		req.Header.Set("User-Agent", qwenCompatUserAgent)
		req.Header.Set("X-Dashscope-Useragent", qwenCompatUserAgent)
		req.Header.Set("X-Stainless-Runtime-Version", "v22.17.0")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("X-Stainless-Lang", "js")
		req.Header.Set("X-Stainless-Arch", "arm64")
		req.Header.Set("X-Stainless-Package-Version", "5.11.0")
		req.Header.Set("X-Dashscope-Cachecontrol", "enable")
		req.Header.Set("X-Stainless-Retry-Count", "0")
		req.Header.Set("X-Stainless-Os", "MacOS")
		req.Header.Set("X-Dashscope-Authtype", "qwen-oauth")
		req.Header.Set("X-Stainless-Runtime", "node")
		if stream {
			req.Header.Set("Accept", "text/event-stream")
		} else {
			req.Header.Set("Accept", "application/json")
		}
		return
	case defaultKimiOAuthProvider:
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(attempt.token))
		req.Header.Set("User-Agent", kimiCompatUserAgent)
		req.Header.Set("X-Msh-Platform", "kimi_cli")
		req.Header.Set("X-Msh-Version", "1.10.6")
		req.Header.Set("X-Msh-Device-Name", "clawgo")
		req.Header.Set("X-Msh-Device-Model", runtime.GOOS+" "+runtime.GOARCH)
		if attempt.session != nil && strings.TrimSpace(attempt.session.DeviceID) != "" {
			req.Header.Set("X-Msh-Device-Id", strings.TrimSpace(attempt.session.DeviceID))
		} else {
			req.Header.Set("X-Msh-Device-Id", "clawgo-device")
		}
		if stream {
			req.Header.Set("Accept", "text/event-stream")
		} else {
			req.Header.Set("Accept", "application/json")
		}
		return
	case defaultCodexOAuthProvider:
	default:
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Version", codexClientVersion)
	req.Header.Set("Session_id", randomSessionID())
	req.Header.Set("User-Agent", codexCompatUserAgent)
	req.Header.Set("Connection", "Keep-Alive")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	if attempt.kind != "api_key" {
		req.Header.Set("Originator", "codex_cli_rs")
		if attempt.session != nil && strings.TrimSpace(attempt.session.AccountID) != "" {
			req.Header.Set("Chatgpt-Account-Id", strings.TrimSpace(attempt.session.AccountID))
		}
	}
}

func randomSessionID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

func (p *HTTPProvider) httpClientForAttempt(attempt authAttempt) (*http.Client, error) {
	if attempt.kind == "oauth" && attempt.session != nil && p.oauth != nil {
		return p.oauth.httpClientForSession(attempt.session)
	}
	return p.httpClient, nil
}

func (p *HTTPProvider) doJSONAttempt(req *http.Request, attempt authAttempt) ([]byte, int, string, error) {
	client, err := p.httpClientForAttempt(attempt)
	if err != nil {
		return nil, 0, "", err
	}
	resp, err := client.Do(req)
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

func (p *HTTPProvider) doStreamAttempt(req *http.Request, attempt authAttempt, onEvent func(string)) ([]byte, int, string, bool, error) {
	client, err := p.httpClientForAttempt(attempt)
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
		return nil, resp.StatusCode, ctype, false, fmt.Errorf("failed to read stream: %w", err)
	}
	if len(finalJSON) == 0 {
		finalJSON = []byte("{}")
	}
	return finalJSON, resp.StatusCode, ctype, false, nil
}

func shouldRetryOAuthQuota(status int, body []byte) bool {
	_, retry := classifyOAuthFailure(status, body)
	return retry
}

func classifyOAuthFailure(status int, body []byte) (oauthFailureReason, bool) {
	if status != http.StatusTooManyRequests && status != http.StatusPaymentRequired && status != http.StatusForbidden {
		return "", false
	}
	lower := strings.ToLower(string(body))
	if strings.Contains(lower, "insufficient_quota") || strings.Contains(lower, "quota") || strings.Contains(lower, "billing") {
		return oauthFailureQuota, true
	}
	if strings.Contains(lower, "rate limit") || strings.Contains(lower, "rate_limit") || strings.Contains(lower, "usage limit") {
		return oauthFailureRateLimit, true
	}
	if status == http.StatusForbidden {
		return oauthFailureForbidden, true
	}
	if status == http.StatusTooManyRequests {
		return oauthFailureRateLimit, true
	}
	return "", false
}

func (p *HTTPProvider) markAttemptSuccess(attempt authAttempt) {
	if attempt.kind == "oauth" && attempt.session != nil && p.oauth != nil {
		p.oauth.markSuccess(attempt.session)
	}
	if attempt.kind == "api_key" {
		p.markAPIKeySuccess()
	}
	p.recordProviderHit(attempt, "")
}

func (p *HTTPProvider) markAPIKeyFailure(reason oauthFailureReason) {
	name := strings.TrimSpace(p.providerName)
	if name == "" {
		return
	}
	providerRuntimeRegistry.mu.Lock()
	defer providerRuntimeRegistry.mu.Unlock()
	state := providerRuntimeRegistry.api[name]
	if state.API.HealthScore <= 0 {
		state.API.HealthScore = 100
	}
	state.API.FailureCount++
	state.API.LastFailure = string(reason)
	state.API.HealthScore = maxInt(1, state.API.HealthScore-healthPenaltyForReason(reason))
	cooldown := 15 * time.Minute
	switch reason {
	case oauthFailureQuota:
		cooldown = 60 * time.Minute
	case oauthFailureForbidden:
		cooldown = 30 * time.Minute
	}
	state.API.CooldownUntil = time.Now().Add(cooldown).Format(time.RFC3339)
	state.API.TokenMasked = maskToken(p.apiKey)
	state.RecentErrors = appendRuntimeEvent(state.RecentErrors, providerRuntimeEvent{
		When:   time.Now().Format(time.RFC3339),
		Kind:   "api_key",
		Target: maskToken(p.apiKey),
		Reason: string(reason),
	}, runtimeEventLimit(state))
	state.RecentChanges = appendRuntimeEvent(state.RecentChanges, providerRuntimeEvent{
		When:   time.Now().Format(time.RFC3339),
		Kind:   "api_key",
		Target: maskToken(p.apiKey),
		Reason: "api_key_cooldown_" + string(reason),
		Detail: "api key entered cooldown after request failure",
	}, runtimeEventLimit(state))
	persistProviderRuntimeLocked(name, state)
	providerRuntimeRegistry.api[name] = state
}

func (p *HTTPProvider) markAPIKeySuccess() {
	name := strings.TrimSpace(p.providerName)
	if name == "" {
		return
	}
	providerRuntimeRegistry.mu.Lock()
	defer providerRuntimeRegistry.mu.Unlock()
	state := providerRuntimeRegistry.api[name]
	if state.API.HealthScore <= 0 {
		state.API.HealthScore = 100
	} else {
		state.API.HealthScore = minInt(100, state.API.HealthScore+3)
	}
	wasCooling := strings.TrimSpace(state.API.CooldownUntil) != ""
	state.API.CooldownUntil = ""
	state.API.TokenMasked = maskToken(p.apiKey)
	if wasCooling {
		state.RecentChanges = appendRuntimeEvent(state.RecentChanges, providerRuntimeEvent{
			When:   time.Now().Format(time.RFC3339),
			Kind:   "api_key",
			Target: maskToken(p.apiKey),
			Reason: "api_key_recovered",
			Detail: "api key cooldown cleared after successful request",
		}, runtimeEventLimit(state))
	}
	persistProviderRuntimeLocked(name, state)
	providerRuntimeRegistry.api[name] = state
}

func (p *HTTPProvider) apiKeyAttempt() (authAttempt, bool) {
	token := strings.TrimSpace(p.apiKey)
	if token == "" {
		return authAttempt{}, false
	}
	name := strings.TrimSpace(p.providerName)
	if name == "" {
		return authAttempt{token: token, kind: "api_key"}, true
	}
	providerRuntimeRegistry.mu.Lock()
	defer providerRuntimeRegistry.mu.Unlock()
	state := providerRuntimeRegistry.api[name]
	if state.API.TokenMasked == "" {
		state.API.TokenMasked = maskToken(token)
	}
	if state.API.HealthScore <= 0 {
		state.API.HealthScore = 100
	}
	if state.API.CooldownUntil != "" {
		if until, err := time.Parse(time.RFC3339, state.API.CooldownUntil); err == nil {
			if time.Now().Before(until) {
				providerRuntimeRegistry.api[name] = state
				return authAttempt{}, false
			}
		}
		state.API.CooldownUntil = ""
	}
	providerRuntimeRegistry.api[name] = state
	return authAttempt{token: token, kind: "api_key"}, true
}

func providerAPIHealth(name string) int {
	providerRuntimeRegistry.mu.Lock()
	defer providerRuntimeRegistry.mu.Unlock()
	state := providerRuntimeRegistry.api[name]
	if state.API.HealthScore <= 0 {
		return 100
	}
	return state.API.HealthScore
}

func maskToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return value[:2] + "***"
	}
	return value[:4] + "***" + value[len(value)-4:]
}

func appendRuntimeEvent(events []providerRuntimeEvent, event providerRuntimeEvent, limit int) []providerRuntimeEvent {
	out := append([]providerRuntimeEvent{event}, events...)
	if limit <= 0 {
		limit = 8
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (p *HTTPProvider) recordProviderHit(attempt authAttempt, reason string) {
	name := strings.TrimSpace(p.providerName)
	if name == "" {
		return
	}
	target := ""
	if attempt.kind == "api_key" {
		target = maskToken(p.apiKey)
	} else if attempt.session != nil {
		target = firstNonEmpty(attempt.session.Email, attempt.session.AccountID, attempt.session.FilePath)
	}
	event := providerRuntimeEvent{
		When:   time.Now().Format(time.RFC3339),
		Kind:   attempt.kind,
		Target: target,
		Reason: reason,
	}
	providerRuntimeRegistry.mu.Lock()
	defer providerRuntimeRegistry.mu.Unlock()
	state := providerRuntimeRegistry.api[name]
	state.RecentHits = appendRuntimeEvent(state.RecentHits, event, runtimeEventLimit(state))
	state.LastSuccess = &event
	persistProviderRuntimeLocked(name, state)
	providerRuntimeRegistry.api[name] = state
}

func recordProviderOAuthError(providerName string, session *oauthSession, reason oauthFailureReason) {
	name := strings.TrimSpace(providerName)
	if name == "" || session == nil {
		return
	}
	providerRuntimeRegistry.mu.Lock()
	defer providerRuntimeRegistry.mu.Unlock()
	state := providerRuntimeRegistry.api[name]
	state.RecentErrors = appendRuntimeEvent(state.RecentErrors, providerRuntimeEvent{
		When:   time.Now().Format(time.RFC3339),
		Kind:   "oauth",
		Target: firstNonEmpty(session.Email, session.AccountID, session.FilePath),
		Reason: string(reason),
	}, runtimeEventLimit(state))
	persistProviderRuntimeLocked(name, state)
	providerRuntimeRegistry.api[name] = state
}

func ClearProviderAPICooldown(providerName string) {
	name := strings.TrimSpace(providerName)
	if name == "" {
		return
	}
	providerRuntimeRegistry.mu.Lock()
	defer providerRuntimeRegistry.mu.Unlock()
	state := providerRuntimeRegistry.api[name]
	target := state.API.TokenMasked
	state.API.CooldownUntil = ""
	state.RecentChanges = appendRuntimeEvent(state.RecentChanges, providerRuntimeEvent{
		When:   time.Now().Format(time.RFC3339),
		Kind:   "api_key",
		Target: target,
		Reason: "manual_clear_api_cooldown",
		Detail: "api key cooldown cleared from runtime panel",
	}, runtimeEventLimit(state))
	persistProviderRuntimeLocked(name, state)
	providerRuntimeRegistry.api[name] = state
}

func ClearProviderRuntimeHistory(providerName string) {
	name := strings.TrimSpace(providerName)
	if name == "" {
		return
	}
	providerRuntimeRegistry.mu.Lock()
	defer providerRuntimeRegistry.mu.Unlock()
	state := providerRuntimeRegistry.api[name]
	state.RecentHits = nil
	state.RecentErrors = nil
	state.RecentChanges = nil
	state.LastSuccess = nil
	if state.Persist.Enabled && strings.TrimSpace(state.Persist.File) != "" {
		_ = os.Remove(state.Persist.File)
	}
	providerRuntimeRegistry.api[name] = state
}

func runtimeEventLimit(state providerRuntimeState) int {
	if state.Persist.MaxEvents > 0 {
		return state.Persist.MaxEvents
	}
	return 8
}

func runtimeHistoryMax(pc config.ProviderConfig) int {
	if pc.RuntimeHistoryMax > 0 {
		return pc.RuntimeHistoryMax
	}
	return 24
}

func runtimeHistoryFile(name string, pc config.ProviderConfig) string {
	if file := strings.TrimSpace(pc.RuntimeHistoryFile); file != "" {
		return file
	}
	return filepath.Join(config.GetConfigDir(), "runtime", "providers", strings.TrimSpace(name)+".json")
}

func loadPersistedProviderRuntimeLocked(name string, state *providerRuntimeState) {
	if state == nil || !state.Persist.Enabled || strings.TrimSpace(state.Persist.File) == "" {
		return
	}
	raw, err := os.ReadFile(state.Persist.File)
	if err != nil {
		if os.IsNotExist(err) {
			state.Persist.Loaded = true
		}
		return
	}
	var persisted providerRuntimeState
	if err := json.Unmarshal(raw, &persisted); err != nil {
		state.Persist.Loaded = true
		return
	}
	if state.API == (providerAPIRuntimeState{}) {
		state.API = persisted.API
	}
	if len(state.RecentHits) == 0 {
		state.RecentHits = persisted.RecentHits
	}
	if len(state.RecentErrors) == 0 {
		state.RecentErrors = persisted.RecentErrors
	}
	if len(state.RecentChanges) == 0 {
		state.RecentChanges = persisted.RecentChanges
	}
	if state.LastSuccess == nil && persisted.LastSuccess != nil {
		last := *persisted.LastSuccess
		state.LastSuccess = &last
	}
	if len(state.CandidateOrder) == 0 {
		state.CandidateOrder = persisted.CandidateOrder
	}
	state.Persist.Loaded = true
}

func persistProviderRuntimeLocked(name string, state providerRuntimeState) {
	if !state.Persist.Enabled || strings.TrimSpace(state.Persist.File) == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(state.Persist.File), 0o700); err != nil {
		return
	}
	payload := providerRuntimeState{
		API:            state.API,
		RecentHits:     trimRuntimeEvents(state.RecentHits, runtimeEventLimit(state)),
		RecentErrors:   trimRuntimeEvents(state.RecentErrors, runtimeEventLimit(state)),
		RecentChanges:  trimRuntimeEvents(state.RecentChanges, runtimeEventLimit(state)),
		LastSuccess:    state.LastSuccess,
		CandidateOrder: state.CandidateOrder,
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(state.Persist.File, raw, 0o600)
}

func trimRuntimeEvents(events []providerRuntimeEvent, limit int) []providerRuntimeEvent {
	if limit <= 0 || len(events) <= limit {
		return events
	}
	return events[:limit]
}

func eventTimeUnix(event providerRuntimeEvent) int64 {
	when, err := time.Parse(time.RFC3339, strings.TrimSpace(event.When))
	if err != nil {
		return 0
	}
	return when.Unix()
}

func filterRuntimeEvents(events []providerRuntimeEvent, query ProviderRuntimeQuery) []providerRuntimeEvent {
	if len(events) == 0 {
		return nil
	}
	kind := strings.TrimSpace(query.EventKind)
	reason := strings.TrimSpace(query.Reason)
	target := strings.ToLower(strings.TrimSpace(query.Target))
	var cutoff time.Time
	if query.Window > 0 {
		cutoff = time.Now().Add(-query.Window)
	}
	filtered := make([]providerRuntimeEvent, 0, len(events))
	for _, event := range events {
		if !cutoff.IsZero() {
			when, err := time.Parse(time.RFC3339, strings.TrimSpace(event.When))
			if err != nil || when.Before(cutoff) {
				continue
			}
		}
		if kind != "" && !strings.EqualFold(strings.TrimSpace(event.Kind), kind) {
			continue
		}
		if reason != "" && !strings.Contains(strings.ToLower(strings.TrimSpace(event.Reason)), strings.ToLower(reason)) {
			continue
		}
		if target != "" && !strings.Contains(strings.ToLower(strings.TrimSpace(event.Target)), target) && !strings.Contains(strings.ToLower(strings.TrimSpace(event.Detail)), target) {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func mergeRuntimeEvents(item map[string]interface{}, query ProviderRuntimeQuery) ([]providerRuntimeEvent, int) {
	hits, _ := item["recent_hits"].([]providerRuntimeEvent)
	errors, _ := item["recent_errors"].([]providerRuntimeEvent)
	changes, _ := item["recent_changes"].([]providerRuntimeEvent)
	merged := make([]providerRuntimeEvent, 0, len(hits)+len(errors)+len(changes))
	if !query.ChangesOnly {
		merged = append(merged, filterRuntimeEvents(hits, query)...)
		merged = append(merged, filterRuntimeEvents(errors, query)...)
	}
	merged = append(merged, filterRuntimeEvents(changes, query)...)
	desc := !strings.EqualFold(strings.TrimSpace(query.Sort), "asc")
	for i := 0; i < len(merged); i++ {
		for j := i + 1; j < len(merged); j++ {
			left := eventTimeUnix(merged[i])
			right := eventTimeUnix(merged[j])
			swap := right > left
			if !desc {
				swap = right < left
			}
			if swap {
				merged[i], merged[j] = merged[j], merged[i]
			}
		}
	}
	start := query.Cursor
	if start < 0 {
		start = 0
	}
	if start > len(merged) {
		start = len(merged)
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	end := start + limit
	if end > len(merged) {
		end = len(merged)
	}
	nextCursor := 0
	if end < len(merged) {
		nextCursor = end
	}
	return merged[start:end], nextCursor
}

func matchesProviderCandidateFilters(item map[string]interface{}, query ProviderRuntimeQuery) bool {
	if query.HealthBelow <= 0 && query.CooldownBefore.IsZero() {
		return true
	}
	apiState, _ := item["api_state"].(providerAPIRuntimeState)
	candidates, _ := item["candidate_order"].([]providerRuntimeCandidate)
	if query.HealthBelow > 0 {
		if runtimeHealthValue(apiState.HealthScore) < query.HealthBelow {
			return true
		}
		for _, candidate := range candidates {
			if runtimeHealthValue(candidate.HealthScore) < query.HealthBelow {
				return true
			}
		}
	}
	if !query.CooldownBefore.IsZero() {
		values := []string{apiState.CooldownUntil}
		for _, candidate := range candidates {
			values = append(values, candidate.CooldownUntil)
		}
		for _, value := range values {
			if strings.TrimSpace(value) == "" {
				continue
			}
			until, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
			if err == nil && until.Before(query.CooldownBefore) {
				return true
			}
		}
	}
	return false
}

func providerInCooldown(item map[string]interface{}) bool {
	apiState, _ := item["api_state"].(providerAPIRuntimeState)
	if cooldownActive(apiState.CooldownUntil) {
		return true
	}
	candidates, _ := item["candidate_order"].([]providerRuntimeCandidate)
	for _, candidate := range candidates {
		if cooldownActive(candidate.CooldownUntil) {
			return true
		}
	}
	return false
}

func cooldownActive(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	until, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	return err == nil && time.Now().Before(until)
}

func buildProviderCandidateOrder(_ string, pc config.ProviderConfig, accounts []OAuthAccountInfo, api providerAPIRuntimeState) []providerRuntimeCandidate {
	authMode := strings.ToLower(strings.TrimSpace(pc.Auth))
	apiCandidate := providerRuntimeCandidate{
		Kind:          "api_key",
		Target:        maskToken(pc.APIKey),
		Available:     strings.TrimSpace(pc.APIKey) != "",
		Status:        "ready",
		CooldownUntil: strings.TrimSpace(api.CooldownUntil),
		HealthScore:   runtimeHealthValue(api.HealthScore),
		FailureCount:  api.FailureCount,
	}
	if strings.TrimSpace(apiCandidate.CooldownUntil) != "" {
		if until, err := time.Parse(time.RFC3339, apiCandidate.CooldownUntil); err == nil && time.Now().Before(until) {
			apiCandidate.Available = false
			apiCandidate.Status = "cooldown"
		}
	}
	oauthAvailable := make([]providerRuntimeCandidate, 0, len(accounts))
	oauthUnavailable := make([]providerRuntimeCandidate, 0, len(accounts))
	for _, account := range accounts {
		candidate := providerRuntimeCandidate{
			Kind:          "oauth",
			Target:        firstNonEmpty(account.Email, account.AccountID, account.CredentialFile),
			Available:     true,
			Status:        "ready",
			CooldownUntil: strings.TrimSpace(account.CooldownUntil),
			HealthScore:   runtimeHealthValue(account.HealthScore),
			FailureCount:  account.FailureCount,
		}
		if strings.TrimSpace(candidate.CooldownUntil) != "" {
			if until, err := time.Parse(time.RFC3339, candidate.CooldownUntil); err == nil && time.Now().Before(until) {
				candidate.Available = false
				candidate.Status = "cooldown"
			}
		}
		if candidate.Available {
			oauthAvailable = append(oauthAvailable, candidate)
		} else {
			oauthUnavailable = append(oauthUnavailable, candidate)
		}
	}
	sortRuntimeCandidates(oauthAvailable)
	sortRuntimeCandidates(oauthUnavailable)
	out := make([]providerRuntimeCandidate, 0, 1+len(accounts))
	switch authMode {
	case "oauth":
		out = append(out, oauthAvailable...)
	case "hybrid":
		if apiCandidate.Target != "" && apiCandidate.Available {
			out = append(out, apiCandidate)
		}
		out = append(out, oauthAvailable...)
	case "none":
	default:
		if apiCandidate.Target != "" {
			out = append(out, apiCandidate)
		}
	}
	if authMode == "hybrid" {
		if apiCandidate.Target != "" && !apiCandidate.Available {
			out = append(out, apiCandidate)
		}
		out = append(out, oauthUnavailable...)
	} else if authMode == "oauth" {
		out = append(out, oauthUnavailable...)
	}
	return out
}

func runtimeHealthValue(value int) int {
	if value <= 0 {
		return 100
	}
	return value
}

func sortRuntimeCandidates(items []providerRuntimeCandidate) {
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].HealthScore > items[i].HealthScore || (items[j].HealthScore == items[i].HealthScore && items[j].Target < items[i].Target) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func providerCandidatesEqual(left, right []providerRuntimeCandidate) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].Kind != right[i].Kind ||
			left[i].Target != right[i].Target ||
			left[i].Available != right[i].Available ||
			left[i].Status != right[i].Status ||
			left[i].CooldownUntil != right[i].CooldownUntil ||
			left[i].HealthScore != right[i].HealthScore ||
			left[i].FailureCount != right[i].FailureCount {
			return false
		}
	}
	return true
}

func summarizeCandidate(candidate providerRuntimeCandidate) string {
	target := strings.TrimSpace(candidate.Target)
	if target == "" {
		target = "-"
	}
	return strings.TrimSpace(candidate.Kind) + ":" + target
}

func candidateOrderChangeDetail(before, after []providerRuntimeCandidate) string {
	if len(before) == 0 && len(after) == 0 {
		return ""
	}
	beforeTop := "-"
	afterTop := "-"
	if len(before) > 0 {
		beforeTop = summarizeCandidate(before[0])
	}
	if len(after) > 0 {
		afterTop = summarizeCandidate(after[0])
	}
	beforeOrder := make([]string, 0, len(before))
	for _, item := range before {
		beforeOrder = append(beforeOrder, summarizeCandidate(item))
	}
	afterOrder := make([]string, 0, len(after))
	for _, item := range after {
		afterOrder = append(afterOrder, summarizeCandidate(item))
	}
	return fmt.Sprintf("top %s -> %s | order [%s] -> [%s]", beforeTop, afterTop, strings.Join(beforeOrder, " > "), strings.Join(afterOrder, " > "))
}

func GetProviderRuntimeSnapshot(cfg *config.Config) map[string]interface{} {
	if cfg == nil {
		return map[string]interface{}{"items": []interface{}{}}
	}
	items := make([]map[string]interface{}, 0)
	configs := getAllProviderConfigs(cfg)
	for name, pc := range configs {
		ConfigureProviderRuntime(name, pc)
		providerRuntimeRegistry.mu.Lock()
		state := providerRuntimeRegistry.api[name]
		providerRuntimeRegistry.mu.Unlock()
		item := map[string]interface{}{
			"name":           name,
			"auth":           strings.TrimSpace(pc.Auth),
			"api_base":       strings.TrimSpace(pc.APIBase),
			"api_state":      state.API,
			"recent_hits":    state.RecentHits,
			"recent_errors":  state.RecentErrors,
			"recent_changes": state.RecentChanges,
			"last_success":   state.LastSuccess,
		}
		candidateOrder := state.CandidateOrder
		if strings.EqualFold(strings.TrimSpace(pc.Auth), "oauth") || strings.EqualFold(strings.TrimSpace(pc.Auth), "hybrid") {
			if mgr, err := NewOAuthLoginManager(pc, time.Duration(maxInt(pc.TimeoutSec, 90))*time.Second); err == nil {
				if accounts, err := mgr.ListAccounts(); err == nil {
					item["oauth_accounts"] = accounts
					candidateOrder = buildProviderCandidateOrder(name, pc, accounts, state.API)
				}
			}
		} else if len(candidateOrder) == 0 && strings.TrimSpace(pc.APIKey) != "" {
			candidateOrder = buildProviderCandidateOrder(name, pc, nil, state.API)
		}
		if len(candidateOrder) > 0 {
			providerRuntimeRegistry.mu.Lock()
			state = providerRuntimeRegistry.api[name]
			state.CandidateOrder = candidateOrder
			persistProviderRuntimeLocked(name, state)
			providerRuntimeRegistry.api[name] = state
			providerRuntimeRegistry.mu.Unlock()
		}
		item["candidate_order"] = candidateOrder
		items = append(items, item)
	}
	return map[string]interface{}{"items": items}
}

func GetProviderRuntimeView(cfg *config.Config, query ProviderRuntimeQuery) map[string]interface{} {
	if cfg == nil {
		return map[string]interface{}{"items": []interface{}{}}
	}
	snapshot := GetProviderRuntimeSnapshot(cfg)
	rawItems, _ := snapshot["items"].([]map[string]interface{})
	if len(rawItems) == 0 {
		return map[string]interface{}{"items": []interface{}{}}
	}
	filterName := strings.TrimSpace(query.Provider)
	items := make([]map[string]interface{}, 0, len(rawItems))
	for _, item := range rawItems {
		name := strings.TrimSpace(fmt.Sprintf("%v", item["name"]))
		if filterName != "" && name != filterName {
			continue
		}
		next := map[string]interface{}{}
		for key, value := range item {
			next[key] = value
		}
		hits, _ := item["recent_hits"].([]providerRuntimeEvent)
		errors, _ := item["recent_errors"].([]providerRuntimeEvent)
		changes, _ := item["recent_changes"].([]providerRuntimeEvent)
		next["recent_hits"] = filterRuntimeEvents(hits, query)
		next["recent_errors"] = filterRuntimeEvents(errors, query)
		next["recent_changes"] = filterRuntimeEvents(changes, query)
		if query.ChangesOnly {
			next["recent_hits"] = []providerRuntimeEvent{}
			next["recent_errors"] = []providerRuntimeEvent{}
		}
		events, nextCursor := mergeRuntimeEvents(next, query)
		next["events"] = events
		next["next_cursor"] = nextCursor
		if !matchesProviderCandidateFilters(next, query) {
			continue
		}
		items = append(items, next)
	}
	return map[string]interface{}{"items": items}
}

func GetProviderRuntimeSummary(cfg *config.Config, query ProviderRuntimeQuery) ProviderRuntimeSummary {
	snapshot := GetProviderRuntimeSnapshot(cfg)
	rawItems, _ := snapshot["items"].([]map[string]interface{})
	summary := ProviderRuntimeSummary{Providers: make([]ProviderRuntimeSummaryItem, 0, len(rawItems))}
	for _, item := range rawItems {
		name := strings.TrimSpace(fmt.Sprintf("%v", item["name"]))
		if strings.TrimSpace(query.Provider) != "" && name != strings.TrimSpace(query.Provider) {
			continue
		}
		auth := strings.TrimSpace(fmt.Sprintf("%v", item["auth"]))
		apiState, _ := item["api_state"].(providerAPIRuntimeState)
		accounts, _ := item["oauth_accounts"].([]OAuthAccountInfo)
		candidates, _ := item["candidate_order"].([]providerRuntimeCandidate)
		errors, _ := item["recent_errors"].([]providerRuntimeEvent)
		changes, _ := item["recent_changes"].([]providerRuntimeEvent)
		errors = filterRuntimeEvents(errors, query)
		changes = filterRuntimeEvents(changes, query)
		lastSuccess, _ := item["last_success"].(*providerRuntimeEvent)
		inCooldown := providerInCooldown(item)
		lowHealth := matchesProviderCandidateFilters(item, ProviderRuntimeQuery{HealthBelow: maxInt(query.HealthBelow, 1)})
		hasRecentErrors := len(errors) > 0
		lastError := latestProviderRuntimeEvent(errors)
		topChangedAt := latestRuntimeChangeAt(changes, "candidate_order_changed")
		status := providerRuntimeSummaryStatus(inCooldown, lowHealth, hasRecentErrors)
		providerItem := ProviderRuntimeSummaryItem{
			Name:                  name,
			Auth:                  auth,
			Status:                status,
			APIState:              apiState,
			OAuthAccounts:         accounts,
			CandidateOrder:        candidates,
			LastSuccess:           lastSuccess,
			LastError:             lastError,
			TopCandidateChangedAt: topChangedAt,
			InCooldown:            inCooldown,
			LowHealth:             lowHealth,
			HasRecentErrors:       hasRecentErrors,
		}
		if lastSuccess != nil {
			providerItem.LastSuccessAt = strings.TrimSpace(lastSuccess.When)
			if when := parseRuntimeEventTime(*lastSuccess); !when.IsZero() {
				providerItem.StaleForSec = int64(time.Since(when).Seconds())
			}
		} else {
			providerItem.StaleForSec = -1
		}
		if lastError != nil {
			providerItem.LastErrorAt = strings.TrimSpace(lastError.When)
			providerItem.LastErrorReason = strings.TrimSpace(lastError.Reason)
		}
		if len(candidates) > 0 {
			top := candidates[0]
			providerItem.TopCandidate = &top
		}
		summary.TotalProviders++
		switch status {
		case "critical":
			summary.Critical++
		case "degraded":
			summary.Degraded++
		default:
			summary.Healthy++
		}
		if inCooldown {
			summary.InCooldown++
		}
		if lowHealth {
			summary.LowHealth++
		}
		if hasRecentErrors {
			summary.RecentErrors++
		}
		if inCooldown || lowHealth || hasRecentErrors || strings.TrimSpace(query.Provider) != "" {
			summary.Providers = append(summary.Providers, providerItem)
		}
	}
	return summary
}

func latestProviderRuntimeEvent(events []providerRuntimeEvent) *providerRuntimeEvent {
	if len(events) == 0 {
		return nil
	}
	best := events[0]
	bestTime := eventTimeUnix(best)
	for i := 1; i < len(events); i++ {
		currentTime := eventTimeUnix(events[i])
		if currentTime > bestTime {
			best = events[i]
			bestTime = currentTime
		}
	}
	copyEvent := best
	return &copyEvent
}

func latestRuntimeChangeAt(events []providerRuntimeEvent, reason string) string {
	targetReason := strings.TrimSpace(reason)
	if targetReason == "" || len(events) == 0 {
		return ""
	}
	var latest *providerRuntimeEvent
	var latestUnix int64
	for i := range events {
		if !strings.EqualFold(strings.TrimSpace(events[i].Reason), targetReason) {
			continue
		}
		currentUnix := eventTimeUnix(events[i])
		if latest == nil || currentUnix > latestUnix {
			eventCopy := events[i]
			latest = &eventCopy
			latestUnix = currentUnix
		}
	}
	if latest == nil {
		return ""
	}
	return strings.TrimSpace(latest.When)
}

func parseRuntimeEventTime(event providerRuntimeEvent) time.Time {
	when, err := time.Parse(time.RFC3339, strings.TrimSpace(event.When))
	if err != nil {
		return time.Time{}
	}
	return when
}

func providerRuntimeSummaryStatus(inCooldown, lowHealth, hasRecentErrors bool) string {
	if inCooldown || lowHealth {
		return "critical"
	}
	if hasRecentErrors {
		return "degraded"
	}
	return "healthy"
}

func RefreshProviderRuntimeNow(cfg *config.Config, providerName string, onlyExpiring bool) (*ProviderRefreshResult, error) {
	pc, err := getProviderConfigByName(cfg, providerName)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(pc.Auth), "oauth") && !strings.EqualFold(strings.TrimSpace(pc.Auth), "hybrid") {
		return nil, fmt.Errorf("provider %q does not use oauth", providerName)
	}
	manager, err := newOAuthManager(pc, time.Duration(maxInt(pc.TimeoutSec, 90))*time.Second)
	if err != nil {
		return nil, err
	}
	defer manager.bgCancel()
	manager.providerName = strings.TrimSpace(providerName)
	lead := 365 * 24 * time.Hour
	if onlyExpiring {
		lead = manager.cfg.RefreshLead
		if lead <= 0 {
			lead = 30 * time.Minute
		}
	}
	return manager.refreshExpiringSessions(context.Background(), lead)
}

func RerankProviderRuntime(cfg *config.Config, providerName string) ([]providerRuntimeCandidate, error) {
	provider, err := CreateProviderByName(cfg, providerName)
	if err != nil {
		return nil, err
	}
	httpProvider, ok := unwrapHTTPProvider(provider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support runtime rerank", providerName)
	}
	_, err = httpProvider.authAttempts(context.Background())
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "oauth session not found") {
		return nil, err
	}
	providerRuntimeRegistry.mu.Lock()
	order := append([]providerRuntimeCandidate(nil), providerRuntimeRegistry.api[strings.TrimSpace(providerName)].CandidateOrder...)
	providerRuntimeRegistry.mu.Unlock()
	return order, nil
}

func unwrapHTTPProvider(provider LLMProvider) (*HTTPProvider, bool) {
	switch typed := provider.(type) {
	case *HTTPProvider:
		return typed, true
	case *CodexProvider:
		if typed == nil {
			return nil, false
		}
		return typed.base, typed.base != nil
	case *AntigravityProvider:
		if typed == nil {
			return nil, false
		}
		return typed.base, typed.base != nil
	case *ClaudeProvider:
		if typed == nil {
			return nil, false
		}
		return typed.base, typed.base != nil
	case *QwenProvider:
		if typed == nil {
			return nil, false
		}
		return typed.base, typed.base != nil
	case *KimiProvider:
		if typed == nil {
			return nil, false
		}
		return typed.base, typed.base != nil
	default:
		return nil, false
	}
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

func parseOpenAICompatResponse(body []byte) (*LLMResponse, error) {
	var payload struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
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
		Content:      choice.Message.Content,
		FinishReason: choice.FinishReason,
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

func (p *HTTPProvider) useClaudeCompat() bool {
	if p == nil || p.oauth == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(p.oauth.cfg.Provider), defaultClaudeOAuthProvider)
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
	trimmed := strings.TrimSpace(model)
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
	return requestBody
}

func openAICompatMessages(messages []Message) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(messages))
	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		switch role {
		case "system":
			out = append(out, map[string]interface{}{"role": "system", "content": msg.Content})
		case "developer":
			out = append(out, map[string]interface{}{"role": "user", "content": msg.Content})
		case "assistant":
			item := map[string]interface{}{"role": "assistant", "content": msg.Content}
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
				"content":      msg.Content,
			})
		default:
			out = append(out, map[string]interface{}{"role": "user", "content": msg.Content})
		}
	}
	return out
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
	name := config.PrimaryProviderName(cfg)
	provider, err := CreateProviderByName(cfg, name)
	if err != nil {
		return nil, err
	}
	_, model := config.ParseProviderModelRef(cfg.Agents.Defaults.Model.Primary)
	if hp, ok := provider.(*HTTPProvider); ok && strings.TrimSpace(model) != "" {
		hp.defaultModel = strings.TrimSpace(model)
	}
	return provider, nil
}

func CreateProviderByName(cfg *config.Config, name string) (LLMProvider, error) {
	pc, err := getProviderConfigByName(cfg, name)
	if err != nil {
		return nil, err
	}
	ConfigureProviderRuntime(name, pc)
	oauthProvider := strings.ToLower(strings.TrimSpace(pc.OAuth.Provider))
	if pc.APIBase == "" && oauthProvider != defaultAntigravityOAuthProvider {
		return nil, fmt.Errorf("no API base configured for provider %q", name)
	}
	if pc.TimeoutSec <= 0 {
		return nil, fmt.Errorf("invalid timeout_sec for provider %q: %d", name, pc.TimeoutSec)
	}
	defaultModel := ""
	if len(pc.Models) > 0 {
		defaultModel = pc.Models[0]
	}
	var oauth *oauthManager
	if strings.EqualFold(strings.TrimSpace(pc.Auth), "oauth") || strings.EqualFold(strings.TrimSpace(pc.Auth), "hybrid") {
		oauth, err = newOAuthManager(pc, time.Duration(pc.TimeoutSec)*time.Second)
		if err != nil {
			return nil, err
		}
	}
	if oauthProvider == defaultAntigravityOAuthProvider {
		return NewAntigravityProvider(name, pc.APIKey, pc.APIBase, defaultModel, pc.SupportsResponsesCompact, pc.Auth, time.Duration(pc.TimeoutSec)*time.Second, oauth), nil
	}
	if oauthProvider == defaultCodexOAuthProvider {
		return NewCodexProvider(name, pc.APIKey, pc.APIBase, defaultModel, pc.SupportsResponsesCompact, pc.Auth, time.Duration(pc.TimeoutSec)*time.Second, oauth), nil
	}
	if oauthProvider == defaultClaudeOAuthProvider {
		return NewClaudeProvider(name, pc.APIKey, pc.APIBase, defaultModel, pc.SupportsResponsesCompact, pc.Auth, time.Duration(pc.TimeoutSec)*time.Second, oauth), nil
	}
	if oauthProvider == defaultQwenOAuthProvider {
		return NewQwenProvider(name, pc.APIKey, pc.APIBase, defaultModel, pc.SupportsResponsesCompact, pc.Auth, time.Duration(pc.TimeoutSec)*time.Second, oauth), nil
	}
	if oauthProvider == defaultKimiOAuthProvider {
		return NewKimiProvider(name, pc.APIKey, pc.APIBase, defaultModel, pc.SupportsResponsesCompact, pc.Auth, time.Duration(pc.TimeoutSec)*time.Second, oauth), nil
	}
	return NewHTTPProvider(name, pc.APIKey, pc.APIBase, defaultModel, pc.SupportsResponsesCompact, pc.Auth, time.Duration(pc.TimeoutSec)*time.Second, oauth), nil
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
	return config.AllProviderConfigs(cfg)
}

func getProviderConfigByName(cfg *config.Config, name string) (config.ProviderConfig, error) {
	if pc, ok := config.ProviderConfigByName(cfg, name); ok {
		return pc, nil
	}
	return config.ProviderConfig{}, fmt.Errorf("provider %q not found", strings.TrimSpace(name))
}
