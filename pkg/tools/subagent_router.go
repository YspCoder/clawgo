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
	RunID         string
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

func (r *SubagentRouter) DispatchRun(ctx context.Context, req RouterDispatchRequest) (*SubagentRun, error) {
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
	run, err := r.manager.SpawnRun(ctx, SubagentSpawnOptions{
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
	return run, nil
}

func (r *SubagentRouter) WaitRun(ctx context.Context, runID string, interval time.Duration) (*RouterReply, error) {
	if r == nil || r.manager == nil {
		return nil, fmt.Errorf("subagent router is not configured")
	}
	_ = interval
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("run id is required")
	}
	run, ok, err := r.manager.waitRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if !ok || run == nil {
		return nil, fmt.Errorf("subagent not found")
	}
	return &RouterReply{
		RunID:         run.ID,
		ThreadID:      run.ThreadID,
		CorrelationID: run.CorrelationID,
		AgentID:       run.AgentID,
		Status:        run.Status,
		Result:        strings.TrimSpace(run.Result),
		Run:           runToRunRecord(run),
		Error:         runRuntimeError(run),
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
		sb.WriteString(fmt.Sprintf("[%s] agent=%s status=%s\n", reply.RunID, reply.AgentID, reply.Status))
		if txt := strings.TrimSpace(reply.Result); txt != "" {
			sb.WriteString(txt)
		} else {
			sb.WriteString("(empty result)")
		}
		sb.WriteString("\n\n")
	}
	return strings.TrimSpace(sb.String())
}
