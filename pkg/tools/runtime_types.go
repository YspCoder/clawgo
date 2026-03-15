package tools

import (
	"fmt"
	"strings"
)

const (
	RuntimeStatusPending   = "pending"
	RuntimeStatusRouting   = "routing"
	RuntimeStatusRunning   = "running"
	RuntimeStatusWaiting   = "waiting"
	RuntimeStatusCompleted = "completed"
	RuntimeStatusFailed    = "failed"
	RuntimeStatusCancelled = "cancelled"
	RuntimeStatusRecovered = "recovered"
)

type RuntimeError struct {
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	Stage     string `json:"stage,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
	Source    string `json:"source,omitempty"`
}

type DispatchDecision struct {
	TargetAgent string  `json:"target_agent,omitempty"`
	Reason      string  `json:"reason,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
	TaskText    string  `json:"task_text,omitempty"`
	RouteSource string  `json:"route_source,omitempty"`
}

func (d DispatchDecision) Valid() bool {
	return strings.TrimSpace(d.TargetAgent) != "" && strings.TrimSpace(d.TaskText) != ""
}

type TaskRecord struct {
	ID           string `json:"id"`
	OwnerAgentID string `json:"owner_agent_id,omitempty"`
	Status       string `json:"status"`
	Input        string `json:"input,omitempty"`
	Origin       string `json:"origin,omitempty"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

type RunRecord struct {
	ID        string        `json:"id"`
	TaskID    string        `json:"task_id,omitempty"`
	AgentID   string        `json:"agent_id,omitempty"`
	Kind      string        `json:"kind,omitempty"`
	Status    string        `json:"status"`
	Input     string        `json:"input,omitempty"`
	Output    string        `json:"output,omitempty"`
	Error     *RuntimeError `json:"error,omitempty"`
	CreatedAt int64         `json:"created_at"`
	UpdatedAt int64         `json:"updated_at"`
}

type EventRecord struct {
	ID         string        `json:"id,omitempty"`
	RunID      string        `json:"run_id,omitempty"`
	TaskID     string        `json:"task_id,omitempty"`
	AgentID    string        `json:"agent_id,omitempty"`
	Type       string        `json:"type"`
	Status     string        `json:"status,omitempty"`
	Message    string        `json:"message,omitempty"`
	RetryCount int           `json:"retry_count,omitempty"`
	Error      *RuntimeError `json:"error,omitempty"`
	At         int64         `json:"ts"`
}

type RuntimeSnapshot struct {
	Tasks  []TaskRecord  `json:"tasks,omitempty"`
	Runs   []RunRecord   `json:"runs,omitempty"`
	Events []EventRecord `json:"events,omitempty"`
	World  interface{}   `json:"world,omitempty"`
}

type ExecutionRun struct {
	Run      RunRecord        `json:"run"`
	Task     TaskRecord       `json:"task"`
	Decision DispatchDecision `json:"decision,omitempty"`
}

func IsTerminalRuntimeStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case RuntimeStatusCompleted, RuntimeStatusFailed, RuntimeStatusCancelled:
		return true
	default:
		return false
	}
}

func NewRuntimeError(code, message, stage string, retryable bool, source string) *RuntimeError {
	return &RuntimeError{
		Code:      strings.TrimSpace(code),
		Message:   strings.TrimSpace(message),
		Stage:     strings.TrimSpace(stage),
		Retryable: retryable,
		Source:    strings.TrimSpace(source),
	}
}

func EventRecordID(runID, eventType string, at int64) string {
	runID = strings.TrimSpace(runID)
	eventType = strings.TrimSpace(eventType)
	if runID == "" && eventType == "" && at <= 0 {
		return ""
	}
	return fmt.Sprintf("%s:%s:%d", runID, eventType, at)
}
