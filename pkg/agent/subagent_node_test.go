package agent

import (
	"context"
	"strings"
	"testing"

	"clawgo/pkg/nodes"
	"clawgo/pkg/tools"
)

func TestDispatchNodeSubagentTaskUsesNodeAgentTask(t *testing.T) {
	manager := nodes.NewManager()
	manager.Upsert(nodes.NodeInfo{
		ID:     "edge-dev",
		Name:   "Edge Dev",
		Online: true,
		Capabilities: nodes.Capabilities{
			Model: true,
		},
	})
	manager.RegisterHandler("edge-dev", func(req nodes.Request) nodes.Response {
		if req.Action != "agent_task" {
			t.Fatalf("unexpected action: %s", req.Action)
		}
		if !strings.Contains(req.Task, "Parent Agent: main") {
			t.Fatalf("expected parent-agent context in task, got %q", req.Task)
		}
		return nodes.Response{
			OK:     true,
			Action: req.Action,
			Node:   req.Node,
			Payload: map[string]interface{}{
				"result": "remote-main-done",
			},
		}
	})

	loop := &AgentLoop{
		nodeRouter: &nodes.Router{
			Relay: &nodes.HTTPRelayTransport{Manager: manager},
		},
	}
	out, err := loop.dispatchNodeSubagentTask(context.Background(), &tools.SubagentTask{
		ID:            "subagent-1",
		AgentID:       "node.edge-dev.main",
		Transport:     "node",
		NodeID:        "edge-dev",
		ParentAgentID: "main",
		Task:          "Implement fix on remote node",
	})
	if err != nil {
		t.Fatalf("dispatchNodeSubagentTask failed: %v", err)
	}
	if out != "remote-main-done" {
		t.Fatalf("unexpected node result: %q", out)
	}
}
