package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type PipelineCreateTool struct {
	orc *Orchestrator
}

func NewPipelineCreateTool(orc *Orchestrator) *PipelineCreateTool {
	return &PipelineCreateTool{orc: orc}
}

func (t *PipelineCreateTool) Name() string { return "pipeline_create" }

func (t *PipelineCreateTool) Description() string {
	return "Create a multi-agent pipeline with standardized task protocol (role/goal/dependencies/shared state)."
}

func (t *PipelineCreateTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"label": map[string]interface{}{
				"type":        "string",
				"description": "Optional short pipeline label",
			},
			"objective": map[string]interface{}{
				"type":        "string",
				"description": "Top-level objective for this pipeline",
			},
			"tasks": map[string]interface{}{
				"type":        "array",
				"description": "Task list with id/role/goal/depends_on",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{"type": "string"},
						"role": map[string]interface{}{
							"type":        "string",
							"description": "Agent role, e.g. research/coding/testing",
						},
						"goal": map[string]interface{}{"type": "string"},
						"depends_on": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
					},
					"required": []string{"id", "goal"},
				},
			},
		},
		"required": []string{"objective", "tasks"},
	}
}

func (t *PipelineCreateTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	if t.orc == nil {
		return "", fmt.Errorf("orchestrator is not configured")
	}

	objective, _ := args["objective"].(string)
	label, _ := args["label"].(string)

	rawTasks, ok := args["tasks"].([]interface{})
	if !ok || len(rawTasks) == 0 {
		return "", fmt.Errorf("tasks is required")
	}

	specs := make([]PipelineSpec, 0, len(rawTasks))
	for i, item := range rawTasks {
		m, ok := item.(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("tasks[%d] must be object", i)
		}
		id, _ := m["id"].(string)
		role, _ := m["role"].(string)
		goal, _ := m["goal"].(string)

		deps := make([]string, 0)
		if rawDeps, ok := m["depends_on"].([]interface{}); ok {
			for _, dep := range rawDeps {
				if depS, ok := dep.(string); ok {
					deps = append(deps, depS)
				}
			}
		}
		specs = append(specs, PipelineSpec{
			ID:        id,
			Role:      role,
			Goal:      goal,
			DependsOn: deps,
		})
	}

	p, err := t.orc.CreatePipeline(label, objective, "tool", "tool", specs)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Pipeline created: %s (%d tasks)\nUse spawn with pipeline_id/task_id to run tasks.\nUse pipeline_dispatch to dispatch ready tasks.",
		p.ID, len(p.Tasks)), nil
}

type PipelineStatusTool struct {
	orc *Orchestrator
}

func NewPipelineStatusTool(orc *Orchestrator) *PipelineStatusTool {
	return &PipelineStatusTool{orc: orc}
}

func (t *PipelineStatusTool) Name() string { return "pipeline_status" }

func (t *PipelineStatusTool) Description() string {
	return "Get pipeline status, tasks progress, and shared state. If pipeline_id is empty, list recent pipelines."
}

func (t *PipelineStatusTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pipeline_id": map[string]interface{}{
				"type":        "string",
				"description": "Pipeline ID",
			},
		},
	}
}

func (t *PipelineStatusTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	if t.orc == nil {
		return "", fmt.Errorf("orchestrator is not configured")
	}
	pipelineID, _ := args["pipeline_id"].(string)
	pipelineID = strings.TrimSpace(pipelineID)

	if pipelineID == "" {
		items := t.orc.ListPipelines()
		if len(items) == 0 {
			return "No pipelines found.", nil
		}
		var sb strings.Builder
		sb.WriteString("Pipelines:\n")
		for _, p := range items {
			sb.WriteString(fmt.Sprintf("- %s [%s] %s\n", p.ID, p.Status, p.Label))
		}
		return sb.String(), nil
	}

	return t.orc.SnapshotJSON(pipelineID)
}

type PipelineStateSetTool struct {
	orc *Orchestrator
}

func NewPipelineStateSetTool(orc *Orchestrator) *PipelineStateSetTool {
	return &PipelineStateSetTool{orc: orc}
}

func (t *PipelineStateSetTool) Name() string { return "pipeline_state_set" }

func (t *PipelineStateSetTool) Description() string {
	return "Set shared state key/value for a pipeline, allowing sub-agents to share intermediate results."
}

func (t *PipelineStateSetTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pipeline_id": map[string]interface{}{"type": "string"},
			"key":         map[string]interface{}{"type": "string"},
			"value": map[string]interface{}{
				"description": "Any JSON-serializable value",
			},
		},
		"required": []string{"pipeline_id", "key", "value"},
	}
}

func (t *PipelineStateSetTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	if t.orc == nil {
		return "", fmt.Errorf("orchestrator is not configured")
	}
	pipelineID, _ := args["pipeline_id"].(string)
	key, _ := args["key"].(string)
	value, ok := args["value"]
	if !ok {
		return "", fmt.Errorf("value is required")
	}
	if err := t.orc.SetSharedState(strings.TrimSpace(pipelineID), strings.TrimSpace(key), value); err != nil {
		return "", err
	}
	return fmt.Sprintf("Updated pipeline shared state: %s.%s", pipelineID, key), nil
}

type PipelineDispatchTool struct {
	orc   *Orchestrator
	spawn *SubagentManager
}

func NewPipelineDispatchTool(orc *Orchestrator, spawn *SubagentManager) *PipelineDispatchTool {
	return &PipelineDispatchTool{orc: orc, spawn: spawn}
}

func (t *PipelineDispatchTool) Name() string { return "pipeline_dispatch" }

func (t *PipelineDispatchTool) Description() string {
	return "Dispatch all dependency-ready tasks in a pipeline by spawning subagents automatically."
}

func (t *PipelineDispatchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pipeline_id": map[string]interface{}{
				"type":        "string",
				"description": "Pipeline ID",
			},
			"max_dispatch": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of tasks to dispatch in this call (default 3)",
				"default":     3,
			},
		},
		"required": []string{"pipeline_id"},
	}
}

func (t *PipelineDispatchTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if t.orc == nil || t.spawn == nil {
		return "", fmt.Errorf("pipeline dispatcher is not configured")
	}

	pipelineID, _ := args["pipeline_id"].(string)
	pipelineID = strings.TrimSpace(pipelineID)
	if pipelineID == "" {
		return "", fmt.Errorf("pipeline_id is required")
	}

	maxDispatch := 3
	if raw, ok := args["max_dispatch"].(float64); ok && raw > 0 {
		maxDispatch = int(raw)
	}

	ready, err := t.orc.ReadyTasks(pipelineID)
	if err != nil {
		return "", err
	}
	if len(ready) == 0 {
		return fmt.Sprintf("No ready tasks for pipeline %s", pipelineID), nil
	}

	dispatched := 0
	var lines []string

	for _, task := range ready {
		if dispatched >= maxDispatch {
			break
		}
		shared := map[string]interface{}{}
		if p, ok := t.orc.GetPipeline(pipelineID); ok {
			for k, v := range p.SharedState {
				shared[k] = v
			}
		}

		payload := task.Goal
		if len(shared) > 0 {
			sharedJSON, _ := json.Marshal(shared)
			payload = fmt.Sprintf("%s\n\nShared State:\n%s", payload, string(sharedJSON))
		}

		label := task.ID
		if task.Role != "" {
			label = fmt.Sprintf("%s:%s", task.Role, task.ID)
		}
		if _, err := t.spawn.Spawn(ctx, payload, label, "tool", "tool", pipelineID, task.ID); err != nil {
			lines = append(lines, fmt.Sprintf("- %s failed: %v", task.ID, err))
			continue
		}
		dispatched++
		lines = append(lines, fmt.Sprintf("- %s dispatched", task.ID))
	}

	if len(lines) == 0 {
		return "No tasks dispatched.", nil
	}
	return fmt.Sprintf("Pipeline %s dispatch result:\n%s", pipelineID, strings.Join(lines, "\n")), nil
}
