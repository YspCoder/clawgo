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
	workspace string
	nextID    int
	runFunc   SubagentRunFunc
}

func NewSubagentManager(provider providers.LLMProvider, workspace string, bus *bus.MessageBus) *SubagentManager {
	return &SubagentManager{
		tasks:     make(map[string]*SubagentTask),
		provider:  provider,
		bus:       bus,
		workspace: workspace,
		nextID:    1,
	}
}

func (sm *SubagentManager) Spawn(ctx context.Context, task, label, originChannel, originChatID string) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	taskID := fmt.Sprintf("subagent-%d", sm.nextID)
	sm.nextID++

	subagentTask := &SubagentTask{
		ID:            taskID,
		Task:          task,
		Label:         label,
		OriginChannel: originChannel,
		OriginChatID:  originChatID,
		Status:        "running",
		Created:       time.Now().UnixMilli(),
	}
	sm.tasks[taskID] = subagentTask

	go sm.runTask(ctx, subagentTask)

	if label != "" {
		return fmt.Sprintf("Spawned subagent '%s' for task: %s", label, task), nil
	}
	return fmt.Sprintf("Spawned subagent for task: %s", task), nil
}

func (sm *SubagentManager) runTask(ctx context.Context, task *SubagentTask) {
	task.Status = "running"
	task.Created = time.Now().UnixMilli()

	// 1. 独立 Agent 逻辑：支持递归工具调用
	// 这里简单实现：通过共享 AgentLoop 的逻辑来实现 full subagent 能力
	// 但目前 subagent.go 不方便反向依赖 agent 包，我们暂时通过 Inject 方式解决
	
	// 如果没有注入 RunFunc，则退化为简单的一步 Chat
	if sm.runFunc != nil {
		result, err := sm.runFunc(ctx, task.Task, task.OriginChannel, task.OriginChatID)
		sm.mu.Lock()
		if err != nil {
			task.Status = "failed"
			task.Result = fmt.Sprintf("Error: %v", err)
		} else {
			task.Status = "completed"
			task.Result = result
		}
		sm.mu.Unlock()
	} else {
		// 原有的 One-shot 逻辑
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
		} else {
			task.Status = "completed"
			task.Result = response.Content
		}
		sm.mu.Unlock()
	}

	// 2. 结果广播 (原有逻辑保持)
	if sm.bus != nil {
		prefix := "Task completed"
		if task.Label != "" {
			prefix = fmt.Sprintf("Task '%s' completed", task.Label)
		}
		announceContent := fmt.Sprintf("%s.\n\nResult:\n%s", prefix, task.Result)
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
