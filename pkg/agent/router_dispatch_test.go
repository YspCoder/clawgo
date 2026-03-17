package agent

import (
	"context"
	"testing"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/runtimecfg"
	"github.com/YspCoder/clawgo/pkg/tools"
)

func TestResolveDispatchDecisionExplicitAgent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Router.Enabled = true
	cfg.Agents.Subagents["coder"] = config.SubagentConfig{Enabled: true, SystemPromptFile: "agents/coder/AGENT.md"}

	decision := resolveDispatchDecision(cfg, "@coder fix login")
	if decision.TargetAgent != "coder" || decision.TaskText != "fix login" {
		t.Fatalf("unexpected route target: %+v", decision)
	}
}

func TestResolveDispatchDecisionRulesFirst(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Router.Enabled = true
	cfg.Agents.Router.Strategy = "rules_first"
	cfg.Agents.Subagents["coder"] = config.SubagentConfig{Enabled: true, Role: "coding", SystemPromptFile: "agents/coder/AGENT.md"}
	cfg.Agents.Subagents["tester"] = config.SubagentConfig{Enabled: true, Role: "testing", SystemPromptFile: "agents/tester/AGENT.md"}
	cfg.Agents.Router.Rules = []config.AgentRouteRule{{AgentID: "coder", Keywords: []string{"鐧诲綍", "bug"}}}

	decision := resolveDispatchDecision(cfg, "please fix the login bug and update the code")
	if decision.TargetAgent != "coder" || decision.TaskText == "" {
		t.Fatalf("expected coder route, got %+v", decision)
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
	manager.SetRunFunc(func(ctx context.Context, run *tools.SubagentRun) (string, error) {
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
	manager.SetRunFunc(func(ctx context.Context, run *tools.SubagentRun) (string, error) {
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
		Content:    "please run regression testing and verify this fix",
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

func TestResolveDispatchDecisionIncludesReason(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Router.Enabled = true
	cfg.Agents.Router.Strategy = "rules_first"
	cfg.Agents.Subagents["tester"] = config.SubagentConfig{Enabled: true, Role: "testing", SystemPromptFile: "agents/tester/AGENT.md"}

	decision := resolveDispatchDecision(cfg, "run regression testing for this change")
	if !decision.Valid() {
		t.Fatalf("expected valid decision")
	}
	if decision.TargetAgent != "tester" || decision.RouteSource == "" || decision.Reason == "" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestResolveDispatchDecisionSkipsOversizedIntent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Router.Enabled = true
	cfg.Agents.Router.Policy.IntentMaxInputChars = 5
	cfg.Agents.Subagents["coder"] = config.SubagentConfig{Enabled: true, SystemPromptFile: "agents/coder/AGENT.md"}

	decision := resolveDispatchDecision(cfg, "@coder implement auth")
	if decision.TargetAgent != "" || decision.TaskText != "" {
		t.Fatalf("expected oversized intent to skip routing, got %+v", decision)
	}
}
