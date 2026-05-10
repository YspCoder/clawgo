package tools

import (
	"reflect"
	"sort"
	"testing"

	"github.com/YspCoder/clawgo/pkg/config"
)

func TestBootstrapDefaultToolsRegistersExpectedLocalTools(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = t.TempDir()
	cfg.Tools.MCP.Enabled = false

	result, err := BootstrapDefaultTools(t.Context(), BootstrapOptions{
		Config:    cfg,
		Workspace: cfg.WorkspacePath(),
	})
	if err != nil {
		t.Fatalf("bootstrap default tools: %v", err)
	}
	if result == nil || result.Registry == nil {
		t.Fatalf("expected registry in bootstrap result")
	}
	if result.ProcessManager == nil {
		t.Fatalf("expected process manager in bootstrap result")
	}
	if result.SubagentManager == nil || result.SubagentRouter == nil {
		t.Fatalf("expected subagent manager and router in bootstrap result")
	}

	got := result.Registry.List()
	sort.Strings(got)
	want := []string{
		"browser",
		"camera_snap",
		"edit_file",
		"exec",
		"list_dir",
		"memory_get",
		"memory_search",
		"memory_write",
		"message",
		"parallel",
		"parallel_fetch",
		"process",
		"read_file",
		"sessions",
		"skill_exec",
		"spawn",
		"subagent_profile",
		"system_info",
		"web_fetch",
		"web_search",
		"write_file",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default tool names mismatch\n got: %v\nwant: %v", got, want)
	}
}
