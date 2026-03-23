//go:build !omit_weixin

package channels

import (
	"context"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
)

func TestBuildAndSplitWeixinChatID(t *testing.T) {
	chatID := buildWeixinChatID("bot-a", "wx-user-1")
	if chatID != "bot-a|wx-user-1" {
		t.Fatalf("unexpected composite chat id: %s", chatID)
	}
	botID, rawChatID := splitWeixinChatID(chatID)
	if botID != "bot-a" || rawChatID != "wx-user-1" {
		t.Fatalf("unexpected split result: %s %s", botID, rawChatID)
	}
}

func TestWeixinHandleInboundMessageUsesCompositeSessionChatID(t *testing.T) {
	mb := bus.NewMessageBus()
	ch, err := NewWeixinChannel(config.WeixinConfig{
		BaseURL: "https://ilinkai.weixin.qq.com",
		Accounts: []config.WeixinAccountConfig{
			{BotID: "bot-a", BotToken: "token-a"},
		},
	}, mb)
	if err != nil {
		t.Fatalf("new weixin channel: %v", err)
	}

	ch.handleInboundMessage("bot-a", weixinInboundMessage{
		FromUserID:   "wx-user-1",
		ContextToken: "ctx-1",
		ItemList: []weixinMessageItem{
			{Type: 1, TextItem: struct {
				Text string `json:"text"`
			}{Text: "hello"}},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	msg, ok := mb.ConsumeInbound(ctx)
	if !ok {
		t.Fatalf("expected inbound message")
	}
	if msg.ChatID != "bot-a|wx-user-1" {
		t.Fatalf("expected composite chat id, got %s", msg.ChatID)
	}
	if msg.SessionKey != "weixin:bot-a|wx-user-1" {
		t.Fatalf("expected composite session key, got %s", msg.SessionKey)
	}
	if msg.SenderID != "wx-user-1" {
		t.Fatalf("expected raw sender id, got %s", msg.SenderID)
	}
}

func TestWeixinResolveAccountForCompositeChatID(t *testing.T) {
	mb := bus.NewMessageBus()
	ch, err := NewWeixinChannel(config.WeixinConfig{
		BaseURL:      "https://ilinkai.weixin.qq.com",
		DefaultBotID: "bot-b",
		Accounts: []config.WeixinAccountConfig{
			{BotID: "bot-a", BotToken: "token-a", ContextToken: "ctx-a"},
			{BotID: "bot-b", BotToken: "token-b", ContextToken: "ctx-b"},
		},
	}, mb)
	if err != nil {
		t.Fatalf("new weixin channel: %v", err)
	}

	account, rawChatID, contextToken, err := ch.resolveAccountForChat("bot-a|wx-user-7")
	if err != nil {
		t.Fatalf("resolve account: %v", err)
	}
	if account.cfg.BotID != "bot-a" {
		t.Fatalf("expected bot-a, got %s", account.cfg.BotID)
	}
	if rawChatID != "wx-user-7" {
		t.Fatalf("expected raw chat id wx-user-7, got %s", rawChatID)
	}
	if contextToken != "ctx-a" {
		t.Fatalf("expected context token ctx-a, got %s", contextToken)
	}
}

func TestWeixinSetDefaultAccount(t *testing.T) {
	mb := bus.NewMessageBus()
	ch, err := NewWeixinChannel(config.WeixinConfig{
		BaseURL: "https://ilinkai.weixin.qq.com",
		Accounts: []config.WeixinAccountConfig{
			{BotID: "bot-a", BotToken: "token-a"},
			{BotID: "bot-b", BotToken: "token-b"},
		},
	}, mb)
	if err != nil {
		t.Fatalf("new weixin channel: %v", err)
	}

	if err := ch.SetDefaultAccount("bot-b"); err != nil {
		t.Fatalf("set default account: %v", err)
	}
	accounts := ch.ListAccounts()
	if len(accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accounts))
	}
	defaultCount := 0
	for _, account := range accounts {
		if account.Default {
			defaultCount++
			if account.BotID != "bot-b" {
				t.Fatalf("expected bot-b to be default, got %s", account.BotID)
			}
		}
	}
	if defaultCount != 1 {
		t.Fatalf("expected exactly one default account, got %d", defaultCount)
	}
}

func TestWeixinCancelPendingLogin(t *testing.T) {
	mb := bus.NewMessageBus()
	ch, err := NewWeixinChannel(config.WeixinConfig{BaseURL: "https://ilinkai.weixin.qq.com"}, mb)
	if err != nil {
		t.Fatalf("new weixin channel: %v", err)
	}
	ch.pendingLogins["login-1"] = &WeixinPendingLogin{LoginID: "login-1", QRCode: "code-1", Status: "wait"}
	ch.loginOrder = []string{"login-1"}

	if ok := ch.CancelPendingLogin("login-1"); !ok {
		t.Fatalf("expected cancel to succeed")
	}
	if got := ch.PendingLogins(); len(got) != 0 {
		t.Fatalf("expected no pending logins after cancel, got %d", len(got))
	}
}
