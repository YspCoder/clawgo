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
	Key               string              `json:"key"`
	Kind              string              `json:"kind,omitempty"`
	Messages          []providers.Message `json:"messages"`
	Summary           string              `json:"summary,omitempty"`
	LastLanguage      string              `json:"last_language,omitempty"`
	PreferredLanguage string              `json:"preferred_language,omitempty"`
	Created           time.Time           `json:"created"`
	Updated           time.Time           `json:"updated"`
	mu                sync.RWMutex
}

type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	storage  string
}

type openClawEvent struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp,omitempty"`
	Message   *struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		} `json:"content,omitempty"`
		ToolCallID string `json:"toolCallId,omitempty"`
		ToolName   string `json:"toolName,omitempty"`
	} `json:"message,omitempty"`
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
		Kind:     detectSessionKind(key),
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

	event := toOpenClawMessageEvent(msg)
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	if _, err = f.Write(append(data, '\n')); err != nil {
		return err
	}
	return sm.writeOpenClawSessionsIndex()
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

func (sm *SessionManager) GetLanguagePreferences(key string) (preferred string, last string) {
	sm.mu.RLock()
	session, ok := sm.sessions[key]
	sm.mu.RUnlock()
	if !ok {
		return "", ""
	}

	session.mu.RLock()
	defer session.mu.RUnlock()
	return session.PreferredLanguage, session.LastLanguage
}

func (sm *SessionManager) SetLastLanguage(key, lang string) {
	if strings.TrimSpace(lang) == "" {
		return
	}
	session := sm.GetOrCreate(key)
	session.mu.Lock()
	session.LastLanguage = lang
	session.Updated = time.Now()
	session.mu.Unlock()
}

func (sm *SessionManager) SetPreferredLanguage(key, lang string) {
	if strings.TrimSpace(lang) == "" {
		return
	}
	session := sm.GetOrCreate(key)
	session.mu.Lock()
	session.PreferredLanguage = lang
	session.Updated = time.Now()
	session.mu.Unlock()
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
		"kind":               session.Kind,
		"summary":            session.Summary,
		"last_language":      session.LastLanguage,
		"preferred_language": session.PreferredLanguage,
		"updated":            session.Updated,
		"created":            session.Created,
	}
	data, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(metaPath, data, 0644); err != nil {
		return err
	}
	return sm.writeOpenClawSessionsIndex()
}

func (sm *SessionManager) Count() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}

func (sm *SessionManager) Keys() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	keys := make([]string, 0, len(sm.sessions))
	for k := range sm.sessions {
		keys = append(keys, k)
	}
	return keys
}

func (sm *SessionManager) List(limit int) []Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	items := make([]Session, 0, len(sm.sessions))
	for _, s := range sm.sessions {
		s.mu.RLock()
		items = append(items, Session{
			Key:               s.Key,
			Kind:              s.Kind,
			Summary:           s.Summary,
			LastLanguage:      s.LastLanguage,
			PreferredLanguage: s.PreferredLanguage,
			Created:           s.Created,
			Updated:           s.Updated,
		})
		s.mu.RUnlock()
	}
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func toOpenClawMessageEvent(msg providers.Message) openClawEvent {
	role := strings.TrimSpace(strings.ToLower(msg.Role))
	mappedRole := role
	if role == "tool" {
		mappedRole = "toolResult"
	}
	e := openClawEvent{
		Type:      "message",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Message: &struct {
			Role       string `json:"role"`
			Content    []struct {
				Type string `json:"type"`
				Text string `json:"text,omitempty"`
			} `json:"content,omitempty"`
			ToolCallID string `json:"toolCallId,omitempty"`
			ToolName   string `json:"toolName,omitempty"`
		}{
			Role: mappedRole,
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text,omitempty"`
			}{
				{Type: "text", Text: msg.Content},
			},
			ToolCallID: msg.ToolCallID,
		},
	}
	return e
}

func fromJSONLLine(line []byte) (providers.Message, bool) {
	var raw providers.Message
	if err := json.Unmarshal(line, &raw); err == nil && strings.TrimSpace(raw.Role) != "" {
		return raw, true
	}
	var event openClawEvent
	if err := json.Unmarshal(line, &event); err != nil {
		return providers.Message{}, false
	}
	if event.Type != "message" || event.Message == nil {
		return providers.Message{}, false
	}
	role := strings.TrimSpace(strings.ToLower(event.Message.Role))
	if role == "toolresult" {
		role = "tool"
	}
	var content string
	for _, part := range event.Message.Content {
		if strings.TrimSpace(strings.ToLower(part.Type)) == "text" {
			if content != "" {
				content += "\n"
			}
			content += part.Text
		}
	}
	return providers.Message{Role: role, Content: content, ToolCallID: event.Message.ToolCallID}, true
}

func detectSessionKind(key string) string {
	k := strings.TrimSpace(strings.ToLower(key))
	switch {
	case strings.HasPrefix(k, "cron:"):
		return "cron"
	case strings.HasPrefix(k, "subagent:") || strings.Contains(k, ":subagent:"):
		return "subagent"
	case strings.HasPrefix(k, "hook:"):
		return "hook"
	case strings.HasPrefix(k, "node:"):
		return "node"
	case strings.Contains(k, ":"):
		return "main"
	default:
		return "other"
	}
}

func (sm *SessionManager) writeOpenClawSessionsIndex() error {
	if sm.storage == "" {
		return nil
	}
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	index := map[string]map[string]interface{}{}
	for key, s := range sm.sessions {
		s.mu.RLock()
		sessionFile := filepath.Join(sm.storage, key+".jsonl")
		entry := map[string]interface{}{
			"sessionId":  key,
			"updatedAt":  s.Updated.UnixMilli(),
			"chatType":   mapKindToChatType(s.Kind),
			"sessionFile": sessionFile,
		}
		s.mu.RUnlock()
		index[key] = entry
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(sm.storage, "sessions.json"), data, 0644)
}

func mapKindToChatType(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "main":
		return "direct"
	case "cron", "subagent", "hook", "node":
		return "internal"
	default:
		return "unknown"
	}
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
				if msg, ok := fromJSONLLine(scanner.Bytes()); ok {
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
					Kind              string    `json:"kind"`
					Summary           string    `json:"summary"`
					LastLanguage      string    `json:"last_language"`
					PreferredLanguage string    `json:"preferred_language"`
					Updated           time.Time `json:"updated"`
					Created           time.Time `json:"created"`
				}
				if err := json.Unmarshal(data, &meta); err == nil {
					session.Kind = meta.Kind
					if strings.TrimSpace(session.Kind) == "" {
						session.Kind = detectSessionKind(session.Key)
					}
					session.Summary = meta.Summary
					session.LastLanguage = meta.LastLanguage
					session.PreferredLanguage = meta.PreferredLanguage
					session.Updated = meta.Updated
					session.Created = meta.Created
				}
			}
		}
	}

	return nil
}
