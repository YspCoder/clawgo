package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawgo/pkg/tools"
)

func TestBuildSubagentTaskInputPrefersPromptFile(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "agents", "coder"), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "agents", "coder", "AGENT.md"), []byte("coder-file-policy"), 0644); err != nil {
		t.Fatalf("write AGENT failed: %v", err)
	}
	loop := &AgentLoop{workspace: workspace}
	input := loop.buildSubagentTaskInput(&tools.SubagentTask{
		Task:             "implement login flow",
		SystemPrompt:     "inline-fallback",
		SystemPromptFile: "agents/coder/AGENT.md",
	})
	if !strings.Contains(input, "coder-file-policy") {
		t.Fatalf("expected prompt file content, got: %s", input)
	}
	if strings.Contains(input, "inline-fallback") {
		t.Fatalf("expected file prompt to take precedence, got: %s", input)
	}
}

func TestBuildSubagentTaskInputFallsBackToInlinePrompt(t *testing.T) {
	loop := &AgentLoop{workspace: t.TempDir()}
	input := loop.buildSubagentTaskInput(&tools.SubagentTask{
		Task:         "run regression",
		SystemPrompt: "test inline prompt",
	})
	if !strings.Contains(input, "test inline prompt") {
		t.Fatalf("expected inline prompt in task input, got: %s", input)
	}
}
