package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ProcessTool struct{ m *ProcessManager }

func NewProcessTool(m *ProcessManager) *ProcessTool { return &ProcessTool{m: m} }
func (t *ProcessTool) Name() string                 { return "process" }
func (t *ProcessTool) Description() string {
	return "Manage background exec sessions: list, poll, log, kill, watch_patterns"
}
func (t *ProcessTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{
		"action":         map[string]interface{}{"type": "string", "description": "list|poll|log|kill|watch_patterns"},
		"session_id":     map[string]interface{}{"type": "string"},
		"offset":         map[string]interface{}{"type": "integer"},
		"limit":          map[string]interface{}{"type": "integer"},
		"timeout_ms":     map[string]interface{}{"type": "integer"},
		"interval_ms":    map[string]interface{}{"type": "integer"},
		"patterns":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"case_sensitive": map[string]interface{}{"type": "boolean"},
		"alert_on_exit":  map[string]interface{}{"type": "boolean"},
	}, "required": []string{"action"}}
}

func (t *ProcessTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	action := MapStringArg(args, "action")
	sid := MapStringArg(args, "session_id")
	if sid == "" {
		sid = MapStringArg(args, "sessionId")
	}
	switch action {
	case "list":
		b, _ := json.Marshal(t.m.List())
		return string(b), nil
	case "log":
		off := MapIntArg(args, "offset", 0)
		lim := MapIntArg(args, "limit", 0)
		return t.m.Log(sid, off, lim)
	case "kill":
		if err := t.m.Kill(sid); err != nil {
			return "", err
		}
		return "killed", nil
	case "poll":
		timeout := MapIntArg(args, "timeout_ms", 0)
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
		off := MapIntArg(args, "offset", 0)
		lim := MapIntArg(args, "limit", 0)
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
	case "watch_patterns":
		patterns := MapStringListArg(args, "patterns")
		if len(patterns) == 0 {
			return "", fmt.Errorf("patterns is required")
		}
		timeout := MapIntArg(args, "timeout_ms", 30000)
		if timeout < 1 {
			timeout = 30000
		}
		interval := MapIntArg(args, "interval_ms", 250)
		if interval < 50 {
			interval = 50
		}
		if interval > timeout {
			interval = timeout
		}
		off := MapIntArg(args, "offset", 0)
		if off < 0 {
			off = 0
		}
		caseSensitive := false
		if v, ok := MapBoolArg(args, "case_sensitive"); ok {
			caseSensitive = v
		}
		alertOnExit := true
		if v, ok := MapBoolArg(args, "alert_on_exit"); ok {
			alertOnExit = v
		}
		return t.watchPatterns(ctx, sid, patterns, off, timeout, interval, caseSensitive, alertOnExit)
	default:
		return "", nil
	}
}

func (t *ProcessTool) watchPatterns(ctx context.Context, sid string, patterns []string, offset, timeoutMs, intervalMs int, caseSensitive, alertOnExit bool) (string, error) {
	s, ok := t.m.Get(sid)
	if !ok {
		return "", fmt.Errorf("session not found: %s", sid)
	}
	type watchPattern struct {
		original string
		lookup   string
	}
	normalized := make([]watchPattern, 0, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		lookup := p
		if !caseSensitive {
			lookup = strings.ToLower(p)
		}
		normalized = append(normalized, watchPattern{original: p, lookup: lookup})
	}
	if len(normalized) == 0 {
		return "", fmt.Errorf("patterns is required")
	}
	started := time.Now()
	deadline := started.Add(time.Duration(timeoutMs) * time.Millisecond)
	scanBuf := ""
	nextOffset := offset
	for {
		chunk, err := t.m.Log(sid, nextOffset, 16*1024)
		if err != nil {
			return "", err
		}
		if chunk != "" {
			nextOffset += len(chunk)
			scanBuf += chunk
			if len(scanBuf) > 24*1024 {
				scanBuf = scanBuf[len(scanBuf)-24*1024:]
			}
			haystack := scanBuf
			if !caseSensitive {
				haystack = strings.ToLower(haystack)
			}
			for _, pattern := range normalized {
				if strings.Contains(haystack, pattern.lookup) {
					resp := map[string]interface{}{
						"id":          s.ID,
						"matched":     true,
						"pattern":     pattern.original,
						"running":     processSessionRunning(s),
						"next_offset": nextOffset,
						"elapsed_ms":  time.Since(started).Milliseconds(),
					}
					b, _ := json.Marshal(resp)
					return string(b), nil
				}
			}
		}
		running, exitCode := processSessionState(s)
		if !running {
			resp := map[string]interface{}{
				"id":          s.ID,
				"matched":     false,
				"running":     false,
				"exit_code":   exitCode,
				"next_offset": nextOffset,
				"elapsed_ms":  time.Since(started).Milliseconds(),
			}
			if alertOnExit {
				resp["event"] = "process_exited"
			}
			b, _ := json.Marshal(resp)
			return string(b), nil
		}
		now := time.Now()
		if now.After(deadline) {
			resp := map[string]interface{}{
				"id":          s.ID,
				"matched":     false,
				"running":     true,
				"timed_out":   true,
				"next_offset": nextOffset,
				"elapsed_ms":  now.Sub(started).Milliseconds(),
			}
			b, _ := json.Marshal(resp)
			return string(b), nil
		}
		wait := time.Duration(intervalMs) * time.Millisecond
		if remaining := time.Until(deadline); wait > remaining {
			wait = remaining
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(wait):
		}
	}
}

func processSessionRunning(s *processSession) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ExitCode == nil
}

func processSessionState(s *processSession) (running bool, exitCode interface{}) {
	if s == nil {
		return false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ExitCode == nil {
		return true, nil
	}
	return false, *s.ExitCode
}
