package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"clawgo/pkg/config"
	"clawgo/pkg/providers"
)
func statusCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	configPath := getConfigPath()

	fmt.Printf("%s clawgo Status\n\n", logo)

	if _, err := os.Stat(configPath); err == nil {
		fmt.Println("Config:", configPath, "✓")
	} else {
		fmt.Println("Config:", configPath, "✗")
	}

	workspace := cfg.WorkspacePath()
	if _, err := os.Stat(workspace); err == nil {
		fmt.Println("Workspace:", workspace, "✓")
	} else {
		fmt.Println("Workspace:", workspace, "✗")
	}

	if _, err := os.Stat(configPath); err == nil {
		activeProvider := cfg.Providers.Proxy
		activeProxyName := "proxy"
		if name := strings.TrimSpace(cfg.Agents.Defaults.Proxy); name != "" && name != "proxy" {
			if p, ok := cfg.Providers.Proxies[name]; ok {
				activeProvider = p
				activeProxyName = name
			}
		}
		activeModel := ""
		for _, m := range activeProvider.Models {
			if s := strings.TrimSpace(m); s != "" {
				activeModel = s
				break
			}
		}
		fmt.Printf("Model: %s\n", activeModel)
		fmt.Printf("Proxy: %s\n", activeProxyName)
		fmt.Printf("CLIProxyAPI Base: %s\n", cfg.Providers.Proxy.APIBase)
		fmt.Printf("Supports /v1/responses/compact: %v\n", providers.ProviderSupportsResponsesCompact(cfg, activeProxyName))
		hasKey := cfg.Providers.Proxy.APIKey != ""
		status := "not set"
		if hasKey {
			status = "✓"
		}
		fmt.Printf("CLIProxyAPI Key: %s\n", status)
		fmt.Printf("Logging: %v\n", cfg.Logging.Enabled)
		if cfg.Logging.Enabled {
			fmt.Printf("Log File: %s\n", cfg.LogFilePath())
			fmt.Printf("Log Max Size: %d MB\n", cfg.Logging.MaxSizeMB)
			fmt.Printf("Log Retention: %d days\n", cfg.Logging.RetentionDays)
		}

		fmt.Printf("Heartbeat: enabled=%v interval=%ds ackMaxChars=%d\n",
			cfg.Agents.Defaults.Heartbeat.Enabled,
			cfg.Agents.Defaults.Heartbeat.EverySec,
			cfg.Agents.Defaults.Heartbeat.AckMaxChars,
		)
		printTemplateStatus(cfg)
		fmt.Printf("Cron Runtime: workers=%d sleep=%d-%ds\n",
			cfg.Cron.MaxWorkers,
			cfg.Cron.MinSleepSec,
			cfg.Cron.MaxSleepSec,
		)

		heartbeatLog := filepath.Join(workspace, "memory", "heartbeat.log")
		if data, err := os.ReadFile(heartbeatLog); err == nil {
			trimmed := strings.TrimSpace(string(data))
			if trimmed != "" {
				lines := strings.Split(trimmed, "\n")
				fmt.Printf("Heartbeat Runs Logged: %d\n", len(lines))
				fmt.Printf("Heartbeat Last Log: %s\n", lines[len(lines)-1])
			}
		}

		triggerStats := filepath.Join(workspace, "memory", "trigger-stats.json")
		if data, err := os.ReadFile(triggerStats); err == nil {
			fmt.Printf("Trigger Stats: %s\n", strings.TrimSpace(string(data)))
			if summary := summarizeAutonomyActions(data); summary != "" {
				fmt.Printf("Autonomy Action Stats: %s\n", summary)
			}
		}
		auditPath := filepath.Join(workspace, "memory", "trigger-audit.jsonl")
		if errs, err := collectRecentTriggerErrors(auditPath, 5); err == nil && len(errs) > 0 {
			fmt.Println("Recent Trigger Errors:")
			for _, e := range errs {
				fmt.Printf("  - %s\n", e)
			}
		}
		if agg, err := collectTriggerErrorCounts(auditPath); err == nil && len(agg) > 0 {
			fmt.Println("Trigger Error Counts:")
			keys := make([]string, 0, len(agg))
			for k := range agg {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, trigger := range keys {
				fmt.Printf("  %s: %d\n", trigger, agg[trigger])
			}
		}
		if total, okCnt, failCnt, reasonCov, top, err := collectSkillExecStats(filepath.Join(workspace, "memory", "skill-audit.jsonl")); err == nil && total > 0 {
			fmt.Printf("Skill Exec: total=%d ok=%d fail=%d reason_coverage=%.2f\n", total, okCnt, failCnt, reasonCov)
			if top != "" {
				fmt.Printf("Skill Exec Top: %s\n", top)
			}
		}

		sessionsDir := filepath.Join(filepath.Dir(configPath), "sessions")
		if kinds, err := collectSessionKindCounts(sessionsDir); err == nil && len(kinds) > 0 {
			fmt.Println("Session Kinds:")
			for _, k := range []string{"main", "cron", "subagent", "hook", "node", "other"} {
				if v, ok := kinds[k]; ok {
					fmt.Printf("  %s: %d\n", k, v)
				}
			}
		}
		if recent, err := collectRecentSubagentSessions(sessionsDir, 5); err == nil && len(recent) > 0 {
			fmt.Println("Recent Subagent Sessions:")
			for _, key := range recent {
				fmt.Printf("  - %s\n", key)
			}
		}
		fmt.Printf("Autonomy Config: idle_resume=%ds waiting_debounce=%ds notify_cooldown=%ds same_reason_cooldown=%ds\n",
			cfg.Agents.Defaults.Autonomy.UserIdleResumeSec,
			cfg.Agents.Defaults.Autonomy.WaitingResumeDebounceSec,
			cfg.Agents.Defaults.Autonomy.NotifyCooldownSec,
			cfg.Agents.Defaults.Autonomy.NotifySameReasonCooldownSec,
		)
		if summary, prio, reasons, nextRetry, dedupeHits, waitingLocks, lockKeys, err := collectAutonomyTaskSummary(filepath.Join(workspace, "memory", "tasks.json")); err == nil {
			fmt.Printf("Autonomy Tasks: todo=%d doing=%d waiting=%d blocked=%d done=%d dedupe_hits=%d\n", summary["todo"], summary["doing"], summary["waiting"], summary["blocked"], summary["done"], dedupeHits)
			fmt.Printf("Autonomy Priority: high=%d normal=%d low=%d\n", prio["high"], prio["normal"], prio["low"])
			if reasons["active_user"] > 0 || reasons["manual_pause"] > 0 || reasons["max_consecutive_stalls"] > 0 || reasons["resource_lock"] > 0 {
				fmt.Printf("Autonomy Block Reasons: active_user=%d manual_pause=%d max_stalls=%d resource_lock=%d\n", reasons["active_user"], reasons["manual_pause"], reasons["max_consecutive_stalls"], reasons["resource_lock"])
			}
			if waitingLocks > 0 || lockKeys > 0 {
				fmt.Printf("Autonomy Locks: waiting=%d unique_keys=%d\n", waitingLocks, lockKeys)
			}
			if nextRetry != "" {
				fmt.Printf("Autonomy Next Retry: %s\n", nextRetry)
			}
			fmt.Printf("Autonomy Control: %s\n", autonomyControlState(workspace))
		}
	}
}

func printTemplateStatus(cfg *config.Config) {
	if cfg == nil {
		return
	}
	defaults := config.DefaultConfig().Agents.Defaults.Texts
	cur := cfg.Agents.Defaults.Texts
	fmt.Println("Dialog Templates:")
	printTemplateField("system_rewrite_template", cur.SystemRewriteTemplate, defaults.SystemRewriteTemplate)
	printTemplateField("lang_usage", cur.LangUsage, defaults.LangUsage)
	printTemplateField("lang_invalid", cur.LangInvalid, defaults.LangInvalid)
	printTemplateField("runtime_compaction_note", cur.RuntimeCompactionNote, defaults.RuntimeCompactionNote)
	printTemplateField("startup_compaction_note", cur.StartupCompactionNote, defaults.StartupCompactionNote)
	printTemplateField("autonomy_completion_template", cur.AutonomyCompletionTemplate, defaults.AutonomyCompletionTemplate)
	printTemplateField("autonomy_blocked_template", cur.AutonomyBlockedTemplate, defaults.AutonomyBlockedTemplate)
}

func printTemplateField(name, current, def string) {
	state := "custom"
	if strings.TrimSpace(current) == strings.TrimSpace(def) {
		state = "default"
	}
	fmt.Printf("  %s: %s\n", name, state)
}

func summarizeAutonomyActions(statsJSON []byte) string {
	var payload struct {
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(statsJSON, &payload); err != nil || payload.Counts == nil {
		return ""
	}
	keys := []string{"autonomy:dispatch", "autonomy:waiting", "autonomy:resume", "autonomy:blocked", "autonomy:complete"}
	parts := make([]string, 0, len(keys)+1)
	total := 0
	for _, k := range keys {
		if v, ok := payload.Counts[k]; ok {
			parts = append(parts, fmt.Sprintf("%s=%d", strings.TrimPrefix(k, "autonomy:"), v))
			total += v
		}
	}
	if total > 0 {
		d := payload.Counts["autonomy:dispatch"]
		w := payload.Counts["autonomy:waiting"]
		b := payload.Counts["autonomy:blocked"]
		parts = append(parts, fmt.Sprintf("ratios(dispatch/waiting/blocked)=%.2f/%.2f/%.2f", float64(d)/float64(total), float64(w)/float64(total), float64(b)/float64(total)))
	}
	wa := payload.Counts["autonomy:waiting:active_user"]
	wm := payload.Counts["autonomy:waiting:manual_pause"]
	ra := payload.Counts["autonomy:resume:active_user"]
	rm := payload.Counts["autonomy:resume:manual_pause"]
	if wa+wm+ra+rm > 0 {
		parts = append(parts, fmt.Sprintf("wait_resume(active_user=%d/%d manual_pause=%d/%d)", wa, ra, wm, rm))
		waitTotal := wa + wm
		resumeTotal := ra + rm
		if waitTotal >= 8 {
			parts = append(parts, fmt.Sprintf("flap_risk=%s", flapRisk(waitTotal, resumeTotal)))
		}
	}
	return strings.Join(parts, " ")
}

func flapRisk(waitTotal, resumeTotal int) string {
	if waitTotal <= 0 {
		return "low"
	}
	if resumeTotal == 0 {
		return "high(no_resume)"
	}
	ratio := float64(waitTotal) / float64(resumeTotal)
	if ratio >= 2.0 || ratio <= 0.5 {
		return "high"
	}
	if ratio >= 1.5 || ratio <= 0.67 {
		return "medium"
	}
	return "low"
}

func autonomyControlState(workspace string) string {
	memDir := filepath.Join(workspace, "memory")
	pausePath := filepath.Join(memDir, "autonomy.pause")
	if _, err := os.Stat(pausePath); err == nil {
		return "paused (autonomy.pause)"
	}
	ctrlPath := filepath.Join(memDir, "autonomy.control.json")
	if data, err := os.ReadFile(ctrlPath); err == nil {
		var c struct{ Enabled bool `json:"enabled"` }
		if json.Unmarshal(data, &c) == nil {
			if c.Enabled {
				return "enabled"
			}
			return "disabled (control file)"
		}
	}
	return "default"
}

func collectSessionKindCounts(sessionsDir string) (map[string]int, error) {
	indexPath := filepath.Join(sessionsDir, "sessions.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, err
	}
	var index map[string]struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, row := range index {
		kind := strings.TrimSpace(strings.ToLower(row.Kind))
		if kind == "" {
			kind = "other"
		}
		counts[kind]++
	}
	return counts, nil
}

func collectRecentTriggerErrors(path string, limit int) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 1 && strings.TrimSpace(lines[0]) == "" {
		return nil, nil
	}
	out := make([]string, 0, limit)
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var row struct {
			Time    string `json:"time"`
			Trigger string `json:"trigger"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if strings.TrimSpace(row.Error) == "" {
			continue
		}
		out = append(out, fmt.Sprintf("[%s/%s] %s", row.Time, row.Trigger, row.Error))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func collectTriggerErrorCounts(path string) (map[string]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	counts := map[string]int{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row struct {
			Trigger string `json:"trigger"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if strings.TrimSpace(row.Error) == "" {
			continue
		}
		trigger := strings.ToLower(strings.TrimSpace(row.Trigger))
		if trigger == "" {
			trigger = "unknown"
		}
		counts[trigger]++
	}
	return counts, nil
}

func collectAutonomyTaskSummary(path string) (map[string]int, map[string]int, map[string]int, string, int, int, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]int{"todo": 0, "doing": 0, "waiting": 0, "blocked": 0, "done": 0}, map[string]int{"high": 0, "normal": 0, "low": 0}, map[string]int{"active_user": 0, "manual_pause": 0, "max_consecutive_stalls": 0, "resource_lock": 0}, "", 0, 0, 0, nil
		}
		return nil, nil, nil, "", 0, 0, 0, err
	}
	var items []struct {
		Status       string   `json:"status"`
		Priority     string   `json:"priority"`
		BlockReason  string   `json:"block_reason"`
		RetryAfter   string   `json:"retry_after"`
		DedupeHits   int      `json:"dedupe_hits"`
		ResourceKeys []string `json:"resource_keys"`
	}
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, nil, nil, "", 0, 0, 0, err
	}
	summary := map[string]int{"todo": 0, "doing": 0, "waiting": 0, "blocked": 0, "done": 0}
	priorities := map[string]int{"high": 0, "normal": 0, "low": 0}
	reasons := map[string]int{"active_user": 0, "manual_pause": 0, "max_consecutive_stalls": 0, "resource_lock": 0}
	nextRetry := ""
	nextRetryAt := time.Time{}
	totalDedupe := 0
	waitingLocks := 0
	lockKeySet := map[string]struct{}{}
	for _, it := range items {
		s := strings.ToLower(strings.TrimSpace(it.Status))
		if _, ok := summary[s]; ok {
			summary[s]++
		}
		totalDedupe += it.DedupeHits
		r := strings.ToLower(strings.TrimSpace(it.BlockReason))
		if _, ok := reasons[r]; ok {
			reasons[r]++
		}
		if s == "waiting" && r == "resource_lock" {
			waitingLocks++
			for _, k := range it.ResourceKeys {
				kk := strings.TrimSpace(strings.ToLower(k))
				if kk != "" {
					lockKeySet[kk] = struct{}{}
				}
			}
		}
		p := strings.ToLower(strings.TrimSpace(it.Priority))
		if _, ok := priorities[p]; ok {
			priorities[p]++
		} else {
			priorities["normal"]++
		}
		if strings.TrimSpace(it.RetryAfter) != "" {
			if t, err := time.Parse(time.RFC3339, it.RetryAfter); err == nil {
				if nextRetryAt.IsZero() || t.Before(nextRetryAt) {
					nextRetryAt = t
					nextRetry = t.Format(time.RFC3339)
				}
			}
		}
	}
	return summary, priorities, reasons, nextRetry, totalDedupe, waitingLocks, len(lockKeySet), nil
}

func collectSkillExecStats(path string) (int, int, int, float64, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, 0, 0, "", err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	total, okCnt, failCnt := 0, 0, 0
	reasonCnt := 0
	skillCounts := map[string]int{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row struct {
			Skill  string `json:"skill"`
			Reason string `json:"reason"`
			OK     bool   `json:"ok"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		total++
		if row.OK {
			okCnt++
		} else {
			failCnt++
		}
		r := strings.TrimSpace(strings.ToLower(row.Reason))
		if r != "" && r != "unspecified" {
			reasonCnt++
		}
		s := strings.TrimSpace(row.Skill)
		if s == "" {
			s = "unknown"
		}
		skillCounts[s]++
	}
	topSkill := ""
	topN := 0
	for k, v := range skillCounts {
		if v > topN {
			topN = v
			topSkill = k
		}
	}
	if topSkill != "" {
		topSkill = fmt.Sprintf("%s(%d)", topSkill, topN)
	}
	reasonCoverage := 0.0
	if total > 0 {
		reasonCoverage = float64(reasonCnt) / float64(total)
	}
	return total, okCnt, failCnt, reasonCoverage, topSkill, nil
}

func collectRecentSubagentSessions(sessionsDir string, limit int) ([]string, error) {
	indexPath := filepath.Join(sessionsDir, "sessions.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, err
	}
	var index map[string]struct {
		Kind      string `json:"kind"`
		UpdatedAt int64  `json:"updatedAt"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, err
	}
	type item struct {
		key     string
		updated int64
	}
	items := make([]item, 0)
	for key, row := range index {
		if strings.ToLower(strings.TrimSpace(row.Kind)) != "subagent" {
			continue
		}
		items = append(items, item{key: key, updated: row.UpdatedAt})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].updated > items[j].updated })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.key)
	}
	return out, nil
}
