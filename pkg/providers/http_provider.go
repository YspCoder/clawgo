package providers

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/YspCoder/clawgo/pkg/logger"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"
)

const (
	codexCompatBaseURL    = "https://chatgpt.com/backend-api/codex"
	codexClientVersion    = "0.115.0-alpha.27"
	codexCompatOriginator = "codex-tui"
	codexCompatUserAgent  = "codex-tui/0.115.0-alpha.27 (Mac OS 26.0.1; arm64) Apple_Terminal/464"
	qwenCompatBaseURL     = "https://portal.qwen.ai/v1"
	qwenCompatUserAgent   = "QwenCode/0.10.3 (darwin; arm64)"
	kimiCompatBaseURL     = "https://api.kimi.com/coding/v1"
	kimiCompatUserAgent   = "KimiCLI/1.10.6"
)

type HTTPProvider struct {
	providerName             string
	apiKey                   string
	apiBase                  string
	defaultModel             string
	supportsResponsesCompact bool
	responsesAPI             string
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
		responsesAPI:             "responses",
		authMode:                 authMode,
		timeout:                  timeout,
		httpClient:               &http.Client{Timeout: timeout},
		oauth:                    oauth,
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
	if p.useOpenAICompatChatUpstream() || p.useConfiguredOpenAICompatChat() {
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
	if p.useOpenAICompatChatUpstream() || p.useConfiguredOpenAICompatChat() {
		return parseOpenAICompatResponse(body)
	}
	return parseResponsesAPIResponse(body)
}

func (p *HTTPProvider) postJSONStream(ctx context.Context, endpoint string, payload interface{}, onEvent func(string)) ([]byte, int, string, error) {
	result, err := p.executeStreamAttempts(ctx, endpoint, payload, nil, onEvent)
	if err != nil {
		return nil, 0, "", err
	}
	return result.Body, result.StatusCode, result.ContentType, nil
}

func (p *HTTPProvider) postJSON(ctx context.Context, endpoint string, payload interface{}) ([]byte, int, string, error) {
	result, err := p.executeJSONAttempts(ctx, endpoint, payload, nil, classifyOAuthFailure)
	if err != nil {
		return nil, 0, "", err
	}
	return result.Body, result.StatusCode, result.ContentType, nil
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
		req.Header.Set("X-Stainless-Arch", qwenStainlessArch())
		req.Header.Set("X-Stainless-Package-Version", "5.11.0")
		req.Header.Set("X-Dashscope-Cachecontrol", "enable")
		req.Header.Set("X-Stainless-Retry-Count", "0")
		req.Header.Set("X-Stainless-Os", qwenStainlessOS())
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
		req.Header.Set("X-Msh-Device-Name", kimiDeviceName())
		req.Header.Set("X-Msh-Device-Model", kimiDeviceModel())
		req.Header.Set("X-Msh-Device-Id", kimiDeviceID(attempt.session))
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
		req.Header.Set("Originator", codexCompatOriginator)
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

func qwenStainlessArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "386":
		return "x86"
	default:
		return runtime.GOARCH
	}
}

func qwenStainlessOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "MacOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	default:
		if runtime.GOOS == "" {
			return ""
		}
		return strings.ToUpper(runtime.GOOS[:1]) + runtime.GOOS[1:]
	}
}

func (p *HTTPProvider) httpClientForAttempt(attempt authAttempt) (*http.Client, error) {
	if attempt.kind == "oauth" && attempt.session != nil && p.oauth != nil {
		client, err := p.oauth.httpClientForSession(attempt.session)
		if err != nil {
			return nil, err
		}
		if client != nil {
			return client, nil
		}
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

func (p *HTTPProvider) doStreamAttempt(req *http.Request, attempt authAttempt, onEvent func(string), options *streamAttemptOptions, cancel context.CancelFunc) ([]byte, int, string, bool, error) {
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
	lastActivity := time.Now()
	firstDeltaSeen := false
	done := make(chan struct{})
	if options != nil && cancel != nil {
		go func() {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			defer close(done)
			for {
				select {
				case <-req.Context().Done():
					return
				case <-ticker.C:
					timeout := options.firstDeltaTimeout
					if firstDeltaSeen {
						timeout = options.idleDeltaTimeout
					}
					if timeout > 0 && time.Since(lastActivity) > timeout {
						options.staleTriggered = true
						cancel()
						return
					}
				}
			}
		}()
	} else {
		close(done)
	}
	defer func() {
		<-done
	}()
	for scanner.Scan() {
		lastActivity = time.Now()
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			if len(dataLines) > 0 {
				payload := strings.Join(dataLines, "\n")
				dataLines = dataLines[:0]
				if strings.TrimSpace(payload) == "[DONE]" {
					continue
				}
				firstDeltaSeen = true
				if onEvent != nil {
					onEvent(payload)
				}
				var obj map[string]interface{}
				if err := json.Unmarshal([]byte(payload), &obj); err == nil {
					if typ := strings.TrimSpace(fmt.Sprintf("%v", obj["type"])); typ == "response.completed" {
						if respObj, ok := obj["response"]; ok {
							finalJSON = mergeStreamFinalJSON(finalJSON, respObj)
						}
					}
					if choices, ok := obj["choices"]; ok {
						finalJSON = mergeStreamFinalJSON(finalJSON, map[string]interface{}{"choices": choices, "usage": obj["usage"]})
					} else if _, ok := obj["usage"]; ok && len(finalJSON) > 0 {
						finalJSON = mergeStreamFinalJSON(finalJSON, map[string]interface{}{"usage": obj["usage"]})
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
		if options != nil && options.staleTriggered {
			return nil, resp.StatusCode, ctype, false, fmt.Errorf("stream stale: %w", err)
		}
		return nil, resp.StatusCode, ctype, false, fmt.Errorf("failed to read stream: %w", err)
	}
	if len(finalJSON) == 0 {
		finalJSON = []byte("{}")
	}
	return finalJSON, resp.StatusCode, ctype, false, nil
}

func mergeStreamFinalJSON(existing []byte, incoming interface{}) []byte {
	if incoming == nil {
		return existing
	}
	incomingMap, ok := incoming.(map[string]interface{})
	if !ok {
		data, err := json.Marshal(incoming)
		if err != nil {
			return existing
		}
		return data
	}
	if len(existing) == 0 {
		data, err := json.Marshal(incomingMap)
		if err != nil {
			return existing
		}
		return data
	}
	var merged map[string]interface{}
	if err := json.Unmarshal(existing, &merged); err != nil || merged == nil {
		merged = map[string]interface{}{}
	}
	merged = mergeStringAnyMaps(merged, incomingMap)
	data, err := json.Marshal(merged)
	if err != nil {
		return existing
	}
	return data
}

func mergeStringAnyMaps(dst, src map[string]interface{}) map[string]interface{} {
	if dst == nil {
		dst = map[string]interface{}{}
	}
	for key, value := range src {
		if value == nil {
			continue
		}
		if nestedSrc, ok := value.(map[string]interface{}); ok {
			if nestedDst, ok := dst[key].(map[string]interface{}); ok {
				dst[key] = mergeStringAnyMaps(nestedDst, nestedSrc)
				continue
			}
		}
		dst[key] = value
	}
	return dst
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

func (p *HTTPProvider) GetDefaultModel() string {
	return p.defaultModel
}

func (p *HTTPProvider) SupportsResponsesCompact() bool {
	return p != nil && p.supportsResponsesCompact
}
