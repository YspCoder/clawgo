package tools

import (
	"context"
	"fmt"
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
	Created       int64
}

type SubagentManager struct {
	tasks     map[string]*SubagentTask
	mu        sync.RWMutex
	provider  providers.LLMProvider
	bus       *bus.MessageBus
	orc       *Orchestrator
	workspace string
	nextID    int
	runFunc   SubagentRunFunc
}

func NewSubagentManager(provider providers.LLMProvider, workspace string, bus *bus.MessageBus, orc *Orchestrator) *SubagentManager {
	return &SubagentManager{
		tasks:     make(map[string]*SubagentTask),
		provider:  provider,
		bus:       bus,
		orc:       orc,
		workspace: workspace,
		nextID:    1,
	}
}

func (sm *SubagentManager) Spawn(ctx context.Context, task, label, originChannel, originChatID, pipelineID, pipelineTask string) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	taskID := fmt.Sprintf("subagent-%d", sm.nextID)
	sm.nextID++

	subagentTask := &SubagentTask{
		ID:            taskID,
		Task:          task,
		Label:         label,
		PipelineID:    pipelineID,
		PipelineTask:  pipelineTask,
		OriginChannel: originChannel,
		OriginChatID:  originChatID,
		Status:        "running",
		Created:       time.Now().UnixMilli(),
	}
	sm.tasks[taskID] = subagentTask

	go sm.runTask(ctx, subagentTask)

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
	sm.mu.Lock()
	task.Status = "running"
	task.Created = time.Now().UnixMilli()
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
			if sm.orc != nil && task.PipelineID != "" && task.PipelineTask != "" {
				_ = sm.orc.MarkTaskDone(task.PipelineID, task.PipelineTask, task.Result, err)
			}
		} else {
			task.Status = "completed"
			task.Result = result
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
			if sm.orc != nil && task.PipelineID != "" && task.PipelineTask != "" {
				_ = sm.orc.MarkTaskDone(task.PipelineID, task.PipelineTask, task.Result, err)
			}
		} else {
			task.Status = "completed"
			task.Result = response.Content
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
			Channel:  "system",
			SenderID: fmt.Sprintf("subagent:%s", task.ID),
			ChatID:   fmt.Sprintf("%s:%s", task.OriginChannel, task.OriginChatID),
			Content:  announceContent,
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
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	task, ok := sm.tasks[taskID]
	return task, ok
}

func (sm *SubagentManager) ListTasks() []*SubagentTask {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	tasks := make([]*SubagentTask, 0, len(sm.tasks))
	for _, task := range sm.tasks {
		tasks = append(tasks, task)
	}
	return tasks
}
