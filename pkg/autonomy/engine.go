package autonomy

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clawgo/pkg/bus"
	"clawgo/pkg/lifecycle"
)

type Options struct {
	Enabled                 bool
	TickIntervalSec         int
	MinRunIntervalSec       int
	MaxPendingDurationSec   int
	MaxConsecutiveStalls    int
	MaxDispatchPerTick      int
	Workspace               string
	DefaultNotifyChannel    string
	DefaultNotifyChatID     string
	NotifyCooldownSec       int
}

type taskState struct {
	ID               string
	Content          string
	Status           string // idle|running|waiting|blocked|completed
	LastRunAt        time.Time
	LastAutonomyAt   time.Time
	ConsecutiveStall int
}

type Engine struct {
	opts      Options
	bus       *bus.MessageBus
	runner    *lifecycle.LoopRunner
	taskStore *TaskStore

	mu         sync.Mutex
	state      map[string]*taskState
	lastNotify map[string]time.Time
}

func NewEngine(opts Options, msgBus *bus.MessageBus) *Engine {
	if opts.TickIntervalSec <= 0 {
		opts.TickIntervalSec = 30
	}
	if opts.MinRunIntervalSec <= 0 {
		opts.MinRunIntervalSec = 20
	}
	if opts.MaxPendingDurationSec <= 0 {
		opts.MaxPendingDurationSec = 180
	}
	if opts.MaxConsecutiveStalls <= 0 {
		opts.MaxConsecutiveStalls = 3
	}
	if opts.MaxDispatchPerTick <= 0 {
		opts.MaxDispatchPerTick = 2
	}
	if opts.NotifyCooldownSec <= 0 {
		opts.NotifyCooldownSec = 300
	}
	return &Engine{
		opts:       opts,
		bus:        msgBus,
		runner:     lifecycle.NewLoopRunner(),
		taskStore:  NewTaskStore(opts.Workspace),
		state:      map[string]*taskState{},
		lastNotify: map[string]time.Time{},
	}
}

func (e *Engine) Start() {
	if !e.opts.Enabled {
		return
	}
	e.runner.Start(e.runLoop)
}

func (e *Engine) Stop() {
	e.runner.Stop()
}

func (e *Engine) runLoop(stopCh <-chan struct{}) {
	ticker := time.NewTicker(time.Duration(e.opts.TickIntervalSec) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			e.tick()
		}
	}
}

func (e *Engine) tick() {
	todos := e.scanTodos()
	now := time.Now()
	stored, _ := e.taskStore.Load()

	e.mu.Lock()
	defer e.mu.Unlock()

	storedMap := map[string]TaskItem{}
	for _, it := range stored {
		storedMap[it.ID] = it
	}

	known := map[string]struct{}{}
	for _, t := range todos {
		known[t.ID] = struct{}{}
		st, ok := e.state[t.ID]
		if !ok {
			status := "idle"
			if old, ok := storedMap[t.ID]; ok {
				if old.Status == "blocked" {
					status = "blocked"
				}
			}
			e.state[t.ID] = &taskState{ID: t.ID, Content: t.Content, Status: status}
			continue
		}
		st.Content = t.Content
		if st.Status == "completed" {
			st.Status = "idle"
		}
	}

	// completed when removed from todo source
	for id, st := range e.state {
		if _, ok := known[id]; !ok {
			if st.Status != "completed" {
				st.Status = "completed"
				e.sendCompletionNotification(st)
			}
		}
	}

	dispatched := 0
	for _, st := range e.state {
		if dispatched >= e.opts.MaxDispatchPerTick {
			break
		}
		if st.Status == "completed" || st.Status == "blocked" {
			continue
		}
		if !st.LastRunAt.IsZero() && now.Sub(st.LastRunAt) < time.Duration(e.opts.MinRunIntervalSec)*time.Second {
			continue
		}
		if st.Status == "running" && now.Sub(st.LastRunAt) > time.Duration(e.opts.MaxPendingDurationSec)*time.Second {
			st.ConsecutiveStall++
			if st.ConsecutiveStall > e.opts.MaxConsecutiveStalls {
				st.Status = "blocked"
				e.sendFailureNotification(st, "max consecutive stalls reached")
				continue
			}
		}

		e.dispatchTask(st)
		st.Status = "running"
		st.LastRunAt = now
		st.LastAutonomyAt = now
		dispatched++
	}
	e.persistStateLocked()
}

type todoItem struct {
	ID      string
	Content string
}

func (e *Engine) scanTodos() []todoItem {
	var out []todoItem
	if strings.TrimSpace(e.opts.Workspace) == "" {
		return out
	}
	paths := []string{
		filepath.Join(e.opts.Workspace, "MEMORY.md"),
		filepath.Join(e.opts.Workspace, "memory", time.Now().Format("2006-01-02")+".md"),
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			t := strings.TrimSpace(line)
			if strings.HasPrefix(t, "- [ ]") {
				content := strings.TrimSpace(strings.TrimPrefix(t, "- [ ]"))
				if content == "" {
					continue
				}
				out = append(out, todoItem{ID: hashID(content), Content: content})
				continue
			}
			if strings.HasPrefix(strings.ToLower(t), "todo:") {
				content := strings.TrimSpace(t[5:])
				if content == "" {
					continue
				}
				out = append(out, todoItem{ID: hashID(content), Content: content})
			}
		}
	}
	return out
}

func (e *Engine) dispatchTask(st *taskState) {
	content := fmt.Sprintf("Autonomy task (Plan -> Act -> Reflect):\n- Goal: %s\n- Requirements: concise progress update\n- If blocked, explain blocker and next retry hint", st.Content)
	e.bus.PublishInbound(bus.InboundMessage{
		Channel:    "system",
		SenderID:   "autonomy",
		ChatID:     "internal:autonomy",
		Content:    content,
		SessionKey: "autonomy:" + st.ID,
		Metadata: map[string]string{
			"trigger": "autonomy",
			"task_id": st.ID,
			"source":  "memory_todo",
		},
	})
}

func (e *Engine) sendCompletionNotification(st *taskState) {
	if !e.shouldNotify("done:" + st.ID) {
		return
	}
	e.bus.PublishOutbound(bus.OutboundMessage{
		Channel: e.opts.DefaultNotifyChannel,
		ChatID:  e.opts.DefaultNotifyChatID,
		Content: fmt.Sprintf("[Autonomy] Task completed: %s", st.Content),
	})
}

func (e *Engine) sendFailureNotification(st *taskState, reason string) {
	if !e.shouldNotify("blocked:" + st.ID) {
		return
	}
	e.bus.PublishOutbound(bus.OutboundMessage{
		Channel: e.opts.DefaultNotifyChannel,
		ChatID:  e.opts.DefaultNotifyChatID,
		Content: fmt.Sprintf("[Autonomy] Task blocked: %s (%s)", st.Content, reason),
	})
}

func (e *Engine) shouldNotify(key string) bool {
	if strings.TrimSpace(e.opts.DefaultNotifyChannel) == "" || strings.TrimSpace(e.opts.DefaultNotifyChatID) == "" {
		return false
	}
	now := time.Now()
	if last, ok := e.lastNotify[key]; ok {
		if now.Sub(last) < time.Duration(e.opts.NotifyCooldownSec)*time.Second {
			return false
		}
	}
	e.lastNotify[key] = now
	return true
}

func (e *Engine) persistStateLocked() {
	items := make([]TaskItem, 0, len(e.state))
	for _, st := range e.state {
		status := "todo"
		switch st.Status {
		case "running":
			status = "doing"
		case "blocked":
			status = "blocked"
		case "completed":
			status = "done"
		default:
			status = "todo"
		}
		items = append(items, TaskItem{
			ID:        st.ID,
			Content:   st.Content,
			Priority:  "normal",
			Status:    status,
			Source:    "memory_todo",
			UpdatedAt: nowRFC3339(),
		})
	}
	_ = e.taskStore.Save(items)
}

func hashID(s string) string {
	sum := sha1.Sum([]byte(strings.ToLower(strings.TrimSpace(s))))
	return hex.EncodeToString(sum[:])[:12]
}

func RunOnce(ctx context.Context, engine *Engine) {
	if engine == nil {
		return
	}
	select {
	case <-ctx.Done():
		return
	default:
		engine.tick()
	}
}
