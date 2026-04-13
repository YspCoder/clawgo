package session

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/YspCoder/clawgo/pkg/jsonlog"
	"github.com/YspCoder/clawgo/pkg/providers"
)

const (
	defaultSessionSegmentMaxMessages = 200
	defaultSessionSegmentMaxBytes    = 2 * 1024 * 1024
	defaultSessionPromptLoadSegments = 2
	maxTokenRefsPerSessionToken      = 48
	maxSearchSnippetsPerSession      = 3
)

var archiveSegmentRe = regexp.MustCompile(`^(?P<key>.+)\.(?P<seq>\d{4})\.jsonl$`)

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
	segments          []sessionSegmentMeta
	nextSeq           int
	index             *sessionIndexFile
}

type SessionManager struct {
	sessions           map[string]*Session
	mu                 sync.RWMutex
	storage            string
	segmentMaxMessages int
	segmentMaxBytes    int64
	promptLoadSegments int
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

type sessionSegmentMeta struct {
	Name         string `json:"name"`
	Archived     bool   `json:"archived,omitempty"`
	FirstSeq     int    `json:"first_seq,omitempty"`
	LastSeq      int    `json:"last_seq,omitempty"`
	MessageCount int    `json:"message_count,omitempty"`
	LastOffset   int64  `json:"last_offset,omitempty"`
	UpdatedAt    int64  `json:"updated_at,omitempty"`
}

type sessionMetaFile struct {
	Version           int                  `json:"version"`
	SessionKey        string               `json:"session_key"`
	SessionID         string               `json:"session_id,omitempty"`
	Kind              string               `json:"kind,omitempty"`
	Summary           string               `json:"summary,omitempty"`
	CompactionCount   int                  `json:"compaction_count,omitempty"`
	LastLanguage      string               `json:"last_language,omitempty"`
	PreferredLanguage string               `json:"preferred_language,omitempty"`
	MessageCount      int                  `json:"message_count,omitempty"`
	CreatedAt         int64                `json:"created_at,omitempty"`
	UpdatedAt         int64                `json:"updated_at,omitempty"`
	NextSeq           int                  `json:"next_seq,omitempty"`
	Segments          []sessionSegmentMeta `json:"segments,omitempty"`
}

type sessionIndexRef struct {
	Seq     int    `json:"seq"`
	Role    string `json:"role,omitempty"`
	Segment string `json:"segment,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

type sessionIndexFile struct {
	Version    int                          `json:"version"`
	SessionKey string                       `json:"session_key"`
	LastSeq    int                          `json:"last_seq,omitempty"`
	LastOffset int64                        `json:"last_offset,omitempty"`
	Segment    string                       `json:"segment,omitempty"`
	UpdatedAt  int64                        `json:"updated_at,omitempty"`
	Tokens     map[string][]sessionIndexRef `json:"tokens,omitempty"`
}

type SessionSearchSnippet struct {
	Seq     int    `json:"seq"`
	Role    string `json:"role,omitempty"`
	Segment string `json:"segment,omitempty"`
	Content string `json:"content,omitempty"`
}

type SessionSearchResult struct {
	Key       string                 `json:"key"`
	Kind      string                 `json:"kind,omitempty"`
	Summary   string                 `json:"summary,omitempty"`
	UpdatedAt time.Time              `json:"updated_at"`
	Score     int                    `json:"score"`
	Snippets  []SessionSearchSnippet `json:"snippets,omitempty"`
}

type SessionCompactionSnapshot struct {
	Key     string
	History []providers.Message
	Summary string
	NextSeq int
}

type sessionsIndexEntry struct {
	SessionID         string `json:"sessionId"`
	SessionKey        string `json:"sessionKey"`
	UpdatedAt         int64  `json:"updatedAt"`
	Kind              string `json:"kind"`
	ChatType          string `json:"chatType"`
	CompactionCount   int    `json:"compactionCount"`
	SessionFile       string `json:"sessionFile,omitempty"`
	Summary           string `json:"summary,omitempty"`
	LastLanguage      string `json:"lastLanguage,omitempty"`
	PreferredLanguage string `json:"preferredLanguage,omitempty"`
}

type appendMessageResult struct {
	refreshSessionsIndex bool
}

func NewSessionManager(storage string) *SessionManager {
	sm := &SessionManager{
		sessions:           make(map[string]*Session),
		storage:            storage,
		segmentMaxMessages: readPositiveIntEnv("CLAWGO_SESSION_SEGMENT_MAX_MESSAGES", defaultSessionSegmentMaxMessages),
		segmentMaxBytes:    int64(readPositiveIntEnv("CLAWGO_SESSION_SEGMENT_MAX_BYTES", defaultSessionSegmentMaxBytes)),
		promptLoadSegments: readPositiveIntEnv("CLAWGO_SESSION_PROMPT_LOAD_SEGMENTS", defaultSessionPromptLoadSegments),
	}

	if storage != "" {
		_ = os.MkdirAll(storage, 0755)
		sm.cleanupArchivedSessions()
		sm.loadSessions()
	}

	return sm
}

func (sm *SessionManager) GetOrCreate(key string) *Session {
	session, _ := sm.getOrCreate(key)
	return session
}

func (sm *SessionManager) getOrCreate(key string) (*Session, bool) {
	sm.mu.RLock()
	session, ok := sm.sessions[key]
	sm.mu.RUnlock()
	if ok {
		return session, false
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()
	if session, ok = sm.sessions[key]; ok {
		return session, false
	}
	now := time.Now()
	session = &Session{
		Key:       key,
		SessionID: deriveSessionID(key),
		Kind:      detectSessionKind(key),
		Messages:  []providers.Message{},
		Created:   now,
		Updated:   now,
		nextSeq:   1,
	}
	sm.sessions[key] = session
	return session, true
}

func (sm *SessionManager) AddMessage(sessionKey, role, content string) {
	sm.AddMessageFull(sessionKey, providers.Message{Role: role, Content: content})
}

func (sm *SessionManager) AddMessageFull(sessionKey string, msg providers.Message) {
	session, created := sm.getOrCreate(sessionKey)

	persisted := false
	refreshIndex := created
	session.mu.Lock()
	session.Messages = append(session.Messages, msg)
	session.Updated = time.Now()
	if session.Created.IsZero() {
		session.Created = session.Updated
	}
	appendResult, err := sm.appendMessageLocked(session, msg)
	persisted = err == nil
	refreshIndex = refreshIndex || appendResult.refreshSessionsIndex
	session.mu.Unlock()
	if persisted && refreshIndex {
		_ = sm.writeOpenClawSessionsIndex()
	}
}

func (sm *SessionManager) GetPromptHistory(key string) []providers.Message {
	sm.mu.RLock()
	session, ok := sm.sessions[key]
	sm.mu.RUnlock()
	if !ok {
		return nil
	}
	session.mu.RLock()
	defer session.mu.RUnlock()
	history := make([]providers.Message, len(session.Messages))
	copy(history, session.Messages)
	return history
}

func (sm *SessionManager) GetHistory(key string) []providers.Message {
	sm.mu.RLock()
	session, ok := sm.sessions[key]
	sm.mu.RUnlock()
	if !ok {
		return []providers.Message{}
	}

	session.mu.RLock()
	segments := append([]sessionSegmentMeta(nil), session.segments...)
	loaded := make([]providers.Message, len(session.Messages))
	copy(loaded, session.Messages)
	session.mu.RUnlock()

	if len(segments) == 0 {
		return loaded
	}
	all, err := sm.loadMessagesForSegments(segments)
	if err != nil || len(all) == 0 {
		return loaded
	}
	return all
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

func (sm *SessionManager) SetSummary(key, summary string) {
	session := sm.GetOrCreate(key)
	session.mu.Lock()
	trimmed := strings.TrimSpace(summary)
	if session.Summary == trimmed {
		session.mu.Unlock()
		return
	}
	session.Summary = trimmed
	session.Updated = time.Now()
	_ = sm.persistSidecarsLocked(session)
	session.mu.Unlock()
	_ = sm.writeOpenClawSessionsIndex()
}

func (sm *SessionManager) ApplyCompaction(key string, keep []providers.Message, summary string) bool {
	session := sm.GetOrCreate(key)
	session.mu.Lock()
	applied := sm.applyCompactionLocked(session, keep, summary)
	session.mu.Unlock()
	if applied {
		_ = sm.writeOpenClawSessionsIndex()
	}
	return applied
}

func (sm *SessionManager) ApplyCompactionIfUnchanged(key string, baseNextSeq int, baseSummary string, keep []providers.Message, summary string) bool {
	session := sm.GetOrCreate(key)
	session.mu.Lock()
	if session.nextSeq != baseNextSeq || session.Summary != baseSummary {
		session.mu.Unlock()
		return false
	}
	applied := sm.applyCompactionLocked(session, keep, summary)
	session.mu.Unlock()
	if applied {
		_ = sm.writeOpenClawSessionsIndex()
	}
	return applied
}

func (sm *SessionManager) applyCompactionLocked(session *Session, keep []providers.Message, summary string) bool {
	if session == nil {
		return false
	}
	if len(session.Messages) == 0 && len(keep) == 0 {
		return false
	}
	session.Messages = append([]providers.Message(nil), keep...)
	if trimmed := strings.TrimSpace(summary); trimmed != "" {
		session.Summary = trimmed
	}
	session.CompactionCount++
	session.Updated = time.Now()
	if err := sm.persistSidecarsLocked(session); err != nil {
		return false
	}
	return true
}

func (sm *SessionManager) CompactSession(key string, keepLast int, note string) bool {
	session := sm.GetOrCreate(key)
	session.mu.Lock()
	if keepLast <= 0 || len(session.Messages) <= keepLast {
		session.mu.Unlock()
		return false
	}
	session.Messages = append([]providers.Message(nil), session.Messages[len(session.Messages)-keepLast:]...)
	session.CompactionCount++
	if trimmed := strings.TrimSpace(note); trimmed != "" {
		if strings.TrimSpace(session.Summary) == "" {
			session.Summary = trimmed
		} else {
			session.Summary += "\n" + trimmed
		}
	}
	session.Updated = time.Now()
	err := sm.persistSidecarsLocked(session)
	session.mu.Unlock()
	if err == nil {
		_ = sm.writeOpenClawSessionsIndex()
	}
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

func (sm *SessionManager) CompactionSnapshot(key string) SessionCompactionSnapshot {
	sm.mu.RLock()
	session, ok := sm.sessions[key]
	sm.mu.RUnlock()
	if !ok || session == nil {
		return SessionCompactionSnapshot{Key: key}
	}
	session.mu.RLock()
	defer session.mu.RUnlock()
	history := make([]providers.Message, len(session.Messages))
	copy(history, session.Messages)
	return SessionCompactionSnapshot{
		Key:     key,
		History: history,
		Summary: session.Summary,
		NextSeq: session.nextSeq,
	}
}

func (sm *SessionManager) SetLastLanguage(key, lang string) {
	if strings.TrimSpace(lang) == "" {
		return
	}
	session := sm.GetOrCreate(key)
	session.mu.Lock()
	if session.LastLanguage == lang {
		session.mu.Unlock()
		return
	}
	session.LastLanguage = lang
	session.Updated = time.Now()
	_ = sm.persistSidecarsLocked(session)
	session.mu.Unlock()
	_ = sm.writeOpenClawSessionsIndex()
}

func (sm *SessionManager) SetPreferredLanguage(key, lang string) {
	if strings.TrimSpace(lang) == "" {
		return
	}
	session := sm.GetOrCreate(key)
	session.mu.Lock()
	if session.PreferredLanguage == lang {
		session.mu.Unlock()
		return
	}
	session.PreferredLanguage = lang
	session.Updated = time.Now()
	_ = sm.persistSidecarsLocked(session)
	session.mu.Unlock()
	_ = sm.writeOpenClawSessionsIndex()
}

func (sm *SessionManager) Save(session *Session) error {
	if sm.storage == "" || session == nil {
		return nil
	}
	session.mu.Lock()
	if err := sm.persistSidecarsLocked(session); err != nil {
		session.mu.Unlock()
		return err
	}
	session.mu.Unlock()
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
	sort.Strings(keys)
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
	sort.Slice(items, func(i, j int) bool { return items[i].Updated.After(items[j].Updated) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func (sm *SessionManager) Search(query string, kinds []string, excludeKey string, limit int) []SessionSearchResult {
	terms := tokenizeQueryText(query)
	if len(terms) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 5
	}
	kindSet := make(map[string]struct{}, len(kinds))
	for _, item := range kinds {
		if v := strings.ToLower(strings.TrimSpace(item)); v != "" {
			kindSet[v] = struct{}{}
		}
	}

	sm.mu.RLock()
	keys := make([]string, 0, len(sm.sessions))
	for key := range sm.sessions {
		keys = append(keys, key)
	}
	sm.mu.RUnlock()
	sort.Strings(keys)

	results := make([]SessionSearchResult, 0, len(keys))
	for _, key := range keys {
		if key == strings.TrimSpace(excludeKey) {
			continue
		}
		sm.mu.RLock()
		session := sm.sessions[key]
		sm.mu.RUnlock()
		if session == nil {
			continue
		}
		session.mu.RLock()
		kind := session.Kind
		summary := session.Summary
		updated := session.Updated
		index := cloneSessionIndex(session.index)
		session.mu.RUnlock()
		if len(kindSet) > 0 {
			if _, ok := kindSet[strings.ToLower(strings.TrimSpace(kind))]; !ok {
				continue
			}
		}

		if index != nil {
			if result, ok := searchSessionIndex(key, kind, summary, updated, terms, index); ok {
				results = append(results, result)
				continue
			}
		}
		indexPath := sm.sessionIndexPath(key)
		if index, err := sm.readIndexFile(indexPath); err == nil {
			if result, ok := searchSessionIndex(key, kind, summary, updated, terms, index); ok {
				results = append(results, result)
				continue
			}
		}
		if result, ok := sm.searchSessionByScan(key, kind, summary, updated, terms); ok {
			results = append(results, result)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if !results[i].UpdatedAt.Equal(results[j].UpdatedAt) {
			return results[i].UpdatedAt.After(results[j].UpdatedAt)
		}
		return results[i].Key < results[j].Key
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results
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
	index := map[string]sessionsIndexEntry{}
	for key, s := range sm.sessions {
		s.mu.RLock()
		sid := strings.TrimSpace(s.SessionID)
		if sid == "" {
			sid = deriveSessionID(key)
		}
		entry := sessionsIndexEntry{
			SessionID:         sid,
			SessionKey:        key,
			UpdatedAt:         s.Updated.UnixMilli(),
			ChatType:          mapKindToChatType(s.Kind),
			Kind:              s.Kind,
			CompactionCount:   s.CompactionCount,
			SessionFile:       filepath.Join(sm.storage, activeSegmentFilename(key)),
			Summary:           s.Summary,
			LastLanguage:      s.LastLanguage,
			PreferredLanguage: s.PreferredLanguage,
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
	case "cron", "subagent", "hook":
		return "internal"
	default:
		return "unknown"
	}
}

func (sm *SessionManager) loadSessions() error {
	fallbackIndex := sm.readSessionsIndex()
	keys, err := sm.discoverSessionKeys()
	if err != nil {
		return err
	}
	for _, key := range keys {
		session, loadErr := sm.loadSessionFromDisk(key, fallbackIndex[key])
		if loadErr != nil {
			return loadErr
		}
		if session == nil {
			continue
		}
		sm.sessions[key] = session
	}
	return sm.writeOpenClawSessionsIndex()
}

func (sm *SessionManager) readSessionsIndex() map[string]sessionsIndexEntry {
	if sm.storage == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(sm.storage, "sessions.json"))
	if err != nil {
		return nil
	}
	var index map[string]sessionsIndexEntry
	if err := json.Unmarshal(data, &index); err != nil {
		return nil
	}
	return index
}

func (sm *SessionManager) loadSessionFromDisk(key string, fallback sessionsIndexEntry) (*Session, error) {
	meta, err := sm.loadOrRebuildMeta(key)
	if err != nil {
		return nil, err
	}
	index, indexErr := sm.readIndexFile(sm.sessionIndexPath(key))
	if indexErr != nil {
		index = nil
	}
	if meta == nil {
		if strings.TrimSpace(fallback.SessionKey) == "" && key == "" {
			return nil, nil
		}
		now := time.UnixMilli(fallback.UpdatedAt)
		if fallback.UpdatedAt <= 0 {
			now = time.Now()
		}
		return &Session{
			Key:               key,
			SessionID:         firstNonEmpty(fallback.SessionID, deriveSessionID(key)),
			Kind:              firstNonEmpty(fallback.Kind, detectSessionKind(key)),
			Summary:           fallback.Summary,
			CompactionCount:   fallback.CompactionCount,
			LastLanguage:      fallback.LastLanguage,
			PreferredLanguage: fallback.PreferredLanguage,
			Created:           now,
			Updated:           now,
			nextSeq:           1,
			index:             index,
		}, nil
	}

	workingMessages, err := sm.loadWorkingSet(meta.Segments)
	if err != nil {
		return nil, err
	}
	created := time.UnixMilli(meta.CreatedAt)
	if meta.CreatedAt <= 0 {
		created = time.Now()
	}
	updated := time.UnixMilli(meta.UpdatedAt)
	if meta.UpdatedAt <= 0 {
		updated = created
	}
	if strings.TrimSpace(meta.Summary) == "" && strings.TrimSpace(fallback.Summary) != "" {
		meta.Summary = fallback.Summary
	}
	if strings.TrimSpace(meta.LastLanguage) == "" && strings.TrimSpace(fallback.LastLanguage) != "" {
		meta.LastLanguage = fallback.LastLanguage
	}
	if strings.TrimSpace(meta.PreferredLanguage) == "" && strings.TrimSpace(fallback.PreferredLanguage) != "" {
		meta.PreferredLanguage = fallback.PreferredLanguage
	}
	return &Session{
		Key:               key,
		SessionID:         firstNonEmpty(meta.SessionID, fallback.SessionID, deriveSessionID(key)),
		Kind:              firstNonEmpty(meta.Kind, fallback.Kind, detectSessionKind(key)),
		Messages:          workingMessages,
		Summary:           firstNonEmpty(meta.Summary, fallback.Summary),
		CompactionCount:   maxInt(meta.CompactionCount, fallback.CompactionCount),
		LastLanguage:      firstNonEmpty(meta.LastLanguage, fallback.LastLanguage),
		PreferredLanguage: firstNonEmpty(meta.PreferredLanguage, fallback.PreferredLanguage),
		Created:           created,
		Updated:           updated,
		segments:          append([]sessionSegmentMeta(nil), meta.Segments...),
		nextSeq:           maxInt(meta.NextSeq, meta.MessageCount+1, 1),
		index:             index,
	}, nil
}

func (sm *SessionManager) loadOrRebuildMeta(key string) (*sessionMetaFile, error) {
	metaPath := sm.sessionMetaPath(key)
	indexPath := sm.sessionIndexPath(key)

	var meta sessionMetaFile
	metaValid := false
	if err := jsonlog.ReadJSON(metaPath, &meta); err == nil && meta.Version > 0 {
		if sm.metaMatchesStorage(key, &meta) {
			metaValid = true
		}
	}
	if metaValid {
		if _, err := sm.readIndexFile(indexPath); err == nil {
			return &meta, nil
		}
	}
	rebuilt, err := sm.rebuildSidecars(key, &meta)
	if err != nil {
		return nil, err
	}
	return rebuilt, nil
}

func (sm *SessionManager) metaMatchesStorage(key string, meta *sessionMetaFile) bool {
	if meta == nil || len(meta.Segments) == 0 {
		return false
	}
	for _, segment := range meta.Segments {
		size, err := jsonlog.FileSize(filepath.Join(sm.storage, segment.Name))
		if err != nil {
			return false
		}
		if strings.EqualFold(segment.Name, activeSegmentFilename(key)) && size != segment.LastOffset {
			return false
		}
	}
	return true
}

func (sm *SessionManager) rebuildSidecars(key string, seed *sessionMetaFile) (*sessionMetaFile, error) {
	segments, err := sm.discoverSessionSegments(key)
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		return nil, nil
	}

	meta := &sessionMetaFile{
		Version:           1,
		SessionKey:        key,
		SessionID:         deriveSessionID(key),
		Kind:              detectSessionKind(key),
		Summary:           strings.TrimSpace(seedValue(seed, func(v *sessionMetaFile) string { return v.Summary })),
		CompactionCount:   seedInt(seed, func(v *sessionMetaFile) int { return v.CompactionCount }),
		LastLanguage:      seedValue(seed, func(v *sessionMetaFile) string { return v.LastLanguage }),
		PreferredLanguage: seedValue(seed, func(v *sessionMetaFile) string { return v.PreferredLanguage }),
		Segments:          make([]sessionSegmentMeta, 0, len(segments)),
	}
	index := sessionIndexFile{
		Version:    1,
		SessionKey: key,
		LastSeq:    0,
		LastOffset: 0,
		Segment:    "",
		UpdatedAt:  time.Now().UnixMilli(),
		Tokens:     map[string][]sessionIndexRef{},
	}

	now := time.Now().UnixMilli()
	seq := 0
	for _, name := range segments {
		fullPath := filepath.Join(sm.storage, name)
		size, err := jsonlog.FileSize(fullPath)
		if err != nil {
			return nil, err
		}
		seg := sessionSegmentMeta{
			Name:       name,
			Archived:   !strings.EqualFold(name, activeSegmentFilename(key)),
			LastOffset: size,
			UpdatedAt:  now,
		}
		if st, err := os.Stat(fullPath); err == nil {
			seg.UpdatedAt = st.ModTime().UnixMilli()
			if meta.CreatedAt == 0 || st.ModTime().UnixMilli() < meta.CreatedAt {
				meta.CreatedAt = st.ModTime().UnixMilli()
			}
			if st.ModTime().UnixMilli() > meta.UpdatedAt {
				meta.UpdatedAt = st.ModTime().UnixMilli()
			}
		}
		if err := jsonlog.Scan(fullPath, func(line []byte) error {
			msg, ok := fromJSONLLine(line)
			if !ok {
				return nil
			}
			seq++
			if seg.FirstSeq == 0 {
				seg.FirstSeq = seq
			}
			seg.LastSeq = seq
			seg.MessageCount++
			meta.MessageCount++
			appendTokens(index.Tokens, tokenizeIndexText(msg.Content), sessionIndexRef{
				Seq:     seq,
				Role:    strings.ToLower(strings.TrimSpace(msg.Role)),
				Segment: name,
				Snippet: messageSnippet(msg.Content),
			})
			return nil
		}); err != nil {
			return nil, err
		}
		meta.Segments = append(meta.Segments, seg)
	}
	if meta.CreatedAt == 0 {
		meta.CreatedAt = now
	}
	if meta.UpdatedAt == 0 {
		meta.UpdatedAt = now
	}
	meta.NextSeq = seq + 1
	if len(meta.Segments) > 0 {
		last := meta.Segments[len(meta.Segments)-1]
		index.LastSeq = last.LastSeq
		index.LastOffset = last.LastOffset
		index.Segment = last.Name
	}
	if err := sm.writeSidecarFiles(key, meta, &index); err != nil {
		return nil, err
	}
	return meta, nil
}

func (sm *SessionManager) loadWorkingSet(segments []sessionSegmentMeta) ([]providers.Message, error) {
	if len(segments) == 0 {
		return nil, nil
	}
	start := 0
	if sm.promptLoadSegments > 0 && len(segments) > sm.promptLoadSegments {
		start = len(segments) - sm.promptLoadSegments
	}
	return sm.loadMessagesForSegments(segments[start:])
}

func (sm *SessionManager) loadMessagesForSegments(segments []sessionSegmentMeta) ([]providers.Message, error) {
	out := make([]providers.Message, 0)
	for _, segment := range segments {
		path := filepath.Join(sm.storage, segment.Name)
		if err := jsonlog.Scan(path, func(line []byte) error {
			msg, ok := fromJSONLLine(line)
			if ok {
				out = append(out, msg)
			}
			return nil
		}); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
	}
	return out, nil
}

func (sm *SessionManager) GetHistoryWindow(key string, around, before, after, limit int) []providers.Message {
	sm.mu.RLock()
	session, ok := sm.sessions[key]
	sm.mu.RUnlock()
	if !ok {
		return nil
	}
	session.mu.RLock()
	segments := append([]sessionSegmentMeta(nil), session.segments...)
	session.mu.RUnlock()
	if len(segments) == 0 {
		return nil
	}
	startSeq, endSeq := computeHistorySeqWindow(segments, around, before, after, limit)
	if endSeq < startSeq {
		return nil
	}
	selected := make([]sessionSegmentMeta, 0, len(segments))
	for _, segment := range segments {
		if segment.LastSeq < startSeq || segment.FirstSeq > endSeq {
			continue
		}
		selected = append(selected, segment)
	}
	if len(selected) == 0 {
		return nil
	}
	all, err := sm.loadMessagesForSegments(selected)
	if err != nil {
		return nil
	}
	out := make([]providers.Message, 0, len(all))
	seq := 0
	for _, segment := range selected {
		for i := 0; i < segment.MessageCount && seq < len(all); i++ {
			currentSeq := segment.FirstSeq + i
			msg := all[seq]
			seq++
			if currentSeq < startSeq || currentSeq > endSeq {
				continue
			}
			out = append(out, msg)
		}
	}
	return out
}

func (sm *SessionManager) appendMessageLocked(session *Session, msg providers.Message) (appendMessageResult, error) {
	if sm.storage == "" {
		return appendMessageResult{}, nil
	}
	if err := sm.ensureActiveSegmentLocked(session); err != nil {
		return appendMessageResult{}, err
	}
	result := appendMessageResult{}
	if sm.shouldRolloverLocked(session) {
		if err := sm.rolloverLocked(session); err != nil {
			return appendMessageResult{}, err
		}
		result.refreshSessionsIndex = true
		if err := sm.ensureActiveSegmentLocked(session); err != nil {
			return appendMessageResult{}, err
		}
	}
	active := sm.activeSegmentLocked(session)
	if active == nil {
		return appendMessageResult{}, fmt.Errorf("active session segment unavailable")
	}
	offset, err := jsonlog.AppendLine(filepath.Join(sm.storage, active.Name), toOpenClawMessageEvent(msg))
	if err != nil {
		return appendMessageResult{}, err
	}
	if active.FirstSeq == 0 {
		active.FirstSeq = session.nextSeq
	}
	active.LastSeq = session.nextSeq
	active.MessageCount++
	active.LastOffset = offset
	active.UpdatedAt = time.Now().UnixMilli()
	sm.appendIndexLocked(session, msg, active.Name, active.LastOffset)
	session.nextSeq++
	return result, sm.persistSidecarsLocked(session)
}

func (sm *SessionManager) ensureActiveSegmentLocked(session *Session) error {
	if session == nil {
		return nil
	}
	for i := range session.segments {
		if strings.EqualFold(session.segments[i].Name, activeSegmentFilename(session.Key)) {
			return nil
		}
	}
	session.segments = append(session.segments, sessionSegmentMeta{
		Name:       activeSegmentFilename(session.Key),
		Archived:   false,
		LastOffset: 0,
		UpdatedAt:  time.Now().UnixMilli(),
	})
	return nil
}

func (sm *SessionManager) activeSegmentLocked(session *Session) *sessionSegmentMeta {
	if session == nil {
		return nil
	}
	for i := range session.segments {
		if strings.EqualFold(session.segments[i].Name, activeSegmentFilename(session.Key)) {
			return &session.segments[i]
		}
	}
	return nil
}

func (sm *SessionManager) shouldRolloverLocked(session *Session) bool {
	active := sm.activeSegmentLocked(session)
	if active == nil {
		return false
	}
	if sm.segmentMaxMessages > 0 && active.MessageCount >= sm.segmentMaxMessages {
		return true
	}
	if sm.segmentMaxBytes > 0 && active.LastOffset >= sm.segmentMaxBytes {
		return true
	}
	return false
}

func (sm *SessionManager) rolloverLocked(session *Session) error {
	active := sm.activeSegmentLocked(session)
	if active == nil || active.MessageCount == 0 {
		return nil
	}
	nextSeq := 1
	for _, segment := range session.segments {
		if seq, ok := parseArchiveSegmentFilename(segment.Name, session.Key); ok && seq >= nextSeq {
			nextSeq = seq + 1
		}
	}
	archiveName := archivedSegmentFilename(session.Key, nextSeq)
	if err := os.Rename(filepath.Join(sm.storage, active.Name), filepath.Join(sm.storage, archiveName)); err != nil {
		return err
	}
	active.Name = archiveName
	active.Archived = true
	if err := sm.ensureActiveSegmentLocked(session); err != nil {
		return err
	}
	newActive := sm.activeSegmentLocked(session)
	if newActive != nil {
		newActive.Archived = false
		newActive.FirstSeq = 0
		newActive.LastSeq = 0
		newActive.MessageCount = 0
		newActive.LastOffset = 0
		newActive.UpdatedAt = time.Now().UnixMilli()
	}
	rebuilt, err := sm.rebuildSidecars(session.Key, &sessionMetaFile{
		Summary:           session.Summary,
		CompactionCount:   session.CompactionCount,
		LastLanguage:      session.LastLanguage,
		PreferredLanguage: session.PreferredLanguage,
	})
	if err != nil {
		return err
	}
	if rebuilt != nil {
		session.segments = append([]sessionSegmentMeta(nil), rebuilt.Segments...)
		session.nextSeq = maxInt(rebuilt.NextSeq, session.nextSeq)
	}
	return nil
}

func (sm *SessionManager) persistSidecarsLocked(session *Session) error {
	if sm.storage == "" || session == nil {
		return nil
	}
	meta := sessionMetaFile{
		Version:           1,
		SessionKey:        session.Key,
		SessionID:         firstNonEmpty(session.SessionID, deriveSessionID(session.Key)),
		Kind:              firstNonEmpty(session.Kind, detectSessionKind(session.Key)),
		Summary:           strings.TrimSpace(session.Summary),
		CompactionCount:   session.CompactionCount,
		LastLanguage:      strings.TrimSpace(session.LastLanguage),
		PreferredLanguage: strings.TrimSpace(session.PreferredLanguage),
		MessageCount:      sessionMessageCount(session),
		CreatedAt:         session.Created.UnixMilli(),
		UpdatedAt:         session.Updated.UnixMilli(),
		NextSeq:           maxInt(session.nextSeq, sessionMessageCount(session)+1),
		Segments:          append([]sessionSegmentMeta(nil), session.segments...),
	}
	if session.index == nil {
		index, err := sm.buildIndexForSessionLocked(session, meta.NextSeq-1)
		if err != nil {
			return err
		}
		session.index = index
	}
	return sm.writeSidecarFiles(session.Key, &meta, session.index)
}

func (sm *SessionManager) buildIndexForSessionLocked(session *Session, seqEnd int) (*sessionIndexFile, error) {
	messages, err := sm.loadMessagesForSegments(session.segments)
	if err != nil {
		return nil, err
	}
	index := &sessionIndexFile{
		Version:    1,
		SessionKey: session.Key,
		LastSeq:    0,
		LastOffset: 0,
		Segment:    "",
		UpdatedAt:  time.Now().UnixMilli(),
		Tokens:     map[string][]sessionIndexRef{},
	}
	for i, msg := range messages {
		ref := sessionIndexRef{
			Seq:     i + 1,
			Role:    strings.ToLower(strings.TrimSpace(msg.Role)),
			Segment: segmentNameForSeq(session.segments, i+1),
			Snippet: messageSnippet(msg.Content),
		}
		appendTokens(index.Tokens, tokenizeIndexText(msg.Content), ref)
	}
	index.LastSeq = seqEnd
	if active := sm.activeSegmentLocked(session); active != nil {
		index.LastOffset = active.LastOffset
		index.Segment = active.Name
	}
	return index, nil
}

func segmentNameForSeq(segments []sessionSegmentMeta, seq int) string {
	for _, segment := range segments {
		if seq >= segment.FirstSeq && seq <= segment.LastSeq {
			return segment.Name
		}
	}
	return ""
}

func sessionMessageCount(session *Session) int {
	count := 0
	for _, segment := range session.segments {
		count += segment.MessageCount
	}
	if count == 0 && len(session.Messages) > 0 {
		count = len(session.Messages)
	}
	return count
}

func (sm *SessionManager) writeSidecarFiles(key string, meta *sessionMetaFile, index *sessionIndexFile) error {
	if err := jsonlog.WriteJSON(sm.sessionMetaPath(key), meta); err != nil {
		return err
	}
	return jsonlog.WriteJSON(sm.sessionIndexPath(key), index)
}

func (sm *SessionManager) readIndexFile(path string) (*sessionIndexFile, error) {
	var index sessionIndexFile
	if err := jsonlog.ReadJSON(path, &index); err != nil {
		return nil, err
	}
	if index.Version <= 0 {
		return nil, fmt.Errorf("invalid index version")
	}
	return &index, nil
}

func (sm *SessionManager) searchSessionByScan(key, kind, summary string, updated time.Time, terms []string) (SessionSearchResult, bool) {
	session := SessionSearchResult{
		Key:       key,
		Kind:      kind,
		Summary:   summary,
		UpdatedAt: updated,
	}
	all, err := sm.loadMessagesForSegments(sm.sessionSegments(key))
	if err != nil {
		return SessionSearchResult{}, false
	}
	type scored struct {
		score   int
		snippet SessionSearchSnippet
	}
	matches := make([]scored, 0)
	for idx, msg := range all {
		score := messageMatchScore(msg, terms)
		if score == 0 {
			continue
		}
		matches = append(matches, scored{
			score: score,
			snippet: SessionSearchSnippet{
				Seq:     idx + 1,
				Role:    strings.ToLower(strings.TrimSpace(msg.Role)),
				Content: messageSnippet(msg.Content),
			},
		})
	}
	if len(matches) == 0 {
		return SessionSearchResult{}, false
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].snippet.Seq > matches[j].snippet.Seq
	})
	for i, item := range matches {
		if i >= maxSearchSnippetsPerSession {
			break
		}
		session.Score += item.score
		session.Snippets = append(session.Snippets, item.snippet)
	}
	return session, true
}

func (sm *SessionManager) sessionSegments(key string) []sessionSegmentMeta {
	sm.mu.RLock()
	session := sm.sessions[key]
	sm.mu.RUnlock()
	if session == nil {
		return nil
	}
	session.mu.RLock()
	defer session.mu.RUnlock()
	return append([]sessionSegmentMeta(nil), session.segments...)
}

func searchSessionIndex(key, kind, summary string, updated time.Time, terms []string, index *sessionIndexFile) (SessionSearchResult, bool) {
	type aggregate struct {
		score int
		ref   sessionIndexRef
	}
	hits := map[int]*aggregate{}
	for _, term := range terms {
		for _, ref := range index.Tokens[term] {
			item := hits[ref.Seq]
			if item == nil {
				item = &aggregate{ref: ref}
				hits[ref.Seq] = item
			}
			item.score++
		}
	}
	if len(hits) == 0 {
		return SessionSearchResult{}, false
	}
	aggs := make([]aggregate, 0, len(hits))
	for _, item := range hits {
		aggs = append(aggs, *item)
	}
	sort.Slice(aggs, func(i, j int) bool {
		if aggs[i].score != aggs[j].score {
			return aggs[i].score > aggs[j].score
		}
		return aggs[i].ref.Seq > aggs[j].ref.Seq
	})
	result := SessionSearchResult{
		Key:       key,
		Kind:      kind,
		Summary:   summary,
		UpdatedAt: updated,
	}
	for i, agg := range aggs {
		if i >= maxSearchSnippetsPerSession {
			break
		}
		result.Score += agg.score
		result.Snippets = append(result.Snippets, SessionSearchSnippet{
			Seq:     agg.ref.Seq,
			Role:    agg.ref.Role,
			Segment: agg.ref.Segment,
			Content: agg.ref.Snippet,
		})
	}
	return result, true
}

func appendTokens(dst map[string][]sessionIndexRef, tokens []string, ref sessionIndexRef) {
	for _, token := range tokens {
		items := dst[token]
		if len(items) >= maxTokenRefsPerSessionToken {
			continue
		}
		items = append(items, ref)
		dst[token] = items
	}
}

func (sm *SessionManager) appendIndexLocked(session *Session, msg providers.Message, segment string, offset int64) {
	if session == nil {
		return
	}
	if session.index == nil {
		session.index = &sessionIndexFile{
			Version:    1,
			SessionKey: session.Key,
			Tokens:     map[string][]sessionIndexRef{},
		}
	}
	if session.index.Tokens == nil {
		session.index.Tokens = map[string][]sessionIndexRef{}
	}
	ref := sessionIndexRef{
		Seq:     session.nextSeq,
		Role:    strings.ToLower(strings.TrimSpace(msg.Role)),
		Segment: segment,
		Snippet: messageSnippet(msg.Content),
	}
	appendTokens(session.index.Tokens, tokenizeIndexText(msg.Content), ref)
	session.index.LastSeq = session.nextSeq
	session.index.LastOffset = offset
	session.index.Segment = segment
	session.index.UpdatedAt = time.Now().UnixMilli()
}

func messageMatchScore(msg providers.Message, terms []string) int {
	if len(terms) == 0 {
		return 0
	}
	content := strings.ToLower(msg.Content)
	score := 0
	for _, term := range terms {
		if strings.Contains(content, term) {
			score++
		}
	}
	return score
}

func messageSnippet(content string) string {
	content = strings.TrimSpace(strings.ReplaceAll(content, "\n", " "))
	if len(content) <= 180 {
		return content
	}
	return content[:180] + "..."
}

func tokenizeIndexText(text string) []string {
	return tokenizeSearchText(text, false)
}

func tokenizeQueryText(text string) []string {
	return tokenizeSearchText(text, true)
}

func tokenizeSearchText(text string, includeSingleHan bool) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil
	}
	out := make([]string, 0, 16)
	seen := map[string]struct{}{}
	var asciiBuf strings.Builder
	flushASCII := func() {
		if asciiBuf.Len() == 0 {
			return
		}
		fields := strings.FieldsFunc(asciiBuf.String(), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		})
		for _, field := range fields {
			field = strings.TrimSpace(field)
			if len(field) < 2 {
				continue
			}
			if _, ok := seen[field]; ok {
				continue
			}
			seen[field] = struct{}{}
			out = append(out, field)
		}
		asciiBuf.Reset()
	}
	var hanRunes []rune
	flushHan := func() {
		if len(hanRunes) == 0 {
			return
		}
		if includeSingleHan && len(hanRunes) == 1 {
			token := string(hanRunes[0])
			if _, ok := seen[token]; !ok {
				seen[token] = struct{}{}
				out = append(out, token)
			}
		}
		if len(hanRunes) >= 2 {
			for i := 0; i < len(hanRunes)-1; i++ {
				token := string(hanRunes[i : i+2])
				if _, ok := seen[token]; ok {
					continue
				}
				seen[token] = struct{}{}
				out = append(out, token)
			}
		}
		hanRunes = hanRunes[:0]
	}
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			flushASCII()
			hanRunes = append(hanRunes, r)
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			flushHan()
			asciiBuf.WriteRune(r)
		default:
			flushASCII()
			flushHan()
		}
	}
	flushASCII()
	flushHan()
	return out
}

func (sm *SessionManager) discoverSessionKeys() ([]string, error) {
	if sm.storage == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(sm.storage)
	if err != nil {
		return nil, err
	}
	keys := map[string]struct{}{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if key, ok := sessionKeyFromFilename(entry.Name()); ok {
			keys[key] = struct{}{}
		}
	}
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out, nil
}

func (sm *SessionManager) discoverSessionSegments(key string) ([]string, error) {
	entries, err := os.ReadDir(sm.storage)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		switch {
		case name == activeSegmentFilename(key):
			names = append(names, name)
		case name == legacySegmentFilename(key):
			names = append(names, name)
		default:
			if _, ok := parseArchiveSegmentFilename(name, key); ok {
				names = append(names, name)
			}
		}
	}
	sort.Slice(names, func(i, j int) bool {
		return compareSegmentNames(key, names[i], names[j])
	})
	return names, nil
}

func compareSegmentNames(key, left, right string) bool {
	if left == right {
		return false
	}
	if left == legacySegmentFilename(key) {
		return true
	}
	if right == legacySegmentFilename(key) {
		return false
	}
	if left == activeSegmentFilename(key) {
		return false
	}
	if right == activeSegmentFilename(key) {
		return true
	}
	li, _ := parseArchiveSegmentFilename(left, key)
	ri, _ := parseArchiveSegmentFilename(right, key)
	return li < ri
}

func sessionKeyFromFilename(name string) (string, bool) {
	switch {
	case strings.HasSuffix(name, ".meta.json"):
		return strings.TrimSuffix(name, ".meta.json"), true
	case strings.HasSuffix(name, ".index.json"):
		return strings.TrimSuffix(name, ".index.json"), true
	case strings.HasSuffix(name, ".active.jsonl"):
		return strings.TrimSuffix(name, ".active.jsonl"), true
	case strings.HasSuffix(name, ".jsonl") && !strings.Contains(name, ".deleted."):
		if m := archiveSegmentRe.FindStringSubmatch(name); len(m) == 3 {
			return m[1], true
		}
		return strings.TrimSuffix(name, ".jsonl"), true
	default:
		return "", false
	}
}

func parseArchiveSegmentFilename(name, key string) (int, bool) {
	m := archiveSegmentRe.FindStringSubmatch(name)
	if len(m) != 3 || m[1] != key {
		return 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return 0, false
	}
	return n, true
}

func activeSegmentFilename(key string) string { return key + ".active.jsonl" }
func legacySegmentFilename(key string) string { return key + ".jsonl" }
func archivedSegmentFilename(key string, seq int) string {
	return fmt.Sprintf("%s.%04d.jsonl", key, seq)
}

func (sm *SessionManager) sessionMetaPath(key string) string {
	return filepath.Join(sm.storage, key+".meta.json")
}
func (sm *SessionManager) sessionIndexPath(key string) string {
	return filepath.Join(sm.storage, key+".index.json")
}

func readPositiveIntEnv(name string, fallback int) int {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func seedValue(seed *sessionMetaFile, get func(*sessionMetaFile) string) string {
	if seed == nil {
		return ""
	}
	return get(seed)
}

func seedInt(seed *sessionMetaFile, get func(*sessionMetaFile) int) int {
	if seed == nil {
		return 0
	}
	return get(seed)
}

func maxInt(values ...int) int {
	best := 0
	for _, value := range values {
		if value > best {
			best = value
		}
	}
	return best
}

func cloneSessionIndex(index *sessionIndexFile) *sessionIndexFile {
	if index == nil {
		return nil
	}
	out := &sessionIndexFile{
		Version:    index.Version,
		SessionKey: index.SessionKey,
		LastSeq:    index.LastSeq,
		LastOffset: index.LastOffset,
		Segment:    index.Segment,
		UpdatedAt:  index.UpdatedAt,
		Tokens:     make(map[string][]sessionIndexRef, len(index.Tokens)),
	}
	for key, refs := range index.Tokens {
		out.Tokens[key] = append([]sessionIndexRef(nil), refs...)
	}
	return out
}

func computeHistorySeqWindow(segments []sessionSegmentMeta, around, before, after, limit int) (int, int) {
	total := 0
	for _, segment := range segments {
		if segment.LastSeq > total {
			total = segment.LastSeq
		}
	}
	if total <= 0 {
		return 1, 0
	}
	if limit <= 0 {
		limit = 50
	}
	start := 1
	end := total
	if around > 0 {
		half := limit / 2
		if half < 1 {
			half = 1
		}
		start = around - half
		end = around + half
		if start < 1 {
			start = 1
		}
		if end > total {
			end = total
		}
	} else {
		if after > 0 {
			start = after + 1
		}
		if before > 0 {
			end = before - 1
		}
	}
	if start < 1 {
		start = 1
	}
	if end > total {
		end = total
	}
	if end < start {
		return 1, 0
	}
	if end-start+1 > limit {
		start = end - limit + 1
		if start < 1 {
			start = 1
		}
	}
	return start, end
}
