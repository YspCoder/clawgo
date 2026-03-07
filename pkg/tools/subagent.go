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

	"clawgo/pkg/bus"
	"clawgo/pkg/ekg"
	"clawgo/pkg/providers"
)

type SubagentTask struct {
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
	SystemPrompt     string                 `json:"system_prompt,omitempty"`
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
	tasks              map[string]*SubagentTask
	cancelFuncs        map[string]context.CancelFunc
	recoverableTaskIDs []string
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
	ekg                *ekg.Engine
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
		tasks:              make(map[string]*SubagentTask),
		cancelFuncs:        make(map[string]context.CancelFunc),
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
			if task.Status == "running" {
				mgr.recoverableTaskIDs = append(mgr.recoverableTaskIDs, task.ID)
			}
		}
		mgr.nextID = runStore.NextIDSeed()
	}
	go mgr.resumeRecoveredTasks()
	return mgr
}

func (sm *SubagentManager) Spawn(ctx context.Context, opts SubagentSpawnOptions) (string, error) {
	task, err := sm.spawnTask(ctx, opts)
	if err != nil {
		return "", err
	}
	desc := fmt.Sprintf("Spawned subagent for task: %s (agent=%s)", task.Task, task.AgentID)
	if task.Label != "" {
		desc = fmt.Sprintf("Spawned subagent '%s' for task: %s (agent=%s)", task.Label, task.Task, task.AgentID)
	}
	if task.Role != "" {
		desc += fmt.Sprintf(" role=%s", task.Role)
	}
	return desc, nil
}

func (sm *SubagentManager) SpawnTask(ctx context.Context, opts SubagentSpawnOptions) (*SubagentTask, error) {
	return sm.spawnTask(ctx, opts)
}

func (sm *SubagentManager) spawnTask(ctx context.Context, opts SubagentSpawnOptions) (*SubagentTask, error) {
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
	systemPrompt := ""
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
		systemPrompt = strings.TrimSpace(profile.SystemPrompt)
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

	taskID := fmt.Sprintf("subagent-%d", sm.nextID)
	sm.nextID++
	sessionKey := buildSubagentSessionKey(agentID, taskID)

	now := time.Now().UnixMilli()
	if correlationID == "" {
		correlationID = taskID
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
	subagentTask := &SubagentTask{
		ID:               taskID,
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
		SystemPrompt:     systemPrompt,
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
		Status:           "running",
		Created:          now,
		Updated:          now,
	}
	taskCtx, cancel := context.WithCancel(ctx)
	sm.tasks[taskID] = subagentTask
	sm.cancelFuncs[taskID] = cancel
	sm.recordMailboxMessageLocked(subagentTask, AgentMessage{
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
	sm.persistTaskLocked(subagentTask, "spawned", "")

	go sm.runTask(taskCtx, subagentTask)
	return cloneSubagentTask(subagentTask), nil
}

func (sm *SubagentManager) runTask(ctx context.Context, task *SubagentTask) {
	defer func() {
		sm.mu.Lock()
		delete(sm.cancelFuncs, task.ID)
		sm.mu.Unlock()
	}()

	sm.mu.Lock()
	task.Status = "running"
	task.Created = time.Now().UnixMilli()
	task.Updated = task.Created
	sm.persistTaskLocked(task, "started", "")
	sm.mu.Unlock()

	result, runErr := sm.runWithRetry(ctx, task)
	sm.mu.Lock()
	if runErr != nil {
		task.Status = "failed"
		task.Result = fmt.Sprintf("Error: %v", runErr)
		task.Result = applySubagentResultQuota(task.Result, task.MaxResultChars)
		task.Updated = time.Now().UnixMilli()
		task.WaitingReply = false
		sm.recordMailboxMessageLocked(task, AgentMessage{
			ThreadID:      task.ThreadID,
			FromAgent:     task.AgentID,
			ToAgent:       "main",
			ReplyTo:       task.LastMessageID,
			CorrelationID: task.CorrelationID,
			Type:          "result",
			Content:       task.Result,
			Status:        "delivered",
			CreatedAt:     task.Updated,
		})
		sm.persistTaskLocked(task, "completed", task.Result)
	} else {
		task.Status = "completed"
		task.Result = applySubagentResultQuota(result, task.MaxResultChars)
		task.Updated = time.Now().UnixMilli()
		task.WaitingReply = false
		sm.recordMailboxMessageLocked(task, AgentMessage{
			ThreadID:      task.ThreadID,
			FromAgent:     task.AgentID,
			ToAgent:       "main",
			ReplyTo:       task.LastMessageID,
			CorrelationID: task.CorrelationID,
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
	if sm.bus != nil && shouldNotifyMainOnFinal(task.NotifyMainPolicy, runErr, task) {
		announceContent, notifyReason := buildSubagentMainNotification(task, runErr)
		sm.bus.PublishInbound(bus.InboundMessage{
			Channel:    "system",
			SenderID:   fmt.Sprintf("subagent:%s", task.ID),
			ChatID:     fmt.Sprintf("%s:%s", task.OriginChannel, task.OriginChatID),
			SessionKey: task.SessionKey,
			Content:    announceContent,
			Metadata: map[string]string{
				"trigger":       "subagent",
				"subagent_id":   task.ID,
				"agent_id":      task.AgentID,
				"role":          task.Role,
				"session_key":   task.SessionKey,
				"memory_ns":     task.MemoryNS,
				"retry_count":   fmt.Sprintf("%d", task.RetryCount),
				"timeout_sec":   fmt.Sprintf("%d", task.TimeoutSec),
				"status":        task.Status,
				"notify_reason": notifyReason,
			},
		})
	}
}

func (sm *SubagentManager) recordEKG(task *SubagentTask, runErr error) {
	if sm == nil || sm.ekg == nil || task == nil {
		return
	}
	status := "success"
	logText := strings.TrimSpace(task.Result)
	if runErr != nil {
		status = "error"
		if isBlockedSubagentError(runErr) {
			logText = "blocked: " + strings.TrimSpace(task.Result)
		}
	}
	sm.ekg.Record(ekg.Event{
		TaskID:  task.ID,
		Session: task.SessionKey,
		Channel: task.OriginChannel,
		Source:  "subagent",
		Status:  status,
		Log:     logText,
	})
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

func shouldNotifyMainOnFinal(policy string, runErr error, task *SubagentTask) bool {
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

func buildSubagentMainNotification(task *SubagentTask, runErr error) (string, string) {
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
		strings.TrimSpace(task.AgentID),
		strings.TrimSpace(task.ID),
		status,
		reason,
		summarizeSubagentText(firstNonEmpty(task.Label, task.Task), 120),
		summarizeSubagentText(task.Result, 280),
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

func (sm *SubagentManager) runWithRetry(ctx context.Context, task *SubagentTask) (string, error) {
	maxRetries := normalizePositiveBound(task.MaxRetries, 0, 8)
	backoffMs := normalizePositiveBound(task.RetryBackoff, 500, 120000)
	timeoutSec := normalizePositiveBound(task.TimeoutSec, 0, 3600)

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		result, err := runStringTaskWithCommandTickTimeout(
			ctx,
			timeoutSec,
			2*time.Second,
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
		lastErr = fmt.Errorf("subagent task failed with unknown error")
	}
	return "", lastErr
}

func (sm *SubagentManager) executeTaskOnce(ctx context.Context, task *SubagentTask) (string, error) {
	if task == nil {
		return "", fmt.Errorf("subagent task is nil")
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
		return "", fmt.Errorf("no llm provider configured for subagent execution")
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

func (sm *SubagentManager) resolveSystemPrompt(task *SubagentTask) string {
	systemPrompt := "You are a subagent. Follow workspace AGENTS.md and complete the task independently."
	workspacePrompt := sm.readWorkspacePromptFile("AGENTS.md")
	if workspacePrompt != "" {
		systemPrompt = "Workspace policy (AGENTS.md):\n" + workspacePrompt + "\n\nComplete the given task independently and report the result."
	}
	if task == nil {
		return systemPrompt
	}
	if promptFile := strings.TrimSpace(task.SystemPromptFile); promptFile != "" {
		if promptText := sm.readWorkspacePromptFile(promptFile); promptText != "" {
			return systemPrompt + "\n\nSubagent policy (" + promptFile + "):\n" + promptText
		}
	}
	if rolePrompt := strings.TrimSpace(task.SystemPrompt); rolePrompt != "" {
		return systemPrompt + "\n\nRole-specific profile prompt:\n" + rolePrompt
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

type SubagentRunFunc func(ctx context.Context, task *SubagentTask) (string, error)

func (sm *SubagentManager) SetRunFunc(f SubagentRunFunc) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.runFunc = f
	go sm.resumeRecoveredTasks()
}

func (sm *SubagentManager) ProfileStore() *SubagentProfileStore {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.profileStore
}

func (sm *SubagentManager) resumeRecoveredTasks() {
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
	toResume := make([]*SubagentTask, 0, len(taskIDs))
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

func (sm *SubagentManager) NextTaskSequence() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.nextID
}

func (sm *SubagentManager) GetTask(taskID string) (*SubagentTask, bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.pruneArchivedLocked()
	task, ok := sm.tasks[taskID]
	if !ok && sm.runStore != nil {
		return sm.runStore.Get(taskID)
	}
	return task, ok
}

func (sm *SubagentManager) ListTasks() []*SubagentTask {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.pruneArchivedLocked()

	tasks := make([]*SubagentTask, 0, len(sm.tasks))
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

func (sm *SubagentManager) KillTask(taskID string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	t, ok := sm.tasks[taskID]
	if !ok {
		return false
	}
	if cancel, ok := sm.cancelFuncs[taskID]; ok {
		cancel()
		delete(sm.cancelFuncs, taskID)
	}
	if t.Status == "running" {
		t.Status = "killed"
		t.WaitingReply = false
		t.Updated = time.Now().UnixMilli()
		sm.persistTaskLocked(t, "killed", "")
	}
	return true
}

func (sm *SubagentManager) SteerTask(taskID, message string) bool {
	return sm.sendTaskMessage(taskID, "main", "control", message, false, "")
}

func (sm *SubagentManager) SendTaskMessage(taskID, message string) bool {
	return sm.sendTaskMessage(taskID, "main", "message", message, false, "")
}

func (sm *SubagentManager) ReplyToTask(taskID, replyToMessageID, message string) bool {
	return sm.sendTaskMessage(taskID, "main", "reply", message, false, replyToMessageID)
}

func (sm *SubagentManager) AckTaskMessage(taskID, messageID string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	t, ok := sm.tasks[taskID]
	if !ok {
		return false
	}
	if sm.mailboxStore == nil {
		return false
	}
	if strings.TrimSpace(messageID) == "" {
		return false
	}
	t.Updated = time.Now().UnixMilli()
	msg, err := sm.mailboxStore.UpdateMessageStatus(messageID, "acked", t.Updated)
	if err != nil {
		return false
	}
	t.LastMessageID = msg.MessageID
	t.WaitingReply = false
	sm.persistTaskLocked(t, "acked", messageID)
	return true
}

func (sm *SubagentManager) ResumeTask(ctx context.Context, taskID string) (string, bool) {
	sm.mu.RLock()
	t, ok := sm.tasks[taskID]
	sm.mu.RUnlock()
	if !ok {
		return "", false
	}
	if strings.TrimSpace(t.Task) == "" {
		return "", false
	}
	label := strings.TrimSpace(t.Label)
	if label == "" {
		label = "resumed"
	} else {
		label = label + "-resumed"
	}
	_, err := sm.Spawn(ctx, SubagentSpawnOptions{
		Task:           t.Task,
		Label:          label,
		Role:           t.Role,
		AgentID:        t.AgentID,
		MaxRetries:     t.MaxRetries,
		RetryBackoff:   t.RetryBackoff,
		TimeoutSec:     t.TimeoutSec,
		MaxTaskChars:   t.MaxTaskChars,
		MaxResultChars: t.MaxResultChars,
		OriginChannel:  t.OriginChannel,
		OriginChatID:   t.OriginChatID,
		ThreadID:       t.ThreadID,
		CorrelationID:  t.CorrelationID,
		ParentRunID:    t.ID,
	})
	if err != nil {
		return "", false
	}
	sm.mu.Lock()
	if original, ok := sm.tasks[taskID]; ok {
		sm.persistTaskLocked(original, "resumed", label)
	}
	sm.mu.Unlock()
	return label, true
}

func (sm *SubagentManager) Events(taskID string, limit int) ([]SubagentRunEvent, error) {
	if sm.runStore == nil {
		return nil, nil
	}
	return sm.runStore.Events(taskID, limit)
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

func (sm *SubagentManager) TaskInbox(taskID string, limit int) ([]AgentMessage, error) {
	sm.mu.RLock()
	task, ok := sm.tasks[taskID]
	sm.mu.RUnlock()
	if !ok || sm.mailboxStore == nil {
		return nil, nil
	}
	return sm.mailboxStore.ThreadInbox(task.ThreadID, task.AgentID, limit)
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
	for id, t := range sm.tasks {
		if t.Status == "running" {
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

func buildSubagentSessionKey(agentID, taskID string) string {
	a := normalizeSubagentIdentifier(agentID)
	if a == "" {
		a = "default"
	}
	t := normalizeSubagentIdentifier(taskID)
	if t == "" {
		t = "task"
	}
	return fmt.Sprintf("subagent:%s:%s", a, t)
}

func (sm *SubagentManager) persistTaskLocked(task *SubagentTask, eventType, message string) {
	if task == nil || sm.runStore == nil {
		return
	}
	cp := cloneSubagentTask(task)
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

func (sm *SubagentManager) recordMailboxMessageLocked(task *SubagentTask, msg AgentMessage) {
	if sm.mailboxStore == nil || task == nil {
		return
	}
	if strings.TrimSpace(msg.ThreadID) == "" {
		msg.ThreadID = task.ThreadID
	}
	stored, err := sm.mailboxStore.AppendMessage(msg)
	if err != nil {
		return
	}
	task.LastMessageID = stored.MessageID
	if stored.RequiresReply {
		task.WaitingReply = true
	}
}

func (sm *SubagentManager) sendTaskMessage(taskID, fromAgent, msgType, message string, requiresReply bool, replyTo string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	t, ok := sm.tasks[taskID]
	if !ok {
		return false
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return false
	}
	fromAgent = strings.TrimSpace(fromAgent)
	if fromAgent == "" {
		fromAgent = "main"
	}
	t.Updated = time.Now().UnixMilli()
	if fromAgent == "main" {
		t.Steering = append(t.Steering, message)
	}
	if strings.TrimSpace(replyTo) == "" {
		replyTo = t.LastMessageID
	}
	toAgent := t.AgentID
	if fromAgent != "main" {
		toAgent = "main"
	}
	sm.recordMailboxMessageLocked(t, AgentMessage{
		ThreadID:      t.ThreadID,
		FromAgent:     fromAgent,
		ToAgent:       toAgent,
		ReplyTo:       replyTo,
		CorrelationID: t.CorrelationID,
		Type:          msgType,
		Content:       message,
		RequiresReply: requiresReply,
		Status:        "queued",
		CreatedAt:     t.Updated,
	})
	switch msgType {
	case "control":
		sm.persistTaskLocked(t, "steered", message)
	case "reply":
		sm.persistTaskLocked(t, "reply_sent", message)
	default:
		sm.persistTaskLocked(t, "message_sent", message)
	}
	return true
}

func (sm *SubagentManager) consumeThreadInbox(task *SubagentTask) (string, []string) {
	if task == nil || sm.mailboxStore == nil {
		return "", nil
	}
	msgs, err := sm.mailboxStore.ThreadInbox(task.ThreadID, task.AgentID, 0)
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
