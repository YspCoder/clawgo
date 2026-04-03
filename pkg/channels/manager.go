// ClawGo - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 ClawGo contributors

package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/logger"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
)

type Manager struct {
	channels       map[string]Channel
	bus            *bus.MessageBus
	config         *config.Config
	dispatchTask   *asyncTask
	dispatchSem    chan struct{}
	outboundLimit  *rate.Limiter
	mu             sync.RWMutex
	snapshot       atomic.Value // map[string]Channel
	outboundSeenMu sync.Mutex
	outboundSeen   map[string]time.Time
	outboundTTL    time.Duration
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
		outboundSeen:  map[string]time.Time{},
		outboundTTL:   12 * time.Second,
	}
	m.snapshot.Store(map[string]Channel{})
	if cfg != nil {
		if v := cfg.Channels.OutboundDedupeWindowSeconds; v > 0 {
			m.outboundTTL = time.Duration(v) * time.Second
		}
		setInboundDedupeWindows(
			time.Duration(cfg.Channels.InboundMessageIDDedupeTTLSeconds)*time.Second,
			time.Duration(cfg.Channels.InboundContentDedupeWindowSeconds)*time.Second,
		)
	}

	if err := m.initChannels(); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Manager) initChannels() error {
	logger.InfoC("channels", logger.C0004)

	if m.config.Channels.Telegram.Enabled {
		logger.DebugCF("channels", logger.C0005, map[string]interface{}{
			"has_token": m.config.Channels.Telegram.Token != "",
		})
		if m.config.Channels.Telegram.Token == "" {
			logger.WarnC("channels", logger.C0006)
		} else {
			telegram, err := NewTelegramChannel(m.config.Channels.Telegram, m.bus)
			if err != nil {
				logger.ErrorCF("channels", logger.C0007, map[string]interface{}{
					logger.FieldError: err.Error(),
				})
			} else {
				m.channels["telegram"] = telegram
				logger.InfoC("channels", logger.C0008)
			}
		}
	}

	if m.config.Channels.Weixin.Enabled {
		weixin, err := NewWeixinChannel(m.config.Channels.Weixin, m.bus)
		if err != nil {
			logger.ErrorCF("channels", 0, map[string]interface{}{
				logger.FieldChannel: "weixin",
				logger.FieldError:   err.Error(),
			})
		} else {
			m.channels["weixin"] = weixin
			logger.InfoCF("channels", 0, map[string]interface{}{
				logger.FieldChannel: "weixin",
			})
		}
	}

	if m.config.Channels.Feishu.Enabled {
		feishu, err := NewFeishuChannel(m.config.Channels.Feishu, m.bus)
		if err != nil {
			logger.ErrorCF("channels", logger.C0012, map[string]interface{}{
				logger.FieldError: err.Error(),
			})
		} else {
			m.channels["feishu"] = feishu
			logger.InfoC("channels", logger.C0013)
		}
	}

	logger.InfoCF("channels", logger.C0024, map[string]interface{}{
		"enabled_channels": len(m.channels),
	})
	m.refreshSnapshot()

	return nil
}

func (m *Manager) refreshSnapshot() {
	next := make(map[string]Channel, len(m.channels))
	for k, v := range m.channels {
		next[k] = v
	}
	m.snapshot.Store(next)
}

func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	if len(m.channels) == 0 {
		m.mu.Unlock()
		logger.WarnC("channels", logger.C0025)
		return nil
	}
	channelsSnapshot := make(map[string]Channel, len(m.channels))
	for k, v := range m.channels {
		channelsSnapshot[k] = v
	}
	dispatchCtx, cancel := context.WithCancel(ctx)
	m.dispatchTask = &asyncTask{cancel: cancel}
	m.mu.Unlock()

	logger.InfoC("channels", logger.C0026)
	go m.dispatchOutbound(dispatchCtx)

	var g errgroup.Group
	for name, channel := range channelsSnapshot {
		name := name
		channel := channel
		g.Go(func() error {
			logger.InfoCF("channels", logger.C0027, map[string]interface{}{logger.FieldChannel: name})
			if err := channel.Start(ctx); err != nil {
				logger.ErrorCF("channels", logger.C0028, map[string]interface{}{logger.FieldChannel: name, logger.FieldError: err.Error()})
				return fmt.Errorf("%s: %w", name, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	logger.InfoC("channels", logger.C0029)
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

	logger.InfoC("channels", logger.C0030)
	if task != nil {
		task.cancel()
	}

	var g errgroup.Group
	for name, channel := range channelsSnapshot {
		name := name
		channel := channel
		g.Go(func() error {
			logger.InfoCF("channels", logger.C0031, map[string]interface{}{logger.FieldChannel: name})
			if err := channel.Stop(ctx); err != nil {
				logger.ErrorCF("channels", logger.C0032, map[string]interface{}{logger.FieldChannel: name, logger.FieldError: err.Error()})
				return fmt.Errorf("%s: %w", name, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	logger.InfoC("channels", logger.C0033)
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

	logger.InfoCF("channels", logger.C0034, map[string]interface{}{"channel": name})
	_ = channel.Stop(ctx)
	return channel.Start(ctx)
}

func outboundDigest(msg bus.OutboundMessage) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(msg.Channel))))
	_, _ = h.Write([]byte("|" + strings.TrimSpace(msg.ChatID)))
	_, _ = h.Write([]byte("|" + strings.TrimSpace(msg.Action)))
	_, _ = h.Write([]byte("|" + strings.TrimSpace(msg.Content)))
	_, _ = h.Write([]byte("|" + strings.TrimSpace(msg.Media)))
	_, _ = h.Write([]byte("|" + strings.TrimSpace(msg.ReplyToID)))
	if len(msg.Buttons) > 0 {
		if b, err := json.Marshal(msg.Buttons); err == nil {
			_, _ = h.Write([]byte("|" + string(b)))
		}
	}
	return fmt.Sprintf("%08x", h.Sum32())
}

func (m *Manager) shouldSkipOutboundDuplicate(msg bus.OutboundMessage) bool {
	action := strings.ToLower(strings.TrimSpace(msg.Action))
	if action == "" {
		action = "send"
	}
	if action != "send" {
		return false
	}
	key := outboundDigest(msg)
	now := time.Now()
	ttl := m.outboundTTL
	if ttl <= 0 {
		ttl = 12 * time.Second
	}
	m.outboundSeenMu.Lock()
	defer m.outboundSeenMu.Unlock()
	for k, ts := range m.outboundSeen {
		if now.Sub(ts) > 20*time.Second {
			delete(m.outboundSeen, k)
		}
	}
	if ts, ok := m.outboundSeen[key]; ok && now.Sub(ts) <= ttl {
		return true
	}
	m.outboundSeen[key] = now
	return false
}

func (m *Manager) dispatchOutbound(ctx context.Context) {
	logger.InfoC("channels", logger.C0035)

	for {
		select {
		case <-ctx.Done():
			logger.InfoC("channels", logger.C0036)
			return
		default:
			msg, ok := m.bus.SubscribeOutbound(ctx)
			if !ok {
				logger.InfoC("channels", logger.C0037)
				return
			}
			if m.shouldSkipOutboundDuplicate(msg) {
				logger.WarnCF("channels", logger.C0038, map[string]interface{}{
					logger.FieldChannel: msg.Channel,
					logger.FieldChatID:  msg.ChatID,
				})
				continue
			}

			cur, _ := m.snapshot.Load().(map[string]Channel)
			channel, exists := cur[msg.Channel]

			if !exists {
				ch := strings.ToLower(strings.TrimSpace(msg.Channel))
				if ch == "system" || ch == "internal" || ch == "" {
					// Internal/system pseudo channels are not externally dispatchable.
					continue
				}
				logger.WarnCF("channels", logger.C0039, map[string]interface{}{
					logger.FieldChannel: msg.Channel,
				})
				continue
			}

			action := msg.Action
			if action == "" {
				action = "send"
			}
			if action != "send" {
				if ac, ok := channel.(ActionCapable); !ok || !ac.SupportsAction(action) {
					logger.WarnCF("channels", logger.C0040, map[string]interface{}{
						logger.FieldChannel: msg.Channel,
						"action":            action,
					})
					continue
				}
			}

			if m.outboundLimit != nil {
				if err := m.outboundLimit.Wait(ctx); err != nil {
					logger.WarnCF("channels", logger.C0041, map[string]interface{}{logger.FieldError: err.Error()})
					continue
				}
			}
			// Bound fan-out concurrency to prevent goroutine explosion under burst traffic.
			m.dispatchSem <- struct{}{}
			go func(c Channel, outbound bus.OutboundMessage) {
				defer func() { <-m.dispatchSem }()
				if err := c.Send(ctx, outbound); err != nil {
					logger.ErrorCF("channels", logger.C0042, map[string]interface{}{
						logger.FieldChannel: outbound.Channel,
						logger.FieldError:   err.Error(),
					})
				}
			}(channel, msg)
		}
	}
}

func (m *Manager) GetEnabledChannels() []string {
	cur, _ := m.snapshot.Load().(map[string]Channel)
	names := make([]string, 0, len(cur))
	for name := range cur {
		names = append(names, name)
	}
	return names
}

func (m *Manager) GetChannel(name string) (Channel, bool) {
	cur, _ := m.snapshot.Load().(map[string]Channel)
	ch, ok := cur[strings.TrimSpace(name)]
	return ch, ok
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
