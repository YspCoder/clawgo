package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"clawgo/pkg/bus"
	"clawgo/pkg/providers"
)

type SubagentTask struct {
	ID            string
	Task          string
	Label         string
	Role          string
	PipelineID    string
	PipelineTask  string
	SharedState   map[string]interface{}
	OriginChannel string
	OriginChatID  string
	Status        string
	Result        string
	Steering      []string
	Created       int64
	Updated       int64
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
}

func NewSubagentManager(provider providers.LLMProvider, workspace string, bus *bus.MessageBus, orc *Orchestrator) *SubagentManager {
	return &SubagentManager{
		tasks:              make(map[string]*SubagentTask),
		cancelFuncs:        make(map[string]context.CancelFunc),
		archiveAfterMinute: 60,
		provider:           provider,
		bus:                bus,
		orc:                orc,
		workspace:          workspace,
		nextID:             1,
	}
}

func (sm *SubagentManager) Spawn(ctx context.Context, task, label, originChannel, originChatID, pipelineID, pipelineTask string) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	taskID := fmt.Sprintf("subagent-%d", sm.nextID)
	sm.nextID++

	now := time.Now().UnixMilli()
	subagentTask := &SubagentTask{
		ID:            taskID,
		Task:          task,
		Label:         label,
		PipelineID:    pipelineID,
		PipelineTask:  pipelineTask,
		OriginChannel: originChannel,
		OriginChatID:  originChatID,
		Status:        "running",
		Created:       now,
		Updated:       now,
	}
	taskCtx, cancel := context.WithCancel(ctx)
	sm.tasks[taskID] = subagentTask
	sm.cancelFuncs[taskID] = cancel

	go sm.runTask(taskCtx, subagentTask)

	desc := fmt.Sprintf("Spawned subagent for task: %s", task)
	if label != "" {
		desc = fmt.Sprintf("Spawned subagent '%s' for task: %s", label, task)
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

	// 1. Independent agent logic: supports recursive tool calling.
	// This lightweight approach reuses AgentLoop logic for full subagent capability.
	// subagent.go cannot depend on agent package inversely, so use function injection.

	// Fall back to one-shot chat when RunFunc is not injected.
	if sm.runFunc != nil {
		result, err := sm.runFunc(ctx, task.Task, task.OriginChannel, task.OriginChatID)
		sm.mu.Lock()
		if err != nil {
			task.Status = "failed"
			task.Result = fmt.Sprintf("Error: %v", err)
			task.Updated = time.Now().UnixMilli()
			if sm.orc != nil && task.PipelineID != "" && task.PipelineTask != "" {
				_ = sm.orc.MarkTaskDone(task.PipelineID, task.PipelineTask, task.Result, err)
			}
		} else {
			task.Status = "completed"
			task.Result = result
			task.Updated = time.Now().UnixMilli()
			if sm.orc != nil && task.PipelineID != "" && task.PipelineTask != "" {
				_ = sm.orc.MarkTaskDone(task.PipelineID, task.PipelineTask, task.Result, nil)
			}
		}
		sm.mu.Unlock()
	} else {
		// Original one-shot logic
		messages := []providers.Message{
			{
				Role:    "system",
				Content: "You are a subagent. Complete the given task independently and report the result.",
			},
			{
				Role:    "user",
				Content: task.Task,
			},
		}

		response, err := sm.provider.Chat(ctx, messages, nil, sm.provider.GetDefaultModel(), map[string]interface{}{
			"max_tokens": 4096,
		})

		sm.mu.Lock()
		if err != nil {
			task.Status = "failed"
			task.Result = fmt.Sprintf("Error: %v", err)
			task.Updated = time.Now().UnixMilli()
			if sm.orc != nil && task.PipelineID != "" && task.PipelineTask != "" {
				_ = sm.orc.MarkTaskDone(task.PipelineID, task.PipelineTask, task.Result, err)
			}
		} else {
			task.Status = "completed"
			task.Result = response.Content
			task.Updated = time.Now().UnixMilli()
			if sm.orc != nil && task.PipelineID != "" && task.PipelineTask != "" {
				_ = sm.orc.MarkTaskDone(task.PipelineID, task.PipelineTask, task.Result, nil)
			}
		}
		sm.mu.Unlock()
	}

	// 2. Result broadcast (keep existing behavior)
	if sm.bus != nil {
		prefix := "Task completed"
		if task.Label != "" {
			prefix = fmt.Sprintf("Task '%s' completed", task.Label)
		}
		announceContent := fmt.Sprintf("%s.\n\nResult:\n%s", prefix, task.Result)
		if task.PipelineID != "" && task.PipelineTask != "" {
			announceContent += fmt.Sprintf("\n\nPipeline: %s\nPipeline Task: %s", task.PipelineID, task.PipelineTask)
		}
		sm.bus.PublishInbound(bus.InboundMessage{
			Channel:    "system",
			SenderID:   fmt.Sprintf("subagent:%s", task.ID),
			ChatID:     fmt.Sprintf("%s:%s", task.OriginChannel, task.OriginChatID),
			SessionKey: fmt.Sprintf("subagent:%s", task.ID),
			Content:    announceContent,
			Metadata: map[string]string{
				"trigger":    "subagent",
				"subagent_id": task.ID,
			},
		})
	}
}

type SubagentRunFunc func(ctx context.Context, task, channel, chatID string) (string, error)

func (sm *SubagentManager) SetRunFunc(f SubagentRunFunc) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.runFunc = f
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
