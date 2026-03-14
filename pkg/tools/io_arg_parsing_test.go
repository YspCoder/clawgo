package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/config"
)

func TestFilesystemToolsParseStringArgs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := NewWriteFileTool(root)
	read := NewReadFileTool(root)
	list := NewListDirTool(root)

	if _, err := write.Execute(context.Background(), map[string]interface{}{
		"path":    "demo.txt",
		"content": "hello world",
		"append":  "false",
	}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	out, err := read.Execute(context.Background(), map[string]interface{}{
		"path":   "demo.txt",
		"offset": "6",
		"limit":  "5",
	})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if out != "world" {
		t.Fatalf("unexpected read output: %q", out)
	}

	listOut, err := list.Execute(context.Background(), map[string]interface{}{
		"path":      ".",
		"recursive": "true",
	})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if !strings.Contains(listOut, "demo.txt") {
		t.Fatalf("unexpected list output: %s", listOut)
	}

	edit := NewEditFileTool(root)
	if _, err := edit.Execute(context.Background(), map[string]interface{}{
		"path":     "demo.txt",
		"old_text": "world",
		"new_text": "",
	}); err != nil {
		t.Fatalf("edit with empty new_text failed: %v", err)
	}
	emptyWrite, err := write.Execute(context.Background(), map[string]interface{}{
		"path":    "empty.txt",
		"content": "",
	})
	if err != nil {
		t.Fatalf("write empty content failed: %v", err)
	}
	if !strings.Contains(emptyWrite, "empty.txt") {
		t.Fatalf("unexpected empty write output: %s", emptyWrite)
	}
}

func TestSpawnToolParsesStringNumbers(t *testing.T) {
	manager := NewSubagentManager(nil, t.TempDir(), nil)
	manager.SetRunFunc(func(ctx context.Context, task *SubagentTask) (string, error) {
		return "ok", nil
	})
	tool := NewSpawnTool(manager)

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"task":             "implement check",
		"agent_id":         "coder",
		"max_retries":      "2",
		"retry_backoff_ms": "100",
		"timeout_sec":      "5",
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}
	if !strings.Contains(out, "spawned") && !strings.Contains(strings.ToLower(out), "subagent") {
		t.Fatalf("unexpected spawn output: %s", out)
	}
	time.Sleep(50 * time.Millisecond)
}

func TestExecBrowserWebToolsParseStringArgs(t *testing.T) {
	t.Parallel()

	execTool := NewExecTool(configShellForTest(), t.TempDir(), NewProcessManager(t.TempDir()))
	execOut, err := execTool.Execute(context.Background(), map[string]interface{}{
		"command":    "printf hi",
		"background": "false",
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if !strings.Contains(execOut, "hi") {
		t.Fatalf("unexpected exec output: %s", execOut)
	}

	browserTool := NewBrowserTool()
	if _, err := browserTool.Execute(context.Background(), map[string]interface{}{
		"action": "unknown",
		"url":    "https://example.com",
	}); err == nil {
		t.Fatal("expected browser tool to reject unknown action")
	}

	search := NewWebSearchTool("", 5)
	searchOut, err := search.Execute(context.Background(), map[string]interface{}{
		"query": "golang",
		"count": "3",
	})
	if err != nil {
		t.Fatalf("web search failed: %v", err)
	}
	if !strings.Contains(searchOut, "BRAVE_API_KEY") {
		t.Fatalf("unexpected web search output: %s", searchOut)
	}
}

func configShellForTest() config.ShellConfig {
	return config.ShellConfig{}
}
