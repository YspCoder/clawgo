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
	router  *nodes.Router
}

func NewNodesTool(m *nodes.Manager, r *nodes.Router) *NodesTool { return &NodesTool{manager: m, router: r} }
func (t *NodesTool) Name() string              { return "nodes" }
func (t *NodesTool) Description() string {
	return "Manage paired nodes (status/describe/run/invoke/camera/screen/location/canvas)."
}
func (t *NodesTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{
		"action": map[string]interface{}{"type": "string", "description": "status|describe|run|invoke|camera_snap|camera_clip|screen_record|screen_snapshot|location_get|canvas_snapshot|canvas_action"},
		"node":   map[string]interface{}{"type": "string", "description": "target node id"},
		"mode":   map[string]interface{}{"type": "string", "description": "auto|p2p|relay (default auto)"},
		"args":   map[string]interface{}{"type": "object", "description": "action args"},
		"command": map[string]interface{}{"type": "array", "description": "run command array shortcut"},
		"facing":  map[string]interface{}{"type": "string", "description": "camera facing: front|back|both"},
		"duration_ms": map[string]interface{}{"type": "integer", "description": "clip/record duration"},
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
	mode, _ := args["mode"].(string)
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
		if nodeID == "" {
			if picked, ok := t.manager.PickFor(action); ok {
				nodeID = picked.ID
			}
		}
		if nodeID == "" {
			return "", fmt.Errorf("no eligible node found for action=%s", action)
		}
		if !t.manager.SupportsAction(nodeID, action) {
			return "", fmt.Errorf("node %s does not support action=%s", nodeID, action)
		}
		if t.router == nil {
			return "", fmt.Errorf("nodes transport router not configured")
		}
		reqArgs := map[string]interface{}{}
		if raw, ok := args["args"].(map[string]interface{}); ok {
			for k, v := range raw {
				reqArgs[k] = v
			}
		}
		if cmd, ok := args["command"].([]interface{}); ok && len(cmd) > 0 {
			reqArgs["command"] = cmd
		}
		if facing, _ := args["facing"].(string); strings.TrimSpace(facing) != "" {
			reqArgs["facing"] = strings.TrimSpace(facing)
		}
		if d, ok := args["duration_ms"].(float64); ok && d > 0 {
			reqArgs["duration_ms"] = int(d)
		}
		resp, err := t.router.Dispatch(ctx, nodes.Request{Action: action, Node: nodeID, Args: reqArgs}, mode)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(resp)
		return string(b), nil
	}
}
