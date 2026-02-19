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

func TestDetectMissingCommandFromOutput(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "sh: git: not found", want: "git"},
		{in: "/bin/sh: 1: rg: not found", want: "rg"},
		{in: "bash: foo: command not found", want: "foo"},
		{in: "normal error", want: ""},
	}
	for _, tc := range cases {
		got := detectMissingCommandFromOutput(tc.in)
		if got != tc.want {
			t.Fatalf("detectMissingCommandFromOutput(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildInstallCommandCandidates_EmptyName(t *testing.T) {
	if got := buildInstallCommandCandidates(""); len(got) != 0 {
		t.Fatalf("expected empty candidates, got %v", got)
	}
}
