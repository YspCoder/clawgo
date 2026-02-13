package channels

import (
	"context"
	"fmt"
	"io"
	"log"
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
	"clawgo/pkg/voice"
)

type TelegramChannel struct {
	*BaseChannel
	bot          *telego.Bot
	config       config.TelegramConfig
	chatIDs      map[string]int64
	updates      <-chan telego.Update
	transcriber  *voice.GroqTranscriber
	placeholders sync.Map // chatID -> messageID
	stopThinking sync.Map // chatID -> chan struct{}
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
	}, nil
}

func (c *TelegramChannel) SetTranscriber(transcriber *voice.GroqTranscriber) {
	c.transcriber = transcriber
}

func (c *TelegramChannel) Start(ctx context.Context) error {
	log.Printf("Starting Telegram bot (polling mode)...")

	updates, err := c.bot.UpdatesViaLongPolling(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start updates polling: %w", err)
	}
	c.updates = updates

	c.setRunning(true)

	botInfo, err := c.bot.GetMe(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get bot info: %w", err)
	}
	log.Printf("Telegram bot @%s connected", botInfo.Username)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case update, ok := <-updates:
				if !ok {
					log.Printf("Updates channel closed")
					return
				}
				if update.Message != nil {
					c.handleMessage(update.Message)
				}
			}
		}
	}()

	return nil
}

func (c *TelegramChannel) Stop(ctx context.Context) error {
	log.Println("Stopping Telegram bot...")
	c.setRunning(false)

	// In telego v1.x, the long polling is stopped by canceling the context
	// passed to UpdatesViaLongPolling. We don't need a separate Stop call
	// if we use the parent context correctly.

	return nil
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
		log.Printf("Telegram thinking stop signal: chat_id=%s", msg.ChatID)
		close(stop.(chan struct{}))
		c.stopThinking.Delete(msg.ChatID)
	} else {
		log.Printf("Telegram thinking stop skipped: no stop channel found for chat_id=%s", msg.ChatID)
	}

	htmlContent := sanitizeTelegramHTML(markdownToTelegramHTML(msg.Content))

	// Try to edit placeholder
	if pID, ok := c.placeholders.Load(msg.ChatID); ok {
		c.placeholders.Delete(msg.ChatID)
		log.Printf("Telegram editing thinking placeholder: chat_id=%s message_id=%d", msg.ChatID, pID.(int))

		_, err := c.bot.EditMessageText(ctx, &telego.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: pID.(int),
			Text:      htmlContent,
			ParseMode: telego.ModeHTML,
		})

		if err == nil {
			log.Printf("Telegram placeholder updated successfully: chat_id=%s", msg.ChatID)
			return nil
		}
		log.Printf("Telegram placeholder update failed, fallback to new message: chat_id=%s err=%v", msg.ChatID, err)
		// Fallback to new message if edit fails
	} else {
		log.Printf("Telegram placeholder not found, sending new message: chat_id=%s", msg.ChatID)
	}

	_, err = c.bot.SendMessage(ctx, telegoutil.Message(chatID, htmlContent).WithParseMode(telego.ModeHTML))

	if err != nil {
		log.Printf("HTML parse failed, falling back to plain text: %v", err)
		plain := plainTextFromTelegramHTML(htmlContent)
		_, err = c.bot.SendMessage(ctx, telegoutil.Message(chatID, plain))
		if err != nil {
			log.Printf("Telegram plain-text fallback send failed: chat_id=%s err=%v", msg.ChatID, err)
		}
		return err
	}

	return nil
}

func (c *TelegramChannel) handleMessage(message *telego.Message) {
	if message == nil {
		return
	}

	user := message.From
	if user == nil {
		return
	}

	senderID := fmt.Sprintf("%d", user.ID)

	chatID := message.Chat.ID
	c.chatIDs[senderID] = chatID

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
		photoPath := c.downloadFile(photo.FileID, ".jpg")
		if photoPath != "" {
			mediaPaths = append(mediaPaths, photoPath)
			if content != "" {
				content += "\n"
			}
			content += fmt.Sprintf("[image: %s]", photoPath)
		}
	}

	if message.Voice != nil {
		voicePath := c.downloadFile(message.Voice.FileID, ".ogg")
		if voicePath != "" {
			mediaPaths = append(mediaPaths, voicePath)

			transcribedText := ""
			if c.transcriber != nil && c.transcriber.IsAvailable() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				result, err := c.transcriber.Transcribe(ctx, voicePath)
				if err != nil {
					log.Printf("Voice transcription failed: %v", err)
					transcribedText = fmt.Sprintf("[voice: %s (transcription failed)]", voicePath)
				} else {
					transcribedText = fmt.Sprintf("[voice transcription: %s]", result.Text)
					log.Printf("Voice transcribed successfully: %s", result.Text)
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
		audioPath := c.downloadFile(message.Audio.FileID, ".mp3")
		if audioPath != "" {
			mediaPaths = append(mediaPaths, audioPath)
			if content != "" {
				content += "\n"
			}
			content += fmt.Sprintf("[audio: %s]", audioPath)
		}
	}

	if message.Document != nil {
		docPath := c.downloadFile(message.Document.FileID, "")
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

	log.Printf("Telegram message from %s: %s...", senderID, truncateString(content, 50))

	if !c.IsAllowed(senderID) {
		log.Printf("Telegram message rejected by allowlist: sender=%s chat=%d", senderID, chatID)
		return
	}

	// Thinking indicator
	_ = c.bot.SendChatAction(context.Background(), &telego.SendChatActionParams{
		ChatID: telegoutil.ID(chatID),
		Action: telego.ChatActionTyping,
	})

	stopChan := make(chan struct{})
	c.stopThinking.Store(fmt.Sprintf("%d", chatID), stopChan)
	log.Printf("Telegram thinking started: chat_id=%d", chatID)

	pMsg, err := c.bot.SendMessage(context.Background(), telegoutil.Message(telegoutil.ID(chatID), "Thinking... 💭"))
	if err == nil {
		pID := pMsg.MessageID
		c.placeholders.Store(fmt.Sprintf("%d", chatID), pID)
		log.Printf("Telegram thinking placeholder created: chat_id=%d message_id=%d", chatID, pID)

		go func(cid int64, mid int, stop <-chan struct{}) {
			dots := []string{".", "..", "..."}
			emotes := []string{"💭", "🤔", "☁️"}
			i := 0
			ticker := time.NewTicker(2000 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					log.Printf("Telegram thinking animation stopped: chat_id=%d", cid)
					return
				case <-ticker.C:
					i++
					text := fmt.Sprintf("Thinking%s %s", dots[i%len(dots)], emotes[i%len(emotes)])
					if _, err := c.bot.EditMessageText(context.Background(), &telego.EditMessageTextParams{
						ChatID:    telegoutil.ID(cid),
						MessageID: mid,
						Text:      text,
					}); err != nil {
						log.Printf("Telegram thinking animation edit failed: chat_id=%d message_id=%d err=%v", cid, mid, err)
					}
				}
			}
		}(chatID, pID, stopChan)
	} else {
		log.Printf("Telegram thinking placeholder create failed: chat_id=%d err=%v", chatID, err)
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

func (c *TelegramChannel) downloadFile(fileID, ext string) string {
	file, err := c.bot.GetFile(context.Background(), &telego.GetFileParams{FileID: fileID})
	if err != nil {
		log.Printf("Failed to get file: %v", err)
		return ""
	}

	if file.FilePath == "" {
		return ""
	}

	// In telego, we can use Link() or just build the URL
	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", c.config.Token, file.FilePath)
	log.Printf("File URL: %s", url)

	mediaDir := filepath.Join(os.TempDir(), "clawgo_media")
	if err := os.MkdirAll(mediaDir, 0755); err != nil {
		log.Printf("Failed to create media directory: %v", err)
		return ""
	}

	localPath := filepath.Join(mediaDir, fileID[:min(16, len(fileID))]+ext)

	if err := c.downloadFromURL(url, localPath); err != nil {
		log.Printf("Failed to download file: %v", err)
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

func (c *TelegramChannel) downloadFromURL(url, localPath string) error {
	resp, err := http.Get(url)
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

	log.Printf("File downloaded successfully to: %s", localPath)
	return nil
}

func parseChatID(chatIDStr string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(chatIDStr, "%d", &id)
	return id, err
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
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
