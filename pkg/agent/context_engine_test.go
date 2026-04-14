package agent

import (
	"testing"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/providers"
	"github.com/YspCoder/clawgo/pkg/session"
)

type testContextEngine struct {
	lastReq  ContextBuildRequest
	messages []providers.Message
}

func (e *testContextEngine) BuildMessages(req ContextBuildRequest) []providers.Message {
	e.lastReq = req
	return append([]providers.Message(nil), e.messages...)
}

func (e *testContextEngine) SkillsInfo() map[string]interface{} {
	return map[string]interface{}{"total": 0}
}

func TestAgentLoopUsesPluggableContextEngine(t *testing.T) {
	t.Parallel()

	engine := &testContextEngine{
		messages: []providers.Message{{Role: "system", Content: "from-test-engine"}},
	}
	loop := &AgentLoop{
		sessions:      session.NewSessionManager(""),
		contextEngine: engine,
	}
	msg := bus.InboundMessage{
		Channel:    "cli",
		ChatID:     "direct",
		SessionKey: "main",
		Content:    "hello",
	}
	messages, _ := loop.prepareUserMessageContext(msg, "main")
	if len(messages) != 1 || messages[0].Content != "from-test-engine" {
		t.Fatalf("expected custom engine output, got %#v", messages)
	}
	if engine.lastReq.CurrentMessage != "hello" || engine.lastReq.Channel != "cli" {
		t.Fatalf("unexpected context request: %#v", engine.lastReq)
	}
}
