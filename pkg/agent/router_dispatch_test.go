package agent

import (
	"context"
	"testing"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/runtimecfg"
	"github.com/YspCoder/clawgo/pkg/tools"
)

func TestResolveAutoRouteTarget(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Router.Enabled = true
	cfg.Agents.Subagents["coder"] = config.SubagentConfig{Enabled: true, SystemPromptFile: "agents/coder/AGENT.md"}

	agentID, task := resolveAutoRouteTarget(cfg, "@coder fix login")
	if agentID != "coder" || task != "fix login" {
		t.Fatalf("unexpected route target: %s / %s", agentID, task)
	}
}

func TestResolveAutoRouteTargetRulesFirst(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Router.Enabled = true
	cfg.Agents.Router.Strategy = "rules_first"
	cfg.Agents.Subagents["coder"] = config.SubagentConfig{Enabled: true, Role: "coding", SystemPromptFile: "agents/coder/AGENT.md"}
	cfg.Agents.Subagents["tester"] = config.SubagentConfig{Enabled: true, Role: "testing", SystemPromptFile: "agents/tester/AGENT.md"}
	cfg.Agents.Router.Rules = []config.AgentRouteRule{{AgentID: "coder", Keywords: []string{"鐧诲綍", "bug"}}}

	agentID, task := resolveAutoRouteTarget(cfg, "璇峰府鎴戜慨澶嶇櫥褰曟帴鍙ｇ殑 bug 骞舵敼浠ｇ爜")
	if agentID != "coder" || task == "" {
		t.Fatalf("expected coder route, got %s / %s", agentID, task)
	}
}

func TestMaybeAutoRouteDispatchesExplicitAgentMention(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Router.Enabled = true
	cfg.Agents.Router.DefaultTimeoutSec = 5
	cfg.Agents.Subagents["coder"] = config.SubagentConfig{Enabled: true, SystemPromptFile: "agents/coder/AGENT.md"}
	runtimecfg.Set(cfg)
	t.Cleanup(func() { runtimecfg.Set(config.DefaultConfig()) })

	workspace := t.TempDir()
	manager := tools.NewSubagentManager(nil, workspace, nil)
	manager.SetRunFunc(func(ctx context.Context, task *tools.SubagentTask) (string, error) {
		return "auto-routed", nil
	})
	loop := &AgentLoop{
		subagentManager: manager,
		subagentRouter:  tools.NewSubagentRouter(manager),
	}
	out, ok, err := loop.maybeAutoRoute(context.Background(), bus.InboundMessage{
		Channel:    "cli",
		ChatID:     "direct",
		SessionKey: "main",
		Content:    "@coder implement auth",
	})
	if err != nil {
		t.Fatalf("auto route failed: %v", err)
	}
	if !ok {
		t.Fatalf("expected auto route to trigger")
	}
	if out == "" {
		t.Fatalf("expected merged output")
	}
}

func TestMaybeAutoRouteSkipsNormalMessages(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Router.Enabled = true
	cfg.Agents.Subagents["coder"] = config.SubagentConfig{Enabled: true, SystemPromptFile: "agents/coder/AGENT.md"}
	runtimecfg.Set(cfg)
	t.Cleanup(func() { runtimecfg.Set(config.DefaultConfig()) })

	loop := &AgentLoop{}
	out, ok, err := loop.maybeAutoRoute(context.Background(), bus.InboundMessage{
		Channel:    "cli",
		ChatID:     "direct",
		SessionKey: "main",
		Content:    "please help with auth",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok || out != "" {
		t.Fatalf("expected normal message to skip auto route, got ok=%v out=%q", ok, out)
	}
}

func TestMaybeAutoRouteDispatchesRulesFirstMatch(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Router.Enabled = true
	cfg.Agents.Router.Strategy = "rules_first"
	cfg.Agents.Router.DefaultTimeoutSec = 5
	cfg.Agents.Subagents["tester"] = config.SubagentConfig{Enabled: true, Role: "testing", SystemPromptFile: "agents/tester/AGENT.md"}
	runtimecfg.Set(cfg)
	t.Cleanup(func() { runtimecfg.Set(config.DefaultConfig()) })

	workspace := t.TempDir()
	manager := tools.NewSubagentManager(nil, workspace, nil)
	manager.SetRunFunc(func(ctx context.Context, task *tools.SubagentTask) (string, error) {
		return "tested", nil
	})
	loop := &AgentLoop{
		subagentManager: manager,
		subagentRouter:  tools.NewSubagentRouter(manager),
	}
	out, ok, err := loop.maybeAutoRoute(context.Background(), bus.InboundMessage{
		Channel:    "cli",
		ChatID:     "direct",
		SessionKey: "main",
		Content:    "璇峰仛涓€娆″洖褰掓祴璇曞苟楠岃瘉杩欎釜淇",
	})
	if err != nil {
		t.Fatalf("rules-first auto route failed: %v", err)
	}
	if !ok {
		t.Fatalf("expected rules-first auto route to trigger")
	}
	if out == "" {
		t.Fatalf("expected merged output")
	}
}

func TestResolveAutoRouteTargetSkipsOversizedIntent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Router.Enabled = true
	cfg.Agents.Router.Policy.IntentMaxInputChars = 5
	cfg.Agents.Subagents["coder"] = config.SubagentConfig{Enabled: true, SystemPromptFile: "agents/coder/AGENT.md"}

	agentID, task := resolveAutoRouteTarget(cfg, "@coder implement auth")
	if agentID != "" || task != "" {
		t.Fatalf("expected oversized intent to skip routing, got %s / %s", agentID, task)
	}
}
