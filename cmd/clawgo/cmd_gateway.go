package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	"clawgo/pkg/agent"
	"clawgo/pkg/api"
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
	default:
		fmt.Printf("Unknown gateway command: %s\n", args[0])
		fmt.Println("Usage: clawgo gateway [run|start|stop|restart|status]")
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
		return dispatchCronJob(msgBus, job), nil
	})
	configureCronServiceRuntime(cronService, cfg)
	heartbeatService := buildHeartbeatService(cfg, msgBus)
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
	if cfg.Sentinel.Enabled {
		sentinelService.Start()
		fmt.Println("✓ Sentinel service started")
	}

	if err := channelManager.StartAll(ctx); err != nil {
		fmt.Printf("Error starting channels: %v\n", err)
	}

	registryServer := api.NewServer(cfg.Gateway.Host, cfg.Gateway.Port, cfg.Gateway.Token, nodes.DefaultManager())
	configureGatewayNodeP2P := func(loop *agent.AgentLoop, server *api.Server, runtimeCfg *config.Config) {
		if loop == nil || server == nil || runtimeCfg == nil {
			return
		}
		server.SetNodeP2PStatusHandler(func() map[string]interface{} {
			return map[string]interface{}{
				"enabled":         runtimeCfg.Gateway.Nodes.P2P.Enabled,
				"transport":       strings.TrimSpace(runtimeCfg.Gateway.Nodes.P2P.Transport),
				"configured_stun": append([]string(nil), runtimeCfg.Gateway.Nodes.P2P.STUNServers...),
			}
		})
		switch {
		case runtimeCfg.Gateway.Nodes.P2P.Enabled && strings.EqualFold(strings.TrimSpace(runtimeCfg.Gateway.Nodes.P2P.Transport), "webrtc"):
			webrtcTransport := nodes.NewWebRTCTransport(runtimeCfg.Gateway.Nodes.P2P.STUNServers)
			loop.SetNodeP2PTransport(webrtcTransport)
			server.SetNodeWebRTCTransport(webrtcTransport)
			server.SetNodeP2PStatusHandler(func() map[string]interface{} {
				snapshot := webrtcTransport.Snapshot()
				snapshot["enabled"] = true
				snapshot["transport"] = "webrtc"
				snapshot["configured_stun"] = append([]string(nil), runtimeCfg.Gateway.Nodes.P2P.STUNServers...)
				return snapshot
			})
		default:
			server.SetNodeWebRTCTransport(nil)
		}
	}
	configureGatewayNodeP2P(agentLoop, registryServer, cfg)
	registryServer.SetGatewayVersion(version)
	registryServer.SetWebUIVersion(version)
	registryServer.SetConfigPath(getConfigPath())
	registryServer.SetWorkspacePath(cfg.WorkspacePath())
	registryServer.SetLogFilePath(cfg.LogFilePath())
	registryServer.SetWebUIDir(filepath.Join(cfg.WorkspacePath(), "webui"))
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
	reloadReqCh := make(chan struct{}, 1)
	registryServer.SetConfigAfterHook(func() {
		select {
		case reloadReqCh <- struct{}{}:
		default:
		}
	})
	registryServer.SetSubagentHandler(func(cctx context.Context, action string, args map[string]interface{}) (interface{}, error) {
		return agentLoop.HandleSubagentRuntime(cctx, action, args)
	})
	registryServer.SetToolsCatalogHandler(func() interface{} {
		return agentLoop.GetToolCatalog()
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
	signal.Notify(sigChan, gatewayNotifySignals()...)
	applyReload := func() {
		fmt.Println("\n↻ Reloading config...")
		newCfg, err := config.LoadConfig(getConfigPath())
		if err != nil {
			fmt.Printf("✗ Reload failed (load config): %v\n", err)
			return
		}
		if strings.EqualFold(strings.TrimSpace(os.Getenv(envRootGranted)), "1") || strings.EqualFold(strings.TrimSpace(os.Getenv(envRootGranted)), "true") {
			applyMaximumPermissionPolicy(newCfg)
		}
		configureCronServiceRuntime(cronService, newCfg)
		heartbeatService.Stop()
		heartbeatService = buildHeartbeatService(newCfg, msgBus)
		if err := heartbeatService.Start(); err != nil {
			fmt.Printf("Error starting heartbeat service: %v\n", err)
		}

		if reflect.DeepEqual(cfg, newCfg) {
			fmt.Println("✓ Config unchanged, skip reload")
			return
		}

		runtimeSame := reflect.DeepEqual(cfg.Agents, newCfg.Agents) &&
			reflect.DeepEqual(cfg.Providers, newCfg.Providers) &&
			reflect.DeepEqual(cfg.Tools, newCfg.Tools) &&
			reflect.DeepEqual(cfg.Channels, newCfg.Channels) &&
			reflect.DeepEqual(cfg.Gateway.Nodes, newCfg.Gateway.Nodes)

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
			configureGatewayNodeP2P(agentLoop, registryServer, cfg)
			fmt.Println("✓ Config hot-reload applied (logging/metadata only)")
			return
		}

		newAgentLoop, newChannelManager, err := buildGatewayRuntime(ctx, newCfg, msgBus, cronService)
		if err != nil {
			fmt.Printf("✗ Reload failed (init runtime): %v\n", err)
			return
		}

		channelManager.StopAll(ctx)
		agentLoop.Stop()

		channelManager = newChannelManager
		agentLoop = newAgentLoop
		cfg = newCfg
		runtimecfg.Set(cfg)
		configureGatewayNodeP2P(agentLoop, registryServer, cfg)
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
			return
		}
		go agentLoop.Run(ctx)
		fmt.Println("✓ Config hot-reload applied")
	}

	for {
		select {
		case <-reloadReqCh:
			applyReload()
		case sig := <-sigChan:
			switch {
			case isGatewayReloadSignal(sig):
				applyReload()
			default:
				fmt.Println("\nShutting down...")
				cancel()
				heartbeatService.Stop()
				sentinelService.Stop()
				cronService.Stop()
				agentLoop.Stop()
				channelManager.StopAll(ctx)
				fmt.Println("✓ Gateway stopped")
				return
			}
		}
	}
}

func runGatewayStartupCompactionCheck(parent context.Context, agentLoop *agent.AgentLoop) {
	if agentLoop == nil {
		return
	}

	checkCtx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()

	report := agentLoop.RunStartupSelfCheckAllSessions(checkCtx)
	logger.InfoCF("gateway", logger.C0110, map[string]interface{}{
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
		logger.ErrorCF("gateway", logger.C0111, map[string]interface{}{logger.FieldError: err.Error()})
		return
	}
	line := fmt.Sprintf("%s\n%s\n", time.Now().UTC().Format(time.RFC3339), strings.TrimSpace(resp))
	if err := os.WriteFile(markerPath, []byte(line), 0644); err != nil {
		logger.ErrorCF("gateway", logger.C0112, map[string]interface{}{logger.FieldError: err.Error()})
		return
	}
	// Bootstrap only runs once. After successful initialization marker is written,
	// remove BOOTSTRAP.md to avoid repeated first-run guidance.
	if err := os.Remove(bootstrapPath); err != nil && !os.IsNotExist(err) {
		logger.WarnCF("gateway", logger.C0113, map[string]interface{}{logger.FieldError: err.Error()})
	}
	logger.InfoC("gateway", logger.C0114)
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

func gatewayScopePreference() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("CLAWGO_GATEWAY_SCOPE")))
	if v == "user" || v == "system" {
		return v
	}
	return ""
}

func detectGatewayServiceScopeAndPath() (string, string, error) {
	if runtime.GOOS != "linux" {
		return "", "", fmt.Errorf("gateway service registration currently supports Linux systemd only")
	}
	switch gatewayScopePreference() {
	case "user":
		return userGatewayUnitPath()
	case "system":
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

	preferredScope := gatewayScopePreference()
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
	agentLoop.SetConfigPath(getConfigPath())

	startupInfo := agentLoop.GetStartupInfo()
	toolsInfo := startupInfo["tools"].(map[string]interface{})
	skillsInfo := startupInfo["skills"].(map[string]interface{})
	fmt.Println("\n📦 Agent Status:")
	fmt.Printf("  • Tools: %d loaded\n", toolsInfo["count"])
	fmt.Printf("  • Skills: %d/%d available\n", skillsInfo["available"], skillsInfo["total"])

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
