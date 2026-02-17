package agent

import (
	"os"
	"path/filepath"
	"testing"

	"clawgo/pkg/config"
)

func TestGetConfigPathForCommands_FromArgs(t *testing.T) {
	oldArgs := os.Args
	oldEnv, hadEnv := os.LookupEnv("CLAWGO_CONFIG")
	t.Cleanup(func() {
		os.Args = oldArgs
		if hadEnv {
			_ = os.Setenv("CLAWGO_CONFIG", oldEnv)
		} else {
			_ = os.Unsetenv("CLAWGO_CONFIG")
		}
	})

	_ = os.Unsetenv("CLAWGO_CONFIG")
	os.Args = []string{"clawgo", "gateway", "run", "--config", "/tmp/custom-config.json"}

	al := &AgentLoop{}
	got := al.getConfigPathForCommands()
	if got != "/tmp/custom-config.json" {
		t.Fatalf("expected config path from args, got %q", got)
	}
}

func TestGetConfigPathForCommands_Default(t *testing.T) {
	oldArgs := os.Args
	oldEnv, hadEnv := os.LookupEnv("CLAWGO_CONFIG")
	t.Cleanup(func() {
		os.Args = oldArgs
		if hadEnv {
			_ = os.Setenv("CLAWGO_CONFIG", oldEnv)
		} else {
			_ = os.Unsetenv("CLAWGO_CONFIG")
		}
	})

	_ = os.Unsetenv("CLAWGO_CONFIG")
	os.Args = []string{"clawgo", "gateway", "run"}

	al := &AgentLoop{}
	got := al.getConfigPathForCommands()
	want := filepath.Join(config.GetConfigDir(), "config.json")
	if got != want {
		t.Fatalf("expected default config path %q, got %q", want, got)
	}
}
