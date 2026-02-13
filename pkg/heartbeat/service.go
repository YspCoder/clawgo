package heartbeat

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"clawgo/pkg/lifecycle"
)

type HeartbeatService struct {
	workspace   string
	onHeartbeat func(string) (string, error)
	interval    time.Duration
	enabled     bool
	runner      *lifecycle.LoopRunner
}

func NewHeartbeatService(workspace string, onHeartbeat func(string) (string, error), intervalS int, enabled bool) *HeartbeatService {
	return &HeartbeatService{
		workspace:   workspace,
		onHeartbeat: onHeartbeat,
		interval:    time.Duration(intervalS) * time.Second,
		enabled:     enabled,
		runner:      lifecycle.NewLoopRunner(),
	}
}

func (hs *HeartbeatService) Start() error {
	if !hs.enabled {
		return fmt.Errorf("heartbeat service is disabled")
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
	notesDir := filepath.Join(hs.workspace, "memory")
	notesFile := filepath.Join(notesDir, "HEARTBEAT.md")

	var notes string
	if data, err := os.ReadFile(notesFile); err == nil {
		notes = string(data)
	}

	now := time.Now().Format("2006-01-02 15:04")

	prompt := fmt.Sprintf(`# Heartbeat Check

Current time: %s

Check if there are any tasks I should be aware of or actions I should take.
Review the memory file for any important updates or changes.
Be proactive in identifying potential issues or improvements.

%s
`, now, notes)

	return prompt
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
