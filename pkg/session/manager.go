package session

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawgo/pkg/providers"
)

type Session struct {
	Key               string              `json:"key"`
	SessionID         string              `json:"session_id,omitempty"`
	Kind              string              `json:"kind,omitempty"`
	Messages          []providers.Message `json:"messages"`
	Summary           string              `json:"summary,omitempty"`
	CompactionCount   int                 `json:"compaction_count,omitempty"`
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
		ToolCallID string               `json:"toolCallId,omitempty"`
		ToolName   string               `json:"toolName,omitempty"`
		ToolCalls  []providers.ToolCall `json:"toolCalls,omitempty"`
	} `json:"message,omitempty"`
}

func NewSessionManager(storage string) *SessionManager {
	sm := &SessionManager{
		sessions: make(map[string]*Session),
		storage:  storage,
	}

	if storage != "" {
		os.MkdirAll(storage, 0755)
		sm.migrateLegacySessions()
		sm.cleanupArchivedSessions()
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
		Key:       key,
		SessionID: deriveSessionID(key),
		Kind:      detectSessionKind(key),
		Messages:  []providers.Message{},
		Created:   time.Now(),
		Updated:   time.Now(),
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

	// Persist immediately (append-only).
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

func (sm *SessionManager) CompactSession(key string, keepLast int, note string) bool {
	sm.mu.RLock()
	session, ok := sm.sessions[key]
	sm.mu.RUnlock()
	if !ok {
		return false
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	if keepLast <= 0 || len(session.Messages) <= keepLast {
		return false
	}
	session.Messages = session.Messages[len(session.Messages)-keepLast:]
	session.CompactionCount++
	if strings.TrimSpace(note) != "" {
		if strings.TrimSpace(session.Summary) == "" {
			session.Summary = note
		} else {
			session.Summary += "\n" + note
		}
	}
	session.Updated = time.Now()
	return true
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
	// Messages are persisted incrementally via AddMessageFull.
	// Metadata is now centralized in sessions.json (OpenClaw-style index).
	if sm.storage == "" {
		return nil
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
			SessionID:         s.SessionID,
			Kind:              s.Kind,
			Summary:           s.Summary,
			CompactionCount:   s.CompactionCount,
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
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text,omitempty"`
			} `json:"content,omitempty"`
			ToolCallID string               `json:"toolCallId,omitempty"`
			ToolName   string               `json:"toolName,omitempty"`
			ToolCalls  []providers.ToolCall `json:"toolCalls,omitempty"`
		}{
			Role: mappedRole,
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text,omitempty"`
			}{
				{Type: "text", Text: msg.Content},
			},
			ToolCallID: msg.ToolCallID,
			ToolCalls:  msg.ToolCalls,
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
	return providers.Message{Role: role, Content: content, ToolCallID: event.Message.ToolCallID, ToolCalls: event.Message.ToolCalls}, true
}

func deriveSessionID(key string) string {
	sum := sha1.Sum([]byte("clawgo-session:" + key))
	h := hex.EncodeToString(sum[:])
	// UUID-like deterministic id
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
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
		sid := strings.TrimSpace(s.SessionID)
		if sid == "" {
			sid = deriveSessionID(key)
		}
		entry := map[string]interface{}{
			"sessionId":       sid,
			"sessionKey":      key,
			"updatedAt":       s.Updated.UnixMilli(),
			"systemSent":      true,
			"abortedLastRun":  false,
			"compactionCount": s.CompactionCount,
			"chatType":        mapKindToChatType(s.Kind),
			"sessionFile":     sessionFile,
			"kind":            s.Kind,
		}
		s.mu.RUnlock()
		index[key] = entry
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(sm.storage, "sessions.json"), data, 0644); err != nil {
		return err
	}
	// Cleanup legacy .meta files: sessions.json is source of truth now.
	entries, _ := os.ReadDir(sm.storage)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".meta" {
			continue
		}
		_ = os.Remove(filepath.Join(sm.storage, e.Name()))
	}
	return nil
}

func (sm *SessionManager) migrateLegacySessions() {
	if sm.storage == "" {
		return
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(sm.storage))) // ~/.clawgo
	candidates := []string{
		filepath.Join(root, "sessions"),
		filepath.Join(filepath.Dir(sm.storage), "sessions"),
	}
	for _, legacy := range candidates {
		if strings.TrimSpace(legacy) == "" || legacy == sm.storage {
			continue
		}
		entries, err := os.ReadDir(legacy)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !(strings.HasSuffix(name, ".jsonl") || name == "sessions.json" || strings.Contains(name, ".jsonl.deleted.")) {
				continue
			}
			src := filepath.Join(legacy, name)
			dst := filepath.Join(sm.storage, name)
			if _, err := os.Stat(dst); err == nil {
				continue
			}
			_ = os.Rename(src, dst)
		}
	}
}

func (sm *SessionManager) cleanupArchivedSessions() {
	if sm.storage == "" {
		return
	}
	days := 30
	if v := strings.TrimSpace(os.Getenv("CLAWGO_SESSION_ARCHIVE_RETENTION_DAYS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			days = n
		}
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	entries, err := os.ReadDir(sm.storage)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.Contains(name, ".jsonl.deleted.") {
			continue
		}
		full := filepath.Join(sm.storage, name)
		fi, err := os.Stat(full)
		if err != nil {
			continue
		}
		if fi.ModTime().Before(cutoff) {
			_ = os.Remove(full)
		}
	}
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
	// 1) Load sessions index first (sessions.json as source of truth)
	indexPath := filepath.Join(sm.storage, "sessions.json")
	if data, err := os.ReadFile(indexPath); err == nil {
		var index map[string]struct {
			SessionID       string `json:"sessionId"`
			SessionKey      string `json:"sessionKey"`
			UpdatedAt       int64  `json:"updatedAt"`
			Kind            string `json:"kind"`
			ChatType        string `json:"chatType"`
			CompactionCount int    `json:"compactionCount"`
		}
		if err := json.Unmarshal(data, &index); err == nil {
			for key, row := range index {
				session := sm.GetOrCreate(key)
				session.mu.Lock()
				if strings.TrimSpace(row.SessionID) != "" {
					session.SessionID = row.SessionID
				}
				if strings.TrimSpace(row.Kind) != "" {
					session.Kind = row.Kind
				} else if strings.TrimSpace(row.ChatType) == "direct" {
					session.Kind = "main"
				}
				if row.UpdatedAt > 0 {
					session.Updated = time.UnixMilli(row.UpdatedAt)
				}
				session.CompactionCount = row.CompactionCount
				session.mu.Unlock()
			}
		}
	}

	// 2) Load JSONL histories
	files, err := os.ReadDir(sm.storage)
	if err != nil {
		return err
	}
	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".jsonl" {
			continue
		}
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

	return sm.writeOpenClawSessionsIndex()
}
