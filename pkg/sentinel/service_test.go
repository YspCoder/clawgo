package sentinel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/config"
)

func TestCheckConfigReportsMissingAndCorruptConfig(t *testing.T) {
	s := NewService(filepath.Join(t.TempDir(), "missing.json"), t.TempDir(), 60, false, nil)
	issues := s.checkConfig()
	if len(issues) != 1 || !strings.Contains(issues[0], "config file missing") {
		t.Fatalf("missing config issues = %+v", issues)
	}

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte("{bad-json"), 0644); err != nil {
		t.Fatalf("write corrupt config: %v", err)
	}
	s = NewService(cfgPath, t.TempDir(), 60, false, nil)
	issues = s.checkConfig()
	if len(issues) != 1 || !strings.Contains(issues[0], "config parse failed") {
		t.Fatalf("corrupt config issues = %+v", issues)
	}
}

func TestCheckMemoryReportsMissingAndAutoHeals(t *testing.T) {
	workspace := t.TempDir()
	s := NewService(validConfigFile(t, t.TempDir()), workspace, 60, false, nil)

	issues := s.checkMemory()
	if len(issues) != 1 || !strings.Contains(issues[0], "memory dir missing") {
		t.Fatalf("missing memory issues = %+v", issues)
	}

	s = NewService(validConfigFile(t, t.TempDir()), workspace, 60, true, nil)
	issues = s.checkMemory()
	if len(issues) != 1 || !strings.Contains(issues[0], "auto-healed") {
		t.Fatalf("auto-heal memory issues = %+v", issues)
	}
	if _, err := os.Stat(filepath.Join(workspace, "memory")); err != nil {
		t.Fatalf("memory dir was not created: %v", err)
	}

	issues = s.checkMemory()
	if len(issues) != 1 || !strings.Contains(issues[0], "MEMORY.md missing, auto-healed") {
		t.Fatalf("auto-heal MEMORY.md issues = %+v", issues)
	}
	if _, err := os.Stat(filepath.Join(workspace, "memory", "MEMORY.md")); err != nil {
		t.Fatalf("MEMORY.md was not created: %v", err)
	}
}

func TestCheckLogsReportsMissingLogDirAndAutoHeals(t *testing.T) {
	root := t.TempDir()
	logDir := filepath.Join(root, "logs")
	cfgPath := validConfigFileWithLogDir(t, t.TempDir(), logDir)

	s := NewService(cfgPath, t.TempDir(), 60, false, nil)
	issues := s.checkLogs()
	if len(issues) != 1 || !strings.Contains(issues[0], "log dir missing") {
		t.Fatalf("missing log dir issues = %+v", issues)
	}

	s = NewService(cfgPath, t.TempDir(), 60, true, nil)
	issues = s.checkLogs()
	if len(issues) != 1 || !strings.Contains(issues[0], "auto-healed") {
		t.Fatalf("auto-heal log dir issues = %+v", issues)
	}
	if _, err := os.Stat(logDir); err != nil {
		t.Fatalf("log dir was not created: %v", err)
	}
}

func TestCheckChannelsReportsHealthFailuresAndRestartsWhenAutoHealEnabled(t *testing.T) {
	mgr := &fakeHealthManager{health: map[string]error{"telegram": errors.New("offline")}}
	s := NewService(validConfigFile(t, t.TempDir()), t.TempDir(), 60, true, nil)
	s.mgr = mgr

	issues := s.checkChannels()
	if len(issues) != 1 || !strings.Contains(issues[0], "telegram health check failed") {
		t.Fatalf("channel issues = %+v", issues)
	}
	waitForRestarts(t, &mgr.restarts, 1)
}

func TestRunChecksCallsAlertCallbackAndSuppressesDuplicates(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "missing.json")
	workspace := t.TempDir()
	var alerts int64
	s := NewService(cfgPath, workspace, 60, false, func(msg string) {
		atomic.AddInt64(&alerts, 1)
	})

	s.runChecks()
	if got := atomic.LoadInt64(&alerts); got == 0 {
		t.Fatal("runChecks did not call alert callback")
	}
	first := atomic.LoadInt64(&alerts)
	s.runChecks()
	if got := atomic.LoadInt64(&alerts); got != first {
		t.Fatalf("duplicate alerts = %d, want suppressed at %d", got, first)
	}
}

func TestStartStopLifecycle(t *testing.T) {
	s := NewService(validConfigFile(t, t.TempDir()), t.TempDir(), 3600, false, nil)
	s.Start()
	if !s.runner.Running() {
		t.Fatal("Start did not mark service running")
	}
	s.Stop()
	if s.runner.Running() {
		t.Fatal("Stop left service running")
	}
}

type fakeHealthManager struct {
	health   map[string]error
	restarts int64
}

func (m *fakeHealthManager) CheckHealth(ctx context.Context) map[string]error {
	return m.health
}

func (m *fakeHealthManager) RestartChannel(ctx context.Context, name string) error {
	atomic.AddInt64(&m.restarts, 1)
	return nil
}

func waitForRestarts(t *testing.T, restarts *int64, want int64) {
	t.Helper()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(restarts) >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("restarts = %d, want at least %d", atomic.LoadInt64(restarts), want)
}

func validConfigFile(t *testing.T, dir string) string {
	t.Helper()
	return validConfigFileWithLogDir(t, dir, filepath.Join(dir, "logs"))
}

func validConfigFileWithLogDir(t *testing.T, dir, logDir string) string {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Logging.Enabled = true
	cfg.Logging.Dir = logDir
	cfg.Logging.Filename = "clawgo.log"
	cfg.Agents.Defaults.Workspace = filepath.Join(dir, "workspace")
	cfgPath := filepath.Join(dir, "config.json")
	if err := config.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return cfgPath
}
