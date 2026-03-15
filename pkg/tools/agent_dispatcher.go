package tools

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type AgentDispatchRequest struct {
	Task                string
	RunKind             string
	AgentID             string
	Decision            *DispatchDecision
	Target              *TargetRef
	Origin              *OriginRef
	ExecutionPolicy     *ExecutionPolicy
	WorldDecision       *WorldDecisionContext
}

type AgentDispatchReply struct {
	TaskID   string
	AgentID  string
	Status   string
	Result   string
	Run      RunRecord
	Error    *RuntimeError
}

type AgentDispatcher struct {
	manager *AgentManager
}

func NewAgentDispatcher(manager *AgentManager) *AgentDispatcher {
	return &AgentDispatcher{manager: manager}
}

func (r *AgentDispatcher) DispatchTask(ctx context.Context, req AgentDispatchRequest) (*AgentTask, error) {
	if r == nil || r.manager == nil {
		return nil, fmt.Errorf("agent dispatcher is not configured")
	}
	if req.Decision != nil {
		if strings.TrimSpace(req.AgentID) == "" {
			req.AgentID = strings.TrimSpace(req.Decision.TargetAgent)
		}
		if strings.TrimSpace(req.Task) == "" {
			req.Task = strings.TrimSpace(req.Decision.TaskText)
		}
	}
	task, err := r.manager.SpawnTask(ctx, AgentSpawnOptions{
		Task:                req.Task,
		RunKind:             req.RunKind,
		AgentID:             req.AgentID,
		Target:              req.Target,
		Origin:              req.Origin,
		ExecutionPolicy:     req.ExecutionPolicy,
		WorldDecision:       req.WorldDecision,
	})
	if err != nil {
		return nil, err
	}
	return task, nil
}

func (r *AgentDispatcher) WaitReply(ctx context.Context, taskID string, interval time.Duration) (*AgentDispatchReply, error) {
	if r == nil || r.manager == nil {
		return nil, fmt.Errorf("agent dispatcher is not configured")
	}
	_ = interval
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("task id is required")
	}
	task, ok, err := r.manager.WaitTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if !ok || task == nil {
		return nil, fmt.Errorf("agent not found")
	}
	return &AgentDispatchReply{
		TaskID:   task.ID,
		AgentID:  task.AgentID,
		Status:   task.Status,
		Result:   strings.TrimSpace(task.Result),
		Run:      taskToRunRecord(task),
		Error:    taskRuntimeError(task),
	}, nil
}

func (r *AgentDispatcher) MergeResults(replies []*AgentDispatchReply) string {
	if len(replies) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, reply := range replies {
		if reply == nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("[%s] agent=%s status=%s\n", reply.TaskID, reply.AgentID, reply.Status))
		if txt := strings.TrimSpace(reply.Result); txt != "" {
			sb.WriteString(txt)
		} else {
			sb.WriteString("(empty result)")
		}
		sb.WriteString("\n\n")
	}
	return strings.TrimSpace(sb.String())
}
