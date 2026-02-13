package bus

import (
	"clawgo/pkg/logger"
	"context"
	"sync"
	"time"
)

type MessageBus struct {
	inbound   chan InboundMessage
	outbound  chan OutboundMessage
	handlers  map[string]MessageHandler
	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once
}

const queueWriteTimeout = 2 * time.Second

func NewMessageBus() *MessageBus {
	return &MessageBus{
		inbound:  make(chan InboundMessage, 100),
		outbound: make(chan OutboundMessage, 100),
		handlers: make(map[string]MessageHandler),
	}
}

func (mb *MessageBus) PublishInbound(msg InboundMessage) {
	mb.mu.RLock()
	if mb.closed {
		mb.mu.RUnlock()
		return
	}
	ch := mb.inbound
	mb.mu.RUnlock()

	defer func() {
		if recover() != nil {
			logger.WarnCF("bus", "PublishInbound on closed channel recovered", map[string]interface{}{
				logger.FieldChannel: msg.Channel,
				logger.FieldChatID:  msg.ChatID,
				"session_key":       msg.SessionKey,
			})
		}
	}()

	select {
	case ch <- msg:
	case <-time.After(queueWriteTimeout):
		logger.ErrorCF("bus", "PublishInbound timeout (queue full)", map[string]interface{}{
			logger.FieldChannel: msg.Channel,
			logger.FieldChatID:  msg.ChatID,
			"session_key":       msg.SessionKey,
		})
	}
}

func (mb *MessageBus) ConsumeInbound(ctx context.Context) (InboundMessage, bool) {
	select {
	case msg, ok := <-mb.inbound:
		return msg, ok
	case <-ctx.Done():
		return InboundMessage{}, false
	}
}

func (mb *MessageBus) PublishOutbound(msg OutboundMessage) {
	mb.mu.RLock()
	if mb.closed {
		mb.mu.RUnlock()
		return
	}
	ch := mb.outbound
	mb.mu.RUnlock()

	defer func() {
		if recover() != nil {
			logger.WarnCF("bus", "PublishOutbound on closed channel recovered", map[string]interface{}{
				logger.FieldChannel: msg.Channel,
				logger.FieldChatID:  msg.ChatID,
			})
		}
	}()

	select {
	case ch <- msg:
	case <-time.After(queueWriteTimeout):
		logger.ErrorCF("bus", "PublishOutbound timeout (queue full)", map[string]interface{}{
			logger.FieldChannel: msg.Channel,
			logger.FieldChatID:  msg.ChatID,
		})
	}
}

func (mb *MessageBus) SubscribeOutbound(ctx context.Context) (OutboundMessage, bool) {
	select {
	case msg, ok := <-mb.outbound:
		return msg, ok
	case <-ctx.Done():
		return OutboundMessage{}, false
	}
}

func (mb *MessageBus) RegisterHandler(channel string, handler MessageHandler) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.handlers[channel] = handler
}

func (mb *MessageBus) GetHandler(channel string) (MessageHandler, bool) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	handler, ok := mb.handlers[channel]
	return handler, ok
}

func (mb *MessageBus) Close() {
	mb.closeOnce.Do(func() {
		mb.mu.Lock()
		mb.closed = true
		close(mb.inbound)
		close(mb.outbound)
		mb.mu.Unlock()
	})
}
