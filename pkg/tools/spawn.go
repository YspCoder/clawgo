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
	task, ok := args["task"].(string)
	if !ok {
		return "", fmt.Errorf("task is required")
	}

	label, _ := args["label"].(string)
	role, _ := args["role"].(string)
	agentID, _ := args["agent_id"].(string)
	maxRetries := intArg(args, "max_retries")
	retryBackoff := intArg(args, "retry_backoff_ms")
	timeoutSec := intArg(args, "timeout_sec")
	maxTaskChars := intArg(args, "max_task_chars")
	maxResultChars := intArg(args, "max_result_chars")
	if label == "" && role != "" {
		label = role
	} else if label == "" && agentID != "" {
		label = agentID
	}

	if t.manager == nil {
		return "Error: Subagent manager not configured", nil
	}

	originChannel, _ := args["channel"].(string)
	originChatID, _ := args["chat_id"].(string)
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
		Task:           task,
		Label:          label,
		Role:           role,
		AgentID:        agentID,
		MaxRetries:     maxRetries,
		RetryBackoff:   retryBackoff,
		TimeoutSec:     timeoutSec,
		MaxTaskChars:   maxTaskChars,
		MaxResultChars: maxResultChars,
		OriginChannel:  originChannel,
		OriginChatID:   originChatID,
	})
	if err != nil {
		return "", fmt.Errorf("failed to spawn subagent: %w", err)
	}

	return result, nil
}

func intArg(args map[string]interface{}, key string) int {
	if args == nil {
		return 0
	}
	if v, ok := args[key].(float64); ok {
		return int(v)
	}
	if v, ok := args[key].(int); ok {
		return v
	}
	if v, ok := args[key].(int64); ok {
		return int(v)
	}
	return 0
}
