package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigRejectsUnknownField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	content := `{
  "agents": {
    "defaults": {
      "runtime_control": {
        "intent_high_confidence": 0.8,
        "unknown_field": 1
      }
    }
  }
}`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadConfig(cfgPath)
	if err == nil {
		t.Fatalf("expected unknown field error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unknown field") {
		t.Fatalf("expected unknown field error, got: %v", err)
	}
}

func TestLoadConfigRejectsTrailingJSONContent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	content := `{"agents":{"defaults":{"runtime_control":{"intent_high_confidence":0.8}}}}{"extra":true}`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadConfig(cfgPath)
	if err == nil {
		t.Fatalf("expected trailing json content error")
	}
	if !strings.Contains(err.Error(), "trailing JSON content") {
		t.Fatalf("expected trailing JSON content error, got: %v", err)
	}
}

func TestLoadConfigAllowsKnownRuntimeControlFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	content := `{
  "agents": {
    "defaults": {
      "runtime_control": {
        "intent_high_confidence": 0.88,
        "run_state_max": 321,
        "tool_parallel_safe_names": ["read_file", "memory_search"],
        "tool_max_parallel_calls": 3
      }
    }
  }
}`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := cfg.Agents.Defaults.RuntimeControl.IntentHighConfidence; got != 0.88 {
		t.Fatalf("intent_high_confidence mismatch: got %.2f", got)
	}
	if got := cfg.Agents.Defaults.RuntimeControl.RunStateMax; got != 321 {
		t.Fatalf("run_state_max mismatch: got %d", got)
	}
	if got := cfg.Agents.Defaults.RuntimeControl.ToolMaxParallelCalls; got != 3 {
		t.Fatalf("tool_max_parallel_calls mismatch: got %d", got)
	}
}
