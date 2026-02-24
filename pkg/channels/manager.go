// ClawGo - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 ClawGo contributors

package channels

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
	"clawgo/pkg/logger"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
)

type Manager struct {
	channels      map[string]Channel
	bus           *bus.MessageBus
	config        *config.Config
	dispatchTask  *asyncTask
	dispatchSem   chan struct{}
	outboundLimit *rate.Limiter
	mu            sync.RWMutex
}

type asyncTask struct {
	cancel context.CancelFunc
}

func NewManager(cfg *config.Config, messageBus *bus.MessageBus) (*Manager, error) {
	m := &Manager{
		channels: make(map[string]Channel),
		bus:      messageBus,
		config:   cfg,
		// Limit concurrent outbound sends to avoid unbounded goroutine growth.
		dispatchSem:   make(chan struct{}, 32),
		outboundLimit: rate.NewLimiter(rate.Limit(40), 80),
	}

	if err := m.initChannels(); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Manager) initChannels() error {
	logger.InfoC("channels", "Initializing channel manager")

	if m.config.Channels.Telegram.Enabled {
		logger.DebugCF("channels", "Attempting to initialize Telegram channel", map[string]interface{}{
			"has_token": m.config.Channels.Telegram.Token != "",
		})
		if m.config.Channels.Telegram.Token == "" {
			logger.WarnC("channels", "Telegram token is empty, skipping")
		} else {
			telegram, err := NewTelegramChannel(m.config.Channels.Telegram, m.bus)
			if err != nil {
				logger.ErrorCF("channels", "Failed to initialize Telegram channel", map[string]interface{}{
					logger.FieldError: err.Error(),
				})
			} else {
				m.channels["telegram"] = telegram
				logger.InfoC("channels", "Telegram channel enabled successfully")
			}
		}
	}

	if m.config.Channels.WhatsApp.Enabled {
		if m.config.Channels.WhatsApp.BridgeURL == "" {
			logger.WarnC("channels", "WhatsApp bridge URL is empty, skipping")
		} else {
			whatsapp, err := NewWhatsAppChannel(m.config.Channels.WhatsApp, m.bus)
			if err != nil {
				logger.ErrorCF("channels", "Failed to initialize WhatsApp channel", map[string]interface{}{
					logger.FieldError: err.Error(),
				})
			} else {
				m.channels["whatsapp"] = whatsapp
				logger.InfoC("channels", "WhatsApp channel enabled successfully")
			}
		}
	}

	if m.config.Channels.Feishu.Enabled {
		feishu, err := NewFeishuChannel(m.config.Channels.Feishu, m.bus)
		if err != nil {
			logger.ErrorCF("channels", "Failed to initialize Feishu channel", map[string]interface{}{
				logger.FieldError: err.Error(),
			})
		} else {
			m.channels["feishu"] = feishu
			logger.InfoC("channels", "Feishu channel enabled successfully")
		}
	}

	if m.config.Channels.Discord.Enabled {
		if m.config.Channels.Discord.Token == "" {
			logger.WarnC("channels", "Discord token is empty, skipping")
		} else {
			discord, err := NewDiscordChannel(m.config.Channels.Discord, m.bus)
			if err != nil {
				logger.ErrorCF("channels", "Failed to initialize Discord channel", map[string]interface{}{
					logger.FieldError: err.Error(),
				})
			} else {
				m.channels["discord"] = discord
				logger.InfoC("channels", "Discord channel enabled successfully")
			}
		}
	}

	if m.config.Channels.MaixCam.Enabled {
		maixcam, err := NewMaixCamChannel(m.config.Channels.MaixCam, m.bus)
		if err != nil {
			logger.ErrorCF("channels", "Failed to initialize MaixCam channel", map[string]interface{}{
				logger.FieldError: err.Error(),
			})
		} else {
			m.channels["maixcam"] = maixcam
			logger.InfoC("channels", "MaixCam channel enabled successfully")
		}
	}

	if m.config.Channels.QQ.Enabled {
		qq, err := NewQQChannel(m.config.Channels.QQ, m.bus)
		if err != nil {
			logger.ErrorCF("channels", "Failed to initialize QQ channel", map[string]interface{}{
				logger.FieldError: err.Error(),
			})
		} else {
			m.channels["qq"] = qq
			logger.InfoC("channels", "QQ channel enabled successfully")
		}
	}

	if m.config.Channels.DingTalk.Enabled {
		if m.config.Channels.DingTalk.ClientID == "" {
			logger.WarnC("channels", "DingTalk Client ID is empty, skipping")
		} else {
			dingtalk, err := NewDingTalkChannel(m.config.Channels.DingTalk, m.bus)
			if err != nil {
				logger.ErrorCF("channels", "Failed to initialize DingTalk channel", map[string]interface{}{
					logger.FieldError: err.Error(),
				})
			} else {
				m.channels["dingtalk"] = dingtalk
				logger.InfoC("channels", "DingTalk channel enabled successfully")
			}
		}
	}

	logger.InfoCF("channels", "Channel initialization completed", map[string]interface{}{
		"enabled_channels": len(m.channels),
	})

	return nil
}

func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	if len(m.channels) == 0 {
		m.mu.Unlock()
		logger.WarnC("channels", "No channels enabled")
		return nil
	}
	channelsSnapshot := make(map[string]Channel, len(m.channels))
	for k, v := range m.channels {
		channelsSnapshot[k] = v
	}
	dispatchCtx, cancel := context.WithCancel(ctx)
	m.dispatchTask = &asyncTask{cancel: cancel}
	m.mu.Unlock()

	logger.InfoC("channels", "Starting all channels")
	go m.dispatchOutbound(dispatchCtx)

	g, gctx := errgroup.WithContext(ctx)
	for name, channel := range channelsSnapshot {
		name := name
		channel := channel
		g.Go(func() error {
			logger.InfoCF("channels", "Starting channel", map[string]interface{}{logger.FieldChannel: name})
			if err := channel.Start(gctx); err != nil {
				logger.ErrorCF("channels", "Failed to start channel", map[string]interface{}{logger.FieldChannel: name, logger.FieldError: err.Error()})
				return fmt.Errorf("%s: %w", name, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	logger.InfoC("channels", "All channels started")
	return nil
}

func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	channelsSnapshot := make(map[string]Channel, len(m.channels))
	for k, v := range m.channels {
		channelsSnapshot[k] = v
	}
	task := m.dispatchTask
	m.dispatchTask = nil
	m.mu.Unlock()

	logger.InfoC("channels", "Stopping all channels")
	if task != nil {
		task.cancel()
	}

	g, gctx := errgroup.WithContext(ctx)
	for name, channel := range channelsSnapshot {
		name := name
		channel := channel
		g.Go(func() error {
			logger.InfoCF("channels", "Stopping channel", map[string]interface{}{logger.FieldChannel: name})
			if err := channel.Stop(gctx); err != nil {
				logger.ErrorCF("channels", "Error stopping channel", map[string]interface{}{logger.FieldChannel: name, logger.FieldError: err.Error()})
				return fmt.Errorf("%s: %w", name, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	logger.InfoC("channels", "All channels stopped")
	return nil
}

func (m *Manager) CheckHealth(ctx context.Context) map[string]error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make(map[string]error)
	for name, channel := range m.channels {
		results[name] = channel.HealthCheck(ctx)
	}
	return results
}

func (m *Manager) RestartChannel(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	channel, ok := m.channels[name]
	if !ok {
		return fmt.Errorf("channel %s not found", name)
	}

	logger.InfoCF("channels", "Restarting channel", map[string]interface{}{"channel": name})
	_ = channel.Stop(ctx)
	return channel.Start(ctx)
}

func (m *Manager) dispatchOutbound(ctx context.Context) {
	logger.InfoC("channels", "Outbound dispatcher started")

	for {
		select {
		case <-ctx.Done():
			logger.InfoC("channels", "Outbound dispatcher stopped")
			return
		default:
			msg, ok := m.bus.SubscribeOutbound(ctx)
			if !ok {
				logger.InfoC("channels", "Outbound dispatcher stopped (bus closed)")
				return
			}

			m.mu.RLock()
			channel, exists := m.channels[msg.Channel]
			m.mu.RUnlock()

			if !exists {
				logger.WarnCF("channels", "Unknown channel for outbound message", map[string]interface{}{
					logger.FieldChannel: msg.Channel,
				})
				continue
			}

			action := strings.ToLower(strings.TrimSpace(msg.Action))
			if action == "" {
				action = "send"
			}
			if action != "send" {
				if ac, ok := channel.(ActionCapable); !ok || !ac.SupportsAction(action) {
					logger.WarnCF("channels", "Channel does not support outbound action", map[string]interface{}{
						logger.FieldChannel: msg.Channel,
						"action":          action,
					})
					continue
				}
			}

			if m.outboundLimit != nil {
				if err := m.outboundLimit.Wait(ctx); err != nil {
					logger.WarnCF("channels", "Outbound rate limiter canceled", map[string]interface{}{logger.FieldError: err.Error()})
					continue
				}
			}
			// Bound fan-out concurrency to prevent goroutine explosion under burst traffic.
			m.dispatchSem <- struct{}{}
			go func(c Channel, outbound bus.OutboundMessage) {
				defer func() { <-m.dispatchSem }()
				if err := c.Send(ctx, outbound); err != nil {
					logger.ErrorCF("channels", "Error sending message to channel", map[string]interface{}{
						logger.FieldChannel: outbound.Channel,
						logger.FieldError:   err.Error(),
					})
				}
			}(channel, msg)
		}
	}
}

func (m *Manager) GetChannel(name string) (Channel, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	channel, ok := m.channels[name]
	return channel, ok
}

func (m *Manager) GetStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[string]interface{})
	for name := range m.channels {
		status[name] = map[string]interface{}{}
	}
	return status
}

func (m *Manager) GetEnabledChannels() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.channels))
	for name := range m.channels {
		names = append(names, name)
	}
	return names
}

func (m *Manager) RegisterChannel(name string, channel Channel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[name] = channel
}

func (m *Manager) UnregisterChannel(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.channels, name)
}

func (m *Manager) SendToChannel(ctx context.Context, channelName, chatID, content string) error {
	m.mu.RLock()
	channel, exists := m.channels[channelName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("channel %s not found", channelName)
	}

	msg := bus.OutboundMessage{
		Channel: channelName,
		ChatID:  chatID,
		Content: content,
	}

	return channel.Send(ctx, msg)
}
