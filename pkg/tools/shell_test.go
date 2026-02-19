package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"clawgo/pkg/config"
)

func TestExecToolExecuteBasicCommand(t *testing.T) {
	tool := NewExecTool(config.ShellConfig{Timeout: 2 * time.Second}, ".")
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "echo hello",
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("expected output to contain hello, got %q", out)
	}
}

func TestExecToolExecuteTimeout(t *testing.T) {
	tool := NewExecTool(config.ShellConfig{Timeout: 20 * time.Millisecond}, ".")
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "sleep 1",
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(out, "timed out") {
		t.Fatalf("expected timeout message, got %q", out)
	}
}
