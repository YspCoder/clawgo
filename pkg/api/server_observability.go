package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (s *Server) webUISessionsPayload() map[string]interface{} {
	sessionsDir := filepath.Join(filepath.Dir(s.workspacePath), "agents", "main", "sessions")
	_ = os.MkdirAll(sessionsDir, 0755)
	type item struct {
		Key     string `json:"key"`
		Channel string `json:"channel,omitempty"`
	}
	out := make([]item, 0, 16)
	entries, err := os.ReadDir(sessionsDir)
	if err == nil {
		seen := map[string]struct{}{}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".jsonl") || strings.Contains(name, ".deleted.") {
				continue
			}
			key := strings.TrimSuffix(name, ".jsonl")
			if strings.TrimSpace(key) == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			channel := ""
			if i := strings.Index(key, ":"); i > 0 {
				channel = key[:i]
			}
			out = append(out, item{Key: key, Channel: channel})
		}
	}
	if len(out) == 0 {
		out = append(out, item{Key: "main", Channel: "main"})
	}
	return map[string]interface{}{"sessions": out}
}

func (s *Server) webUITaskQueuePayload(includeHeartbeat bool) map[string]interface{} {
	path := s.memoryFilePath("task-audit.jsonl")
	b, err := os.ReadFile(path)
	lines := []string{}
	if err == nil {
		lines = strings.Split(string(b), "\n")
	}
	type agg struct {
		Last     map[string]interface{}
		Logs     []string
		Attempts int
	}
	m := map[string]*agg{}
	for _, ln := range lines {
		if ln == "" {
			continue
		}
		var row map[string]interface{}
		if err := json.Unmarshal([]byte(ln), &row); err != nil {
			continue
		}
		source := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["source"])))
		if !includeHeartbeat && source == "heartbeat" {
			continue
		}
		id := fmt.Sprintf("%v", row["task_id"])
		if id == "" {
			continue
		}
		if _, ok := m[id]; !ok {
			m[id] = &agg{Last: row, Logs: []string{}, Attempts: 0}
		}
		a := m[id]
		a.Last = row
		a.Attempts++
		if lg := strings.TrimSpace(fmt.Sprintf("%v", row["log"])); lg != "" {
			if len(a.Logs) == 0 || a.Logs[len(a.Logs)-1] != lg {
				a.Logs = append(a.Logs, lg)
				if len(a.Logs) > 20 {
					a.Logs = a.Logs[len(a.Logs)-20:]
				}
			}
		}
	}
	items := make([]map[string]interface{}, 0, len(m))
	running := make([]map[string]interface{}, 0)
	for _, a := range m {
		row := a.Last
		row["logs"] = a.Logs
		row["attempts"] = a.Attempts
		items = append(items, row)
		if fmt.Sprintf("%v", row["status"]) == "running" {
			running = append(running, row)
		}
	}
	queuePath := s.memoryFilePath("task_queue.json")
	if qb, qErr := os.ReadFile(queuePath); qErr == nil {
		var q map[string]interface{}
		if json.Unmarshal(qb, &q) == nil {
			if arr, ok := q["running"].([]interface{}); ok {
				for _, it := range arr {
					if row, ok := it.(map[string]interface{}); ok {
						running = append(running, row)
					}
				}
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return fmt.Sprintf("%v", items[i]["updated_at"]) > fmt.Sprintf("%v", items[j]["updated_at"])
	})
	sort.Slice(running, func(i, j int) bool {
		return fmt.Sprintf("%v", running[i]["updated_at"]) > fmt.Sprintf("%v", running[j]["updated_at"])
	})
	if len(items) > 30 {
		items = items[:30]
	}
	return map[string]interface{}{"items": items, "running": running}
}

func (s *Server) webUIEKGSummaryPayload(window string) map[string]interface{} {
	ekgPath := s.memoryFilePath("ekg-events.jsonl")
	window = strings.ToLower(strings.TrimSpace(window))
	windowDur := 24 * time.Hour
	switch window {
	case "6h":
		windowDur = 6 * time.Hour
	case "24h", "":
		windowDur = 24 * time.Hour
	case "7d":
		windowDur = 7 * 24 * time.Hour
	}
	selectedWindow := window
	if selectedWindow == "" {
		selectedWindow = "24h"
	}
	cutoff := time.Now().UTC().Add(-windowDur)
	rows := s.loadEKGRowsCached(ekgPath, 3000)
	type kv struct {
		Key   string  `json:"key"`
		Score float64 `json:"score,omitempty"`
		Count int     `json:"count,omitempty"`
	}
	providerScore := map[string]float64{}
	providerScoreWorkload := map[string]float64{}
	errSigCount := map[string]int{}
	errSigHeartbeat := map[string]int{}
	errSigWorkload := map[string]int{}
	sourceStats := map[string]int{}
	channelStats := map[string]int{}
	for _, row := range rows {
		ts := strings.TrimSpace(fmt.Sprintf("%v", row["time"]))
		if ts != "" {
			if tm, err := time.Parse(time.RFC3339, ts); err == nil && tm.Before(cutoff) {
				continue
			}
		}
		provider := strings.TrimSpace(fmt.Sprintf("%v", row["provider"]))
		status := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["status"])))
		errSig := strings.TrimSpace(fmt.Sprintf("%v", row["errsig"]))
		source := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["source"])))
		channel := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["channel"])))
		if source == "heartbeat" {
			continue
		}
		if source == "" {
			source = "unknown"
		}
		if channel == "" {
			channel = "unknown"
		}
		sourceStats[source]++
		channelStats[channel]++
		if provider != "" {
			providerScoreWorkload[provider] += 1
			if status == "success" {
				providerScore[provider] += 1
			} else if status == "error" {
				providerScore[provider] -= 2
			}
		}
		if errSig != "" {
			errSigWorkload[errSig]++
			if source == "heartbeat" {
				errSigHeartbeat[errSig]++
			} else if status == "error" {
				errSigCount[errSig]++
			}
		}
	}
	toTopScore := func(m map[string]float64, limit int) []kv {
		out := make([]kv, 0, len(m))
		for k, v := range m {
			out = append(out, kv{Key: k, Score: v})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Score == out[j].Score {
				return out[i].Key < out[j].Key
			}
			return out[i].Score > out[j].Score
		})
		if len(out) > limit {
			out = out[:limit]
		}
		return out
	}
	toTopCount := func(m map[string]int, limit int) []kv {
		out := make([]kv, 0, len(m))
		for k, v := range m {
			out = append(out, kv{Key: k, Count: v})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Count == out[j].Count {
				return out[i].Key < out[j].Key
			}
			return out[i].Count > out[j].Count
		})
		if len(out) > limit {
			out = out[:limit]
		}
		return out
	}
	return map[string]interface{}{
		"ok":                    true,
		"window":                selectedWindow,
		"rows":                  len(rows),
		"provider_top_score":    toTopScore(providerScore, 5),
		"provider_top_workload": toTopCount(mapFromFloatCounts(providerScoreWorkload), 5),
		"errsig_top":            toTopCount(errSigCount, 5),
		"errsig_top_heartbeat":  toTopCount(errSigHeartbeat, 5),
		"errsig_top_workload":   toTopCount(errSigWorkload, 5),
		"source_top":            toTopCount(sourceStats, 5),
		"channel_top":           toTopCount(channelStats, 5),
	}
}

func mapFromFloatCounts(src map[string]float64) map[string]int {
	out := make(map[string]int, len(src))
	for k, v := range src {
		out[k] = int(v)
	}
	return out
}

func (s *Server) handleWebUISessions(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionsDir := filepath.Join(filepath.Dir(s.workspacePath), "agents", "main", "sessions")
	_ = os.MkdirAll(sessionsDir, 0755)
	includeInternal := r.URL.Query().Get("include_internal") == "1"
	type item struct {
		Key     string `json:"key"`
		Channel string `json:"channel,omitempty"`
	}
	out := make([]item, 0, 16)
	entries, err := os.ReadDir(sessionsDir)
	if err == nil {
		seen := map[string]struct{}{}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".jsonl") || strings.Contains(name, ".deleted.") {
				continue
			}
			key := strings.TrimSuffix(name, ".jsonl")
			if strings.TrimSpace(key) == "" {
				continue
			}
			if !includeInternal && !isUserFacingSessionKey(key) {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			channel := ""
			if i := strings.Index(key, ":"); i > 0 {
				channel = key[:i]
			}
			out = append(out, item{Key: key, Channel: channel})
		}
	}
	if len(out) == 0 {
		out = append(out, item{Key: "main", Channel: "main"})
	}
	writeJSON(w, map[string]interface{}{"ok": true, "sessions": out})
}

func isUserFacingSessionKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return false
	}
	switch {
	case strings.HasPrefix(k, "subagent:"):
		return false
	case strings.HasPrefix(k, "internal:"):
		return false
	case strings.HasPrefix(k, "heartbeat:"):
		return false
	case strings.HasPrefix(k, "cron:"):
		return false
	case strings.HasPrefix(k, "hook:"):
		return false
	case strings.HasPrefix(k, "node:"):
		return false
	default:
		return true
	}
}

func (s *Server) handleWebUITaskQueue(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := s.memoryFilePath("task-audit.jsonl")
	includeHeartbeat := r.URL.Query().Get("include_heartbeat") == "1"
	b, err := os.ReadFile(path)
	lines := []string{}
	if err == nil {
		lines = strings.Split(string(b), "\n")
	}
	type agg struct {
		Last     map[string]interface{}
		Logs     []string
		Attempts int
	}
	m := map[string]*agg{}
	for _, ln := range lines {
		if ln == "" {
			continue
		}
		var row map[string]interface{}
		if err := json.Unmarshal([]byte(ln), &row); err != nil {
			continue
		}
		source := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["source"])))
		if !includeHeartbeat && source == "heartbeat" {
			continue
		}
		id := fmt.Sprintf("%v", row["task_id"])
		if id == "" {
			continue
		}
		if _, ok := m[id]; !ok {
			m[id] = &agg{Last: row, Logs: []string{}, Attempts: 0}
		}
		a := m[id]
		a.Last = row
		a.Attempts++
		if lg := strings.TrimSpace(fmt.Sprintf("%v", row["log"])); lg != "" {
			if len(a.Logs) == 0 || a.Logs[len(a.Logs)-1] != lg {
				a.Logs = append(a.Logs, lg)
				if len(a.Logs) > 20 {
					a.Logs = a.Logs[len(a.Logs)-20:]
				}
			}
		}
	}
	items := make([]map[string]interface{}, 0, len(m))
	running := make([]map[string]interface{}, 0)
	for _, a := range m {
		row := a.Last
		row["logs"] = a.Logs
		row["attempts"] = a.Attempts
		items = append(items, row)
		if fmt.Sprintf("%v", row["status"]) == "running" {
			running = append(running, row)
		}
	}

	queuePath := s.memoryFilePath("task_queue.json")
	if qb, qErr := os.ReadFile(queuePath); qErr == nil {
		var q map[string]interface{}
		if json.Unmarshal(qb, &q) == nil {
			if arr, ok := q["running"].([]interface{}); ok {
				for _, item := range arr {
					row, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					id := fmt.Sprintf("%v", row["id"])
					if strings.TrimSpace(id) == "" {
						continue
					}
					label := fmt.Sprintf("%v", row["label"])
					source := strings.TrimSpace(fmt.Sprintf("%v", row["source"]))
					if source == "" {
						source = "task_watchdog"
					}
					rec := map[string]interface{}{
						"task_id":       "cmd:" + id,
						"time":          fmt.Sprintf("%v", row["started_at"]),
						"status":        "running",
						"source":        "task_watchdog",
						"channel":       source,
						"session":       "watchdog:" + id,
						"input_preview": label,
						"duration_ms":   0,
						"attempts":      1,
						"retry_count":   0,
						"logs": []string{
							fmt.Sprintf("watchdog source=%s heavy=%v", source, row["heavy"]),
							fmt.Sprintf("next_check_at=%v stalled_rounds=%v/%v", row["next_check_at"], row["stalled_rounds"], row["stall_round_limit"]),
						},
						"idle_run": true,
					}
					items = append(items, rec)
					running = append(running, rec)
				}
			}
			if arr, ok := q["waiting"].([]interface{}); ok {
				for _, item := range arr {
					row, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					id := fmt.Sprintf("%v", row["id"])
					if strings.TrimSpace(id) == "" {
						continue
					}
					label := fmt.Sprintf("%v", row["label"])
					source := strings.TrimSpace(fmt.Sprintf("%v", row["source"]))
					if source == "" {
						source = "task_watchdog"
					}
					rec := map[string]interface{}{
						"task_id":       "cmd:" + id,
						"time":          fmt.Sprintf("%v", row["enqueued_at"]),
						"status":        "waiting",
						"source":        "task_watchdog",
						"channel":       source,
						"session":       "watchdog:" + id,
						"input_preview": label,
						"duration_ms":   0,
						"attempts":      1,
						"retry_count":   0,
						"logs": []string{
							fmt.Sprintf("watchdog source=%s heavy=%v", source, row["heavy"]),
							fmt.Sprintf("enqueued_at=%v", row["enqueued_at"]),
						},
						"idle_run": true,
					}
					items = append(items, rec)
				}
			}
			if wd, ok := q["watchdog"].(map[string]interface{}); ok {
				items = append(items, map[string]interface{}{
					"task_id":       "cmd:watchdog",
					"time":          fmt.Sprintf("%v", q["time"]),
					"status":        "running",
					"source":        "task_watchdog",
					"channel":       "watchdog",
					"session":       "watchdog:stats",
					"input_preview": "task watchdog capacity snapshot",
					"duration_ms":   0,
					"attempts":      1,
					"retry_count":   0,
					"logs": []string{
						fmt.Sprintf("cpu_total=%v usage_ratio=%v reserve_pct=%v", wd["cpu_total"], wd["usage_ratio"], wd["reserve_pct"]),
						fmt.Sprintf("active=%v/%v heavy=%v/%v waiting=%v running=%v", wd["active"], wd["max_active"], wd["active_heavy"], wd["max_heavy"], wd["waiting"], wd["running"]),
					},
					"idle_run": true,
				})
			}
		}
	}

	sort.Slice(items, func(i, j int) bool { return fmt.Sprintf("%v", items[i]["time"]) > fmt.Sprintf("%v", items[j]["time"]) })
	stats := map[string]int{"total": len(items), "running": len(running)}
	writeJSON(w, map[string]interface{}{"ok": true, "running": running, "items": items, "stats": stats})
}

func (s *Server) loadEKGRowsCached(path string, maxLines int) []map[string]interface{} {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	s.ekgCacheMu.Lock()
	defer s.ekgCacheMu.Unlock()
	if s.ekgCachePath == path && s.ekgCacheSize == fi.Size() && s.ekgCacheStamp.Equal(fi.ModTime()) && len(s.ekgCacheRows) > 0 {
		return s.ekgCacheRows
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	rows := make([]map[string]interface{}, 0, len(lines))
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		var row map[string]interface{}
		if json.Unmarshal([]byte(ln), &row) == nil {
			rows = append(rows, row)
		}
	}
	s.ekgCachePath = path
	s.ekgCacheSize = fi.Size()
	s.ekgCacheStamp = fi.ModTime()
	s.ekgCacheRows = rows
	return rows
}

func (s *Server) handleWebUIEKGStats(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ekgPath := s.memoryFilePath("ekg-events.jsonl")
	window := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("window")))
	windowDur := 24 * time.Hour
	switch window {
	case "6h":
		windowDur = 6 * time.Hour
	case "24h", "":
		windowDur = 24 * time.Hour
	case "7d":
		windowDur = 7 * 24 * time.Hour
	}
	selectedWindow := window
	if selectedWindow == "" {
		selectedWindow = "24h"
	}
	cutoff := time.Now().UTC().Add(-windowDur)
	rows := s.loadEKGRowsCached(ekgPath, 3000)
	type kv struct {
		Key   string  `json:"key"`
		Score float64 `json:"score,omitempty"`
		Count int     `json:"count,omitempty"`
	}
	providerScore := map[string]float64{}
	providerScoreWorkload := map[string]float64{}
	errSigCount := map[string]int{}
	errSigHeartbeat := map[string]int{}
	errSigWorkload := map[string]int{}
	sourceStats := map[string]int{}
	channelStats := map[string]int{}
	for _, row := range rows {
		ts := strings.TrimSpace(fmt.Sprintf("%v", row["time"]))
		if ts != "" {
			if tm, err := time.Parse(time.RFC3339, ts); err == nil {
				if tm.Before(cutoff) {
					continue
				}
			}
		}
		provider := strings.TrimSpace(fmt.Sprintf("%v", row["provider"]))
		status := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["status"])))
		errSig := strings.TrimSpace(fmt.Sprintf("%v", row["errsig"]))
		source := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["source"])))
		channel := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["channel"])))
		if source == "heartbeat" {
			continue
		}
		if source == "" {
			source = "unknown"
		}
		if channel == "" {
			channel = "unknown"
		}
		sourceStats[source]++
		channelStats[channel]++
		if provider != "" {
			switch status {
			case "success":
				providerScore[provider] += 1
				providerScoreWorkload[provider] += 1
			case "suppressed":
				providerScore[provider] += 0.2
				providerScoreWorkload[provider] += 0.2
			case "error":
				providerScore[provider] -= 1
				providerScoreWorkload[provider] -= 1
			}
		}
		if errSig != "" && status == "error" {
			errSigCount[errSig]++
			errSigWorkload[errSig]++
		}
	}
	toTopScore := func(m map[string]float64, n int) []kv {
		out := make([]kv, 0, len(m))
		for k, v := range m {
			out = append(out, kv{Key: k, Score: v})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
		if len(out) > n {
			out = out[:n]
		}
		return out
	}
	toTopCount := func(m map[string]int, n int) []kv {
		out := make([]kv, 0, len(m))
		for k, v := range m {
			out = append(out, kv{Key: k, Count: v})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
		if len(out) > n {
			out = out[:n]
		}
		return out
	}
	writeJSON(w, map[string]interface{}{
		"ok":                    true,
		"window":                selectedWindow,
		"provider_top":          toTopScore(providerScore, 5),
		"provider_top_workload": toTopScore(providerScoreWorkload, 5),
		"errsig_top":            toTopCount(errSigCount, 5),
		"errsig_top_heartbeat":  toTopCount(errSigHeartbeat, 5),
		"errsig_top_workload":   toTopCount(errSigWorkload, 5),
		"source_stats":          sourceStats,
		"channel_stats":         channelStats,
		"escalation_count":      0,
	})
}

func (s *Server) handleWebUILogsRecent(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimSpace(s.logFilePath)
	if path == "" {
		http.Error(w, "log path not configured", http.StatusInternalServerError)
		return
	}
	limit := queryBoundedPositiveInt(r, "limit", 10, 200)
	b, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	start := 0
	if len(lines) > limit {
		start = len(lines) - limit
	}
	out := make([]map[string]interface{}, 0, limit)
	for _, ln := range lines[start:] {
		if parsed, ok := parseLogLine(ln); ok {
			out = append(out, parsed)
		}
	}
	writeJSON(w, map[string]interface{}{"ok": true, "logs": out})
}
