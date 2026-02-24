package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"clawgo/pkg/nodes"
)

// NodesTool provides an OpenClaw-style control surface for paired nodes.
type NodesTool struct {
	manager *nodes.Manager
}

func NewNodesTool(m *nodes.Manager) *NodesTool { return &NodesTool{manager: m} }
func (t *NodesTool) Name() string              { return "nodes" }
func (t *NodesTool) Description() string {
	return "Manage paired nodes (status/describe/run/invoke/camera/screen/location)."
}
func (t *NodesTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{
		"action": map[string]interface{}{"type": "string", "description": "status|describe|run|invoke|camera_snap|screen_record|location_get"},
		"node":   map[string]interface{}{"type": "string", "description": "target node id"},
		"args":   map[string]interface{}{"type": "object", "description": "action args"},
	}, "required": []string{"action"}}
}

func (t *NodesTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	_ = ctx
	action, _ := args["action"].(string)
	action = strings.TrimSpace(strings.ToLower(action))
	if action == "" {
		return "", fmt.Errorf("action is required")
	}
	nodeID, _ := args["node"].(string)
	if t.manager == nil {
		return "", fmt.Errorf("nodes manager not configured")
	}

	switch action {
	case "status", "describe":
		if nodeID != "" {
			n, ok := t.manager.Get(nodeID)
			if !ok {
				return "", fmt.Errorf("node not found: %s", nodeID)
			}
			b, _ := json.Marshal(n)
			return string(b), nil
		}
		b, _ := json.Marshal(t.manager.List())
		return string(b), nil
	default:
		// Phase-1: control-plane exists, data-plane RPC bridge lands in next step.
		if nodeID == "" {
			if picked, ok := t.manager.PickFor(action); ok {
				nodeID = picked.ID
			}
		}
		if nodeID == "" {
			return "", fmt.Errorf("no eligible node found for action=%s", action)
		}
		resp := nodes.Response{OK: false, Node: nodeID, Action: action, Error: "node transport bridge not implemented yet"}
		b, _ := json.Marshal(resp)
		return string(b), nil
	}
}
