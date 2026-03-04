package channels

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoutil"

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
	"clawgo/pkg/logger"
)

const (
	telegramDownloadTimeout        = 30 * time.Second
	telegramAPICallTimeout         = 15 * time.Second
	telegramMaxConcurrentHandlers  = 32
	telegramStopWaitHandlersPeriod = 5 * time.Second
	telegramSafeHTMLMaxRunes       = 3500
	telegramStreamPreviewMaxRunes  = 3200
)

type TelegramChannel struct {
	*BaseChannel
	bot         *telego.Bot
	config      config.TelegramConfig
	chatIDs     map[string]int64
	chatIDsMu   sync.RWMutex
	updates     <-chan telego.Update
	runCancel   cancelGuard
	handleSem   chan struct{}
	handleWG    sync.WaitGroup
	botUsername string
	streamMu    sync.Mutex
	streamState map[string]telegramStreamState
}

type telegramStreamState struct {
	MessageID   int
	LastContent string
}

func (c *TelegramChannel) SupportsAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "", "send", "edit", "delete", "react", "stream":
		return true
	default:
		return false
	}
}

func NewTelegramChannel(cfg config.TelegramConfig, bus *bus.MessageBus) (*TelegramChannel, error) {
	bot, err := telego.NewBot(cfg.Token, telego.WithDefaultLogger(false, false))
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot: %w", err)
	}

	base := NewBaseChannel("telegram", cfg, bus, cfg.AllowFrom)

	return &TelegramChannel{
		BaseChannel: base,
		bot:         bot,
		config:      cfg,
		chatIDs:     make(map[string]int64),
		handleSem:   make(chan struct{}, telegramMaxConcurrentHandlers),
		streamState: make(map[string]telegramStreamState),
	}, nil
}

func withTelegramAPITimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, telegramAPICallTimeout)
}

func (c *TelegramChannel) HealthCheck(ctx context.Context) error {
	if !c.IsRunning() {
		return fmt.Errorf("telegram bot not running")
	}
	hCtx, cancel := withTelegramAPITimeout(ctx)
	defer cancel()
	_, err := c.bot.GetMe(hCtx)
	return err
}

func (c *TelegramChannel) Start(ctx context.Context) error {
	if c.IsRunning() {
		return nil
	}
	logger.InfoC("telegram", logger.C0054)

	runCtx, cancel := context.WithCancel(ctx)
	c.runCancel.set(cancel)

	updates, err := c.bot.UpdatesViaLongPolling(runCtx, nil)
	if err != nil {
		return fmt.Errorf("failed to start updates polling: %w", err)
	}
	c.updates = updates

	c.setRunning(true)

	getMeCtx, cancelGetMe := withTelegramAPITimeout(ctx)
	botInfo, err := c.bot.GetMe(getMeCtx)
	cancelGetMe()
	if err != nil {
		return fmt.Errorf("failed to get bot info: %w", err)
	}
	c.botUsername = strings.ToLower(strings.TrimSpace(botInfo.Username))
	logger.InfoCF("telegram", logger.C0055, map[string]interface{}{
		"username": botInfo.Username,
	})

	go func() {
		for {
			select {
			case <-runCtx.Done():
				return
			case update, ok := <-updates:
				if !ok {
					logger.WarnC("telegram", logger.C0056)
					c.setRunning(false)

					select {
					case <-runCtx.Done():
						return
					case <-time.After(5 * time.Second):
					}

					newUpdates, err := c.bot.UpdatesViaLongPolling(runCtx, nil)
					if err != nil {
						logger.ErrorCF("telegram", logger.C0057, map[string]interface{}{
							logger.FieldError: err.Error(),
						})
						continue
					}

					updates = newUpdates
					c.updates = newUpdates
					c.setRunning(true)
					logger.InfoC("telegram", logger.C0058)
					continue
				}
				if update.Message != nil {
					c.dispatchHandleMessage(runCtx, update.Message)
				} else if update.CallbackQuery != nil {
					c.handleCallbackQuery(runCtx, update.CallbackQuery)
				}
			}
		}
	}()

	return nil
}

func (c *TelegramChannel) Stop(ctx context.Context) error {
	if !c.IsRunning() {
		return nil
	}
	logger.InfoC("telegram", logger.C0059)
	c.setRunning(false)
	c.runCancel.cancelAndClear()

	done := make(chan struct{})
	go func() {
		c.handleWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(telegramStopWaitHandlersPeriod):
		logger.WarnC("telegram", logger.C0060)
	}

	return nil
}

func (c *TelegramChannel) dispatchHandleMessage(runCtx context.Context, message *telego.Message) {
	if message == nil {
		return
	}
	c.handleWG.Add(1)
	go func(msg *telego.Message) {
		defer c.handleWG.Done()

		select {
		case <-runCtx.Done():
			return
		case c.handleSem <- struct{}{}:
		}
		defer func() { <-c.handleSem }()
		defer func() {
			if r := recover(); r != nil {
				logger.ErrorCF("telegram", logger.C0061, map[string]interface{}{
					"panic": fmt.Sprintf("%v", r),
				})
			}
		}()

		c.handleMessage(runCtx, msg)
	}(message)
}

func (c *TelegramChannel) handleCallbackQuery(ctx context.Context, query *telego.CallbackQuery) {
	if query == nil || query.Message == nil {
		return
	}

	senderID := fmt.Sprintf("%d", query.From.ID)
	chatID := fmt.Sprintf("%d", query.Message.GetChat().ID)

	answerCtx, cancel := withTelegramAPITimeout(ctx)
	_ = c.bot.AnswerCallbackQuery(answerCtx, &telego.AnswerCallbackQueryParams{
		CallbackQueryID: query.ID,
	})
	cancel()

	logger.InfoCF("telegram", logger.C0062, map[string]interface{}{
		"sender_id": senderID,
		"data":      query.Data,
	})

	if !c.IsAllowed(senderID) {
		return
	}

	c.HandleMessage(senderID, chatID, query.Data, nil, map[string]string{
		"is_callback": "true",
		"callback_id": query.ID,
	})
}

func (c *TelegramChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("telegram bot not running")
	}

	chatIDInt, err := parseChatID(msg.ChatID)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}
	chatID := telegoutil.ID(chatIDInt)

	action := strings.ToLower(strings.TrimSpace(msg.Action))
	if action == "" {
		action = "send"
	}
	if action != "send" {
		return c.handleAction(ctx, chatIDInt, action, msg)
	}
	streamKey := telegramStreamKey(chatIDInt, msg.ReplyToID)

	htmlContent := sanitizeTelegramHTML(markdownToTelegramHTML(msg.Content))

	if strings.TrimSpace(msg.Media) != "" {
		return c.sendMedia(ctx, chatIDInt, msg, htmlContent)
	}

	var markup *telego.InlineKeyboardMarkup
	if len(msg.Buttons) > 0 {
		var rows [][]telego.InlineKeyboardButton
		for _, row := range msg.Buttons {
			var buttons []telego.InlineKeyboardButton
			for _, btn := range row {
				buttons = append(buttons, telegoutil.InlineKeyboardButton(btn.Text).WithCallbackData(btn.Data))
			}
			rows = append(rows, buttons)
		}
		markup = telegoutil.InlineKeyboard(rows...)
	}

	if len([]rune(htmlContent)) > 3500 {
		chunks := splitTelegramMarkdown(msg.Content, 3000)
		for i, ch := range chunks {
			htmlChunk := sanitizeTelegramHTML(markdownToTelegramHTML(ch))
			sendParams := telegoutil.Message(chatID, htmlChunk).WithParseMode(telego.ModeHTML)
			if i == 0 {
				if markup != nil {
					sendParams.WithReplyMarkup(markup)
				}
				if replyID, ok := parseTelegramMessageID(msg.ReplyToID); ok {
					sendParams.ReplyParameters = &telego.ReplyParameters{MessageID: replyID}
				}
			}
			sendCtx, cancelSend := withTelegramAPITimeout(ctx)
			_, err := c.bot.SendMessage(sendCtx, sendParams)
			cancelSend()
			if err != nil {
				return err
			}
		}
		c.streamMu.Lock()
		delete(c.streamState, streamKey)
		c.streamMu.Unlock()
		return nil
	}

	sendParams := telegoutil.Message(chatID, htmlContent).WithParseMode(telego.ModeHTML)
	if markup != nil {
		sendParams.WithReplyMarkup(markup)
	}
	if replyID, ok := parseTelegramMessageID(msg.ReplyToID); ok {
		sendParams.ReplyParameters = &telego.ReplyParameters{MessageID: replyID}
	}

	sendCtx, cancelSend := withTelegramAPITimeout(ctx)
	_, err = c.bot.SendMessage(sendCtx, sendParams)
	cancelSend()

	if err != nil {
		logger.WarnCF("telegram", logger.C0063, map[string]interface{}{
			logger.FieldError: err.Error(),
		})
		plain := plainTextFromTelegramHTML(htmlContent)
		chunks := splitTelegramText(plain, 3500)
		for i, ch := range chunks {
			sendPlainParams := telegoutil.Message(chatID, ch)
			if i == 0 && markup != nil {
				sendPlainParams.WithReplyMarkup(markup)
			}
			sendPlainCtx, cancelSendPlain := withTelegramAPITimeout(ctx)
			_, err = c.bot.SendMessage(sendPlainCtx, sendPlainParams)
			cancelSendPlain()
			if err != nil {
				return err
			}
		}
		c.streamMu.Lock()
		delete(c.streamState, streamKey)
		c.streamMu.Unlock()
		return nil
	}
	c.streamMu.Lock()
	delete(c.streamState, streamKey)
	c.streamMu.Unlock()

	return nil
}

func (c *TelegramChannel) sendMedia(ctx context.Context, chatID int64, msg bus.OutboundMessage, htmlCaption string) error {
	media := strings.TrimSpace(msg.Media)
	if media == "" {
		return fmt.Errorf("empty media")
	}

	method := "sendDocument"
	field := "document"
	lower := strings.ToLower(media)
	if strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") || strings.HasSuffix(lower, ".png") || strings.HasSuffix(lower, ".webp") || strings.HasSuffix(lower, ".gif") {
		method = "sendPhoto"
		field = "photo"
	}

	replyID, hasReply := parseTelegramMessageID(msg.ReplyToID)
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/%s", c.config.Token, method)

	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		vals := url.Values{}
		vals.Set("chat_id", strconv.FormatInt(chatID, 10))
		vals.Set(field, media)
		if strings.TrimSpace(htmlCaption) != "" {
			vals.Set("caption", htmlCaption)
			vals.Set("parse_mode", "HTML")
		}
		if hasReply {
			vals.Set("reply_to_message_id", strconv.Itoa(replyID))
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(vals.Encode()))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			return fmt.Errorf("telegram media send failed: status=%d body=%s", resp.StatusCode, string(body))
		}
		return nil
	}

	f, err := os.Open(media)
	if err != nil {
		return err
	}
	defer f.Close()

	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	_ = w.WriteField("chat_id", strconv.FormatInt(chatID, 10))
	if strings.TrimSpace(htmlCaption) != "" {
		_ = w.WriteField("caption", htmlCaption)
		_ = w.WriteField("parse_mode", "HTML")
	}
	if hasReply {
		_ = w.WriteField("reply_to_message_id", strconv.Itoa(replyID))
	}
	part, err := w.CreateFormFile(field, filepath.Base(media))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, &b)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("telegram media send failed: status=%d body=%s", resp.StatusCode, string(body))
	}
	return nil
}

func (c *TelegramChannel) isAllowedChat(chatID int64, chatType string) bool {
	// Private chats are governed by allow_from (sender allowlist), not allow_chats.
	if chatType == telego.ChatTypePrivate {
		return true
	}
	if len(c.config.AllowChats) == 0 {
		return true
	}
	sid := fmt.Sprintf("%d", chatID)
	for _, allowed := range c.config.AllowChats {
		if allowed == sid {
			return true
		}
	}
	return false
}

func (c *TelegramChannel) shouldHandleGroupMessage(message *telego.Message, content string) bool {
	if message == nil {
		return false
	}
	if message.Chat.Type == telego.ChatTypePrivate {
		return true
	}
	if !c.config.EnableGroups {
		return false
	}
	if !c.config.RequireMentionInGroups {
		return true
	}
	if message.ReplyToMessage != nil && message.ReplyToMessage.From != nil && message.ReplyToMessage.From.IsBot {
		return true
	}
	if strings.HasPrefix(content, "/") {
		return true
	}
	if c.botUsername != "" && strings.Contains(strings.ToLower(content), "@"+c.botUsername) {
		return true
	}
	return false
}

func (c *TelegramChannel) handleMessage(runCtx context.Context, message *telego.Message) {
	if message == nil {
		return
	}

	user := message.From
	if user == nil {
		return
	}
	if user.IsBot {
		logger.DebugCF("telegram", logger.C0064, map[string]interface{}{
			"user_id": user.ID,
		})
		return
	}

	senderID := fmt.Sprintf("%d", user.ID)

	chatID := message.Chat.ID
	c.chatIDsMu.Lock()
	c.chatIDs[senderID] = chatID
	c.chatIDsMu.Unlock()

	content := ""
	mediaPaths := []string{}

	if message.Text != "" {
		content += message.Text
	}

	if message.Caption != "" {
		if content != "" {
			content += "\n"
		}
		content += message.Caption
	}

	if message.Photo != nil && len(message.Photo) > 0 {
		photo := message.Photo[len(message.Photo)-1]
		photoPath := c.downloadFile(runCtx, photo.FileID, ".jpg", "")
		if photoPath != "" {
			mediaPaths = append(mediaPaths, photoPath)
			if content != "" {
				content += "\n"
			}
			content += fmt.Sprintf("[image: %s]", photoPath)
		}
	}

	if message.Voice != nil {
		voicePath := c.downloadFile(runCtx, message.Voice.FileID, ".ogg", "")
		if voicePath != "" {
			mediaPaths = append(mediaPaths, voicePath)
			if content != "" {
				content += "\n"
			}
			content += fmt.Sprintf("[voice: %s]", voicePath)
		}
	}

	if message.Audio != nil {
		audioPath := c.downloadFile(runCtx, message.Audio.FileID, ".mp3", message.Audio.FileName)
		if audioPath != "" {
			mediaPaths = append(mediaPaths, audioPath)
			if content != "" {
				content += "\n"
			}
			content += fmt.Sprintf("[audio: %s]", audioPath)
		}
	}

	if message.Document != nil {
		docPath := c.downloadFile(runCtx, message.Document.FileID, "", message.Document.FileName)
		if docPath != "" {
			mediaPaths = append(mediaPaths, docPath)
			if content != "" {
				content += "\n"
			}
			content += fmt.Sprintf("[file: %s]", docPath)
		}
	}

	if content == "" {
		content = "[empty message]"
	}

	if !c.isAllowedChat(chatID, message.Chat.Type) {
		logger.WarnCF("telegram", logger.C0065, map[string]interface{}{
			logger.FieldSenderID: senderID,
			logger.FieldChatID:   chatID,
		})
		return
	}
	if !c.shouldHandleGroupMessage(message, content) {
		logger.DebugCF("telegram", logger.C0048, map[string]interface{}{
			logger.FieldSenderID: senderID,
			logger.FieldChatID:   chatID,
		})
		return
	}

	logger.InfoCF("telegram", logger.C0066, map[string]interface{}{
		logger.FieldSenderID: senderID,
		logger.FieldPreview:  truncateString(content, 50),
	})

	if !c.IsAllowed(senderID) {
		logger.WarnCF("telegram", logger.C0067, map[string]interface{}{
			logger.FieldSenderID: senderID,
			logger.FieldChatID:   chatID,
		})
		return
	}

	apiCtx, cancelAPI := context.WithTimeout(runCtx, telegramAPICallTimeout)
	_ = c.bot.SendChatAction(apiCtx, &telego.SendChatActionParams{
		ChatID: telegoutil.ID(chatID),
		Action: telego.ChatActionTyping,
	})
	cancelAPI()

	metadata := map[string]string{
		"message_id": fmt.Sprintf("%d", message.MessageID),
		"user_id":    fmt.Sprintf("%d", user.ID),
		"username":   user.Username,
		"first_name": user.FirstName,
		"is_group":   fmt.Sprintf("%t", message.Chat.Type != telego.ChatTypePrivate),
	}

	c.HandleMessage(senderID, fmt.Sprintf("%d", chatID), content, mediaPaths, metadata)
}

func (c *TelegramChannel) downloadFile(runCtx context.Context, fileID, ext, fileName string) string {
	getFileCtx, cancelGetFile := context.WithTimeout(runCtx, telegramAPICallTimeout)
	file, err := c.bot.GetFile(getFileCtx, &telego.GetFileParams{FileID: fileID})
	cancelGetFile()
	if err != nil {
		return ""
	}

	if file.FilePath == "" {
		return ""
	}

	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", c.config.Token, file.FilePath)
	mediaDir := filepath.Join(os.TempDir(), "clawgo_media")
	_ = os.MkdirAll(mediaDir, 0755)
	finalExt := ext
	if finalExt == "" {
		if fromName := filepath.Ext(fileName); fromName != "" {
			finalExt = fromName
		} else if fromPath := filepath.Ext(file.FilePath); fromPath != "" {
			finalExt = fromPath
		}
	}
	localPath := filepath.Join(mediaDir, fileID[:min(16, len(fileID))]+finalExt)

	if err := c.downloadFromURL(runCtx, url, localPath); err != nil {
		return ""
	}

	return localPath
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func splitTelegramMarkdown(s string, maxRunes int) []string {
	if s == "" {
		return []string{""}
	}
	if maxRunes <= 0 {
		maxRunes = 3000
	}
	parts := splitTelegramText(s, maxRunes)
	if len(parts) <= 1 {
		return parts
	}
	// Keep fenced code blocks balanced across chunks.
	for i := 0; i < len(parts)-1; i++ {
		if strings.Count(parts[i], "```")%2 == 1 {
			parts[i] += "\n```"
			parts[i+1] = "```\n" + parts[i+1]
		}
	}
	return parts
}

func splitTelegramText(s string, maxRunes int) []string {
	if s == "" {
		return []string{""}
	}
	if maxRunes <= 0 {
		maxRunes = 3500
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return []string{s}
	}
	out := make([]string, 0, len(r)/maxRunes+1)
	for start := 0; start < len(r); {
		end := start + maxRunes
		if end >= len(r) {
			out = append(out, string(r[start:]))
			break
		}
		split := end
		for i := end; i > start+maxRunes/2; i-- {
			if r[i-1] == '\n' || r[i-1] == ' ' {
				split = i
				break
			}
		}
		out = append(out, string(r[start:split]))
		start = split
	}
	cleaned := make([]string, 0, len(out))
	for _, part := range out {
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	if len(cleaned) == 0 {
		return []string{""}
	}
	return cleaned
}

func (c *TelegramChannel) downloadFromURL(runCtx context.Context, url, localPath string) error {
	downloadCtx, cancelDownload := context.WithTimeout(runCtx, telegramDownloadTimeout)
	defer cancelDownload()

	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: telegramDownloadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status: %d", resp.StatusCode)
	}

	out, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func parseChatID(chatIDStr string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(chatIDStr, "%d", &id)
	return id, err
}

func telegramStreamKey(chatID int64, replyToID string) string {
	return fmt.Sprintf("%d:%s", chatID, strings.TrimSpace(replyToID))
}

func tailRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return "…\n" + string(r[len(r)-maxRunes:])
}

func clampTelegramHTML(markdown string, maxRunes int) string {
	if maxRunes <= 0 {
		maxRunes = telegramSafeHTMLMaxRunes
	}
	htmlContent := sanitizeTelegramHTML(markdownToTelegramHTML(markdown))
	if len([]rune(htmlContent)) <= maxRunes {
		return htmlContent
	}
	chunks := splitTelegramMarkdown(markdown, maxRunes-400)
	if len(chunks) == 0 {
		return ""
	}
	return sanitizeTelegramHTML(markdownToTelegramHTML(chunks[0]))
}

func (c *TelegramChannel) handleStreamAction(ctx context.Context, chatID int64, msg bus.OutboundMessage) error {
	streamKey := telegramStreamKey(chatID, msg.ReplyToID)
	content := tailRunes(msg.Content, telegramStreamPreviewMaxRunes)
	htmlContent := clampTelegramHTML(content, telegramSafeHTMLMaxRunes)
	if strings.TrimSpace(htmlContent) == "" {
		return nil
	}

	c.streamMu.Lock()
	state := c.streamState[streamKey]
	if state.LastContent == htmlContent {
		c.streamMu.Unlock()
		return nil
	}
	c.streamMu.Unlock()

	if state.MessageID == 0 {
		sendParams := telegoutil.Message(telegoutil.ID(chatID), htmlContent).WithParseMode(telego.ModeHTML)
		if replyID, ok := parseTelegramMessageID(msg.ReplyToID); ok {
			sendParams.ReplyParameters = &telego.ReplyParameters{MessageID: replyID}
		}
		sendCtx, cancel := withTelegramAPITimeout(ctx)
		sent, err := c.bot.SendMessage(sendCtx, sendParams)
		cancel()
		if err != nil {
			plain := tailRunes(plainTextFromTelegramHTML(htmlContent), telegramSafeHTMLMaxRunes)
			if strings.TrimSpace(plain) == "" {
				return err
			}
			sendPlainCtx, cancelPlain := withTelegramAPITimeout(ctx)
			sent, err = c.bot.SendMessage(sendPlainCtx, telegoutil.Message(telegoutil.ID(chatID), plain))
			cancelPlain()
			if err != nil {
				return err
			}
		}
		c.streamMu.Lock()
		c.streamState[streamKey] = telegramStreamState{MessageID: sent.MessageID, LastContent: htmlContent}
		c.streamMu.Unlock()
		return nil
	}

	editCtx, cancel := withTelegramAPITimeout(ctx)
	_, err := c.bot.EditMessageText(editCtx, &telego.EditMessageTextParams{
		ChatID:    telegoutil.ID(chatID),
		MessageID: state.MessageID,
		Text:      htmlContent,
		ParseMode: telego.ModeHTML,
	})
	cancel()
	if err != nil {
		errText := strings.ToLower(err.Error())
		if strings.Contains(errText, "message is not modified") {
			return nil
		}
		if strings.Contains(errText, "message is too long") || strings.Contains(errText, "can't parse entities") {
			plain := tailRunes(plainTextFromTelegramHTML(htmlContent), telegramSafeHTMLMaxRunes)
			if strings.TrimSpace(plain) == "" {
				return err
			}
			editPlainCtx, cancelPlain := withTelegramAPITimeout(ctx)
			_, err = c.bot.EditMessageText(editPlainCtx, &telego.EditMessageTextParams{
				ChatID:    telegoutil.ID(chatID),
				MessageID: state.MessageID,
				Text:      plain,
			})
			cancelPlain()
			if err != nil && !strings.Contains(strings.ToLower(err.Error()), "message is not modified") {
				return err
			}
		} else {
			return err
		}
	}

	c.streamMu.Lock()
	state.LastContent = htmlContent
	c.streamState[streamKey] = state
	c.streamMu.Unlock()
	return nil
}

func (c *TelegramChannel) handleAction(ctx context.Context, chatID int64, action string, msg bus.OutboundMessage) error {
	messageID, ok := parseTelegramMessageID(msg.MessageID)
	if !ok && action != "send" && action != "stream" {
		return fmt.Errorf("message_id required for action=%s", action)
	}
	switch action {
	case "edit":
		htmlContent := clampTelegramHTML(msg.Content, telegramSafeHTMLMaxRunes)
		editCtx, cancel := withTelegramAPITimeout(ctx)
		defer cancel()
		_, err := c.bot.EditMessageText(editCtx, &telego.EditMessageTextParams{ChatID: telegoutil.ID(chatID), MessageID: messageID, Text: htmlContent, ParseMode: telego.ModeHTML})
		return err
	case "stream":
		return c.handleStreamAction(ctx, chatID, msg)
	case "delete":
		delCtx, cancel := withTelegramAPITimeout(ctx)
		defer cancel()
		return c.bot.DeleteMessage(delCtx, &telego.DeleteMessageParams{ChatID: telegoutil.ID(chatID), MessageID: messageID})
	case "react":
		reactCtx, cancel := withTelegramAPITimeout(ctx)
		defer cancel()
		emoji := strings.TrimSpace(msg.Emoji)
		if emoji == "" {
			return fmt.Errorf("emoji required for react action")
		}
		return c.bot.SetMessageReaction(reactCtx, &telego.SetMessageReactionParams{
			ChatID:    telegoutil.ID(chatID),
			MessageID: messageID,
			Reaction:  []telego.ReactionType{&telego.ReactionTypeEmoji{Emoji: emoji}},
		})
	default:
		return fmt.Errorf("unsupported telegram action: %s", action)
	}
}

func parseTelegramMessageID(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	var id int
	if _, err := fmt.Sscanf(raw, "%d", &id); err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func markdownToTelegramHTML(text string) string {
	if text == "" {
		return ""
	}

	codeBlocks := extractCodeBlocks(text)
	text = codeBlocks.text

	inlineCodes := extractInlineCodes(text)
	text = inlineCodes.text

	text = escapeHTML(text)

	text = regexp.MustCompile("(?m)^#{1,6}\\s+(.+)$").ReplaceAllString(text, "<b>$1</b>")
	text = regexp.MustCompile("(?m)^>\\s*(.*)$").ReplaceAllString(text, "│ $1")
	text = regexp.MustCompile("\\[([^\\]]+)\\]\\(([^)]+)\\)").ReplaceAllString(text, `<a href="$2">$1</a>`)
	text = regexp.MustCompile("\\*\\*(.+?)\\*\\*").ReplaceAllString(text, "<b>$1</b>")
	text = regexp.MustCompile("__(.+?)__").ReplaceAllString(text, "<b>$1</b>")
	text = regexp.MustCompile("\\*([^*\\n]+)\\*").ReplaceAllString(text, "<i>$1</i>")
	text = regexp.MustCompile("_([^_\\n]+)_").ReplaceAllString(text, "<i>$1</i>")
	text = regexp.MustCompile("~~(.+?)~~").ReplaceAllString(text, "<s>$1</s>")
	text = regexp.MustCompile("(?m)^[-*]\\s+").ReplaceAllString(text, "• ")
	text = regexp.MustCompile("(?m)^\\d+\\.\\s+").ReplaceAllString(text, "• ")

	for i, code := range inlineCodes.codes {
		escaped := escapeHTML(code)
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00IC%d\x00", i), fmt.Sprintf("<code>%s</code>", escaped))
	}

	for i, code := range codeBlocks.codes {
		escaped := escapeHTML(code)
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00CB%d\x00", i), fmt.Sprintf("<pre><code>%s</code></pre>", escaped))
	}

	return text
}

type codeBlockMatch struct {
	text  string
	codes []string
}

func extractCodeBlocks(text string) codeBlockMatch {
	re := regexp.MustCompile("```[\\w]*\\n?([\\s\\S]*?)```")
	matches := re.FindAllStringSubmatch(text, -1)

	codes := make([]string, 0, len(matches))
	for _, match := range matches {
		codes = append(codes, match[1])
	}

	index := 0
	text = re.ReplaceAllStringFunc(text, func(m string) string {
		placeholder := fmt.Sprintf("\x00CB%d\x00", index)
		index++
		return placeholder
	})

	return codeBlockMatch{text: text, codes: codes}
}

type inlineCodeMatch struct {
	text  string
	codes []string
}

func extractInlineCodes(text string) inlineCodeMatch {
	re := regexp.MustCompile("`([^`]+)`")
	matches := re.FindAllStringSubmatch(text, -1)

	codes := make([]string, 0, len(matches))
	for _, match := range matches {
		codes = append(codes, match[1])
	}

	index := 0
	text = re.ReplaceAllStringFunc(text, func(m string) string {
		placeholder := fmt.Sprintf("\x00IC%d\x00", index)
		index++
		return placeholder
	})

	return inlineCodeMatch{text: text, codes: codes}
}

func escapeHTML(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}

var telegramAllowedTags = map[string]bool{
	"b":      true,
	"strong": true,
	"i":      true,
	"em":     true,
	"u":      true,
	"s":      true,
	"strike": true,
	"del":    true,
	"code":   true,
	"pre":    true,
	"a":      true,
}

func sanitizeTelegramHTML(input string) string {
	if input == "" {
		return ""
	}

	tagRe := regexp.MustCompile("(?is)<\\s*(/?)\\s*([a-z0-9]+)([^>]*)>")
	hrefRe := regexp.MustCompile("(?is)\\bhref\\s*=\\s*\"([^\"]+)\"")

	var out strings.Builder
	stack := make([]string, 0, 16)
	pos := 0

	matches := tagRe.FindAllStringSubmatchIndex(input, -1)
	for _, m := range matches {
		start, end := m[0], m[1]
		out.WriteString(input[pos:start])

		isClose := strings.TrimSpace(input[m[2]:m[3]]) == "/"
		tagName := strings.ToLower(strings.TrimSpace(input[m[4]:m[5]]))
		attrRaw := ""
		if m[6] >= 0 && m[7] >= 0 {
			attrRaw = input[m[6]:m[7]]
		}

		if !telegramAllowedTags[tagName] {
			out.WriteString(escapeHTML(input[start:end]))
			pos = end
			continue
		}

		if isClose {
			found := -1
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i] == tagName {
					found = i
					break
				}
			}
			if found == -1 {
				pos = end
				continue
			}
			for i := len(stack) - 1; i >= found; i-- {
				out.WriteString("</" + stack[i] + ">")
			}
			stack = stack[:found]
			pos = end
			continue
		}

		if tagName == "a" {
			hrefMatch := hrefRe.FindStringSubmatch(attrRaw)
			if len(hrefMatch) < 2 {
				out.WriteString("&lt;a&gt;")
				pos = end
				continue
			}
			href := strings.TrimSpace(hrefMatch[1])
			if !isSafeTelegramHref(href) {
				out.WriteString("&lt;a&gt;")
				pos = end
				continue
			}
			out.WriteString(`<a href="` + escapeHTMLAttr(href) + `">`)
		} else {
			out.WriteString("<" + tagName + ">")
		}
		stack = append(stack, tagName)
		pos = end
	}

	out.WriteString(input[pos:])
	for i := len(stack) - 1; i >= 0; i-- {
		out.WriteString("</" + stack[i] + ">")
	}
	return out.String()
}

func isSafeTelegramHref(href string) bool {
	lower := strings.ToLower(strings.TrimSpace(href))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "tg://")
}

func escapeHTMLAttr(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, `"`, "&quot;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}

func plainTextFromTelegramHTML(text string) string {
	tagRe := regexp.MustCompile("(?is)<[^>]+>")
	plain := tagRe.ReplaceAllString(text, "")
	plain = strings.ReplaceAll(plain, "&lt;", "<")
	plain = strings.ReplaceAll(plain, "&gt;", ">")
	plain = strings.ReplaceAll(plain, "&quot;", "\"")
	plain = strings.ReplaceAll(plain, "&amp;", "&")
	return plain
}
