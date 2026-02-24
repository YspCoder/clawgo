package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"clawgo/pkg/events"
)

type processSession struct {
	ID        string
	Command   string
	StartedAt time.Time
	EndedAt   time.Time
	ExitCode  *int
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	done      chan struct{}
	mu        sync.RWMutex
	log       bytes.Buffer
	logPath   string
	logQueue  chan []byte
}

type ProcessManager struct {
	mu       sync.RWMutex
	sessions map[string]*processSession
	seq      uint64
	metaPath string
	persistQ chan struct{}
	events   *events.TypedBus[ProcessEvent]
}

// ProcessEvent is a typed lifecycle event for process sessions.
type ProcessEvent struct {
	Type      string    `json:"type"`
	SessionID string    `json:"session_id"`
	Command   string    `json:"command,omitempty"`
	At        time.Time `json:"at"`
	ExitCode  *int      `json:"exit_code,omitempty"`
}

func NewProcessManager(workspace string) *ProcessManager {
	m := &ProcessManager{
		sessions: map[string]*processSession{},
		persistQ: make(chan struct{}, 1),
		events:   events.NewTypedBus[ProcessEvent](),
	}
	if workspace != "" {
		memDir := filepath.Join(workspace, "memory")
		_ = os.MkdirAll(memDir, 0755)
		m.metaPath = filepath.Join(memDir, "process-sessions.json")
		m.load()
	}
	go m.persistLoop()
	return m
}

func (m *ProcessManager) Start(parent context.Context, command, cwd string) (string, error) {
	id := "p-" + strconv.FormatUint(atomic.AddUint64(&m.seq, 1), 10)
	if parent == nil {
		parent = context.Background()
	}
	procCtx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(procCtx, "sh", "-c", command)
	if cwd != "" {
		cmd.Dir = cwd
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	s := &processSession{ID: id, Command: command, StartedAt: time.Now().UTC(), cmd: cmd, cancel: cancel, done: make(chan struct{}), logQueue: make(chan []byte, 128)}
	if m.metaPath != "" {
		s.logPath = filepath.Join(filepath.Dir(m.metaPath), "process-"+id+".log")
	}

	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	go m.logWriter(s)
	m.persist()
	m.events.Publish(ProcessEvent{Type: "start", SessionID: id, Command: command, At: time.Now().UTC()})

	if err := cmd.Start(); err != nil {
		cancel()
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		return "", err
	}

	go m.capture(s, stdout)
	go m.capture(s, stderr)
	go func() {
		err := cmd.Wait()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				code = -1
			}
		}
		s.mu.Lock()
		s.EndedAt = time.Now().UTC()
		s.ExitCode = &code
		s.mu.Unlock()
		if s.logQueue != nil {
			close(s.logQueue)
		}
		m.persist()
		m.events.Publish(ProcessEvent{Type: "exit", SessionID: s.ID, Command: s.Command, At: s.EndedAt, ExitCode: &code})
		close(s.done)
	}()

	return id, nil
}

func (m *ProcessManager) capture(s *processSession, r interface{ Read([]byte) (int, error) }) {
	buf := make([]byte, 2048)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			s.mu.Lock()
			_, _ = s.log.Write(chunk)
			s.mu.Unlock()
			if s.logQueue != nil {
				select {
				case s.logQueue <- chunk:
				default:
					// backpressure: drop to keep process capture non-blocking
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func (m *ProcessManager) List() []map[string]interface{} {
	m.mu.RLock()
	items := make([]*processSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		items = append(items, s)
	}
	m.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].StartedAt.After(items[j].StartedAt) })
	out := make([]map[string]interface{}, 0, len(items))
	for _, s := range items {
		s.mu.RLock()
		running := s.ExitCode == nil
		code := interface{}(nil)
		if s.ExitCode != nil {
			code = *s.ExitCode
		}
		out = append(out, map[string]interface{}{"id": s.ID, "command": s.Command, "running": running, "exit_code": code, "started_at": s.StartedAt.Format(time.RFC3339)})
		s.mu.RUnlock()
	}
	return out
}

func (m *ProcessManager) Get(id string) (*processSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *ProcessManager) Log(id string, offset, limit int) (string, error) {
	s, ok := m.Get(id)
	if !ok {
		return "", fmt.Errorf("session not found: %s", id)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	b := s.log.Bytes()
	if len(b) == 0 && s.logPath != "" {
		if data, err := os.ReadFile(s.logPath); err == nil {
			b = data
		}
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(b) {
		offset = len(b)
	}
	end := len(b)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return string(b[offset:end]), nil
}

func (m *ProcessManager) Kill(id string) error {
	s, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}
	s.mu.RLock()
	cmd := s.cmd
	running := s.ExitCode == nil
	s.mu.RUnlock()
	if !running {
		return nil
	}
	if cmd.Process == nil {
		return fmt.Errorf("process not started")
	}
	if s.cancel != nil {
		s.cancel()
	}
	err := cmd.Process.Kill()
	m.persist()
	m.events.Publish(ProcessEvent{Type: "kill", SessionID: s.ID, Command: s.Command, At: time.Now().UTC()})
	return err
}

func (m *ProcessManager) SubscribeEvents(buffer int) <-chan ProcessEvent {
	return m.events.Subscribe(buffer)
}

func (m *ProcessManager) logWriter(s *processSession) {
	if s == nil || s.logPath == "" || s.logQueue == nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.logPath), 0755)
	f, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	buf := bytes.Buffer{}
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		_, _ = f.Write(buf.Bytes())
		_ = f.Sync()
		buf.Reset()
	}

	for {
		select {
		case chunk, ok := <-s.logQueue:
			if !ok {
				flush()
				return
			}
			_, _ = buf.Write(chunk)
			if buf.Len() >= 4096 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (m *ProcessManager) persistLoop() {
	for range m.persistQ {
		m.persistNow()
	}
}

type processSessionMeta struct {
	ID        string `json:"id"`
	Command   string `json:"command"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	Recovered bool   `json:"recovered"`
	LogPath   string `json:"log_path,omitempty"`
}

func (m *ProcessManager) persist() {
	if m.metaPath == "" {
		return
	}
	select {
	case m.persistQ <- struct{}{}:
	default:
	}
}

func (m *ProcessManager) persistNow() {
	if m.metaPath == "" {
		return
	}
	m.mu.RLock()
	items := make([]processSessionMeta, 0, len(m.sessions))
	for _, s := range m.sessions {
		s.mu.RLock()
		row := processSessionMeta{
			ID:        s.ID,
			Command:   s.Command,
			StartedAt: s.StartedAt.Format(time.RFC3339),
			Recovered: s.cmd == nil,
			LogPath:   s.logPath,
		}
		if !s.EndedAt.IsZero() {
			row.EndedAt = s.EndedAt.Format(time.RFC3339)
		}
		if s.ExitCode != nil {
			code := *s.ExitCode
			row.ExitCode = &code
		}
		s.mu.RUnlock()
		items = append(items, row)
	}
	m.mu.RUnlock()
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(m.metaPath, data, 0644)
}

func (m *ProcessManager) load() {
	if m.metaPath == "" {
		return
	}
	data, err := os.ReadFile(m.metaPath)
	if err != nil {
		return
	}
	var items []processSessionMeta
	if err := json.Unmarshal(data, &items); err != nil {
		return
	}
	maxSeq := uint64(0)
	for _, it := range items {
		s := &processSession{ID: it.ID, Command: it.Command, done: make(chan struct{}), logPath: it.LogPath}
		if s.logPath == "" && m.metaPath != "" {
			s.logPath = filepath.Join(filepath.Dir(m.metaPath), "process-"+s.ID+".log")
		}
		if t, err := time.Parse(time.RFC3339, it.StartedAt); err == nil {
			s.StartedAt = t
		}
		if it.EndedAt != "" {
			if t, err := time.Parse(time.RFC3339, it.EndedAt); err == nil {
				s.EndedAt = t
			}
		}
		if it.ExitCode != nil {
			code := *it.ExitCode
			s.ExitCode = &code
			close(s.done)
		} else {
			code := -2
			s.ExitCode = &code
			s.EndedAt = time.Now().UTC()
			close(s.done)
		}
		m.sessions[s.ID] = s
		if strings.HasPrefix(s.ID, "p-") {
			if n, err := strconv.ParseUint(strings.TrimPrefix(s.ID, "p-"), 10, 64); err == nil && n > maxSeq {
				maxSeq = n
			}
		}
	}
	m.seq = maxSeq
}
