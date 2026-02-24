package autonomy

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	QuietHours              string
	UserIdleResumeSec       int
}

type taskState struct {
	ID               string
	Content          string
	Priority         string
	DueAt            string
	Status           string // idle|running|waiting|blocked|completed
	BlockReason      string
	LastRunAt        time.Time
	LastAutonomyAt   time.Time
	RetryAfter       time.Time
	ConsecutiveStall int
	DedupeHits       int
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
	if opts.UserIdleResumeSec <= 0 {
		opts.UserIdleResumeSec = 20
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
	if e.hasManualPause() {
		for _, st := range e.state {
			if st.Status == "running" {
				st.Status = "waiting"
				st.BlockReason = "manual_pause"
				e.writeReflectLog("waiting", st, "paused by manual switch")
			}
		}
		e.persistStateLocked()
		e.mu.Unlock()
		return
	}
	if e.hasRecentUserActivity(now) {
		for _, st := range e.state {
			if st.Status == "running" {
				st.Status = "waiting"
				e.writeReflectLog("waiting", st, "paused due to active user conversation")
			}
		}
		e.persistStateLocked()
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()

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
			retryAfter := time.Time{}
			if old, ok := storedMap[t.ID]; ok {
				if old.Status == "blocked" {
					status = "blocked"
				}
				if strings.TrimSpace(old.RetryAfter) != "" {
					if rt, err := time.Parse(time.RFC3339, old.RetryAfter); err == nil {
						retryAfter = rt
					}
				}
			}
			e.state[t.ID] = &taskState{ID: t.ID, Content: t.Content, Priority: t.Priority, DueAt: t.DueAt, Status: status, RetryAfter: retryAfter, DedupeHits: t.DedupeHits}
			continue
		}
		st.Content = t.Content
		st.Priority = t.Priority
		st.DueAt = t.DueAt
		st.DedupeHits = t.DedupeHits
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

	ordered := make([]*taskState, 0, len(e.state))
	for _, st := range e.state {
		ordered = append(ordered, st)
	}
	sort.Slice(ordered, func(i, j int) bool {
		pi := priorityWeight(ordered[i].Priority)
		pj := priorityWeight(ordered[j].Priority)
		if pi != pj {
			return pi > pj
		}
		di := dueWeight(ordered[i].DueAt)
		dj := dueWeight(ordered[j].DueAt)
		if di != dj {
			return di > dj
		}
		return ordered[i].ID < ordered[j].ID
	})

	dispatched := 0
	for _, st := range ordered {
		if dispatched >= e.opts.MaxDispatchPerTick {
			break
		}
		if st.Status == "completed" {
			continue
		}
		if st.Status == "waiting" {
			st.Status = "idle"
			st.BlockReason = ""
			e.writeReflectLog("resume", st, "user conversation idle, autonomy resumed")
		}
		if st.Status == "blocked" {
			if !st.RetryAfter.IsZero() && now.Before(st.RetryAfter) {
				continue
			}
			if now.Sub(st.LastRunAt) >= blockedRetryBackoff(st.ConsecutiveStall, e.opts.MinRunIntervalSec) {
				st.Status = "idle"
				st.BlockReason = ""
			} else {
				continue
			}
		}
		if !st.LastRunAt.IsZero() && now.Sub(st.LastRunAt) < time.Duration(e.opts.MinRunIntervalSec)*time.Second {
			continue
		}
		if st.Status == "running" && now.Sub(st.LastRunAt) > time.Duration(e.opts.MaxPendingDurationSec)*time.Second {
			st.ConsecutiveStall++
			if st.ConsecutiveStall > e.opts.MaxConsecutiveStalls {
				st.Status = "blocked"
				st.BlockReason = "max_consecutive_stalls"
				st.RetryAfter = now.Add(blockedRetryBackoff(st.ConsecutiveStall, e.opts.MinRunIntervalSec))
				e.sendFailureNotification(st, "max consecutive stalls reached")
				continue
			}
		}

		e.dispatchTask(st)
		st.Status = "running"
		st.BlockReason = ""
		st.LastRunAt = now
		st.LastAutonomyAt = now
		e.writeReflectLog("dispatch", st, "task dispatched to agent loop")
		e.writeTriggerAudit("dispatch", st, "")
		dispatched++
	}
	e.persistStateLocked()
}

type todoItem struct {
	ID        string
	Content   string
	Priority  string
	DueAt     string
	DedupeHits int
}

func (e *Engine) scanTodos() []todoItem {
	if strings.TrimSpace(e.opts.Workspace) == "" {
		return nil
	}
	merged := map[string]todoItem{}
	merge := func(it todoItem) {
		if strings.TrimSpace(it.ID) == "" || strings.TrimSpace(it.Content) == "" {
			return
		}
		if cur, ok := merged[it.ID]; ok {
			if priorityWeight(it.Priority) > priorityWeight(cur.Priority) {
				cur.Priority = it.Priority
			}
			if cur.DueAt == "" && it.DueAt != "" {
				cur.DueAt = it.DueAt
			}
			cur.DedupeHits++
			merged[it.ID] = cur
			return
		}
		merged[it.ID] = it
	}

	// 1) Parse markdown todos from MEMORY + today's daily memory.
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
				priority, dueAt, normalized := parseTodoAttributes(content)
				merge(todoItem{ID: hashID(normalized), Content: normalized, Priority: priority, DueAt: dueAt})
				continue
			}
			if strings.HasPrefix(strings.ToLower(t), "todo:") {
				content := strings.TrimSpace(t[5:])
				priority, dueAt, normalized := parseTodoAttributes(content)
				merge(todoItem{ID: hashID(normalized), Content: normalized, Priority: priority, DueAt: dueAt})
			}
		}
	}

	// 2) Merge structured tasks.json items (manual injections / prior state).
	if items, err := e.taskStore.Load(); err == nil {
		for _, it := range items {
			status := strings.ToLower(strings.TrimSpace(it.Status))
			if status == "done" {
				continue
			}
			content := strings.TrimSpace(it.Content)
			if content == "" {
				continue
			}
			id := strings.TrimSpace(it.ID)
			if id == "" {
				id = hashID(content)
			}
			priority := strings.TrimSpace(it.Priority)
			if priority == "" {
				priority = "normal"
			}
			merge(todoItem{ID: id, Content: content, Priority: priority, DueAt: strings.TrimSpace(it.DueAt)})
		}
	}

	out := make([]todoItem, 0, len(merged))
	for _, it := range merged {
		out = append(out, it)
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
	e.writeReflectLog("complete", st, "task marked completed")
	e.writeTriggerAudit("complete", st, "")
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
	e.writeReflectLog("blocked", st, reason)
	e.writeTriggerAudit("blocked", st, reason)
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
	if inQuietHours(now, e.opts.QuietHours) {
		return false
	}
	if last, ok := e.lastNotify[key]; ok {
		if now.Sub(last) < time.Duration(e.opts.NotifyCooldownSec)*time.Second {
			return false
		}
	}
	e.lastNotify[key] = now
	return true
}

func (e *Engine) writeTriggerAudit(action string, st *taskState, errText string) {
	if strings.TrimSpace(e.opts.Workspace) == "" || st == nil {
		return
	}
	path := filepath.Join(e.opts.Workspace, "memory", "trigger-audit.jsonl")
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	row := map[string]interface{}{
		"time":    time.Now().UTC().Format(time.RFC3339),
		"trigger": "autonomy",
		"action":  action,
		"session": "autonomy:" + st.ID,
	}
	if strings.TrimSpace(errText) != "" {
		row["error"] = errText
	}
	if b, err := json.Marshal(row); err == nil {
		f, oErr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if oErr == nil {
			_, _ = f.Write(append(b, '\n'))
			_ = f.Close()
		}
	}
}

func (e *Engine) writeReflectLog(stage string, st *taskState, outcome string) {
	if strings.TrimSpace(e.opts.Workspace) == "" || st == nil {
		return
	}
	memDir := filepath.Join(e.opts.Workspace, "memory")
	_ = os.MkdirAll(memDir, 0755)
	path := filepath.Join(memDir, time.Now().Format("2006-01-02")+".md")
	line := fmt.Sprintf("- [%s] [autonomy][%s] task=%s status=%s outcome=%s\n", time.Now().Format("15:04"), stage, st.Content, st.Status, outcome)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

func inQuietHours(now time.Time, spec string) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return false
	}
	parts := strings.Split(spec, "-")
	if len(parts) != 2 {
		return false
	}
	parseHM := func(v string) (int, bool) {
		hm := strings.Split(strings.TrimSpace(v), ":")
		if len(hm) != 2 {
			return 0, false
		}
		h, err1 := strconv.Atoi(hm[0])
		m, err2 := strconv.Atoi(hm[1])
		if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
			return 0, false
		}
		return h*60 + m, true
	}
	start, ok1 := parseHM(parts[0])
	end, ok2 := parseHM(parts[1])
	if !ok1 || !ok2 {
		return false
	}
	nowMin := now.Hour()*60 + now.Minute()
	if start <= end {
		return nowMin >= start && nowMin <= end
	}
	return nowMin >= start || nowMin <= end
}

func (e *Engine) persistStateLocked() {
	items := make([]TaskItem, 0, len(e.state))
	for _, st := range e.state {
		status := "todo"
		switch st.Status {
		case "running":
			status = "doing"
		case "waiting":
			status = "waiting"
		case "blocked":
			status = "blocked"
		case "completed":
			status = "done"
		default:
			status = "todo"
		}
		retryAfter := ""
		if !st.RetryAfter.IsZero() {
			retryAfter = st.RetryAfter.UTC().Format(time.RFC3339)
		}
		items = append(items, TaskItem{
			ID:          st.ID,
			Content:     st.Content,
			Priority:    st.Priority,
			DueAt:       st.DueAt,
			Status:      status,
			BlockReason: st.BlockReason,
			RetryAfter:  retryAfter,
			Source:      "memory_todo",
			DedupeHits:  st.DedupeHits,
			UpdatedAt:   nowRFC3339(),
		})
	}
	_ = e.taskStore.Save(items)
}

func parseTodoAttributes(content string) (priority, dueAt, normalized string) {
	priority = "normal"
	normalized = strings.TrimSpace(content)
	l := strings.ToLower(normalized)
	if strings.HasPrefix(l, "[high]") || strings.HasPrefix(l, "p1:") {
		priority = "high"
		normalized = strings.TrimSpace(normalized[6:])
	} else if strings.HasPrefix(l, "[low]") || strings.HasPrefix(l, "p3:") {
		priority = "low"
		normalized = strings.TrimSpace(normalized[5:])
	} else if strings.HasPrefix(l, "[medium]") || strings.HasPrefix(l, "p2:") {
		priority = "normal"
		if strings.HasPrefix(l, "[medium]") {
			normalized = strings.TrimSpace(normalized[8:])
		} else {
			normalized = strings.TrimSpace(normalized[3:])
		}
	}
	if idx := strings.Index(strings.ToLower(normalized), " due:"); idx > 0 {
		dueAt = strings.TrimSpace(normalized[idx+5:])
		normalized = strings.TrimSpace(normalized[:idx])
	}
	if normalized == "" {
		normalized = strings.TrimSpace(content)
	}
	return priority, dueAt, normalized
}

func priorityWeight(p string) int {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "high":
		return 3
	case "normal", "medium":
		return 2
	case "low":
		return 1
	default:
		return 2
	}
}

func dueWeight(dueAt string) int64 {
	dueAt = strings.TrimSpace(dueAt)
	if dueAt == "" {
		return 0
	}
	layouts := []string{"2006-01-02", time.RFC3339, time.RFC3339Nano}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, dueAt); err == nil {
			return -t.Unix() // earlier due => bigger score after descending sort
		}
	}
	return 0
}

func (e *Engine) pauseFilePath() string {
	if strings.TrimSpace(e.opts.Workspace) == "" {
		return ""
	}
	return filepath.Join(e.opts.Workspace, "memory", "autonomy.pause")
}

func (e *Engine) hasManualPause() bool {
	p := e.pauseFilePath()
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func (e *Engine) hasRecentUserActivity(now time.Time) bool {
	if e.opts.UserIdleResumeSec <= 0 || strings.TrimSpace(e.opts.Workspace) == "" {
		return false
	}
	sessionsPath := filepath.Join(filepath.Dir(e.opts.Workspace), "sessions", "sessions.json")
	data, err := os.ReadFile(sessionsPath)
	if err != nil {
		return false
	}
	var index map[string]struct {
		Kind        string `json:"kind"`
		SessionFile string `json:"sessionFile"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return false
	}
	cutoff := now.Add(-time.Duration(e.opts.UserIdleResumeSec) * time.Second)
	for _, row := range index {
		if strings.ToLower(strings.TrimSpace(row.Kind)) != "main" {
			continue
		}
		if strings.TrimSpace(row.SessionFile) == "" {
			continue
		}
		if ts := latestUserMessageTime(row.SessionFile); !ts.IsZero() && ts.After(cutoff) {
			return true
		}
	}
	return false
}

func latestUserMessageTime(path string) time.Time {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}
	}
	defer f.Close()

	var latest time.Time
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Bytes()

		// OpenClaw-like event line
		var ev struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Message   *struct {
				Role string `json:"role"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &ev); err == nil && ev.Message != nil {
			if strings.ToLower(strings.TrimSpace(ev.Message.Role)) == "user" {
				if t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(ev.Timestamp)); err == nil && t.After(latest) {
					latest = t
				} else if t, err := time.Parse(time.RFC3339, strings.TrimSpace(ev.Timestamp)); err == nil && t.After(latest) {
					latest = t
				}
			}
			continue
		}

		// Legacy line
		var msg struct {
			Role string `json:"role"`
		}
		if err := json.Unmarshal(line, &msg); err == nil {
			if strings.ToLower(strings.TrimSpace(msg.Role)) == "user" {
				latest = time.Now().UTC()
			}
		}
	}
	return latest
}

func blockedRetryBackoff(stalls int, minRunIntervalSec int) time.Duration {
	if minRunIntervalSec <= 0 {
		minRunIntervalSec = 20
	}
	if stalls < 1 {
		stalls = 1
	}
	base := time.Duration(minRunIntervalSec) * time.Second
	factor := 1 << min(stalls, 5)
	return time.Duration(factor) * base
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
