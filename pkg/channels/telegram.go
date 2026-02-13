package channels

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoutil"

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
	"clawgo/pkg/logger"
	"clawgo/pkg/voice"
)

const (
	telegramDownloadTimeout        = 30 * time.Second
	telegramAPICallTimeout         = 15 * time.Second
	telegramMaxConcurrentHandlers  = 32
	telegramStopWaitHandlersPeriod = 5 * time.Second
)

type TelegramChannel struct {
	*BaseChannel
	bot          *telego.Bot
	config       config.TelegramConfig
	chatIDs      map[string]int64
	chatIDsMu    sync.RWMutex
	updates      <-chan telego.Update
	runCancel    cancelGuard
	transcriber  *voice.GroqTranscriber
	placeholders sync.Map // chatID -> messageID
	stopThinking sync.Map // chatID -> chan struct{}
	handleSem    chan struct{}
	handleWG     sync.WaitGroup
}

func NewTelegramChannel(cfg config.TelegramConfig, bus *bus.MessageBus) (*TelegramChannel, error) {
	bot, err := telego.NewBot(cfg.Token, telego.WithDefaultLogger(false, false))
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot: %w", err)
	}

	base := NewBaseChannel("telegram", cfg, bus, cfg.AllowFrom)

	return &TelegramChannel{
		BaseChannel:  base,
		bot:          bot,
		config:       cfg,
		chatIDs:      make(map[string]int64),
		transcriber:  nil,
		placeholders: sync.Map{},
		stopThinking: sync.Map{},
		handleSem:    make(chan struct{}, telegramMaxConcurrentHandlers),
	}, nil
}

func (c *TelegramChannel) SetTranscriber(transcriber *voice.GroqTranscriber) {
	c.transcriber = transcriber
}

func (c *TelegramChannel) Start(ctx context.Context) error {
	if c.IsRunning() {
		return nil
	}
	logger.InfoC("telegram", "Starting Telegram bot (polling mode)")

	runCtx, cancel := context.WithCancel(ctx)
	c.runCancel.set(cancel)

	updates, err := c.bot.UpdatesViaLongPolling(runCtx, nil)
	if err != nil {
		return fmt.Errorf("failed to start updates polling: %w", err)
	}
	c.updates = updates

	c.setRunning(true)

	botInfo, err := c.bot.GetMe(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get bot info: %w", err)
	}
	logger.InfoCF("telegram", "Telegram bot connected", map[string]interface{}{
		"username": botInfo.Username,
	})

	go func() {
		for {
			select {
			case <-runCtx.Done():
				return
			case update, ok := <-updates:
				if !ok {
					logger.WarnC("telegram", "Updates channel closed unexpectedly, attempting to restart polling...")
					c.setRunning(false)

					select {
					case <-runCtx.Done():
						return
					case <-time.After(5 * time.Second):
					}

					newUpdates, err := c.bot.UpdatesViaLongPolling(runCtx, nil)
					if err != nil {
						logger.ErrorCF("telegram", "Failed to restart updates polling", map[string]interface{}{
							logger.FieldError: err.Error(),
						})
						continue
					}

					updates = newUpdates
					c.updates = newUpdates
					c.setRunning(true)
					logger.InfoC("telegram", "Updates polling restarted successfully")
					continue
				}
				if update.Message != nil {
					c.dispatchHandleMessage(runCtx, update.Message)
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
	logger.InfoC("telegram", "Stopping Telegram bot")
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
		logger.WarnC("telegram", "Timeout waiting for telegram message handlers to stop")
	}

	c.stopThinking.Range(func(key, value interface{}) bool {
		safeCloseSignal(value)
		c.stopThinking.Delete(key)
		return true
	})
	c.placeholders.Range(func(key, _ interface{}) bool {
		c.placeholders.Delete(key)
		return true
	})

	// In telego v1.x, the long polling is stopped by canceling the context
	// passed to UpdatesViaLongPolling. We don't need a separate Stop call
	// if we use the parent context correctly.

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
				logger.ErrorCF("telegram", "Recovered panic in telegram message handler", map[string]interface{}{
					"panic": fmt.Sprintf("%v", r),
				})
			}
		}()

		c.handleMessage(runCtx, msg)
	}(message)
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

	// Stop thinking animation
	if stop, ok := c.stopThinking.Load(msg.ChatID); ok {
		logger.DebugCF("telegram", "Telegram thinking stop signal", map[string]interface{}{
			logger.FieldChatID: msg.ChatID,
		})
		safeCloseSignal(stop)
		c.stopThinking.Delete(msg.ChatID)
	} else {
		logger.DebugCF("telegram", "Telegram thinking stop skipped (not found)", map[string]interface{}{
			logger.FieldChatID: msg.ChatID,
		})
	}

	htmlContent := sanitizeTelegramHTML(markdownToTelegramHTML(msg.Content))

	// Try to edit placeholder
	if pID, ok := c.placeholders.Load(msg.ChatID); ok {
		c.placeholders.Delete(msg.ChatID)
		logger.DebugCF("telegram", "Telegram editing thinking placeholder", map[string]interface{}{
			logger.FieldChatID: msg.ChatID,
			"message_id":       pID.(int),
		})

		_, err := c.bot.EditMessageText(ctx, &telego.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: pID.(int),
			Text:      htmlContent,
			ParseMode: telego.ModeHTML,
		})

		if err == nil {
			logger.DebugCF("telegram", "Telegram placeholder updated", map[string]interface{}{
				logger.FieldChatID: msg.ChatID,
			})
			return nil
		}
		logger.WarnCF("telegram", "Telegram placeholder update failed; fallback to new message", map[string]interface{}{
			logger.FieldChatID: msg.ChatID,
			logger.FieldError:  err.Error(),
		})
		// Fallback to new message if edit fails
	} else {
		logger.DebugCF("telegram", "Telegram placeholder not found, sending new message", map[string]interface{}{
			logger.FieldChatID: msg.ChatID,
		})
	}

	_, err = c.bot.SendMessage(ctx, telegoutil.Message(chatID, htmlContent).WithParseMode(telego.ModeHTML))

	if err != nil {
		logger.WarnCF("telegram", "HTML parse failed, fallback to plain text", map[string]interface{}{
			logger.FieldError: err.Error(),
		})
		plain := plainTextFromTelegramHTML(htmlContent)
		_, err = c.bot.SendMessage(ctx, telegoutil.Message(chatID, plain))
		if err != nil {
			logger.ErrorCF("telegram", "Telegram plain-text fallback send failed", map[string]interface{}{
				logger.FieldChatID: msg.ChatID,
				logger.FieldError:  err.Error(),
			})
		}
		return err
	}

	return nil
}

func (c *TelegramChannel) handleMessage(runCtx context.Context, message *telego.Message) {
	if message == nil {
		return
	}

	user := message.From
	if user == nil {
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
		photoPath := c.downloadFile(runCtx, photo.FileID, ".jpg")
		if photoPath != "" {
			mediaPaths = append(mediaPaths, photoPath)
			if content != "" {
				content += "\n"
			}
			content += fmt.Sprintf("[image: %s]", photoPath)
		}
	}

	if message.Voice != nil {
		voicePath := c.downloadFile(runCtx, message.Voice.FileID, ".ogg")
		if voicePath != "" {
			mediaPaths = append(mediaPaths, voicePath)

			transcribedText := ""
			if c.transcriber != nil && c.transcriber.IsAvailable() {
				ctx, cancel := context.WithTimeout(runCtx, 30*time.Second)
				defer cancel()

				result, err := c.transcriber.Transcribe(ctx, voicePath)
				if err != nil {
					logger.WarnCF("telegram", "Voice transcription failed", map[string]interface{}{
						logger.FieldError: err.Error(),
					})
					transcribedText = fmt.Sprintf("[voice: %s (transcription failed)]", voicePath)
				} else {
					transcribedText = fmt.Sprintf("[voice transcription: %s]", result.Text)
					logger.InfoCF("telegram", "Voice transcribed successfully", map[string]interface{}{
						"text_preview": truncateString(result.Text, 120),
					})
				}
			} else {
				transcribedText = fmt.Sprintf("[voice: %s]", voicePath)
			}

			if content != "" {
				content += "\n"
			}
			content += transcribedText
		}
	}

	if message.Audio != nil {
		audioPath := c.downloadFile(runCtx, message.Audio.FileID, ".mp3")
		if audioPath != "" {
			mediaPaths = append(mediaPaths, audioPath)
			if content != "" {
				content += "\n"
			}
			content += fmt.Sprintf("[audio: %s]", audioPath)
		}
	}

	if message.Document != nil {
		docPath := c.downloadFile(runCtx, message.Document.FileID, "")
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

	logger.InfoCF("telegram", "Telegram message received", map[string]interface{}{
		logger.FieldSenderID: senderID,
		logger.FieldPreview:  truncateString(content, 50),
	})

	if !c.IsAllowed(senderID) {
		logger.WarnCF("telegram", "Telegram message rejected by allowlist", map[string]interface{}{
			logger.FieldSenderID: senderID,
			logger.FieldChatID:   chatID,
		})
		return
	}

	// Thinking indicator
	apiCtx, cancelAPI := context.WithTimeout(runCtx, telegramAPICallTimeout)
	_ = c.bot.SendChatAction(apiCtx, &telego.SendChatActionParams{
		ChatID: telegoutil.ID(chatID),
		Action: telego.ChatActionTyping,
	})
	cancelAPI()

	stopChan := make(chan struct{})
	if prev, ok := c.stopThinking.LoadAndDelete(fmt.Sprintf("%d", chatID)); ok {
		// Ensure previous animation loop exits before replacing channel.
		safeCloseSignal(prev)
	}
	c.stopThinking.Store(fmt.Sprintf("%d", chatID), stopChan)
	logger.DebugCF("telegram", "Telegram thinking started", map[string]interface{}{
		logger.FieldChatID: chatID,
	})

	sendCtx, cancelSend := context.WithTimeout(runCtx, telegramAPICallTimeout)
	pMsg, err := c.bot.SendMessage(sendCtx, telegoutil.Message(telegoutil.ID(chatID), "Thinking... 💭"))
	cancelSend()
	if err == nil {
		pID := pMsg.MessageID
		c.placeholders.Store(fmt.Sprintf("%d", chatID), pID)
		logger.DebugCF("telegram", "Telegram thinking placeholder created", map[string]interface{}{
			logger.FieldChatID: chatID,
			"message_id":       pID,
		})

		go func(cid int64, mid int, stop <-chan struct{}, parentCtx context.Context) {
			dots := []string{".", "..", "..."}
			emotes := []string{"💭", "🤔", "☁️"}
			i := 0
			ticker := time.NewTicker(2000 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-parentCtx.Done():
					return
				case <-stop:
					logger.DebugCF("telegram", "Telegram thinking animation stopped", map[string]interface{}{
						logger.FieldChatID: cid,
					})
					return
				case <-ticker.C:
					i++
					text := fmt.Sprintf("Thinking%s %s", dots[i%len(dots)], emotes[i%len(emotes)])
					editCtx, cancelEdit := context.WithTimeout(parentCtx, telegramAPICallTimeout)
					_, err := c.bot.EditMessageText(editCtx, &telego.EditMessageTextParams{
						ChatID:    telegoutil.ID(cid),
						MessageID: mid,
						Text:      text,
					})
					cancelEdit()
					if err != nil {
						logger.DebugCF("telegram", "Telegram thinking animation edit failed", map[string]interface{}{
							logger.FieldChatID: cid,
							"message_id":       mid,
							logger.FieldError:  err.Error(),
						})
					}
				}
			}
		}(chatID, pID, stopChan, runCtx)
	} else {
		logger.WarnCF("telegram", "Telegram thinking placeholder create failed", map[string]interface{}{
			logger.FieldChatID: chatID,
			logger.FieldError:  err.Error(),
		})
	}

	metadata := map[string]string{
		"message_id": fmt.Sprintf("%d", message.MessageID),
		"user_id":    fmt.Sprintf("%d", user.ID),
		"username":   user.Username,
		"first_name": user.FirstName,
		"is_group":   fmt.Sprintf("%t", message.Chat.Type != telego.ChatTypePrivate),
	}

	c.HandleMessage(senderID, fmt.Sprintf("%d", chatID), content, mediaPaths, metadata)
}

func (c *TelegramChannel) downloadFile(runCtx context.Context, fileID, ext string) string {
	getFileCtx, cancelGetFile := context.WithTimeout(runCtx, telegramAPICallTimeout)
	file, err := c.bot.GetFile(getFileCtx, &telego.GetFileParams{FileID: fileID})
	cancelGetFile()
	if err != nil {
		logger.WarnCF("telegram", "Failed to get file", map[string]interface{}{
			logger.FieldError: err.Error(),
		})
		return ""
	}

	if file.FilePath == "" {
		return ""
	}

	// In telego, we can use Link() or just build the URL
	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", c.config.Token, file.FilePath)
	logger.DebugCF("telegram", "Telegram file URL resolved", map[string]interface{}{
		"url": url,
	})

	mediaDir := filepath.Join(os.TempDir(), "clawgo_media")
	if err := os.MkdirAll(mediaDir, 0755); err != nil {
		logger.WarnCF("telegram", "Failed to create media directory", map[string]interface{}{
			logger.FieldError: err.Error(),
		})
		return ""
	}

	localPath := filepath.Join(mediaDir, fileID[:min(16, len(fileID))]+ext)

	if err := c.downloadFromURL(runCtx, url, localPath); err != nil {
		logger.WarnCF("telegram", "Failed to download file", map[string]interface{}{
			logger.FieldError: err.Error(),
		})
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

func (c *TelegramChannel) downloadFromURL(runCtx context.Context, url, localPath string) error {
	downloadCtx, cancelDownload := context.WithTimeout(runCtx, telegramDownloadTimeout)
	defer cancelDownload()

	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: telegramDownloadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	out, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	logger.DebugCF("telegram", "File downloaded successfully", map[string]interface{}{
		"path": localPath,
	})
	return nil
}

func parseChatID(chatIDStr string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(chatIDStr, "%d", &id)
	return id, err
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

	text = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`).ReplaceAllString(text, "<b>$1</b>")

	text = regexp.MustCompile(`(?m)^>\s*(.*)$`).ReplaceAllString(text, "│ $1")

	text = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`).ReplaceAllString(text, `<a href="$2">$1</a>`)

	text = regexp.MustCompile(`\*\*(.+?)\*\*`).ReplaceAllString(text, "<b>$1</b>")

	text = regexp.MustCompile(`__(.+?)__`).ReplaceAllString(text, "<b>$1</b>")

	text = regexp.MustCompile(`\*([^*\n]+)\*`).ReplaceAllString(text, "<i>$1</i>")
	text = regexp.MustCompile(`_([^_\n]+)_`).ReplaceAllString(text, "<i>$1</i>")

	text = regexp.MustCompile(`~~(.+?)~~`).ReplaceAllString(text, "<s>$1</s>")

	text = regexp.MustCompile(`(?m)^[-*]\s+`).ReplaceAllString(text, "• ")
	text = regexp.MustCompile(`(?m)^\d+\.\s+`).ReplaceAllString(text, "• ")

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

	tagRe := regexp.MustCompile(`(?is)<\s*(/?)\s*([a-z0-9]+)([^>]*)>`)
	hrefRe := regexp.MustCompile(`(?is)\bhref\s*=\s*"([^"]+)"`)

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
			// Ensure tag stack remains balanced; drop unmatched close tags.
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

		// Normalize opening tags; only <a href="..."> can carry attributes.
		if tagName == "a" {
			hrefMatch := hrefRe.FindStringSubmatch(attrRaw)
			if len(hrefMatch) < 2 {
				// Invalid anchor tag -> degrade to escaped text to avoid parse errors.
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
	// Best-effort fallback for parse failures: drop tags and keep readable content.
	tagRe := regexp.MustCompile(`(?is)<[^>]+>`)
	plain := tagRe.ReplaceAllString(text, "")
	plain = strings.ReplaceAll(plain, "&lt;", "<")
	plain = strings.ReplaceAll(plain, "&gt;", ">")
	plain = strings.ReplaceAll(plain, "&quot;", "\"")
	plain = strings.ReplaceAll(plain, "&amp;", "&")
	return plain
}
