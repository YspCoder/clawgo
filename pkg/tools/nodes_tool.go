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

const nodeAuditArtifactPreviewLimit = 32768

func NewNodesTool(m *nodes.Manager, r *nodes.Router, auditPath string) *NodesTool {
	return &NodesTool{manager: m, router: r, auditPath: strings.TrimSpace(auditPath)}
}
func (t *NodesTool) Name() string { return "nodes" }
func (t *NodesTool) Description() string {
	return "Manage paired nodes (status/describe/run/invoke/camera/screen/location/canvas)."
}
func (t *NodesTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{
		"action":         map[string]interface{}{"type": "string", "description": "status|describe|run|invoke|agent_task|camera_snap|camera_clip|screen_record|screen_snapshot|location_get|canvas_snapshot|canvas_action"},
		"node":           map[string]interface{}{"type": "string", "description": "target node id"},
		"mode":           map[string]interface{}{"type": "string", "description": "auto|p2p|relay (default auto)"},
		"args":           map[string]interface{}{"type": "object", "description": "action args"},
		"artifact_paths": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "optional workspace-relative file paths to bring back as artifacts for agent_task"},
		"task":           map[string]interface{}{"type": "string", "description": "agent_task content for child node model"},
		"model":          map[string]interface{}{"type": "string", "description": "optional model for agent_task"},
		"command":        map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "run command array shortcut"},
		"facing":         map[string]interface{}{"type": "string", "description": "camera facing: front|back|both"},
		"duration_ms":    map[string]interface{}{"type": "integer", "description": "clip/record duration"},
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
		reqArgs := map[string]interface{}{}
		if raw, ok := args["args"].(map[string]interface{}); ok {
			for k, v := range raw {
				reqArgs[k] = v
			}
		}
		if rawPaths, ok := args["artifact_paths"].([]interface{}); ok && len(rawPaths) > 0 {
			reqArgs["artifact_paths"] = rawPaths
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
		if nodeID == "" {
			if picked, ok := t.manager.PickRequest(nodes.Request{Action: action, Task: task, Model: model, Args: reqArgs}, mode); ok {
				nodeID = picked.ID
			}
		}
		if nodeID == "" {
			return "", fmt.Errorf("no eligible node found for action=%s", action)
		}
		req := nodes.Request{Action: action, Node: nodeID, Task: task, Model: model, Args: reqArgs}
		if !t.manager.SupportsRequest(nodeID, req) {
			return "", fmt.Errorf("node %s does not support action=%s", nodeID, action)
		}
		if t.router == nil {
			return "", fmt.Errorf("nodes transport router not configured")
		}
		started := time.Now()
		resp, err := t.router.Dispatch(ctx, req, mode)
		durationMs := int(time.Since(started).Milliseconds())
		if err != nil {
			t.writeAudit(req, nodes.Response{OK: false, Code: "transport_error", Error: err.Error(), Node: nodeID, Action: action}, mode, durationMs)
			return "", err
		}
		t.writeAudit(req, resp, mode, durationMs)
		b, _ := json.Marshal(resp)
		return string(b), nil
	}
}

func (t *NodesTool) writeAudit(req nodes.Request, resp nodes.Response, mode string, durationMs int) {
	if strings.TrimSpace(t.auditPath) == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(t.auditPath), 0755)
	row := map[string]interface{}{
		"time":        time.Now().UTC().Format(time.RFC3339),
		"mode":        mode,
		"action":      req.Action,
		"node":        req.Node,
		"task":        req.Task,
		"model":       req.Model,
		"ok":          resp.OK,
		"code":        resp.Code,
		"error":       resp.Error,
		"duration_ms": durationMs,
	}
	if used, _ := resp.Payload["used_transport"].(string); strings.TrimSpace(used) != "" {
		row["used_transport"] = strings.TrimSpace(used)
	}
	if fallback, _ := resp.Payload["fallback_from"].(string); strings.TrimSpace(fallback) != "" {
		row["fallback_from"] = strings.TrimSpace(fallback)
	}
	if count, kinds := artifactAuditSummary(resp.Payload["artifacts"]); count > 0 {
		row["artifact_count"] = count
		if len(kinds) > 0 {
			row["artifact_kinds"] = kinds
		}
		if previews := artifactAuditPreviews(resp.Payload["artifacts"]); len(previews) > 0 {
			row["artifacts"] = previews
		}
	}
	b, _ := json.Marshal(row)
	f, err := os.OpenFile(t.auditPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func artifactAuditSummary(raw interface{}) (int, []string) {
	items, ok := raw.([]interface{})
	if !ok {
		if typed, ok := raw.([]map[string]interface{}); ok {
			items = make([]interface{}, 0, len(typed))
			for _, item := range typed {
				items = append(items, item)
			}
		}
	}
	if len(items) == 0 {
		return 0, nil
	}
	kinds := make([]string, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if kind, _ := row["kind"].(string); strings.TrimSpace(kind) != "" {
			kinds = append(kinds, strings.TrimSpace(kind))
		}
	}
	return len(items), kinds
}

func artifactAuditPreviews(raw interface{}) []map[string]interface{} {
	items, ok := raw.([]interface{})
	if !ok {
		if typed, ok := raw.([]map[string]interface{}); ok {
			items = make([]interface{}, 0, len(typed))
			for _, item := range typed {
				items = append(items, item)
			}
		}
	}
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]interface{})
		if !ok || len(row) == 0 {
			continue
		}
		entry := map[string]interface{}{}
		for _, key := range []string{"name", "kind", "mime_type", "storage", "path", "url", "source_path"} {
			if value, ok := row[key]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
				entry[key] = value
			}
		}
		if size, ok := row["size_bytes"]; ok {
			entry["size_bytes"] = size
		}
		if text, _ := row["content_text"].(string); strings.TrimSpace(text) != "" {
			entry["content_text"] = trimAuditContent(text)
		}
		if b64, _ := row["content_base64"].(string); strings.TrimSpace(b64) != "" {
			entry["content_base64"] = trimAuditContent(b64)
			entry["content_base64_truncated"] = len(b64) > nodeAuditArtifactPreviewLimit
		}
		if truncated, ok := row["truncated"].(bool); ok && truncated {
			entry["truncated"] = true
		}
		if len(entry) > 0 {
			out = append(out, entry)
		}
	}
	return out
}

func trimAuditContent(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) <= nodeAuditArtifactPreviewLimit {
		return raw
	}
	return raw[:nodeAuditArtifactPreviewLimit]
}
