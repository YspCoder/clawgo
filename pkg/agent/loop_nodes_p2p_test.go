package agent

import (
	"testing"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
)

func TestNewAgentLoopDisablesNodeP2PByDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = t.TempDir()

	loop := NewAgentLoop(cfg, bus.NewMessageBus(), stubLLMProvider{}, nil)
	if loop.nodeRouter == nil {
		t.Fatalf("expected node router to be configured")
	}
	if loop.nodeRouter.P2P != nil {
		t.Fatalf("expected node p2p transport to be disabled by default")
	}
}

func TestNewAgentLoopEnablesNodeP2PWhenConfigured(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = t.TempDir()
	cfg.Gateway.Nodes.P2P.Enabled = true

	loop := NewAgentLoop(cfg, bus.NewMessageBus(), stubLLMProvider{}, nil)
	if loop.nodeRouter == nil || loop.nodeRouter.P2P == nil {
		t.Fatalf("expected node p2p transport to be enabled")
	}
}
