package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clawgo/pkg/providers"
)

type Session struct {
	Key      string              `json:"key"`
	Messages []providers.Message `json:"messages"`
	Summary  string              `json:"summary,omitempty"`
	Created  time.Time           `json:"created"`
	Updated  time.Time           `json:"updated"`
	mu       sync.RWMutex
}

type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	storage  string
}

func NewSessionManager(storage string) *SessionManager {
	sm := &SessionManager{
		sessions: make(map[string]*Session),
		storage:  storage,
	}

	if storage != "" {
		os.MkdirAll(storage, 0755)
		sm.loadSessions()
	}

	return sm
}

func (sm *SessionManager) GetOrCreate(key string) *Session {
	sm.mu.RLock()
	session, ok := sm.sessions[key]
	sm.mu.RUnlock()

	if ok {
		return session
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Re-check existence after acquiring Write lock
	if session, ok = sm.sessions[key]; ok {
		return session
	}

	session = &Session{
		Key:      key,
		Messages: []providers.Message{},
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	sm.sessions[key] = session

	return session
}

func (sm *SessionManager) AddMessage(sessionKey, role, content string) {
	sm.AddMessageFull(sessionKey, providers.Message{
		Role:    role,
		Content: content,
	})
}

func (sm *SessionManager) AddMessageFull(sessionKey string, msg providers.Message) {
	session := sm.GetOrCreate(sessionKey)

	session.mu.Lock()
	session.Messages = append(session.Messages, msg)
	session.Updated = time.Now()
	session.mu.Unlock()

	// 立即持久化 (Append-only)
	sm.appendMessage(sessionKey, msg)
}

func (sm *SessionManager) appendMessage(sessionKey string, msg providers.Message) error {
	if sm.storage == "" {
		return nil
	}

	sessionPath := filepath.Join(sm.storage, sessionKey+".jsonl")
	f, err := os.OpenFile(sessionPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = f.Write(append(data, '\n'))
	return err
}

func (sm *SessionManager) GetHistory(key string) []providers.Message {
	sm.mu.RLock()
	session, ok := sm.sessions[key]
	sm.mu.RUnlock()

	if !ok {
		return []providers.Message{}
	}

	session.mu.RLock()
	defer session.mu.RUnlock()

	history := make([]providers.Message, len(session.Messages))
	copy(history, session.Messages)
	return history
}

func (sm *SessionManager) GetSummary(key string) string {
	sm.mu.RLock()
	session, ok := sm.sessions[key]
	sm.mu.RUnlock()

	if !ok {
		return ""
	}

	session.mu.RLock()
	defer session.mu.RUnlock()

	return session.Summary
}

func (sm *SessionManager) SetSummary(key string, summary string) {
	sm.mu.RLock()
	session, ok := sm.sessions[key]
	sm.mu.RUnlock()

	if ok {
		session.mu.Lock()
		defer session.mu.Unlock()

		session.Summary = summary
		session.Updated = time.Now()
	}
}

func (sm *SessionManager) TruncateHistory(key string, keepLast int) {
	sm.mu.RLock()
	session, ok := sm.sessions[key]
	sm.mu.RUnlock()

	if !ok {
		return
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if len(session.Messages) <= keepLast {
		return
	}

	session.Messages = session.Messages[len(session.Messages)-keepLast:]
	session.Updated = time.Now()
}

func (sm *SessionManager) Save(session *Session) error {
	// 现已通过 AddMessageFull 实时增量持久化
	// 这里保留 Save 方法用于更新 Summary 等元数据
	if sm.storage == "" {
		return nil
	}

	metaPath := filepath.Join(sm.storage, session.Key+".meta")
	meta := map[string]interface{}{
		"summary": session.Summary,
		"updated": session.Updated,
		"created": session.Created,
	}
	data, _ := json.MarshalIndent(meta, "", "  ")
	return os.WriteFile(metaPath, data, 0644)
}

func (sm *SessionManager) loadSessions() error {
	files, err := os.ReadDir(sm.storage)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		// 处理 JSONL 历史消息
		if filepath.Ext(file.Name()) == ".jsonl" {
			sessionKey := strings.TrimSuffix(file.Name(), ".jsonl")
			session := sm.GetOrCreate(sessionKey)

			f, err := os.Open(filepath.Join(sm.storage, file.Name()))
			if err != nil {
				continue
			}
			
			scanner := bufio.NewScanner(f)
			session.mu.Lock()
			for scanner.Scan() {
				var msg providers.Message
				if err := json.Unmarshal(scanner.Bytes(), &msg); err == nil {
					session.Messages = append(session.Messages, msg)
				}
			}
			session.mu.Unlock()
			f.Close()
		}

		// 处理元数据
		if filepath.Ext(file.Name()) == ".meta" {
			sessionKey := strings.TrimSuffix(file.Name(), ".meta")
			session := sm.GetOrCreate(sessionKey)

			data, err := os.ReadFile(filepath.Join(sm.storage, file.Name()))
			if err == nil {
				var meta struct {
					Summary string    `json:"summary"`
					Updated time.Time `json:"updated"`
					Created time.Time `json:"created"`
				}
				if err := json.Unmarshal(data, &meta); err == nil {
					session.Summary = meta.Summary
					session.Updated = meta.Updated
					session.Created = meta.Created
				}
			}
		}
	}

	return nil
}
