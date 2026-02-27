package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
	"clawgo/pkg/logger"
)

type FeishuChannel struct {
	*BaseChannel
	config   config.FeishuConfig
	client   *lark.Client
	wsClient *larkws.Client

	mu        sync.Mutex
	runCancel cancelGuard
}

func (c *FeishuChannel) SupportsAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "", "send":
		return true
	default:
		return false
	}
}

func NewFeishuChannel(cfg config.FeishuConfig, bus *bus.MessageBus) (*FeishuChannel, error) {
	base := NewBaseChannel("feishu", cfg, bus, cfg.AllowFrom)

	return &FeishuChannel{
		BaseChannel: base,
		config:      cfg,
		client:      lark.NewClient(cfg.AppID, cfg.AppSecret),
	}, nil
}

func (c *FeishuChannel) Start(ctx context.Context) error {
	if c.IsRunning() {
		return nil
	}
	if c.config.AppID == "" || c.config.AppSecret == "" {
		return fmt.Errorf("feishu app_id or app_secret is empty")
	}

	dispatcher := larkdispatcher.NewEventDispatcher(c.config.VerificationToken, c.config.EncryptKey).
		OnP2MessageReceiveV1(c.handleMessageReceive)

	runCtx, cancel := context.WithCancel(ctx)
	c.runCancel.set(cancel)

	c.mu.Lock()
	c.wsClient = larkws.NewClient(
		c.config.AppID,
		c.config.AppSecret,
		larkws.WithEventHandler(dispatcher),
	)
	wsClient := c.wsClient
	c.mu.Unlock()

	c.setRunning(true)
	logger.InfoC("feishu", "Feishu channel started (websocket mode)")

	runChannelTask("feishu", "websocket", func() error {
		return wsClient.Start(runCtx)
	}, func(_ error) {
		c.setRunning(false)
	})

	return nil
}

func (c *FeishuChannel) Stop(ctx context.Context) error {
	if !c.IsRunning() {
		return nil
	}
	c.mu.Lock()
	c.wsClient = nil
	c.mu.Unlock()
	c.runCancel.cancelAndClear()

	c.setRunning(false)
	logger.InfoC("feishu", "Feishu channel stopped")
	return nil
}

func (c *FeishuChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("feishu channel not running")
	}

	if msg.ChatID == "" {
		return fmt.Errorf("chat ID is empty")
	}
	action := strings.ToLower(strings.TrimSpace(msg.Action))
	if action != "" && action != "send" {
		return fmt.Errorf("unsupported feishu action: %s", action)
	}

	msgType, contentPayload, err := buildFeishuOutbound(msg)
	if err != nil {
		return err
	}

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(msg.ChatID).
			MsgType(msgType).
			Content(contentPayload).
			Uuid(fmt.Sprintf("clawgo-%d", time.Now().UnixNano())).
			Build()).
		Build()

	resp, err := c.client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to send feishu message: %w", err)
	}

	if !resp.Success() {
		return fmt.Errorf("feishu api error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	logger.DebugCF("feishu", "Feishu message sent", map[string]interface{}{
		logger.FieldChatID: msg.ChatID,
	})

	return nil
}

func (c *FeishuChannel) handleMessageReceive(_ context.Context, event *larkim.P2MessageReceiveV1) error {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return nil
	}

	message := event.Event.Message
	sender := event.Event.Sender

	chatID := stringValue(message.ChatId)
	if chatID == "" {
		return nil
	}
	chatType := strings.ToLower(strings.TrimSpace(stringValue(message.ChatType)))
	if !c.isAllowedChat(chatID, chatType) {
		logger.WarnCF("feishu", "Feishu message rejected by chat allowlist", map[string]interface{}{
			logger.FieldSenderID: extractFeishuSenderID(sender),
			logger.FieldChatID:   chatID,
			"chat_type":         chatType,
		})
		return nil
	}

	senderID := extractFeishuSenderID(sender)
	if senderID == "" {
		senderID = "unknown"
	}

	content := extractFeishuMessageContent(message)
	if content == "" {
		content = "[empty message]"
	}
	if !c.shouldHandleGroupMessage(chatType, content) {
		logger.DebugCF("feishu", "Ignoring group message without mention/command", map[string]interface{}{
			logger.FieldSenderID: senderID,
			logger.FieldChatID:   chatID,
		})
		return nil
	}

	metadata := map[string]string{}
	if messageID := stringValue(message.MessageId); messageID != "" {
		metadata["message_id"] = messageID
	}
	if messageType := stringValue(message.MessageType); messageType != "" {
		metadata["message_type"] = messageType
	}
	if chatType := stringValue(message.ChatType); chatType != "" {
		metadata["chat_type"] = chatType
	}
	if sender != nil && sender.TenantKey != nil {
		metadata["tenant_key"] = *sender.TenantKey
	}

	logger.InfoCF("feishu", "Feishu message received", map[string]interface{}{
		logger.FieldSenderID: senderID,
		logger.FieldChatID:   chatID,
		logger.FieldPreview:  truncateString(content, 80),
	})

	c.HandleMessage(senderID, chatID, content, nil, metadata)
	return nil
}

func (c *FeishuChannel) isAllowedChat(chatID, chatType string) bool {
	chatID = strings.TrimSpace(chatID)
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	isGroup := chatType != "" && chatType != "p2p"
	if isGroup && !c.config.EnableGroups {
		return false
	}
	if len(c.config.AllowChats) == 0 {
		return true
	}
	for _, allowed := range c.config.AllowChats {
		if strings.TrimSpace(allowed) == chatID {
			return true
		}
	}
	return false
}

func (c *FeishuChannel) shouldHandleGroupMessage(chatType, content string) bool {
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	isGroup := chatType != "" && chatType != "p2p"
	if !isGroup {
		return true
	}
	if !c.config.RequireMentionInGroups {
		return true
	}
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "/") {
		return true
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "@") || strings.Contains(lower, "<at") {
		return true
	}
	return false
}

func buildFeishuOutbound(msg bus.OutboundMessage) (string, string, error) {
	content := strings.TrimSpace(msg.Content)

	// Support Feishu interactive card when content is raw card JSON.
	if looksLikeFeishuCard(content) {
		return larkim.MsgTypeInteractive, content, nil
	}

	if looksLikeMarkdown(content) {
		postPayload, err := buildFeishuPostContent(content)
		if err != nil {
			return "", "", fmt.Errorf("failed to marshal feishu post content: %w", err)
		}
		return larkim.MsgTypePost, postPayload, nil
	}

	textPayload, err := json.Marshal(map[string]string{"text": normalizeFeishuText(content)})
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal feishu text content: %w", err)
	}
	return larkim.MsgTypeText, string(textPayload), nil
}

func looksLikeFeishuCard(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || (!strings.HasPrefix(s, "{") && !strings.HasPrefix(s, "[")) {
		return false
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		return false
	}
	_, hasElements := obj["elements"]
	_, hasHeader := obj["header"]
	_, hasConfig := obj["config"]
	return hasElements || hasHeader || hasConfig
}

func looksLikeMarkdown(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}
	markers := []string{"```", "# ", "## ", "- ", "* ", "**", "`", "[", "]("}
	for _, m := range markers {
		if strings.Contains(trimmed, m) {
			return true
		}
	}
	return false
}

func buildFeishuPostContent(s string) (string, error) {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	contentRows := make([][]map[string]string, 0, len(lines))
	for _, line := range lines {
		n := normalizeFeishuText(line)
		if strings.TrimSpace(n) == "" {
			continue
		}
		contentRows = append(contentRows, []map[string]string{{"tag": "text", "text": n}})
	}
	if len(contentRows) == 0 {
		contentRows = append(contentRows, []map[string]string{{"tag": "text", "text": " "}})
	}
	payload := map[string]interface{}{
		"zh_cn": map[string]interface{}{
			"title":   "",
			"content": contentRows,
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func normalizeFeishuText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	// Headers: "## title" -> "title"
	s = regexp.MustCompile(`(?m)^#{1,6}\s+`).ReplaceAllString(s, "")
	// Bullet styles
	s = regexp.MustCompile(`(?m)^[-*]\s+`).ReplaceAllString(s, "• ")
	// Ordered list to bullet for readability
	s = regexp.MustCompile(`(?m)^\d+\.\s+`).ReplaceAllString(s, "• ")
	// Bold/italic/strike markers
	s = regexp.MustCompile(`\*\*(.*?)\*\*`).ReplaceAllString(s, `$1`)
	s = regexp.MustCompile(`__(.*?)__`).ReplaceAllString(s, `$1`)
	s = regexp.MustCompile(`\*(.*?)\*`).ReplaceAllString(s, `$1`)
	s = regexp.MustCompile(`_(.*?)_`).ReplaceAllString(s, `$1`)
	s = regexp.MustCompile(`~~(.*?)~~`).ReplaceAllString(s, `$1`)
	// Inline code markers keep content
	s = regexp.MustCompile("`([^`]+)`").ReplaceAllString(s, "$1")
	// Markdown link: [text](url) -> text (url)
	s = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`).ReplaceAllString(s, `$1 ($2)`)
	return strings.TrimSpace(s)
}

func extractFeishuSenderID(sender *larkim.EventSender) string {
	if sender == nil || sender.SenderId == nil {
		return ""
	}

	if sender.SenderId.UserId != nil && *sender.SenderId.UserId != "" {
		return *sender.SenderId.UserId
	}
	if sender.SenderId.OpenId != nil && *sender.SenderId.OpenId != "" {
		return *sender.SenderId.OpenId
	}
	if sender.SenderId.UnionId != nil && *sender.SenderId.UnionId != "" {
		return *sender.SenderId.UnionId
	}

	return ""
}

func extractFeishuMessageContent(message *larkim.EventMessage) string {
	if message == nil || message.Content == nil || *message.Content == "" {
		return ""
	}

	if message.MessageType != nil && *message.MessageType == larkim.MsgTypeText {
		var textPayload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(*message.Content), &textPayload); err == nil {
			return textPayload.Text
		}
	}

	return *message.Content
}

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
