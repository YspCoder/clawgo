package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawgo/pkg/logger"
	"clawgo/pkg/providers"

	"github.com/google/uuid"
)

const (
	sessionsIndexFile = "sessions.json"
	openClawVersion   = 3
)

type Session struct {
	Key       string              `json:"key"`
	SessionID string              `json:"session_id,omitempty"`
	Messages  []providers.Message `json:"messages"`
	Summary   string              `json:"summary,omitempty"`
	TokenIn   int                 `json:"token_in,omitempty"`
	TokenOut  int                 `json:"token_out,omitempty"`
	TokenSum  int                 `json:"token_sum,omitempty"`
	Created   time.Time           `json:"created"`
	Updated   time.Time           `json:"updated"`
	mu        sync.RWMutex
}

type SessionMeta struct {
	SessionID   string                 `json:"sessionId"`
	SessionFile string                 `json:"sessionFile"`
	UpdatedAt   int64                  `json:"updatedAt"`
	Extra       map[string]interface{} `json:"-"`
}

func (m *SessionMeta) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	m.Extra = make(map[string]interface{})
	for k, v := range raw {
		switch k {
		case "sessionId":
			_ = json.Unmarshal(v, &m.SessionID)
		case "sessionFile":
			_ = json.Unmarshal(v, &m.SessionFile)
		case "updatedAt":
			if err := unmarshalUpdatedAt(v, &m.UpdatedAt); err != nil {
				return err
			}
		default:
			var anyVal interface{}
			if err := json.Unmarshal(v, &anyVal); err == nil {
				m.Extra[k] = anyVal
			}
		}
	}
	return nil
}

func (m SessionMeta) MarshalJSON() ([]byte, error) {
	obj := make(map[string]interface{})
	for k, v := range m.Extra {
		obj[k] = v
	}
	obj["sessionId"] = m.SessionID
	obj["sessionFile"] = m.SessionFile
	obj["updatedAt"] = m.UpdatedAt
	return json.Marshal(obj)
}

type openClawContentPart struct {
	Type       string                 `json:"type"`
	Text       string                 `json:"text,omitempty"`
	Thinking   string                 `json:"thinking,omitempty"`
	ID         string                 `json:"id,omitempty"`
	Name       string                 `json:"name,omitempty"`
	Arguments  map[string]interface{} `json:"arguments,omitempty"`
	ToolCallID string                 `json:"toolCallId,omitempty"`
	ToolName   string                 `json:"toolName,omitempty"`
	IsError    bool                   `json:"isError,omitempty"`
}

type openClawMessage struct {
	Role      string                `json:"role"`
	Content   []openClawContentPart `json:"content,omitempty"`
	Timestamp int64                 `json:"timestamp,omitempty"`
}

type sessionEvent struct {
	Type      string           `json:"type"`
	Version   int              `json:"version,omitempty"`
	ID        string           `json:"id,omitempty"`
	ParentID  *string          `json:"parentId,omitempty"`
	Timestamp string           `json:"timestamp"`
	Cwd       string           `json:"cwd,omitempty"`
	Message   *openClawMessage `json:"message,omitempty"`
}

type SessionManager struct {
	sessions             map[string]*Session
	sessionIndex         map[string]SessionMeta
	sessionKeyByID       map[string]string
	lastEventBySessionID map[string]string
	mu                   sync.RWMutex
	storage              string
}

func NewSessionManager(storage string) *SessionManager {
	sm := &SessionManager{
		sessions:             make(map[string]*Session),
		sessionIndex:         make(map[string]SessionMeta),
		sessionKeyByID:       make(map[string]string),
		lastEventBySessionID: make(map[string]string),
		storage:              storage,
	}

	if storage != "" {
		if err := os.MkdirAll(storage, 0755); err != nil {
			logger.ErrorCF("session", "Failed to create session storage", map[string]interface{}{
				"storage":         storage,
				logger.FieldError: err.Error(),
			})
		}
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

	now := time.Now().UTC()
	meta, err := sm.ensureSessionMeta(key, now)
	if err != nil {
		logger.WarnCF("session", "Failed to ensure session meta", map[string]interface{}{
			"session_key":     key,
			logger.FieldError: err.Error(),
		})
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()
	if session, ok = sm.sessions[key]; ok {
		return session
	}

	session = &Session{
		Key:       key,
		SessionID: meta.SessionID,
		Messages:  []providers.Message{},
		Created:   now,
		Updated:   now,
	}
	sm.sessions[key] = session
	return session
}

func (sm *SessionManager) AddMessage(sessionKey, role, content string) {
	sm.AddMessageFull(sessionKey, providers.Message{Role: role, Content: content})
}

func (sm *SessionManager) AddMessageFull(sessionKey string, msg providers.Message) {
	session := sm.GetOrCreate(sessionKey)

	session.mu.Lock()
	session.Messages = append(session.Messages, msg)
	session.Updated = time.Now().UTC()
	session.mu.Unlock()

	if err := sm.appendMessage(sessionKey, msg); err != nil {
		logger.ErrorCF("session", "Failed to persist session message", map[string]interface{}{
			"session_key":     sessionKey,
			logger.FieldError: err.Error(),
		})
	}
}

func (sm *SessionManager) appendMessage(sessionKey string, msg providers.Message) error {
	if sm.storage == "" {
		return nil
	}

	now := time.Now().UTC()
	meta, err := sm.ensureSessionMeta(sessionKey, now)
	if err != nil {
		return err
	}
	if err := sm.appendSessionHistory(meta.SessionID, msg); err != nil {
		return err
	}

	meta.UpdatedAt = toUnixMs(now)
	return sm.saveSessionMeta(sessionKey, meta)
}

func (sm *SessionManager) rewriteHistory(sessionKey string, messages []providers.Message) error {
	if sm.storage == "" {
		return nil
	}

	meta, ok := sm.getSession(sessionKey)
	if !ok {
		now := time.Now().UTC()
		var err error
		meta, err = sm.ensureSessionMeta(sessionKey, now)
		if err != nil {
			return err
		}
	}

	sessionPath := sm.resolveSessionFile(meta)
	tmpPath := sessionPath + ".tmp"
	if err := sm.ensureSessionHeader(meta.SessionID, sessionPath); err != nil {
		return err
	}

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	header := sm.buildSessionHeaderEvent(meta.SessionID, time.Now().UTC())
	if err := writeEventLine(f, header); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	lastID := header.ID
	for _, msg := range messages {
		ev := sm.buildMessageEvent(msg, time.Now().UTC(), lastID)
		if err := writeEventLine(f, ev); err != nil {
			_ = f.Close()
			_ = os.Remove(tmpPath)
			return err
		}
		lastID = ev.ID
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, sessionPath); err != nil {
		return err
	}

	sm.mu.Lock()
	sm.lastEventBySessionID[meta.SessionID] = lastID
	sm.mu.Unlock()

	meta.UpdatedAt = toUnixMs(time.Now().UTC())
	return sm.saveSessionMeta(sessionKey, meta)
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
	if !ok {
		return
	}

	session.mu.Lock()
	session.Summary = summary
	session.Updated = time.Now().UTC()
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
	session.Updated = time.Now().UTC()
}

func (sm *SessionManager) MessageCount(key string) int {
	sm.mu.RLock()
	session, ok := sm.sessions[key]
	sm.mu.RUnlock()
	if !ok {
		return 0
	}
	session.mu.RLock()
	defer session.mu.RUnlock()
	return len(session.Messages)
}

func (sm *SessionManager) ListSessionKeys() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	keys := make([]string, 0, len(sm.sessions))
	for k := range sm.sessions {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (sm *SessionManager) CompactHistory(key, summary string, keepLast int) (int, int, error) {
	sm.mu.RLock()
	session, ok := sm.sessions[key]
	sm.mu.RUnlock()
	if !ok {
		return 0, 0, fmt.Errorf("session not found: %s", key)
	}
	if keepLast < 0 {
		keepLast = 0
	}

	session.mu.Lock()
	before := len(session.Messages)
	if keepLast < before {
		session.Messages = session.Messages[before-keepLast:]
	}
	session.Summary = summary
	session.Updated = time.Now().UTC()
	after := len(session.Messages)
	msgs := make([]providers.Message, after)
	copy(msgs, session.Messages)
	session.mu.Unlock()

	if err := sm.rewriteHistory(key, msgs); err != nil {
		return before, after, err
	}
	return before, after, nil
}

func (sm *SessionManager) Save(session *Session) error {
	if sm.storage == "" || session == nil {
		return nil
	}

	session.mu.RLock()
	updated := session.Updated
	session.mu.RUnlock()
	if updated.IsZero() {
		updated = time.Now().UTC()
	}

	meta, err := sm.ensureSessionMeta(session.Key, updated)
	if err != nil {
		return err
	}
	meta.UpdatedAt = toUnixMs(updated)
	return sm.saveSessionMeta(session.Key, meta)
}

func (sm *SessionManager) AddTokenUsage(sessionKey string, in, out, sum int) {
	session := sm.GetOrCreate(sessionKey)
	if sum <= 0 {
		sum = in + out
	}

	session.mu.Lock()
	session.TokenIn += in
	session.TokenOut += out
	session.TokenSum += sum
	session.Updated = time.Now().UTC()
	session.mu.Unlock()

	if sm.storage != "" {
		if err := sm.Save(session); err != nil {
			logger.WarnCF("session", "Failed to persist token usage", map[string]interface{}{
				"session_key":     sessionKey,
				logger.FieldError: err.Error(),
			})
		}
	}
}

func (sm *SessionManager) GetSession(sessionKey string) (SessionMeta, bool) {
	return sm.getSession(sessionKey)
}

func (sm *SessionManager) SaveSessionMeta(sessionKey string, sessionMeta SessionMeta) error {
	return sm.saveSessionMeta(sessionKey, sessionMeta)
}

func (sm *SessionManager) AppendSessionHistory(sessionID string, event providers.Message) error {
	return sm.appendSessionHistory(sessionID, event)
}

func (sm *SessionManager) LoadSessionHistory(sessionID string, limit int) ([]providers.Message, error) {
	messages, _, err := sm.loadSessionHistoryWithState(sessionID, limit)
	return messages, err
}

func (sm *SessionManager) getSession(sessionKey string) (SessionMeta, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	meta, ok := sm.sessionIndex[sessionKey]
	return meta, ok
}

func (sm *SessionManager) saveSessionMeta(sessionKey string, sessionMeta SessionMeta) error {
	if sm.storage == "" {
		return nil
	}

	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return fmt.Errorf("sessionKey is required")
	}
	sessionMeta.SessionID = strings.TrimSpace(sessionMeta.SessionID)
	if sessionMeta.SessionID == "" {
		sessionMeta.SessionID = uuid.NewString()
	}
	if strings.TrimSpace(sessionMeta.SessionFile) == "" {
		sessionMeta.SessionFile = filepath.Join(sm.storage, sessionMeta.SessionID+".jsonl")
	} else if !filepath.IsAbs(sessionMeta.SessionFile) {
		sessionMeta.SessionFile = filepath.Join(sm.storage, sessionMeta.SessionFile)
	}
	if sessionMeta.UpdatedAt == 0 {
		sessionMeta.UpdatedAt = toUnixMs(time.Now().UTC())
	}
	if sessionMeta.Extra == nil {
		sessionMeta.Extra = make(map[string]interface{})
	}

	sm.mu.Lock()
	sm.sessionIndex[sessionKey] = sessionMeta
	sm.sessionKeyByID[sessionMeta.SessionID] = sessionKey
	if sess, ok := sm.sessions[sessionKey]; ok {
		sess.SessionID = sessionMeta.SessionID
	}
	err := sm.writeSessionsIndexLocked()
	sm.mu.Unlock()
	if err != nil {
		return err
	}

	return sm.ensureSessionHeader(sessionMeta.SessionID, sessionMeta.SessionFile)
}

func (sm *SessionManager) appendSessionHistory(sessionID string, event providers.Message) error {
	if sm.storage == "" {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("sessionID is required")
	}

	sessionPath := filepath.Join(sm.storage, sessionID+".jsonl")
	sm.mu.RLock()
	if key, ok := sm.sessionKeyByID[sessionID]; ok {
		if meta, ok := sm.sessionIndex[key]; ok {
			sessionPath = sm.resolveSessionFile(meta)
		}
	}
	parent := sm.lastEventBySessionID[sessionID]
	sm.mu.RUnlock()

	if err := sm.ensureSessionHeader(sessionID, sessionPath); err != nil {
		return err
	}

	f, err := os.OpenFile(sessionPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	ev := sm.buildMessageEvent(event, time.Now().UTC(), parent)
	if err := writeEventLine(f, ev); err != nil {
		return err
	}

	sm.mu.Lock()
	sm.lastEventBySessionID[sessionID] = ev.ID
	sm.mu.Unlock()
	return nil
}

func (sm *SessionManager) loadSessionHistory(sessionID string, limit int) ([]providers.Message, error) {
	messages, _, err := sm.loadSessionHistoryWithState(sessionID, limit)
	return messages, err
}

func (sm *SessionManager) loadSessionHistoryWithState(sessionID string, limit int) ([]providers.Message, string, error) {
	if sm.storage == "" || strings.TrimSpace(sessionID) == "" {
		return []providers.Message{}, "", nil
	}

	sessionPath := filepath.Join(sm.storage, sessionID+".jsonl")
	sm.mu.RLock()
	if key, ok := sm.sessionKeyByID[sessionID]; ok {
		if meta, ok := sm.sessionIndex[key]; ok {
			sessionPath = sm.resolveSessionFile(meta)
		}
	}
	sm.mu.RUnlock()

	f, err := os.Open(sessionPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []providers.Message{}, "", nil
		}
		return nil, "", err
	}
	defer f.Close()

	messages := make([]providers.Message, 0)
	var lastEventID string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev sessionEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			logger.WarnCF("session", "Skip malformed session event", map[string]interface{}{
				"session_id":      sessionID,
				logger.FieldError: err.Error(),
			})
			continue
		}
		if strings.TrimSpace(ev.ID) != "" {
			lastEventID = ev.ID
		}
		if ev.Type != "message" || ev.Message == nil {
			continue
		}
		messages = append(messages, toProviderMessage(*ev.Message))
	}
	if err := scanner.Err(); err != nil {
		return nil, "", err
	}

	if limit > 0 && len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return messages, lastEventID, nil
}

func (sm *SessionManager) ensureSessionMeta(sessionKey string, updatedAt time.Time) (SessionMeta, error) {
	if sm.storage == "" {
		return SessionMeta{}, nil
	}

	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return SessionMeta{}, fmt.Errorf("sessionKey is required")
	}

	if meta, ok := sm.getSession(sessionKey); ok {
		if !updatedAt.IsZero() {
			meta.UpdatedAt = toUnixMs(updatedAt)
		}
		if err := sm.saveSessionMeta(sessionKey, meta); err != nil {
			return SessionMeta{}, err
		}
		return meta, nil
	}

	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	meta := SessionMeta{
		SessionID:   uuid.NewString(),
		SessionFile: filepath.Join(sm.storage, uuid.NewString()+".jsonl"),
		UpdatedAt:   toUnixMs(updatedAt),
	}
	meta.SessionFile = filepath.Join(sm.storage, meta.SessionID+".jsonl")
	if err := sm.saveSessionMeta(sessionKey, meta); err != nil {
		return SessionMeta{}, err
	}
	meta, ok := sm.getSession(sessionKey)
	if !ok {
		return SessionMeta{}, fmt.Errorf("session meta not found after save: %s", sessionKey)
	}
	return meta, nil
}

func (sm *SessionManager) loadSessions() error {
	if sm.storage == "" {
		return nil
	}
	if err := sm.readSessionsIndex(); err != nil {
		return err
	}

	sm.mu.Lock()
	for sessionKey, meta := range sm.sessionIndex {
		sm.sessionKeyByID[meta.SessionID] = sessionKey
		updated := fromUnixMs(meta.UpdatedAt)
		sm.sessions[sessionKey] = &Session{
			Key:       sessionKey,
			SessionID: meta.SessionID,
			Messages:  []providers.Message{},
			Created:   updated,
			Updated:   updated,
		}
	}
	sm.mu.Unlock()

	sm.mu.RLock()
	indexCopy := make(map[string]SessionMeta, len(sm.sessionIndex))
	for k, v := range sm.sessionIndex {
		indexCopy[k] = v
	}
	sm.mu.RUnlock()

	for sessionKey, meta := range indexCopy {
		messages, lastID, err := sm.loadSessionHistoryWithState(meta.SessionID, 0)
		if err != nil {
			logger.WarnCF("session", "Failed to load session history", map[string]interface{}{
				"session_key":     sessionKey,
				"session_id":      meta.SessionID,
				logger.FieldError: err.Error(),
			})
			continue
		}
		sm.mu.Lock()
		sm.lastEventBySessionID[meta.SessionID] = lastID
		session := sm.sessions[sessionKey]
		sm.mu.Unlock()
		if session == nil {
			continue
		}
		session.mu.Lock()
		session.Messages = append(session.Messages, messages...)
		session.mu.Unlock()
	}

	return nil
}

func (sm *SessionManager) readSessionsIndex() error {
	indexPath := filepath.Join(sm.storage, sessionsIndexFile)
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}

	parsed := make(map[string]SessionMeta)
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}

	sm.mu.Lock()
	for k, meta := range parsed {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		meta.SessionID = strings.TrimSpace(meta.SessionID)
		if meta.SessionID == "" {
			continue
		}
		if strings.TrimSpace(meta.SessionFile) == "" {
			meta.SessionFile = filepath.Join(sm.storage, meta.SessionID+".jsonl")
		} else if !filepath.IsAbs(meta.SessionFile) {
			meta.SessionFile = filepath.Join(sm.storage, meta.SessionFile)
		}
		if meta.UpdatedAt == 0 {
			meta.UpdatedAt = toUnixMs(time.Now().UTC())
		}
		if meta.Extra == nil {
			meta.Extra = make(map[string]interface{})
		}
		sm.sessionIndex[k] = meta
	}
	sm.mu.Unlock()
	return nil
}

func (sm *SessionManager) writeSessionsIndexLocked() error {
	if sm.storage == "" {
		return nil
	}

	indexPath := filepath.Join(sm.storage, sessionsIndexFile)
	tmpPath := indexPath + ".tmp"

	indexCopy := make(map[string]SessionMeta, len(sm.sessionIndex))
	for k, v := range sm.sessionIndex {
		indexCopy[k] = v
	}

	data, err := json.MarshalIndent(indexCopy, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, indexPath)
}

func (sm *SessionManager) ensureSessionHeader(sessionID, sessionFile string) error {
	if sm.storage == "" {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("sessionID is required")
	}
	sessionPath := sessionFile
	if strings.TrimSpace(sessionPath) == "" {
		sessionPath = filepath.Join(sm.storage, sessionID+".jsonl")
	}
	if !filepath.IsAbs(sessionPath) {
		sessionPath = filepath.Join(sm.storage, sessionPath)
	}

	if err := os.MkdirAll(filepath.Dir(sessionPath), 0755); err != nil {
		return err
	}

	st, err := os.Stat(sessionPath)
	if err == nil && st.Size() > 0 {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	f, err := os.OpenFile(sessionPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	ev := sm.buildSessionHeaderEvent(sessionID, time.Now().UTC())
	if err := writeEventLine(f, ev); err != nil {
		return err
	}

	sm.mu.Lock()
	sm.lastEventBySessionID[sessionID] = ev.ID
	sm.mu.Unlock()
	return nil
}

func (sm *SessionManager) buildSessionHeaderEvent(sessionID string, ts time.Time) sessionEvent {
	cwd, _ := os.Getwd()
	return sessionEvent{
		Type:      "session",
		Version:   openClawVersion,
		ID:        sessionID,
		Timestamp: ts.UTC().Format(time.RFC3339Nano),
		Cwd:       cwd,
	}
}

func (sm *SessionManager) buildMessageEvent(msg providers.Message, ts time.Time, parentID string) sessionEvent {
	evID := newEventID()
	var parent *string
	parentID = strings.TrimSpace(parentID)
	if parentID != "" {
		p := parentID
		parent = &p
	}
	return sessionEvent{
		Type:      "message",
		ID:        evID,
		ParentID:  parent,
		Timestamp: ts.UTC().Format(time.RFC3339Nano),
		Message:   toOpenClawMessage(msg, ts),
	}
}

func toOpenClawMessage(msg providers.Message, ts time.Time) *openClawMessage {
	parts := make([]openClawContentPart, 0, 1+len(msg.ContentParts)+len(msg.ToolCalls))
	for _, p := range msg.ContentParts {
		if p.Type == "" {
			continue
		}
		part := openClawContentPart{Type: p.Type, Text: p.Text}
		parts = append(parts, part)
	}
	if strings.TrimSpace(msg.Content) != "" {
		partType := "text"
		if msg.Role == "assistant" && strings.Contains(msg.Content, "<think>") {
			partType = "thinking"
		}
		parts = append(parts, openClawContentPart{Type: partType, Text: msg.Content})
	}
	for _, tc := range msg.ToolCalls {
		part := openClawContentPart{
			Type:      "toolCall",
			ID:        tc.ID,
			Name:      tc.Name,
			Arguments: tc.Arguments,
		}
		if part.Name == "" && tc.Function != nil {
			part.Name = tc.Function.Name
		}
		parts = append(parts, part)
	}
	if msg.Role == "tool" {
		parts = []openClawContentPart{{
			Type:       "toolResult",
			ToolCallID: msg.ToolCallID,
			ToolName:   "tool",
			Text:       msg.Content,
		}}
	}

	return &openClawMessage{
		Role:      msg.Role,
		Content:   parts,
		Timestamp: toUnixMs(ts),
	}
}

func toProviderMessage(msg openClawMessage) providers.Message {
	out := providers.Message{Role: msg.Role}
	textParts := make([]string, 0)
	toolCalls := make([]providers.ToolCall, 0)
	for _, p := range msg.Content {
		switch p.Type {
		case "text", "thinking":
			if strings.TrimSpace(p.Text) != "" {
				textParts = append(textParts, p.Text)
			}
		case "toolCall":
			toolCalls = append(toolCalls, providers.ToolCall{
				ID:        p.ID,
				Type:      "function",
				Name:      p.Name,
				Arguments: p.Arguments,
			})
		case "toolResult":
			if strings.TrimSpace(p.Text) != "" {
				textParts = append(textParts, p.Text)
			}
			if strings.TrimSpace(p.ToolCallID) != "" {
				out.ToolCallID = p.ToolCallID
			}
		}
	}
	if len(toolCalls) > 0 {
		out.ToolCalls = toolCalls
	}
	if len(textParts) > 0 {
		out.Content = strings.Join(textParts, "\n")
	}
	return out
}

func (sm *SessionManager) resolveSessionFile(meta SessionMeta) string {
	if strings.TrimSpace(meta.SessionFile) == "" {
		return filepath.Join(sm.storage, meta.SessionID+".jsonl")
	}
	if filepath.IsAbs(meta.SessionFile) {
		return meta.SessionFile
	}
	return filepath.Join(sm.storage, meta.SessionFile)
}

func writeEventLine(f *os.File, ev sessionEvent) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

func unmarshalUpdatedAt(raw json.RawMessage, out *int64) error {
	if len(raw) == 0 {
		*out = 0
		return nil
	}

	var asInt int64
	if err := json.Unmarshal(raw, &asInt); err == nil {
		*out = asInt
		return nil
	}

	var asFloat float64
	if err := json.Unmarshal(raw, &asFloat); err == nil {
		*out = int64(asFloat)
		return nil
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		if asString == "" {
			*out = 0
			return nil
		}
		if n, err := strconv.ParseInt(asString, 10, 64); err == nil {
			*out = n
			return nil
		}
		if ts, err := time.Parse(time.RFC3339Nano, asString); err == nil {
			*out = toUnixMs(ts)
			return nil
		}
	}

	return fmt.Errorf("invalid updatedAt value: %s", string(raw))
}

func toUnixMs(t time.Time) int64 {
	return t.UTC().UnixNano() / int64(time.Millisecond)
}

func fromUnixMs(ms int64) time.Time {
	if ms <= 0 {
		return time.Now().UTC()
	}
	return time.Unix(0, ms*int64(time.Millisecond)).UTC()
}

func newEventID() string {
	id := strings.ReplaceAll(uuid.NewString(), "-", "")
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
