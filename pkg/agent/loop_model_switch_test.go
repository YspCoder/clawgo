package agent

import "testing"

func TestApplyRuntimeModelConfig_Model(t *testing.T) {
	al := &AgentLoop{model: "old-model"}
	al.applyRuntimeModelConfig("agents.defaults.model", "new-model")
	if al.model != "new-model" {
		t.Fatalf("expected runtime model updated, got %q", al.model)
	}
}

func TestApplyRuntimeModelConfig_ModelFallbacks(t *testing.T) {
	al := &AgentLoop{modelFallbacks: []string{"old-fallback"}}
	al.applyRuntimeModelConfig("agents.defaults.model_fallbacks", []interface{}{"gpt-4o-mini", "", "claude-3-5-sonnet"})
	if len(al.modelFallbacks) != 2 {
		t.Fatalf("expected 2 fallbacks, got %d: %v", len(al.modelFallbacks), al.modelFallbacks)
	}
	if al.modelFallbacks[0] != "gpt-4o-mini" || al.modelFallbacks[1] != "claude-3-5-sonnet" {
		t.Fatalf("unexpected fallbacks: %v", al.modelFallbacks)
	}
}

func TestParseModelFallbacks_StringValue(t *testing.T) {
	fallbacks := parseModelFallbacks("gpt-4o-mini")
	if len(fallbacks) != 1 || fallbacks[0] != "gpt-4o-mini" {
		t.Fatalf("unexpected parse result: %v", fallbacks)
	}
}
