package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"clawgo/pkg/config"
)

type ExecTool struct {
	workingDir     string
	timeout        time.Duration
	sandboxEnabled bool
	sandboxImage   string
}

func NewExecTool(cfg config.ShellConfig, workspace string) *ExecTool {
	return &ExecTool{
		workingDir:     workspace,
		timeout:        cfg.Timeout,
		sandboxEnabled: cfg.Sandbox.Enabled,
		sandboxImage:   cfg.Sandbox.Image,
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

	if t.sandboxEnabled {
		return t.executeInSandbox(ctx, command, cwd)
	}

	return t.executeCommand(ctx, command, cwd)
}

func (t *ExecTool) executeInSandbox(ctx context.Context, command, cwd string) (string, error) {
	// Execute command inside Docker sandbox
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

func (t *ExecTool) SetTimeout(timeout time.Duration) {
	t.timeout = timeout
}

func (t *ExecTool) executeCommand(ctx context.Context, command, cwd string) (string, error) {
	output, err, timedOut := t.runShellCommand(ctx, command, cwd)
	if timedOut {
		return fmt.Sprintf("Error: Command timed out after %v", t.timeout), nil
	}

	if err != nil {
		if missingCmd := detectMissingCommandFromOutput(output); missingCmd != "" {
			if installLog, installed := t.tryAutoInstallMissingCommand(ctx, missingCmd, cwd); installed {
				output += "\n[AUTO-INSTALL]\n" + installLog
				retryOutput, retryErr, retryTimedOut := t.runShellCommand(ctx, command, cwd)
				if retryTimedOut {
					return fmt.Sprintf("Error: Command timed out after %v", t.timeout), nil
				}
				output += "\n[RETRY]\n" + retryOutput
				err = retryErr
			}
		}
	}

	if err != nil {
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

func (t *ExecTool) runShellCommand(ctx context.Context, command, cwd string) (string, error, bool) {
	cmdCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)
	cmd.Env = buildExecEnv()
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
			return output, err, true
		}
	}
	return output, err, false
}

func buildExecEnv() []string {
	env := os.Environ()
	current := os.Getenv("PATH")
	fallback := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/opt/homebrew/bin:/opt/homebrew/sbin"
	if strings.TrimSpace(current) == "" {
		return append(env, "PATH="+fallback)
	}
	// Append common paths to reduce false "command not found" in service/daemon envs.
	return append(env, "PATH="+current+":"+fallback)
}

func detectMissingCommandFromOutput(output string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?m)(?:^|[:\s])([a-zA-Z0-9._+-]+): not found`),
		regexp.MustCompile(`(?m)(?:^|[:\s])([a-zA-Z0-9._+-]+): command not found`),
	}
	for _, p := range patterns {
		match := p.FindStringSubmatch(output)
		if len(match) >= 2 && strings.TrimSpace(match[1]) != "" {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

func commandExists(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	_, err := exec.LookPath(name)
	return err == nil
}

func buildInstallCommandCandidates(commandName string) []string {
	cmd := strings.TrimSpace(commandName)
	if cmd == "" {
		return nil
	}
	type pkgTool struct {
		bin  string
		cmd  string
		sudo bool
	}
	candidates := []pkgTool{
		{bin: "apt-get", cmd: "apt-get update && apt-get install -y %s", sudo: true},
		{bin: "dnf", cmd: "dnf install -y %s", sudo: true},
		{bin: "yum", cmd: "yum install -y %s", sudo: true},
		{bin: "apk", cmd: "apk add --no-cache %s", sudo: true},
		{bin: "pacman", cmd: "pacman -Sy --noconfirm %s", sudo: true},
		{bin: "zypper", cmd: "zypper --non-interactive install %s", sudo: true},
		{bin: "brew", cmd: "brew install %s", sudo: false},
	}

	var out []string
	for _, c := range candidates {
		if !commandExists(c.bin) {
			continue
		}
		installCmd := fmt.Sprintf(c.cmd, cmd)
		if c.sudo && runtime.GOOS != "windows" && os.Geteuid() != 0 && commandExists("sudo") {
			installCmd = "sudo " + installCmd
		}
		out = append(out, installCmd)
	}
	return out
}

func (t *ExecTool) tryAutoInstallMissingCommand(ctx context.Context, commandName, cwd string) (string, bool) {
	name := strings.TrimSpace(commandName)
	if name == "" || commandExists(name) {
		return "", false
	}
	candidates := buildInstallCommandCandidates(name)
	if len(candidates) == 0 {
		return fmt.Sprintf("No supported package manager found to install missing command: %s", name), false
	}

	timeout := 5 * time.Minute
	if t.timeout > 0 && t.timeout < timeout {
		timeout = t.timeout
	}

	for _, installCmd := range candidates {
		installCtx, cancel := context.WithTimeout(ctx, timeout)
		output, err, timedOut := t.runShellCommand(installCtx, installCmd, cwd)
		cancel()

		if timedOut {
			continue
		}
		if err == nil && commandExists(name) {
			return fmt.Sprintf("Installed %s using: %s\n%s", name, installCmd, output), true
		}
	}
	return fmt.Sprintf("Failed to auto-install missing command: %s", name), false
}
