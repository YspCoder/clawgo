package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillExecWriteAuditIncludesCallerIdentity(t *testing.T) {
	workspace := t.TempDir()
	tool := NewSkillExecTool(workspace)

	tool.writeAudit("demo", "scripts/run.sh", "test", "coder", "agent", true, "")

	data, err := os.ReadFile(filepath.Join(workspace, "memory", "skill-audit.jsonl"))
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"caller_agent":"coder"`) {
		t.Fatalf("expected caller_agent in audit row, got: %s", text)
	}
	if !strings.Contains(text, `"caller_scope":"agent"`) {
		t.Fatalf("expected caller_scope in audit row, got: %s", text)
	}
}
