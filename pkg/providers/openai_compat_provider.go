package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func openAICompatDefaultModel(base *HTTPProvider) string {
	if base == nil {
		return ""
	}
	return base.GetDefaultModel()
}

type openAICompatHooks interface {
	beforeAttempt(attempt authAttempt) (int, []byte, string, bool)
	endpoint(base *HTTPProvider, attempt authAttempt, path string) string
	classifyFailure(status int, body []byte) (int, oauthFailureReason, bool, *time.Duration)
	afterFailure(base *HTTPProvider, attempt authAttempt, reason oauthFailureReason, retryAfter *time.Duration)
}

func doOpenAICompatJSONWithAttempts(ctx context.Context, base *HTTPProvider, path string, payload map[string]interface{}, hooks openAICompatHooks) ([]byte, int, string, error) {
	if base == nil {
		return nil, 0, "", fmt.Errorf("provider not configured")
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to marshal request: %w", err)
	}
	attempts, err := base.authAttempts(ctx)
	if err != nil {
		return nil, 0, "", err
	}
	var lastBody []byte
	var lastStatus int
	var lastType string
	for _, attempt := range attempts {
		attemptEndpoint := endpointFor(base.compatBase(), path)
		if hooks != nil {
			attemptEndpoint = hooks.endpoint(base, attempt, path)
		}
		if hooks != nil {
			if status, body, contentType, blocked := hooks.beforeAttempt(attempt); blocked {
				lastBody, lastStatus, lastType = body, status, contentType
				_, reason, retry, retryAfter := hooks.classifyFailure(status, body)
				if retry {
					hooks.afterFailure(base, attempt, reason, retryAfter)
					continue
				}
				return body, status, contentType, nil
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, attemptEndpoint, bytes.NewReader(jsonData))
		if err != nil {
			return nil, 0, "", fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		applyAttemptAuth(req, attempt)
		applyAttemptProviderHeaders(req, attempt, base, false)

		body, status, contentType, err := base.doJSONAttempt(req, attempt)
		if err != nil {
			return nil, 0, "", err
		}
		mappedStatus := status
		reason, retry := oauthFailureReason(""), false
		var retryAfter *time.Duration
		if hooks != nil {
			mappedStatus, reason, retry, retryAfter = hooks.classifyFailure(status, body)
		} else {
			reason, retry = classifyOAuthFailure(status, body)
		}
		if !retry {
			base.markAttemptSuccess(attempt)
			return body, mappedStatus, contentType, nil
		}
		lastBody, lastStatus, lastType = body, mappedStatus, contentType
		if hooks != nil {
			hooks.afterFailure(base, attempt, reason, retryAfter)
		} else {
			applyAttemptFailure(base, attempt, reason, nil)
		}
	}
	return lastBody, lastStatus, lastType, nil
}

func doOpenAICompatStreamWithAttempts(ctx context.Context, base *HTTPProvider, path string, payload map[string]interface{}, onDelta func(string), hooks openAICompatHooks) ([]byte, int, string, error) {
	if base == nil {
		return nil, 0, "", fmt.Errorf("provider not configured")
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to marshal request: %w", err)
	}
	attempts, err := base.authAttempts(ctx)
	if err != nil {
		return nil, 0, "", err
	}
	var lastBody []byte
	var lastStatus int
	var lastType string
	for _, attempt := range attempts {
		attemptEndpoint := endpointFor(base.compatBase(), path)
		if hooks != nil {
			attemptEndpoint = hooks.endpoint(base, attempt, path)
		}
		if hooks != nil {
			if status, body, contentType, blocked := hooks.beforeAttempt(attempt); blocked {
				lastBody, lastStatus, lastType = body, status, contentType
				_, reason, retry, retryAfter := hooks.classifyFailure(status, body)
				if retry {
					hooks.afterFailure(base, attempt, reason, retryAfter)
					continue
				}
				return body, status, contentType, nil
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, attemptEndpoint, bytes.NewReader(jsonData))
		if err != nil {
			return nil, 0, "", fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		applyAttemptAuth(req, attempt)
		applyAttemptProviderHeaders(req, attempt, base, true)

		body, status, contentType, _, err := base.doStreamAttempt(req, attempt, func(event string) {
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
		}, nil, nil)
		if err != nil {
			return nil, 0, "", err
		}
		mappedStatus := status
		reason, retry := oauthFailureReason(""), false
		var retryAfter *time.Duration
		if hooks != nil {
			mappedStatus, reason, retry, retryAfter = hooks.classifyFailure(status, body)
		} else {
			reason, retry = classifyOAuthFailure(status, body)
		}
		if !retry {
			base.markAttemptSuccess(attempt)
			return body, mappedStatus, contentType, nil
		}
		lastBody, lastStatus, lastType = body, mappedStatus, contentType
		if hooks != nil {
			hooks.afterFailure(base, attempt, reason, retryAfter)
		} else {
			applyAttemptFailure(base, attempt, reason, nil)
		}
	}
	return lastBody, lastStatus, lastType, nil
}

func applyAttemptFailure(base *HTTPProvider, attempt authAttempt, reason oauthFailureReason, retryAfter *time.Duration) {
	if base == nil {
		return
	}
	if attempt.kind == "oauth" && attempt.session != nil && base.oauth != nil {
		if retryAfter != nil {
			base.oauth.mu.Lock()
			until := time.Now().Add(*retryAfter)
			if strings.TrimSpace(attempt.session.FilePath) != "" {
				base.oauth.cooldowns[strings.TrimSpace(attempt.session.FilePath)] = until
			}
			attempt.session.CooldownUntil = until.Format(time.RFC3339)
			attempt.session.FailureCount++
			attempt.session.LastFailure = string(reason)
			if attempt.session.HealthScore == 0 {
				attempt.session.HealthScore = 100
			}
			attempt.session.HealthScore = maxInt(1, attempt.session.HealthScore-healthPenaltyForReason(reason))
			base.oauth.mu.Unlock()
			recordProviderOAuthError(base.providerName, attempt.session, reason)
			recordProviderRuntimeChange(base.providerName, "oauth", firstNonEmpty(attempt.session.Email, attempt.session.AccountID, attempt.session.FilePath), "oauth_cooldown_"+string(reason), "oauth credential entered provider-specific cooldown after request failure")
			return
		}
		base.oauth.markExhausted(attempt.session, reason)
		recordProviderOAuthError(base.providerName, attempt.session, reason)
		return
	}
	if attempt.kind == "api_key" {
		base.markAPIKeyFailure(reason)
	}
}

func estimateOpenAICompatTokenCount(body map[string]interface{}) (int, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("failed to encode request for token count: %w", err)
	}
	const charsPerToken = 4
	count := (len(data) + charsPerToken - 1) / charsPerToken
	if count < 1 {
		count = 1
	}
	return count, nil
}
