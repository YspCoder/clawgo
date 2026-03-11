package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/logger"
)

type sentinelWebhookPayload struct {
	Source    string `json:"source"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

func buildSentinelAlertHandler(cfg *config.Config, msgBus *bus.MessageBus) func(string) {
	return func(message string) {
		if cfg == nil {
			return
		}
		sendSentinelChannelAlert(cfg, msgBus, message)
		sendSentinelWebhookAlert(cfg, message)
	}
}

func sendSentinelChannelAlert(cfg *config.Config, msgBus *bus.MessageBus, message string) {
	if cfg == nil || msgBus == nil {
		return
	}
	if strings.TrimSpace(cfg.Sentinel.NotifyChannel) == "" || strings.TrimSpace(cfg.Sentinel.NotifyChatID) == "" {
		return
	}
	msgBus.PublishOutbound(bus.OutboundMessage{
		Channel: cfg.Sentinel.NotifyChannel,
		ChatID:  cfg.Sentinel.NotifyChatID,
		Content: "[Sentinel] " + message,
	})
}

func sendSentinelWebhookAlert(cfg *config.Config, message string) {
	if cfg == nil {
		return
	}
	webhookURL := strings.TrimSpace(cfg.Sentinel.WebhookURL)
	if webhookURL == "" {
		return
	}
	payload := sentinelWebhookPayload{
		Source:    "sentinel",
		Level:     "warning",
		Message:   message,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		logger.ErrorCF("sentinel", logger.C0137, map[string]interface{}{"error": err.Error(), "target": "webhook marshal"})
		return
	}
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		logger.ErrorCF("sentinel", logger.C0137, map[string]interface{}{"error": err.Error(), "target": "webhook request"})
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		logger.ErrorCF("sentinel", logger.C0137, map[string]interface{}{"error": err.Error(), "target": "webhook send"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		logger.ErrorCF("sentinel", logger.C0137, map[string]interface{}{
			"error":  "unexpected webhook status",
			"status": resp.StatusCode,
		})
	}
}
