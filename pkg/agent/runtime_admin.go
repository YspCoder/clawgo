package agent

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"clawgo/pkg/tools"
)

func (al *AgentLoop) HandleSubagentRuntime(ctx context.Context, action string, args map[string]interface{}) (interface{}, error) {
	if al == nil || al.subagentManager == nil {
		return nil, fmt.Errorf("subagent runtime is not configured")
	}
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		action = "list"
	}

	sm := al.subagentManager
	switch action {
	case "list":
		tasks := sm.ListTasks()
		items := make([]*tools.SubagentTask, 0, len(tasks))
		for _, task := range tasks {
			items = append(items, cloneSubagentTask(task))
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Created > items[j].Created })
		return map[string]interface{}{"items": items}, nil
	case "get", "info":
		taskID, err := resolveSubagentTaskIDForRuntime(sm, runtimeStringArg(args, "id"))
		if err != nil {
			return nil, err
		}
		task, ok := sm.GetTask(taskID)
		if !ok {
			return map[string]interface{}{"found": false}, nil
		}
		return map[string]interface{}{"found": true, "task": cloneSubagentTask(task)}, nil
	case "spawn", "create":
		taskInput := runtimeStringArg(args, "task")
		if taskInput == "" {
			return nil, fmt.Errorf("task is required")
		}
		msg, err := sm.Spawn(ctx, tools.SubagentSpawnOptions{
			Task:           taskInput,
			Label:          runtimeStringArg(args, "label"),
			Role:           runtimeStringArg(args, "role"),
			AgentID:        runtimeStringArg(args, "agent_id"),
			MaxRetries:     runtimeIntArg(args, "max_retries", 0),
			RetryBackoff:   runtimeIntArg(args, "retry_backoff_ms", 0),
			TimeoutSec:     runtimeIntArg(args, "timeout_sec", 0),
			MaxTaskChars:   runtimeIntArg(args, "max_task_chars", 0),
			MaxResultChars: runtimeIntArg(args, "max_result_chars", 0),
			OriginChannel:  fallbackString(runtimeStringArg(args, "channel"), "webui"),
			OriginChatID:   fallbackString(runtimeStringArg(args, "chat_id"), "webui"),
			PipelineID:     runtimeStringArg(args, "pipeline_id"),
			PipelineTask:   runtimeStringArg(args, "task_id"),
		})
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"message": msg}, nil
	case "kill":
		taskID, err := resolveSubagentTaskIDForRuntime(sm, runtimeStringArg(args, "id"))
		if err != nil {
			return nil, err
		}
		ok := sm.KillTask(taskID)
		return map[string]interface{}{"ok": ok}, nil
	case "resume":
		taskID, err := resolveSubagentTaskIDForRuntime(sm, runtimeStringArg(args, "id"))
		if err != nil {
			return nil, err
		}
		label, ok := sm.ResumeTask(ctx, taskID)
		return map[string]interface{}{"ok": ok, "label": label}, nil
	case "steer", "send":
		taskID, err := resolveSubagentTaskIDForRuntime(sm, runtimeStringArg(args, "id"))
		if err != nil {
			return nil, err
		}
		msg := runtimeStringArg(args, "message")
		if msg == "" {
			return nil, fmt.Errorf("message is required")
		}
		ok := sm.SteerTask(taskID, msg)
		return map[string]interface{}{"ok": ok}, nil
	default:
		return nil, fmt.Errorf("unsupported action: %s", action)
	}
}

func (al *AgentLoop) HandlePipelineRuntime(ctx context.Context, action string, args map[string]interface{}) (interface{}, error) {
	if al == nil || al.orchestrator == nil {
		return nil, fmt.Errorf("pipeline runtime is not configured")
	}
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		action = "list"
	}

	switch action {
	case "list":
		return map[string]interface{}{"items": al.orchestrator.ListPipelines()}, nil
	case "get", "status":
		pipelineID := fallbackString(runtimeStringArg(args, "pipeline_id"), runtimeStringArg(args, "id"))
		if strings.TrimSpace(pipelineID) == "" {
			return nil, fmt.Errorf("pipeline_id is required")
		}
		p, ok := al.orchestrator.GetPipeline(strings.TrimSpace(pipelineID))
		if !ok {
			return map[string]interface{}{"found": false}, nil
		}
		return map[string]interface{}{"found": true, "pipeline": p}, nil
	case "ready":
		pipelineID := fallbackString(runtimeStringArg(args, "pipeline_id"), runtimeStringArg(args, "id"))
		if strings.TrimSpace(pipelineID) == "" {
			return nil, fmt.Errorf("pipeline_id is required")
		}
		items, err := al.orchestrator.ReadyTasks(strings.TrimSpace(pipelineID))
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"items": items}, nil
	case "create":
		objective := runtimeStringArg(args, "objective")
		if objective == "" {
			return nil, fmt.Errorf("objective is required")
		}
		specs, err := parsePipelineSpecsForRuntime(args["tasks"])
		if err != nil {
			return nil, err
		}
		label := runtimeStringArg(args, "label")
		p, err := al.orchestrator.CreatePipeline(label, objective, "webui", "webui", specs)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"pipeline": p}, nil
	case "state_set":
		pipelineID := fallbackString(runtimeStringArg(args, "pipeline_id"), runtimeStringArg(args, "id"))
		key := runtimeStringArg(args, "key")
		if strings.TrimSpace(pipelineID) == "" || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("pipeline_id and key are required")
		}
		value, ok := args["value"]
		if !ok {
			return nil, fmt.Errorf("value is required")
		}
		if err := al.orchestrator.SetSharedState(strings.TrimSpace(pipelineID), strings.TrimSpace(key), value); err != nil {
			return nil, err
		}
		p, _ := al.orchestrator.GetPipeline(strings.TrimSpace(pipelineID))
		return map[string]interface{}{"ok": true, "pipeline": p}, nil
	case "dispatch":
		pipelineID := fallbackString(runtimeStringArg(args, "pipeline_id"), runtimeStringArg(args, "id"))
		if strings.TrimSpace(pipelineID) == "" {
			return nil, fmt.Errorf("pipeline_id is required")
		}
		maxDispatch := runtimeIntArg(args, "max_dispatch", 3)
		dispatchTool := tools.NewPipelineDispatchTool(al.orchestrator, al.subagentManager)
		result, err := dispatchTool.Execute(ctx, map[string]interface{}{
			"pipeline_id":  strings.TrimSpace(pipelineID),
			"max_dispatch": float64(maxDispatch),
		})
		if err != nil {
			return nil, err
		}
		p, _ := al.orchestrator.GetPipeline(strings.TrimSpace(pipelineID))
		return map[string]interface{}{"message": result, "pipeline": p}, nil
	default:
		return nil, fmt.Errorf("unsupported action: %s", action)
	}
}

func cloneSubagentTask(in *tools.SubagentTask) *tools.SubagentTask {
	if in == nil {
		return nil
	}
	out := *in
	if len(in.ToolAllowlist) > 0 {
		out.ToolAllowlist = append([]string(nil), in.ToolAllowlist...)
	}
	if len(in.Steering) > 0 {
		out.Steering = append([]string(nil), in.Steering...)
	}
	if in.SharedState != nil {
		out.SharedState = make(map[string]interface{}, len(in.SharedState))
		for k, v := range in.SharedState {
			out.SharedState[k] = v
		}
	}
	return &out
}

func resolveSubagentTaskIDForRuntime(sm *tools.SubagentManager, raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if id == "" {
		return "", fmt.Errorf("id is required")
	}
	if !strings.HasPrefix(id, "#") {
		return id, nil
	}
	idx, err := strconv.Atoi(strings.TrimPrefix(id, "#"))
	if err != nil || idx <= 0 {
		return "", fmt.Errorf("invalid subagent index")
	}
	tasks := sm.ListTasks()
	if len(tasks) == 0 {
		return "", fmt.Errorf("no subagents")
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Created > tasks[j].Created })
	if idx > len(tasks) {
		return "", fmt.Errorf("subagent index out of range")
	}
	return tasks[idx-1].ID, nil
}

func parsePipelineSpecsForRuntime(raw interface{}) ([]tools.PipelineSpec, error) {
	items, ok := raw.([]interface{})
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("tasks is required")
	}
	specs := make([]tools.PipelineSpec, 0, len(items))
	for i, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("tasks[%d] must be object", i)
		}
		id := runtimeStringArg(m, "id")
		if id == "" {
			return nil, fmt.Errorf("tasks[%d].id is required", i)
		}
		goal := runtimeStringArg(m, "goal")
		if goal == "" {
			return nil, fmt.Errorf("tasks[%d].goal is required", i)
		}
		spec := tools.PipelineSpec{
			ID:   id,
			Role: runtimeStringArg(m, "role"),
			Goal: goal,
		}
		if deps, ok := m["depends_on"].([]interface{}); ok {
			spec.DependsOn = make([]string, 0, len(deps))
			for _, dep := range deps {
				d, _ := dep.(string)
				d = strings.TrimSpace(d)
				if d == "" {
					continue
				}
				spec.DependsOn = append(spec.DependsOn, d)
			}
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func runtimeStringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func runtimeIntArg(args map[string]interface{}, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	switch v := args[key].(type) {
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	}
	return fallback
}

func fallbackString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}
