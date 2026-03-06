package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
	"clawgo/pkg/runtimecfg"
)

func TestMaybeHandleSubagentConfigIntentCreateAndConfirm(t *testing.T) {
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

	loop := &AgentLoop{
		configPath:           configPath,
		pendingSubagentDraft: map[string]map[string]interface{}{},
	}
	out, handled, err := loop.maybeHandleSubagentConfigIntent(context.Background(), bus.InboundMessage{
		SessionKey: "main",
		Channel:    "cli",
		Content:    "创建一个负责回归测试和验证修复结果的子代理",
	})
	if err != nil {
		t.Fatalf("create draft failed: %v", err)
	}
	if !handled || !strings.Contains(out, "已生成 subagent 草案") {
		t.Fatalf("expected draft response, got handled=%v out=%q", handled, out)
	}

	out, handled, err = loop.maybeHandleSubagentConfigIntent(context.Background(), bus.InboundMessage{
		SessionKey: "main",
		Channel:    "cli",
		Content:    "确认创建",
	})
	if err != nil {
		t.Fatalf("confirm draft failed: %v", err)
	}
	if !handled || !strings.Contains(out, "已写入 config.json") {
		t.Fatalf("expected confirm response, got handled=%v out=%q", handled, out)
	}

	reloaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("reload config failed: %v", err)
	}
	if _, ok := reloaded.Agents.Subagents["tester"]; !ok {
		t.Fatalf("expected tester subagent to persist, got %+v", reloaded.Agents.Subagents)
	}
}

func TestMaybeHandleSubagentConfigIntentCancel(t *testing.T) {
	loop := &AgentLoop{
		pendingSubagentDraft: map[string]map[string]interface{}{},
	}
	_, handled, err := loop.maybeHandleSubagentConfigIntent(context.Background(), bus.InboundMessage{
		SessionKey: "main",
		Channel:    "cli",
		Content:    "创建一个负责文档整理的子代理",
	})
	if err != nil {
		t.Fatalf("create draft failed: %v", err)
	}
	if !handled {
		t.Fatalf("expected create to be handled")
	}

	out, handled, err := loop.maybeHandleSubagentConfigIntent(context.Background(), bus.InboundMessage{
		SessionKey: "main",
		Channel:    "cli",
		Content:    "取消创建",
	})
	if err != nil {
		t.Fatalf("cancel draft failed: %v", err)
	}
	if !handled || !strings.Contains(out, "已取消") {
		t.Fatalf("expected cancel response, got handled=%v out=%q", handled, out)
	}
	if got := loop.loadPendingSubagentDraft("main"); got != nil {
		t.Fatalf("expected pending draft to be cleared, got %#v", got)
	}
}

func TestPendingSubagentDraftPersistsAcrossLoopRestart(t *testing.T) {
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

	store := NewPendingSubagentDraftStore(workspace)
	loop := &AgentLoop{
		workspace:            workspace,
		configPath:           configPath,
		pendingDraftStore:    store,
		pendingSubagentDraft: map[string]map[string]interface{}{},
	}
	_, handled, err := loop.maybeHandleSubagentConfigIntent(context.Background(), bus.InboundMessage{
		SessionKey: "main",
		Channel:    "cli",
		Content:    "创建一个负责文档整理的子代理",
	})
	if err != nil {
		t.Fatalf("create draft failed: %v", err)
	}
	if !handled {
		t.Fatalf("expected create to be handled")
	}

	reloadedStore := NewPendingSubagentDraftStore(workspace)
	reloadedLoop := &AgentLoop{
		workspace:            workspace,
		configPath:           configPath,
		pendingDraftStore:    reloadedStore,
		pendingSubagentDraft: reloadedStore.All(),
	}
	if reloadedLoop.loadPendingSubagentDraft("main") == nil {
		t.Fatalf("expected draft to be restored from store")
	}

	out, handled, err := reloadedLoop.maybeHandleSubagentConfigIntent(context.Background(), bus.InboundMessage{
		SessionKey: "main",
		Channel:    "cli",
		Content:    "确认创建",
	})
	if err != nil {
		t.Fatalf("confirm draft failed: %v", err)
	}
	if !handled || !strings.Contains(out, "已写入 config.json") {
		t.Fatalf("expected confirm response, got handled=%v out=%q", handled, out)
	}

	reloadedCfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("reload config failed: %v", err)
	}
	if _, ok := reloadedCfg.Agents.Subagents["doc_writer"]; !ok {
		t.Fatalf("expected doc_writer subagent to persist, got %+v", reloadedCfg.Agents.Subagents)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "agents", "runtime", "pending_subagent_drafts.json"))
	if err != nil {
		t.Fatalf("expected persisted draft store file: %v", err)
	}
	if !strings.Contains(string(data), "{}") {
		t.Fatalf("expected draft store to be cleared after confirm, got %s", string(data))
	}
}
