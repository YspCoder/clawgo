package rpc

import (
	"context"

	"github.com/YspCoder/clawgo/pkg/tools"
)

type SubagentService interface {
	List(context.Context, ListSubagentsRequest) (*ListSubagentsResponse, *Error)
	Snapshot(context.Context, SnapshotRequest) (*SnapshotResponse, *Error)
	Get(context.Context, GetSubagentRequest) (*GetSubagentResponse, *Error)
	Spawn(context.Context, SpawnSubagentRequest) (*SpawnSubagentResponse, *Error)
	DispatchAndWait(context.Context, DispatchAndWaitRequest) (*DispatchAndWaitResponse, *Error)
	Registry(context.Context, RegistryRequest) (*RegistryResponse, *Error)
}

type ListSubagentsRequest struct{}

type ListSubagentsResponse struct {
	Items []*tools.SubagentTask `json:"items"`
}

type SnapshotRequest struct {
	Limit int `json:"limit,omitempty"`
}

type SnapshotResponse struct {
	Snapshot tools.RuntimeSnapshot `json:"snapshot"`
}

type GetSubagentRequest struct {
	ID string `json:"id"`
}

type GetSubagentResponse struct {
	Found bool                `json:"found"`
	Task  *tools.SubagentTask `json:"task,omitempty"`
}

type SpawnSubagentRequest struct {
	Task           string `json:"task"`
	Label          string `json:"label,omitempty"`
	Role           string `json:"role,omitempty"`
	AgentID        string `json:"agent_id,omitempty"`
	MaxRetries     int    `json:"max_retries,omitempty"`
	RetryBackoffMS int    `json:"retry_backoff_ms,omitempty"`
	TimeoutSec     int    `json:"timeout_sec,omitempty"`
	MaxTaskChars   int    `json:"max_task_chars,omitempty"`
	MaxResultChars int    `json:"max_result_chars,omitempty"`
	Channel        string `json:"channel,omitempty"`
	ChatID         string `json:"chat_id,omitempty"`
}

type SpawnSubagentResponse struct {
	Message string `json:"message"`
}

type DispatchAndWaitRequest struct {
	Task           string `json:"task"`
	Label          string `json:"label,omitempty"`
	Role           string `json:"role,omitempty"`
	AgentID        string `json:"agent_id,omitempty"`
	ThreadID       string `json:"thread_id,omitempty"`
	CorrelationID  string `json:"correlation_id,omitempty"`
	ParentRunID    string `json:"parent_run_id,omitempty"`
	Channel        string `json:"channel,omitempty"`
	ChatID         string `json:"chat_id,omitempty"`
	MaxRetries     int    `json:"max_retries,omitempty"`
	RetryBackoffMS int    `json:"retry_backoff_ms,omitempty"`
	TimeoutSec     int    `json:"timeout_sec,omitempty"`
	MaxTaskChars   int    `json:"max_task_chars,omitempty"`
	MaxResultChars int    `json:"max_result_chars,omitempty"`
	WaitTimeoutSec int    `json:"wait_timeout_sec,omitempty"`
}

type DispatchAndWaitResponse struct {
	Task   *tools.SubagentTask `json:"task,omitempty"`
	Reply  *tools.RouterReply  `json:"reply,omitempty"`
	Merged string              `json:"merged,omitempty"`
}

type RegistryRequest struct{}

type RegistryResponse struct {
	Items []map[string]interface{} `json:"items"`
}
