package autonomy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type TaskItem struct {
	ID          string `json:"id"`
	Content     string `json:"content"`
	Priority    string `json:"priority"`
	DueAt       string `json:"due_at,omitempty"`
	Status      string `json:"status"` // todo|doing|blocked|done
	RetryAfter  string `json:"retry_after,omitempty"`
	Source      string `json:"source"`
	DedupeHits  int    `json:"dedupe_hits,omitempty"`
	UpdatedAt   string `json:"updated_at"`
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
	case "todo", "doing", "blocked", "done":
		return s
	default:
		return "todo"
	}
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
