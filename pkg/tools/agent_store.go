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

type AgentRunEvent struct {
	RunID      string `json:"run_id"`
	AgentID    string `json:"agent_id,omitempty"`
	Type       string `json:"type"`
	Status     string `json:"status,omitempty"`
	Message    string `json:"message,omitempty"`
	RetryCount int    `json:"retry_count,omitempty"`
	At         int64  `json:"ts"`
}

type AgentRunStore struct {
	dir        string
	runsPath   string
	eventsPath string
	mu         sync.RWMutex
	runs       map[string]*AgentTask
}

func NewAgentRunStore(workspace string) *AgentRunStore {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil
	}
	dir := filepath.Join(workspace, "agents", "runtime")
	store := &AgentRunStore{
		dir:        dir,
		runsPath:   filepath.Join(dir, "agent_runs.jsonl"),
		eventsPath: filepath.Join(dir, "agent_events.jsonl"),
		runs:       map[string]*AgentTask{},
	}
	_ = os.MkdirAll(dir, 0755)
	_ = store.load()
	return store
}

func (s *AgentRunStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.runs = map[string]*AgentTask{}
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
			task := &AgentTask{
				ID:            record.ID,
				Task:          record.Input,
				AgentID:       record.AgentID,
				Status:        record.Status,
				Result:        record.Output,
				Created:       record.CreatedAt,
				Updated:       record.UpdatedAt,
			}
			s.runs[task.ID] = task
			continue
		}
		var task AgentTask
		if err := json.Unmarshal([]byte(line), &task); err != nil {
			continue
		}
		cp := cloneAgentTask(&task)
		s.runs[task.ID] = cp
	}
	return scanner.Err()
}

func (s *AgentRunStore) AppendRun(task *AgentTask) error {
	if s == nil || task == nil {
		return nil
	}
	cp := cloneAgentTask(task)
	data, err := json.Marshal(taskToRunRecord(cp))
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

func (s *AgentRunStore) AppendEvent(evt AgentRunEvent) error {
	if s == nil {
		return nil
	}
	record := EventRecord{
		ID:         EventRecordID(evt.RunID, evt.Type, evt.At),
		RunID:      evt.RunID,
		TaskID:     evt.RunID,
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

func (s *AgentRunStore) Get(runID string) (*AgentTask, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.runs[strings.TrimSpace(runID)]
	if !ok {
		return nil, false
	}
	return cloneAgentTask(task), true
}

func (s *AgentRunStore) List() []*AgentTask {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*AgentTask, 0, len(s.runs))
	for _, task := range s.runs {
		out = append(out, cloneAgentTask(task))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Created != out[j].Created {
			return out[i].Created > out[j].Created
		}
		return out[i].ID > out[j].ID
	})
	return out
}

func (s *AgentRunStore) Events(runID string, limit int) ([]AgentRunEvent, error) {
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
	events := make([]AgentRunEvent, 0)
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var evt AgentRunEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			var record EventRecord
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				continue
			}
			evt = AgentRunEvent{
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

func (s *AgentRunStore) NextIDSeed() int {
	if s == nil {
		return 1
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	maxSeq := 0
	for runID := range s.runs {
		if n := parseAgentSequence(runID); n > maxSeq {
			maxSeq = n
		}
	}
	if maxSeq <= 0 {
		return 1
	}
	return maxSeq + 1
}

func parseAgentSequence(runID string) int {
	runID = strings.TrimSpace(runID)
	if !strings.HasPrefix(runID, "agent-") {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimPrefix(runID, "agent-"))
	return n
}

func cloneAgentTask(task *AgentTask) *AgentTask {
	if task == nil {
		return nil
	}
	cp := *task
	cp.Target = cloneTargetRef(task.Target)
	cp.ExecutionPolicy = cloneExecutionPolicy(task.ExecutionPolicy)
	cp.WorldDecision = cloneWorldDecisionContext(task.WorldDecision)
	cp.Origin = cloneOriginRef(task.Origin)
	return &cp
}

func taskToTaskRecord(task *AgentTask) TaskRecord {
	if task == nil {
		return TaskRecord{}
	}
	return TaskRecord{
		ID:           task.ID,
		OwnerAgentID: task.AgentID,
		Status:       strings.TrimSpace(task.Status),
		Input:        task.Task,
		Origin:       formatTaskOrigin(task.Origin),
		CreatedAt:    task.Created,
		UpdatedAt:    task.Updated,
	}
}

func taskRuntimeError(task *AgentTask) *RuntimeError {
	if task == nil || !strings.EqualFold(strings.TrimSpace(task.Status), RuntimeStatusFailed) {
		return nil
	}
	msg := strings.TrimSpace(task.Result)
	msg = strings.TrimPrefix(msg, "Error:")
	msg = strings.TrimSpace(msg)
	return NewRuntimeError("agent_failed", msg, "agent", false, "agent")
}

func taskToRunRecord(task *AgentTask) RunRecord {
	if task == nil {
		return RunRecord{}
	}
	return RunRecord{
		ID:        task.ID,
		TaskID:    task.ID,
		AgentID:   task.AgentID,
		Kind:      agentRunKind(task),
		Status:    strings.TrimSpace(task.Status),
		Input:     task.Task,
		Output:    strings.TrimSpace(task.Result),
		Error:     taskRuntimeError(task),
		CreatedAt: task.Created,
		UpdatedAt: task.Updated,
	}
}

func agentRunKind(task *AgentTask) string {
	if task == nil {
		return "agent"
	}
	if IsWorldDecisionTask(task) {
		return "world_npc"
	}
	return normalizeAgentRunKind(task.RunKind, task.WorldDecision)
}

func formatTaskOrigin(origin *OriginRef) string {
	channel, chatID := OriginValues(origin)
	return channel + ":" + chatID
}

func cloneMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneMapSlice(in []map[string]interface{}) []map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(in))
	for _, item := range in {
		out = append(out, cloneMap(item))
	}
	return out
}

func cloneWorldDecisionContext(in *WorldDecisionContext) *WorldDecisionContext {
	if in == nil {
		return nil
	}
	return &WorldDecisionContext{
		WorldTick:           in.WorldTick,
		WorldSnapshot:       cloneMap(in.WorldSnapshot),
		NPCSnapshot:         cloneMap(in.NPCSnapshot),
		VisibleEvents:       cloneMapSlice(in.VisibleEvents),
		IntentSchemaVersion: strings.TrimSpace(in.IntentSchemaVersion),
	}
}

func IsWorldDecisionTask(task *AgentTask) bool {
	if task == nil {
		return false
	}
	if task.WorldDecision != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(task.RunKind), "world_npc")
}

func formatAgentEventLog(evt AgentRunEvent) string {
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
