package tools

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type SubagentRunEvent struct {
	RunID      string `json:"run_id"`
	AgentID    string `json:"agent_id,omitempty"`
	Type       string `json:"type"`
	Status     string `json:"status,omitempty"`
	Message    string `json:"message,omitempty"`
	RetryCount int    `json:"retry_count,omitempty"`
	At         int64  `json:"ts"`
}

type SubagentRunStore struct {
	dir        string
	runsPath   string
	eventsPath string
	mu         sync.RWMutex
	runs       map[string]*SubagentTask
}

func NewSubagentRunStore(workspace string) *SubagentRunStore {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil
	}
	dir := filepath.Join(workspace, "agents", "runtime")
	store := &SubagentRunStore{
		dir:        dir,
		runsPath:   filepath.Join(dir, "subagent_runs.jsonl"),
		eventsPath: filepath.Join(dir, "subagent_events.jsonl"),
		runs:       map[string]*SubagentTask{},
	}
	_ = os.MkdirAll(dir, 0755)
	_ = store.load()
	return store
}

func (s *SubagentRunStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.runs = map[string]*SubagentTask{}
	f, err := os.Open(s.runsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var task SubagentTask
		if err := json.Unmarshal([]byte(line), &task); err != nil {
			continue
		}
		cp := cloneSubagentTask(&task)
		s.runs[task.ID] = cp
	}
	return scanner.Err()
}

func (s *SubagentRunStore) AppendRun(task *SubagentTask) error {
	if s == nil || task == nil {
		return nil
	}
	cp := cloneSubagentTask(task)
	data, err := json.Marshal(cp)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.runsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	s.runs[cp.ID] = cp
	return nil
}

func (s *SubagentRunStore) AppendEvent(evt SubagentRunEvent) error {
	if s == nil {
		return nil
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.eventsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

func (s *SubagentRunStore) Get(runID string) (*SubagentTask, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.runs[strings.TrimSpace(runID)]
	if !ok {
		return nil, false
	}
	return cloneSubagentTask(task), true
}

func (s *SubagentRunStore) List() []*SubagentTask {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*SubagentTask, 0, len(s.runs))
	for _, task := range s.runs {
		out = append(out, cloneSubagentTask(task))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Created != out[j].Created {
			return out[i].Created > out[j].Created
		}
		return out[i].ID > out[j].ID
	})
	return out
}

func (s *SubagentRunStore) Events(runID string, limit int) ([]SubagentRunEvent, error) {
	if s == nil {
		return nil, nil
	}
	f, err := os.Open(s.eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	runID = strings.TrimSpace(runID)
	events := make([]SubagentRunEvent, 0)
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var evt SubagentRunEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		if evt.RunID != runID {
			continue
		}
		events = append(events, evt)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Slice(events, func(i, j int) bool { return events[i].At < events[j].At })
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	return events, nil
}

func (s *SubagentRunStore) NextIDSeed() int {
	if s == nil {
		return 1
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	maxSeq := 0
	for runID := range s.runs {
		if n := parseSubagentSequence(runID); n > maxSeq {
			maxSeq = n
		}
	}
	if maxSeq <= 0 {
		return 1
	}
	return maxSeq + 1
}

func parseSubagentSequence(runID string) int {
	runID = strings.TrimSpace(runID)
	if !strings.HasPrefix(runID, "subagent-") {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimPrefix(runID, "subagent-"))
	return n
}

func cloneSubagentTask(task *SubagentTask) *SubagentTask {
	if task == nil {
		return nil
	}
	cp := *task
	if len(task.ToolAllowlist) > 0 {
		cp.ToolAllowlist = append([]string(nil), task.ToolAllowlist...)
	}
	if len(task.Steering) > 0 {
		cp.Steering = append([]string(nil), task.Steering...)
	}
	if task.SharedState != nil {
		cp.SharedState = make(map[string]interface{}, len(task.SharedState))
		for k, v := range task.SharedState {
			cp.SharedState[k] = v
		}
	}
	return &cp
}

func formatSubagentEventLog(evt SubagentRunEvent) string {
	base := fmt.Sprintf("- %d %s", evt.At, evt.Type)
	if strings.TrimSpace(evt.Status) != "" {
		base += fmt.Sprintf(" status=%s", evt.Status)
	}
	if evt.RetryCount > 0 {
		base += fmt.Sprintf(" retry=%d", evt.RetryCount)
	}
	if strings.TrimSpace(evt.Message) != "" {
		base += fmt.Sprintf(" msg=%s", strings.TrimSpace(evt.Message))
	}
	return base
}
