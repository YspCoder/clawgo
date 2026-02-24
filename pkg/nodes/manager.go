package nodes

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// Manager keeps paired node metadata and basic routing helpers.
type Handler func(req Request) Response

type Manager struct {
	mu       sync.RWMutex
	nodes    map[string]NodeInfo
	handlers map[string]Handler
}

var defaultManager = NewManager()

func DefaultManager() *Manager { return defaultManager }

func NewManager() *Manager {
	return &Manager{nodes: map[string]NodeInfo{}, handlers: map[string]Handler{}}
}

func (m *Manager) Upsert(info NodeInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	info.LastSeenAt = time.Now().UTC()
	info.Online = true
	m.nodes[info.ID] = info
}

func (m *Manager) MarkOffline(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n, ok := m.nodes[id]; ok {
		n.Online = false
		m.nodes[id] = n
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

func (m *Manager) PickFor(action string) (NodeInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, n := range m.nodes {
		if !n.Online {
			continue
		}
		switch action {
		case "run":
			if n.Capabilities.Run {
				return n, true
			}
		case "camera_snap", "camera_clip":
			if n.Capabilities.Camera {
				return n, true
			}
		case "screen_record":
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
