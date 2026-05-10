package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
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

func NewProviderExecutionError(code, message, stage string, retryable bool, source string) *ProviderExecutionError {
	return newProviderExecutionError(code, message, stage, retryable, source)
}

func ExecutionErrorCode(err error) string {
	var execErr *ProviderExecutionError
	if errors.As(err, &execErr) && execErr != nil {
		return execErr.Code
	}
	return ""
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
			execErr := newProviderExecutionError("request_failed", err.Error(), "request", false, p.providerName)
			return ProviderExecutionResult{
				StatusCode:  status,
				ContentType: ctype,
				AttemptKind: attempt.kind,
				Error:       execErr,
			}, execErr
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
		attemptCtx, cancel := context.WithCancel(ctx)
		req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, endpoint, bytes.NewReader(jsonData))
		if err != nil {
			cancel()
			return ProviderExecutionResult{Error: newProviderExecutionError("request_build_failed", err.Error(), "request", false, p.providerName)}, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		applyAttemptAuth(req, attempt)
		applyAttemptProviderHeaders(req, attempt, p, true)
		if mutate != nil {
			mutate(req, attempt)
		}
		streamOptions := streamAttemptTimeouts(ctx)
		body, status, ctype, quotaHit, err := p.doStreamAttempt(req, attempt, onEvent, streamOptions, cancel)
		cancel()
		if err != nil {
			code := "stream_failed"
			if streamOptions.staleTriggered {
				code = "stream_stale"
			}
			execErr := newProviderExecutionError(code, err.Error(), "request", true, p.providerName)
			return ProviderExecutionResult{
				StatusCode:  status,
				ContentType: ctype,
				AttemptKind: attempt.kind,
				Error:       execErr,
			}, execErr
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

type streamAttemptOptions struct {
	firstDeltaTimeout time.Duration
	idleDeltaTimeout  time.Duration
	staleTriggered    bool
}

func streamAttemptTimeouts(ctx context.Context) *streamAttemptOptions {
	opts := &streamAttemptOptions{
		firstDeltaTimeout: 20 * time.Second,
		idleDeltaTimeout:  45 * time.Second,
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 {
			first := remaining / 4
			idle := remaining / 3
			if remaining < 5*time.Second {
				first = maxDuration(100*time.Millisecond, first)
			} else if first < 5*time.Second {
				first = 5 * time.Second
			}
			if remaining < 15*time.Second {
				idle = maxDuration(250*time.Millisecond, idle)
			} else if idle < 15*time.Second {
				idle = 15 * time.Second
			}
			if first < opts.firstDeltaTimeout {
				opts.firstDeltaTimeout = first
			}
			if idle < opts.idleDeltaTimeout {
				opts.idleDeltaTimeout = idle
			}
		}
	}
	return opts
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
