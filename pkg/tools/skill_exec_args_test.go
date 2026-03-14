package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillExecParsesStringArgsList(t *testing.T) {
	workspace := t.TempDir()
	skillDir := filepath.Join(workspace, "skills", "demo")
	scriptDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Demo\n"), 0644); err != nil {
		t.Fatalf("write skill md failed: %v", err)
	}
	scriptPath := filepath.Join(scriptDir, "run.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nprintf \"%s %s\" \"$1\" \"$2\"\n"), 0755); err != nil {
		t.Fatalf("write script failed: %v", err)
	}

	tool := NewSkillExecTool(workspace)
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"skill":  "demo",
		"script": "scripts/run.sh",
		"args":   "hello,world",
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(out, "hello world") {
		t.Fatalf("unexpected output: %s", out)
	}
}
