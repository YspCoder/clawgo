package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/YspCoder/clawgo/pkg/agent"
	"github.com/YspCoder/clawgo/pkg/api"
	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/channels"
	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/cron"
	"github.com/YspCoder/clawgo/pkg/heartbeat"
	"github.com/YspCoder/clawgo/pkg/logger"
	"github.com/YspCoder/clawgo/pkg/providers"
	"github.com/YspCoder/clawgo/pkg/sentinel"
)

type gatewayRuntimeState struct {
	cfg              *config.Config
	agentLoop        *agent.AgentLoop
	channelManager   *channels.Manager
	heartbeatService *heartbeat.HeartbeatService
	sentinelService  *sentinel.Service
}

func buildGatewayRuntime(ctx context.Context, cfg *config.Config, msgBus *bus.MessageBus, cronService *cron.CronService) (*agent.AgentLoop, *channels.Manager, error) {
	provider, err := providers.CreateProvider(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("create provider: %w", err)
	}

	agentLoop := agent.NewAgentLoop(cfg, msgBus, provider, cronService)
	agentLoop.SetConfigPath(getConfigPath())

	startupInfo := agentLoop.GetStartupInfo()
	toolsInfo := startupInfo["tools"].(map[string]interface{})
	skillsInfo := startupInfo["skills"].(map[string]interface{})
	fmt.Println("\nAgent Status:")
	fmt.Printf("  - Tools: %d loaded\n", toolsInfo["count"])
	fmt.Printf("  - Skills: %d/%d available\n", skillsInfo["available"], skillsInfo["total"])

	logger.InfoCF("agent", logger.C0098,
		map[string]interface{}{
			"tools_count":      toolsInfo["count"],
			"skills_total":     skillsInfo["total"],
			"skills_available": skillsInfo["available"],
		})

	channelManager, err := channels.NewManager(cfg, msgBus)
	if err != nil {
		return nil, nil, fmt.Errorf("create channel manager: %w", err)
	}

	return agentLoop, channelManager, nil
}

func bindAgentLoopHandlers(registryServer *api.Server, loop *agent.AgentLoop) {
	registryServer.SetChatHandler(func(cctx context.Context, sessionKey, content string) (string, error) {
		if strings.TrimSpace(content) == "" {
			return "", nil
		}
		return loop.ProcessDirect(cctx, content, sessionKey)
	})
	registryServer.SetChatHistoryHandler(func(sessionKey string) []map[string]interface{} {
		h := loop.GetSessionHistory(sessionKey)
		out := make([]map[string]interface{}, 0, len(h))
		for _, m := range h {
			entry := map[string]interface{}{"role": m.Role, "content": m.Content}
			if strings.TrimSpace(m.ToolCallID) != "" {
				entry["tool_call_id"] = m.ToolCallID
			}
			if len(m.ToolCalls) > 0 {
				entry["tool_calls"] = m.ToolCalls
			}
			out = append(out, entry)
		}
		return out
	})
	registryServer.SetToolsCatalogHandler(func() interface{} {
		return loop.GetToolCatalog()
	})
}

func bindCronHandler(registryServer *api.Server, cronService *cron.CronService) {
	registryServer.SetCronHandler(func(action string, args map[string]interface{}) (interface{}, error) {
		getStr := func(k string) string {
			v, _ := args[k].(string)
			return strings.TrimSpace(v)
		}
		getBoolPtr := func(k string) *bool {
			v, ok := args[k].(bool)
			if !ok {
				return nil
			}
			vv := v
			return &vv
		}
		switch strings.ToLower(strings.TrimSpace(action)) {
		case "", "list":
			return cronService.ListJobs(true), nil
		case "get":
			id := getStr("id")
			if id == "" {
				return nil, fmt.Errorf("id required")
			}
			j := cronService.GetJob(id)
			if j == nil {
				return nil, fmt.Errorf("job not found: %s", id)
			}
			return j, nil
		case "create":
			name := getStr("name")
			if name == "" {
				name = "webui-cron"
			}
			msg := getStr("message")
			if msg == "" {
				return nil, fmt.Errorf("message required")
			}
			schedule := cron.CronSchedule{}
			if expr := getStr("expr"); expr != "" {
				schedule.Expr = expr
			} else {
				// Backward compatibility for older clients.
				kind := strings.ToLower(getStr("kind"))
				switch kind {
				case "every":
					everyMS, ok := args["everyMs"].(float64)
					if !ok || int64(everyMS) <= 0 {
						return nil, fmt.Errorf("expr required")
					}
					ev := int64(everyMS)
					schedule.Kind = "every"
					schedule.EveryMS = &ev
				case "once", "at":
					atMS, ok := args["atMs"].(float64)
					var at int64
					if !ok || int64(atMS) <= 0 {
						at = time.Now().Add(1 * time.Minute).UnixMilli()
					} else {
						at = int64(atMS)
					}
					schedule.Kind = "at"
					schedule.AtMS = &at
				default:
					return nil, fmt.Errorf("expr required")
				}
			}
			deliver := false
			if v, ok := args["deliver"].(bool); ok {
				deliver = v
			}
			return cronService.AddJob(name, schedule, msg, deliver, getStr("channel"), getStr("to"))
		case "update":
			id := getStr("id")
			if id == "" {
				return nil, fmt.Errorf("id required")
			}
			in := cron.UpdateJobInput{}
			if v := getStr("name"); v != "" {
				in.Name = &v
			}
			if v := getStr("message"); v != "" {
				in.Message = &v
			}
			if p := getBoolPtr("enabled"); p != nil {
				in.Enabled = p
			}
			if p := getBoolPtr("deliver"); p != nil {
				in.Deliver = p
			}
			if v := getStr("channel"); v != "" {
				in.Channel = &v
			}
			if v := getStr("to"); v != "" {
				in.To = &v
			}
			if expr := getStr("expr"); expr != "" {
				s := cron.CronSchedule{Expr: expr}
				in.Schedule = &s
			} else if kind := strings.ToLower(getStr("kind")); kind != "" {
				// Backward compatibility for older clients.
				s := cron.CronSchedule{Kind: kind}
				switch kind {
				case "every":
					if everyMS, ok := args["everyMs"].(float64); ok && int64(everyMS) > 0 {
						ev := int64(everyMS)
						s.EveryMS = &ev
					} else {
						return nil, fmt.Errorf("expr required")
					}
				case "once", "at":
					s.Kind = "at"
					if atMS, ok := args["atMs"].(float64); ok && int64(atMS) > 0 {
						at := int64(atMS)
						s.AtMS = &at
					} else {
						at := time.Now().Add(1 * time.Minute).UnixMilli()
						s.AtMS = &at
					}
				default:
					return nil, fmt.Errorf("expr required")
				}
				in.Schedule = &s
			}
			return cronService.UpdateJob(id, in)
		case "delete":
			id := getStr("id")
			return map[string]interface{}{"deleted": cronService.RemoveJob(id), "id": id}, nil
		case "enable":
			id := getStr("id")
			j := cronService.EnableJob(id, true)
			return map[string]interface{}{"ok": j != nil, "id": id}, nil
		case "disable":
			id := getStr("id")
			j := cronService.EnableJob(id, false)
			return map[string]interface{}{"ok": j != nil, "id": id}, nil
		default:
			return nil, fmt.Errorf("unsupported cron action: %s", action)
		}
	})
}

func normalizeCronTargetChatID(channel, chatID string) string {
	ch := strings.ToLower(strings.TrimSpace(channel))
	target := strings.TrimSpace(chatID)
	if ch == "" || target == "" {
		return target
	}
	prefix := ch + ":"
	if strings.HasPrefix(strings.ToLower(target), prefix) {
		return strings.TrimSpace(target[len(prefix):])
	}
	return target
}

func dispatchCronJob(msgBus *bus.MessageBus, job *cron.CronJob) string {
	if job == nil {
		return ""
	}
	message := strings.TrimSpace(job.Payload.Message)
	if message == "" {
		return ""
	}
	targetChannel := strings.TrimSpace(job.Payload.Channel)
	targetChatID := normalizeCronTargetChatID(targetChannel, job.Payload.To)

	if targetChannel != "" && targetChatID != "" {
		msgBus.PublishOutbound(bus.OutboundMessage{
			Channel: targetChannel,
			ChatID:  targetChatID,
			Content: message,
		})
		if job.Payload.Deliver {
			return "delivered"
		}
		return "delivered_targeted"
	}

	msgBus.PublishInbound(bus.InboundMessage{
		Channel:    "system",
		SenderID:   "cron",
		ChatID:     "internal:cron",
		Content:    message,
		SessionKey: fmt.Sprintf("cron:%s", job.ID),
		Metadata: map[string]string{
			"trigger": "cron",
			"job_id":  job.ID,
		},
	})
	return "scheduled"
}

func configureCronServiceRuntime(cs *cron.CronService, cfg *config.Config) {
	if cs == nil || cfg == nil {
		return
	}
	cs.SetRuntimeOptions(cron.RuntimeOptions{
		RunLoopMinSleep:              time.Duration(cfg.Cron.MinSleepSec) * time.Second,
		RunLoopMaxSleep:              time.Duration(cfg.Cron.MaxSleepSec) * time.Second,
		RetryBackoffBase:             time.Duration(cfg.Cron.RetryBackoffBaseSec) * time.Second,
		RetryBackoffMax:              time.Duration(cfg.Cron.RetryBackoffMaxSec) * time.Second,
		MaxConsecutiveFailureRetries: int64(cfg.Cron.MaxConsecutiveFailureRetries),
		MaxWorkers:                   cfg.Cron.MaxWorkers,
	})
}

func buildHeartbeatService(cfg *config.Config, msgBus *bus.MessageBus) *heartbeat.HeartbeatService {
	hbInterval := cfg.Agents.Defaults.Heartbeat.EverySec
	if hbInterval <= 0 {
		hbInterval = 30 * 60
	}
	return heartbeat.NewHeartbeatService(cfg.WorkspacePath(), func(prompt string) (string, error) {
		msgBus.PublishInbound(bus.InboundMessage{
			Channel:    "system",
			SenderID:   "heartbeat",
			ChatID:     "internal:heartbeat",
			Content:    prompt,
			SessionKey: "heartbeat:default",
			Metadata: map[string]string{
				"trigger": "heartbeat",
			},
		})
		return "queued", nil
	}, hbInterval, cfg.Agents.Defaults.Heartbeat.Enabled, cfg.Agents.Defaults.Heartbeat.PromptTemplate)
}
