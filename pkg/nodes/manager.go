package nodes

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultNodeTTL = 60 * time.Second

// Manager keeps paired node metadata and basic routing helpers.
type Handler func(req Request) Response

type Manager struct {
	mu       sync.RWMutex
	nodes    map[string]NodeInfo
	handlers map[string]Handler
	ttl      time.Duration
}

var defaultManager = NewManager()

func DefaultManager() *Manager { return defaultManager }

func NewManager() *Manager {
	m := &Manager{nodes: map[string]NodeInfo{}, handlers: map[string]Handler{}, ttl: defaultNodeTTL}
	go m.reaperLoop()
	return m
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

func (m *Manager) SupportsAction(nodeID, action string) bool {
	n, ok := m.Get(nodeID)
	if !ok || !n.Online {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "run":
		return n.Capabilities.Run
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
		for id, n := range m.nodes {
			if n.Online && !n.LastSeenAt.IsZero() && n.LastSeenAt.Before(cutoff) {
				n.Online = false
				m.nodes[id] = n
			}
		}
		m.mu.Unlock()
	}
}
