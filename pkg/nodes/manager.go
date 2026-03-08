package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultNodeTTL = 60 * time.Second

// Manager keeps paired node metadata and basic routing helpers.
type Handler func(req Request) Response
type WireSender interface {
	Send(msg WireMessage) error
}

type Manager struct {
	mu        sync.RWMutex
	nodes     map[string]NodeInfo
	handlers  map[string]Handler
	senders   map[string]WireSender
	pending   map[string]chan WireMessage
	nextWire  uint64
	ttl       time.Duration
	auditPath string
	statePath string
}

var defaultManager = NewManager()

func DefaultManager() *Manager { return defaultManager }

func NewManager() *Manager {
	m := &Manager{
		nodes:    map[string]NodeInfo{},
		handlers: map[string]Handler{},
		senders:  map[string]WireSender{},
		pending:  map[string]chan WireMessage{},
		ttl:      defaultNodeTTL,
	}
	go m.reaperLoop()
	return m
}

func (m *Manager) SetAuditPath(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.auditPath = strings.TrimSpace(path)
}

func (m *Manager) SetStatePath(path string) {
	m.mu.Lock()
	m.statePath = strings.TrimSpace(path)
	m.mu.Unlock()
	m.loadState()
}

func (m *Manager) Upsert(info NodeInfo) {
	m.mu.Lock()
	now := time.Now().UTC()
	old, existed := m.nodes[info.ID]
	info.LastSeenAt = now
	info.Online = true
	if existed {
		if info.RegisteredAt.IsZero() {
			info.RegisteredAt = old.RegisteredAt
		}
		if strings.TrimSpace(info.Endpoint) == "" {
			info.Endpoint = old.Endpoint
		}
		if strings.TrimSpace(info.Token) == "" {
			info.Token = old.Token
		}
	} else if info.RegisteredAt.IsZero() {
		info.RegisteredAt = now
	}
	m.nodes[info.ID] = info
	m.mu.Unlock()
	m.saveState()
	m.appendAudit("upsert", info.ID, map[string]interface{}{"existed": existed, "endpoint": info.Endpoint, "version": info.Version})
}

func (m *Manager) MarkOffline(id string) {
	m.mu.Lock()
	changed := false
	if n, ok := m.nodes[id]; ok {
		n.Online = false
		m.nodes[id] = n
		changed = true
	}
	m.mu.Unlock()
	if changed {
		m.saveState()
	}
}

func (m *Manager) Get(id string) (NodeInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n, ok := m.nodes[id]
	return n, ok
}

func (m *Manager) List() []NodeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]NodeInfo, 0, len(m.nodes))
	for _, n := range m.nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeenAt.After(out[j].LastSeenAt) })
	return out
}

func (m *Manager) Remove(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	m.mu.Lock()
	_, exists := m.nodes[id]
	if exists {
		delete(m.nodes, id)
		delete(m.handlers, id)
	}
	m.mu.Unlock()
	if exists {
		m.saveState()
		m.appendAudit("delete", id, nil)
	}
	return exists
}

func (m *Manager) RegisterHandler(nodeID string, h Handler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(nodeID) == "" || h == nil {
		return
	}
	m.handlers[nodeID] = h
}

func (m *Manager) RegisterWireSender(nodeID string, sender WireSender) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if sender == nil {
		delete(m.senders, nodeID)
		return
	}
	m.senders[nodeID] = sender
}

func (m *Manager) HandleWireMessage(msg WireMessage) bool {
	switch strings.ToLower(strings.TrimSpace(msg.Type)) {
	case "node_response":
		if strings.TrimSpace(msg.ID) == "" {
			return false
		}
		m.mu.Lock()
		ch := m.pending[msg.ID]
		if ch != nil {
			delete(m.pending, msg.ID)
		}
		m.mu.Unlock()
		if ch == nil {
			return false
		}
		select {
		case ch <- msg:
		default:
		}
		return true
	default:
		return false
	}
}

func (m *Manager) SendWireRequest(ctx context.Context, nodeID string, req Request) (Response, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return Response{}, fmt.Errorf("node id required")
	}
	m.mu.Lock()
	sender := m.senders[nodeID]
	if sender == nil {
		m.mu.Unlock()
		return Response{}, fmt.Errorf("node %s websocket sender unavailable", nodeID)
	}
	m.nextWire++
	wireID := fmt.Sprintf("wire-%d", m.nextWire)
	ch := make(chan WireMessage, 1)
	m.pending[wireID] = ch
	m.mu.Unlock()

	msg := WireMessage{
		Type:    "node_request",
		ID:      wireID,
		To:      nodeID,
		Request: &req,
	}
	if err := sender.Send(msg); err != nil {
		m.mu.Lock()
		delete(m.pending, wireID)
		m.mu.Unlock()
		return Response{}, err
	}

	select {
	case <-ctx.Done():
		m.mu.Lock()
		delete(m.pending, wireID)
		m.mu.Unlock()
		return Response{}, ctx.Err()
	case incoming := <-ch:
		if incoming.Response == nil {
			return Response{}, fmt.Errorf("node %s returned empty response", nodeID)
		}
		return *incoming.Response, nil
	}
}

func (m *Manager) Invoke(req Request) (Response, bool) {
	m.mu.RLock()
	h, ok := m.handlers[req.Node]
	m.mu.RUnlock()
	if !ok {
		return Response{}, false
	}
	resp := h(req)
	if strings.TrimSpace(resp.Node) == "" {
		resp.Node = req.Node
	}
	if strings.TrimSpace(resp.Action) == "" {
		resp.Action = req.Action
	}
	return resp, true
}

func (m *Manager) SupportsAction(nodeID, action string) bool {
	n, ok := m.Get(nodeID)
	if !ok || !n.Online {
		return false
	}
	return nodeSupportsRequest(n, Request{Action: action})
}

func (m *Manager) SupportsRequest(nodeID string, req Request) bool {
	n, ok := m.Get(nodeID)
	if !ok || !n.Online {
		return false
	}
	return nodeSupportsRequest(n, req)
}

func nodeSupportsRequest(n NodeInfo, req Request) bool {
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if len(n.Actions) > 0 {
		allowed := false
		for _, a := range n.Actions {
			if strings.ToLower(strings.TrimSpace(a)) == action {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	switch action {
	case "run":
		return n.Capabilities.Run
	case "agent_task":
		return n.Capabilities.Model
	case "camera_snap", "camera_clip":
		return n.Capabilities.Camera
	case "screen_record", "screen_snapshot":
		return n.Capabilities.Screen
	case "location_get":
		return n.Capabilities.Location
	case "canvas_snapshot", "canvas_action":
		return n.Capabilities.Canvas
	default:
		return n.Capabilities.Invoke
	}
}

func (m *Manager) PickFor(action string) (NodeInfo, bool) {
	return m.PickRequest(Request{Action: action}, "auto")
}

func (m *Manager) PickRequest(req Request, mode string) (NodeInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	bestScore := -1
	bestNode := NodeInfo{}
	for _, n := range m.nodes {
		score, ok := scoreNodeCandidate(n, req, mode, m.senders[strings.TrimSpace(n.ID)] != nil)
		if !ok {
			continue
		}
		if score > bestScore || (score == bestScore && bestNode.ID != "" && n.LastSeenAt.After(bestNode.LastSeenAt)) {
			bestScore = score
			bestNode = n
		}
	}
	if bestScore < 0 || strings.TrimSpace(bestNode.ID) == "" {
		return NodeInfo{}, false
	}
	return bestNode, true
}

func scoreNodeCandidate(n NodeInfo, req Request, mode string, hasWireSender bool) (int, bool) {
	if !n.Online {
		return 0, false
	}
	if !nodeSupportsRequest(n, req) {
		return 0, false
	}

	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "p2p" && !hasWireSender {
		return 0, false
	}

	score := 100
	if hasWireSender {
		score += 30
	}
	if prefersRealtimeTransport(req.Action) && hasWireSender {
		score += 40
	}
	if mode == "relay" && hasWireSender {
		score -= 10
	}
	if mode == "p2p" && hasWireSender {
		score += 80
	}
	if strings.EqualFold(strings.TrimSpace(req.Action), "agent_task") {
		remoteAgentID := requestedRemoteAgentID(req.Args)
		switch {
		case remoteAgentID == "", remoteAgentID == "main":
			score += 20
		case nodeHasAgent(n, remoteAgentID):
			score += 80
		default:
			return 0, false
		}
	}
	if !n.LastSeenAt.IsZero() {
		ageSeconds := int(time.Since(n.LastSeenAt).Seconds())
		if ageSeconds < 0 {
			ageSeconds = 0
		}
		if ageSeconds < 60 {
			score += 20
		} else if ageSeconds < 300 {
			score += 5
		}
	}
	return score, true
}

func requestedRemoteAgentID(args map[string]interface{}) string {
	if len(args) == 0 {
		return ""
	}
	value, ok := args["remote_agent_id"]
	if !ok || value == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
}

func nodeHasAgent(n NodeInfo, agentID string) bool {
	agentID = strings.ToLower(strings.TrimSpace(agentID))
	if agentID == "" {
		return false
	}
	for _, agent := range n.Agents {
		if strings.ToLower(strings.TrimSpace(agent.ID)) == agentID {
			return true
		}
	}
	return false
}

func prefersRealtimeTransport(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "camera_snap", "camera_clip", "screen_record", "screen_snapshot", "canvas_snapshot", "canvas_action":
		return true
	default:
		return false
	}
}

func (m *Manager) reaperLoop() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().UTC().Add(-m.ttl)
		m.mu.Lock()
		offlined := make([]string, 0)
		for id, n := range m.nodes {
			if n.Online && !n.LastSeenAt.IsZero() && n.LastSeenAt.Before(cutoff) {
				n.Online = false
				m.nodes[id] = n
				offlined = append(offlined, id)
			}
		}
		m.mu.Unlock()
		if len(offlined) > 0 {
			m.saveState()
		}
		for _, id := range offlined {
			m.appendAudit("offline_ttl", id, nil)
		}
	}
}

func (m *Manager) appendAudit(event, nodeID string, data map[string]interface{}) {
	m.mu.RLock()
	path := m.auditPath
	m.mu.RUnlock()
	if strings.TrimSpace(path) == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	row := map[string]interface{}{"time": time.Now().UTC().Format(time.RFC3339), "event": event, "node": nodeID}
	for k, v := range data {
		row[k] = v
	}
	b, _ := json.Marshal(row)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func (m *Manager) saveState() {
	m.mu.RLock()
	path := m.statePath
	items := make([]NodeInfo, 0, len(m.nodes))
	for _, n := range m.nodes {
		items = append(items, n)
	}
	m.mu.RUnlock()
	if strings.TrimSpace(path) == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, b, 0644)
}

func (m *Manager) loadState() {
	m.mu.RLock()
	path := m.statePath
	m.mu.RUnlock()
	if strings.TrimSpace(path) == "" {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var items []NodeInfo
	if err := json.Unmarshal(b, &items); err != nil {
		return
	}
	m.mu.Lock()
	for _, n := range items {
		if strings.TrimSpace(n.ID) == "" {
			continue
		}
		m.nodes[n.ID] = n
	}
	m.mu.Unlock()
}
