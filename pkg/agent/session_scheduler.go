package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/YspCoder/clawgo/pkg/scheduling"
)

const (
	defaultSessionMaxParallelRuns = 4
	maxSessionMaxParallelRuns     = 16
)

var errSessionSchedulerClosed = errors.New("session scheduler closed")

type sessionWaiter struct {
	id   uint64
	keys []string
	ch   chan struct{}
}

type sessionState struct {
	running  int
	owners   map[string]uint64
	inflight map[uint64][]string
	waiters  []*sessionWaiter
}

type SessionScheduler struct {
	maxParallel int

	mu       sync.Mutex
	sessions map[string]*sessionState
	closed   bool
	nextID   uint64
}

func NewSessionScheduler(maxParallel int) *SessionScheduler {
	if maxParallel <= 0 {
		maxParallel = defaultSessionMaxParallelRuns
	}
	if maxParallel < 1 {
		maxParallel = 1
	}
	if maxParallel > maxSessionMaxParallelRuns {
		maxParallel = maxSessionMaxParallelRuns
	}
	return &SessionScheduler{
		maxParallel: maxParallel,
		sessions:    map[string]*sessionState{},
	}
}

func (s *SessionScheduler) Acquire(ctx context.Context, sessionKey string, keys []string) (func(), error) {
	if s == nil {
		return func() {}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sessionKey = normalizeSessionKey(sessionKey)
	keys = scheduling.NormalizeResourceKeys(keys)
	if len(keys) == 0 {
		keys = []string{"session:" + sessionKey}
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errSessionSchedulerClosed
	}
	st := s.ensureSessionLocked(sessionKey)
	runID := atomic.AddUint64(&s.nextID, 1)
	if s.canRunLocked(st, keys) {
		s.grantLocked(st, runID, keys)
		s.mu.Unlock()
		return s.releaseFunc(sessionKey, runID), nil
	}

	w := &sessionWaiter{id: runID, keys: keys, ch: make(chan struct{}, 1)}
	st.waiters = append(st.waiters, w)
	s.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			if st = s.sessions[sessionKey]; st != nil {
				s.removeWaiterLocked(st, runID)
				s.pruneSessionLocked(sessionKey, st)
			}
			s.mu.Unlock()
			return nil, ctx.Err()
		case <-w.ch:
			s.mu.Lock()
			st = s.sessions[sessionKey]
			if st != nil {
				if _, ok := st.inflight[runID]; ok {
					s.mu.Unlock()
					return s.releaseFunc(sessionKey, runID), nil
				}
			}
			if s.closed {
				s.mu.Unlock()
				return nil, errSessionSchedulerClosed
			}
			s.mu.Unlock()
		}
	}
}

func (s *SessionScheduler) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	for _, st := range s.sessions {
		for _, w := range st.waiters {
			select {
			case w.ch <- struct{}{}:
			default:
			}
		}
	}
}

func (s *SessionScheduler) releaseFunc(sessionKey string, runID uint64) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			s.release(sessionKey, runID)
		})
	}
}

func (s *SessionScheduler) release(sessionKey string, runID uint64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.sessions[sessionKey]
	if st == nil {
		return
	}
	keys, ok := st.inflight[runID]
	if !ok {
		return
	}
	for _, k := range keys {
		delete(st.owners, k)
	}
	delete(st.inflight, runID)
	if st.running > 0 {
		st.running--
	}
	s.scheduleWaitersLocked(st)
	s.pruneSessionLocked(sessionKey, st)
}

func (s *SessionScheduler) ensureSessionLocked(sessionKey string) *sessionState {
	st := s.sessions[sessionKey]
	if st != nil {
		return st
	}
	st = &sessionState{
		owners:   map[string]uint64{},
		inflight: map[uint64][]string{},
		waiters:  make([]*sessionWaiter, 0, 4),
	}
	s.sessions[sessionKey] = st
	return st
}

func (s *SessionScheduler) canRunLocked(st *sessionState, keys []string) bool {
	if st == nil {
		return false
	}
	if st.running >= s.maxParallel {
		return false
	}
	for _, k := range keys {
		if _, ok := st.owners[k]; ok {
			return false
		}
	}
	return true
}

func (s *SessionScheduler) grantLocked(st *sessionState, runID uint64, keys []string) {
	if st == nil {
		return
	}
	st.running++
	st.inflight[runID] = append([]string(nil), keys...)
	for _, k := range keys {
		st.owners[k] = runID
	}
}

func (s *SessionScheduler) scheduleWaitersLocked(st *sessionState) {
	if st == nil || len(st.waiters) == 0 {
		return
	}
	for {
		progress := false
		for i := 0; i < len(st.waiters); {
			w := st.waiters[i]
			if !s.canRunLocked(st, w.keys) {
				i++
				continue
			}
			s.grantLocked(st, w.id, w.keys)
			st.waiters = append(st.waiters[:i], st.waiters[i+1:]...)
			select {
			case w.ch <- struct{}{}:
			default:
			}
			progress = true
		}
		if !progress {
			break
		}
	}
}

func (s *SessionScheduler) removeWaiterLocked(st *sessionState, runID uint64) {
	if st == nil || len(st.waiters) == 0 {
		return
	}
	for i, w := range st.waiters {
		if w.id != runID {
			continue
		}
		st.waiters = append(st.waiters[:i], st.waiters[i+1:]...)
		return
	}
}

func (s *SessionScheduler) pruneSessionLocked(sessionKey string, st *sessionState) {
	if st == nil {
		delete(s.sessions, sessionKey)
		return
	}
	if st.running == 0 && len(st.waiters) == 0 {
		delete(s.sessions, sessionKey)
	}
}

func normalizeSessionKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return "default"
	}
	return sessionKey
}
