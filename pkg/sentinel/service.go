package sentinel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"clawgo/pkg/channels"
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
	mgr             *channels.Manager
	healingChannels map[string]bool
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
		healingChannels: map[string]bool{},
	}
}

func (s *Service) SetManager(mgr *channels.Manager) {
	s.mgr = mgr
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
	issues = append(issues, s.checkChannels()...)

	if len(issues) == 0 {
		return
	}

	for _, issue := range issues {
		s.alert(issue)
	}
}

func (s *Service) checkChannels() []string {
	if s.mgr == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	health := s.mgr.CheckHealth(ctx)
	var issues []string
	for name, err := range health {
		if err != nil {
			msg := fmt.Sprintf("sentinel: channel %s health check failed: %v", name, err)
			issues = append(issues, msg)
			if s.autoHeal {
				s.mu.Lock()
				if s.healingChannels[name] {
					s.mu.Unlock()
					continue
				}
				s.healingChannels[name] = true
				s.mu.Unlock()

				go func(n string) {
					defer func() {
						s.mu.Lock()
						delete(s.healingChannels, n)
						s.mu.Unlock()
					}()
					logger.InfoCF("sentinel", "Attempting auto-heal for channel", map[string]interface{}{"channel": n})
					// Use a fresh context for restart to avoid being canceled by sentinel loop
					if rErr := s.mgr.RestartChannel(context.Background(), n); rErr != nil {
						logger.ErrorCF("sentinel", "Auto-heal restart failed", map[string]interface{}{"channel": n, "error": rErr.Error()})
					} else {
						logger.InfoCF("sentinel", "Auto-heal successful", map[string]interface{}{"channel": n})
					}
				}(name)
			}
		}
	}
	return issues
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
