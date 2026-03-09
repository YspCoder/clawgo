package nodes

import (
	"testing"
	"time"
)

func TestPickRequestPrefersMatchingRemoteAgent(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	now := time.Now().UTC()
	manager.Upsert(NodeInfo{
		ID:         "node-main-only",
		Online:     true,
		LastSeenAt: now,
		Capabilities: Capabilities{
			Model: true,
		},
		Agents: []AgentInfo{{ID: "main"}},
	})
	manager.Upsert(NodeInfo{
		ID:         "node-coder",
		Online:     true,
		LastSeenAt: now,
		Capabilities: Capabilities{
			Model: true,
		},
		Agents: []AgentInfo{{ID: "main"}, {ID: "coder"}},
	})

	picked, ok := manager.PickRequest(Request{
		Action: "agent_task",
		Args:   map[string]interface{}{"remote_agent_id": "coder"},
	}, "auto")
	if !ok {
		t.Fatalf("expected node pick")
	}
	if picked.ID != "node-coder" {
		t.Fatalf("expected node-coder, got %+v", picked)
	}
}

func TestPickRequestPrefersRealtimeCapableNodeForScreenActions(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	now := time.Now().UTC()
	manager.Upsert(NodeInfo{
		ID:         "relay-only",
		Online:     true,
		LastSeenAt: now.Add(-2 * time.Minute),
		Capabilities: Capabilities{
			Screen: true,
		},
		Actions: []string{"screen_snapshot"},
	})
	manager.Upsert(NodeInfo{
		ID:         "p2p-ready",
		Online:     true,
		LastSeenAt: now,
		Capabilities: Capabilities{
			Screen: true,
		},
		Actions: []string{"screen_snapshot"},
	})
	manager.RegisterWireSender("p2p-ready", &captureWireSender{})

	picked, ok := manager.PickRequest(Request{Action: "screen_snapshot"}, "auto")
	if !ok {
		t.Fatalf("expected node pick")
	}
	if picked.ID != "p2p-ready" {
		t.Fatalf("expected p2p-ready, got %+v", picked)
	}
}

func TestPickRequestHonorsActionTagsPolicy(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	manager.SetDispatchPolicy(DispatchPolicy{
		PreferP2P:          true,
		AllowRelayFallback: true,
		ActionTags: map[string][]string{
			"screen_snapshot": {"vision"},
		},
	})
	now := time.Now().UTC()
	manager.Upsert(NodeInfo{
		ID:         "build-node",
		Tags:       []string{"build"},
		Online:     true,
		LastSeenAt: now,
		Capabilities: Capabilities{
			Screen: true,
		},
		Actions: []string{"screen_snapshot"},
	})
	manager.Upsert(NodeInfo{
		ID:         "vision-node",
		Tags:       []string{"vision"},
		Online:     true,
		LastSeenAt: now,
		Capabilities: Capabilities{
			Screen: true,
		},
		Actions: []string{"screen_snapshot"},
	})

	picked, ok := manager.PickRequest(Request{Action: "screen_snapshot"}, "auto")
	if !ok {
		t.Fatalf("expected node pick")
	}
	if picked.ID != "vision-node" {
		t.Fatalf("expected vision-node, got %+v", picked)
	}
}

func TestPickRequestHonorsPreferLocalPolicy(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	manager.SetDispatchPolicy(DispatchPolicy{
		PreferLocal:        true,
		PreferP2P:          false,
		AllowRelayFallback: true,
	})
	now := time.Now().UTC()
	manager.Upsert(NodeInfo{
		ID:         "local",
		Online:     true,
		LastSeenAt: now.Add(-1 * time.Minute),
		Capabilities: Capabilities{
			Run: true,
		},
		Actions: []string{"run"},
	})
	manager.Upsert(NodeInfo{
		ID:         "remote",
		Online:     true,
		LastSeenAt: now,
		Capabilities: Capabilities{
			Run: true,
		},
		Actions: []string{"run"},
	})

	picked, ok := manager.PickRequest(Request{Action: "run"}, "auto")
	if !ok {
		t.Fatalf("expected node pick")
	}
	if picked.ID != "local" {
		t.Fatalf("expected local, got %+v", picked)
	}
}

func TestPickRequestHonorsNodeAllowActionsPolicy(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	manager.SetDispatchPolicy(DispatchPolicy{
		AllowRelayFallback: true,
		AllowActions: map[string][]string{
			"camera-node": {"camera_snap"},
		},
	})
	now := time.Now().UTC()
	manager.Upsert(NodeInfo{
		ID:         "camera-node",
		Online:     true,
		LastSeenAt: now,
		Capabilities: Capabilities{
			Camera: true,
			Screen: true,
		},
		Actions: []string{"camera_snap", "screen_snapshot"},
	})

	if _, ok := manager.PickRequest(Request{Action: "screen_snapshot"}, "auto"); ok {
		t.Fatalf("expected screen_snapshot to be blocked by allow_actions")
	}
	if _, ok := manager.PickRequest(Request{Action: "camera_snap"}, "auto"); !ok {
		t.Fatalf("expected camera_snap to remain allowed")
	}
}

func TestPickRequestHonorsNodeDenyAgentsPolicy(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	manager.SetDispatchPolicy(DispatchPolicy{
		AllowRelayFallback: true,
		DenyAgents: map[string][]string{
			"edge-a": {"coder"},
		},
	})
	now := time.Now().UTC()
	manager.Upsert(NodeInfo{
		ID:         "edge-a",
		Online:     true,
		LastSeenAt: now,
		Capabilities: Capabilities{
			Model: true,
		},
		Agents: []AgentInfo{{ID: "main"}, {ID: "coder"}},
	})

	if _, ok := manager.PickRequest(Request{Action: "agent_task", Args: map[string]interface{}{"remote_agent_id": "coder"}}, "auto"); ok {
		t.Fatalf("expected coder agent_task to be denied by policy")
	}
}
