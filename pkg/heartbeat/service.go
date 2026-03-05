package heartbeat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"clawgo/pkg/lifecycle"
)

type HeartbeatService struct {
	workspace      string
	onHeartbeat    func(string) (string, error)
	interval       time.Duration
	enabled        bool
	promptTemplate string
	runner         *lifecycle.LoopRunner
}

func NewHeartbeatService(workspace string, onHeartbeat func(string) (string, error), intervalS int, enabled bool, promptTemplate string) *HeartbeatService {
	return &HeartbeatService{
		workspace:      workspace,
		onHeartbeat:    onHeartbeat,
		interval:       time.Duration(intervalS) * time.Second,
		enabled:        enabled,
		promptTemplate: strings.TrimSpace(promptTemplate),
		runner:         lifecycle.NewLoopRunner(),
	}
}

func (hs *HeartbeatService) Start() error {
	if !hs.enabled {
		return nil
	}
	hs.runner.Start(hs.runLoop)
	return nil
}

func (hs *HeartbeatService) Stop() {
	hs.runner.Stop()
}

func (hs *HeartbeatService) running() bool {
	return hs.runner.Running()
}

func (hs *HeartbeatService) runLoop(stopCh <-chan struct{}) {
	ticker := time.NewTicker(hs.interval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			hs.checkHeartbeat()
		}
	}
}

func (hs *HeartbeatService) checkHeartbeat() {
	if !hs.enabled || !hs.running() {
		return
	}

	prompt := hs.buildPrompt()

	if hs.onHeartbeat != nil {
		_, err := hs.onHeartbeat(prompt)
		if err != nil {
			hs.log(fmt.Sprintf("Heartbeat error: %v", err))
		}
	}
}

func (hs *HeartbeatService) buildPrompt() string {
	notesFile := filepath.Join(hs.workspace, "HEARTBEAT.md")
	agentsFile := filepath.Join(hs.workspace, "AGENTS.md")

	var notes string
	if data, err := os.ReadFile(notesFile); err == nil {
		candidate := string(data)
		if !isEffectivelyEmptyMarkdown(candidate) {
			notes = candidate
		}
	}
	agents := ""
	if data, err := os.ReadFile(agentsFile); err == nil {
		agents = strings.TrimSpace(string(data))
	}
	ackToken := heartbeatAckTokenFromText(agents)
	if ackToken == "" {
		ackToken = heartbeatAckTokenFromText(notes)
	}

	now := time.Now().Format("2006-01-02 15:04")

	tpl := hs.promptTemplate
	if strings.TrimSpace(tpl) == "" {
		if strings.TrimSpace(ackToken) != "" {
			tpl = fmt.Sprintf("Follow workspace policy in AGENTS.md first, then HEARTBEAT.md. If no action is needed, return %s.", ackToken)
		} else {
			tpl = "Follow workspace policy in AGENTS.md first, then HEARTBEAT.md."
		}
	}
	prompt := fmt.Sprintf("%s\n\nCurrent time: %s\n\n## AGENTS.md\n%s\n\n## HEARTBEAT.md\n%s\n", tpl, now, agents, notes)

	return prompt
}

func heartbeatAckTokenFromText(text string) string {
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		raw := strings.TrimLeft(t, "-*# ")
		lower := strings.ToLower(raw)
		if !strings.HasPrefix(lower, "heartbeat_ack_token:") {
			continue
		}
		v := strings.TrimSpace(raw[len("heartbeat_ack_token:"):])
		v = strings.Trim(v, "`\"' ")
		if v != "" {
			return v
		}
	}
	return ""
}

func (hs *HeartbeatService) log(message string) {
	logFile := filepath.Join(hs.workspace, "memory", "heartbeat.log")
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	f.WriteString(fmt.Sprintf("[%s] %s\n", timestamp, message))
}

func isEffectivelyEmptyMarkdown(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "#") {
			continue
		}
		return false
	}
	return true
}
