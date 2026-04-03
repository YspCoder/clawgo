//go:build !omit_weixin

package channels

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
)

type weixinRoundTripFunc func(*http.Request) (*http.Response, error)

func (f weixinRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

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

func TestWeixinSendSessionExpiredTriggersPause(t *testing.T) {
	mb := bus.NewMessageBus()
	ch, err := NewWeixinChannel(config.WeixinConfig{
		BaseURL: "https://ilinkai.weixin.qq.com",
		Accounts: []config.WeixinAccountConfig{
			{BotID: "bot-a", BotToken: "token-a", ContextToken: "ctx-a"},
		},
		DefaultBotID: "bot-a",
	}, mb)
	if err != nil {
		t.Fatalf("new weixin channel: %v", err)
	}
	ch.setRunning(true)
	ch.httpClient = &http.Client{Transport: weixinRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"ret":-14,"errcode":0,"errmsg":"expired"}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}

	err = ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "bot-a|wx-user-1",
		Action:  "send",
		Content: "hello",
	})
	if err == nil {
		t.Fatalf("expected send error")
	}
	if !strings.Contains(err.Error(), "sendmessage failed") {
		t.Fatalf("unexpected send error: %v", err)
	}
	if remaining := ch.remainingPause(); remaining <= 0 {
		t.Fatalf("expected session pause > 0, got %s", remaining)
	}
}

func TestWeixinGetUpdatesSessionExpiredTriggersPause(t *testing.T) {
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
	ch.httpClient = &http.Client{Transport: weixinRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"ret":0,"errcode":-14,"errmsg":"expired"}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}

	_, err = ch.getUpdates(context.Background(), config.WeixinAccountConfig{
		BotID:    "bot-a",
		BotToken: "token-a",
	}, time.Second)
	if err == nil {
		t.Fatalf("expected getupdates error")
	}
	if _, ok := err.(*weixinAPIStatusError); !ok {
		t.Fatalf("expected weixinAPIStatusError, got %T", err)
	}
	if remaining := ch.remainingPause(); remaining <= 0 {
		t.Fatalf("expected session pause > 0, got %s", remaining)
	}
}

func TestWeixinHeadersForAuthAndLogin(t *testing.T) {
	mb := bus.NewMessageBus()
	ch, err := NewWeixinChannel(config.WeixinConfig{
		BaseURL: "https://ilinkai.weixin.qq.com",
		Accounts: []config.WeixinAccountConfig{
			{BotID: "bot-a", BotToken: "token-a", ContextToken: "ctx-a"},
		},
		DefaultBotID: "bot-a",
	}, mb)
	if err != nil {
		t.Fatalf("new weixin channel: %v", err)
	}
	ch.setRunning(true)

	var mu sync.Mutex
	requests := map[string]*http.Request{}
	ch.httpClient = &http.Client{Transport: weixinRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		requests[req.URL.Path] = req.Clone(req.Context())
		mu.Unlock()
		switch req.URL.Path {
		case "/ilink/bot/get_bot_qrcode":
			body := `{"qrcode":"abc","qrcode_img_content":"img"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		case "/ilink/bot/sendmessage":
			body := `{"ret":0,"errcode":0}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader("not found")),
				Header:     make(http.Header),
			}, nil
		}
	})}

	if _, err := ch.StartLogin(context.Background()); err != nil {
		t.Fatalf("start login: %v", err)
	}
	if err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "bot-a|wx-user-1",
		Action:  "send",
		Content: "hello",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}

	mu.Lock()
	loginReq := requests["/ilink/bot/get_bot_qrcode"]
	sendReq := requests["/ilink/bot/sendmessage"]
	mu.Unlock()

	if loginReq == nil || sendReq == nil {
		t.Fatalf("expected both login and send requests")
	}
	if got := loginReq.Header.Get("iLink-App-Id"); got != weixinIlinkAppID {
		t.Fatalf("login iLink-App-Id = %q", got)
	}
	if got := loginReq.Header.Get("iLink-App-ClientVersion"); got != weixinClientVersion {
		t.Fatalf("login iLink-App-ClientVersion = %q", got)
	}
	if loginReq.Header.Get("AuthorizationType") != "" || loginReq.Header.Get("Authorization") != "" || loginReq.Header.Get("X-WECHAT-UIN") != "" {
		t.Fatalf("login request should not include auth headers")
	}

	if got := sendReq.Header.Get("iLink-App-Id"); got != weixinIlinkAppID {
		t.Fatalf("send iLink-App-Id = %q", got)
	}
	if got := sendReq.Header.Get("iLink-App-ClientVersion"); got != weixinClientVersion {
		t.Fatalf("send iLink-App-ClientVersion = %q", got)
	}
	if got := sendReq.Header.Get("AuthorizationType"); got != "ilink_bot_token" {
		t.Fatalf("send AuthorizationType = %q", got)
	}
	if got := sendReq.Header.Get("Authorization"); got != "Bearer token-a" {
		t.Fatalf("send Authorization = %q", got)
	}
	if sendReq.Header.Get("X-WECHAT-UIN") == "" {
		t.Fatalf("send X-WECHAT-UIN should not be empty")
	}
}

func TestWeixinGetTypingTicketCachesAndFallsBack(t *testing.T) {
	mb := bus.NewMessageBus()
	ch, err := NewWeixinChannel(config.WeixinConfig{
		BaseURL: "https://ilinkai.weixin.qq.com",
		Accounts: []config.WeixinAccountConfig{
			{BotID: "bot-a", BotToken: "token-a", IlinkUserID: "u-1"},
		},
	}, mb)
	if err != nil {
		t.Fatalf("new weixin channel: %v", err)
	}

	var calls int
	ch.httpClient = &http.Client{Transport: weixinRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			body := `{"ret":0,"errcode":0,"typing_ticket":"ticket-1"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}
		body := `{"ret":1,"errcode":1,"errmsg":"bad"}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}

	account := config.WeixinAccountConfig{
		BotID:       "bot-a",
		BotToken:    "token-a",
		IlinkUserID: "u-1",
	}

	ticket, err := ch.getTypingTicket(context.Background(), account, "ctx-1")
	if err != nil {
		t.Fatalf("first get typing ticket: %v", err)
	}
	if ticket != "ticket-1" {
		t.Fatalf("first ticket = %q", ticket)
	}

	ticket, err = ch.getTypingTicket(context.Background(), account, "ctx-1")
	if err != nil {
		t.Fatalf("cached get typing ticket: %v", err)
	}
	if ticket != "ticket-1" {
		t.Fatalf("cached ticket = %q", ticket)
	}
	if calls != 1 {
		t.Fatalf("expected 1 upstream call for cache hit, got %d", calls)
	}

	ch.typingMu.Lock()
	entry := ch.typingCache["bot-a"]
	entry.nextFetchAt = time.Now().Add(-time.Second)
	ch.typingCache["bot-a"] = entry
	ch.typingMu.Unlock()

	ticket, err = ch.getTypingTicket(context.Background(), account, "ctx-1")
	if err != nil {
		t.Fatalf("fallback get typing ticket: %v", err)
	}
	if ticket != "ticket-1" {
		t.Fatalf("fallback ticket = %q", ticket)
	}
	if calls != 2 {
		t.Fatalf("expected 2 upstream calls, got %d", calls)
	}
	ch.typingMu.Lock()
	defer ch.typingMu.Unlock()
	if ch.typingCache["bot-a"].retryDelay < weixinConfigRetryInitial {
		t.Fatalf("expected retry delay >= initial, got %s", ch.typingCache["bot-a"].retryDelay)
	}
}

func TestPollDelayForAttempt(t *testing.T) {
	if got := pollDelayForAttempt(1); got != weixinRetryDelay {
		t.Fatalf("attempt 1 delay = %s", got)
	}
	if got := pollDelayForAttempt(weixinMaxConsecutiveFails); got != weixinBackoffDelay {
		t.Fatalf("threshold delay = %s", got)
	}
}

func TestWeixinValidateAPIStatusErrorShape(t *testing.T) {
	mb := bus.NewMessageBus()
	ch, err := NewWeixinChannel(config.WeixinConfig{BaseURL: "https://ilinkai.weixin.qq.com"}, mb)
	if err != nil {
		t.Fatalf("new weixin channel: %v", err)
	}
	err = ch.validateAPIStatus("sendmessage", 1, 2, "bad")
	if err == nil {
		t.Fatalf("expected error")
	}
	apiErr, ok := err.(*weixinAPIStatusError)
	if !ok {
		t.Fatalf("expected weixinAPIStatusError")
	}
	b, _ := json.Marshal(apiErr.Error())
	if len(b) == 0 {
		t.Fatalf("marshal error text")
	}
}
