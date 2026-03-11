package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YspCoder/clawgo/pkg/config"
)

func TestStatusCmdUsesActiveProviderDetails(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	workspace := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Logging.Enabled = false
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Agents.Defaults.Model.Primary = "backup/backup-model"
	cfg.Gateway.Nodes.P2P.Enabled = true
	cfg.Gateway.Nodes.P2P.Transport = "webrtc"
	cfg.Gateway.Nodes.P2P.STUNServers = []string{"stun:stun.example.net:3478"}
	cfg.Gateway.Nodes.P2P.ICEServers = []config.GatewayICEConfig{
		{URLs: []string{"turn:turn.example.net:3478"}, Username: "user", Credential: "secret"},
	}
	cfg.Models.Providers["openai"] = config.ProviderConfig{
		APIBase:    "https://primary.example/v1",
		APIKey:     "",
		Models:     []string{"gpt-5.4"},
		Auth:       "bearer",
		TimeoutSec: 30,
	}
	cfg.Models.Providers["backup"] = config.ProviderConfig{
		APIBase:    "https://backup.example/v1",
		APIKey:     "backup-key",
		Models:     []string{"backup-model"},
		Auth:       "bearer",
		TimeoutSec: 30,
	}
	if err := config.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	prev := globalConfigPathOverride
	globalConfigPathOverride = cfgPath
	defer func() { globalConfigPathOverride = prev }()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	statusCmd()

	_ = w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Primary Provider: backup") {
		t.Fatalf("expected backup provider in output, got: %s", out)
	}
	if !strings.Contains(out, "Provider Base URL: https://backup.example/v1") {
		t.Fatalf("expected active provider api base in output, got: %s", out)
	}
	if !strings.Contains(out, "API Key Status: configured") {
		t.Fatalf("expected active provider api key status in output, got: %s", out)
	}
	if !strings.Contains(out, "Nodes P2P: enabled=true transport=webrtc") {
		t.Fatalf("expected nodes p2p status in output, got: %s", out)
	}
	if !strings.Contains(out, "Nodes P2P ICE: stun=1 ice=1") {
		t.Fatalf("expected nodes p2p ice summary in output, got: %s", out)
	}
}
