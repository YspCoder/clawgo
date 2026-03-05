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
			"pipeline_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional pipeline ID for orchestrated multi-agent workflow",
			},
			"task_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional task ID under the pipeline",
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
	pipelineID, _ := args["pipeline_id"].(string)
	taskID, _ := args["task_id"].(string)
	if label == "" && role != "" {
		label = role
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

	result, err := t.manager.Spawn(ctx, task, label, originChannel, originChatID, pipelineID, taskID)
	if err != nil {
		return "", fmt.Errorf("failed to spawn subagent: %w", err)
	}

	return result, nil
}
