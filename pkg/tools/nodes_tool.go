package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"clawgo/pkg/nodes"
)

// NodesTool provides an OpenClaw-style control surface for paired nodes.
type NodesTool struct {
	manager   *nodes.Manager
	router    *nodes.Router
	auditPath string
}

func NewNodesTool(m *nodes.Manager, r *nodes.Router, auditPath string) *NodesTool {
	return &NodesTool{manager: m, router: r, auditPath: strings.TrimSpace(auditPath)}
}
func (t *NodesTool) Name() string              { return "nodes" }
func (t *NodesTool) Description() string {
	return "Manage paired nodes (status/describe/run/invoke/camera/screen/location/canvas)."
}
func (t *NodesTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{
		"action": map[string]interface{}{"type": "string", "description": "status|describe|run|invoke|agent_task|camera_snap|camera_clip|screen_record|screen_snapshot|location_get|canvas_snapshot|canvas_action"},
		"node":   map[string]interface{}{"type": "string", "description": "target node id"},
		"mode":   map[string]interface{}{"type": "string", "description": "auto|p2p|relay (default auto)"},
		"args":   map[string]interface{}{"type": "object", "description": "action args"},
		"task":   map[string]interface{}{"type": "string", "description": "agent_task content for child node model"},
		"model":  map[string]interface{}{"type": "string", "description": "optional model for agent_task"},
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
			f := strings.ToLower(strings.TrimSpace(facing))
			if f != "front" && f != "back" && f != "both" {
				return "", fmt.Errorf("invalid_args: facing must be front|back|both")
			}
			reqArgs["facing"] = f
		}
		if d, ok := args["duration_ms"].(float64); ok {
			di := int(d)
			if di <= 0 || di > 300000 {
				return "", fmt.Errorf("invalid_args: duration_ms must be in 1..300000")
			}
			reqArgs["duration_ms"] = di
		}
		task, _ := args["task"].(string)
		model, _ := args["model"].(string)
		if action == "agent_task" && strings.TrimSpace(task) == "" {
			return "", fmt.Errorf("invalid_args: agent_task requires task")
		}
		if action == "canvas_action" {
			if act, _ := reqArgs["action"].(string); strings.TrimSpace(act) == "" {
				return "", fmt.Errorf("invalid_args: canvas_action requires args.action")
			}
		}
		req := nodes.Request{Action: action, Node: nodeID, Task: strings.TrimSpace(task), Model: strings.TrimSpace(model), Args: reqArgs}
		resp, err := t.router.Dispatch(ctx, req, mode)
		if err != nil {
			t.writeAudit(req, nodes.Response{OK: false, Code: "transport_error", Error: err.Error(), Node: nodeID, Action: action}, mode)
			return "", err
		}
		t.writeAudit(req, resp, mode)
		b, _ := json.Marshal(resp)
		return string(b), nil
	}
}

func (t *NodesTool) writeAudit(req nodes.Request, resp nodes.Response, mode string) {
	if strings.TrimSpace(t.auditPath) == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(t.auditPath), 0755)
	row := map[string]interface{}{
		"time":   time.Now().UTC().Format(time.RFC3339),
		"mode":   strings.TrimSpace(mode),
		"action": req.Action,
		"node":   req.Node,
		"task":   req.Task,
		"model":  req.Model,
		"ok":     resp.OK,
		"code":   resp.Code,
		"error":  resp.Error,
	}
	b, _ := json.Marshal(row)
	f, err := os.OpenFile(t.auditPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}
