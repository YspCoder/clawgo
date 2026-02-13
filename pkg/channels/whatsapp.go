package channels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
	"clawgo/pkg/logger"
)

type WhatsAppChannel struct {
	*BaseChannel
	conn      *websocket.Conn
	config    config.WhatsAppConfig
	url       string
	runCancel cancelGuard
	mu        sync.Mutex
	connected bool
}

func NewWhatsAppChannel(cfg config.WhatsAppConfig, bus *bus.MessageBus) (*WhatsAppChannel, error) {
	base := NewBaseChannel("whatsapp", cfg, bus, cfg.AllowFrom)

	return &WhatsAppChannel{
		BaseChannel: base,
		config:      cfg,
		url:         cfg.BridgeURL,
		connected:   false,
	}, nil
}

func (c *WhatsAppChannel) Start(ctx context.Context) error {
	if c.IsRunning() {
		return nil
	}
	logger.InfoCF("whatsapp", "Starting WhatsApp channel", map[string]interface{}{
		"url": c.url,
	})
	runCtx, cancel := context.WithCancel(ctx)
	c.runCancel.set(cancel)

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	conn, _, err := dialer.Dial(c.url, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to WhatsApp bridge: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.mu.Unlock()

	c.setRunning(true)
	logger.InfoC("whatsapp", "WhatsApp channel connected")

	go c.listen(runCtx)

	return nil
}

func (c *WhatsAppChannel) Stop(ctx context.Context) error {
	if !c.IsRunning() {
		return nil
	}
	logger.InfoC("whatsapp", "Stopping WhatsApp channel")
	c.runCancel.cancelAndClear()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			logger.WarnCF("whatsapp", "Error closing WhatsApp connection", map[string]interface{}{
				logger.FieldError: err.Error(),
			})
		}
		c.conn = nil
	}

	c.connected = false
	c.setRunning(false)

	return nil
}

func (c *WhatsAppChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("whatsapp connection not established")
	}

	payload := map[string]interface{}{
		"type":    "message",
		"to":      msg.ChatID,
		"content": msg.Content,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

func (c *WhatsAppChannel) listen(ctx context.Context) {
	backoff := 200 * time.Millisecond
	const maxBackoff = 3 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()

			if conn == nil {
				if !sleepWithContext(ctx, backoff) {
					return
				}
				backoff = nextBackoff(backoff, maxBackoff)
				continue
			}

			_, message, err := conn.ReadMessage()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) || errors.Is(err, net.ErrClosed) {
					logger.InfoCF("whatsapp", "WhatsApp connection closed", map[string]interface{}{
						logger.FieldError: err.Error(),
					})
					return
				}
				logger.WarnCF("whatsapp", "WhatsApp read error", map[string]interface{}{
					logger.FieldError: err.Error(),
				})
				if !sleepWithContext(ctx, backoff) {
					return
				}
				backoff = nextBackoff(backoff, maxBackoff)
				continue
			}
			backoff = 200 * time.Millisecond

			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err != nil {
				logger.WarnCF("whatsapp", "Failed to unmarshal WhatsApp message", map[string]interface{}{
					logger.FieldError: err.Error(),
				})
				continue
			}

			msgType, ok := msg["type"].(string)
			if !ok {
				continue
			}

			if msgType == "message" {
				c.handleIncomingMessage(msg)
			}
		}
	}
}

func (c *WhatsAppChannel) handleIncomingMessage(msg map[string]interface{}) {
	senderID, ok := msg["from"].(string)
	if !ok {
		return
	}

	chatID, ok := msg["chat"].(string)
	if !ok {
		chatID = senderID
	}

	content, ok := msg["content"].(string)
	if !ok {
		content = ""
	}

	var mediaPaths []string
	if mediaData, ok := msg["media"].([]interface{}); ok {
		mediaPaths = make([]string, 0, len(mediaData))
		for _, m := range mediaData {
			if path, ok := m.(string); ok {
				mediaPaths = append(mediaPaths, path)
			}
		}
	}

	metadata := make(map[string]string)
	if messageID, ok := msg["id"].(string); ok {
		metadata["message_id"] = messageID
	}
	if userName, ok := msg["from_name"].(string); ok {
		metadata["user_name"] = userName
	}

	logger.InfoCF("whatsapp", "WhatsApp message received", map[string]interface{}{
		logger.FieldSenderID: senderID,
		logger.FieldPreview:  truncateString(content, 50),
	})

	c.HandleMessage(senderID, chatID, content, mediaPaths, metadata)
}

func nextBackoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		return max
	}
	return next
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
