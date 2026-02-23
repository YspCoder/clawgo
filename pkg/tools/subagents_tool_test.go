package tools

import (
	"context"
	"strings"
	"testing"
)

func TestSubagentsInfoAll(t *testing.T) {
	m := NewSubagentManager(nil, ".", nil, nil)
	m.tasks["subagent-1"] = &SubagentTask{ID: "subagent-1", Status: "completed", Label: "a", Created: 2}
	m.tasks["subagent-2"] = &SubagentTask{ID: "subagent-2", Status: "running", Label: "b", Created: 3}

	tool := NewSubagentsTool(m, "", "")
	out, err := tool.Execute(context.Background(), map[string]interface{}{"action": "info", "id": "all"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Subagents Summary") || !strings.Contains(out, "subagent-2") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestSubagentsKillAll(t *testing.T) {
	m := NewSubagentManager(nil, ".", nil, nil)
	m.tasks["subagent-1"] = &SubagentTask{ID: "subagent-1", Status: "running", Label: "a", Created: 2}
	m.tasks["subagent-2"] = &SubagentTask{ID: "subagent-2", Status: "running", Label: "b", Created: 3}

	tool := NewSubagentsTool(m, "", "")
	out, err := tool.Execute(context.Background(), map[string]interface{}{"action": "kill", "id": "all"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "2") {
		t.Fatalf("unexpected kill output: %s", out)
	}
}
