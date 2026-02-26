package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"time"

	"clawgo/pkg/agent"
	"clawgo/pkg/autonomy"
	"clawgo/pkg/bus"
	"clawgo/pkg/channels"
	"clawgo/pkg/config"
	"clawgo/pkg/cron"
	"clawgo/pkg/heartbeat"
	"clawgo/pkg/logger"
	"clawgo/pkg/nodes"
	"clawgo/pkg/providers"
	"clawgo/pkg/runtimecfg"
	"clawgo/pkg/sentinel"
)

func gatewayCmd() {
	args := os.Args[2:]
	if len(args) == 0 {
		if err := gatewayInstallServiceCmd(); err != nil {
			fmt.Printf("Error registering gateway service: %v\n", err)
			os.Exit(1)
		}
		return
	}

	switch args[0] {
	case "run":
	case "start", "stop", "restart", "status":
		if err := gatewayServiceControlCmd(args[0]); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		return
	case "autonomy":
		if err := gatewayAutonomyControlCmd(args[1:]); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		return
	default:
		fmt.Printf("Unknown gateway command: %s\n", args[0])
		fmt.Println("Usage: clawgo gateway [run|start|stop|restart|status|autonomy on|off|status]")
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
	runtimecfg.Set(cfg)
	if strings.EqualFold(strings.TrimSpace(os.Getenv(envRootGranted)), "1") || strings.EqualFold(strings.TrimSpace(os.Getenv(envRootGranted)), "true") {
		applyMaximumPermissionPolicy(cfg)
	}

	msgBus := bus.NewMessageBus()
	cronStorePath := filepath.Join(filepath.Dir(getConfigPath()), "cron", "jobs.json")
	cronService := cron.NewCronService(cronStorePath, func(job *cron.CronJob) (string, error) {
		if job == nil {
			return "", nil
		}

		targetChannel := strings.TrimSpace(job.Payload.Channel)
		targetChatID := strings.TrimSpace(job.Payload.To)
		message := strings.TrimSpace(job.Payload.Message)

		if job.Payload.Deliver && targetChannel != "" && targetChatID != "" && message != "" {
			msgBus.PublishOutbound(bus.OutboundMessage{
				Channel: targetChannel,
				ChatID:  targetChatID,
				Content: message,
			})
			return "delivered", nil
		}

		if message == "" {
			return "", nil
		}
		if targetChannel == "" || targetChatID == "" {
			targetChannel = "internal"
			targetChatID = "cron"
		}
		msgBus.PublishInbound(bus.InboundMessage{
			Channel:    "system",
			SenderID:   "cron",
			ChatID:     fmt.Sprintf("%s:%s", targetChannel, targetChatID),
			Content:    message,
			SessionKey: fmt.Sprintf("cron:%s", job.ID),
			Metadata: map[string]string{
				"trigger": "cron",
				"job_id":  job.ID,
			},
		})
		return "scheduled", nil
	})
	configureCronServiceRuntime(cronService, cfg)
	heartbeatService := buildHeartbeatService(cfg, msgBus)
	autonomyEngine := buildAutonomyEngine(cfg, msgBus)
	sentinelService := sentinel.NewService(
		getConfigPath(),
		cfg.WorkspacePath(),
		cfg.Sentinel.IntervalSec,
		cfg.Sentinel.AutoHeal,
		func(message string) {
			if cfg.Sentinel.NotifyChannel != "" && cfg.Sentinel.NotifyChatID != "" {
				msgBus.PublishOutbound(bus.OutboundMessage{
					Channel: cfg.Sentinel.NotifyChannel,
					ChatID:  cfg.Sentinel.NotifyChatID,
					Content: "[Sentinel] " + message,
				})
			}
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agentLoop, channelManager, err := buildGatewayRuntime(ctx, cfg, msgBus, cronService)
	if err != nil {
		fmt.Printf("Error initializing gateway runtime: %v\n", err)
		os.Exit(1)
	}
	sentinelService.SetManager(channelManager)

	pidFile := filepath.Join(filepath.Dir(getConfigPath()), "gateway.pid")
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644); err != nil {
		fmt.Printf("Warning: failed to write PID file: %v\n", err)
	} else {
		defer os.Remove(pidFile)
	}

	enabledChannels := channelManager.GetEnabledChannels()
	if len(enabledChannels) > 0 {
		fmt.Printf("✓ Channels enabled: %s\n", enabledChannels)
	} else {
		fmt.Println("⚠ Warning: No channels enabled")
	}

	fmt.Printf("✓ Gateway started on %s:%d\n", cfg.Gateway.Host, cfg.Gateway.Port)
	fmt.Println("Press Ctrl+C to stop. Send SIGHUP to hot-reload config.")

	if err := cronService.Start(); err != nil {
		fmt.Printf("Error starting cron service: %v\n", err)
	}
	fmt.Println("✓ Cron service started")

	if err := heartbeatService.Start(); err != nil {
		fmt.Printf("Error starting heartbeat service: %v\n", err)
	}
	fmt.Println("✓ Heartbeat service started")
	autonomyEngine.Start()
	if cfg.Agents.Defaults.Autonomy.Enabled {
		fmt.Println("✓ Autonomy engine started")
	}
	if cfg.Sentinel.Enabled {
		sentinelService.Start()
		fmt.Println("✓ Sentinel service started")
	}

	if err := channelManager.StartAll(ctx); err != nil {
		fmt.Printf("Error starting channels: %v\n", err)
	}

	registryServer := nodes.NewRegistryServer(cfg.Gateway.Host, cfg.Gateway.Port, cfg.Gateway.Token, nodes.DefaultManager())
	registryServer.SetConfigPath(getConfigPath())
	registryServer.SetWorkspacePath(cfg.WorkspacePath())
	registryServer.SetLogFilePath(cfg.LogFilePath())
	registryServer.SetWebUIDir(filepath.Join(cfg.WorkspacePath(), "webui-dist"))
	registryServer.SetChatHandler(func(cctx context.Context, sessionKey, content string) (string, error) {
		if strings.TrimSpace(content) == "" {
			return "", nil
		}
		return agentLoop.ProcessDirect(cctx, content, sessionKey)
	})
	registryServer.SetChatHistoryHandler(func(sessionKey string) []map[string]interface{} {
		h := agentLoop.GetSessionHistory(sessionKey)
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
	registryServer.SetConfigAfterHook(func() {
		_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
	})
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
	if err := registryServer.Start(ctx); err != nil {
		fmt.Printf("Error starting node registry server: %v\n", err)
	} else {
		fmt.Printf("✓ Node registry server started on %s:%d\n", cfg.Gateway.Host, cfg.Gateway.Port)
	}

	go agentLoop.Run(ctx)
	go runGatewayStartupCompactionCheck(ctx, agentLoop)
	go runGatewayBootstrapInit(ctx, cfg, agentLoop)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	for {
		sig := <-sigChan
		switch sig {
		case syscall.SIGHUP:
			fmt.Println("\n↻ Reloading config...")
			newCfg, err := config.LoadConfig(getConfigPath())
			if err != nil {
				fmt.Printf("✗ Reload failed (load config): %v\n", err)
				continue
			}
			if strings.EqualFold(strings.TrimSpace(os.Getenv(envRootGranted)), "1") || strings.EqualFold(strings.TrimSpace(os.Getenv(envRootGranted)), "true") {
				applyMaximumPermissionPolicy(newCfg)
			}
			configureCronServiceRuntime(cronService, newCfg)
			heartbeatService.Stop()
			autonomyEngine.Stop()
			heartbeatService = buildHeartbeatService(newCfg, msgBus)
			autonomyEngine = buildAutonomyEngine(newCfg, msgBus)
			if err := heartbeatService.Start(); err != nil {
				fmt.Printf("Error starting heartbeat service: %v\n", err)
			}
			autonomyEngine.Start()

			if reflect.DeepEqual(cfg, newCfg) {
				fmt.Println("✓ Config unchanged, skip reload")
				continue
			}

			runtimeSame := reflect.DeepEqual(cfg.Agents, newCfg.Agents) &&
				reflect.DeepEqual(cfg.Providers, newCfg.Providers) &&
				reflect.DeepEqual(cfg.Tools, newCfg.Tools) &&
				reflect.DeepEqual(cfg.Channels, newCfg.Channels)

			templateChanges := summarizeDialogTemplateChanges(cfg, newCfg)
			autonomyChanges := summarizeAutonomyChanges(cfg, newCfg)

			if runtimeSame {
				configureLogging(newCfg)
				sentinelService.Stop()
				sentinelService = sentinel.NewService(
					getConfigPath(),
					newCfg.WorkspacePath(),
					newCfg.Sentinel.IntervalSec,
					newCfg.Sentinel.AutoHeal,
					func(message string) {
						if newCfg.Sentinel.NotifyChannel != "" && newCfg.Sentinel.NotifyChatID != "" {
							msgBus.PublishOutbound(bus.OutboundMessage{
								Channel: newCfg.Sentinel.NotifyChannel,
								ChatID:  newCfg.Sentinel.NotifyChatID,
								Content: "[Sentinel] " + message,
							})
						}
					},
				)
				if newCfg.Sentinel.Enabled {
					sentinelService.SetManager(channelManager)
					sentinelService.Start()
				}
				cfg = newCfg
				runtimecfg.Set(cfg)
				if len(templateChanges) > 0 {
					fmt.Printf("↻ Dialog template changes: %s\n", strings.Join(templateChanges, ", "))
				}
				if len(autonomyChanges) > 0 {
					fmt.Printf("↻ Autonomy changes: %s\n", strings.Join(autonomyChanges, ", "))
				}
				fmt.Println("✓ Config hot-reload applied (logging/metadata only)")
				continue
			}

			newAgentLoop, newChannelManager, err := buildGatewayRuntime(ctx, newCfg, msgBus, cronService)
			if err != nil {
				fmt.Printf("✗ Reload failed (init runtime): %v\n", err)
				continue
			}

			channelManager.StopAll(ctx)
			agentLoop.Stop()

			channelManager = newChannelManager
			agentLoop = newAgentLoop
			cfg = newCfg
			runtimecfg.Set(cfg)
			sentinelService.Stop()
			sentinelService = sentinel.NewService(
				getConfigPath(),
				newCfg.WorkspacePath(),
				newCfg.Sentinel.IntervalSec,
				newCfg.Sentinel.AutoHeal,
				func(message string) {
					if newCfg.Sentinel.NotifyChannel != "" && newCfg.Sentinel.NotifyChatID != "" {
						msgBus.PublishOutbound(bus.OutboundMessage{
							Channel: newCfg.Sentinel.NotifyChannel,
							ChatID:  newCfg.Sentinel.NotifyChatID,
							Content: "[Sentinel] " + message,
						})
					}
				},
			)
			if newCfg.Sentinel.Enabled {
				sentinelService.Start()
			}
			sentinelService.SetManager(channelManager)

			if err := channelManager.StartAll(ctx); err != nil {
				fmt.Printf("✗ Reload failed (start channels): %v\n", err)
				continue
			}
			go agentLoop.Run(ctx)
			if len(templateChanges) > 0 {
				fmt.Printf("↻ Dialog template changes: %s\n", strings.Join(templateChanges, ", "))
			}
			if len(autonomyChanges) > 0 {
				fmt.Printf("↻ Autonomy changes: %s\n", strings.Join(autonomyChanges, ", "))
			}
			fmt.Println("✓ Config hot-reload applied")
		default:
			fmt.Println("\nShutting down...")
			cancel()
			heartbeatService.Stop()
			autonomyEngine.Stop()
			sentinelService.Stop()
			cronService.Stop()
			agentLoop.Stop()
			channelManager.StopAll(ctx)
			fmt.Println("✓ Gateway stopped")
			return
		}
	}
}

func gatewayAutonomyControlCmd(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: clawgo gateway autonomy [on|off|status]")
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	memDir := filepath.Join(cfg.WorkspacePath(), "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		return err
	}
	pausePath := filepath.Join(memDir, "autonomy.pause")
	ctrlPath := filepath.Join(memDir, "autonomy.control.json")

	type autonomyControl struct {
		Enabled   bool   `json:"enabled"`
		UpdatedAt string `json:"updated_at"`
		Source    string `json:"source"`
	}

	writeControl := func(enabled bool) error {
		c := autonomyControl{Enabled: enabled, UpdatedAt: time.Now().UTC().Format(time.RFC3339), Source: "manual_cli"}
		data, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(ctrlPath, append(data, '\n'), 0644)
	}

	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "on":
		_ = os.Remove(pausePath)
		if err := writeControl(true); err != nil {
			return err
		}
		fmt.Println("✓ Autonomy enabled")
		return nil
	case "off":
		if err := writeControl(false); err != nil {
			return err
		}
		if err := os.WriteFile(pausePath, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0644); err != nil {
			return err
		}
		fmt.Println("✓ Autonomy disabled (paused)")
		return nil
	case "status":
		enabled := true
		reason := "default"
		updatedAt := ""
		source := ""
		if data, err := os.ReadFile(ctrlPath); err == nil {
			var c autonomyControl
			if json.Unmarshal(data, &c) == nil {
				enabled = c.Enabled
				updatedAt = c.UpdatedAt
				source = c.Source
				if !c.Enabled {
					reason = "control_file"
				}
			}
		}
		if _, err := os.Stat(pausePath); err == nil {
			enabled = false
			reason = "pause_file"
		}
		fmt.Printf("Autonomy status: %v (%s)\n", enabled, reason)
		if strings.TrimSpace(updatedAt) != "" {
			fmt.Printf("Last switch: %s", updatedAt)
			if strings.TrimSpace(source) != "" {
				fmt.Printf(" via %s", source)
			}
			fmt.Println()
		}
		fmt.Printf("Control file: %s\n", ctrlPath)
		fmt.Printf("Pause file: %s\n", pausePath)
		return nil
	default:
		return fmt.Errorf("usage: clawgo gateway autonomy [on|off|status]")
	}
}

func summarizeAutonomyChanges(oldCfg, newCfg *config.Config) []string {
	if oldCfg == nil || newCfg == nil {
		return nil
	}
	o := oldCfg.Agents.Defaults.Autonomy
	n := newCfg.Agents.Defaults.Autonomy
	changes := make([]string, 0)
	if o.Enabled != n.Enabled {
		changes = append(changes, "enabled")
	}
	if o.TickIntervalSec != n.TickIntervalSec {
		changes = append(changes, "tick_interval_sec")
	}
	if o.MinRunIntervalSec != n.MinRunIntervalSec {
		changes = append(changes, "min_run_interval_sec")
	}
	if o.UserIdleResumeSec != n.UserIdleResumeSec {
		changes = append(changes, "user_idle_resume_sec")
	}
	if o.WaitingResumeDebounceSec != n.WaitingResumeDebounceSec {
		changes = append(changes, "waiting_resume_debounce_sec")
	}
	if strings.TrimSpace(o.QuietHours) != strings.TrimSpace(n.QuietHours) {
		changes = append(changes, "quiet_hours")
	}
	if o.NotifyCooldownSec != n.NotifyCooldownSec {
		changes = append(changes, "notify_cooldown_sec")
	}
	if o.NotifySameReasonCooldownSec != n.NotifySameReasonCooldownSec {
		changes = append(changes, "notify_same_reason_cooldown_sec")
	}
	return changes
}

func summarizeDialogTemplateChanges(oldCfg, newCfg *config.Config) []string {
	if oldCfg == nil || newCfg == nil {
		return nil
	}
	type pair struct {
		name string
		a    string
		b    string
	}
	oldT := oldCfg.Agents.Defaults.Texts
	newT := newCfg.Agents.Defaults.Texts
	checks := []pair{
		{name: "system_rewrite_template", a: oldT.SystemRewriteTemplate, b: newT.SystemRewriteTemplate},
		{name: "lang_usage", a: oldT.LangUsage, b: newT.LangUsage},
		{name: "lang_invalid", a: oldT.LangInvalid, b: newT.LangInvalid},
		{name: "lang_updated_template", a: oldT.LangUpdatedTemplate, b: newT.LangUpdatedTemplate},
		{name: "runtime_compaction_note", a: oldT.RuntimeCompactionNote, b: newT.RuntimeCompactionNote},
		{name: "startup_compaction_note", a: oldT.StartupCompactionNote, b: newT.StartupCompactionNote},
		{name: "autonomy_completion_template", a: oldT.AutonomyCompletionTemplate, b: newT.AutonomyCompletionTemplate},
		{name: "autonomy_blocked_template", a: oldT.AutonomyBlockedTemplate, b: newT.AutonomyBlockedTemplate},
	}
	out := make([]string, 0)
	for _, c := range checks {
		if strings.TrimSpace(c.a) != strings.TrimSpace(c.b) {
			out = append(out, c.name)
		}
	}
	if strings.Join(oldT.AutonomyImportantKeywords, "|") != strings.Join(newT.AutonomyImportantKeywords, "|") {
		out = append(out, "autonomy_important_keywords")
	}
	if oldCfg.Agents.Defaults.Heartbeat.PromptTemplate != newCfg.Agents.Defaults.Heartbeat.PromptTemplate {
		out = append(out, "heartbeat.prompt_template")
	}
	return out
}

func runGatewayStartupCompactionCheck(parent context.Context, agentLoop *agent.AgentLoop) {
	if agentLoop == nil {
		return
	}

	checkCtx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()

	report := agentLoop.RunStartupSelfCheckAllSessions(checkCtx)
	logger.InfoCF("gateway", "Startup compaction check completed", map[string]interface{}{
		"sessions_total":     report.TotalSessions,
		"sessions_compacted": report.CompactedSessions,
	})
}

func runGatewayBootstrapInit(parent context.Context, cfg *config.Config, agentLoop *agent.AgentLoop) {
	if agentLoop == nil || cfg == nil {
		return
	}
	workspace := cfg.WorkspacePath()
	bootstrapPath := filepath.Join(workspace, "BOOTSTRAP.md")
	if _, err := os.Stat(bootstrapPath); err != nil {
		return
	}
	memDir := filepath.Join(workspace, "memory")
	_ = os.MkdirAll(memDir, 0755)
	markerPath := filepath.Join(memDir, "bootstrap.init.done")
	if _, err := os.Stat(markerPath); err == nil {
		return
	}

	initCtx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	prompt := "System startup bootstrap: read BOOTSTRAP.md and perform one-time self-initialization checks now. If already initialized, return concise status only."
	resp, err := agentLoop.ProcessDirect(initCtx, prompt, "system:bootstrap:init")
	if err != nil {
		logger.ErrorCF("gateway", "Bootstrap init model call failed", map[string]interface{}{logger.FieldError: err.Error()})
		return
	}
	line := fmt.Sprintf("%s\n%s\n", time.Now().UTC().Format(time.RFC3339), strings.TrimSpace(resp))
	_ = os.WriteFile(markerPath, []byte(line), 0644)
	logger.InfoC("gateway", "Bootstrap init model call completed")
}

func maybePromptAndEscalateRoot(command string) {
	if os.Getenv(envRootPrompted) == "1" {
		return
	}
	if !isInteractiveStdin() {
		return
	}

	fmt.Printf("Grant root permissions for `clawgo %s`? (yes/no): ", command)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer != "yes" && answer != "y" {
		_ = os.Setenv(envRootPrompted, "1")
		_ = os.Setenv(envRootGranted, "0")
		return
	}

	_ = os.Setenv(envRootPrompted, "1")
	_ = os.Setenv(envRootGranted, "1")

	if os.Geteuid() == 0 {
		return
	}

	exePath, err := os.Executable()
	if err != nil {
		fmt.Printf("Error resolving executable for sudo re-run: %v\n", err)
		os.Exit(1)
	}
	exePath, _ = filepath.Abs(exePath)

	cmdArgs := append([]string{"-E", exePath}, os.Args[1:]...)
	cmd := exec.Command("sudo", cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), envRootPrompted+"=1", envRootGranted+"=1")

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Printf("Failed to elevate privileges with sudo: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func shouldPromptGatewayRoot(args []string) bool {
	return len(args) == 2 && args[1] == "gateway"
}

func isInteractiveStdin() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func applyMaximumPermissionPolicy(cfg *config.Config) {
	cfg.Tools.Shell.Enabled = true
	cfg.Tools.Shell.Sandbox.Enabled = false
}

func gatewayInstallServiceCmd() error {
	scope, unitPath, err := detectGatewayServiceScopeAndPath()
	if err != nil {
		return err
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path failed: %w", err)
	}
	exePath, _ = filepath.Abs(exePath)
	configPath := getConfigPath()
	workDir := filepath.Dir(exePath)

	unitContent := buildGatewayUnitContent(scope, exePath, configPath, workDir)
	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		return fmt.Errorf("create service directory failed: %w", err)
	}
	if err := os.WriteFile(unitPath, []byte(unitContent), 0644); err != nil {
		return fmt.Errorf("write service unit failed: %w", err)
	}

	if err := runSystemctl(scope, "daemon-reload"); err != nil {
		return err
	}
	if err := runSystemctl(scope, "enable", gatewayServiceName); err != nil {
		return err
	}

	fmt.Printf("✓ Gateway service registered: %s (%s)\n", gatewayServiceName, scope)
	fmt.Printf("  Unit file: %s\n", unitPath)
	fmt.Println("  Start service:   clawgo gateway start")
	fmt.Println("  Restart service: clawgo gateway restart")
	fmt.Println("  Stop service:    clawgo gateway stop")
	return nil
}

func gatewayServiceControlCmd(action string) error {
	scope, _, err := detectInstalledGatewayService()
	if err != nil {
		return err
	}
	return runSystemctl(scope, action, gatewayServiceName)
}

func detectGatewayServiceScopeAndPath() (string, string, error) {
	if runtime.GOOS != "linux" {
		return "", "", fmt.Errorf("gateway service registration currently supports Linux systemd only")
	}
	if strings.ToLower(strings.TrimSpace(os.Getenv("CLAWGO_GATEWAY_SCOPE"))) == "user" {
		return userGatewayUnitPath()
	}
	if strings.ToLower(strings.TrimSpace(os.Getenv("CLAWGO_GATEWAY_SCOPE"))) == "system" {
		return "system", "/etc/systemd/system/" + gatewayServiceName, nil
	}
	if os.Geteuid() == 0 {
		return "system", "/etc/systemd/system/" + gatewayServiceName, nil
	}
	return userGatewayUnitPath()
}

func userGatewayUnitPath() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve user home failed: %w", err)
	}
	return "user", filepath.Join(home, ".config", "systemd", "user", gatewayServiceName), nil
}

func detectInstalledGatewayService() (string, string, error) {
	systemPath := "/etc/systemd/system/" + gatewayServiceName
	userScope, userPath, err := userGatewayUnitPath()
	if err != nil {
		return "", "", err
	}

	systemExists := false
	if info, err := os.Stat(systemPath); err == nil && !info.IsDir() {
		systemExists = true
	}

	userExists := false
	if info, err := os.Stat(userPath); err == nil && !info.IsDir() {
		userExists = true
	}

	preferredScope := strings.ToLower(strings.TrimSpace(os.Getenv("CLAWGO_GATEWAY_SCOPE")))
	switch preferredScope {
	case "system":
		if systemExists {
			return "system", systemPath, nil
		}
		return "", "", fmt.Errorf("gateway service unit not found in system scope: %s", systemPath)
	case "user":
		if userExists {
			return userScope, userPath, nil
		}
		return "", "", fmt.Errorf("gateway service unit not found in user scope: %s", userPath)
	}

	// Auto-pick scope by current privilege to avoid non-root users accidentally
	// selecting system scope when both unit files exist.
	if os.Geteuid() == 0 {
		if systemExists {
			return "system", systemPath, nil
		}
		if userExists {
			return userScope, userPath, nil
		}
	} else {
		if userExists {
			return userScope, userPath, nil
		}
		if systemExists {
			return "system", systemPath, nil
		}
	}

	return "", "", fmt.Errorf("gateway service not registered. Run: clawgo gateway")
}

func buildGatewayUnitContent(scope, exePath, configPath, workDir string) string {
	quotedExec := fmt.Sprintf("%q gateway run --config %q", exePath, configPath)
	installTarget := "default.target"
	if scope == "system" {
		installTarget = "multi-user.target"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = filepath.Dir(configPath)
	}

	return fmt.Sprintf(`[Unit]
Description=ClawGo Gateway
After=network.target

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s
Restart=always
RestartSec=3
Environment=CLAWGO_CONFIG=%s
Environment=HOME=%s

[Install]
WantedBy=%s
`, workDir, quotedExec, configPath, home, installTarget)
}

func runSystemctl(scope string, args ...string) error {
	cmdArgs := make([]string, 0, len(args)+1)
	if scope == "user" {
		cmdArgs = append(cmdArgs, "--user")
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command("systemctl", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if scope == "user" {
			return fmt.Errorf("systemctl --user %s failed: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("systemctl %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

func buildGatewayRuntime(ctx context.Context, cfg *config.Config, msgBus *bus.MessageBus, cronService *cron.CronService) (*agent.AgentLoop, *channels.Manager, error) {
	provider, err := providers.CreateProvider(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("create provider: %w", err)
	}

	agentLoop := agent.NewAgentLoop(cfg, msgBus, provider, cronService)

	startupInfo := agentLoop.GetStartupInfo()
	toolsInfo := startupInfo["tools"].(map[string]interface{})
	skillsInfo := startupInfo["skills"].(map[string]interface{})
	fmt.Println("\n📦 Agent Status:")
	fmt.Printf("  • Tools: %d loaded\n", toolsInfo["count"])
	fmt.Printf("  • Skills: %d/%d available\n", skillsInfo["available"], skillsInfo["total"])

	logger.InfoCF("agent", "Agent initialized",
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

func buildAutonomyEngine(cfg *config.Config, msgBus *bus.MessageBus) *autonomy.Engine {
	a := cfg.Agents.Defaults.Autonomy
	return autonomy.NewEngine(autonomy.Options{
		Enabled:                     a.Enabled,
		TickIntervalSec:             a.TickIntervalSec,
		MinRunIntervalSec:           a.MinRunIntervalSec,
		MaxPendingDurationSec:       a.MaxPendingDurationSec,
		MaxConsecutiveStalls:        a.MaxConsecutiveStalls,
		MaxDispatchPerTick:          a.MaxDispatchPerTick,
		NotifyCooldownSec:           a.NotifyCooldownSec,
		NotifySameReasonCooldownSec: a.NotifySameReasonCooldownSec,
		QuietHours:                  a.QuietHours,
		UserIdleResumeSec:           a.UserIdleResumeSec,
		WaitingResumeDebounceSec:    a.WaitingResumeDebounceSec,
		ImportantKeywords:           cfg.Agents.Defaults.Texts.AutonomyImportantKeywords,
		CompletionTemplate:          cfg.Agents.Defaults.Texts.AutonomyCompletionTemplate,
		BlockedTemplate:             cfg.Agents.Defaults.Texts.AutonomyBlockedTemplate,
		Workspace:                   cfg.WorkspacePath(),
		DefaultNotifyChannel:        a.NotifyChannel,
		DefaultNotifyChatID:         a.NotifyChatID,
	}, msgBus)
}
