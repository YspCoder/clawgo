package channels

import (
	"context"
	"fmt"
	"hash/fnv"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"clawgo/pkg/bus"
	"clawgo/pkg/logger"
)

type Channel interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Send(ctx context.Context, msg bus.OutboundMessage) error
	IsRunning() bool
	IsAllowed(senderID string) bool
	HealthCheck(ctx context.Context) error
}

// ActionCapable is an optional capability interface for channels that support non-send message actions.
type ActionCapable interface {
	SupportsAction(action string) bool
}

type BaseChannel struct {
	config    interface{}
	bus       *bus.MessageBus
	running   atomic.Bool
	name      string
	allowList []string
	recentMsgMu sync.Mutex
	recentMsg   map[string]time.Time
}

func NewBaseChannel(name string, config interface{}, bus *bus.MessageBus, allowList []string) *BaseChannel {
	return &BaseChannel{
		config:    config,
		bus:       bus,
		name:      name,
		allowList: allowList,
		recentMsg: map[string]time.Time{},
	}
}

func (c *BaseChannel) Name() string {
	return c.name
}

func (c *BaseChannel) IsRunning() bool {
	return c.running.Load()
}

func (c *BaseChannel) HealthCheck(ctx context.Context) error {
	if !c.IsRunning() {
		return fmt.Errorf("%s channel not running", c.name)
	}
	return nil
}

func (c *BaseChannel) IsAllowed(senderID string) bool {
	if len(c.allowList) == 0 {
		return true
	}

	// Normalize sender id for channels that include display suffix, e.g. "12345|alice".
	rawSenderID := senderID
	if idx := strings.Index(senderID, "|"); idx > 0 {
		rawSenderID = senderID[:idx]
	}

	for _, allowed := range c.allowList {
		if senderID == allowed || rawSenderID == allowed {
			return true
		}
	}

	return false
}

func (c *BaseChannel) seenRecently(key string, ttl time.Duration) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	now := time.Now()
	c.recentMsgMu.Lock()
	defer c.recentMsgMu.Unlock()
	for id, ts := range c.recentMsg {
		if now.Sub(ts) > 10*time.Minute {
			delete(c.recentMsg, id)
		}
	}
	if ts, ok := c.recentMsg[key]; ok {
		if now.Sub(ts) <= ttl {
			return true
		}
	}
	c.recentMsg[key] = now
	return false
}

func messageDigest(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%08x", h.Sum32())
}

func (c *BaseChannel) HandleMessage(senderID, chatID, content string, media []string, metadata map[string]string) {
	if !c.IsAllowed(senderID) {
		logger.WarnCF("channels", "Message rejected by allowlist", map[string]interface{}{
			logger.FieldChannel:  c.name,
			logger.FieldSenderID: senderID,
			logger.FieldChatID:   chatID,
		})
		return
	}

	if metadata != nil {
		if messageID := strings.TrimSpace(metadata["message_id"]); messageID != "" {
			if c.seenRecently(c.name+":"+messageID, 10*time.Minute) {
				logger.WarnCF("channels", "Duplicate inbound message skipped", map[string]interface{}{
					logger.FieldChannel: c.name,
					"message_id":      messageID,
					logger.FieldChatID: chatID,
				})
				return
			}
		}
	}
	// Fallback dedupe when platform omits/changes message_id (short window, same sender/chat/content).
	contentKey := c.name + ":content:" + chatID + ":" + senderID + ":" + messageDigest(content)
	if c.seenRecently(contentKey, 12*time.Second) {
		logger.WarnCF("channels", "Duplicate inbound content skipped", map[string]interface{}{
			logger.FieldChannel: c.name,
			logger.FieldChatID:  chatID,
		})
		return
	}

	// Build session key: channel:chatID
	sessionKey := fmt.Sprintf("%s:%s", c.name, chatID)

	msg := bus.InboundMessage{
		Channel:    c.name,
		SenderID:   senderID,
		ChatID:     chatID,
		Content:    content,
		Media:      media,
		MediaItems: toMediaItems(c.name, media),
		Metadata:   metadata,
		SessionKey: sessionKey,
	}

	c.bus.PublishInbound(msg)
}

func toMediaItems(channel string, media []string) []bus.MediaItem {
	if len(media) == 0 {
		return nil
	}
	out := make([]bus.MediaItem, 0, len(media))
	for _, m := range media {
		item := bus.MediaItem{Channel: channel, Ref: m, Source: "raw", Type: "unknown"}
		switch {
		case strings.HasPrefix(m, "feishu:image:"):
			item.Source = "feishu"
			item.Type = "image"
		case strings.HasPrefix(m, "feishu:file:"):
			item.Source = "feishu"
			item.Type = "file"
		case strings.HasPrefix(m, "telegram:"):
			item.Source = "telegram"
			item.Type = "remote"
		case strings.HasPrefix(m, "http://") || strings.HasPrefix(m, "https://"):
			item.Source = "url"
			item.Type = "remote"
		default:
			ext := strings.ToLower(filepath.Ext(m))
			item.Path = m
			switch ext {
			case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
				item.Type = "image"
			case ".mp4", ".mov", ".webm", ".avi":
				item.Type = "video"
			case ".mp3", ".wav", ".ogg", ".m4a":
				item.Type = "audio"
			default:
				item.Type = "file"
			}
			item.Source = "local"
		}
		out = append(out, item)
	}
	return out
}

func (c *BaseChannel) setRunning(running bool) {
	c.running.Store(running)
}
