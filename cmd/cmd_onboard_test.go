package main

import (
	"testing"

	"github.com/YspCoder/clawgo/pkg/config"
)

func TestParseOnboardOptionsSyncWebUI(t *testing.T) {
	t.Parallel()

	opts := parseOnboardOptions([]string{"--sync-webui"})
	if !opts.syncWebUIOnly {
		t.Fatalf("expected sync webui option to be enabled")
	}
}

func TestEnsureConfigOnboardGeneratesGatewayToken(t *testing.T) {
	t.Parallel()

	configPath := t.TempDir() + "/config.json"
	cfg := config.DefaultConfig()
	cfg.Gateway.Token = ""

	state, err := ensureConfigOnboard(configPath, cfg)
	if err != nil {
		t.Fatalf("ensureConfigOnboard failed: %v", err)
	}
	if state != "created" {
		t.Fatalf("unexpected state: %s", state)
	}

	loaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if loaded.Gateway.Token == "" {
		t.Fatalf("expected gateway token to be generated")
	}
}
