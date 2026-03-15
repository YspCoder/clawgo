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

type AgentMessage struct {
	MessageID     string `json:"message_id"`
	ThreadID      string `json:"thread_id"`
	FromAgent     string `json:"from_agent"`
	ToAgent       string `json:"to_agent"`
	Type          string `json:"type"`
	Content       string `json:"content"`
	Status        string `json:"status"`
	CreatedAt     int64  `json:"created_at"`
}

type AgentMailboxStore struct {
	dir       string
	msgsPath  string
	mu        sync.RWMutex
	messages  map[string]*AgentMessage
	msgSeq    int
	threadSeq int
}

func NewAgentMailboxStore(workspace string) *AgentMailboxStore {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil
	}
	dir := filepath.Join(workspace, "agents", "runtime")
	s := &AgentMailboxStore{
		dir:      dir,
		msgsPath: filepath.Join(dir, "agent_messages.jsonl"),
		messages: map[string]*AgentMessage{},
	}
	_ = os.MkdirAll(dir, 0755)
	_ = s.load()
	return s
}

func (s *AgentMailboxStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = map[string]*AgentMessage{}
	return s.scanMessagesLocked()
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
		if n := parseThreadSequence(msg.ThreadID); n > s.threadSeq {
			s.threadSeq = n
		}
		cp := msg
		s.messages[msg.MessageID] = &cp
	}
	return scanner.Err()
}

func (s *AgentMailboxStore) EnsureThreadID(threadID string) string {
	if s == nil {
		return strings.TrimSpace(threadID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	threadID = strings.TrimSpace(threadID)
	if threadID != "" {
		if n := parseThreadSequence(threadID); n > s.threadSeq {
			s.threadSeq = n
		}
		return threadID
	}
	s.threadSeq++
	return fmt.Sprintf("thread-%04d", s.threadSeq)
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
	cp := msg
	s.messages[msg.MessageID] = &cp
	return msg, nil
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
