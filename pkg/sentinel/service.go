package sentinel

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"clawgo/pkg/config"
	"clawgo/pkg/lifecycle"
	"clawgo/pkg/logger"
)

type AlertFunc func(msg string)

type Service struct {
	cfgPath    string
	workspace  string
	interval   time.Duration
	autoHeal   bool
	onAlert    AlertFunc
	runner     *lifecycle.LoopRunner
	mu         sync.RWMutex
	lastAlerts map[string]time.Time
}

func NewService(cfgPath, workspace string, intervalSec int, autoHeal bool, onAlert AlertFunc) *Service {
	if intervalSec <= 0 {
		intervalSec = 60
	}
	return &Service{
		cfgPath:    cfgPath,
		workspace:  workspace,
		interval:   time.Duration(intervalSec) * time.Second,
		autoHeal:   autoHeal,
		onAlert:    onAlert,
		runner:     lifecycle.NewLoopRunner(),
		lastAlerts: map[string]time.Time{},
	}
}

func (s *Service) Start() {
	if !s.runner.Start(s.loop) {
		return
	}
	logger.InfoCF("sentinel", "Sentinel started", map[string]interface{}{
		"interval":  s.interval.String(),
		"auto_heal": s.autoHeal,
	})
}

func (s *Service) Stop() {
	if !s.runner.Stop() {
		return
	}
	logger.InfoC("sentinel", "Sentinel stopped")
}

func (s *Service) loop(stopCh <-chan struct{}) {
	tk := time.NewTicker(s.interval)
	defer tk.Stop()

	s.runChecks()
	for {
		select {
		case <-stopCh:
			return
		case <-tk.C:
			s.runChecks()
		}
	}
}

func (s *Service) runChecks() {
	issues := s.checkConfig()
	issues = append(issues, s.checkMemory()...)
	issues = append(issues, s.checkLogs()...)

	if len(issues) == 0 {
		return
	}

	for _, issue := range issues {
		s.alert(issue)
	}
}

func (s *Service) checkConfig() []string {
	_, err := os.Stat(s.cfgPath)
	if err != nil {
		return []string{fmt.Sprintf("sentinel: config file missing: %s", s.cfgPath)}
	}

	cfg, err := config.LoadConfig(s.cfgPath)
	if err != nil {
		return []string{fmt.Sprintf("sentinel: config parse failed: %v", err)}
	}

	verrs := config.Validate(cfg)
	out := make([]string, 0, len(verrs))
	for _, e := range verrs {
		out = append(out, fmt.Sprintf("sentinel: config validation issue: %v", e))
	}
	return out
}

func (s *Service) checkMemory() []string {
	memoryDir := filepath.Join(s.workspace, "memory")
	memoryFile := filepath.Join(memoryDir, "MEMORY.md")

	if _, err := os.Stat(memoryDir); err != nil {
		if s.autoHeal {
			if mkErr := os.MkdirAll(memoryDir, 0755); mkErr == nil {
				return []string{"sentinel: memory dir missing, auto-healed"}
			}
		}
		return []string{fmt.Sprintf("sentinel: memory dir missing: %s", memoryDir)}
	}

	if _, err := os.Stat(memoryFile); err != nil {
		if s.autoHeal {
			content := "# Long-term Memory\n\n(auto-created by sentinel)\n"
			if wrErr := os.WriteFile(memoryFile, []byte(content), 0644); wrErr == nil {
				return []string{"sentinel: MEMORY.md missing, auto-healed"}
			}
		}
		return []string{fmt.Sprintf("sentinel: MEMORY.md missing: %s", memoryFile)}
	}
	return nil
}

func (s *Service) checkLogs() []string {
	cfg, err := config.LoadConfig(s.cfgPath)
	if err != nil || !cfg.Logging.Enabled {
		return nil
	}
	logDir := filepath.Clean(filepath.Dir(cfg.LogFilePath()))
	if _, err := os.Stat(logDir); err != nil {
		if s.autoHeal {
			if mkErr := os.MkdirAll(logDir, 0755); mkErr == nil {
				return []string{"sentinel: log dir missing, auto-healed"}
			}
		}
		return []string{fmt.Sprintf("sentinel: log dir missing: %s", logDir)}
	}
	return nil
}

func (s *Service) alert(msg string) {
	now := time.Now()
	s.mu.Lock()
	last, ok := s.lastAlerts[msg]
	if ok && now.Sub(last) < 5*time.Minute {
		s.mu.Unlock()
		return
	}
	s.lastAlerts[msg] = now
	s.mu.Unlock()

	logger.WarnCF("sentinel", msg, nil)
	if s.onAlert != nil {
		s.onAlert(msg)
	}
}
