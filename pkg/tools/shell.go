package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
