package tools

import (
	"bufio"
	"encoding/json"
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
	runs       map[string]*SubagentRun
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
		runs:       map[string]*SubagentRun{},
	}
	_ = os.MkdirAll(dir, 0755)
	_ = store.load()
	return store
}

func (s *SubagentRunStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.runs = map[string]*SubagentRun{}
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
		var record RunRecord
		if err := json.Unmarshal([]byte(line), &record); err == nil && strings.TrimSpace(record.ID) != "" {
			run := &SubagentRun{
				ID:            record.ID,
				Task:          record.Input,
				AgentID:       record.AgentID,
				ThreadID:      record.ThreadID,
				CorrelationID: record.CorrelationID,
				ParentRunID:   record.ParentRunID,
				Status:        record.Status,
				Result:        record.Output,
				Created:       record.CreatedAt,
				Updated:       record.UpdatedAt,
			}
			s.runs[run.ID] = run
			continue
		}
		var run SubagentRun
		if err := json.Unmarshal([]byte(line), &run); err != nil {
			continue
		}
		cp := cloneSubagentRun(&run)
		s.runs[run.ID] = cp
	}
	return scanner.Err()
}

func (s *SubagentRunStore) AppendRun(run *SubagentRun) error {
	if s == nil || run == nil {
		return nil
	}
	cp := cloneSubagentRun(run)
	data, err := json.Marshal(runToRunRecord(cp))
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
	record := EventRecord{
		ID:         EventRecordID(evt.RunID, evt.Type, evt.At),
		RunID:      evt.RunID,
		RequestID:  evt.RunID,
		AgentID:    evt.AgentID,
		Type:       evt.Type,
		Status:     evt.Status,
		Message:    evt.Message,
		RetryCount: evt.RetryCount,
		At:         evt.At,
	}
	data, err := json.Marshal(record)
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

func (s *SubagentRunStore) Get(runID string) (*SubagentRun, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[strings.TrimSpace(runID)]
	if !ok {
		return nil, false
	}
	return cloneSubagentRun(run), true
}

func (s *SubagentRunStore) List() []*SubagentRun {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*SubagentRun, 0, len(s.runs))
	for _, run := range s.runs {
		out = append(out, cloneSubagentRun(run))
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
			var record EventRecord
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				continue
			}
			evt = SubagentRunEvent{
				RunID:      record.RunID,
				AgentID:    record.AgentID,
				Type:       record.Type,
				Status:     record.Status,
				Message:    record.Message,
				RetryCount: record.RetryCount,
				At:         record.At,
			}
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

func cloneSubagentRun(run *SubagentRun) *SubagentRun {
	if run == nil {
		return nil
	}
	cp := *run
	if len(run.ToolAllowlist) > 0 {
		cp.ToolAllowlist = append([]string(nil), run.ToolAllowlist...)
	}
	if len(run.Steering) > 0 {
		cp.Steering = append([]string(nil), run.Steering...)
	}
	if run.SharedState != nil {
		cp.SharedState = make(map[string]interface{}, len(run.SharedState))
		for k, v := range run.SharedState {
			cp.SharedState[k] = v
		}
	}
	return &cp
}

func runToRequestRecord(run *SubagentRun) RequestRecord {
	if run == nil {
		return RequestRecord{}
	}
	return RequestRecord{
		ID:            run.ID,
		ThreadID:      run.ThreadID,
		CorrelationID: run.CorrelationID,
		OwnerAgentID:  run.AgentID,
		Status:        strings.TrimSpace(run.Status),
		Input:         run.Task,
		OriginChannel: run.OriginChannel,
		OriginChatID:  run.OriginChatID,
		CreatedAt:     run.Created,
		UpdatedAt:     run.Updated,
	}
}

func runRuntimeError(run *SubagentRun) *RuntimeError {
	if run == nil || !strings.EqualFold(strings.TrimSpace(run.Status), RuntimeStatusFailed) {
		return nil
	}
	msg := strings.TrimSpace(run.Result)
	msg = strings.TrimPrefix(msg, "Error:")
	msg = strings.TrimSpace(msg)
	return NewRuntimeError("subagent_failed", msg, "subagent", false, "subagent")
}

func runToRunRecord(run *SubagentRun) RunRecord {
	if run == nil {
		return RunRecord{}
	}
	return RunRecord{
		ID:            run.ID,
		RequestID:     run.ID,
		ThreadID:      run.ThreadID,
		CorrelationID: run.CorrelationID,
		AgentID:       run.AgentID,
		ParentRunID:   run.ParentRunID,
		Kind:          "subagent",
		Status:        strings.TrimSpace(run.Status),
		Input:         run.Task,
		Output:        strings.TrimSpace(run.Result),
		Error:         runRuntimeError(run),
		CreatedAt:     run.Created,
		UpdatedAt:     run.Updated,
	}
}

