package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
	"clawgo/pkg/runtimecfg"
)

func TestMaybeHandleSubagentConfigIntentCreatePersistsImmediately(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.json")
	cfg := config.DefaultConfig()
	cfg.Agents.Router.Enabled = true
	cfg.Agents.Subagents["main"] = config.SubagentConfig{
		Enabled:          true,
		Type:             "router",
		Role:             "orchestrator",
		SystemPromptFile: "agents/main/AGENT.md",
	}
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("save config failed: %v", err)
	}
	runtimecfg.Set(cfg)
	t.Cleanup(func() { runtimecfg.Set(config.DefaultConfig()) })

	loop := &AgentLoop{configPath: configPath}
	out, handled, err := loop.maybeHandleSubagentConfigIntent(context.Background(), bus.InboundMessage{
		SessionKey: "main",
		Channel:    "cli",
		Content:    "创建一个负责回归测试和验证修复结果的子代理",
	})
	if err != nil {
		t.Fatalf("create subagent failed: %v", err)
	}
	if !handled || !strings.Contains(out, "已写入 config.json") {
		t.Fatalf("expected immediate persist response, got handled=%v out=%q", handled, out)
	}
	if !strings.Contains(out, configPath) {
		t.Fatalf("expected response to include config path, got %q", out)
	}

	reloaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("reload config failed: %v", err)
	}
	if _, ok := reloaded.Agents.Subagents["tester"]; !ok {
		t.Fatalf("expected tester subagent to persist, got %+v", reloaded.Agents.Subagents)
	}
}

func TestMaybeHandleSubagentConfigIntentConfirmCancelNoLongerHandled(t *testing.T) {
	loop := &AgentLoop{}
	for _, content := range []string{"确认创建", "取消创建"} {
		out, handled, err := loop.maybeHandleSubagentConfigIntent(context.Background(), bus.InboundMessage{
			SessionKey: "main",
			Channel:    "cli",
			Content:    content,
		})
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", content, err)
		}
		if handled || out != "" {
			t.Fatalf("expected %q to pass through, got handled=%v out=%q", content, handled, out)
		}
	}
}
