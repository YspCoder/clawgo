package agent

import (
	"context"
	"testing"
)

func TestEnsureToolAllowedByContext(t *testing.T) {
	ctx := context.Background()

	if err := ensureToolAllowedByContext(ctx, "write_file", map[string]interface{}{}); err != nil {
		t.Fatalf("expected unrestricted context to allow tool, got: %v", err)
	}

	restricted := withToolAllowlistContext(ctx, []string{"read_file", "memory_search"})
	if err := ensureToolAllowedByContext(restricted, "read_file", map[string]interface{}{}); err != nil {
		t.Fatalf("expected allowed tool to pass, got: %v", err)
	}
	if err := ensureToolAllowedByContext(restricted, "write_file", map[string]interface{}{}); err == nil {
		t.Fatalf("expected disallowed tool to fail")
	}
	if err := ensureToolAllowedByContext(restricted, "skill_exec", map[string]interface{}{}); err != nil {
		t.Fatalf("expected skill_exec to bypass subagent allowlist, got: %v", err)
	}
}

func TestEnsureToolAllowedByContextParallelNested(t *testing.T) {
	restricted := withToolAllowlistContext(context.Background(), []string{"parallel", "read_file"})

	okArgs := map[string]interface{}{
		"calls": []interface{}{
			map[string]interface{}{"tool": "read_file", "arguments": map[string]interface{}{"path": "README.md"}},
		},
	}
	if err := ensureToolAllowedByContext(restricted, "parallel", okArgs); err != nil {
		t.Fatalf("expected parallel with allowed nested tool to pass, got: %v", err)
	}

	badArgs := map[string]interface{}{
		"calls": []interface{}{
			map[string]interface{}{"tool": "write_file", "arguments": map[string]interface{}{"path": "README.md", "content": "x"}},
		},
	}
	if err := ensureToolAllowedByContext(restricted, "parallel", badArgs); err == nil {
		t.Fatalf("expected parallel with disallowed nested tool to fail")
	}

	skillArgs := map[string]interface{}{
		"calls": []interface{}{
			map[string]interface{}{"tool": "skill_exec", "arguments": map[string]interface{}{"skill": "demo", "script": "scripts/run.sh"}},
		},
	}
	if err := ensureToolAllowedByContext(restricted, "parallel", skillArgs); err != nil {
		t.Fatalf("expected parallel with nested skill_exec to pass, got: %v", err)
	}
}

func TestEnsureToolAllowedByContext_GroupAllowlist(t *testing.T) {
	ctx := withToolAllowlistContext(context.Background(), []string{"group:files_read"})
	if err := ensureToolAllowedByContext(ctx, "read_file", map[string]interface{}{}); err != nil {
		t.Fatalf("expected files_read group to allow read_file, got: %v", err)
	}
	if err := ensureToolAllowedByContext(ctx, "write_file", map[string]interface{}{}); err == nil {
		t.Fatalf("expected files_read group to block write_file")
	}
}

func TestFilteredToolDefinitionsForContext(t *testing.T) {
	ctx := withToolAllowlistContext(context.Background(), []string{"read_file", "parallel"})
	toolDefs := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "read_file", "description": "read", "parameters": map[string]interface{}{}}},
		{"function": map[string]interface{}{"name": "write_file", "description": "write", "parameters": map[string]interface{}{}}},
		{"function": map[string]interface{}{"name": "parallel", "description": "parallel", "parameters": map[string]interface{}{}}},
		{"function": map[string]interface{}{"name": "skill_exec", "description": "skill", "parameters": map[string]interface{}{}}},
	}
	filtered := filterToolDefinitionsByContext(ctx, toolDefs)
	got := map[string]bool{}
	for _, td := range filtered {
		fnRaw, _ := td["function"].(map[string]interface{})
		name, _ := fnRaw["name"].(string)
		got[name] = true
	}
	if !got["read_file"] || !got["parallel"] || !got["skill_exec"] {
		t.Fatalf("expected filtered tools to include read_file, parallel, and inherited skill_exec, got: %v", got)
	}
	if got["write_file"] {
		t.Fatalf("expected filtered tools to exclude write_file, got: %v", got)
	}
}

func TestWithToolRuntimeArgsForSkillExec(t *testing.T) {
	mainArgs := withToolRuntimeArgs(context.Background(), "skill_exec", map[string]interface{}{"skill": "demo"})
	if mainArgs["caller_agent"] != "main" || mainArgs["caller_scope"] != "main_agent" {
		t.Fatalf("expected main agent runtime args, got: %#v", mainArgs)
	}

	subagentCtx := withMemoryNamespaceContext(context.Background(), "coder")
	subArgs := withToolRuntimeArgs(subagentCtx, "skill_exec", map[string]interface{}{"skill": "demo"})
	if subArgs["caller_agent"] != "coder" || subArgs["caller_scope"] != "subagent" {
		t.Fatalf("expected subagent runtime args, got: %#v", subArgs)
	}
}

func TestSubagentToolVisibilityMode(t *testing.T) {
	if got := subagentToolVisibilityMode("skill_exec"); got != "inherited" {
		t.Fatalf("expected skill_exec inherited, got %q", got)
	}
	if got := subagentToolVisibilityMode("write_file"); got != "allowlist" {
		t.Fatalf("expected write_file allowlist, got %q", got)
	}
}
