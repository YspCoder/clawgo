package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type PipelineStatus string

const (
	PipelinePending   PipelineStatus = "pending"
	PipelineRunning   PipelineStatus = "running"
	PipelineCompleted PipelineStatus = "completed"
	PipelineFailed    PipelineStatus = "failed"
)

type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
)

type PipelineTask struct {
	ID        string     `json:"id"`
	Role      string     `json:"role"`
	Goal      string     `json:"goal"`
	DependsOn []string   `json:"depends_on,omitempty"`
	Status    TaskStatus `json:"status"`
	Result    string     `json:"result,omitempty"`
	Error     string     `json:"error,omitempty"`
	CreatedAt int64      `json:"created_at"`
	UpdatedAt int64      `json:"updated_at"`
}

type Pipeline struct {
	ID            string                 `json:"id"`
	Label         string                 `json:"label"`
	Objective     string                 `json:"objective"`
	Status        PipelineStatus         `json:"status"`
	OriginChannel string                 `json:"origin_channel"`
	OriginChatID  string                 `json:"origin_chat_id"`
	SharedState   map[string]interface{} `json:"shared_state"`
	Tasks         map[string]*PipelineTask
	CreatedAt     int64 `json:"created_at"`
	UpdatedAt     int64 `json:"updated_at"`
}

type PipelineSpec struct {
	ID        string   `json:"id"`
	Role      string   `json:"role"`
	Goal      string   `json:"goal"`
	DependsOn []string `json:"depends_on,omitempty"`
}

type Orchestrator struct {
	mu        sync.RWMutex
	pipelines map[string]*Pipeline
	nextID    int
}

func NewOrchestrator() *Orchestrator {
	return &Orchestrator{
		pipelines: make(map[string]*Pipeline),
		nextID:    1,
	}
}

func (o *Orchestrator) CreatePipeline(label, objective, originChannel, originChatID string, tasks []PipelineSpec) (*Pipeline, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if strings.TrimSpace(objective) == "" {
		return nil, fmt.Errorf("objective is required")
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("at least one task is required")
	}

	id := fmt.Sprintf("pipe-%d", o.nextID)
	o.nextID++

	now := time.Now().UnixMilli()
	p := &Pipeline{
		ID:            id,
		Label:         strings.TrimSpace(label),
		Objective:     strings.TrimSpace(objective),
		Status:        PipelinePending,
		OriginChannel: originChannel,
		OriginChatID:  originChatID,
		SharedState:   make(map[string]interface{}),
		Tasks:         make(map[string]*PipelineTask, len(tasks)),
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	for _, task := range tasks {
		taskID := strings.TrimSpace(task.ID)
		if taskID == "" {
			return nil, fmt.Errorf("task id is required")
		}
		if _, exists := p.Tasks[taskID]; exists {
			return nil, fmt.Errorf("duplicate task id: %s", taskID)
		}

		p.Tasks[taskID] = &PipelineTask{
			ID:        taskID,
			Role:      strings.TrimSpace(task.Role),
			Goal:      strings.TrimSpace(task.Goal),
			DependsOn: normalizeDepends(task.DependsOn),
			Status:    TaskPending,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}

	for taskID, task := range p.Tasks {
		for _, dep := range task.DependsOn {
			if dep == taskID {
				return nil, fmt.Errorf("task %s cannot depend on itself", taskID)
			}
			if _, exists := p.Tasks[dep]; !exists {
				return nil, fmt.Errorf("task %s depends on missing task %s", taskID, dep)
			}
		}
	}

	o.pipelines[p.ID] = p
	return clonePipeline(p), nil
}

func (o *Orchestrator) MarkTaskRunning(pipelineID, taskID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	p, t, err := o.getTaskLocked(pipelineID, taskID)
	if err != nil {
		return err
	}
	if t.Status == TaskCompleted || t.Status == TaskFailed {
		return nil
	}

	t.Status = TaskRunning
	t.UpdatedAt = time.Now().UnixMilli()
	p.Status = PipelineRunning
	p.UpdatedAt = t.UpdatedAt
	return nil
}

func (o *Orchestrator) MarkTaskDone(pipelineID, taskID, result string, runErr error) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	p, t, err := o.getTaskLocked(pipelineID, taskID)
	if err != nil {
		return err
	}

	now := time.Now().UnixMilli()
	t.UpdatedAt = now
	t.Result = strings.TrimSpace(result)
	if runErr != nil {
		t.Status = TaskFailed
		t.Error = runErr.Error()
		p.Status = PipelineFailed
	} else {
		t.Status = TaskCompleted
		t.Error = ""
		p.Status = o.computePipelineStatusLocked(p)
	}
	p.UpdatedAt = now
	return nil
}

func (o *Orchestrator) SetSharedState(pipelineID, key string, value interface{}) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	p, ok := o.pipelines[pipelineID]
	if !ok {
		return fmt.Errorf("pipeline not found: %s", pipelineID)
	}
	k := strings.TrimSpace(key)
	if k == "" {
		return fmt.Errorf("state key is required")
	}
	p.SharedState[k] = value
	p.UpdatedAt = time.Now().UnixMilli()
	return nil
}

func (o *Orchestrator) GetPipeline(pipelineID string) (*Pipeline, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	p, ok := o.pipelines[pipelineID]
	if !ok {
		return nil, false
	}
	return clonePipeline(p), true
}

func (o *Orchestrator) ListPipelines() []*Pipeline {
	o.mu.RLock()
	defer o.mu.RUnlock()

	items := make([]*Pipeline, 0, len(o.pipelines))
	for _, p := range o.pipelines {
		items = append(items, clonePipeline(p))
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt > items[j].CreatedAt
	})
	return items
}

func (o *Orchestrator) ReadyTasks(pipelineID string) ([]*PipelineTask, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	p, ok := o.pipelines[pipelineID]
	if !ok {
		return nil, fmt.Errorf("pipeline not found: %s", pipelineID)
	}

	var ready []*PipelineTask
	for _, t := range p.Tasks {
		if t.Status != TaskPending {
			continue
		}
		if depsDone(p, t.DependsOn) {
			ready = append(ready, cloneTask(t))
		}
	}
	sort.Slice(ready, func(i, j int) bool { return ready[i].ID < ready[j].ID })
	return ready, nil
}

func (o *Orchestrator) SnapshotJSON(pipelineID string) (string, error) {
	p, ok := o.GetPipeline(pipelineID)
	if !ok {
		return "", fmt.Errorf("pipeline not found: %s", pipelineID)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (o *Orchestrator) getTaskLocked(pipelineID, taskID string) (*Pipeline, *PipelineTask, error) {
	p, ok := o.pipelines[pipelineID]
	if !ok {
		return nil, nil, fmt.Errorf("pipeline not found: %s", pipelineID)
	}
	t, ok := p.Tasks[taskID]
	if !ok {
		return nil, nil, fmt.Errorf("task %s not found in pipeline %s", taskID, pipelineID)
	}
	return p, t, nil
}

func (o *Orchestrator) computePipelineStatusLocked(p *Pipeline) PipelineStatus {
	allDone := true
	for _, t := range p.Tasks {
		if t.Status == TaskFailed {
			return PipelineFailed
		}
		if t.Status != TaskCompleted {
			allDone = false
		}
	}
	if allDone {
		return PipelineCompleted
	}
	return PipelineRunning
}

func depsDone(p *Pipeline, dependsOn []string) bool {
	for _, dep := range dependsOn {
		t, ok := p.Tasks[dep]
		if !ok || t.Status != TaskCompleted {
			return false
		}
	}
	return true
}

func normalizeDepends(in []string) []string {
	uniq := make(map[string]struct{})
	out := make([]string, 0, len(in))
	for _, item := range in {
		v := strings.TrimSpace(item)
		if v == "" {
			continue
		}
		if _, ok := uniq[v]; ok {
			continue
		}
		uniq[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func cloneTask(in *PipelineTask) *PipelineTask {
	if in == nil {
		return nil
	}
	deps := make([]string, len(in.DependsOn))
	copy(deps, in.DependsOn)
	out := *in
	out.DependsOn = deps
	return &out
}

func clonePipeline(in *Pipeline) *Pipeline {
	if in == nil {
		return nil
	}
	out := *in
	out.SharedState = make(map[string]interface{}, len(in.SharedState))
	for k, v := range in.SharedState {
		out.SharedState[k] = v
	}
	out.Tasks = make(map[string]*PipelineTask, len(in.Tasks))
	for id, t := range in.Tasks {
		out.Tasks[id] = cloneTask(t)
	}
	return &out
}
