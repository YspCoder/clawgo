package providers

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/YspCoder/clawgo/pkg/wsrelay"
)

var aistudioRelayRegistry struct {
	mu        sync.RWMutex
	manager   *wsrelay.Manager
	connected map[string]time.Time
	succeeded map[string]time.Time
}

func SetAIStudioRelayManager(manager *wsrelay.Manager) {
	aistudioRelayRegistry.mu.Lock()
	aistudioRelayRegistry.manager = manager
	if aistudioRelayRegistry.connected == nil {
		aistudioRelayRegistry.connected = map[string]time.Time{}
	}
	if aistudioRelayRegistry.succeeded == nil {
		aistudioRelayRegistry.succeeded = map[string]time.Time{}
	}
	aistudioRelayRegistry.mu.Unlock()
}

func getAIStudioRelayManager() *wsrelay.Manager {
	aistudioRelayRegistry.mu.RLock()
	defer aistudioRelayRegistry.mu.RUnlock()
	return aistudioRelayRegistry.manager
}

func aistudioChannelCandidates(providerName string, options map[string]interface{}) []string {
	for _, key := range []string{"aistudio_channel", "aistudio_provider", "relay_provider", "channel_id", "provider_id"} {
		if value, ok := stringOption(options, key); ok && strings.TrimSpace(value) != "" {
			return []string{strings.ToLower(strings.TrimSpace(value))}
		}
	}
	for _, key := range []string{"aistudio_channels", "channel_ids", "relay_providers"} {
		if values, ok := stringSliceOption(options, key); ok && len(values) > 0 {
			out := make([]string, 0, len(values))
			seen := map[string]struct{}{}
			for _, value := range values {
				channelID := strings.ToLower(strings.TrimSpace(value))
				if channelID == "" {
					continue
				}
				if _, exists := seen[channelID]; exists {
					continue
				}
				seen[channelID] = struct{}{}
				out = append(out, channelID)
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	if runtimeSelected := preferredAIStudioRelayChannels(); len(runtimeSelected) > 0 {
		return runtimeSelected
	}
	if fallback := strings.ToLower(strings.TrimSpace(providerName)); fallback != "" {
		return []string{fallback}
	}
	return nil
}

func preferredAIStudioRelayChannels() []string {
	providerRuntimeRegistry.mu.Lock()
	defer providerRuntimeRegistry.mu.Unlock()
	state := providerRuntimeRegistry.api["aistudio"]
	candidates := append([]providerRuntimeCandidate(nil), state.CandidateOrder...)
	sortAIStudioRelayCandidates(candidates)
	out := make([]string, 0, len(candidates))
	fallbacks := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Kind != "relay" {
			continue
		}
		target := strings.TrimSpace(candidate.Target)
		if target == "" {
			continue
		}
		fallbacks = append(fallbacks, target)
		if !candidate.Available {
			continue
		}
		if cooldownActive(candidate.CooldownUntil) {
			continue
		}
		out = append(out, target)
	}
	if len(out) > 0 {
		return out
	}
	return fallbacks
}

func NotifyAIStudioRelayConnected(channelID string) {
	channelID = strings.ToLower(strings.TrimSpace(channelID))
	if channelID == "" {
		return
	}
	aistudioRelayRegistry.mu.Lock()
	if aistudioRelayRegistry.connected == nil {
		aistudioRelayRegistry.connected = map[string]time.Time{}
	}
	if aistudioRelayRegistry.succeeded == nil {
		aistudioRelayRegistry.succeeded = map[string]time.Time{}
	}
	aistudioRelayRegistry.connected[channelID] = time.Now().UTC()
	channels := aistudioRelayChannelsLocked()
	aistudioRelayRegistry.mu.Unlock()
	updateAIStudioRelayRuntime(channels)
	recordProviderRuntimeChange("aistudio", "relay", channelID, "relay_connected", "aistudio websocket relay connected")
}

func NotifyAIStudioRelayDisconnected(channelID string, cause error) {
	channelID = strings.ToLower(strings.TrimSpace(channelID))
	if channelID == "" {
		return
	}
	aistudioRelayRegistry.mu.Lock()
	if aistudioRelayRegistry.connected != nil {
		delete(aistudioRelayRegistry.connected, channelID)
	}
	if aistudioRelayRegistry.succeeded != nil {
		delete(aistudioRelayRegistry.succeeded, channelID)
	}
	channels := aistudioRelayChannelsLocked()
	aistudioRelayRegistry.mu.Unlock()
	updateAIStudioRelayRuntime(channels)
	detail := "aistudio websocket relay disconnected"
	if cause != nil {
		detail = fmt.Sprintf("%s: %v", detail, cause)
	}
	recordProviderRuntimeChange("aistudio", "relay", channelID, "relay_disconnected", detail)
}

func updateAIStudioRelayRuntime(channels []string) {
	candidates := make([]providerRuntimeCandidate, 0, len(channels))
	for _, channelID := range channels {
		health, failures, cooldown, _ := aistudioRelayHealth(channelID)
		status := "ready"
		available := true
		if cooldown != "" {
			status = "cooldown"
			available = false
		}
		candidates = append(candidates, providerRuntimeCandidate{
			Kind:          "relay",
			Target:        channelID,
			Available:     available,
			Status:        status,
			CooldownUntil: cooldown,
			HealthScore:   health,
			FailureCount:  failures,
		})
	}
	sortAIStudioRelayCandidates(candidates)
	providerRuntimeRegistry.mu.Lock()
	defer providerRuntimeRegistry.mu.Unlock()
	state := providerRuntimeRegistry.api["aistudio"]
	if !providerCandidatesEqual(state.CandidateOrder, candidates) {
		state.RecentChanges = appendRuntimeEvent(state.RecentChanges, providerRuntimeEvent{
			When:   time.Now().Format(time.RFC3339),
			Kind:   "relay",
			Target: "aistudio",
			Reason: "candidate_order_changed",
			Detail: candidateOrderChangeDetail(state.CandidateOrder, candidates),
		}, runtimeEventLimit(state))
	}
	state.CandidateOrder = candidates
	persistProviderRuntimeLocked("aistudio", state)
	providerRuntimeRegistry.api["aistudio"] = state
}

func aistudioRelayChannelsLocked() []string {
	out := make([]string, 0, len(aistudioRelayRegistry.connected))
	for channelID := range aistudioRelayRegistry.connected {
		out = append(out, channelID)
	}
	sort.Strings(out)
	return out
}

func listAIStudioRelayAccounts() []OAuthAccountInfo {
	aistudioRelayRegistry.mu.RLock()
	defer aistudioRelayRegistry.mu.RUnlock()
	if len(aistudioRelayRegistry.connected) == 0 {
		return nil
	}
	channels := aistudioRelayChannelsLocked()
	out := make([]OAuthAccountInfo, 0, len(channels))
	for _, channelID := range channels {
		connectedAt := aistudioRelayRegistry.connected[channelID]
		health, failures, cooldown, lastSuccess := aistudioRelayHealth(channelID)
		lastRefresh := connectedAt.Format(time.RFC3339)
		if !lastSuccess.IsZero() {
			lastRefresh = lastSuccess.Format(time.RFC3339)
		}
		out = append(out, OAuthAccountInfo{
			Email:         channelID,
			AccountID:     channelID,
			AccountLabel:  channelID,
			LastRefresh:   lastRefresh,
			HealthScore:   health,
			FailureCount:  failures,
			CooldownUntil: cooldown,
			PlanType:      "relay",
			QuotaSource:   "relay",
			BalanceLabel:  "connected",
		})
	}
	return out
}

func aistudioRelayHealth(channelID string) (health int, failures int, cooldown string, lastSuccess time.Time) {
	providerRuntimeRegistry.mu.Lock()
	defer providerRuntimeRegistry.mu.Unlock()
	state := providerRuntimeRegistry.api["aistudio"]
	if state.API.HealthScore <= 0 {
		health = 100
	} else {
		health = state.API.HealthScore
	}
	for _, candidate := range state.CandidateOrder {
		if candidate.Kind == "relay" && candidate.Target == channelID {
			if candidate.HealthScore > 0 {
				health = candidate.HealthScore
			}
			failures = candidate.FailureCount
			cooldown = strings.TrimSpace(candidate.CooldownUntil)
			break
		}
	}
	aistudioRelayRegistry.mu.RLock()
	lastSuccess = aistudioRelayRegistry.succeeded[channelID]
	aistudioRelayRegistry.mu.RUnlock()
	return health, failures, cooldown, lastSuccess
}

func recordAIStudioRelaySuccess(channelID string) {
	aistudioRelayRegistry.mu.Lock()
	if aistudioRelayRegistry.succeeded == nil {
		aistudioRelayRegistry.succeeded = map[string]time.Time{}
	}
	aistudioRelayRegistry.succeeded[channelID] = time.Now().UTC()
	aistudioRelayRegistry.mu.Unlock()
	updateAIStudioRelayAttempt(channelID, "", true)
}

func recordAIStudioRelayFailure(channelID string, err error) {
	reason := "relay_error"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		reason = strings.TrimSpace(err.Error())
	}
	updateAIStudioRelayAttempt(channelID, reason, false)
}

func updateAIStudioRelayAttempt(channelID, reason string, success bool) {
	channelID = strings.ToLower(strings.TrimSpace(channelID))
	if channelID == "" {
		return
	}
	now := time.Now()
	providerRuntimeRegistry.mu.Lock()
	defer providerRuntimeRegistry.mu.Unlock()
	state := providerRuntimeRegistry.api["aistudio"]
	if state.API.HealthScore <= 0 {
		state.API.HealthScore = 100
	}
	found := false
	for i := range state.CandidateOrder {
		candidate := &state.CandidateOrder[i]
		if candidate.Kind != "relay" || candidate.Target != channelID {
			continue
		}
		found = true
		if success {
			candidate.HealthScore = minInt(100, maxInt(candidate.HealthScore, 100)+3)
			candidate.FailureCount = 0
			candidate.CooldownUntil = ""
			candidate.Available = true
			candidate.Status = "ready"
		} else {
			if candidate.HealthScore <= 0 {
				candidate.HealthScore = 100
			}
			candidate.HealthScore = maxInt(1, candidate.HealthScore-20)
			candidate.FailureCount++
			candidate.CooldownUntil = now.Add(5 * time.Minute).Format(time.RFC3339)
			candidate.Available = false
			candidate.Status = "cooldown"
			state.RecentErrors = appendRuntimeEvent(state.RecentErrors, providerRuntimeEvent{
				When:   now.Format(time.RFC3339),
				Kind:   "relay",
				Target: channelID,
				Reason: reason,
			}, runtimeEventLimit(state))
		}
		break
	}
	if !found {
		candidate := providerRuntimeCandidate{
			Kind:        "relay",
			Target:      channelID,
			Available:   success,
			Status:      "ready",
			HealthScore: 100,
		}
		if !success {
			candidate.Status = "cooldown"
			candidate.Available = false
			candidate.FailureCount = 1
			candidate.HealthScore = 80
			candidate.CooldownUntil = now.Add(5 * time.Minute).Format(time.RFC3339)
			state.RecentErrors = appendRuntimeEvent(state.RecentErrors, providerRuntimeEvent{
				When:   now.Format(time.RFC3339),
				Kind:   "relay",
				Target: channelID,
				Reason: reason,
			}, runtimeEventLimit(state))
		}
		state.CandidateOrder = append(state.CandidateOrder, candidate)
		sortRuntimeCandidates(state.CandidateOrder)
	}
	if success {
		state.API.HealthScore = minInt(100, state.API.HealthScore+2)
		state.API.CooldownUntil = ""
		state.LastSuccess = &providerRuntimeEvent{
			When:   now.Format(time.RFC3339),
			Kind:   "relay",
			Target: channelID,
			Reason: "success",
		}
		state.RecentHits = appendRuntimeEvent(state.RecentHits, *state.LastSuccess, runtimeEventLimit(state))
	} else {
		state.API.HealthScore = maxInt(1, state.API.HealthScore-10)
		state.API.FailureCount++
		state.API.LastFailure = reason
		state.API.CooldownUntil = now.Add(5 * time.Minute).Format(time.RFC3339)
	}
	persistProviderRuntimeLocked("aistudio", state)
	providerRuntimeRegistry.api["aistudio"] = state
}

func sortAIStudioRelayCandidates(items []providerRuntimeCandidate) {
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		if left.Available != right.Available {
			return left.Available
		}
		leftSuccess := aistudioRelayLastSuccess(left.Target)
		rightSuccess := aistudioRelayLastSuccess(right.Target)
		if !leftSuccess.Equal(rightSuccess) {
			return leftSuccess.After(rightSuccess)
		}
		if left.HealthScore != right.HealthScore {
			return left.HealthScore > right.HealthScore
		}
		return left.Target < right.Target
	})
}

func aistudioRelayLastSuccess(channelID string) time.Time {
	aistudioRelayRegistry.mu.RLock()
	defer aistudioRelayRegistry.mu.RUnlock()
	if aistudioRelayRegistry.succeeded == nil {
		return time.Time{}
	}
	return aistudioRelayRegistry.succeeded[channelID]
}
