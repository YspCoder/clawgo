package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type TriggerStats struct {
	UpdatedAt string         `json:"updated_at"`
	Counts    map[string]int `json:"counts"`
}

type triggerAudit struct {
	workspace string
	mu        sync.Mutex
}

type triggerEvent struct {
	Time      string `json:"time"`
	Trigger   string `json:"trigger"`
	Channel   string `json:"channel"`
	Session   string `json:"session"`
	Suppressed bool   `json:"suppressed,omitempty"`
	Error     string `json:"error,omitempty"`
}

func newTriggerAudit(workspace string) *triggerAudit {
	return &triggerAudit{workspace: workspace}
}

func (ta *triggerAudit) Record(trigger, channel, session string, suppressed bool, err error) {
	if ta == nil {
		return
	}
	trigger = normalizeTrigger(trigger)
	e := triggerEvent{
		Time:      time.Now().UTC().Format(time.RFC3339),
		Trigger:   trigger,
		Channel:   channel,
		Session:   session,
		Suppressed: suppressed,
	}
	if err != nil {
		e.Error = err.Error()
	}

	ta.mu.Lock()
	defer ta.mu.Unlock()

	memDir := filepath.Join(ta.workspace, "memory")
	_ = os.MkdirAll(memDir, 0755)

	logPath := filepath.Join(memDir, "trigger-audit.jsonl")
	if data, mErr := json.Marshal(e); mErr == nil {
		f, oErr := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if oErr == nil {
			_, _ = f.Write(append(data, '\n'))
			_ = f.Close()
		}
	}

	statsPath := filepath.Join(memDir, "trigger-stats.json")
	stats := TriggerStats{Counts: map[string]int{}}
	if raw, rErr := os.ReadFile(statsPath); rErr == nil {
		_ = json.Unmarshal(raw, &stats)
		if stats.Counts == nil {
			stats.Counts = map[string]int{}
		}
	}
	stats.Counts[trigger]++
	stats.UpdatedAt = e.Time
	if raw, mErr := json.MarshalIndent(stats, "", "  "); mErr == nil {
		_ = os.WriteFile(statsPath, raw, 0644)
	}
}

func normalizeTrigger(v string) string {
	s := v
	if s == "" {
		return "user"
	}
	return s
}
