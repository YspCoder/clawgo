package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YspCoder/clawgo/pkg/jsonlog"
)

type SubagentRunEvent struct {
	RunID       string `json:"run_id"`
	AgentID     string `json:"agent_id,omitempty"`
	Type        string `json:"type"`
	Status      string `json:"status,omitempty"`
	FailureCode string `json:"failure_code,omitempty"`
	Message     string `json:"message,omitempty"`
	RetryCount  int    `json:"retry_count,omitempty"`
	At          int64  `json:"ts"`
}

type subagentRunsMetaFile struct {
	Version    int           `json:"version"`
	LastOffset int64         `json:"last_offset,omitempty"`
	NextIDSeed int           `json:"next_id_seed,omitempty"`
	UpdatedAt  int64         `json:"updated_at,omitempty"`
	Runs       []SubagentRun `json:"runs,omitempty"`
}

type subagentEventsMetaFile struct {
	Version     int                           `json:"version"`
	LastOffset  int64                         `json:"last_offset,omitempty"`
	UpdatedAt   int64                         `json:"updated_at,omitempty"`
	EventsByRun map[string][]SubagentRunEvent `json:"events_by_run,omitempty"`
}

type SubagentRunStore struct {
	dir            string
	runsPath       string
	eventsPath     string
	runsMetaPath   string
	eventsMetaPath string
	mu             sync.RWMutex
	runs           map[string]*SubagentRun
	events         map[string][]SubagentRunEvent
	nextIDSeed     int
}

func NewSubagentRunStore(workspace string) *SubagentRunStore {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil
	}
	dir := filepath.Join(workspace, "agents", "runtime")
	store := &SubagentRunStore{
		dir:            dir,
		runsPath:       filepath.Join(dir, "subagent_runs.jsonl"),
		eventsPath:     filepath.Join(dir, "subagent_events.jsonl"),
		runsMetaPath:   filepath.Join(dir, "subagent_runs.meta.json"),
		eventsMetaPath: filepath.Join(dir, "subagent_events.meta.json"),
		runs:           map[string]*SubagentRun{},
		events:         map[string][]SubagentRunEvent{},
		nextIDSeed:     1,
	}
	_ = os.MkdirAll(dir, 0o755)
	_ = store.load()
	return store
}

func (s *SubagentRunStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.runs = map[string]*SubagentRun{}
	s.events = map[string][]SubagentRunEvent{}
	s.nextIDSeed = 1

	if err := s.loadRunsLocked(); err != nil {
		return err
	}
	return s.loadEventsLocked()
}

func (s *SubagentRunStore) loadRunsLocked() error {
	size, err := jsonlog.FileSize(s.runsPath)
	if err != nil {
		return err
	}
	var meta subagentRunsMetaFile
	if err := jsonlog.ReadJSON(s.runsMetaPath, &meta); err == nil && meta.Version > 0 && meta.LastOffset == size {
		for _, run := range meta.Runs {
			cp := cloneSubagentRun(&run)
			s.runs[cp.ID] = cp
		}
		s.nextIDSeed = meta.NextIDSeed
		if s.nextIDSeed <= 0 {
			s.nextIDSeed = deriveNextRunSeed(s.runs)
		}
		return nil
	}

	if size == 0 {
		return s.persistRunsMetaLocked(size)
	}
	if err := jsonlog.Scan(s.runsPath, func(line []byte) error {
		run, ok := decodeSubagentRunLine(line)
		if !ok {
			return nil
		}
		s.runs[run.ID] = run
		return nil
	}); err != nil {
		return err
	}
	s.nextIDSeed = deriveNextRunSeed(s.runs)
	return s.persistRunsMetaLocked(size)
}

func (s *SubagentRunStore) loadEventsLocked() error {
	size, err := jsonlog.FileSize(s.eventsPath)
	if err != nil {
		return err
	}
	var meta subagentEventsMetaFile
	if err := jsonlog.ReadJSON(s.eventsMetaPath, &meta); err == nil && meta.Version > 0 && meta.LastOffset == size {
		s.events = cloneEventsByRun(meta.EventsByRun)
		return nil
	}
	if size == 0 {
		return s.persistEventsMetaLocked(size)
	}
	eventsByRun := map[string][]SubagentRunEvent{}
	if err := jsonlog.Scan(s.eventsPath, func(line []byte) error {
		evt, ok := decodeSubagentEventLine(line)
		if !ok {
			return nil
		}
		eventsByRun[evt.RunID] = append(eventsByRun[evt.RunID], evt)
		return nil
	}); err != nil {
		return err
	}
	for runID, events := range eventsByRun {
		sort.Slice(events, func(i, j int) bool { return events[i].At < events[j].At })
		eventsByRun[runID] = events
	}
	s.events = eventsByRun
	return s.persistEventsMetaLocked(size)
}

func (s *SubagentRunStore) AppendRun(run *SubagentRun) error {
	if s == nil || run == nil {
		return nil
	}
	cp := cloneSubagentRun(run)
	record := runToRunRecord(cp)

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	offset, err := jsonlog.AppendLine(s.runsPath, record)
	if err != nil {
		return err
	}
	s.runs[cp.ID] = cp
	if next := parseSubagentSequence(cp.ID) + 1; next > s.nextIDSeed {
		s.nextIDSeed = next
	}
	return s.persistRunsMetaLocked(offset)
}

func (s *SubagentRunStore) AppendEvent(evt SubagentRunEvent) error {
	if s == nil {
		return nil
	}
	record := EventRecord{
		ID:          EventRecordID(evt.RunID, evt.Type, evt.At),
		RunID:       evt.RunID,
		RequestID:   evt.RunID,
		AgentID:     evt.AgentID,
		Type:        evt.Type,
		Status:      evt.Status,
		FailureCode: evt.FailureCode,
		Message:     evt.Message,
		RetryCount:  evt.RetryCount,
		At:          evt.At,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	offset, err := jsonlog.AppendLine(s.eventsPath, record)
	if err != nil {
		return err
	}
	s.events[evt.RunID] = append(s.events[evt.RunID], evt)
	sort.Slice(s.events[evt.RunID], func(i, j int) bool { return s.events[evt.RunID][i].At < s.events[evt.RunID][j].At })
	return s.persistEventsMetaLocked(offset)
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := append([]SubagentRunEvent(nil), s.events[strings.TrimSpace(runID)]...)
	sort.Slice(items, func(i, j int) bool { return items[i].At < items[j].At })
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items, nil
}

func (s *SubagentRunStore) NextIDSeed() int {
	if s == nil {
		return 1
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.nextIDSeed <= 0 {
		return 1
	}
	return s.nextIDSeed
}

func (s *SubagentRunStore) persistRunsMetaLocked(offset int64) error {
	meta := subagentRunsMetaFile{
		Version:    1,
		LastOffset: offset,
		NextIDSeed: maxRunSeed(deriveNextRunSeed(s.runs), s.nextIDSeed, 1),
		UpdatedAt:  time.Now().UnixMilli(),
		Runs:       make([]SubagentRun, 0, len(s.runs)),
	}
	for _, run := range s.runs {
		meta.Runs = append(meta.Runs, *cloneSubagentRun(run))
	}
	sort.Slice(meta.Runs, func(i, j int) bool {
		if meta.Runs[i].Created != meta.Runs[j].Created {
			return meta.Runs[i].Created > meta.Runs[j].Created
		}
		return meta.Runs[i].ID > meta.Runs[j].ID
	})
	s.nextIDSeed = meta.NextIDSeed
	return jsonlog.WriteJSON(s.runsMetaPath, meta)
}

func (s *SubagentRunStore) persistEventsMetaLocked(offset int64) error {
	meta := subagentEventsMetaFile{
		Version:     1,
		LastOffset:  offset,
		UpdatedAt:   time.Now().UnixMilli(),
		EventsByRun: cloneEventsByRun(s.events),
	}
	return jsonlog.WriteJSON(s.eventsMetaPath, meta)
}

func deriveNextRunSeed(runs map[string]*SubagentRun) int {
	maxSeq := 0
	for runID := range runs {
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

func decodeSubagentRunLine(line []byte) (*SubagentRun, bool) {
	var record RunRecord
	if err := json.Unmarshal(line, &record); err == nil && strings.TrimSpace(record.ID) != "" {
		return &SubagentRun{
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
		}, true
	}
	var run SubagentRun
	if err := json.Unmarshal(line, &run); err != nil || strings.TrimSpace(run.ID) == "" {
		return nil, false
	}
	return cloneSubagentRun(&run), true
}

func decodeSubagentEventLine(line []byte) (SubagentRunEvent, bool) {
	var evt SubagentRunEvent
	if err := json.Unmarshal(line, &evt); err == nil && strings.TrimSpace(evt.RunID) != "" {
		return evt, true
	}
	var record EventRecord
	if err := json.Unmarshal(line, &record); err != nil || strings.TrimSpace(record.RunID) == "" {
		return SubagentRunEvent{}, false
	}
	return SubagentRunEvent{
		RunID:       record.RunID,
		AgentID:     record.AgentID,
		Type:        record.Type,
		Status:      record.Status,
		FailureCode: record.FailureCode,
		Message:     record.Message,
		RetryCount:  record.RetryCount,
		At:          record.At,
	}, true
}

func cloneEventsByRun(src map[string][]SubagentRunEvent) map[string][]SubagentRunEvent {
	if len(src) == 0 {
		return map[string][]SubagentRunEvent{}
	}
	out := make(map[string][]SubagentRunEvent, len(src))
	for key, items := range src {
		out[key] = append([]SubagentRunEvent(nil), items...)
	}
	return out
}

func maxRunSeed(values ...int) int {
	best := 0
	for _, value := range values {
		if value > best {
			best = value
		}
	}
	return best
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
