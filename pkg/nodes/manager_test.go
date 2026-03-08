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
