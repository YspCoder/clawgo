//go:build !omit_whatsapp

package channels

import (
	"testing"

	"github.com/YspCoder/clawgo/pkg/config"
)

func TestWhatsAppShouldHandleIncomingMessage(t *testing.T) {
	ch := &WhatsAppChannel{
		config: config.WhatsAppConfig{
			EnableGroups:           true,
			RequireMentionInGroups: true,
		},
	}
	if !ch.shouldHandleIncomingMessage(false, false, false) {
		t.Fatalf("private chats should always be allowed")
	}
	if ch.shouldHandleIncomingMessage(true, false, false) {
		t.Fatalf("group message without mention should be blocked")
	}
	if !ch.shouldHandleIncomingMessage(true, true, false) {
		t.Fatalf("group mention should be allowed")
	}
	if !ch.shouldHandleIncomingMessage(true, false, true) {
		t.Fatalf("reply-to-me should be allowed")
	}

	ch.config.EnableGroups = false
	if ch.shouldHandleIncomingMessage(true, true, true) {
		t.Fatalf("groups should be blocked when disabled")
	}

	ch.config.EnableGroups = true
	ch.config.RequireMentionInGroups = false
	if !ch.shouldHandleIncomingMessage(true, false, false) {
		t.Fatalf("group should be allowed when mention is not required")
	}
}
