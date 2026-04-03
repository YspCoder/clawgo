//go:build !omit_weixin

package channels

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/logger"
)

const (
	weixinDefaultBaseURL = "https://ilinkai.weixin.qq.com"
	weixinDefaultTimeout = 45 * time.Second
	weixinRetryDelay     = 2 * time.Second
	weixinBackoffDelay   = 30 * time.Second
	weixinChannelVersion = "2.1.1"
	weixinIlinkAppID     = "bot"
	// 2.1.1 encoded as 0x00MMNNPP => 0x00020101 => 131329
	weixinClientVersion       = "131329"
	weixinMaxConsecutiveFails = 3
	weixinPersistDelay        = 1200 * time.Millisecond
	weixinDefaultPollTimeout  = 35 * time.Second
	weixinSessionExpiredCode  = -14
	weixinSessionPauseWindow  = time.Hour
	weixinConfigCacheTTL      = 24 * time.Hour
	weixinConfigRetryInitial  = 2 * time.Second
	weixinConfigRetryMax      = time.Hour
)

type WeixinChannel struct {
	*BaseChannel
	config     config.WeixinConfig
	configPath string
	httpClient *http.Client
	runCancel  cancelGuard
	runCtx     context.Context
	onPersist  func(string)

	mu            sync.RWMutex
	accounts      map[string]*weixinAccountState
	accountOrder  []string
	chatBindings  map[string]string
	chatContexts  map[string]string
	pollers       map[string]context.CancelFunc
	pendingLogins map[string]*WeixinPendingLogin
	loginOrder    []string
	persistTimer  *time.Timer
	typingMu      sync.Mutex
	typingCache   map[string]weixinTypingCacheEntry
	pauseMu       sync.Mutex
	pauseUntil    time.Time
}

type weixinTypingCacheEntry struct {
	ticket      string
	nextFetchAt time.Time
	retryDelay  time.Duration
}

type WeixinAccountSnapshot struct {
	BotID           string `json:"bot_id"`
	IlinkUserID     string `json:"ilink_user_id,omitempty"`
	ContextToken    string `json:"context_token,omitempty"`
	GetUpdatesBuf   string `json:"get_updates_buf,omitempty"`
	Connected       bool   `json:"connected"`
	LastEvent       string `json:"last_event,omitempty"`
	LastError       string `json:"last_error,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
	Default         bool   `json:"default"`
	LastInboundAt   string `json:"last_inbound_at,omitempty"`
	LastInboundChat string `json:"last_inbound_chat,omitempty"`
	LastInboundText string `json:"last_inbound_text,omitempty"`
}

type WeixinPendingLogin struct {
	LoginID          string `json:"login_id,omitempty"`
	QRCode           string `json:"qr_code,omitempty"`
	QRCodeImgContent string `json:"qr_code_img_content,omitempty"`
	Status           string `json:"status,omitempty"`
	LastError        string `json:"last_error,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
}

type weixinAccountState struct {
	cfg             config.WeixinAccountConfig
	connected       bool
	lastEvent       string
	lastError       string
	updatedAt       time.Time
	lastInboundAt   time.Time
	lastInboundChat string
	lastInboundText string
}

type weixinGetUpdatesResponse struct {
	Ret                  int                    `json:"ret"`
	Errcode              int                    `json:"errcode"`
	Errmsg               string                 `json:"errmsg"`
	ErrMsg               string                 `json:"err_msg"`
	GetUpdatesBuf        string                 `json:"get_updates_buf"`
	LongpollingTimeoutMs int                    `json:"longpolling_timeout_ms"`
	Msgs                 []weixinInboundMessage `json:"msgs"`
}

type weixinInboundMessage struct {
	FromUserID   string              `json:"from_user_id"`
	ContextToken string              `json:"context_token"`
	ItemList     []weixinMessageItem `json:"item_list"`
}

type weixinMessageItem struct {
	Type     int `json:"type"`
	TextItem struct {
		Text string `json:"text"`
	} `json:"text_item"`
}

type weixinAPIResponse struct {
	Ret     int    `json:"ret"`
	Errcode int    `json:"errcode"`
	Errmsg  string `json:"errmsg"`
	ErrMsg  string `json:"err_msg"`
}

type weixinAPIStatusError struct {
	Operation string
	Ret       int
	Errcode   int
	Message   string
}

func (e *weixinAPIStatusError) Error() string {
	return fmt.Sprintf("%s failed: ret=%d errcode=%d msg=%s", e.Operation, e.Ret, e.Errcode, e.Message)
}

type weixinQRCodeResponse struct {
	QRcode           string `json:"qrcode"`
	QRcodeImgContent string `json:"qrcode_img_content"`
}

type weixinQRCodeStatusResponse struct {
	Status      string `json:"status"`
	BotToken    string `json:"bot_token"`
	IlinkBotID  string `json:"ilink_bot_id"`
	IlinkUserID string `json:"ilink_user_id"`
}

func NewWeixinChannel(cfg config.WeixinConfig, messageBus *bus.MessageBus) (*WeixinChannel, error) {
	base := NewBaseChannel("weixin", cfg, messageBus, cfg.AllowFrom)
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = weixinDefaultBaseURL
	}
	cfg.BaseURL = baseURL

	ch := &WeixinChannel{
		BaseChannel:   base,
		config:        cfg,
		httpClient:    &http.Client{Timeout: weixinDefaultTimeout},
		accounts:      map[string]*weixinAccountState{},
		chatBindings:  map[string]string{},
		chatContexts:  map[string]string{},
		pollers:       map[string]context.CancelFunc{},
		pendingLogins: map[string]*WeixinPendingLogin{},
		typingCache:   map[string]weixinTypingCacheEntry{},
	}
	for _, account := range normalizeWeixinAccounts(cfg) {
		ch.accounts[account.BotID] = &weixinAccountState{cfg: account}
		ch.accountOrder = append(ch.accountOrder, account.BotID)
	}
	sort.Strings(ch.accountOrder)
	return ch, nil
}

func (c *WeixinChannel) SupportsAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "", "send", "typing":
		return true
	default:
		return false
	}
}

func (c *WeixinChannel) SetConfigPath(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.configPath = strings.TrimSpace(path)
}

func (c *WeixinChannel) SetPersistHook(fn func(string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onPersist = fn
}

func (c *WeixinChannel) Start(ctx context.Context) error {
	if c.IsRunning() {
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	c.runCancel.set(cancel)
	c.runCtx = runCtx
	c.setRunning(true)

	logger.InfoCF("weixin", 0, map[string]interface{}{
		"accounts": len(c.accountOrder),
		"base_url": c.config.BaseURL,
	})

	c.mu.RLock()
	accountIDs := append([]string(nil), c.accountOrder...)
	c.mu.RUnlock()
	for _, botID := range accountIDs {
		c.startAccountPoller(botID)
	}
	return nil
}

func (c *WeixinChannel) Stop(ctx context.Context) error {
	if !c.IsRunning() {
		return nil
	}
	c.runCancel.cancelAndClear()
	c.setRunning(false)

	c.mu.Lock()
	defer c.mu.Unlock()
	for botID, cancel := range c.pollers {
		cancel()
		delete(c.pollers, botID)
	}
	if c.persistTimer != nil {
		c.persistTimer.Stop()
		c.persistTimer = nil
	}
	return nil
}

func (c *WeixinChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("weixin channel not running")
	}
	if err := c.ensureSessionActive(); err != nil {
		return err
	}

	action := strings.ToLower(strings.TrimSpace(msg.Action))
	switch action {
	case "", "send":
		return c.sendMessage(ctx, msg)
	case "typing":
		return c.sendTyping(ctx, strings.TrimSpace(msg.ChatID), 1)
	default:
		return fmt.Errorf("unsupported weixin action %q", msg.Action)
	}
}

func (c *WeixinChannel) StartLogin(ctx context.Context) (*WeixinPendingLogin, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.BaseURL+"/ilink/bot/get_bot_qrcode?bot_type=3", nil)
	if err != nil {
		return nil, err
	}
	c.applyHeaders(req, false, "", false)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload weixinQRCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if strings.TrimSpace(payload.QRcode) == "" {
		return nil, fmt.Errorf("empty qrcode returned")
	}
	loginID := weixinClientID()
	pending := &WeixinPendingLogin{
		LoginID:          loginID,
		QRCode:           strings.TrimSpace(payload.QRcode),
		QRCodeImgContent: strings.TrimSpace(firstNonEmpty(payload.QRcodeImgContent, payload.QRcode)),
		Status:           "wait",
		UpdatedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	c.mu.Lock()
	c.pendingLogins[loginID] = pending
	c.loginOrder = append(c.loginOrder, loginID)
	c.mu.Unlock()
	return clonePendingLogin(pending), nil
}

func (c *WeixinChannel) RefreshLoginStatuses(ctx context.Context) ([]*WeixinPendingLogin, error) {
	c.mu.RLock()
	loginIDs := append([]string(nil), c.loginOrder...)
	c.mu.RUnlock()
	for _, loginID := range loginIDs {
		if err := c.refreshLoginStatus(ctx, loginID); err != nil {
			return nil, err
		}
	}
	return c.PendingLogins(), nil
}

func (c *WeixinChannel) PendingLogins() []*WeixinPendingLogin {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*WeixinPendingLogin, 0, len(c.loginOrder))
	for _, loginID := range c.loginOrder {
		if pending := c.pendingLogins[loginID]; pending != nil {
			out = append(out, clonePendingLogin(pending))
		}
	}
	return out
}

func (c *WeixinChannel) PendingLoginByID(loginID string) *WeixinPendingLogin {
	loginID = strings.TrimSpace(loginID)
	if loginID == "" {
		return nil
	}
	return c.pendingLogin(loginID)
}

func (c *WeixinChannel) CancelPendingLogin(loginID string) bool {
	loginID = strings.TrimSpace(loginID)
	if loginID == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pendingLogins[loginID] == nil {
		return false
	}
	delete(c.pendingLogins, loginID)
	filtered := c.loginOrder[:0]
	for _, item := range c.loginOrder {
		if item != loginID {
			filtered = append(filtered, item)
		}
	}
	c.loginOrder = append([]string(nil), filtered...)
	return true
}

func (c *WeixinChannel) ListAccounts() []WeixinAccountSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	defaultBotID := c.defaultBotIDLocked()
	out := make([]WeixinAccountSnapshot, 0, len(c.accountOrder))
	for _, botID := range c.accountOrder {
		state := c.accounts[botID]
		if state == nil {
			continue
		}
		out = append(out, WeixinAccountSnapshot{
			BotID:           state.cfg.BotID,
			IlinkUserID:     state.cfg.IlinkUserID,
			ContextToken:    state.cfg.ContextToken,
			GetUpdatesBuf:   state.cfg.GetUpdatesBuf,
			Connected:       state.connected,
			LastEvent:       state.lastEvent,
			LastError:       state.lastError,
			UpdatedAt:       formatTime(state.updatedAt),
			Default:         botID == defaultBotID,
			LastInboundAt:   formatTime(state.lastInboundAt),
			LastInboundChat: state.lastInboundChat,
			LastInboundText: state.lastInboundText,
		})
	}
	return out
}

func (c *WeixinChannel) SetDefaultAccount(botID string) error {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return fmt.Errorf("bot_id is required")
	}
	c.mu.Lock()
	if c.accounts[botID] == nil {
		c.mu.Unlock()
		return fmt.Errorf("bot_id not found: %s", botID)
	}
	c.config.DefaultBotID = botID
	c.schedulePersistLocked()
	c.mu.Unlock()
	return nil
}

func (c *WeixinChannel) RemoveAccount(botID string) error {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return fmt.Errorf("bot_id is required")
	}

	c.mu.Lock()
	if cancel := c.pollers[botID]; cancel != nil {
		cancel()
		delete(c.pollers, botID)
	}
	delete(c.accounts, botID)
	filtered := c.accountOrder[:0]
	for _, item := range c.accountOrder {
		if item != botID {
			filtered = append(filtered, item)
		}
	}
	c.accountOrder = append([]string(nil), filtered...)
	for chatID, owner := range c.chatBindings {
		if owner == botID {
			delete(c.chatBindings, chatID)
			delete(c.chatContexts, chatID)
		}
	}
	if strings.TrimSpace(c.config.DefaultBotID) == botID {
		c.config.DefaultBotID = ""
	}
	c.schedulePersistLocked()
	c.mu.Unlock()
	return nil
}

func (c *WeixinChannel) sendMessage(ctx context.Context, msg bus.OutboundMessage) error {
	chatID := strings.TrimSpace(msg.ChatID)
	if chatID == "" {
		return fmt.Errorf("weixin chat_id is required")
	}

	account, rawChatID, contextToken, err := c.resolveAccountForChat(chatID)
	if err != nil {
		return err
	}

	reqBody := map[string]interface{}{
		"msg": map[string]interface{}{
			"from_user_id":  "",
			"to_user_id":    rawChatID,
			"client_id":     weixinClientID(),
			"message_type":  2,
			"message_state": 2,
			"context_token": contextToken,
			"item_list": []map[string]interface{}{
				{
					"type": 1,
					"text_item": map[string]string{
						"text": msg.Content,
					},
				},
			},
		},
		"base_info": map[string]string{
			"channel_version": weixinChannelVersion,
		},
	}

	var resp weixinAPIResponse
	if err := c.doJSON(ctx, "/ilink/bot/sendmessage", reqBody, &resp, account.cfg.BotToken); err != nil {
		c.updateAccountError(account.cfg.BotID, err)
		return err
	}
	if err := c.validateAPIStatus("sendmessage", resp.Ret, resp.Errcode, firstNonEmpty(resp.Errmsg, resp.ErrMsg)); err != nil {
		if c.isSessionExpiredError(err) {
			c.pauseSession("sendmessage", resp.Ret, resp.Errcode, firstNonEmpty(resp.Errmsg, resp.ErrMsg))
		}
		c.updateAccountError(account.cfg.BotID, err)
		return err
	}
	c.updateAccountEvent(account.cfg.BotID, "outbound_sent", true, nil)
	return nil
}

func (c *WeixinChannel) sendTyping(ctx context.Context, chatID string, status int) error {
	if err := c.ensureSessionActive(); err != nil {
		return err
	}
	account, _, contextToken, err := c.resolveAccountForChat(chatID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(account.cfg.IlinkUserID) == "" {
		return fmt.Errorf("weixin ilink_user_id is required for typing")
	}

	ticket, err := c.getTypingTicket(ctx, account.cfg, contextToken)
	if err != nil {
		return err
	}

	reqBody := map[string]interface{}{
		"ilink_user_id": account.cfg.IlinkUserID,
		"typing_ticket": ticket,
		"status":        status,
		"base_info": map[string]string{
			"channel_version": weixinChannelVersion,
		},
	}

	var resp weixinAPIResponse
	if err := c.doJSON(ctx, "/ilink/bot/sendtyping", reqBody, &resp, account.cfg.BotToken); err != nil {
		return err
	}
	if err := c.validateAPIStatus("sendtyping", resp.Ret, resp.Errcode, firstNonEmpty(resp.Errmsg, resp.ErrMsg)); err != nil {
		if c.isSessionExpiredError(err) {
			c.pauseSession("sendtyping", resp.Ret, resp.Errcode, firstNonEmpty(resp.Errmsg, resp.ErrMsg))
		}
		return err
	}
	return nil
}

func (c *WeixinChannel) getTypingTicket(ctx context.Context, account config.WeixinAccountConfig, contextToken string) (string, error) {
	key := strings.TrimSpace(account.BotID)
	if key == "" {
		key = strings.TrimSpace(account.IlinkUserID)
	}
	now := time.Now()
	c.typingMu.Lock()
	entry, ok := c.typingCache[key]
	if ok && now.Before(entry.nextFetchAt) {
		ticket := entry.ticket
		c.typingMu.Unlock()
		return ticket, nil
	}
	cachedTicket := entry.ticket
	retryDelay := entry.retryDelay
	c.typingMu.Unlock()

	reqBody := map[string]interface{}{
		"ilink_user_id": account.IlinkUserID,
		"context_token": contextToken,
		"base_info": map[string]string{
			"channel_version": weixinChannelVersion,
		},
	}
	var resp struct {
		Ret          int    `json:"ret"`
		Errcode      int    `json:"errcode"`
		Errmsg       string `json:"errmsg"`
		ErrMsg       string `json:"err_msg"`
		TypingTicket string `json:"typing_ticket"`
	}
	if err := c.doJSON(ctx, "/ilink/bot/getconfig", reqBody, &resp, account.BotToken); err != nil {
		c.storeTypingCacheFailure(key, cachedTicket, retryDelay, now)
		if cachedTicket != "" {
			return cachedTicket, nil
		}
		return "", err
	}
	if err := c.validateAPIStatus("getconfig", resp.Ret, resp.Errcode, firstNonEmpty(resp.Errmsg, resp.ErrMsg)); err != nil {
		c.storeTypingCacheFailure(key, cachedTicket, retryDelay, now)
		if c.isSessionExpiredError(err) {
			c.pauseSession("getconfig", resp.Ret, resp.Errcode, firstNonEmpty(resp.Errmsg, resp.ErrMsg))
		}
		if cachedTicket != "" {
			return cachedTicket, nil
		}
		return "", err
	}
	ticket := strings.TrimSpace(resp.TypingTicket)
	if ticket == "" {
		c.storeTypingCacheFailure(key, cachedTicket, retryDelay, now)
		if cachedTicket != "" {
			return cachedTicket, nil
		}
		return "", fmt.Errorf("getconfig failed: empty typing_ticket")
	}
	c.typingMu.Lock()
	c.typingCache[key] = weixinTypingCacheEntry{
		ticket:      ticket,
		nextFetchAt: now.Add(weixinConfigCacheTTL),
		retryDelay:  weixinConfigRetryInitial,
	}
	c.typingMu.Unlock()
	return ticket, nil
}

func (c *WeixinChannel) pollAccount(ctx context.Context, botID string) {
	consecutiveFails := 0
	pollTimeout := weixinDefaultPollTimeout
	for {
		if ctx.Err() != nil {
			return
		}
		if err := c.waitWhileSessionPaused(ctx); err != nil {
			return
		}
		account, ok := c.accountConfig(botID)
		if !ok {
			return
		}
		resp, err := c.getUpdates(ctx, account, pollTimeout)
		if err != nil {
			c.updateAccountError(botID, err)
			consecutiveFails++
			if c.isSessionExpiredError(err) {
				continue
			}
			delay := pollDelayForAttempt(consecutiveFails)
			logger.WarnCF("weixin", 0, map[string]interface{}{
				"operation": "getupdates",
				"bot_id":    botID,
				"attempt":   consecutiveFails,
				"delay_ms":  delay.Milliseconds(),
				"error":     err.Error(),
			})
			if !sleepWithContext(ctx, delay) {
				return
			}
			continue
		}
		consecutiveFails = 0

		if resp.LongpollingTimeoutMs > 0 {
			pollTimeout = time.Duration(resp.LongpollingTimeoutMs+5000) * time.Millisecond
		}

		if next := strings.TrimSpace(resp.GetUpdatesBuf); next != "" {
			c.mu.Lock()
			if state := c.accounts[botID]; state != nil {
				if state.cfg.GetUpdatesBuf != next {
					state.cfg.GetUpdatesBuf = next
					c.schedulePersistLocked()
				}
			}
			c.mu.Unlock()
		}

		for _, msg := range resp.Msgs {
			c.handleInboundMessage(botID, msg)
		}
		c.updateAccountEvent(botID, "poll_ok", true, nil)
	}
}

func (c *WeixinChannel) getUpdates(ctx context.Context, account config.WeixinAccountConfig, timeout time.Duration) (*weixinGetUpdatesResponse, error) {
	reqBody := map[string]interface{}{
		"get_updates_buf": strings.TrimSpace(account.GetUpdatesBuf),
		"base_info": map[string]string{
			"channel_version": weixinChannelVersion,
		},
	}

	var resp weixinGetUpdatesResponse
	if err := c.doJSONWithTimeout(ctx, "/ilink/bot/getupdates", reqBody, &resp, account.BotToken, timeout); err != nil {
		return nil, err
	}
	if err := c.validateAPIStatus("getupdates", resp.Ret, resp.Errcode, firstNonEmpty(resp.Errmsg, resp.ErrMsg)); err != nil {
		if c.isSessionExpiredError(err) {
			c.pauseSession("getupdates", resp.Ret, resp.Errcode, firstNonEmpty(resp.Errmsg, resp.ErrMsg))
		}
		return nil, err
	}
	return &resp, nil
}

func (c *WeixinChannel) handleInboundMessage(botID string, msg weixinInboundMessage) {
	rawChatID := strings.TrimSpace(msg.FromUserID)
	if rawChatID == "" {
		return
	}
	chatID := buildWeixinChatID(botID, rawChatID)
	contextToken := strings.TrimSpace(msg.ContextToken)

	c.mu.Lock()
	if contextToken != "" {
		c.chatContexts[chatID] = contextToken
	}
	c.chatBindings[chatID] = botID
	if state := c.accounts[botID]; state != nil {
		if contextToken != "" && state.cfg.ContextToken != contextToken {
			state.cfg.ContextToken = contextToken
			c.schedulePersistLocked()
		}
		state.lastInboundAt = time.Now().UTC()
		state.lastInboundChat = rawChatID
	}
	c.mu.Unlock()

	var textParts []string
	var itemTypes []string
	for _, item := range msg.ItemList {
		itemTypes = append(itemTypes, fmt.Sprintf("%d", item.Type))
		if item.Type == 1 {
			if text := strings.TrimSpace(item.TextItem.Text); text != "" {
				textParts = append(textParts, text)
			}
		}
	}
	content := strings.Join(textParts, "\n")
	if content == "" {
		return
	}
	c.mu.Lock()
	if state := c.accounts[botID]; state != nil {
		state.lastInboundText = truncateString(content, 120)
	}
	c.mu.Unlock()

	metadata := map[string]string{
		"bot_id":        botID,
		"context_token": contextToken,
		"item_types":    strings.Join(itemTypes, ","),
		"raw_chat_id":   rawChatID,
	}
	c.HandleMessage(rawChatID, chatID, content, nil, metadata)
	c.updateAccountEvent(botID, "inbound_message", true, nil)
}

func (c *WeixinChannel) accountConfig(botID string) (config.WeixinAccountConfig, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	state := c.accounts[botID]
	if state == nil {
		return config.WeixinAccountConfig{}, false
	}
	return state.cfg, true
}

func (c *WeixinChannel) resolveAccountForChat(chatID string) (*weixinAccountState, string, string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	chatID = strings.TrimSpace(chatID)
	if explicitBotID, rawChatID := splitWeixinChatID(chatID); explicitBotID != "" && rawChatID != "" {
		if state := c.accounts[explicitBotID]; state != nil {
			if token := strings.TrimSpace(c.chatContexts[chatID]); token != "" {
				return cloneAccountState(state), rawChatID, token, nil
			}
			if token := strings.TrimSpace(state.cfg.ContextToken); token != "" {
				return cloneAccountState(state), rawChatID, token, nil
			}
			return nil, "", "", fmt.Errorf("weixin context_token missing for chat %s", chatID)
		}
	}

	if botID := strings.TrimSpace(c.chatBindings[chatID]); botID != "" {
		if state := c.accounts[botID]; state != nil {
			rawChatID := chatID
			if _, parsedRawChatID := splitWeixinChatID(chatID); parsedRawChatID != "" {
				rawChatID = parsedRawChatID
			}
			if token := strings.TrimSpace(c.chatContexts[chatID]); token != "" {
				return cloneAccountState(state), rawChatID, token, nil
			}
			if token := strings.TrimSpace(state.cfg.ContextToken); token != "" {
				return cloneAccountState(state), rawChatID, token, nil
			}
			return nil, "", "", fmt.Errorf("weixin context_token missing for chat %s", chatID)
		}
	}

	defaultBotID := c.defaultBotIDLocked()
	if defaultBotID != "" {
		if state := c.accounts[defaultBotID]; state != nil {
			if token := strings.TrimSpace(state.cfg.ContextToken); token != "" {
				return cloneAccountState(state), chatID, token, nil
			}
			return nil, "", "", fmt.Errorf("weixin context_token missing for default bot %s", defaultBotID)
		}
	}
	return nil, "", "", fmt.Errorf("weixin no account available for chat %s", chatID)
}

func (c *WeixinChannel) addOrUpdateAccount(account config.WeixinAccountConfig) error {
	account.BotID = strings.TrimSpace(account.BotID)
	account.BotToken = strings.TrimSpace(account.BotToken)
	account.IlinkUserID = strings.TrimSpace(account.IlinkUserID)
	account.ContextToken = strings.TrimSpace(account.ContextToken)
	account.GetUpdatesBuf = strings.TrimSpace(account.GetUpdatesBuf)
	if account.BotID == "" || account.BotToken == "" {
		return fmt.Errorf("bot_id and bot_token are required")
	}

	shouldStartPoller := false
	c.mu.Lock()
	if state := c.accounts[account.BotID]; state != nil {
		state.cfg = mergeWeixinAccount(state.cfg, account)
	} else {
		c.accounts[account.BotID] = &weixinAccountState{cfg: account}
		c.accountOrder = append(c.accountOrder, account.BotID)
		sort.Strings(c.accountOrder)
	}
	if strings.TrimSpace(c.config.DefaultBotID) == "" {
		c.config.DefaultBotID = account.BotID
	}
	shouldStartPoller = c.IsRunning()
	c.schedulePersistLocked()
	c.mu.Unlock()

	if shouldStartPoller {
		c.startAccountPoller(account.BotID)
	}
	return nil
}

func (c *WeixinChannel) schedulePersistLocked() {
	if strings.TrimSpace(c.configPath) == "" {
		return
	}
	if c.persistTimer == nil {
		c.persistTimer = time.AfterFunc(weixinPersistDelay, func() {
			_ = c.persistAccounts()
		})
		return
	}
	c.persistTimer.Reset(weixinPersistDelay)
}

func (c *WeixinChannel) persistAccounts() error {
	c.mu.RLock()
	configPath := strings.TrimSpace(c.configPath)
	cfgCopy := c.config
	accounts := c.accountConfigsLocked()
	onPersist := c.onPersist
	c.mu.RUnlock()
	if configPath == "" {
		return nil
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return err
	}
	cfg.Channels.Weixin.Enabled = cfgCopy.Enabled
	cfg.Channels.Weixin.BaseURL = cfgCopy.BaseURL
	cfg.Channels.Weixin.AllowFrom = append([]string(nil), cfgCopy.AllowFrom...)
	cfg.Channels.Weixin.DefaultBotID = strings.TrimSpace(cfgCopy.DefaultBotID)
	cfg.Channels.Weixin.Accounts = accounts
	cfg.Channels.Weixin.BotID = ""
	cfg.Channels.Weixin.BotToken = ""
	cfg.Channels.Weixin.IlinkUserID = ""
	cfg.Channels.Weixin.ContextToken = ""
	cfg.Channels.Weixin.GetUpdatesBuf = ""
	err = config.SaveConfig(configPath, cfg)
	c.mu.Lock()
	if c.persistTimer != nil {
		c.persistTimer.Stop()
		c.persistTimer = nil
	}
	c.mu.Unlock()
	if err == nil && onPersist != nil {
		onPersist("weixin")
	}
	return err
}

func (c *WeixinChannel) accountConfigsLocked() []config.WeixinAccountConfig {
	out := make([]config.WeixinAccountConfig, 0, len(c.accountOrder))
	for _, botID := range c.accountOrder {
		state := c.accounts[botID]
		if state == nil {
			continue
		}
		out = append(out, state.cfg)
	}
	return out
}

func (c *WeixinChannel) startAccountPoller(botID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.startAccountPollerLocked(botID)
}

func (c *WeixinChannel) startAccountPollerLocked(botID string) {
	if !c.IsRunning() || c.runCtx == nil {
		return
	}
	if cancel := c.pollers[botID]; cancel != nil {
		cancel()
		delete(c.pollers, botID)
	}
	accountCtx, cancel := context.WithCancel(c.runCtx)
	c.pollers[botID] = cancel
	go c.pollAccount(accountCtx, botID)
}

func (c *WeixinChannel) updatePendingLogin(loginID string, mut func(*WeixinPendingLogin)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pending := c.pendingLogins[loginID]
	if pending == nil {
		return
	}
	mut(pending)
}

func (c *WeixinChannel) deletePendingLogin(loginID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pendingLogins, loginID)
	filtered := c.loginOrder[:0]
	for _, item := range c.loginOrder {
		if item != loginID {
			filtered = append(filtered, item)
		}
	}
	c.loginOrder = append([]string(nil), filtered...)
}

func (c *WeixinChannel) pendingLogin(loginID string) *WeixinPendingLogin {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return clonePendingLogin(c.pendingLogins[loginID])
}

func (c *WeixinChannel) refreshLoginStatus(ctx context.Context, loginID string) error {
	pending := c.pendingLogin(loginID)
	if pending == nil || strings.TrimSpace(pending.QRCode) == "" {
		return nil
	}

	reqURL := c.config.BaseURL + "/ilink/bot/get_qrcode_status?qrcode=" + url.QueryEscape(pending.QRCode)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	c.applyHeaders(req, false, "", false)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.updatePendingLogin(loginID, func(pl *WeixinPendingLogin) {
			pl.LastError = err.Error()
			pl.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		})
		return nil
	}
	defer resp.Body.Close()

	var status weixinQRCodeStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return err
	}

	switch strings.ToLower(strings.TrimSpace(status.Status)) {
	case "", "wait", "scaned":
		c.updatePendingLogin(loginID, func(pl *WeixinPendingLogin) {
			pl.Status = firstNonEmpty(status.Status, "wait")
			pl.LastError = ""
			pl.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		})
	case "expired":
		c.updatePendingLogin(loginID, func(pl *WeixinPendingLogin) {
			pl.Status = "expired"
			pl.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		})
	case "confirmed":
		account := config.WeixinAccountConfig{
			BotID:       strings.TrimSpace(status.IlinkBotID),
			BotToken:    strings.TrimSpace(status.BotToken),
			IlinkUserID: strings.TrimSpace(status.IlinkUserID),
		}
		if strings.TrimSpace(account.BotID) == "" || strings.TrimSpace(account.BotToken) == "" {
			return fmt.Errorf("confirmed login missing bot credentials")
		}
		if err := c.addOrUpdateAccount(account); err != nil {
			return err
		}
		c.deletePendingLogin(loginID)
	default:
		c.updatePendingLogin(loginID, func(pl *WeixinPendingLogin) {
			pl.Status = firstNonEmpty(status.Status, "unknown")
			pl.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		})
	}
	return nil
}

func (c *WeixinChannel) defaultBotIDLocked() string {
	if strings.TrimSpace(c.config.DefaultBotID) != "" {
		return strings.TrimSpace(c.config.DefaultBotID)
	}
	if len(c.accountOrder) == 1 {
		return c.accountOrder[0]
	}
	return ""
}

func (c *WeixinChannel) updateAccountEvent(botID, event string, connected bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.accounts[botID]
	if state == nil {
		return
	}
	state.connected = connected
	state.lastEvent = strings.TrimSpace(event)
	if err != nil {
		state.lastError = err.Error()
	} else if connected {
		state.lastError = ""
	}
	state.updatedAt = time.Now().UTC()
}

func (c *WeixinChannel) updateAccountError(botID string, err error) {
	c.updateAccountEvent(botID, "error", false, err)
}

func (c *WeixinChannel) validateAPIStatus(operation string, ret, errcode int, msg string) error {
	if ret == 0 && errcode == 0 {
		return nil
	}
	return &weixinAPIStatusError{
		Operation: operation,
		Ret:       ret,
		Errcode:   errcode,
		Message:   strings.TrimSpace(msg),
	}
}

func (c *WeixinChannel) isSessionExpiredError(err error) bool {
	apiErr, ok := err.(*weixinAPIStatusError)
	return ok && (apiErr.Ret == weixinSessionExpiredCode || apiErr.Errcode == weixinSessionExpiredCode)
}

func (c *WeixinChannel) pauseSession(operation string, ret, errcode int, errmsg string) time.Duration {
	c.pauseMu.Lock()
	defer c.pauseMu.Unlock()
	until := time.Now().Add(weixinSessionPauseWindow)
	if until.After(c.pauseUntil) {
		c.pauseUntil = until
	}
	remaining := time.Until(c.pauseUntil)
	logger.ErrorCF("weixin", 0, map[string]interface{}{
		"operation":   operation,
		"ret":         ret,
		"errcode":     errcode,
		"errmsg":      strings.TrimSpace(errmsg),
		"pause_until": c.pauseUntil.UTC().Format(time.RFC3339),
		"minutes":     int((remaining + time.Minute - 1) / time.Minute),
	})
	return remaining
}

func (c *WeixinChannel) remainingPause() time.Duration {
	c.pauseMu.Lock()
	defer c.pauseMu.Unlock()
	if c.pauseUntil.IsZero() {
		return 0
	}
	remaining := time.Until(c.pauseUntil)
	if remaining <= 0 {
		c.pauseUntil = time.Time{}
		return 0
	}
	return remaining
}

func (c *WeixinChannel) waitWhileSessionPaused(ctx context.Context) error {
	remaining := c.remainingPause()
	if remaining <= 0 {
		return nil
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *WeixinChannel) ensureSessionActive() error {
	remaining := c.remainingPause()
	if remaining <= 0 {
		return nil
	}
	return fmt.Errorf("weixin session paused (%d min remaining)", int((remaining+time.Minute-1)/time.Minute))
}

func pollDelayForAttempt(attempt int) time.Duration {
	if attempt >= weixinMaxConsecutiveFails {
		return weixinBackoffDelay
	}
	return weixinRetryDelay
}

func (c *WeixinChannel) storeTypingCacheFailure(key, cachedTicket string, retryDelay time.Duration, now time.Time) {
	if retryDelay <= 0 {
		retryDelay = weixinConfigRetryInitial
	} else {
		retryDelay *= 2
		if retryDelay > weixinConfigRetryMax {
			retryDelay = weixinConfigRetryMax
		}
	}
	c.typingMu.Lock()
	c.typingCache[key] = weixinTypingCacheEntry{
		ticket:      cachedTicket,
		nextFetchAt: now.Add(retryDelay),
		retryDelay:  retryDelay,
	}
	c.typingMu.Unlock()
}

func (c *WeixinChannel) doJSON(ctx context.Context, path string, payload interface{}, out interface{}, token string) error {
	return c.doJSONWithTimeout(ctx, path, payload, out, token, 0)
}

func (c *WeixinChannel) doJSONWithTimeout(ctx context.Context, path string, payload interface{}, out interface{}, token string, timeout time.Duration) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	c.applyHeaders(req, true, token, true)

	client := c.httpClient
	if timeout > 0 {
		clone := *c.httpClient
		clone.Timeout = timeout
		client = &clone
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *WeixinChannel) applyHeaders(req *http.Request, jsonBody bool, token string, withAuth bool) {
	if jsonBody {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("iLink-App-Id", weixinIlinkAppID)
	req.Header.Set("iLink-App-ClientVersion", weixinClientVersion)
	if withAuth {
		req.Header.Set("AuthorizationType", "ilink_bot_token")
		req.Header.Set("X-WECHAT-UIN", randomWeixinUIN())
		if strings.TrimSpace(token) != "" {
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
		}
	}
}

func normalizeWeixinAccounts(cfg config.WeixinConfig) []config.WeixinAccountConfig {
	out := make([]config.WeixinAccountConfig, 0, len(cfg.Accounts)+1)
	seen := map[string]struct{}{}
	add := func(account config.WeixinAccountConfig) {
		account.BotID = strings.TrimSpace(account.BotID)
		account.BotToken = strings.TrimSpace(account.BotToken)
		account.IlinkUserID = strings.TrimSpace(account.IlinkUserID)
		account.ContextToken = strings.TrimSpace(account.ContextToken)
		account.GetUpdatesBuf = strings.TrimSpace(account.GetUpdatesBuf)
		if account.BotID == "" || account.BotToken == "" {
			return
		}
		if _, ok := seen[account.BotID]; ok {
			return
		}
		seen[account.BotID] = struct{}{}
		out = append(out, account)
	}
	for _, account := range cfg.Accounts {
		add(account)
	}
	add(config.WeixinAccountConfig{
		BotID:         cfg.BotID,
		BotToken:      cfg.BotToken,
		IlinkUserID:   cfg.IlinkUserID,
		ContextToken:  cfg.ContextToken,
		GetUpdatesBuf: cfg.GetUpdatesBuf,
	})
	return out
}

func clonePendingLogin(in *WeixinPendingLogin) *WeixinPendingLogin {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

func cloneAccountState(in *weixinAccountState) *weixinAccountState {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

func mergeWeixinAccount(existing, next config.WeixinAccountConfig) config.WeixinAccountConfig {
	out := existing
	if strings.TrimSpace(next.BotToken) != "" {
		out.BotToken = strings.TrimSpace(next.BotToken)
	}
	if strings.TrimSpace(next.IlinkUserID) != "" {
		out.IlinkUserID = strings.TrimSpace(next.IlinkUserID)
	}
	if strings.TrimSpace(next.ContextToken) != "" {
		out.ContextToken = strings.TrimSpace(next.ContextToken)
	}
	if strings.TrimSpace(next.GetUpdatesBuf) != "" {
		out.GetUpdatesBuf = strings.TrimSpace(next.GetUpdatesBuf)
	}
	return out
}

func formatTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}

func randomWeixinUIN() string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	val := uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3])
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d", val)))
}

func weixinClientID() string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("clawgo-weixin:%d-%x", time.Now().UnixMilli(), buf)
}

func buildWeixinChatID(botID, rawChatID string) string {
	botID = strings.TrimSpace(botID)
	rawChatID = strings.TrimSpace(rawChatID)
	if botID == "" || rawChatID == "" {
		return rawChatID
	}
	return botID + "|" + rawChatID
}

func splitWeixinChatID(chatID string) (string, string) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return "", ""
	}
	botID, rawChatID, ok := strings.Cut(chatID, "|")
	if !ok {
		return "", chatID
	}
	botID = strings.TrimSpace(botID)
	rawChatID = strings.TrimSpace(rawChatID)
	if botID == "" || rawChatID == "" {
		return "", chatID
	}
	return botID, rawChatID
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
