package tools

import (
	"context"
	"fmt"
	"sync"
)

type SpawnTool struct {
	mu            sync.RWMutex
	manager       *SubagentManager
	originChannel string
	originChatID  string
}

func NewSpawnTool(manager *SubagentManager) *SpawnTool {
	return &SpawnTool{
		manager:       manager,
		originChannel: "cli",
		originChatID:  "direct",
	}
}

func (t *SpawnTool) Name() string {
	return "spawn"
}

func (t *SpawnTool) Description() string {
	return "Spawn a subagent to handle a task in the background. Use this for complex or time-consuming tasks that can run independently. The subagent will complete the task and report back when done."
}

func (t *SpawnTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task": map[string]interface{}{
				"type":        "string",
				"description": "The task for subagent to complete",
			},
			"label": map[string]interface{}{
				"type":        "string",
				"description": "Optional short label for the task (for display)",
			},
			"role": map[string]interface{}{
				"type":        "string",
				"description": "Optional role for this subagent, e.g. research/coding/testing",
			},
			"agent_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional logical agent ID. If omitted, role will be used as fallback.",
			},
			"max_retries": map[string]interface{}{
				"type":        "integer",
				"description": "Optional retry limit for this task.",
			},
			"retry_backoff_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Optional retry backoff in milliseconds.",
			},
			"timeout_sec": map[string]interface{}{
				"type":        "integer",
				"description": "Optional per-attempt timeout in seconds.",
			},
			"max_tool_iterations": map[string]interface{}{
				"type":        "integer",
				"description": "Optional independent tool-calling iteration budget.",
			},
			"max_task_chars": map[string]interface{}{
				"type":        "integer",
				"description": "Optional task size quota in characters.",
			},
			"max_result_chars": map[string]interface{}{
				"type":        "integer",
				"description": "Optional result size quota in characters.",
			},
			"channel": map[string]interface{}{
				"type":        "string",
				"description": "Optional origin channel override",
			},
			"chat_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional origin chat ID override",
			},
		},
		"required": []string{"task"},
	}
}

func (t *SpawnTool) SetContext(channel, chatID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.originChannel = channel
	t.originChatID = chatID
}

func (t *SpawnTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	task := MapStringArg(args, "task")
	if task == "" {
		return "", fmt.Errorf("task is required")
	}

	label := MapStringArg(args, "label")
	role := MapStringArg(args, "role")
	agentID := MapStringArg(args, "agent_id")
	maxRetries := MapIntArg(args, "max_retries", 0)
	retryBackoff := MapIntArg(args, "retry_backoff_ms", 0)
	timeoutSec := MapIntArg(args, "timeout_sec", 0)
	maxToolIterations := MapIntArg(args, "max_tool_iterations", 0)
	maxTaskChars := MapIntArg(args, "max_task_chars", 0)
	maxResultChars := MapIntArg(args, "max_result_chars", 0)
	if label == "" && role != "" {
		label = role
	} else if label == "" && agentID != "" {
		label = agentID
	}

	if t.manager == nil {
		return "Error: Subagent manager not configured", nil
	}

	originChannel := MapStringArg(args, "channel")
	originChatID := MapStringArg(args, "chat_id")
	if originChannel == "" || originChatID == "" {
		t.mu.RLock()
		defaultChannel := t.originChannel
		defaultChatID := t.originChatID
		t.mu.RUnlock()
		if originChannel == "" {
			originChannel = defaultChannel
		}
		if originChatID == "" {
			originChatID = defaultChatID
		}
	}

	result, err := t.manager.Spawn(ctx, SubagentSpawnOptions{
		Task:              task,
		Label:             label,
		Role:              role,
		AgentID:           agentID,
		MaxRetries:        maxRetries,
		RetryBackoff:      retryBackoff,
		TimeoutSec:        timeoutSec,
		MaxToolIterations: maxToolIterations,
		MaxTaskChars:      maxTaskChars,
		MaxResultChars:    maxResultChars,
		OriginChannel:     originChannel,
		OriginChatID:      originChatID,
	})
	if err != nil {
		return "", fmt.Errorf("failed to spawn subagent: %w", err)
	}

	return result, nil
}
