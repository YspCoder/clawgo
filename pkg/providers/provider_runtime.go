package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/YspCoder/clawgo/pkg/config"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
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
		if strings.EqualFold(name, "aistudio") {
			if accounts := listAIStudioRelayAccounts(); len(accounts) > 0 {
				item["oauth_accounts"] = accounts
			}
		} else if strings.EqualFold(strings.TrimSpace(pc.Auth), "oauth") || strings.EqualFold(strings.TrimSpace(pc.Auth), "hybrid") {
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
