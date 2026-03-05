package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clawgo/pkg/bus"
	"clawgo/pkg/providers"
)

type SubagentTask struct {
	ID             string                 `json:"id"`
	Task           string                 `json:"task"`
	Label          string                 `json:"label"`
	Role           string                 `json:"role"`
	AgentID        string                 `json:"agent_id"`
	SessionKey     string                 `json:"session_key"`
	MemoryNS       string                 `json:"memory_ns"`
	SystemPrompt   string                 `json:"system_prompt,omitempty"`
	ToolAllowlist  []string               `json:"tool_allowlist,omitempty"`
	MaxRetries     int                    `json:"max_retries,omitempty"`
	RetryBackoff   int                    `json:"retry_backoff,omitempty"`
	TimeoutSec     int                    `json:"timeout_sec,omitempty"`
	MaxTaskChars   int                    `json:"max_task_chars,omitempty"`
	MaxResultChars int                    `json:"max_result_chars,omitempty"`
	RetryCount     int                    `json:"retry_count,omitempty"`
	PipelineID     string                 `json:"pipeline_id,omitempty"`
	PipelineTask   string                 `json:"pipeline_task,omitempty"`
	SharedState    map[string]interface{} `json:"shared_state,omitempty"`
	OriginChannel  string                 `json:"origin_channel,omitempty"`
	OriginChatID   string                 `json:"origin_chat_id,omitempty"`
	Status         string                 `json:"status"`
	Result         string                 `json:"result,omitempty"`
	Steering       []string               `json:"steering,omitempty"`
	Created        int64                  `json:"created"`
	Updated        int64                  `json:"updated"`
}

type SubagentManager struct {
	tasks              map[string]*SubagentTask
	cancelFuncs        map[string]context.CancelFunc
	archiveAfterMinute int64
	mu                 sync.RWMutex
	provider           providers.LLMProvider
	bus                *bus.MessageBus
	orc                *Orchestrator
	workspace          string
	nextID             int
	runFunc            SubagentRunFunc
	profileStore       *SubagentProfileStore
}

type SubagentSpawnOptions struct {
	Task           string
	Label          string
	Role           string
	AgentID        string
	MaxRetries     int
	RetryBackoff   int
	TimeoutSec     int
	MaxTaskChars   int
	MaxResultChars int
	OriginChannel  string
	OriginChatID   string
	PipelineID     string
	PipelineTask   string
}

func NewSubagentManager(provider providers.LLMProvider, workspace string, bus *bus.MessageBus, orc *Orchestrator) *SubagentManager {
	store := NewSubagentProfileStore(workspace)
	return &SubagentManager{
		tasks:              make(map[string]*SubagentTask),
		cancelFuncs:        make(map[string]context.CancelFunc),
		archiveAfterMinute: 60,
		provider:           provider,
		bus:                bus,
		orc:                orc,
		workspace:          workspace,
		nextID:             1,
		profileStore:       store,
	}
}

func (sm *SubagentManager) Spawn(ctx context.Context, opts SubagentSpawnOptions) (string, error) {
	task := strings.TrimSpace(opts.Task)
	if task == "" {
		return "", fmt.Errorf("task is required")
	}
	label := strings.TrimSpace(opts.Label)
	role := strings.TrimSpace(opts.Role)
	agentID := normalizeSubagentIdentifier(opts.AgentID)
	originalRole := role
	var profile *SubagentProfile
	if sm.profileStore != nil {
		if agentID != "" {
			if p, ok, err := sm.profileStore.Get(agentID); err != nil {
				return "", err
			} else if ok {
				profile = p
			}
		} else if role != "" {
			if p, ok, err := sm.profileStore.FindByRole(role); err != nil {
				return "", err
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
	toolAllowlist := []string(nil)
	maxRetries := 0
	retryBackoff := 1000
	timeoutSec := 0
	maxTaskChars := 0
	maxResultChars := 0
	if profile == nil && sm.profileStore != nil {
		if p, ok, err := sm.profileStore.Get(agentID); err != nil {
			return "", err
		} else if ok {
			profile = p
		}
	}
	if profile != nil {
		if strings.EqualFold(strings.TrimSpace(profile.Status), "disabled") {
			return "", fmt.Errorf("subagent profile '%s' is disabled", profile.AgentID)
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
		systemPrompt = strings.TrimSpace(profile.SystemPrompt)
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
		return "", fmt.Errorf("task exceeds max_task_chars quota (%d > %d)", len(task), maxTaskChars)
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
	pipelineID := strings.TrimSpace(opts.PipelineID)
	pipelineTask := strings.TrimSpace(opts.PipelineTask)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	taskID := fmt.Sprintf("subagent-%d", sm.nextID)
	sm.nextID++
	sessionKey := buildSubagentSessionKey(agentID, taskID)

	now := time.Now().UnixMilli()
	subagentTask := &SubagentTask{
		ID:             taskID,
		Task:           task,
		Label:          label,
		Role:           role,
		AgentID:        agentID,
		SessionKey:     sessionKey,
		MemoryNS:       memoryNS,
		SystemPrompt:   systemPrompt,
		ToolAllowlist:  toolAllowlist,
		MaxRetries:     maxRetries,
		RetryBackoff:   retryBackoff,
		TimeoutSec:     timeoutSec,
		MaxTaskChars:   maxTaskChars,
		MaxResultChars: maxResultChars,
		RetryCount:     0,
		PipelineID:     pipelineID,
		PipelineTask:   pipelineTask,
		OriginChannel:  originChannel,
		OriginChatID:   originChatID,
		Status:         "running",
		Created:        now,
		Updated:        now,
	}
	taskCtx, cancel := context.WithCancel(ctx)
	sm.tasks[taskID] = subagentTask
	sm.cancelFuncs[taskID] = cancel

	go sm.runTask(taskCtx, subagentTask)

	desc := fmt.Sprintf("Spawned subagent for task: %s (agent=%s)", task, agentID)
	if label != "" {
		desc = fmt.Sprintf("Spawned subagent '%s' for task: %s (agent=%s)", label, task, agentID)
	}
	if role != "" {
		desc += fmt.Sprintf(" role=%s", role)
	}
	if pipelineID != "" && pipelineTask != "" {
		desc += fmt.Sprintf(" (pipeline=%s task=%s)", pipelineID, pipelineTask)
	}
	return desc, nil
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
	sm.mu.Unlock()

	if sm.orc != nil && task.PipelineID != "" && task.PipelineTask != "" {
		_ = sm.orc.MarkTaskRunning(task.PipelineID, task.PipelineTask)
	}
	result, runErr := sm.runWithRetry(ctx, task)
	sm.mu.Lock()
	if runErr != nil {
		task.Status = "failed"
		task.Result = fmt.Sprintf("Error: %v", runErr)
		task.Result = applySubagentResultQuota(task.Result, task.MaxResultChars)
		task.Updated = time.Now().UnixMilli()
		if sm.orc != nil && task.PipelineID != "" && task.PipelineTask != "" {
			_ = sm.orc.MarkTaskDone(task.PipelineID, task.PipelineTask, task.Result, runErr)
		}
	} else {
		task.Status = "completed"
		task.Result = applySubagentResultQuota(result, task.MaxResultChars)
		task.Updated = time.Now().UnixMilli()
		if sm.orc != nil && task.PipelineID != "" && task.PipelineTask != "" {
			_ = sm.orc.MarkTaskDone(task.PipelineID, task.PipelineTask, task.Result, nil)
		}
	}
	sm.mu.Unlock()

	// 2. Result broadcast (keep existing behavior)
	if sm.bus != nil {
		prefix := "Task completed"
		if runErr != nil {
			prefix = "Task failed"
		}
		if task.Label != "" {
			if runErr != nil {
				prefix = fmt.Sprintf("Task '%s' failed", task.Label)
			} else {
				prefix = fmt.Sprintf("Task '%s' completed", task.Label)
			}
		}
		announceContent := fmt.Sprintf("%s.\n\nResult:\n%s", prefix, task.Result)
		if task.PipelineID != "" && task.PipelineTask != "" {
			announceContent += fmt.Sprintf("\n\nPipeline: %s\nPipeline Task: %s", task.PipelineID, task.PipelineTask)
		}
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
				"pipeline_id":   task.PipelineID,
				"pipeline_task": task.PipelineTask,
				"status":        task.Status,
			},
		})
	}
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
			sm.mu.Unlock()
			return result, nil
		}
		lastErr = err
		sm.mu.Lock()
		task.RetryCount = attempt
		task.Updated = time.Now().UnixMilli()
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
	if sm.runFunc != nil {
		return sm.runFunc(ctx, task)
	}
	if sm.provider == nil {
		return "", fmt.Errorf("no llm provider configured for subagent execution")
	}

	systemPrompt := "You are a subagent. Follow workspace AGENTS.md and complete the task independently."
	rolePrompt := strings.TrimSpace(task.SystemPrompt)
	if ws := strings.TrimSpace(sm.workspace); ws != "" {
		if data, err := os.ReadFile(filepath.Join(ws, "AGENTS.md")); err == nil {
			txt := strings.TrimSpace(string(data))
			if txt != "" {
				systemPrompt = "Workspace policy (AGENTS.md):\n" + txt + "\n\nComplete the given task independently and report the result."
			}
		}
	}
	if rolePrompt != "" {
		systemPrompt += "\n\nRole-specific profile prompt:\n" + rolePrompt
	}
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

	response, err := sm.provider.Chat(ctx, messages, nil, sm.provider.GetDefaultModel(), map[string]interface{}{
		"max_tokens": 4096,
	})
	if err != nil {
		return "", err
	}
	return response.Content, nil
}

type SubagentRunFunc func(ctx context.Context, task *SubagentTask) (string, error)

func (sm *SubagentManager) SetRunFunc(f SubagentRunFunc) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.runFunc = f
}

func (sm *SubagentManager) ProfileStore() *SubagentProfileStore {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.profileStore
}

func (sm *SubagentManager) GetTask(taskID string) (*SubagentTask, bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.pruneArchivedLocked()
	task, ok := sm.tasks[taskID]
	return task, ok
}

func (sm *SubagentManager) ListTasks() []*SubagentTask {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.pruneArchivedLocked()

	tasks := make([]*SubagentTask, 0, len(sm.tasks))
	for _, task := range sm.tasks {
		tasks = append(tasks, task)
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
		t.Updated = time.Now().UnixMilli()
	}
	return true
}

func (sm *SubagentManager) SteerTask(taskID, message string) bool {
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
	t.Steering = append(t.Steering, message)
	t.Updated = time.Now().UnixMilli()
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
		PipelineID:     t.PipelineID,
		PipelineTask:   t.PipelineTask,
	})
	if err != nil {
		return "", false
	}
	return label, true
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
