//go:build !omit_maixcam

package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/logger"
)

type MaixCamChannel struct {
	*BaseChannel
	config     config.MaixCamConfig
	listener   net.Listener
	clients    map[net.Conn]bool
	clientsMux sync.RWMutex
}

const maixcamCompiled = true

type MaixCamMessage struct {
	Type      string                 `json:"type"`
	Tips      string                 `json:"tips"`
	Timestamp float64                `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

func NewMaixCamChannel(cfg config.MaixCamConfig, bus *bus.MessageBus) (*MaixCamChannel, error) {
	base := NewBaseChannel("maixcam", cfg, bus, cfg.AllowFrom)

	return &MaixCamChannel{
		BaseChannel: base,
		config:      cfg,
		clients:     make(map[net.Conn]bool),
	}, nil
}

func (c *MaixCamChannel) Start(ctx context.Context) error {
	logger.InfoC("maixcam", logger.C0079)

	addr := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	c.listener = listener
	c.setRunning(true)

	logger.InfoCF("maixcam", logger.C0080, map[string]interface{}{
		"host": c.config.Host,
		"port": c.config.Port,
	})

	go c.acceptConnections(ctx)

	return nil
}

func (c *MaixCamChannel) acceptConnections(ctx context.Context) {
	logger.DebugC("maixcam", logger.C0081)

	for {
		select {
		case <-ctx.Done():
			logger.InfoC("maixcam", logger.C0082)
			return
		default:
			conn, err := c.listener.Accept()
			if err != nil {
				if c.IsRunning() {
					logger.ErrorCF("maixcam", logger.C0083, map[string]interface{}{
						logger.FieldError: err.Error(),
					})
				}
				return
			}

			logger.InfoCF("maixcam", logger.C0084, map[string]interface{}{
				"remote_addr": conn.RemoteAddr().String(),
			})

			c.clientsMux.Lock()
			c.clients[conn] = true
			c.clientsMux.Unlock()

			go c.handleConnection(conn, ctx)
		}
	}
}

func (c *MaixCamChannel) handleConnection(conn net.Conn, ctx context.Context) {
	logger.DebugC("maixcam", logger.C0085)

	defer func() {
		conn.Close()
		c.clientsMux.Lock()
		delete(c.clients, conn)
		c.clientsMux.Unlock()
		logger.DebugC("maixcam", logger.C0086)
	}()

	decoder := json.NewDecoder(conn)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			var msg MaixCamMessage
			if err := decoder.Decode(&msg); err != nil {
				if err.Error() != "EOF" {
					logger.ErrorCF("maixcam", logger.C0087, map[string]interface{}{
						logger.FieldError: err.Error(),
					})
				}
				return
			}

			c.processMessage(msg, conn)
		}
	}
}

func (c *MaixCamChannel) processMessage(msg MaixCamMessage, conn net.Conn) {
	switch msg.Type {
	case "person_detected":
		c.handlePersonDetection(msg)
	case "heartbeat":
		logger.DebugC("maixcam", logger.C0088)
	case "status":
		c.handleStatusUpdate(msg)
	default:
		logger.WarnCF("maixcam", logger.C0089, map[string]interface{}{
			"message_type": msg.Type,
		})
	}
}

func (c *MaixCamChannel) handlePersonDetection(msg MaixCamMessage) {
	logger.InfoCF("maixcam", logger.C0090, map[string]interface{}{
		logger.FieldSenderID: "maixcam",
		logger.FieldChatID:   "default",
		"timestamp":          msg.Timestamp,
		"data":               msg.Data,
	})

	senderID := "maixcam"
	chatID := "default"

	classInfo, ok := msg.Data["class_name"].(string)
	if !ok {
		classInfo = "person"
	}

	score, _ := msg.Data["score"].(float64)
	x, _ := msg.Data["x"].(float64)
	y, _ := msg.Data["y"].(float64)
	w, _ := msg.Data["w"].(float64)
	h, _ := msg.Data["h"].(float64)

	content := fmt.Sprintf("馃摲 Person detected!\nClass: %s\nConfidence: %.2f%%\nPosition: (%.0f, %.0f)\nSize: %.0fx%.0f",
		classInfo, score*100, x, y, w, h)

	metadata := map[string]string{
		"timestamp": fmt.Sprintf("%.0f", msg.Timestamp),
		"class_id":  fmt.Sprintf("%.0f", msg.Data["class_id"]),
		"score":     fmt.Sprintf("%.2f", score),
		"x":         fmt.Sprintf("%.0f", x),
		"y":         fmt.Sprintf("%.0f", y),
		"w":         fmt.Sprintf("%.0f", w),
		"h":         fmt.Sprintf("%.0f", h),
	}

	c.HandleMessage(senderID, chatID, content, []string{}, metadata)
}

func (c *MaixCamChannel) handleStatusUpdate(msg MaixCamMessage) {
	logger.InfoCF("maixcam", logger.C0091, map[string]interface{}{
		"status": msg.Data,
	})
}

func (c *MaixCamChannel) Stop(ctx context.Context) error {
	logger.InfoC("maixcam", logger.C0092)
	c.setRunning(false)

	if c.listener != nil {
		c.listener.Close()
	}

	c.clientsMux.Lock()
	defer c.clientsMux.Unlock()

	for conn := range c.clients {
		conn.Close()
	}
	c.clients = make(map[net.Conn]bool)

	logger.InfoC("maixcam", logger.C0093)
	return nil
}

func (c *MaixCamChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("maixcam channel not running")
	}

	c.clientsMux.RLock()
	defer c.clientsMux.RUnlock()

	if len(c.clients) == 0 {
		logger.WarnC("maixcam", logger.C0094)
		return fmt.Errorf("no connected MaixCam devices")
	}

	response := map[string]interface{}{
		"type":             "command",
		"timestamp":        float64(0),
		"message":          msg.Content,
		logger.FieldChatID: msg.ChatID,
	}

	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	var sendErr error
	for conn := range c.clients {
		if _, err := conn.Write(data); err != nil {
			logger.ErrorCF("maixcam", logger.C0095, map[string]interface{}{
				"client":          conn.RemoteAddr().String(),
				logger.FieldError: err.Error(),
			})
			sendErr = err
		}
	}

	return sendErr
}
