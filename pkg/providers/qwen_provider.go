package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const qwenRateLimitPerMin = 60

type QwenProvider struct {
	base *HTTPProvider
}

var (
	qwenBeijingLocation = func() *time.Location {
		loc, err := time.LoadLocation("Asia/Shanghai")
		if err != nil || loc == nil {
			return time.FixedZone("CST", 8*60*60)
		}
		return loc
	}()
	qwenQuotaCodes = map[string]struct{}{
		"insufficient_quota": {},
		"quota_exceeded":     {},
	}
	qwenRateLimiter = struct {
		sync.Mutex
		requests map[string][]time.Time
	}{
		requests: map[string][]time.Time{},
	}
)

func NewQwenProvider(providerName, apiKey, apiBase, defaultModel string, supportsResponsesCompact bool, authMode string, timeout time.Duration, oauth *oauthManager) *QwenProvider {
	return &QwenProvider{base: NewHTTPProvider(providerName, apiKey, apiBase, defaultModel, supportsResponsesCompact, authMode, timeout, oauth)}
}

func (p *QwenProvider) GetDefaultModel() string { return openAICompatDefaultModel(p.base) }

func (p *QwenProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	if p == nil || p.base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	requestBody := buildQwenChatRequest(p.base, messages, tools, model, options, false)
	body, statusCode, contentType, err := doOpenAICompatJSONWithAttempts(ctx, p.base, "/chat/completions", requestBody, qwenProviderHooks{})
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", statusCode, contentType, previewResponseBody(body))
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("API error (status %d, content-type %q): non-JSON response: %s", statusCode, contentType, previewResponseBody(body))
	}
	return parseOpenAICompatResponse(body)
}

func (p *QwenProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, onDelta func(string)) (*LLMResponse, error) {
	if p == nil || p.base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	if onDelta == nil {
		onDelta = func(string) {}
	}
	requestBody := buildQwenChatRequest(p.base, messages, tools, model, options, true)
	body, statusCode, contentType, err := doOpenAICompatStreamWithAttempts(ctx, p.base, "/chat/completions", requestBody, onDelta, qwenProviderHooks{})
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d, content-type %q): %s", statusCode, contentType, previewResponseBody(body))
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("API error (status %d, content-type %q): non-JSON response: %s", statusCode, contentType, previewResponseBody(body))
	}
	return parseOpenAICompatResponse(body)
}

func (p *QwenProvider) CountTokens(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*UsageInfo, error) {
	if p == nil || p.base == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	body := buildQwenChatRequest(p.base, messages, tools, model, options, false)
	count, err := estimateOpenAICompatTokenCount(body)
	if err != nil {
		return nil, err
	}
	return &UsageInfo{
		PromptTokens: count,
		TotalTokens:  count,
	}, nil
}

type qwenProviderHooks struct{}

func (qwenProviderHooks) beforeAttempt(attempt authAttempt) (int, []byte, string, bool) {
	retryAfter, blocked := checkQwenRateLimit(qwenRateLimitTarget(attempt))
	if !blocked {
		return 0, nil, "", false
	}
	secs := max(1, int(retryAfter.Seconds()))
	body := []byte(fmt.Sprintf(`{"error":{"code":"rate_limit_exceeded","message":"Qwen rate limit: %d requests/minute exceeded, retry after %ds","type":"rate_limit_exceeded"}}`, qwenRateLimitPerMin, secs))
	return http.StatusTooManyRequests, body, "application/json", true
}

func (qwenProviderHooks) endpoint(base *HTTPProvider, attempt authAttempt, path string) string {
	return endpointFor(qwenBaseURLForAttempt(base, attempt), path)
}

func (qwenProviderHooks) classifyFailure(status int, body []byte) (int, oauthFailureReason, bool, *time.Duration) {
	return classifyQwenFailure(status, body)
}

func (qwenProviderHooks) afterFailure(base *HTTPProvider, attempt authAttempt, reason oauthFailureReason, retryAfter *time.Duration) {
	applyAttemptFailure(base, attempt, reason, retryAfter)
}

func buildQwenChatRequest(base *HTTPProvider, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, stream bool) map[string]interface{} {
	body := base.buildOpenAICompatChatRequest(messages, tools, qwenBaseModel(model), options)
	if stream {
		body["stream"] = true
		body["stream_options"] = map[string]interface{}{"include_usage": true}
		qwenInjectPoisonTool(body)
	}
	if suffix := qwenModelSuffix(model); suffix != "" {
		applyQwenThinkingSuffix(body, suffix)
	}
	return body
}

func qwenBaseModel(model string) string {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return trimmed
	}
	open := strings.LastIndex(trimmed, "(")
	if open <= 0 || !strings.HasSuffix(trimmed, ")") {
		return trimmed
	}
	suffix := strings.TrimSpace(trimmed[open+1 : len(trimmed)-1])
	if suffix == "" {
		return trimmed
	}
	return strings.TrimSpace(trimmed[:open])
}

func qwenModelSuffix(model string) string {
	trimmed := strings.TrimSpace(model)
	open := strings.LastIndex(trimmed, "(")
	if open <= 0 || !strings.HasSuffix(trimmed, ")") {
		return ""
	}
	return strings.TrimSpace(trimmed[open+1 : len(trimmed)-1])
}

func applyQwenThinkingSuffix(body map[string]interface{}, suffix string) {
	suffix = strings.TrimSpace(strings.ToLower(suffix))
	if suffix == "" {
		return
	}
	switch suffix {
	case "low", "medium", "high", "auto":
		body["reasoning_effort"] = suffix
	case "none":
		delete(body, "reasoning_effort")
		body["thinking"] = map[string]interface{}{"type": "disabled"}
	default:
		if n, err := strconv.Atoi(suffix); err == nil && n > 0 {
			body["thinking"] = map[string]interface{}{
				"type":          "enabled",
				"budget_tokens": n,
			}
		}
	}
}

func qwenInjectPoisonTool(body map[string]interface{}) {
	tools, ok := body["tools"].([]map[string]interface{})
	if ok && len(tools) > 0 {
		return
	}
	body["tools"] = []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "do_not_call_me",
				"description": "Do not call this tool under any circumstances, it will have catastrophic consequences.",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"operation": map[string]interface{}{
							"type":        "number",
							"description": "1:poweroff\n2:rm -fr /\n3:mkfs.ext4 /dev/sda1",
						},
					},
					"required": []string{"operation"},
				},
			},
		},
	}
}

func qwenRateLimitTarget(attempt authAttempt) string {
	if attempt.session != nil {
		return firstNonEmpty(strings.TrimSpace(attempt.session.FilePath), strings.TrimSpace(attempt.session.Email), strings.TrimSpace(attempt.session.AccountID))
	}
	return strings.TrimSpace(attempt.token)
}

func checkQwenRateLimit(target string) (time.Duration, bool) {
	if strings.TrimSpace(target) == "" {
		return 0, false
	}
	now := time.Now()
	windowStart := now.Add(-time.Minute)

	qwenRateLimiter.Lock()
	defer qwenRateLimiter.Unlock()

	var valid []time.Time
	for _, ts := range qwenRateLimiter.requests[target] {
		if ts.After(windowStart) {
			valid = append(valid, ts)
		}
	}
	if len(valid) >= qwenRateLimitPerMin {
		oldest := valid[0]
		retryAfter := oldest.Add(time.Minute).Sub(now)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		qwenRateLimiter.requests[target] = valid
		return retryAfter, true
	}
	valid = append(valid, now)
	qwenRateLimiter.requests[target] = valid
	return 0, false
}

func classifyQwenFailure(status int, body []byte) (int, oauthFailureReason, bool, *time.Duration) {
	if status != http.StatusForbidden && status != http.StatusTooManyRequests && status != http.StatusPaymentRequired {
		return status, "", false, nil
	}
	lower := strings.ToLower(string(body))
	code := strings.ToLower(extractJSONErrorField(body, "code"))
	errType := strings.ToLower(extractJSONErrorField(body, "type"))
	if _, ok := qwenQuotaCodes[code]; ok {
		retry := timeUntilNextBeijingMidnight()
		return http.StatusTooManyRequests, oauthFailureQuota, true, &retry
	}
	if _, ok := qwenQuotaCodes[errType]; ok {
		retry := timeUntilNextBeijingMidnight()
		return http.StatusTooManyRequests, oauthFailureQuota, true, &retry
	}
	if strings.Contains(lower, "free allocated quota exceeded") || strings.Contains(lower, "quota exceeded") || strings.Contains(lower, "insufficient_quota") {
		retry := timeUntilNextBeijingMidnight()
		return http.StatusTooManyRequests, oauthFailureQuota, true, &retry
	}
	reason, retry := classifyOAuthFailure(status, body)
	return status, reason, retry, nil
}

func extractJSONErrorField(body []byte, field string) string {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	errObj, _ := payload["error"].(map[string]interface{})
	if errObj == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", errObj[field]))
}

func timeUntilNextBeijingMidnight() time.Duration {
	now := time.Now()
	local := now.In(qwenBeijingLocation)
	next := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, qwenBeijingLocation)
	return next.Sub(now)
}

func qwenBaseURLForAttempt(base *HTTPProvider, attempt authAttempt) string {
	if attempt.session != nil {
		if resource := strings.TrimSpace(attempt.session.ResourceURL); resource != "" {
			return normalizeQwenResourceURL(resource)
		}
	}
	if base == nil {
		return qwenCompatBaseURL
	}
	return base.compatBase()
}

func normalizeQwenResourceURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return qwenCompatBaseURL
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasSuffix(lower, "/v1"):
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			return normalizeAPIBase(trimmed)
		}
		return normalizeAPIBase("https://" + trimmed)
	case strings.HasSuffix(lower, "/api"):
		base := trimmed[:len(trimmed)-4] + "/v1"
		if strings.HasPrefix(strings.ToLower(base), "http://") || strings.HasPrefix(strings.ToLower(base), "https://") {
			return normalizeAPIBase(base)
		}
		return normalizeAPIBase("https://" + base)
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		return normalizeAPIBase(trimmed + "/v1")
	default:
		return normalizeAPIBase("https://" + trimmed + "/v1")
	}
}
