package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
	}
}

func collectSessionKindCounts(sessionsDir string) (map[string]int, error) {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".meta") {
			continue
		}
		metaPath := filepath.Join(sessionsDir, e.Name())
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var meta struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		kind := strings.TrimSpace(strings.ToLower(meta.Kind))
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

func collectRecentSubagentSessions(sessionsDir string, limit int) ([]string, error) {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return nil, err
	}
	type item struct {
		key     string
		updated int64
	}
	items := make([]item, 0)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".meta") {
			continue
		}
		metaPath := filepath.Join(sessionsDir, e.Name())
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var meta struct {
			Kind    string `json:"kind"`
			Updated string `json:"updated"`
		}
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(meta.Kind)) != "subagent" {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, meta.Updated)
		if err != nil {
			t, _ = time.Parse(time.RFC3339, meta.Updated)
		}
		items = append(items, item{key: strings.TrimSuffix(e.Name(), ".meta"), updated: t.UnixMilli()})
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
