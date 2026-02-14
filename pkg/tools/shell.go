package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"clawgo/pkg/config"
	"clawgo/pkg/logger"
)

var blockedRootWipePattern = regexp.MustCompile(`(?i)(^|[;&|\n])\s*rm\b[^\n;&|]*\s(?:'/'|"/"|/)(?:\s|$)`)

type ExecTool struct {
	workingDir          string
	timeout             time.Duration
	denyPatterns        []*regexp.Regexp
	allowPatterns       []*regexp.Regexp
	restrictToWorkspace bool
	sandboxEnabled      bool
	sandboxImage        string
	riskCfg             config.RiskConfig
}

func NewExecTool(cfg config.ShellConfig, workspace string) *ExecTool {
	denyPatterns := make([]*regexp.Regexp, 0, len(cfg.DeniedCmds))
	for _, p := range cfg.DeniedCmds {
		denyPatterns = append(denyPatterns, regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(p)+`\b`))
	}

	allowPatterns := make([]*regexp.Regexp, 0, len(cfg.AllowedCmds))
	for _, p := range cfg.AllowedCmds {
		allowPatterns = append(allowPatterns, regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(p)+`\b`))
	}

	allowPatterns := make([]*regexp.Regexp, 0, len(cfg.AllowedCmds))
	for _, p := range cfg.AllowedCmds {
		allowPatterns = append(allowPatterns, regexp.MustCompile(`\b`+regexp.QuoteMeta(p)+`\b`))
	}

	return &ExecTool{
		workingDir:          workspace,
		timeout:             cfg.Timeout,
		denyPatterns:        denyPatterns,
		allowPatterns:       allowPatterns,
		restrictToWorkspace: cfg.RestrictPath,
		sandboxEnabled:      cfg.Sandbox.Enabled,
		sandboxImage:        cfg.Sandbox.Image,
		riskCfg:             cfg.Risk,
	}
}

func (t *ExecTool) Name() string {
	return "exec"
}

func (t *ExecTool) Description() string {
	return "Execute a shell command and return its output. Use with caution."
}

func (t *ExecTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The shell command to execute",
			},
			"working_dir": map[string]interface{}{
				"type":        "string",
				"description": "Optional working directory for the command",
			},
			"force": map[string]interface{}{
				"type":        "boolean",
				"description": "Bypass risk gate for destructive operations (still strongly discouraged).",
			},
		},
		"required": []string{"command"},
	}
}

func (t *ExecTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	command, ok := args["command"].(string)
	if !ok {
		return "", fmt.Errorf("command is required")
	}

	cwd := t.workingDir
	if wd, ok := args["working_dir"].(string); ok && wd != "" {
		cwd = wd
	}

	if cwd == "" {
		wd, err := os.Getwd()
		if err == nil {
			cwd = wd
		}
	}

	if guardError := t.guardCommand(command, cwd); guardError != "" {
		return fmt.Sprintf("Error: %s", guardError), nil
	}

	force, _ := args["force"].(bool)
	if blockMsg, dryRunCmd := t.applyRiskGate(command, force); blockMsg != "" {
		if dryRunCmd != "" {
			dryRunResult, _ := t.executeCommand(ctx, dryRunCmd, cwd)
			return fmt.Sprintf("%s\n\nDry-run command: %s\nDry-run output:\n%s", blockMsg, dryRunCmd, dryRunResult), nil
		}
		return blockMsg, nil
	}

	if t.sandboxEnabled {
		return t.executeInSandbox(ctx, command, cwd)
	}

	return t.executeCommand(ctx, command, cwd)
}

func (t *ExecTool) executeInSandbox(ctx context.Context, command, cwd string) (string, error) {
	// 实现 Docker 沙箱执行逻辑
	absCwd, _ := filepath.Abs(cwd)
	dockerArgs := []string{
		"run", "--rm",
		"-v", fmt.Sprintf("%s:/app", absCwd),
		"-w", "/app",
		t.sandboxImage,
		"sh", "-c", command,
	}

	cmdCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "docker", dockerArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\nSTDERR:\n" + stderr.String()
	}

	if err != nil {
		output += fmt.Sprintf("\nSandbox Exit code: %v", err)
	}

	return output, nil
}

func (t *ExecTool) guardCommand(command, cwd string) string {
	cmd := strings.TrimSpace(command)
	lower := strings.ToLower(cmd)

	if blockedRootWipePattern.MatchString(lower) {
		return "Command blocked by safety guard (removing root path / is forbidden)"
	}

	for _, pattern := range t.denyPatterns {
		if pattern.MatchString(cmd) {
			return "Command blocked by safety guard (dangerous pattern detected)"
		}
	}

	if len(t.allowPatterns) > 0 {
		allowed := false
		for _, pattern := range t.allowPatterns {
			if pattern.MatchString(cmd) {
				allowed = true
				break
			}
		}
		if !allowed {
			return "Command blocked by safety guard (not in allowlist)"
		}
	}

	if t.restrictToWorkspace {
		if strings.Contains(cmd, "..\\") || strings.Contains(cmd, "../") {
			return "Command blocked by safety guard (path traversal detected)"
		}

		cwdPath, err := filepath.Abs(cwd)
		if err != nil {
			return ""
		}

		pathPattern := regexp.MustCompile(`[A-Za-z]:\\[^\\\"']+|/[^\s\"']+`)
		matches := pathPattern.FindAllString(cmd, -1)

		for _, raw := range matches {
			p, err := filepath.Abs(raw)
			if err != nil {
				continue
			}

			rel, err := filepath.Rel(cwdPath, p)
			if err != nil {
				continue
			}

			if strings.HasPrefix(rel, "..") {
				return "Command blocked by safety guard (path outside working dir)"
			}
		}
	}

	return ""
}

func (t *ExecTool) SetTimeout(timeout time.Duration) {
	t.timeout = timeout
}

func (t *ExecTool) SetRestrictToWorkspace(restrict bool) {
	t.restrictToWorkspace = restrict
}

func (t *ExecTool) SetAllowPatterns(patterns []string) error {
	t.allowPatterns = make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile("(?i)" + p)
		if err != nil {
			return fmt.Errorf("invalid allow pattern %q: %w", p, err)
		}
		t.allowPatterns = append(t.allowPatterns, re)
	}
	return nil
}

func (t *ExecTool) applyRiskGate(command string, force bool) (string, string) {
	if !t.riskCfg.Enabled {
		return "", ""
	}

	assessment := assessCommandRisk(command)
	logger.InfoCF("risk", "Command risk assessed", map[string]interface{}{
		"level":   assessment.Level,
		"command": truncateCmd(command, 200),
		"reasons": assessment.Reasons,
	})

	if assessment.Level != RiskDestructive {
		return "", ""
	}

	if t.riskCfg.RequireForceFlag && !force {
		msg := "Error: destructive command blocked by risk gate. Re-run with force=true if intentional."
		if t.riskCfg.RequireDryRun {
			if dryRunCmd, ok := buildDryRunCommand(command); ok {
				return msg, dryRunCmd
			}
		}
		return msg, ""
	}

	if !t.riskCfg.AllowDestructive {
		return "Error: destructive command is disabled by policy (tools.shell.risk.allow_destructive=false).", ""
	}

	if t.riskCfg.RequireDryRun && !force {
		if dryRunCmd, ok := buildDryRunCommand(command); ok {
			return "Risk gate: dry-run required first. Review output, then execute intentionally with force=true.", dryRunCmd
		}
		return "Error: destructive command requires explicit force=true because no dry-run strategy is available.", ""
	}
	return "", ""
}

func (t *ExecTool) executeCommand(ctx context.Context, command, cwd string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)
	if cwd != "" {
		cmd.Dir = cwd
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\nSTDERR:\n" + stderr.String()
	}

	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			return fmt.Sprintf("Error: Command timed out after %v", t.timeout), nil
		}
		output += fmt.Sprintf("\nExit code: %v", err)
	}

	if output == "" {
		output = "(no output)"
	}

	maxLen := 10000
	if len(output) > maxLen {
		output = output[:maxLen] + fmt.Sprintf("\n... (truncated, %d more chars)", len(output)-maxLen)
	}
	return output, nil
}

func truncateCmd(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
