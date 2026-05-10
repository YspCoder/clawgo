package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/YspCoder/clawgo/pkg/agent"
	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/logger"
)

func runGatewayStartupCompactionCheck(parent context.Context, agentLoop *agent.AgentLoop) {
	if agentLoop == nil {
		return
	}

	checkCtx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()

	report := agentLoop.RunStartupSelfCheckAllSessions(checkCtx)
	logger.InfoCF("gateway", logger.C0110, map[string]interface{}{
		"sessions_total":     report.TotalSessions,
		"sessions_compacted": report.CompactedSessions,
	})
}

func runGatewayBootstrapInit(parent context.Context, cfg *config.Config, agentLoop *agent.AgentLoop) {
	if agentLoop == nil || cfg == nil {
		return
	}
	workspace := cfg.WorkspacePath()
	bootstrapPath := filepath.Join(workspace, "BOOTSTRAP.md")
	if _, err := os.Stat(bootstrapPath); err != nil {
		return
	}
	memDir := filepath.Join(workspace, "memory")
	_ = os.MkdirAll(memDir, 0755)
	markerPath := filepath.Join(memDir, "bootstrap.init.done")
	if _, err := os.Stat(markerPath); err == nil {
		return
	}

	initCtx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	prompt := "System startup bootstrap: read BOOTSTRAP.md and perform one-time self-initialization checks now. If already initialized, return concise status only."
	resp, err := agentLoop.ProcessDirect(initCtx, prompt, "system:bootstrap:init")
	if err != nil {
		logger.ErrorCF("gateway", logger.C0111, map[string]interface{}{logger.FieldError: err.Error()})
		return
	}
	line := fmt.Sprintf("%s\n%s\n", time.Now().UTC().Format(time.RFC3339), strings.TrimSpace(resp))
	if err := os.WriteFile(markerPath, []byte(line), 0644); err != nil {
		logger.ErrorCF("gateway", logger.C0112, map[string]interface{}{logger.FieldError: err.Error()})
		return
	}
	// Bootstrap only runs once. After successful initialization marker is written,
	// remove BOOTSTRAP.md to avoid repeated first-run guidance.
	if err := os.Remove(bootstrapPath); err != nil && !os.IsNotExist(err) {
		logger.WarnCF("gateway", logger.C0113, map[string]interface{}{logger.FieldError: err.Error()})
	}
	logger.InfoC("gateway", logger.C0114)
}

func applyMaximumPermissionPolicy(cfg *config.Config) {
	cfg.Tools.Shell.Enabled = true
	cfg.Tools.Shell.Sandbox.Enabled = false
}
