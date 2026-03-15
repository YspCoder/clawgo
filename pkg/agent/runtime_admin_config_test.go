package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/runtimecfg"
	"github.com/YspCoder/clawgo/pkg/tools"
)

func TestHandleSubagentRuntimeUpsertConfigSubagent(t *testing.T) {
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

	manager := tools.NewSubagentManager(nil, workspace, nil)
	loop := &AgentLoop{
		configPath:      configPath,
		subagentManager: manager,
		subagentRouter:  tools.NewSubagentRouter(manager),
	}
	out, err := loop.HandleSubagentRuntime(context.Background(), "upsert_config_subagent", map[string]interface{}{
		"agent_id":           "reviewer",
		"role":               "testing",
		"notify_main_policy": "internal_only",
		"display_name":       "Review Agent",
		"system_prompt_file": "agents/reviewer/AGENT.md",
		"routing_keywords":   []interface{}{"review", "regression"},
		"tool_allowlist":     []interface{}{"shell", "sessions"},
	})
	if err != nil {
		t.Fatalf("upsert config subagent failed: %v", err)
	}
	payload, ok := out.(map[string]interface{})
	if !ok || payload["ok"] != true {
		t.Fatalf("unexpected payload: %#v", out)
	}
	reloaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("reload config failed: %v", err)
	}
	subcfg, ok := reloaded.Agents.Subagents["reviewer"]
	if !ok || subcfg.DisplayName != "Review Agent" {
		t.Fatalf("expected reviewer subagent in config, got %+v", reloaded.Agents.Subagents)
	}
	if subcfg.SystemPromptFile != "agents/reviewer/AGENT.md" {
		t.Fatalf("expected system_prompt_file to persist, got %+v", subcfg)
	}
	if subcfg.NotifyMainPolicy != "internal_only" {
		t.Fatalf("expected notify_main_policy to persist, got %+v", subcfg)
	}
	if len(reloaded.Agents.Router.Rules) == 0 {
		t.Fatalf("expected router rules to be persisted")
	}
	data, err := os.ReadFile(configPath)
	if err != nil || len(data) == 0 {
		t.Fatalf("expected config file to be written")
	}
}

func TestHandleSubagentRuntimeRegistryAndToggleEnabled(t *testing.T) {
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
	cfg.Agents.Subagents["tester"] = config.SubagentConfig{
		Enabled:          true,
		Type:             "worker",
		Role:             "testing",
		DisplayName:      "Test Agent",
		SystemPromptFile: "agents/tester/AGENT.md",
		MemoryNamespace:  "tester",
		Tools: config.SubagentToolsConfig{
			Allowlist: []string{"shell", "sessions"},
		},
	}
	cfg.Agents.Router.Rules = []config.AgentRouteRule{{AgentID: "tester", Keywords: []string{"test", "regression"}}}
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("save config failed: %v", err)
	}
	runtimecfg.Set(cfg)
	t.Cleanup(func() { runtimecfg.Set(config.DefaultConfig()) })

	manager := tools.NewSubagentManager(nil, workspace, nil)
	loop := &AgentLoop{
		configPath:      configPath,
		subagentManager: manager,
		subagentRouter:  tools.NewSubagentRouter(manager),
	}
	out, err := loop.HandleSubagentRuntime(context.Background(), "registry", nil)
	if err != nil {
		t.Fatalf("registry failed: %v", err)
	}
	payload, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected registry payload: %T", out)
	}
	items, ok := payload["items"].([]map[string]interface{})
	if !ok || len(items) < 2 {
		t.Fatalf("expected registry items, got %#v", payload["items"])
	}
	var tester map[string]interface{}
	for _, item := range items {
		if item["agent_id"] == "tester" {
			tester = item
			break
		}
	}
	if tester == nil {
		t.Fatalf("expected tester registry item, got %#v", items)
	}
	toolVisibility, _ := tester["tool_visibility"].(map[string]interface{})
	if toolVisibility == nil {
		t.Fatalf("expected tool_visibility in tester registry item, got %#v", tester)
	}
	if toolVisibility["mode"] != "allowlist" {
		t.Fatalf("expected tester tool mode allowlist, got %#v", toolVisibility)
	}
	inherited, _ := tester["inherited_tools"].([]string)
	if len(inherited) != 1 || inherited[0] != "skill_exec" {
		t.Fatalf("expected inherited skill_exec, got %#v", tester["inherited_tools"])
	}

	_, err = loop.HandleSubagentRuntime(context.Background(), "set_config_subagent_enabled", map[string]interface{}{
		"agent_id": "tester",
		"enabled":  false,
	})
	if err != nil {
		t.Fatalf("toggle enabled failed: %v", err)
	}
	reloaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("reload config failed: %v", err)
	}
	if reloaded.Agents.Subagents["tester"].Enabled {
		t.Fatalf("expected tester to be disabled")
	}
}

func TestHandleSubagentRuntimeDeleteConfigSubagent(t *testing.T) {
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
	cfg.Agents.Subagents["tester"] = config.SubagentConfig{
		Enabled:          true,
		Type:             "worker",
		Role:             "testing",
		SystemPromptFile: "agents/tester/AGENT.md",
	}
	cfg.Agents.Router.Rules = []config.AgentRouteRule{{AgentID: "tester", Keywords: []string{"test"}}}
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("save config failed: %v", err)
	}
	runtimecfg.Set(cfg)
	t.Cleanup(func() { runtimecfg.Set(config.DefaultConfig()) })

	manager := tools.NewSubagentManager(nil, workspace, nil)
	loop := &AgentLoop{
		configPath:      configPath,
		subagentManager: manager,
		subagentRouter:  tools.NewSubagentRouter(manager),
	}
	out, err := loop.HandleSubagentRuntime(context.Background(), "delete_config_subagent", map[string]interface{}{"agent_id": "tester"})
	if err != nil {
		t.Fatalf("delete config subagent failed: %v", err)
	}
	payload, ok := out.(map[string]interface{})
	if !ok || payload["ok"] != true {
		t.Fatalf("unexpected delete payload: %#v", out)
	}
	reloaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("reload config failed: %v", err)
	}
	if _, ok := reloaded.Agents.Subagents["tester"]; ok {
		t.Fatalf("expected tester to be removed")
	}
	if len(reloaded.Agents.Router.Rules) != 0 {
		t.Fatalf("expected tester route rule to be removed")
	}
}

func TestHandleSubagentRuntimeToggleEnabledParsesStringBool(t *testing.T) {
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
	cfg.Agents.Subagents["tester"] = config.SubagentConfig{
		Enabled:          true,
		Type:             "worker",
		Role:             "testing",
		SystemPromptFile: "agents/tester/AGENT.md",
	}
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("save config failed: %v", err)
	}
	runtimecfg.Set(cfg)
	t.Cleanup(func() { runtimecfg.Set(config.DefaultConfig()) })

	manager := tools.NewSubagentManager(nil, workspace, nil)
	loop := &AgentLoop{
		configPath:      configPath,
		subagentManager: manager,
		subagentRouter:  tools.NewSubagentRouter(manager),
	}
	if _, err := loop.HandleSubagentRuntime(context.Background(), "set_config_subagent_enabled", map[string]interface{}{
		"agent_id": "tester",
		"enabled":  "false",
	}); err != nil {
		t.Fatalf("toggle enabled failed: %v", err)
	}
	reloaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("reload config failed: %v", err)
	}
	if reloaded.Agents.Subagents["tester"].Enabled {
		t.Fatalf("expected tester to be disabled")
	}
}

func TestHandleSubagentRuntimePromptFileGetSetBootstrap(t *testing.T) {
	workspace := t.TempDir()
	manager := tools.NewSubagentManager(nil, workspace, nil)
	loop := &AgentLoop{
		workspace:       workspace,
		subagentManager: manager,
		subagentRouter:  tools.NewSubagentRouter(manager),
	}

	out, err := loop.HandleSubagentRuntime(context.Background(), "prompt_file_get", map[string]interface{}{
		"path": "agents/coder/AGENT.md",
	})
	if err != nil {
		t.Fatalf("prompt_file_get failed: %v", err)
	}
	payload, ok := out.(map[string]interface{})
	if !ok || payload["found"] != false {
		t.Fatalf("expected missing prompt file, got %#v", out)
	}

	out, err = loop.HandleSubagentRuntime(context.Background(), "prompt_file_bootstrap", map[string]interface{}{
		"agent_id": "coder",
		"role":     "coding",
	})
	if err != nil {
		t.Fatalf("prompt_file_bootstrap failed: %v", err)
	}
	payload, ok = out.(map[string]interface{})
	if !ok || payload["created"] != true {
		t.Fatalf("expected prompt file bootstrap to create file, got %#v", out)
	}

	out, err = loop.HandleSubagentRuntime(context.Background(), "prompt_file_set", map[string]interface{}{
		"path":    "agents/coder/AGENT.md",
		"content": "# coder\nupdated",
	})
	if err != nil {
		t.Fatalf("prompt_file_set failed: %v", err)
	}
	payload, ok = out.(map[string]interface{})
	if !ok || payload["ok"] != true {
		t.Fatalf("expected prompt_file_set ok, got %#v", out)
	}

	out, err = loop.HandleSubagentRuntime(context.Background(), "prompt_file_get", map[string]interface{}{
		"path": "agents/coder/AGENT.md",
	})
	if err != nil {
		t.Fatalf("prompt_file_get after set failed: %v", err)
	}
	payload, ok = out.(map[string]interface{})
	if !ok || payload["found"] != true || payload["content"] != "# coder\nupdated" {
		t.Fatalf("unexpected prompt file payload: %#v", out)
	}
}

func TestHandleSubagentRuntimeProtectsMainAgent(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.json")
	cfg := config.DefaultConfig()
	cfg.Agents.Router.Enabled = true
	cfg.Agents.Router.MainAgentID = "main"
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

	manager := tools.NewSubagentManager(nil, workspace, nil)
	loop := &AgentLoop{
		configPath:      configPath,
		workspace:       workspace,
		subagentManager: manager,
		subagentRouter:  tools.NewSubagentRouter(manager),
	}
	if _, err := loop.HandleSubagentRuntime(context.Background(), "set_config_subagent_enabled", map[string]interface{}{
		"agent_id": "main",
		"enabled":  false,
	}); err == nil {
		t.Fatalf("expected disabling main agent to fail")
	}
	if _, err := loop.HandleSubagentRuntime(context.Background(), "delete_config_subagent", map[string]interface{}{
		"agent_id": "main",
	}); err == nil {
		t.Fatalf("expected deleting main agent to fail")
	}
}
