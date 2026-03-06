package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

type SubagentConfigTool struct {
	mu         sync.RWMutex
	configPath string
}

func NewSubagentConfigTool(configPath string) *SubagentConfigTool {
	return &SubagentConfigTool{configPath: strings.TrimSpace(configPath)}
}

func (t *SubagentConfigTool) Name() string { return "subagent_config" }

func (t *SubagentConfigTool) Description() string {
	return "Draft or persist subagent config and router rules into config.json."
}

func (t *SubagentConfigTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "draft|upsert",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Natural-language worker description for draft or upsert.",
			},
			"agent_id_hint": map[string]interface{}{
				"type":        "string",
				"description": "Optional preferred agent id seed for draft.",
			},
			"agent_id":         map[string]interface{}{"type": "string"},
			"role":             map[string]interface{}{"type": "string"},
			"display_name":     map[string]interface{}{"type": "string"},
			"system_prompt":    map[string]interface{}{"type": "string"},
			"memory_namespace": map[string]interface{}{"type": "string"},
			"type":             map[string]interface{}{"type": "string"},
			"tool_allowlist": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			"routing_keywords": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
		},
		"required": []string{"action"},
	}
}

func (t *SubagentConfigTool) SetConfigPath(path string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.configPath = strings.TrimSpace(path)
}

func (t *SubagentConfigTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	_ = ctx
	switch stringArgFromMap(args, "action") {
	case "draft":
		description := stringArgFromMap(args, "description")
		if description == "" {
			return "", fmt.Errorf("description is required")
		}
		return marshalSubagentConfigPayload(map[string]interface{}{
			"draft": DraftConfigSubagent(description, stringArgFromMap(args, "agent_id_hint")),
		})
	case "upsert":
		result, err := UpsertConfigSubagent(t.getConfigPath(), cloneSubagentConfigArgs(args))
		if err != nil {
			return "", err
		}
		return marshalSubagentConfigPayload(result)
	default:
		return "", fmt.Errorf("unsupported action")
	}
}

func (t *SubagentConfigTool) getConfigPath() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.configPath
}

func cloneSubagentConfigArgs(args map[string]interface{}) map[string]interface{} {
	if args == nil {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(args))
	for k, v := range args {
		if k == "action" || k == "agent_id_hint" {
			continue
		}
		out[k] = v
	}
	return out
}

func marshalSubagentConfigPayload(payload map[string]interface{}) (string, error) {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
