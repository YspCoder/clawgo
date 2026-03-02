package autonomy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type TaskAttempt struct {
	Time    string `json:"time"`
	Status  string `json:"status"`
	Session string `json:"session,omitempty"`
	Note    string `json:"note,omitempty"`
}

type TaskItem struct {
	ID            string        `json:"id"`
	ParentTaskID  string        `json:"parent_task_id,omitempty"`
	Content       string        `json:"content"`
	Priority      string        `json:"priority"`
	DueAt         string        `json:"due_at,omitempty"`
	Status        string        `json:"status"` // todo|doing|waiting|blocked|done|paused|canceled
	BlockReason   string        `json:"block_reason,omitempty"`
	RetryAfter    string        `json:"retry_after,omitempty"`
	Source        string        `json:"source"`
	DedupeHits    int           `json:"dedupe_hits,omitempty"`
	ResourceKeys  []string      `json:"resource_keys,omitempty"`
	LastPauseReason string      `json:"last_pause_reason,omitempty"`
	LastPauseAt     string      `json:"last_pause_at,omitempty"`
	MemoryRefs    []string      `json:"memory_refs,omitempty"`
	AuditRefs     []string      `json:"audit_refs,omitempty"`
	Attempts      []TaskAttempt `json:"attempts,omitempty"`
	UpdatedAt     string        `json:"updated_at"`
}

type TaskStore struct {
	workspace string
}

func NewTaskStore(workspace string) *TaskStore {
	return &TaskStore{workspace: workspace}
}

func (s *TaskStore) path() string {
	return filepath.Join(s.workspace, "memory", "tasks.json")
}

func (s *TaskStore) Load() ([]TaskItem, error) {
	data, err := os.ReadFile(s.path())
	if err != nil {
		if os.IsNotExist(err) {
			return []TaskItem{}, nil
		}
		return nil, err
	}
	var items []TaskItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *TaskStore) Save(items []TaskItem) error {
	_ = os.MkdirAll(filepath.Dir(s.path()), 0755)
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt > items[j].UpdatedAt })
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(), data, 0644)
}

func normalizeStatus(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	switch s {
	case "todo", "doing", "waiting", "blocked", "done", "paused", "canceled":
		return s
	default:
		return "todo"
	}
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
