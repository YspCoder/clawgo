package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type PendingSubagentDraftStore struct {
	path  string
	mu    sync.RWMutex
	items map[string]map[string]interface{}
}

func NewPendingSubagentDraftStore(workspace string) *PendingSubagentDraftStore {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil
	}
	dir := filepath.Join(workspace, "agents", "runtime")
	store := &PendingSubagentDraftStore{
		path:  filepath.Join(dir, "pending_subagent_drafts.json"),
		items: map[string]map[string]interface{}{},
	}
	_ = os.MkdirAll(dir, 0755)
	_ = store.load()
	return store
}

func (s *PendingSubagentDraftStore) load() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.items = map[string]map[string]interface{}{}
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		s.items = map[string]map[string]interface{}{}
		return nil
	}
	items := map[string]map[string]interface{}{}
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}
	s.items = items
	return nil
}

func (s *PendingSubagentDraftStore) All() map[string]map[string]interface{} {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]map[string]interface{}, len(s.items))
	for key, item := range s.items {
		out[key] = cloneDraftMap(item)
	}
	return out
}

func (s *PendingSubagentDraftStore) Get(sessionKey string) map[string]interface{} {
	if s == nil {
		return nil
	}
	sessionKey = strings.TrimSpace(sessionKey)
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneDraftMap(s.items[sessionKey])
}

func (s *PendingSubagentDraftStore) Put(sessionKey string, draft map[string]interface{}) error {
	if s == nil {
		return nil
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || draft == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = map[string]map[string]interface{}{}
	}
	s.items[sessionKey] = cloneDraftMap(draft)
	return s.persistLocked()
}

func (s *PendingSubagentDraftStore) Delete(sessionKey string) error {
	if s == nil {
		return nil
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, sessionKey)
	return s.persistLocked()
}

func (s *PendingSubagentDraftStore) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

func cloneDraftMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		switch vv := v.(type) {
		case []string:
			out[k] = append([]string(nil), vv...)
		case []interface{}:
			cp := make([]interface{}, len(vv))
			copy(cp, vv)
			out[k] = cp
		default:
			out[k] = vv
		}
	}
	return out
}
