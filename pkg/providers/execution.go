package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

func newProviderExecutionError(code, message, stage string, retryable bool, source string) *ProviderExecutionError {
	return &ProviderExecutionError{
		Code:      code,
		Message:   message,
		Stage:     stage,
		Retryable: retryable,
		Source:    source,
	}
}

func (p *HTTPProvider) executeJSONAttempts(ctx context.Context, endpoint string, payload interface{}, mutate func(*http.Request, authAttempt), classify func(int, []byte) (oauthFailureReason, bool)) (ProviderExecutionResult, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return ProviderExecutionResult{Error: newProviderExecutionError("marshal_failed", err.Error(), "marshal", false, p.providerName)}, fmt.Errorf("failed to marshal request: %w", err)
	}
	attempts, err := p.authAttempts(ctx)
	if err != nil {
		return ProviderExecutionResult{Error: newProviderExecutionError("auth_unavailable", err.Error(), "auth", false, p.providerName)}, err
	}
	var last ProviderExecutionResult
	for _, attempt := range attempts {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonData))
		if err != nil {
			return ProviderExecutionResult{Error: newProviderExecutionError("request_build_failed", err.Error(), "request", false, p.providerName)}, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		applyAttemptAuth(req, attempt)
		applyAttemptProviderHeaders(req, attempt, p, false)
		if mutate != nil {
			mutate(req, attempt)
		}
		body, status, ctype, err := p.doJSONAttempt(req, attempt)
		if err != nil {
			return ProviderExecutionResult{
				StatusCode:  status,
				ContentType: ctype,
				AttemptKind: attempt.kind,
				Error:       newProviderExecutionError("request_failed", err.Error(), "request", false, p.providerName),
			}, err
		}
		reason, retry := classify(status, body)
		last = ProviderExecutionResult{
			Body:        body,
			StatusCode:  status,
			ContentType: ctype,
			AttemptKind: attempt.kind,
			Retryable:   retry,
			Failure:     reason,
		}
		if !retry {
			p.markAttemptSuccess(attempt)
			return last, nil
		}
		applyAttemptFailure(p, attempt, reason, nil)
	}
	return last, nil
}

func (p *HTTPProvider) executeStreamAttempts(ctx context.Context, endpoint string, payload interface{}, mutate func(*http.Request, authAttempt), onEvent func(string)) (ProviderExecutionResult, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return ProviderExecutionResult{Error: newProviderExecutionError("marshal_failed", err.Error(), "marshal", false, p.providerName)}, fmt.Errorf("failed to marshal request: %w", err)
	}
	attempts, err := p.authAttempts(ctx)
	if err != nil {
		return ProviderExecutionResult{Error: newProviderExecutionError("auth_unavailable", err.Error(), "auth", false, p.providerName)}, err
	}
	var last ProviderExecutionResult
	for _, attempt := range attempts {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonData))
		if err != nil {
			return ProviderExecutionResult{Error: newProviderExecutionError("request_build_failed", err.Error(), "request", false, p.providerName)}, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		applyAttemptAuth(req, attempt)
		applyAttemptProviderHeaders(req, attempt, p, true)
		if mutate != nil {
			mutate(req, attempt)
		}
		body, status, ctype, quotaHit, err := p.doStreamAttempt(req, attempt, onEvent)
		if err != nil {
			return ProviderExecutionResult{
				StatusCode:  status,
				ContentType: ctype,
				AttemptKind: attempt.kind,
				Error:       newProviderExecutionError("stream_failed", err.Error(), "request", false, p.providerName),
			}, err
		}
		reason, _ := classifyOAuthFailure(status, body)
		last = ProviderExecutionResult{
			Body:        body,
			StatusCode:  status,
			ContentType: ctype,
			AttemptKind: attempt.kind,
			Retryable:   quotaHit,
			Failure:     reason,
		}
		if !quotaHit {
			p.markAttemptSuccess(attempt)
			return last, nil
		}
		applyAttemptFailure(p, attempt, reason, nil)
	}
	return last, nil
}
