package tools

import (
	"context"
	"encoding/json"
	"time"
)

type ProcessTool struct{ m *ProcessManager }

func NewProcessTool(m *ProcessManager) *ProcessTool { return &ProcessTool{m: m} }
func (t *ProcessTool) Name() string                 { return "process" }
func (t *ProcessTool) Description() string {
	return "Manage background exec sessions: list, poll, log, kill"
}
func (t *ProcessTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{
		"action":     map[string]interface{}{"type": "string", "description": "list|poll|log|kill"},
		"session_id": map[string]interface{}{"type": "string"},
		"offset":     map[string]interface{}{"type": "integer"},
		"limit":      map[string]interface{}{"type": "integer"},
		"timeout_ms": map[string]interface{}{"type": "integer"},
	}, "required": []string{"action"}}
}

func (t *ProcessTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	action, _ := args["action"].(string)
	sid, _ := args["session_id"].(string)
	if sid == "" {
		sid, _ = args["sessionId"].(string)
	}
	switch action {
	case "list":
		b, _ := json.Marshal(t.m.List())
		return string(b), nil
	case "log":
		off := toInt(args["offset"])
		lim := toInt(args["limit"])
		return t.m.Log(sid, off, lim)
	case "kill":
		if err := t.m.Kill(sid); err != nil {
			return "", err
		}
		return "killed", nil
	case "poll":
		timeout := toInt(args["timeout_ms"])
		if timeout < 0 {
			timeout = 0
		}
		s, ok := t.m.Get(sid)
		if !ok {
			return "", nil
		}
		if timeout > 0 {
			select {
			case <-s.done:
			case <-time.After(time.Duration(timeout) * time.Millisecond):
			case <-ctx.Done():
			}
		}
		off := toInt(args["offset"])
		lim := toInt(args["limit"])
		if lim <= 0 {
			lim = 1200
		}
		if off < 0 {
			off = 0
		}
		chunk, _ := t.m.Log(sid, off, lim)
		s.mu.RLock()
		defer s.mu.RUnlock()
		resp := map[string]interface{}{"id": s.ID, "running": s.ExitCode == nil, "started_at": s.StartedAt.Format(time.RFC3339), "log": chunk, "next_offset": off + len(chunk)}
		if s.ExitCode != nil {
			resp["exit_code"] = *s.ExitCode
			resp["ended_at"] = s.EndedAt.Format(time.RFC3339)
		}
		b, _ := json.Marshal(resp)
		return string(b), nil
	default:
		return "", nil
	}
}

func toInt(v interface{}) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	default:
		return 0
	}
}
