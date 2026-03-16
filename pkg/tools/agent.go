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
	"github.com/YspCoder/clawgo/pkg/ekg"
	"github.com/YspCoder/clawgo/pkg/providers"
)

type AgentTask struct {
	ID                  string                   `json:"id"`
	Task                string                   `json:"task"`
	RunKind             string                   `json:"run_kind,omitempty"`
	AgentID             string                   `json:"agent_id"`
	Target              *TargetRef               `json:"target,omitempty"`
	ExecutionPolicy     *ExecutionPolicy         `json:"execution_policy,omitempty"`
	RetryCount          int                      `json:"retry_count,omitempty"`
	WorldDecision       *WorldDecisionContext    `json:"world_decision,omitempty"`
	Origin              *OriginRef               `json:"origin,omitempty"`
	Status              string                   `json:"status"`
	Result              string                   `json:"result,omitempty"`
	Created             int64                    `json:"created"`
	Updated             int64                    `json:"updated"`
}

type WorldDecisionContext struct {
	WorldTick           int64                    `json:"world_tick,omitempty"`
	WorldSnapshot       map[string]interface{}   `json:"world_snapshot,omitempty"`
	NPCSnapshot         map[string]interface{}   `json:"npc_snapshot,omitempty"`
	VisibleEvents       []map[string]interface{} `json:"visible_events,omitempty"`
	IntentSchemaVersion string                   `json:"intent_schema_version,omitempty"`
}

type OriginRef struct {
	Channel string `json:"channel,omitempty"`
	ChatID  string `json:"chat_id,omitempty"`
}

type TargetRef struct {
	Transport     string `json:"transport,omitempty"`
	NodeID        string `json:"node_id,omitempty"`
	ParentAgentID string `json:"parent_agent_id,omitempty"`
}

type ExecutionPolicy struct {
	PromptFile     string   `json:"prompt_file,omitempty"`
	ToolAllowlist  []string `json:"tool_allowlist,omitempty"`
	MaxRetries     int      `json:"max_retries,omitempty"`
	RetryBackoff   int      `json:"retry_backoff,omitempty"`
	TimeoutSec     int      `json:"timeout_sec,omitempty"`
	MaxTaskChars   int      `json:"max_task_chars,omitempty"`
	MaxResultChars int      `json:"max_result_chars,omitempty"`
}

type AgentManager struct {
	tasks              map[string]*AgentTask
	cancelFuncs        map[string]context.CancelFunc
	waiters            map[string]map[chan struct{}]struct{}
	recoverableTaskIDs []string
	archiveAfterMinute int64
	mu                 sync.RWMutex
	provider           providers.LLMProvider
	bus                *bus.MessageBus
	workspace          string
	nextID             int
	runFunc            AgentRunFunc
	profileStore       *AgentProfileStore
	runStore           *AgentRunStore
	mailboxStore       *AgentMailboxStore
	ekg                *ekg.Engine
}

type AgentSpawnOptions struct {
	Task                string
	RunKind             string
	Role                string
	AgentID             string
	Target              *TargetRef
	ExecutionPolicy     *ExecutionPolicy
	Origin              *OriginRef
	WorldDecision       *WorldDecisionContext
}

func NewAgentManager(provider providers.LLMProvider, workspace string, bus *bus.MessageBus) *AgentManager {
	store := NewAgentProfileStore(workspace)
	runStore := NewAgentRunStore(workspace)
	mailboxStore := NewAgentMailboxStore(workspace)
	mgr := &AgentManager{
		tasks:              make(map[string]*AgentTask),
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
		ekg:                ekg.New(workspace),
	}
	if runStore != nil {
		for _, task := range runStore.List() {
			mgr.tasks[task.ID] = task
			if task.Status == RuntimeStatusRunning {
				mgr.recoverableTaskIDs = append(mgr.recoverableTaskIDs, task.ID)
			}
		}
		mgr.nextID = runStore.NextIDSeed()
	}
	go mgr.resumeRecoveredTasks()
	return mgr
}

func (sm *AgentManager) HasProvider() bool {
	return sm != nil && sm.provider != nil
}

func (sm *AgentManager) Spawn(ctx context.Context, opts AgentSpawnOptions) (string, error) {
	task, err := sm.spawnTask(ctx, opts)
	if err != nil {
		return "", err
	}
	desc := fmt.Sprintf("Spawned agent for task: %s (agent=%s)", task.Task, task.AgentID)
	return desc, nil
}

func (sm *AgentManager) SpawnTask(ctx context.Context, opts AgentSpawnOptions) (*AgentTask, error) {
	return sm.spawnTask(ctx, opts)
}

func (sm *AgentManager) spawnTask(ctx context.Context, opts AgentSpawnOptions) (*AgentTask, error) {
	task := strings.TrimSpace(opts.Task)
	if task == "" {
		return nil, fmt.Errorf("task is required")
	}
	role := strings.TrimSpace(opts.Role)
	agentID := normalizeAgentIdentifier(opts.AgentID)
	var profile *AgentProfile
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
				agentID = normalizeAgentIdentifier(p.AgentID)
			}
		}
	}
	if agentID == "" {
		agentID = normalizeAgentIdentifier(role)
	}
	if agentID == "" {
		agentID = "default"
	}
	systemPromptFile := ""
	transport := "local"
	nodeID := ""
	parentAgentID := ""
	policy := &ExecutionPolicy{RetryBackoff: 1000}
	if profile == nil && sm.profileStore != nil {
		if p, ok, err := sm.profileStore.Get(agentID); err != nil {
			return nil, err
		} else if ok {
			profile = p
		}
	}
	if profile != nil {
		if strings.EqualFold(strings.TrimSpace(profile.Status), "disabled") {
			return nil, fmt.Errorf("agent profile '%s' is disabled", profile.AgentID)
		}
		transport = strings.TrimSpace(profile.Transport)
		if transport == "" {
			transport = "local"
		}
		nodeID = strings.TrimSpace(profile.NodeID)
		parentAgentID = strings.TrimSpace(profile.ParentAgentID)
		systemPromptFile = strings.TrimSpace(profile.PromptFile)
		policy = &ExecutionPolicy{
			PromptFile:     strings.TrimSpace(profile.PromptFile),
			ToolAllowlist:  append([]string(nil), profile.ToolAllowlist...),
			MaxRetries:     profile.MaxRetries,
			RetryBackoff:   profile.RetryBackoff,
			TimeoutSec:     profile.TimeoutSec,
			MaxTaskChars:   profile.MaxTaskChars,
			MaxResultChars: profile.MaxResultChars,
		}
	}
	policy = mergeExecutionPolicy(policy, opts.ExecutionPolicy)
	systemPromptFile = strings.TrimSpace(policy.PromptFile)
	toolAllowlist := append([]string(nil), policy.ToolAllowlist...)
	maxRetries := policy.MaxRetries
	retryBackoff := policy.RetryBackoff
	timeoutSec := policy.TimeoutSec
	maxTaskChars := policy.MaxTaskChars
	maxResultChars := policy.MaxResultChars
	if maxTaskChars > 0 && len(task) > maxTaskChars {
		return nil, fmt.Errorf("task exceeds max_task_chars quota (%d > %d)", len(task), maxTaskChars)
	}
	maxRetries = normalizePositiveBound(maxRetries, 0, 8)
	retryBackoff = normalizePositiveBound(retryBackoff, 500, 120000)
	timeoutSec = normalizePositiveBound(timeoutSec, 0, 3600)
	maxTaskChars = normalizePositiveBound(maxTaskChars, 0, 400000)
	maxResultChars = normalizePositiveBound(maxResultChars, 0, 400000)
	sm.mu.Lock()
	defer sm.mu.Unlock()

	taskID := fmt.Sprintf("agent-%d", sm.nextID)
	sm.nextID++
	now := time.Now().UnixMilli()
	agentTask := &AgentTask{
		ID:                  taskID,
		Task:                task,
		RunKind:             normalizeAgentRunKind(opts.RunKind, opts.WorldDecision),
		AgentID:             agentID,
		Target:              mergeTargetRef(&TargetRef{Transport: transport, NodeID: nodeID, ParentAgentID: parentAgentID}, opts.Target),
		ExecutionPolicy:     &ExecutionPolicy{PromptFile: systemPromptFile, ToolAllowlist: toolAllowlist, MaxRetries: maxRetries, RetryBackoff: retryBackoff, TimeoutSec: timeoutSec, MaxTaskChars: maxTaskChars, MaxResultChars: maxResultChars},
		RetryCount:          0,
		WorldDecision:       cloneWorldDecisionContext(opts.WorldDecision),
		Origin:              cloneOriginRef(opts.Origin),
		Status:              RuntimeStatusRouting,
		Created:             now,
		Updated:             now,
	}
	taskCtx, cancel := context.WithCancel(ctx)
	sm.tasks[taskID] = agentTask
	sm.cancelFuncs[taskID] = cancel
	sm.recordMailboxMessageLocked(agentTask, AgentMessage{
		ThreadID:      taskMailboxThreadID(agentTask),
		FromAgent:     "main",
		ToAgent:       agentID,
		Type:          "task",
		Content:       task,
		Status:        "queued",
		CreatedAt:     now,
	})
	sm.persistTaskLocked(agentTask, "spawned", "")

	go sm.runTask(taskCtx, agentTask)
	return cloneAgentTask(agentTask), nil
}

func (sm *AgentManager) runTask(ctx context.Context, task *AgentTask) {
	defer func() {
		sm.mu.Lock()
		delete(sm.cancelFuncs, task.ID)
		sm.mu.Unlock()
	}()

	sm.mu.Lock()
	task.Status = RuntimeStatusRunning
	task.Created = time.Now().UnixMilli()
	task.Updated = task.Created
	sm.persistTaskLocked(task, "started", "")
	sm.mu.Unlock()

	result, runErr := sm.runWithRetry(ctx, task)
	sm.mu.Lock()
	if runErr != nil {
		task.Status = RuntimeStatusFailed
		task.Result = fmt.Sprintf("Error: %v", runErr)
		task.Result = applyAgentResultQuota(task.Result, maxResultCharsFromPolicy(task.ExecutionPolicy))
		task.Updated = time.Now().UnixMilli()
		sm.recordMailboxMessageLocked(task, AgentMessage{
			ThreadID:      taskMailboxThreadID(task),
			FromAgent:     task.AgentID,
			ToAgent:       "main",
			Type:          "result",
			Content:       task.Result,
			Status:        "delivered",
			CreatedAt:     task.Updated,
		})
		sm.persistTaskLocked(task, "failed", task.Result)
	} else {
		task.Status = RuntimeStatusCompleted
		task.Result = applyAgentResultQuota(result, maxResultCharsFromPolicy(task.ExecutionPolicy))
		task.Updated = time.Now().UnixMilli()
		sm.recordMailboxMessageLocked(task, AgentMessage{
			ThreadID:      taskMailboxThreadID(task),
			FromAgent:     task.AgentID,
			ToAgent:       "main",
			Type:          "result",
			Content:       task.Result,
			Status:        "delivered",
			CreatedAt:     task.Updated,
		})
		sm.persistTaskLocked(task, "completed", task.Result)
	}
	sm.mu.Unlock()

	sm.recordEKG(task, runErr)

	// 2. Result broadcast
	if sm.bus != nil && shouldNotifyMainOnFinal(task, runErr) {
		announceContent, notifyReason := buildAgentMainNotification(task, runErr)
		sessionKey := BuildAgentSessionKey(task.AgentID, task.ID)
		originChannel, originChatID := OriginValues(task.Origin)
		memoryNS := sm.ResolveMemoryNamespace(task.AgentID)
		sm.bus.PublishInbound(bus.InboundMessage{
			Channel:    "system",
			SenderID:   fmt.Sprintf("agent:%s", task.ID),
			ChatID:     fmt.Sprintf("%s:%s", originChannel, originChatID),
			SessionKey: sessionKey,
			Content:    announceContent,
			Metadata: map[string]string{
				"trigger":       "agent",
				"agent_run_id":  task.ID,
				"agent_id":      task.AgentID,
				"session_key":   sessionKey,
				"memory_ns":     memoryNS,
				"retry_count":   fmt.Sprintf("%d", task.RetryCount),
				"timeout_sec":   fmt.Sprintf("%d", timeoutSecFromPolicy(task.ExecutionPolicy)),
				"status":        task.Status,
				"notify_reason": notifyReason,
			},
		})
	}
	sm.mu.Lock()
	sm.notifyTaskWaitersLocked(task.ID)
	sm.mu.Unlock()
}

func (sm *AgentManager) recordEKG(task *AgentTask, runErr error) {
	if sm == nil || sm.ekg == nil || task == nil {
		return
	}
	status := "success"
	logText := strings.TrimSpace(task.Result)
	if runErr != nil {
		status = "error"
		if isBlockedAgentError(runErr) {
			logText = "blocked: " + strings.TrimSpace(task.Result)
		}
	}
	sm.ekg.Record(ekg.Event{
		TaskID:  task.ID,
		Session: BuildAgentSessionKey(task.AgentID, task.ID),
		Channel: originChannelOrDefault(task.Origin),
		Source:  "agent",
		Status:  status,
		Log:     logText,
	})
}

func shouldNotifyMainOnFinal(task *AgentTask, runErr error) bool {
	_ = runErr
	return !IsWorldDecisionTask(task)
}

func buildAgentMainNotification(task *AgentTask, runErr error) (string, string) {
	status := "completed"
	reason := "final"
	if runErr != nil {
		status = "failed"
		if isBlockedAgentError(runErr) {
			status = "blocked"
			reason = "blocked"
		}
	}
	return fmt.Sprintf(
		"Agent update\nagent: %s\nrun: %s\nstatus: %s\nreason: %s\ntask: %s\nsummary: %s",
		strings.TrimSpace(task.AgentID),
		strings.TrimSpace(task.ID),
		status,
		reason,
		summarizeAgentText(task.Task, 120),
		summarizeAgentText(task.Result, 280),
	), reason
}

func isBlockedAgentError(err error) bool {
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

func summarizeAgentText(s string, max int) string {
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

func (sm *AgentManager) runWithRetry(ctx context.Context, task *AgentTask) (string, error) {
	maxRetries := maxRetriesFromPolicy(task.ExecutionPolicy)
	backoffMs := retryBackoffFromPolicy(task.ExecutionPolicy)
	timeoutSec := timeoutSecFromPolicy(task.ExecutionPolicy)

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		result, err := runStringTaskWithTaskWatchdog(
			ctx,
			timeoutSec,
			2*time.Second,
			stringTaskWatchdogOptions{
				ProgressFn: func() int {
					return sm.taskWatchdogProgress(task)
				},
				CanExtend: func() bool {
					return sm.taskCanAutoExtend(task)
				},
			},
			func(runCtx context.Context) (string, error) {
				return sm.executeTaskOnce(runCtx, task)
			},
		)
		if err == nil {
			sm.mu.Lock()
			task.RetryCount = attempt
			task.Updated = time.Now().UnixMilli()
			sm.persistTaskLocked(task, "attempt_succeeded", "")
			sm.mu.Unlock()
			return result, nil
		}
		lastErr = err
		sm.mu.Lock()
		task.RetryCount = attempt
		task.Updated = time.Now().UnixMilli()
		sm.persistTaskLocked(task, "attempt_failed", err.Error())
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
		lastErr = fmt.Errorf("agent task failed with unknown error")
	}
	return "", lastErr
}

func (sm *AgentManager) taskWatchdogProgress(task *AgentTask) int {
	if sm == nil || task == nil {
		return 0
	}
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	current, ok := sm.tasks[task.ID]
	if !ok || current == nil {
		current = task
	}
	if current.Updated <= 0 {
		return 0
	}
	return int(current.Updated)
}

func (sm *AgentManager) taskCanAutoExtend(task *AgentTask) bool {
	if sm == nil || task == nil {
		return false
	}
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	current, ok := sm.tasks[task.ID]
	if !ok || current == nil {
		current = task
	}
	return strings.EqualFold(strings.TrimSpace(current.Status), "running")
}

func (sm *AgentManager) executeTaskOnce(ctx context.Context, task *AgentTask) (string, error) {
	if task == nil {
		return "", fmt.Errorf("agent task is nil")
	}
	pending, consumedIDs := sm.consumeThreadInbox(task)
	if sm.runFunc != nil {
		result, err := sm.runFunc(ctx, task)
		if err != nil {
			sm.restoreMessageStatuses(consumedIDs)
		} else {
			sm.ackMessageStatuses(consumedIDs)
		}
		return result, err
	}
	if sm.provider == nil {
		sm.restoreMessageStatuses(consumedIDs)
		return "", fmt.Errorf("no llm provider configured for agent execution")
	}

	systemPrompt := sm.resolveSystemPrompt(task)
	messages := []providers.Message{
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role:    "user",
			Content: task.Task,
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

func (sm *AgentManager) resolveSystemPrompt(task *AgentTask) string {
	if IsWorldDecisionTask(task) {
		return "You are an NPC in a simulated world. Return only a JSON action intent. You may propose actions but must not directly alter world state."
	}
	systemPrompt := "You are a agent. Follow workspace AGENTS.md and complete the task independently."
	workspacePrompt := sm.readWorkspacePromptFile("AGENTS.md")
	if workspacePrompt != "" {
		systemPrompt = "Workspace policy (AGENTS.md):\n" + workspacePrompt + "\n\nComplete the given task independently and report the result."
	}
	if task == nil {
		return systemPrompt
	}
	if promptFile := PromptFileFromPolicy(task.ExecutionPolicy); promptFile != "" {
		if promptText := sm.readWorkspacePromptFile(promptFile); promptText != "" {
			return systemPrompt + "\n\nAgent policy (" + promptFile + "):\n" + promptText
		}
	}
	return systemPrompt
}

func (sm *AgentManager) readWorkspacePromptFile(relPath string) string {
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

type AgentRunFunc func(ctx context.Context, task *AgentTask) (string, error)

func (sm *AgentManager) SetRunFunc(f AgentRunFunc) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.runFunc = f
	go sm.resumeRecoveredTasks()
}

func (sm *AgentManager) ProfileStore() *AgentProfileStore {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.profileStore
}

func (sm *AgentManager) ResolveMemoryNamespace(agentID string) string {
	if sm == nil {
		return normalizeAgentIdentifier(agentID)
	}
	agentID = normalizeAgentIdentifier(agentID)
	if agentID == "" {
		return "default"
	}
	sm.mu.RLock()
	store := sm.profileStore
	sm.mu.RUnlock()
	if store != nil {
		if profile, ok, err := store.Get(agentID); err == nil && ok && profile != nil {
			if ns := normalizeAgentIdentifier(profile.MemoryNamespace); ns != "" {
				return ns
			}
		}
	}
	return agentID
}

func (sm *AgentManager) resumeRecoveredTasks() {
	if sm == nil {
		return
	}
	sm.mu.Lock()
	if sm.runFunc == nil && sm.provider == nil {
		sm.mu.Unlock()
		return
	}
	taskIDs := append([]string(nil), sm.recoverableTaskIDs...)
	sm.recoverableTaskIDs = nil
	toResume := make([]*AgentTask, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		task, ok := sm.tasks[taskID]
		if !ok || task == nil || task.Status != "running" {
			continue
		}
		task.Updated = time.Now().UnixMilli()
		sm.persistTaskLocked(task, "recovered", "auto-resumed after restart")
		toResume = append(toResume, task)
	}
	sm.mu.Unlock()

	for _, task := range toResume {
		taskCtx, cancel := context.WithCancel(context.Background())
		sm.mu.Lock()
		sm.cancelFuncs[task.ID] = cancel
		sm.mu.Unlock()
		go sm.runTask(taskCtx, task)
	}
}

func (sm *AgentManager) NextTaskSequence() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.nextID
}

func (sm *AgentManager) GetTask(taskID string) (*AgentTask, bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.pruneArchivedLocked()
	task, ok := sm.tasks[taskID]
	if !ok && sm.runStore != nil {
		return sm.runStore.Get(taskID)
	}
	return task, ok
}

func (sm *AgentManager) ListTasks() []*AgentTask {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.pruneArchivedLocked()

	tasks := make([]*AgentTask, 0, len(sm.tasks))
	seen := make(map[string]struct{}, len(sm.tasks))
	for _, task := range sm.tasks {
		tasks = append(tasks, task)
		seen[task.ID] = struct{}{}
	}
	if sm.runStore != nil {
		for _, task := range sm.runStore.List() {
			if _, ok := seen[task.ID]; ok {
				continue
			}
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func (sm *AgentManager) Events(taskID string, limit int) ([]AgentRunEvent, error) {
	if sm.runStore == nil {
		return nil, nil
	}
	return sm.runStore.Events(taskID, limit)
}

func (sm *AgentManager) RuntimeSnapshot(limit int) RuntimeSnapshot {
	if sm == nil {
		return RuntimeSnapshot{}
	}
	tasks := sm.ListTasks()
	snapshot := RuntimeSnapshot{
		Tasks: make([]TaskRecord, 0, len(tasks)),
		Runs:  make([]RunRecord, 0, len(tasks)),
	}
	for _, task := range tasks {
		snapshot.Tasks = append(snapshot.Tasks, taskToTaskRecord(task))
		snapshot.Runs = append(snapshot.Runs, taskToRunRecord(task))
		if evts, err := sm.Events(task.ID, limit); err == nil {
			for _, evt := range evts {
				snapshot.Events = append(snapshot.Events, EventRecord{
					ID:         EventRecordID(evt.RunID, evt.Type, evt.At),
					RunID:      evt.RunID,
					TaskID:     evt.RunID,
					AgentID:    evt.AgentID,
					Type:       evt.Type,
					Status:     evt.Status,
					Message:    evt.Message,
					RetryCount: evt.RetryCount,
					At:         evt.At,
				})
			}
		}
	}
	return snapshot
}

func (sm *AgentManager) Inbox(agentID string, limit int) ([]AgentMessage, error) {
	if sm.mailboxStore == nil {
		return nil, nil
	}
	return sm.mailboxStore.Inbox(agentID, limit)
}

func (sm *AgentManager) SendAgentMessage(fromAgent, toAgent, messageType, content, threadID string) (AgentMessage, error) {
	if sm == nil || sm.mailboxStore == nil {
		return AgentMessage{}, fmt.Errorf("mailbox store not available")
	}
	fromAgent = strings.TrimSpace(fromAgent)
	toAgent = strings.TrimSpace(toAgent)
	content = strings.TrimSpace(content)
	if fromAgent == "" || toAgent == "" {
		return AgentMessage{}, fmt.Errorf("from_agent and to_agent are required")
	}
	if content == "" {
		return AgentMessage{}, fmt.Errorf("content is required")
	}
	now := time.Now().UnixMilli()
	threadID = sm.mailboxStore.EnsureThreadID(threadID)
	return sm.mailboxStore.AppendMessage(AgentMessage{
		ThreadID:      threadID,
		FromAgent:     fromAgent,
		ToAgent:       toAgent,
		Type:          firstNonEmptyMessageType(messageType),
		Content:       content,
		Status:        "queued",
		CreatedAt:     now,
	})
}

func (sm *AgentManager) pruneArchivedLocked() {
	if sm.archiveAfterMinute <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(sm.archiveAfterMinute) * time.Minute).UnixMilli()
	for id, t := range sm.tasks {
		if !IsTerminalRuntimeStatus(t.Status) {
			continue
		}
		if t.Updated > 0 && t.Updated < cutoff {
			delete(sm.tasks, id)
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

func applyAgentResultQuota(result string, maxChars int) string {
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

func normalizeAgentIdentifier(in string) string {
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

func normalizeAgentRunKind(kind string, worldDecision *WorldDecisionContext) string {
	kind = strings.TrimSpace(kind)
	if kind != "" {
		return kind
	}
	if worldDecision != nil {
		return "world_npc"
	}
	return "agent"
}

func cloneOriginRef(in *OriginRef) *OriginRef {
	if in == nil {
		return nil
	}
	return &OriginRef{
		Channel: strings.TrimSpace(in.Channel),
		ChatID:  strings.TrimSpace(in.ChatID),
	}
}

func cloneTargetRef(in *TargetRef) *TargetRef {
	if in == nil {
		return nil
	}
	return &TargetRef{
		Transport:     strings.TrimSpace(in.Transport),
		NodeID:        strings.TrimSpace(in.NodeID),
		ParentAgentID: strings.TrimSpace(in.ParentAgentID),
	}
}

func mergeTargetRef(base, override *TargetRef) *TargetRef {
	out := cloneTargetRef(base)
	if out == nil {
		out = &TargetRef{}
	}
	if override == nil {
		return out
	}
	if v := strings.TrimSpace(override.Transport); v != "" {
		out.Transport = v
	}
	if v := strings.TrimSpace(override.NodeID); v != "" {
		out.NodeID = v
	}
	if v := strings.TrimSpace(override.ParentAgentID); v != "" {
		out.ParentAgentID = v
	}
	return out
}

func TargetTransport(target *TargetRef) string {
	if target == nil {
		return ""
	}
	return strings.TrimSpace(target.Transport)
}

func TargetNodeID(target *TargetRef) string {
	if target == nil {
		return ""
	}
	return strings.TrimSpace(target.NodeID)
}

func TargetParentAgentID(target *TargetRef) string {
	if target == nil {
		return ""
	}
	return strings.TrimSpace(target.ParentAgentID)
}

func cloneExecutionPolicy(in *ExecutionPolicy) *ExecutionPolicy {
	if in == nil {
		return nil
	}
	return &ExecutionPolicy{
		PromptFile:     strings.TrimSpace(in.PromptFile),
		ToolAllowlist:  append([]string(nil), in.ToolAllowlist...),
		MaxRetries:     in.MaxRetries,
		RetryBackoff:   in.RetryBackoff,
		TimeoutSec:     in.TimeoutSec,
		MaxTaskChars:   in.MaxTaskChars,
		MaxResultChars: in.MaxResultChars,
	}
}

func mergeExecutionPolicy(base, override *ExecutionPolicy) *ExecutionPolicy {
	out := cloneExecutionPolicy(base)
	if out == nil {
		out = &ExecutionPolicy{RetryBackoff: 1000}
	}
	if override == nil {
		return out
	}
	if v := strings.TrimSpace(override.PromptFile); v != "" {
		out.PromptFile = v
	}
	if len(override.ToolAllowlist) > 0 {
		out.ToolAllowlist = append([]string(nil), override.ToolAllowlist...)
	}
	if override.MaxRetries > 0 {
		out.MaxRetries = override.MaxRetries
	}
	if override.RetryBackoff > 0 {
		out.RetryBackoff = override.RetryBackoff
	}
	if override.TimeoutSec > 0 {
		out.TimeoutSec = override.TimeoutSec
	}
	if override.MaxTaskChars > 0 {
		out.MaxTaskChars = override.MaxTaskChars
	}
	if override.MaxResultChars > 0 {
		out.MaxResultChars = override.MaxResultChars
	}
	return out
}

func OriginValues(in *OriginRef) (string, string) {
	channel := "cli"
	chatID := "direct"
	if in == nil {
		return channel, chatID
	}
	if v := strings.TrimSpace(in.Channel); v != "" {
		channel = v
	}
	if v := strings.TrimSpace(in.ChatID); v != "" {
		chatID = v
	}
	return channel, chatID
}

func PromptFileFromPolicy(policy *ExecutionPolicy) string {
	if policy == nil {
		return ""
	}
	return strings.TrimSpace(policy.PromptFile)
}

func ToolAllowlistFromPolicy(policy *ExecutionPolicy) []string {
	if policy == nil || len(policy.ToolAllowlist) == 0 {
		return nil
	}
	return append([]string(nil), policy.ToolAllowlist...)
}

func maxRetriesFromPolicy(policy *ExecutionPolicy) int {
	if policy == nil {
		return 0
	}
	return normalizePositiveBound(policy.MaxRetries, 0, 8)
}

func retryBackoffFromPolicy(policy *ExecutionPolicy) int {
	if policy == nil {
		return 1000
	}
	return normalizePositiveBound(policy.RetryBackoff, 500, 120000)
}

func timeoutSecFromPolicy(policy *ExecutionPolicy) int {
	if policy == nil {
		return 0
	}
	return normalizePositiveBound(policy.TimeoutSec, 0, 3600)
}

func maxTaskCharsFromPolicy(policy *ExecutionPolicy) int {
	if policy == nil {
		return 0
	}
	return normalizePositiveBound(policy.MaxTaskChars, 0, 400000)
}

func maxResultCharsFromPolicy(policy *ExecutionPolicy) int {
	if policy == nil {
		return 0
	}
	return normalizePositiveBound(policy.MaxResultChars, 0, 400000)
}

func originChannelOrDefault(in *OriginRef) string {
	channel, _ := OriginValues(in)
	return channel
}

func taskMailboxThreadID(task *AgentTask) string {
	if task == nil {
		return ""
	}
	return strings.TrimSpace(task.ID)
}

func BuildAgentSessionKey(agentID, taskID string) string {
	a := normalizeAgentIdentifier(agentID)
	if a == "" {
		a = "default"
	}
	t := normalizeAgentIdentifier(taskID)
	if t == "" {
		t = "task"
	}
	return fmt.Sprintf("agent:%s:%s", a, t)
}

func firstNonEmptyMessageType(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "message"
	}
	return v
}

func (sm *AgentManager) persistTaskLocked(task *AgentTask, eventType, message string) {
	if task == nil || sm.runStore == nil {
		return
	}
	cp := cloneAgentTask(task)
	_ = sm.runStore.AppendRun(cp)
	_ = sm.runStore.AppendEvent(AgentRunEvent{
		RunID:      cp.ID,
		AgentID:    cp.AgentID,
		Type:       strings.TrimSpace(eventType),
		Status:     cp.Status,
		Message:    strings.TrimSpace(message),
		RetryCount: cp.RetryCount,
		At:         cp.Updated,
	})
}

func (sm *AgentManager) WaitTask(ctx context.Context, taskID string) (*AgentTask, bool, error) {
	if sm == nil {
		return nil, false, fmt.Errorf("agent manager not available")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, false, fmt.Errorf("task id is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ch := make(chan struct{}, 1)
	sm.mu.Lock()
	sm.pruneArchivedLocked()
	task, ok := sm.tasks[taskID]
	if !ok && sm.runStore != nil {
		if persisted, found := sm.runStore.Get(taskID); found && persisted != nil {
			if IsTerminalRuntimeStatus(persisted.Status) {
				sm.mu.Unlock()
				return persisted, true, nil
			}
		}
	}
	if ok && task != nil && IsTerminalRuntimeStatus(task.Status) {
		cp := cloneAgentTask(task)
		sm.mu.Unlock()
		return cp, true, nil
	}
	waiters := sm.waiters[taskID]
	if waiters == nil {
		waiters = map[chan struct{}]struct{}{}
		sm.waiters[taskID] = waiters
	}
	waiters[ch] = struct{}{}
	sm.mu.Unlock()

	defer sm.removeTaskWaiter(taskID, ch)
	for {
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-ch:
			sm.mu.Lock()
			sm.pruneArchivedLocked()
			task, ok := sm.tasks[taskID]
			if ok && task != nil && IsTerminalRuntimeStatus(task.Status) {
				cp := cloneAgentTask(task)
				sm.mu.Unlock()
				return cp, true, nil
			}
			if !ok && sm.runStore != nil {
				if persisted, found := sm.runStore.Get(taskID); found && persisted != nil && IsTerminalRuntimeStatus(persisted.Status) {
					sm.mu.Unlock()
					return persisted, true, nil
				}
			}
			sm.mu.Unlock()
		}
	}
}

func (sm *AgentManager) removeTaskWaiter(taskID string, ch chan struct{}) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	waiters := sm.waiters[taskID]
	if len(waiters) == 0 {
		delete(sm.waiters, taskID)
		return
	}
	delete(waiters, ch)
	if len(waiters) == 0 {
		delete(sm.waiters, taskID)
	}
}

func (sm *AgentManager) notifyTaskWaitersLocked(taskID string) {
	waiters := sm.waiters[taskID]
	if len(waiters) == 0 {
		delete(sm.waiters, taskID)
		return
	}
	for ch := range waiters {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	delete(sm.waiters, taskID)
}

func (sm *AgentManager) recordMailboxMessageLocked(task *AgentTask, msg AgentMessage) {
	if sm.mailboxStore == nil || task == nil {
		return
	}
	if strings.TrimSpace(msg.ThreadID) == "" {
		msg.ThreadID = taskMailboxThreadID(task)
	}
	stored, err := sm.mailboxStore.AppendMessage(msg)
	if err != nil {
		return
	}
	_ = stored
}

func (sm *AgentManager) consumeThreadInbox(task *AgentTask) (string, []string) {
	if task == nil || sm.mailboxStore == nil {
		return "", nil
	}
	msgs, err := sm.mailboxStore.ThreadInbox(taskMailboxThreadID(task), task.AgentID, 0)
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
		sb.WriteString("\n")
		sb.WriteString(strings.TrimSpace(msg.Content))
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String()), consumed
}

func (sm *AgentManager) restoreMessageStatuses(messageIDs []string) {
	if sm.mailboxStore == nil || len(messageIDs) == 0 {
		return
	}
	now := time.Now().UnixMilli()
	for _, messageID := range messageIDs {
		_, _ = sm.mailboxStore.UpdateMessageStatus(messageID, "queued", now)
	}
}

func (sm *AgentManager) ackMessageStatuses(messageIDs []string) {
	if sm.mailboxStore == nil || len(messageIDs) == 0 {
		return
	}
	now := time.Now().UnixMilli()
	for _, messageID := range messageIDs {
		_, _ = sm.mailboxStore.UpdateMessageStatus(messageID, "acked", now)
	}
}
