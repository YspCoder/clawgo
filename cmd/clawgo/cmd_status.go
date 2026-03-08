package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"clawgo/pkg/nodes"
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
		fmt.Printf("Provider API Base: %s\n", activeProvider.APIBase)
		fmt.Printf("Supports /v1/responses/compact: %v\n", providers.ProviderSupportsResponsesCompact(cfg, activeProxyName))
		hasKey := strings.TrimSpace(activeProvider.APIKey) != ""
		status := "not set"
		if hasKey {
			status = "✓"
		}
		fmt.Printf("Provider API Key: %s\n", status)
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
		ns := nodes.DefaultManager().List()
		if len(ns) > 0 {
			online := 0
			caps := map[string]int{"run": 0, "model": 0, "camera": 0, "screen": 0, "location": 0, "canvas": 0}
			for _, n := range ns {
				if n.Online {
					online++
				}
				if n.Capabilities.Run {
					caps["run"]++
				}
				if n.Capabilities.Model {
					caps["model"]++
				}
				if n.Capabilities.Camera {
					caps["camera"]++
				}
				if n.Capabilities.Screen {
					caps["screen"]++
				}
				if n.Capabilities.Location {
					caps["location"]++
				}
				if n.Capabilities.Canvas {
					caps["canvas"]++
				}
			}
			fmt.Printf("Nodes: total=%d online=%d\n", len(ns), online)
			fmt.Printf("Nodes Capabilities: run=%d model=%d camera=%d screen=%d location=%d canvas=%d\n", caps["run"], caps["model"], caps["camera"], caps["screen"], caps["location"], caps["canvas"])
			fmt.Printf("Nodes P2P: enabled=%t transport=%s\n", cfg.Gateway.Nodes.P2P.Enabled, strings.TrimSpace(cfg.Gateway.Nodes.P2P.Transport))
			if total, okCnt, avgMs, actionTop, transportTop, fallbackCnt, err := collectNodeDispatchStats(filepath.Join(workspace, "memory", "nodes-dispatch-audit.jsonl")); err == nil && total > 0 {
				fmt.Printf("Nodes Dispatch: total=%d ok=%d fail=%d avg_ms=%d\n", total, okCnt, total-okCnt, avgMs)
				if actionTop != "" {
					fmt.Printf("Nodes Dispatch Top Action: %s\n", actionTop)
				}
				if transportTop != "" {
					fmt.Printf("Nodes Dispatch Top Transport: %s\n", transportTop)
				}
				if fallbackCnt > 0 {
					fmt.Printf("Nodes Dispatch Fallbacks: %d\n", fallbackCnt)
				}
			}
		}
	}
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

func collectNodeDispatchStats(path string) (int, int, int, string, string, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, 0, "", "", 0, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	total, okCnt, msSum, fallbackCnt := 0, 0, 0, 0
	actionCnt := map[string]int{}
	transportCnt := map[string]int{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row struct {
			Action        string `json:"action"`
			UsedTransport string `json:"used_transport"`
			FallbackFrom  string `json:"fallback_from"`
			OK            bool   `json:"ok"`
			DurationMS    int    `json:"duration_ms"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		total++
		if row.OK {
			okCnt++
		}
		if row.DurationMS > 0 {
			msSum += row.DurationMS
		}
		a := strings.TrimSpace(strings.ToLower(row.Action))
		if a == "" {
			a = "unknown"
		}
		actionCnt[a]++
		used := strings.TrimSpace(strings.ToLower(row.UsedTransport))
		if used != "" {
			transportCnt[used]++
		}
		if strings.TrimSpace(row.FallbackFrom) != "" {
			fallbackCnt++
		}
	}
	avg := 0
	if total > 0 {
		avg = msSum / total
	}
	topAction := ""
	topN := 0
	for k, v := range actionCnt {
		if v > topN {
			topN = v
			topAction = k
		}
	}
	if topAction != "" {
		topAction = fmt.Sprintf("%s(%d)", topAction, topN)
	}
	topTransport := ""
	topTN := 0
	for k, v := range transportCnt {
		if v > topTN {
			topTN = v
			topTransport = k
		}
	}
	if topTransport != "" {
		topTransport = fmt.Sprintf("%s(%d)", topTransport, topTN)
	}
	return total, okCnt, avg, topAction, topTransport, fallbackCnt, nil
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
