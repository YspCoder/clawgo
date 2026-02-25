package nodes

import (
	"encoding/json"
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

type Manager struct {
	mu        sync.RWMutex
	nodes     map[string]NodeInfo
	handlers  map[string]Handler
	ttl       time.Duration
	auditPath string
	statePath string
}

var defaultManager = NewManager()

func DefaultManager() *Manager { return defaultManager }

func NewManager() *Manager {
	m := &Manager{nodes: map[string]NodeInfo{}, handlers: map[string]Handler{}, ttl: defaultNodeTTL}
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

func (m *Manager) RegisterHandler(nodeID string, h Handler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(nodeID) == "" || h == nil {
		return
	}
	m.handlers[nodeID] = h
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
	action = strings.ToLower(strings.TrimSpace(action))
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
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, n := range m.nodes {
		if !n.Online {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(action)) {
		case "run":
			if n.Capabilities.Run {
				return n, true
			}
		case "agent_task":
			if n.Capabilities.Model {
				return n, true
			}
		case "camera_snap", "camera_clip":
			if n.Capabilities.Camera {
				return n, true
			}
		case "screen_record", "screen_snapshot":
			if n.Capabilities.Screen {
				return n, true
			}
		case "location_get":
			if n.Capabilities.Location {
				return n, true
			}
		case "canvas_snapshot", "canvas_action":
			if n.Capabilities.Canvas {
				return n, true
			}
		default:
			if n.Capabilities.Invoke {
				return n, true
			}
		}
	}
	return NodeInfo{}, false
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
