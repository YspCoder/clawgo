package tools

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

type AgentMailboxStore struct {
	dir         string
	threadsPath string
	msgsPath    string
	mu          sync.RWMutex
	threads     map[string]*AgentThread
	messages    map[string]*AgentMessage
	msgSeq      int
	threadSeq   int
}

func NewAgentMailboxStore(workspace string) *AgentMailboxStore {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil
	}
	dir := filepath.Join(workspace, "agents", "runtime")
	s := &AgentMailboxStore{
		dir:         dir,
		threadsPath: filepath.Join(dir, "threads.jsonl"),
		msgsPath:    filepath.Join(dir, "agent_messages.jsonl"),
		threads:     map[string]*AgentThread{},
		messages:    map[string]*AgentMessage{},
	}
	_ = os.MkdirAll(dir, 0755)
	_ = s.load()
	return s
}

func (s *AgentMailboxStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threads = map[string]*AgentThread{}
	s.messages = map[string]*AgentMessage{}
	if err := s.loadThreadsLocked(); err != nil {
		return err
	}
	return s.scanMessagesLocked()
}

func (s *AgentMailboxStore) loadThreadsLocked() error {
	f, err := os.Open(s.threadsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var thread AgentThread
		if err := json.Unmarshal([]byte(line), &thread); err != nil {
			continue
		}
		cp := thread
		s.threads[thread.ThreadID] = &cp
		if n := parseThreadSequence(thread.ThreadID); n > s.threadSeq {
			s.threadSeq = n
		}
	}
	return scanner.Err()
}

func (s *AgentMailboxStore) scanMessagesLocked() error {
	f, err := os.Open(s.msgsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg AgentMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if n := parseMessageSequence(msg.MessageID); n > s.msgSeq {
			s.msgSeq = n
		}
		cp := msg
		s.messages[msg.MessageID] = &cp
		if thread := s.threads[msg.ThreadID]; thread != nil && msg.CreatedAt > thread.UpdatedAt {
			thread.UpdatedAt = msg.CreatedAt
		}
	}
	return scanner.Err()
}

func (s *AgentMailboxStore) EnsureThread(thread AgentThread) (AgentThread, error) {
	if s == nil {
		return thread, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0755); err != nil {
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
	data, err := json.Marshal(thread)
	if err != nil {
		return AgentThread{}, err
	}
	f, err := os.OpenFile(s.threadsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return AgentThread{}, err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return AgentThread{}, err
	}
	cp := thread
	s.threads[thread.ThreadID] = &cp
	return thread, nil
}

func (s *AgentMailboxStore) AppendMessage(msg AgentMessage) (AgentMessage, error) {
	if s == nil {
		return msg, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return AgentMessage{}, err
	}
	if strings.TrimSpace(msg.MessageID) == "" {
		s.msgSeq++
		msg.MessageID = fmt.Sprintf("msg-%06d", s.msgSeq)
	}
	if strings.TrimSpace(msg.Status) == "" {
		msg.Status = "queued"
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return AgentMessage{}, err
	}
	f, err := os.OpenFile(s.msgsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return AgentMessage{}, err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return AgentMessage{}, err
	}
	if thread := s.threads[msg.ThreadID]; thread != nil {
		thread.UpdatedAt = msg.CreatedAt
		participants := append([]string(nil), thread.Participants...)
		participants = append(participants, msg.FromAgent, msg.ToAgent)
		thread.Participants = normalizeStringList(participants)
	}
	cp := msg
	s.messages[msg.MessageID] = &cp
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
	return s.currentMessages(func(msg AgentMessage) bool {
		return msg.ThreadID == strings.TrimSpace(threadID)
	}, limit), nil
}

func (s *AgentMailboxStore) Inbox(agentID string, limit int) ([]AgentMessage, error) {
	if s == nil {
		return nil, nil
	}
	agentID = strings.TrimSpace(agentID)
	return s.currentMessages(func(msg AgentMessage) bool {
		return msg.ToAgent == agentID && strings.EqualFold(strings.TrimSpace(msg.Status), "queued")
	}, limit), nil
}

func (s *AgentMailboxStore) ThreadInbox(threadID, agentID string, limit int) ([]AgentMessage, error) {
	if s == nil {
		return nil, nil
	}
	threadID = strings.TrimSpace(threadID)
	agentID = strings.TrimSpace(agentID)
	return s.currentMessages(func(msg AgentMessage) bool {
		return msg.ThreadID == threadID && msg.ToAgent == agentID && strings.EqualFold(strings.TrimSpace(msg.Status), "queued")
	}, limit), nil
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

func (s *AgentMailboxStore) currentMessages(match func(AgentMessage) bool, limit int) []AgentMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []AgentMessage
	for _, item := range s.messages {
		msg := *item
		if match != nil && !match(msg) {
			continue
		}
		out = append(out, msg)
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

func parseThreadSequence(threadID string) int {
	threadID = strings.TrimSpace(threadID)
	if !strings.HasPrefix(threadID, "thread-") {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimPrefix(threadID, "thread-"))
	return n
}

func parseMessageSequence(messageID string) int {
	messageID = strings.TrimSpace(messageID)
	if !strings.HasPrefix(messageID, "msg-") {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimPrefix(messageID, "msg-"))
	return n
}
