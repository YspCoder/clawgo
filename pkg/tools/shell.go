package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/YspCoder/clawgo/pkg/config"
)

type ExecTool struct {
	workingDir         string
	timeout            time.Duration
	sandboxEnabled     bool
	sandboxImage       string
	autoInstallMissing bool
	procManager        *ProcessManager
}

func NewExecTool(cfg config.ShellConfig, workspace string, pm *ProcessManager) *ExecTool {
	return &ExecTool{
		workingDir:         workspace,
		timeout:            cfg.Timeout,
		sandboxEnabled:     cfg.Sandbox.Enabled,
		sandboxImage:       cfg.Sandbox.Image,
		autoInstallMissing: cfg.AutoInstallMissing,
		procManager:        pm,
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
			"background": map[string]interface{}{
				"type":        "boolean",
				"description": "Run command in background and return session id",
			},
		},
		"required": []string{"command"},
	}
}

func (t *ExecTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	command := MapRawStringArg(args, "command")
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command is required")
	}

	cwd := t.workingDir
	if wd := MapStringArg(args, "working_dir"); wd != "" {
		cwd = wd
	}

	if cwd == "" {
		wd, err := os.Getwd()
		if err == nil {
			cwd = wd
		}
	}
	if bg, _ := MapBoolArg(args, "background"); bg {
		if t.procManager == nil {
			return "", fmt.Errorf("background process manager not configured")
		}
		sid, err := t.procManager.Start(ctx, command, cwd)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("{\"session_id\":%q,\"running\":true}", sid), nil
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
		"--privileged",
		"--user", "0:0",
		"-v", fmt.Sprintf("%s:/app:rw", absCwd),
		"-w", "/app",
		t.sandboxImage,
		"sh", "-c", command,
	}
	cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	var stdout, stderr trackedOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String()
	if stderr.Len() > 0 {
		out += "\nSTDERR:\n" + stderr.String()
	}
	if err != nil {
		if strings.TrimSpace(out) != "" {
			out += "\n"
		}
		out += fmt.Sprintf("Sandbox Exit code: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		out = "(no output)"
	}
	return out, nil
}

func (t *ExecTool) SetTimeout(timeout time.Duration) {
	t.timeout = timeout
}

func (t *ExecTool) executeCommand(ctx context.Context, command, cwd string) (string, error) {
	output, err := t.runShellCommand(ctx, command, cwd)

	if err != nil && t.autoInstallMissing {
		if missingCmd := detectMissingCommandFromOutput(output); missingCmd != "" {
			if installLog, installed := t.tryAutoInstallMissingCommand(ctx, missingCmd, cwd); installed {
				output += "\n[AUTO-INSTALL]\n" + installLog
				retryOutput, retryErr := t.runShellCommand(ctx, command, cwd)
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

func (t *ExecTool) runShellCommand(ctx context.Context, command, cwd string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = buildExecEnv()
	if cwd != "" {
		cmd.Dir = cwd
	}

	var stdout, stderr trackedOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := stdout.String()
	if stderr.Len() > 0 {
		out += "\nSTDERR:\n" + stderr.String()
	}
	return out, err
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

	for _, installCmd := range candidates {
		output, err := t.runShellCommand(ctx, installCmd, cwd)
		if err == nil && commandExists(name) {
			return fmt.Sprintf("Installed %s using: %s\n%s", name, installCmd, output), true
		}
	}
	return fmt.Sprintf("Failed to auto-install missing command: %s", name), false
}
