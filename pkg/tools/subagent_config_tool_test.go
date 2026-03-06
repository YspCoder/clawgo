package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"clawgo/pkg/config"
	"clawgo/pkg/runtimecfg"
)

func TestSubagentConfigToolDraft(t *testing.T) {
	tool := NewSubagentConfigTool("")
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":      "draft",
		"description": "创建一个负责回归测试和验证修复结果的子代理",
	})
	if err != nil {
		t.Fatalf("draft failed: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal payload failed: %v", err)
	}
	draft, ok := payload["draft"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected draft map, got %#v", payload["draft"])
	}
	if draft["agent_id"] == "" || draft["role"] == "" {
		t.Fatalf("expected draft agent_id and role, got %#v", draft)
	}
}

func TestSubagentConfigToolUpsert(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.json")
	cfg := config.DefaultConfig()
	cfg.Agents.Router.Enabled = true
	cfg.Agents.Subagents["main"] = config.SubagentConfig{
		Enabled: true,
		Type:    "router",
		Role:    "orchestrator",
	}
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("save config failed: %v", err)
	}
	runtimecfg.Set(cfg)
	t.Cleanup(func() { runtimecfg.Set(config.DefaultConfig()) })

	tool := NewSubagentConfigTool(configPath)
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":           "upsert",
		"agent_id":         "reviewer",
		"role":             "testing",
		"display_name":     "Review Agent",
		"description":      "负责回归与评审",
		"system_prompt":    "review changes",
		"routing_keywords": []interface{}{"review", "regression"},
		"tool_allowlist":   []interface{}{"shell", "sessions"},
	})
	if err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal payload failed: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("expected ok payload, got %#v", payload)
	}
	reloaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("reload config failed: %v", err)
	}
	if reloaded.Agents.Subagents["reviewer"].DisplayName != "Review Agent" {
		t.Fatalf("expected config to persist reviewer, got %+v", reloaded.Agents.Subagents["reviewer"])
	}
	if len(reloaded.Agents.Router.Rules) == 0 {
		t.Fatalf("expected router rules to persist")
	}
}
