package tools

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type RouterDispatchRequest struct {
	Task             string
	Label            string
	Role             string
	AgentID          string
	Decision         *DispatchDecision
	NotifyMainPolicy string
	ThreadID         string
	CorrelationID    string
	ParentRunID      string
	OriginChannel    string
	OriginChatID     string
	MaxRetries       int
	RetryBackoff     int
	TimeoutSec       int
	MaxTaskChars     int
	MaxResultChars   int
}

type RouterReply struct {
	TaskID        string
	ThreadID      string
	CorrelationID string
	AgentID       string
	Status        string
	Result        string
	Run           RunRecord
	Error         *RuntimeError
}

type SubagentRouter struct {
	manager *SubagentManager
}

func NewSubagentRouter(manager *SubagentManager) *SubagentRouter {
	return &SubagentRouter{manager: manager}
}

func (r *SubagentRouter) DispatchTask(ctx context.Context, req RouterDispatchRequest) (*SubagentTask, error) {
	if r == nil || r.manager == nil {
		return nil, fmt.Errorf("subagent router is not configured")
	}
	if req.Decision != nil {
		if strings.TrimSpace(req.AgentID) == "" {
			req.AgentID = strings.TrimSpace(req.Decision.TargetAgent)
		}
		if strings.TrimSpace(req.Task) == "" {
			req.Task = strings.TrimSpace(req.Decision.TaskText)
		}
	}
	task, err := r.manager.SpawnTask(ctx, SubagentSpawnOptions{
		Task:             req.Task,
		Label:            req.Label,
		Role:             req.Role,
		AgentID:          req.AgentID,
		NotifyMainPolicy: req.NotifyMainPolicy,
		ThreadID:         req.ThreadID,
		CorrelationID:    req.CorrelationID,
		ParentRunID:      req.ParentRunID,
		OriginChannel:    req.OriginChannel,
		OriginChatID:     req.OriginChatID,
		MaxRetries:       req.MaxRetries,
		RetryBackoff:     req.RetryBackoff,
		TimeoutSec:       req.TimeoutSec,
		MaxTaskChars:     req.MaxTaskChars,
		MaxResultChars:   req.MaxResultChars,
	})
	if err != nil {
		return nil, err
	}
	return task, nil
}

func (r *SubagentRouter) WaitReply(ctx context.Context, taskID string, interval time.Duration) (*RouterReply, error) {
	if r == nil || r.manager == nil {
		return nil, fmt.Errorf("subagent router is not configured")
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
		return nil, fmt.Errorf("subagent not found")
	}
	return &RouterReply{
		TaskID:        task.ID,
		ThreadID:      task.ThreadID,
		CorrelationID: task.CorrelationID,
		AgentID:       task.AgentID,
		Status:        task.Status,
		Result:        strings.TrimSpace(task.Result),
		Run:           taskToRunRecord(task),
		Error:         taskRuntimeError(task),
	}, nil
}

func (r *SubagentRouter) MergeResults(replies []*RouterReply) string {
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
