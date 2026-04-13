package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YspCoder/clawgo/pkg/jsonlog"
)

type AgentThread struct {
	ThreadID     string   `json:"thread_id"`
	Owner        string   `json:"owner"`
	Participants []string `json:"participants,omitempty"`
	Status       string   `json:"status"`
	Topic        string   `json:"topic,omitempty"`
	CreatedAt    int64    `json:"created_at"`
	UpdatedAt    int64    `json:"updated_at"`
}

type AgentMessage struct {
	MessageID     string `json:"message_id"`
	ThreadID      string `json:"thread_id"`
	FromAgent     string `json:"from_agent"`
	ToAgent       string `json:"to_agent"`
	ReplyTo       string `json:"reply_to,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	Type          string `json:"type"`
	Content       string `json:"content"`
	RequiresReply bool   `json:"requires_reply,omitempty"`
	Status        string `json:"status"`
	CreatedAt     int64  `json:"created_at"`
}

type mailboxThreadsMetaFile struct {
	Version    int           `json:"version"`
	LastOffset int64         `json:"last_offset,omitempty"`
	ThreadSeq  int           `json:"thread_seq,omitempty"`
	UpdatedAt  int64         `json:"updated_at,omitempty"`
	Threads    []AgentThread `json:"threads,omitempty"`
}

type mailboxMessagesMetaFile struct {
	Version             int                 `json:"version"`
	LastOffset          int64               `json:"last_offset,omitempty"`
	MsgSeq              int                 `json:"msg_seq,omitempty"`
	UpdatedAt           int64               `json:"updated_at,omitempty"`
	Messages            []AgentMessage      `json:"messages,omitempty"`
	ThreadMessages      map[string][]string `json:"thread_messages,omitempty"`
	QueuedByAgent       map[string][]string `json:"queued_by_agent,omitempty"`
	QueuedByThreadAgent map[string][]string `json:"queued_by_thread_agent,omitempty"`
}

type AgentMailboxStore struct {
	dir                  string
	threadsPath          string
	msgsPath             string
	threadsMetaPath      string
	msgsMetaPath         string
	mu                   sync.RWMutex
	threads              map[string]*AgentThread
	messages             map[string]*AgentMessage
	threadMessages       map[string][]string
	queuedByAgent        map[string][]string
	queuedByThreadAgent  map[string][]string
	msgSeq               int
	threadSeq            int
}

func NewAgentMailboxStore(workspace string) *AgentMailboxStore {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil
	}
	dir := filepath.Join(workspace, "agents", "runtime")
	s := &AgentMailboxStore{
		dir:                 dir,
		threadsPath:         filepath.Join(dir, "threads.jsonl"),
		msgsPath:            filepath.Join(dir, "agent_messages.jsonl"),
		threadsMetaPath:     filepath.Join(dir, "threads.meta.json"),
		msgsMetaPath:        filepath.Join(dir, "agent_messages.meta.json"),
		threads:             map[string]*AgentThread{},
		messages:            map[string]*AgentMessage{},
		threadMessages:      map[string][]string{},
		queuedByAgent:       map[string][]string{},
		queuedByThreadAgent: map[string][]string{},
	}
	_ = os.MkdirAll(dir, 0o755)
	_ = s.load()
	return s
}

func (s *AgentMailboxStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetLocked()
	if err := s.loadThreadsLocked(); err != nil {
		return err
	}
	return s.loadMessagesLocked()
}

func (s *AgentMailboxStore) resetLocked() {
	s.threads = map[string]*AgentThread{}
	s.messages = map[string]*AgentMessage{}
	s.threadMessages = map[string][]string{}
	s.queuedByAgent = map[string][]string{}
	s.queuedByThreadAgent = map[string][]string{}
	s.msgSeq = 0
	s.threadSeq = 0
}

func (s *AgentMailboxStore) loadThreadsLocked() error {
	size, err := jsonlog.FileSize(s.threadsPath)
	if err != nil {
		return err
	}
	var meta mailboxThreadsMetaFile
	if err := jsonlog.ReadJSON(s.threadsMetaPath, &meta); err == nil && meta.Version > 0 && meta.LastOffset == size {
		for _, thread := range meta.Threads {
			cp := thread
			cp.Participants = append([]string(nil), thread.Participants...)
			s.threads[cp.ThreadID] = &cp
		}
		s.threadSeq = meta.ThreadSeq
		return nil
	}
	if size == 0 {
		return s.persistThreadsMetaLocked(size)
	}
	if err := jsonlog.Scan(s.threadsPath, func(line []byte) error {
		var thread AgentThread
		if err := json.Unmarshal(line, &thread); err != nil || strings.TrimSpace(thread.ThreadID) == "" {
			return nil
		}
		cp := thread
		cp.Participants = append([]string(nil), thread.Participants...)
		s.threads[thread.ThreadID] = &cp
		if n := parseThreadSequence(thread.ThreadID); n > s.threadSeq {
			s.threadSeq = n
		}
		return nil
	}); err != nil {
		return err
	}
	return s.persistThreadsMetaLocked(size)
}

func (s *AgentMailboxStore) loadMessagesLocked() error {
	size, err := jsonlog.FileSize(s.msgsPath)
	if err != nil {
		return err
	}
	var meta mailboxMessagesMetaFile
	if err := jsonlog.ReadJSON(s.msgsMetaPath, &meta); err == nil && meta.Version > 0 && meta.LastOffset == size {
		for _, msg := range meta.Messages {
			cp := msg
			s.messages[msg.MessageID] = &cp
		}
		s.threadMessages = cloneStringSliceMap(meta.ThreadMessages)
		s.queuedByAgent = cloneStringSliceMap(meta.QueuedByAgent)
		s.queuedByThreadAgent = cloneStringSliceMap(meta.QueuedByThreadAgent)
		s.msgSeq = meta.MsgSeq
		s.reconcileThreadsFromMessagesLocked()
		return nil
	}
	if size == 0 {
		return s.persistMessagesMetaLocked(size)
	}
	if err := jsonlog.Scan(s.msgsPath, func(line []byte) error {
		var msg AgentMessage
		if err := json.Unmarshal(line, &msg); err != nil || strings.TrimSpace(msg.MessageID) == "" {
			return nil
		}
		s.indexMessageLocked(msg)
		return nil
	}); err != nil {
		return err
	}
	return s.persistMessagesMetaLocked(size)
}

func (s *AgentMailboxStore) EnsureThread(thread AgentThread) (AgentThread, error) {
	if s == nil {
		return thread, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return AgentThread{}, err
	}
	if strings.TrimSpace(thread.ThreadID) == "" {
		s.threadSeq++
		thread.ThreadID = fmt.Sprintf("thread-%04d", s.threadSeq)
	}
	thread.Participants = normalizeStringList(thread.Participants)
	if strings.TrimSpace(thread.Status) == "" {
		thread.Status = "open"
	}
	if thread.CreatedAt <= 0 {
		thread.CreatedAt = thread.UpdatedAt
	}
	if thread.CreatedAt <= 0 {
		thread.CreatedAt = 1
	}
	if thread.UpdatedAt <= 0 {
		thread.UpdatedAt = thread.CreatedAt
	}
	offset, err := jsonlog.AppendLine(s.threadsPath, thread)
	if err != nil {
		return AgentThread{}, err
	}
	cp := thread
	cp.Participants = append([]string(nil), thread.Participants...)
	s.threads[thread.ThreadID] = &cp
	if n := parseThreadSequence(thread.ThreadID); n > s.threadSeq {
		s.threadSeq = n
	}
	if err := s.persistThreadsMetaLocked(offset); err != nil {
		return AgentThread{}, err
	}
	return thread, nil
}

func (s *AgentMailboxStore) AppendMessage(msg AgentMessage) (AgentMessage, error) {
	if s == nil {
		return msg, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return AgentMessage{}, err
	}
	if strings.TrimSpace(msg.MessageID) == "" {
		s.msgSeq++
		msg.MessageID = fmt.Sprintf("msg-%06d", s.msgSeq)
	}
	if strings.TrimSpace(msg.Status) == "" {
		msg.Status = "queued"
	}
	offset, err := jsonlog.AppendLine(s.msgsPath, msg)
	if err != nil {
		return AgentMessage{}, err
	}
	s.indexMessageLocked(msg)
	if err := s.persistMessagesMetaLocked(offset); err != nil {
		return AgentMessage{}, err
	}
	if err := s.persistThreadsMetaLocked(0); err != nil {
		return AgentMessage{}, err
	}
	return msg, nil
}

func (s *AgentMailboxStore) Thread(threadID string) (*AgentThread, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	thread, ok := s.threads[strings.TrimSpace(threadID)]
	if !ok {
		return nil, false
	}
	cp := *thread
	cp.Participants = append([]string(nil), thread.Participants...)
	return &cp, true
}

func (s *AgentMailboxStore) MessagesByThread(threadID string, limit int) ([]AgentMessage, error) {
	if s == nil {
		return nil, nil
	}
	return s.currentIndexedMessages(s.threadMessages[strings.TrimSpace(threadID)], limit), nil
}

func (s *AgentMailboxStore) Inbox(agentID string, limit int) ([]AgentMessage, error) {
	if s == nil {
		return nil, nil
	}
	return s.currentIndexedMessages(s.queuedByAgent[strings.TrimSpace(agentID)], limit), nil
}

func (s *AgentMailboxStore) ThreadInbox(threadID, agentID string, limit int) ([]AgentMessage, error) {
	if s == nil {
		return nil, nil
	}
	return s.currentIndexedMessages(s.queuedByThreadAgent[mailboxThreadAgentKey(threadID, agentID)], limit), nil
}

func (s *AgentMailboxStore) Message(messageID string) (*AgentMessage, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	msg, ok := s.messages[strings.TrimSpace(messageID)]
	if !ok {
		return nil, false
	}
	cp := *msg
	return &cp, true
}

func (s *AgentMailboxStore) UpdateMessageStatus(messageID, status string, at int64) (*AgentMessage, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.RLock()
	current, ok := s.messages[strings.TrimSpace(messageID)]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("message not found: %s", messageID)
	}
	updated := *current
	updated.Status = strings.TrimSpace(status)
	if updated.Status == "" {
		updated.Status = current.Status
	}
	if at > 0 {
		updated.CreatedAt = at
	}
	msg, err := s.AppendMessage(updated)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

func (s *AgentMailboxStore) currentIndexedMessages(ids []string, limit int) []AgentMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AgentMessage, 0, len(ids))
	for _, id := range ids {
		msg := s.messages[id]
		if msg == nil {
			continue
		}
		out = append(out, *msg)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].MessageID < out[j].MessageID
	})
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func (s *AgentMailboxStore) indexMessageLocked(msg AgentMessage) {
	msg.MessageID = strings.TrimSpace(msg.MessageID)
	if msg.MessageID == "" {
		return
	}
	if n := parseMessageSequence(msg.MessageID); n > s.msgSeq {
		s.msgSeq = n
	}
	if existing := s.messages[msg.MessageID]; existing != nil {
		s.removeQueuedIndexesLocked(*existing)
	}
	cp := msg
	s.messages[msg.MessageID] = &cp
	s.threadMessages[msg.ThreadID] = appendUniqueString(s.threadMessages[msg.ThreadID], msg.MessageID)
	if strings.EqualFold(strings.TrimSpace(msg.Status), "queued") {
		s.queuedByAgent[msg.ToAgent] = appendUniqueString(s.queuedByAgent[msg.ToAgent], msg.MessageID)
		s.queuedByThreadAgent[mailboxThreadAgentKey(msg.ThreadID, msg.ToAgent)] = appendUniqueString(s.queuedByThreadAgent[mailboxThreadAgentKey(msg.ThreadID, msg.ToAgent)], msg.MessageID)
	}
	thread := s.ensureThreadStateLocked(msg.ThreadID)
	if thread != nil {
		if msg.CreatedAt > thread.UpdatedAt {
			thread.UpdatedAt = msg.CreatedAt
		}
		participants := append([]string(nil), thread.Participants...)
		participants = append(participants, msg.FromAgent, msg.ToAgent)
		thread.Participants = normalizeStringList(participants)
	}
}

func (s *AgentMailboxStore) ensureThreadStateLocked(threadID string) *AgentThread {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	thread := s.threads[threadID]
	if thread != nil {
		return thread
	}
	thread = &AgentThread{
		ThreadID:     threadID,
		Status:       "open",
		CreatedAt:    1,
		UpdatedAt:    1,
		Participants: nil,
	}
	s.threads[threadID] = thread
	if n := parseThreadSequence(threadID); n > s.threadSeq {
		s.threadSeq = n
	}
	return thread
}

func (s *AgentMailboxStore) removeQueuedIndexesLocked(msg AgentMessage) {
	if !strings.EqualFold(strings.TrimSpace(msg.Status), "queued") {
		return
	}
	s.queuedByAgent[msg.ToAgent] = removeStringValue(s.queuedByAgent[msg.ToAgent], msg.MessageID)
	s.queuedByThreadAgent[mailboxThreadAgentKey(msg.ThreadID, msg.ToAgent)] = removeStringValue(s.queuedByThreadAgent[mailboxThreadAgentKey(msg.ThreadID, msg.ToAgent)], msg.MessageID)
}

func (s *AgentMailboxStore) reconcileThreadsFromMessagesLocked() {
	for _, msg := range s.messages {
		s.ensureThreadStateLocked(msg.ThreadID)
	}
	for _, msg := range s.messages {
		thread := s.threads[msg.ThreadID]
		if thread == nil {
			continue
		}
		if msg.CreatedAt > thread.UpdatedAt {
			thread.UpdatedAt = msg.CreatedAt
		}
		participants := append([]string(nil), thread.Participants...)
		participants = append(participants, msg.FromAgent, msg.ToAgent)
		thread.Participants = normalizeStringList(participants)
	}
}

func (s *AgentMailboxStore) persistThreadsMetaLocked(offset int64) error {
	if offset <= 0 {
		size, err := jsonlog.FileSize(s.threadsPath)
		if err != nil {
			return err
		}
		offset = size
	}
	meta := mailboxThreadsMetaFile{
		Version:    1,
		LastOffset: offset,
		ThreadSeq:  s.threadSeq,
		UpdatedAt:  time.Now().UnixMilli(),
		Threads:    make([]AgentThread, 0, len(s.threads)),
	}
	for _, thread := range s.threads {
		cp := *thread
		cp.Participants = append([]string(nil), thread.Participants...)
		meta.Threads = append(meta.Threads, cp)
	}
	sort.Slice(meta.Threads, func(i, j int) bool {
		if meta.Threads[i].UpdatedAt != meta.Threads[j].UpdatedAt {
			return meta.Threads[i].UpdatedAt > meta.Threads[j].UpdatedAt
		}
		return meta.Threads[i].ThreadID < meta.Threads[j].ThreadID
	})
	return jsonlog.WriteJSON(s.threadsMetaPath, meta)
}

func (s *AgentMailboxStore) persistMessagesMetaLocked(offset int64) error {
	meta := mailboxMessagesMetaFile{
		Version:             1,
		LastOffset:          offset,
		MsgSeq:              s.msgSeq,
		UpdatedAt:           time.Now().UnixMilli(),
		Messages:            make([]AgentMessage, 0, len(s.messages)),
		ThreadMessages:      cloneStringSliceMap(s.threadMessages),
		QueuedByAgent:       cloneStringSliceMap(s.queuedByAgent),
		QueuedByThreadAgent: cloneStringSliceMap(s.queuedByThreadAgent),
	}
	for _, msg := range s.messages {
		meta.Messages = append(meta.Messages, *msg)
	}
	sort.Slice(meta.Messages, func(i, j int) bool {
		if meta.Messages[i].CreatedAt != meta.Messages[j].CreatedAt {
			return meta.Messages[i].CreatedAt < meta.Messages[j].CreatedAt
		}
		return meta.Messages[i].MessageID < meta.Messages[j].MessageID
	})
	return jsonlog.WriteJSON(s.msgsMetaPath, meta)
}

func parseThreadSequence(threadID string) int {
	threadID = strings.TrimSpace(threadID)
	if !strings.HasPrefix(threadID, "thread-") {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimPrefix(threadID, "thread-"))
	return n
}

func threadToThreadRecord(thread *AgentThread) ThreadRecord {
	if thread == nil {
		return ThreadRecord{}
	}
	return ThreadRecord{
		ID:           thread.ThreadID,
		OwnerAgentID: thread.Owner,
		Participants: append([]string(nil), thread.Participants...),
		Status:       thread.Status,
		Topic:        thread.Topic,
		CreatedAt:    thread.CreatedAt,
		UpdatedAt:    thread.UpdatedAt,
	}
}

func messageToArtifactRecord(msg AgentMessage) ArtifactRecord {
	agentID := strings.TrimSpace(msg.FromAgent)
	if agentID == "" {
		agentID = strings.TrimSpace(msg.ToAgent)
	}
	return ArtifactRecord{
		ID:            msg.MessageID,
		RunID:         msg.CorrelationID,
		RequestID:     msg.CorrelationID,
		ThreadID:      msg.ThreadID,
		Kind:          "message",
		Name:          msg.Type,
		Content:       msg.Content,
		AgentID:       agentID,
		FromAgent:     msg.FromAgent,
		ToAgent:       msg.ToAgent,
		ReplyTo:       msg.ReplyTo,
		CorrelationID: msg.CorrelationID,
		Status:        msg.Status,
		RequiresReply: msg.RequiresReply,
		CreatedAt:     msg.CreatedAt,
		Visible:       true,
		SourceType:    "agent_message",
	}
}

func parseMessageSequence(messageID string) int {
	messageID = strings.TrimSpace(messageID)
	if !strings.HasPrefix(messageID, "msg-") {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimPrefix(messageID, "msg-"))
	return n
}

func mailboxThreadAgentKey(threadID, agentID string) string {
	return strings.TrimSpace(threadID) + "::" + strings.TrimSpace(agentID)
}

func cloneStringSliceMap(src map[string][]string) map[string][]string {
	if len(src) == 0 {
		return map[string][]string{}
	}
	out := make(map[string][]string, len(src))
	for key, values := range src {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func removeStringValue(values []string, value string) []string {
	if len(values) == 0 {
		return values
	}
	out := values[:0]
	for _, existing := range values {
		if existing == value {
			continue
		}
		out = append(out, existing)
	}
	return append([]string(nil), out...)
}
