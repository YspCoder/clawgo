package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YspCoder/clawgo/pkg/tools"
)

func TestBuildSubagentRunInputPrefersPromptFile(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "agents", "coder"), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "agents", "coder", "AGENT.md"), []byte("coder-file-policy"), 0644); err != nil {
		t.Fatalf("write AGENT failed: %v", err)
	}
	loop := &AgentLoop{workspace: workspace}
	input := loop.buildSubagentRunInput(&tools.SubagentRun{
		Task:             "implement login flow",
		SystemPromptFile: "agents/coder/AGENT.md",
	})
	if !strings.Contains(input, "coder-file-policy") {
		t.Fatalf("expected prompt file content, got: %s", input)
	}
	if strings.Contains(input, "inline-fallback") {
		t.Fatalf("expected file prompt to take precedence, got: %s", input)
	}
}

func TestBuildSubagentRunInputWithoutPromptFileUsesTaskOnly(t *testing.T) {
	loop := &AgentLoop{workspace: t.TempDir()}
	input := loop.buildSubagentRunInput(&tools.SubagentRun{
		Task: "run regression",
	})
	if strings.Contains(input, "test inline prompt") {
		t.Fatalf("did not expect inline prompt fallback, got: %s", input)
	}
	if !strings.Contains(input, "run regression") {
		t.Fatalf("expected task input to contain task, got: %s", input)
	}
}
