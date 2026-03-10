package main

import (
	"testing"

	"github.com/YspCoder/clawgo/pkg/config"
)

func TestShouldStartEmbeddedWhatsAppBridge(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.Channels.WhatsApp.Enabled = false
	cfg.Channels.WhatsApp.BridgeURL = ""
	if !shouldStartEmbeddedWhatsAppBridge(cfg) {
		t.Fatalf("expected embedded bridge to start when using default embedded url")
	}

	cfg.Channels.WhatsApp.BridgeURL = "ws://127.0.0.1:3001"
	if !shouldStartEmbeddedWhatsAppBridge(cfg) {
		t.Fatalf("expected embedded bridge to start for legacy local bridge url")
	}

	cfg.Channels.WhatsApp.BridgeURL = "ws://example.com:3001/ws"
	if shouldStartEmbeddedWhatsAppBridge(cfg) {
		t.Fatalf("expected external bridge url to disable embedded bridge")
	}

	if shouldStartEmbeddedWhatsAppBridge(nil) {
		t.Fatalf("expected nil config to disable embedded bridge")
	}
}
