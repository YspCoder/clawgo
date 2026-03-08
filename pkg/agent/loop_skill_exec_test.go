package agent

import (
	"context"
	"testing"

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
	"clawgo/pkg/providers"
)

type stubLLMProvider struct{}

func (stubLLMProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	return &providers.LLMResponse{Content: "ok", FinishReason: "stop"}, nil
}

func (stubLLMProvider) GetDefaultModel() string { return "stub-model" }

func TestNewAgentLoopRegistersSkillExec(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = t.TempDir()

	loop := NewAgentLoop(cfg, bus.NewMessageBus(), stubLLMProvider{}, nil)
	catalog := loop.GetToolCatalog()

	found := false
	for _, item := range catalog {
		if item["name"] == "skill_exec" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected skill_exec in tool catalog, got %v", catalog)
	}
}
