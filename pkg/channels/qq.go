package channels

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tencent-connect/botgo"
	"github.com/tencent-connect/botgo/dto"
	"github.com/tencent-connect/botgo/event"
	"github.com/tencent-connect/botgo/openapi"
	"github.com/tencent-connect/botgo/token"
	"golang.org/x/oauth2"

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
	"clawgo/pkg/logger"
)

type QQChannel struct {
	*BaseChannel
	config         config.QQConfig
	api            openapi.OpenAPI
	tokenSource    oauth2.TokenSource
	runCancel      cancelGuard
	sessionManager botgo.SessionManager
	processedIDs   map[string]bool
	mu             sync.RWMutex
}

func NewQQChannel(cfg config.QQConfig, messageBus *bus.MessageBus) (*QQChannel, error) {
	base := NewBaseChannel("qq", cfg, messageBus, cfg.AllowFrom)

	return &QQChannel{
		BaseChannel:  base,
		config:       cfg,
		processedIDs: make(map[string]bool),
	}, nil
}

func (c *QQChannel) Start(ctx context.Context) error {
	if c.IsRunning() {
		return nil
	}
	if c.config.AppID == "" || c.config.AppSecret == "" {
		return fmt.Errorf("QQ app_id and app_secret not configured")
	}

	logger.InfoC("qq", "Starting QQ bot (WebSocket mode)")

	// Create token source
	credentials := &token.QQBotCredentials{
		AppID:     c.config.AppID,
		AppSecret: c.config.AppSecret,
	}
	c.tokenSource = token.NewQQBotTokenSource(credentials)

	// Create child context
	runCtx, cancel := context.WithCancel(ctx)
	c.runCancel.set(cancel)

	// Start token auto-refresh goroutine
	if err := token.StartRefreshAccessToken(runCtx, c.tokenSource); err != nil {
		return fmt.Errorf("failed to start token refresh: %w", err)
	}

	// Initialize OpenAPI client
	c.api = botgo.NewOpenAPI(c.config.AppID, c.tokenSource).WithTimeout(5 * time.Second)

	// Register event handlers
	intent := event.RegisterHandlers(
		c.handleC2CMessage(),
		c.handleGroupATMessage(),
	)

	// Get WebSocket endpoint
	wsInfo, err := c.api.WS(runCtx, nil, "")
	if err != nil {
		return fmt.Errorf("failed to get websocket info: %w", err)
	}

	logger.InfoCF("qq", "Got WebSocket info", map[string]interface{}{
		"shards": wsInfo.Shards,
	})

	// Create and store session manager
	c.sessionManager = botgo.NewSessionManager()

	// Start WebSocket connection in a goroutine to avoid blocking
	runChannelTask("qq", "websocket session", func() error {
		return c.sessionManager.Start(wsInfo, c.tokenSource, &intent)
	}, func(_ error) {
		c.setRunning(false)
	})

	c.setRunning(true)
	logger.InfoC("qq", "QQ bot started successfully")

	return nil
}

func (c *QQChannel) Stop(ctx context.Context) error {
	if !c.IsRunning() {
		return nil
	}
	logger.InfoC("qq", "Stopping QQ bot")
	c.setRunning(false)

	c.runCancel.cancelAndClear()
	return nil
}

func (c *QQChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("QQ bot not running")
	}

	// Build message
	msgToCreate := &dto.MessageToCreate{
		Content: msg.Content,
	}

	// Send C2C message
	_, err := c.api.PostC2CMessage(ctx, msg.ChatID, msgToCreate)
	if err != nil {
		logger.ErrorCF("qq", "Failed to send C2C message", map[string]interface{}{
			logger.FieldError: err.Error(),
		})
		return err
	}

	return nil
}

// handleC2CMessage handles QQ private messages
func (c *QQChannel) handleC2CMessage() event.C2CMessageEventHandler {
	return func(event *dto.WSPayload, data *dto.WSC2CMessageData) error {
		// Deduplication check
		if c.isDuplicate(data.ID) {
			return nil
		}

		// Extract sender information
		var senderID string
		if data.Author != nil && data.Author.ID != "" {
			senderID = data.Author.ID
		} else {
			logger.WarnC("qq", "Received message with no sender ID")
			return nil
		}

		// Extract message content
		content := data.Content
		if content == "" {
			logger.DebugC("qq", "Received empty message, ignoring")
			return nil
		}

		logger.InfoCF("qq", "Received C2C message", map[string]interface{}{
			logger.FieldSenderID:             senderID,
			logger.FieldChatID:               senderID,
			logger.FieldMessageContentLength: len(content),
		})

		// Forward to message bus
		metadata := map[string]string{
			"message_id": data.ID,
		}

		c.HandleMessage(senderID, senderID, content, []string{}, metadata)

		return nil
	}
}

// handleGroupATMessage handles group @ messages
func (c *QQChannel) handleGroupATMessage() event.GroupATMessageEventHandler {
	return func(event *dto.WSPayload, data *dto.WSGroupATMessageData) error {
		// Deduplication check
		if c.isDuplicate(data.ID) {
			return nil
		}

		// Extract sender information
		var senderID string
		if data.Author != nil && data.Author.ID != "" {
			senderID = data.Author.ID
		} else {
			logger.WarnC("qq", "Received group message with no sender ID")
			return nil
		}

		// Extract message content (remove bot mention)
		content := data.Content
		if content == "" {
			logger.DebugC("qq", "Received empty group message, ignoring")
			return nil
		}

		logger.InfoCF("qq", "Received group AT message", map[string]interface{}{
			logger.FieldSenderID:             senderID,
			"group_id":                       data.GroupID,
			logger.FieldMessageContentLength: len(content),
		})

		// Forward to message bus (use GroupID as ChatID)
		metadata := map[string]string{
			"message_id": data.ID,
			"group_id":   data.GroupID,
		}

		c.HandleMessage(senderID, data.GroupID, content, []string{}, metadata)

		return nil
	}
}

// isDuplicate checks whether a message is duplicated
func (c *QQChannel) isDuplicate(messageID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.processedIDs[messageID] {
		return true
	}

	c.processedIDs[messageID] = true

	// Simple cleanup: limit map size
	if len(c.processedIDs) > 10000 {
		// Remove half of the entries
		count := 0
		for id := range c.processedIDs {
			if count >= 5000 {
				break
			}
			delete(c.processedIDs, id)
			count++
		}
	}

	return false
}
