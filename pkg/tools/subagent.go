package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/providers"
)

type SubagentRun struct {
	ID               string                 `json:"id"`
	Task             string                 `json:"task"`
	Label            string                 `json:"label"`
	Role             string                 `json:"role"`
	AgentID          string                 `json:"agent_id"`
	Transport        string                 `json:"transport,omitempty"`
	NodeID           string                 `json:"node_id,omitempty"`
	ParentAgentID    string                 `json:"parent_agent_id,omitempty"`
	NotifyMainPolicy string                 `json:"notify_main_policy,omitempty"`
	SessionKey       string                 `json:"session_key"`
	MemoryNS         string                 `json:"memory_ns"`
	SystemPromptFile string                 `json:"system_prompt_file,omitempty"`
	ToolAllowlist    []string               `json:"tool_allowlist,omitempty"`
	MaxRetries       int                    `json:"max_retries,omitempty"`
	RetryBackoff     int                    `json:"retry_backoff,omitempty"`
	TimeoutSec       int                    `json:"timeout_sec,omitempty"`
	MaxTaskChars     int                    `json:"max_task_chars,omitempty"`
	MaxResultChars   int                    `json:"max_result_chars,omitempty"`
	RetryCount       int                    `json:"retry_count,omitempty"`
	ThreadID         string                 `json:"thread_id,omitempty"`
	CorrelationID    string                 `json:"correlation_id,omitempty"`
	ParentRunID      string                 `json:"parent_run_id,omitempty"`
	LastMessageID    string                 `json:"last_message_id,omitempty"`
	WaitingReply     bool                   `json:"waiting_for_reply,omitempty"`
	SharedState      map[string]interface{} `json:"shared_state,omitempty"`
	OriginChannel    string                 `json:"origin_channel,omitempty"`
	OriginChatID     string                 `json:"origin_chat_id,omitempty"`
	Status           string                 `json:"status"`
	Result           string                 `json:"result,omitempty"`
	Steering         []string               `json:"steering,omitempty"`
	Created          int64                  `json:"created"`
	Updated          int64                  `json:"updated"`
}

type SubagentManager struct {
	runs               map[string]*SubagentRun
	cancelFuncs        map[string]context.CancelFunc
	waiters            map[string]map[chan struct{}]struct{}
	recoverableRunIDs  []string
	archiveAfterMinute int64
	mu                 sync.RWMutex
	provider           providers.LLMProvider
	bus                *bus.MessageBus
	workspace          string
	nextID             int
	runFunc            SubagentRunFunc
	profileStore       *SubagentProfileStore
	runStore           *SubagentRunStore
	mailboxStore       *AgentMailboxStore
}

type SubagentSpawnOptions struct {
	Task             string
	Label            string
	Role             string
	AgentID          string
	NotifyMainPolicy string
	MaxRetries       int
	RetryBackoff     int
	TimeoutSec       int
	MaxTaskChars     int
	MaxResultChars   int
	OriginChannel    string
	OriginChatID     string
	ThreadID         string
	CorrelationID    string
	ParentRunID      string
}

func NewSubagentManager(provider providers.LLMProvider, workspace string, bus *bus.MessageBus) *SubagentManager {
	store := NewSubagentProfileStore(workspace)
	runStore := NewSubagentRunStore(workspace)
	mailboxStore := NewAgentMailboxStore(workspace)
	mgr := &SubagentManager{
		runs:               make(map[string]*SubagentRun),
		cancelFuncs:        make(map[string]context.CancelFunc),
		waiters:            make(map[string]map[chan struct{}]struct{}),
		archiveAfterMinute: 60,
		provider:           provider,
		bus:                bus,
		workspace:          workspace,
		nextID:             1,
		profileStore:       store,
		runStore:           runStore,
		mailboxStore:       mailboxStore,
	}
	if runStore != nil {
		for _, run := range runStore.List() {
			mgr.runs[run.ID] = run
			if run.Status == RuntimeStatusRunning {
				mgr.recoverableRunIDs = append(mgr.recoverableRunIDs, run.ID)
			}
		}
		mgr.nextID = runStore.NextIDSeed()
	}
	go mgr.resumeRecoveredRuns()
	return mgr
}

func (sm *SubagentManager) Spawn(ctx context.Context, opts SubagentSpawnOptions) (string, error) {
	run, err := sm.spawnRun(ctx, opts)
	if err != nil {
		return "", err
	}
	desc := fmt.Sprintf("Spawned subagent for task: %s (agent=%s)", run.Task, run.AgentID)
	if run.Label != "" {
		desc = fmt.Sprintf("Spawned subagent '%s' for task: %s (agent=%s)", run.Label, run.Task, run.AgentID)
	}
	if run.Role != "" {
		desc += fmt.Sprintf(" role=%s", run.Role)
	}
	return desc, nil
}

func (sm *SubagentManager) SpawnRun(ctx context.Context, opts SubagentSpawnOptions) (*SubagentRun, error) {
	return sm.spawnRun(ctx, opts)
}

func (sm *SubagentManager) spawnRun(ctx context.Context, opts SubagentSpawnOptions) (*SubagentRun, error) {
	task := strings.TrimSpace(opts.Task)
	if task == "" {
		return nil, fmt.Errorf("task is required")
	}
	label := strings.TrimSpace(opts.Label)
	role := strings.TrimSpace(opts.Role)
	agentID := normalizeSubagentIdentifier(opts.AgentID)
	originalRole := role
	var profile *SubagentProfile
	if sm.profileStore != nil {
		if agentID != "" {
			if p, ok, err := sm.profileStore.Get(agentID); err != nil {
				return nil, err
			} else if ok {
				profile = p
			}
		} else if role != "" {
			if p, ok, err := sm.profileStore.FindByRole(role); err != nil {
				return nil, err
			} else if ok {
				profile = p
				agentID = normalizeSubagentIdentifier(p.AgentID)
			}
		}
	}
	if agentID == "" {
		agentID = normalizeSubagentIdentifier(role)
	}
	if agentID == "" {
		agentID = "default"
	}
	memoryNS := agentID
	systemPromptFile := ""
	transport := "local"
	nodeID := ""
	parentAgentID := ""
	notifyMainPolicy := "final_only"
	toolAllowlist := []string(nil)
	maxRetries := 0
	retryBackoff := 1000
	timeoutSec := 0
	maxTaskChars := 0
	maxResultChars := 0
	if profile == nil && sm.profileStore != nil {
		if p, ok, err := sm.profileStore.Get(agentID); err != nil {
			return nil, err
		} else if ok {
			profile = p
		}
	}
	if profile != nil {
		if strings.EqualFold(strings.TrimSpace(profile.Status), "disabled") {
			return nil, fmt.Errorf("subagent profile '%s' is disabled", profile.AgentID)
		}
		if label == "" {
			label = strings.TrimSpace(profile.Name)
		}
		if role == "" {
			role = strings.TrimSpace(profile.Role)
		}
		if ns := normalizeSubagentIdentifier(profile.MemoryNamespace); ns != "" {
			memoryNS = ns
		}
		transport = strings.TrimSpace(profile.Transport)
		if transport == "" {
			transport = "local"
		}
		nodeID = strings.TrimSpace(profile.NodeID)
		parentAgentID = strings.TrimSpace(profile.ParentAgentID)
		notifyMainPolicy = normalizeNotifyMainPolicy(profile.NotifyMainPolicy)
		systemPromptFile = strings.TrimSpace(profile.SystemPromptFile)
		toolAllowlist = append([]string(nil), profile.ToolAllowlist...)
		maxRetries = profile.MaxRetries
		retryBackoff = profile.RetryBackoff
		timeoutSec = profile.TimeoutSec
		maxTaskChars = profile.MaxTaskChars
		maxResultChars = profile.MaxResultChars
	}
	if opts.MaxRetries > 0 {
		maxRetries = opts.MaxRetries
	}
	if opts.RetryBackoff > 0 {
		retryBackoff = opts.RetryBackoff
	}
	if opts.TimeoutSec > 0 {
		timeoutSec = opts.TimeoutSec
	}
	if opts.MaxTaskChars > 0 {
		maxTaskChars = opts.MaxTaskChars
	}
	if opts.MaxResultChars > 0 {
		maxResultChars = opts.MaxResultChars
	}
	if maxTaskChars > 0 && len(task) > maxTaskChars {
		return nil, fmt.Errorf("task exceeds max_task_chars quota (%d > %d)", len(task), maxTaskChars)
	}
	maxRetries = normalizePositiveBound(maxRetries, 0, 8)
	retryBackoff = normalizePositiveBound(retryBackoff, 500, 120000)
	timeoutSec = normalizePositiveBound(timeoutSec, 0, 3600)
	maxTaskChars = normalizePositiveBound(maxTaskChars, 0, 400000)
	maxResultChars = normalizePositiveBound(maxResultChars, 0, 400000)
	if role == "" {
		role = originalRole
	}
	originChannel := strings.TrimSpace(opts.OriginChannel)
	originChatID := strings.TrimSpace(opts.OriginChatID)
	if raw := strings.TrimSpace(opts.NotifyMainPolicy); raw != "" {
		notifyMainPolicy = normalizeNotifyMainPolicy(raw)
	}
	threadID := strings.TrimSpace(opts.ThreadID)
	correlationID := strings.TrimSpace(opts.CorrelationID)
	parentRunID := strings.TrimSpace(opts.ParentRunID)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	runID := fmt.Sprintf("subagent-%d", sm.nextID)
	sm.nextID++
	sessionKey := buildSubagentSessionKey(agentID, runID)

	now := time.Now().UnixMilli()
	if correlationID == "" {
		correlationID = runID
	}
	if sm.mailboxStore != nil {
		thread, err := sm.mailboxStore.EnsureThread(AgentThread{
			ThreadID:     threadID,
			Owner:        "main",
			Participants: []string{"main", agentID},
			Status:       "open",
			Topic:        task,
			CreatedAt:    now,
			UpdatedAt:    now,
		})
		if err == nil {
			threadID = thread.ThreadID
		}
	}
	subagentRun := &SubagentRun{
		ID:               runID,
		Task:             task,
		Label:            label,
		Role:             role,
		AgentID:          agentID,
		Transport:        transport,
		NodeID:           nodeID,
		ParentAgentID:    parentAgentID,
		NotifyMainPolicy: notifyMainPolicy,
		SessionKey:       sessionKey,
		MemoryNS:         memoryNS,
		SystemPromptFile: systemPromptFile,
		ToolAllowlist:    toolAllowlist,
		MaxRetries:       maxRetries,
		RetryBackoff:     retryBackoff,
		TimeoutSec:       timeoutSec,
		MaxTaskChars:     maxTaskChars,
		MaxResultChars:   maxResultChars,
		RetryCount:       0,
		ThreadID:         threadID,
		CorrelationID:    correlationID,
		ParentRunID:      parentRunID,
		OriginChannel:    originChannel,
		OriginChatID:     originChatID,
		Status:           RuntimeStatusRouting,
		Created:          now,
		Updated:          now,
	}
	taskCtx, cancel := context.WithCancel(ctx)
	sm.runs[runID] = subagentRun
	sm.cancelFuncs[runID] = cancel
	sm.recordMailboxMessageLocked(subagentRun, AgentMessage{
		ThreadID:      threadID,
		FromAgent:     "main",
		ToAgent:       agentID,
		CorrelationID: correlationID,
		Type:          "task",
		Content:       task,
		RequiresReply: true,
		Status:        "queued",
		CreatedAt:     now,
	})
	sm.persistRunLocked(subagentRun, "spawned", "")

	go sm.runSubagent(taskCtx, subagentRun)
	return cloneSubagentRun(subagentRun), nil
}

func (sm *SubagentManager) runSubagent(ctx context.Context, run *SubagentRun) {
	defer func() {
		sm.mu.Lock()
		delete(sm.cancelFuncs, run.ID)
		sm.mu.Unlock()
	}()

	sm.mu.Lock()
	run.Status = RuntimeStatusRunning
	run.Created = time.Now().UnixMilli()
	run.Updated = run.Created
	sm.persistRunLocked(run, "started", "")
	sm.mu.Unlock()

	result, runErr := sm.runWithRetry(ctx, run)
	sm.mu.Lock()
	if runErr != nil {
		run.Status = RuntimeStatusFailed
		run.Result = fmt.Sprintf("Error: %v", runErr)
		run.Result = applySubagentResultQuota(run.Result, run.MaxResultChars)
		run.Updated = time.Now().UnixMilli()
		run.WaitingReply = false
		sm.recordMailboxMessageLocked(run, AgentMessage{
			ThreadID:      run.ThreadID,
			FromAgent:     run.AgentID,
			ToAgent:       "main",
			ReplyTo:       run.LastMessageID,
			CorrelationID: run.CorrelationID,
			Type:          "result",
			Content:       run.Result,
			Status:        "delivered",
			CreatedAt:     run.Updated,
		})
		sm.persistRunLocked(run, "failed", run.Result)
		sm.notifyRunWaitersLocked(run.ID)
	} else {
		run.Status = RuntimeStatusCompleted
		run.Result = applySubagentResultQuota(result, run.MaxResultChars)
		run.Updated = time.Now().UnixMilli()
		run.WaitingReply = false
		sm.recordMailboxMessageLocked(run, AgentMessage{
			ThreadID:      run.ThreadID,
			FromAgent:     run.AgentID,
			ToAgent:       "main",
			ReplyTo:       run.LastMessageID,
			CorrelationID: run.CorrelationID,
			Type:          "result",
			Content:       run.Result,
			Status:        "delivered",
			CreatedAt:     run.Updated,
		})
		sm.persistRunLocked(run, "completed", run.Result)
		sm.notifyRunWaitersLocked(run.ID)
	}
	sm.mu.Unlock()

	// 2. Result broadcast
	if sm.bus != nil && shouldNotifyMainOnFinal(run.NotifyMainPolicy, runErr, run) {
		announceContent, notifyReason := buildSubagentMainNotification(run, runErr)
		sm.bus.PublishInbound(bus.InboundMessage{
			Channel:    "system",
			SenderID:   fmt.Sprintf("subagent:%s", run.ID),
			ChatID:     fmt.Sprintf("%s:%s", run.OriginChannel, run.OriginChatID),
			SessionKey: run.SessionKey,
			Content:    announceContent,
			Metadata: map[string]string{
				"trigger":       "subagent",
				"subagent_id":   run.ID,
				"agent_id":      run.AgentID,
				"role":          run.Role,
				"session_key":   run.SessionKey,
				"memory_ns":     run.MemoryNS,
				"retry_count":   fmt.Sprintf("%d", run.RetryCount),
				"timeout_sec":   fmt.Sprintf("%d", run.TimeoutSec),
				"status":        run.Status,
				"notify_reason": notifyReason,
			},
		})
	}
}

func normalizeNotifyMainPolicy(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "final_only":
		return "final_only"
	case "milestone", "on_blocked", "always", "internal_only":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "final_only"
	}
}

func shouldNotifyMainOnFinal(policy string, runErr error, run *SubagentRun) bool {
	switch normalizeNotifyMainPolicy(policy) {
	case "internal_only":
		return false
	case "always", "final_only":
		return true
	case "on_blocked":
		return isBlockedSubagentError(runErr)
	case "milestone":
		return false
	default:
		return true
	}
}

func buildSubagentMainNotification(run *SubagentRun, runErr error) (string, string) {
	status := "completed"
	reason := "final"
	if runErr != nil {
		status = "failed"
		if isBlockedSubagentError(runErr) {
			status = "blocked"
			reason = "blocked"
		}
	}
	return fmt.Sprintf(
		"Subagent update\nagent: %s\nrun: %s\nstatus: %s\nreason: %s\ntask: %s\nsummary: %s",
		strings.TrimSpace(run.AgentID),
		strings.TrimSpace(run.ID),
		status,
		reason,
		summarizeSubagentText(firstNonEmpty(run.Label, run.Task), 120),
		summarizeSubagentText(run.Result, 280),
	), reason
}

func isBlockedSubagentError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}
	blockedHints := []string{
		"timeout",
		"deadline exceeded",
		"quota",
		"rate limit",
		"too many requests",
		"permission denied",
		"requires input",
		"waiting for reply",
		"blocked",
	}
	for _, hint := range blockedHints {
		if strings.Contains(msg, hint) {
			return true
		}
	}
	return false
}

func summarizeSubagentText(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "(empty)"
	}
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max-3]) + "..."
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (sm *SubagentManager) runWithRetry(ctx context.Context, run *SubagentRun) (string, error) {
	maxRetries := normalizePositiveBound(run.MaxRetries, 0, 8)
	backoffMs := normalizePositiveBound(run.RetryBackoff, 500, 120000)
	timeoutSec := normalizePositiveBound(run.TimeoutSec, 0, 3600)

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		runCtx := ctx
		var cancel context.CancelFunc
		if timeoutSec > 0 {
			runCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		}
		result, err := sm.executeRunOnce(runCtx, run)
		if cancel != nil {
			cancel()
		}
		if err == nil {
			sm.mu.Lock()
			run.RetryCount = attempt
			run.Updated = time.Now().UnixMilli()
			sm.persistRunLocked(run, "attempt_succeeded", "")
			sm.mu.Unlock()
			return result, nil
		}
		lastErr = err
		sm.mu.Lock()
		run.RetryCount = attempt
		run.Updated = time.Now().UnixMilli()
		sm.persistRunLocked(run, "attempt_failed", err.Error())
		sm.mu.Unlock()
		if attempt >= maxRetries {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Duration(backoffMs) * time.Millisecond):
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("subagent run failed with unknown error")
	}
	return "", lastErr
}

func (sm *SubagentManager) executeRunOnce(ctx context.Context, run *SubagentRun) (string, error) {
	if run == nil {
		return "", fmt.Errorf("subagent run is nil")
	}
	pending, consumedIDs := sm.consumeThreadInbox(run)
	if sm.runFunc != nil {
		result, err := sm.runFunc(ctx, run)
		if err != nil {
			sm.restoreMessageStatuses(consumedIDs)
		} else {
			sm.ackMessageStatuses(consumedIDs)
		}
		return result, err
	}
	if sm.provider == nil {
		sm.restoreMessageStatuses(consumedIDs)
		return "", fmt.Errorf("no llm provider configured for subagent execution")
	}

	systemPrompt := sm.resolveSystemPrompt(run)
	messages := []providers.Message{
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role:    "user",
			Content: run.Task,
		},
	}
	if strings.TrimSpace(pending) != "" {
		messages = append(messages, providers.Message{
			Role:    "user",
			Content: "Mailbox updates on this thread:\n" + pending,
		})
	}

	response, err := sm.provider.Chat(ctx, messages, nil, sm.provider.GetDefaultModel(), map[string]interface{}{
		"max_tokens": 4096,
	})
	if err != nil {
		sm.restoreMessageStatuses(consumedIDs)
		return "", err
	}
	sm.ackMessageStatuses(consumedIDs)
	return response.Content, nil
}

func (sm *SubagentManager) resolveSystemPrompt(run *SubagentRun) string {
	systemPrompt := "You are a subagent. Follow workspace AGENTS.md and complete the task independently."
	workspacePrompt := sm.readWorkspacePromptFile("AGENTS.md")
	if workspacePrompt != "" {
		systemPrompt = "Workspace policy (AGENTS.md):\n" + workspacePrompt + "\n\nComplete the given task independently and report the result."
	}
	if run == nil {
		return systemPrompt
	}
	if promptFile := strings.TrimSpace(run.SystemPromptFile); promptFile != "" {
		if promptText := sm.readWorkspacePromptFile(promptFile); promptText != "" {
			return systemPrompt + "\n\nSubagent policy (" + promptFile + "):\n" + promptText
		}
	}
	return systemPrompt
}

func (sm *SubagentManager) readWorkspacePromptFile(relPath string) string {
	ws := strings.TrimSpace(sm.workspace)
	relPath = strings.TrimSpace(relPath)
	if ws == "" || relPath == "" || filepath.IsAbs(relPath) {
		return ""
	}
	fullPath := filepath.Clean(filepath.Join(ws, relPath))
	relToWorkspace, err := filepath.Rel(ws, fullPath)
	if err != nil || strings.HasPrefix(relToWorkspace, "..") {
		return ""
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

type SubagentRunFunc func(ctx context.Context, run *SubagentRun) (string, error)

func (sm *SubagentManager) SetRunFunc(f SubagentRunFunc) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.runFunc = f
	go sm.resumeRecoveredRuns()
}

func (sm *SubagentManager) ProfileStore() *SubagentProfileStore {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.profileStore
}

func (sm *SubagentManager) resumeRecoveredRuns() {
	if sm == nil {
		return
	}
	sm.mu.Lock()
	if sm.runFunc == nil && sm.provider == nil {
		sm.mu.Unlock()
		return
	}
	runIDs := append([]string(nil), sm.recoverableRunIDs...)
	sm.recoverableRunIDs = nil
	toResume := make([]*SubagentRun, 0, len(runIDs))
	for _, runID := range runIDs {
		run, ok := sm.runs[runID]
		if !ok || run == nil || run.Status != "running" {
			continue
		}
		run.Updated = time.Now().UnixMilli()
		sm.persistRunLocked(run, "recovered", "auto-resumed after restart")
		toResume = append(toResume, run)
	}
	sm.mu.Unlock()

	for _, run := range toResume {
		taskCtx, cancel := context.WithCancel(context.Background())
		sm.mu.Lock()
		sm.cancelFuncs[run.ID] = cancel
		sm.mu.Unlock()
		go sm.runSubagent(taskCtx, run)
	}
}

func (sm *SubagentManager) NextRunSequence() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.nextID
}

func (sm *SubagentManager) listRuns() []*SubagentRun {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.pruneArchivedLocked()

	runs := make([]*SubagentRun, 0, len(sm.runs))
	seen := make(map[string]struct{}, len(sm.runs))
	for _, run := range sm.runs {
		runs = append(runs, run)
		seen[run.ID] = struct{}{}
	}
	if sm.runStore != nil {
		for _, run := range sm.runStore.List() {
			if _, ok := seen[run.ID]; ok {
				continue
			}
			runs = append(runs, run)
		}
	}
	return runs
}

func (sm *SubagentManager) Events(runID string, limit int) ([]SubagentRunEvent, error) {
	if sm.runStore == nil {
		return nil, nil
	}
	return sm.runStore.Events(runID, limit)
}

func (sm *SubagentManager) RuntimeSnapshot(limit int) RuntimeSnapshot {
	if sm == nil {
		return RuntimeSnapshot{}
	}
	runs := sm.listRuns()
	snapshot := RuntimeSnapshot{
		Requests: make([]RequestRecord, 0, len(runs)),
		Runs:     make([]RunRecord, 0, len(runs)),
	}
	seenThreads := map[string]struct{}{}
	for _, run := range runs {
		snapshot.Requests = append(snapshot.Requests, runToRequestRecord(run))
		snapshot.Runs = append(snapshot.Runs, runToRunRecord(run))
		if evts, err := sm.Events(run.ID, limit); err == nil {
			for _, evt := range evts {
				snapshot.Events = append(snapshot.Events, EventRecord{
					ID:         EventRecordID(evt.RunID, evt.Type, evt.At),
					RunID:      evt.RunID,
					RequestID:  evt.RunID,
					AgentID:    evt.AgentID,
					Type:       evt.Type,
					Status:     evt.Status,
					Message:    evt.Message,
					RetryCount: evt.RetryCount,
					At:         evt.At,
				})
			}
		}
		threadID := strings.TrimSpace(run.ThreadID)
		if threadID == "" {
			continue
		}
		if _, ok := seenThreads[threadID]; !ok {
			if thread, found := sm.Thread(threadID); found {
				snapshot.Threads = append(snapshot.Threads, threadToThreadRecord(thread))
			}
			seenThreads[threadID] = struct{}{}
		}
		if msgs, err := sm.ThreadMessages(threadID, limit); err == nil {
			for _, msg := range msgs {
				snapshot.Artifacts = append(snapshot.Artifacts, messageToArtifactRecord(msg))
			}
		}
	}
	return snapshot
}

func (sm *SubagentManager) Thread(threadID string) (*AgentThread, bool) {
	if sm.mailboxStore == nil {
		return nil, false
	}
	return sm.mailboxStore.Thread(threadID)
}

func (sm *SubagentManager) ThreadMessages(threadID string, limit int) ([]AgentMessage, error) {
	if sm.mailboxStore == nil {
		return nil, nil
	}
	return sm.mailboxStore.MessagesByThread(threadID, limit)
}

func (sm *SubagentManager) Inbox(agentID string, limit int) ([]AgentMessage, error) {
	if sm.mailboxStore == nil {
		return nil, nil
	}
	return sm.mailboxStore.Inbox(agentID, limit)
}

func (sm *SubagentManager) Message(messageID string) (*AgentMessage, bool) {
	if sm.mailboxStore == nil {
		return nil, false
	}
	return sm.mailboxStore.Message(messageID)
}

func (sm *SubagentManager) pruneArchivedLocked() {
	if sm.archiveAfterMinute <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(sm.archiveAfterMinute) * time.Minute).UnixMilli()
	for id, run := range sm.runs {
		if !IsTerminalRuntimeStatus(run.Status) {
			continue
		}
		if run.Updated > 0 && run.Updated < cutoff {
			delete(sm.runs, id)
			delete(sm.cancelFuncs, id)
		}
	}
}

func normalizePositiveBound(v, min, max int) int {
	if v < min {
		return min
	}
	if max > 0 && v > max {
		return max
	}
	return v
}

func applySubagentResultQuota(result string, maxChars int) string {
	if maxChars <= 0 {
		return result
	}
	if len(result) <= maxChars {
		return result
	}
	suffix := "\n\n[TRUNCATED: result exceeds max_result_chars quota]"
	trimmed := result[:maxChars]
	if len(trimmed)+len(suffix) > maxChars && maxChars > len(suffix) {
		trimmed = trimmed[:maxChars-len(suffix)]
	}
	return strings.TrimSpace(trimmed) + suffix
}

func normalizeSubagentIdentifier(in string) string {
	in = strings.TrimSpace(strings.ToLower(in))
	if in == "" {
		return ""
	}
	var sb strings.Builder
	for _, r := range in {
		switch {
		case r >= 'a' && r <= 'z':
			sb.WriteRune(r)
		case r >= '0' && r <= '9':
			sb.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			sb.WriteRune(r)
		case r == ' ':
			sb.WriteRune('-')
		}
	}
	out := strings.Trim(sb.String(), "-_.")
	if out == "" {
		return ""
	}
	return out
}

func buildSubagentSessionKey(agentID, runID string) string {
	a := normalizeSubagentIdentifier(agentID)
	if a == "" {
		a = "default"
	}
	t := normalizeSubagentIdentifier(runID)
	if t == "" {
		t = "run"
	}
	return fmt.Sprintf("subagent:%s:%s", a, t)
}

func (sm *SubagentManager) persistRunLocked(run *SubagentRun, eventType, message string) {
	if run == nil || sm.runStore == nil {
		return
	}
	cp := cloneSubagentRun(run)
	_ = sm.runStore.AppendRun(cp)
	_ = sm.runStore.AppendEvent(SubagentRunEvent{
		RunID:      cp.ID,
		AgentID:    cp.AgentID,
		Type:       strings.TrimSpace(eventType),
		Status:     cp.Status,
		Message:    strings.TrimSpace(message),
		RetryCount: cp.RetryCount,
		At:         cp.Updated,
	})
}

func (sm *SubagentManager) waitRun(ctx context.Context, runID string) (*SubagentRun, bool, error) {
	if sm == nil {
		return nil, false, fmt.Errorf("subagent manager not available")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, false, fmt.Errorf("run id is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ch := make(chan struct{}, 1)
	sm.mu.Lock()
	sm.pruneArchivedLocked()
	run, ok := sm.runs[runID]
	if !ok && sm.runStore != nil {
		if persisted, found := sm.runStore.Get(runID); found && persisted != nil {
			if IsTerminalRuntimeStatus(persisted.Status) {
				sm.mu.Unlock()
				return persisted, true, nil
			}
		}
	}
	if ok && run != nil && IsTerminalRuntimeStatus(run.Status) {
		cp := cloneSubagentRun(run)
		sm.mu.Unlock()
		return cp, true, nil
	}
	waiters := sm.waiters[runID]
	if waiters == nil {
		waiters = map[chan struct{}]struct{}{}
		sm.waiters[runID] = waiters
	}
	waiters[ch] = struct{}{}
	sm.mu.Unlock()

	defer sm.removeRunWaiter(runID, ch)
	for {
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-ch:
			sm.mu.Lock()
			sm.pruneArchivedLocked()
			run, ok := sm.runs[runID]
			if ok && run != nil && IsTerminalRuntimeStatus(run.Status) {
				cp := cloneSubagentRun(run)
				sm.mu.Unlock()
				return cp, true, nil
			}
			if !ok && sm.runStore != nil {
				if persisted, found := sm.runStore.Get(runID); found && persisted != nil && IsTerminalRuntimeStatus(persisted.Status) {
					sm.mu.Unlock()
					return persisted, true, nil
				}
			}
			sm.mu.Unlock()
		}
	}
}

func (sm *SubagentManager) removeRunWaiter(runID string, ch chan struct{}) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	waiters := sm.waiters[runID]
	if len(waiters) == 0 {
		delete(sm.waiters, runID)
		return
	}
	delete(waiters, ch)
	if len(waiters) == 0 {
		delete(sm.waiters, runID)
	}
}

func (sm *SubagentManager) notifyRunWaitersLocked(runID string) {
	waiters := sm.waiters[runID]
	if len(waiters) == 0 {
		delete(sm.waiters, runID)
		return
	}
	for ch := range waiters {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	delete(sm.waiters, runID)
}

func (sm *SubagentManager) recordMailboxMessageLocked(run *SubagentRun, msg AgentMessage) {
	if sm.mailboxStore == nil || run == nil {
		return
	}
	if strings.TrimSpace(msg.ThreadID) == "" {
		msg.ThreadID = run.ThreadID
	}
	stored, err := sm.mailboxStore.AppendMessage(msg)
	if err != nil {
		return
	}
	run.LastMessageID = stored.MessageID
	if stored.RequiresReply {
		run.WaitingReply = true
	}
}

func (sm *SubagentManager) consumeThreadInbox(run *SubagentRun) (string, []string) {
	if run == nil || sm.mailboxStore == nil {
		return "", nil
	}
	msgs, err := sm.mailboxStore.ThreadInbox(run.ThreadID, run.AgentID, 0)
	if err != nil || len(msgs) == 0 {
		return "", nil
	}
	var sb strings.Builder
	consumed := make([]string, 0, len(msgs))
	now := time.Now().UnixMilli()
	for _, msg := range msgs {
		if _, err := sm.mailboxStore.UpdateMessageStatus(msg.MessageID, "processing", now); err != nil {
			continue
		}
		consumed = append(consumed, msg.MessageID)
		sb.WriteString(fmt.Sprintf("- [%s] from=%s type=%s", msg.MessageID, msg.FromAgent, msg.Type))
		if strings.TrimSpace(msg.ReplyTo) != "" {
			sb.WriteString(fmt.Sprintf(" reply_to=%s", msg.ReplyTo))
		}
		sb.WriteString("\n")
		sb.WriteString(strings.TrimSpace(msg.Content))
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String()), consumed
}

func (sm *SubagentManager) restoreMessageStatuses(messageIDs []string) {
	if sm.mailboxStore == nil || len(messageIDs) == 0 {
		return
	}
	now := time.Now().UnixMilli()
	for _, messageID := range messageIDs {
		_, _ = sm.mailboxStore.UpdateMessageStatus(messageID, "queued", now)
	}
}

func (sm *SubagentManager) ackMessageStatuses(messageIDs []string) {
	if sm.mailboxStore == nil || len(messageIDs) == 0 {
		return
	}
	now := time.Now().UnixMilli()
	for _, messageID := range messageIDs {
		_, _ = sm.mailboxStore.UpdateMessageStatus(messageID, "acked", now)
	}
}
