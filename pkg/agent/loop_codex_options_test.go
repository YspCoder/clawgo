package agent

import "testing"

func TestBuildResponsesOptionsAddsCodexExecutionSession(t *testing.T) {
	loop := &AgentLoop{
		sessionProvider: map[string]string{
			"chat-1": "codex",
		},
	}

	options := loop.buildResponsesOptions("chat-1", 8192, 0.7)
	if got := options["codex_execution_session"]; got != "chat-1" {
		t.Fatalf("expected codex_execution_session chat-1, got %#v", got)
	}
}

func TestBuildResponsesOptionsSkipsCodexExecutionSessionForOtherProviders(t *testing.T) {
	loop := &AgentLoop{
		sessionProvider: map[string]string{
			"chat-1": "claude",
		},
	}

	options := loop.buildResponsesOptions("chat-1", 8192, 0.7)
	if _, ok := options["codex_execution_session"]; ok {
		t.Fatalf("expected no codex_execution_session for non-codex provider, got %#v", options["codex_execution_session"])
	}
}

func TestSyncSessionDefaultProviderOverridesStaleSessionProvider(t *testing.T) {
	loop := &AgentLoop{
		providerNames: []string{"openai"},
		sessionProvider: map[string]string{
			"chat-1": "codex",
		},
	}

	loop.syncSessionDefaultProvider("chat-1")

	if got := loop.getSessionProvider("chat-1"); got != "openai" {
		t.Fatalf("expected stale session provider to be replaced with current default, got %q", got)
	}
}
