package tools

import (
	"bytes"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type processSession struct {
	ID        string
	Command   string
	StartedAt time.Time
	EndedAt   time.Time
	ExitCode  *int
	cmd       *exec.Cmd
	done      chan struct{}
	mu        sync.RWMutex
	log       bytes.Buffer
}

type ProcessManager struct {
	mu       sync.RWMutex
	sessions map[string]*processSession
	seq      uint64
}

func NewProcessManager() *ProcessManager {
	return &ProcessManager{sessions: map[string]*processSession{}}
}

func (m *ProcessManager) Start(command, cwd string) (string, error) {
	id := "p-" + strconv.FormatUint(atomic.AddUint64(&m.seq, 1), 10)
	cmd := exec.Command("sh", "-c", command)
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
	s := &processSession{ID: id, Command: command, StartedAt: time.Now().UTC(), cmd: cmd, done: make(chan struct{})}

	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()

	if err := cmd.Start(); err != nil {
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
		close(s.done)
	}()

	return id, nil
}

func (m *ProcessManager) capture(s *processSession, r interface{ Read([]byte) (int, error) }) {
	buf := make([]byte, 2048)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			s.mu.Lock()
			_, _ = s.log.Write(buf[:n])
			s.mu.Unlock()
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
	return cmd.Process.Kill()
}
