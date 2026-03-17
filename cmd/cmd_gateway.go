package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
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
	"github.com/YspCoder/clawgo/pkg/runtimecfg"
	"github.com/YspCoder/clawgo/pkg/sentinel"
	"github.com/YspCoder/clawgo/pkg/wsrelay"
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
		buildSentinelAlertHandler(cfg, msgBus),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if shouldEmbedWhatsAppBridge(cfg) {
		cfg.Channels.WhatsApp.BridgeURL = embeddedWhatsAppBridgeURL(cfg)
	}

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
		fmt.Printf("Channels enabled: %s\n", enabledChannels)
	} else {
		fmt.Println("Warning: no channels enabled")
	}

	fmt.Printf("Gateway started on %s:%d\n", cfg.Gateway.Host, cfg.Gateway.Port)
	fmt.Println("Press Ctrl+C to stop. Send SIGHUP to hot-reload config.")

	if err := cronService.Start(); err != nil {
		fmt.Printf("Error starting cron service: %v\n", err)
	}
	fmt.Println("Cron service started")

	if err := heartbeatService.Start(); err != nil {
		fmt.Printf("Error starting heartbeat service: %v\n", err)
	}
	fmt.Println("Heartbeat service started")
	if cfg.Sentinel.Enabled {
		sentinelService.Start()
		fmt.Println("Sentinel service started")
	}

	registryServer := api.NewServer(cfg.Gateway.Host, cfg.Gateway.Port, cfg.Gateway.Token)
	registryServer.SetGatewayVersion(version)
	registryServer.SetConfigPath(getConfigPath())
	registryServer.SetToken(cfg.Gateway.Token)
	registryServer.SetWorkspacePath(cfg.WorkspacePath())
	registryServer.SetLogFilePath(cfg.LogFilePath())
	aistudioRelay := wsrelay.NewManager(wsrelay.Options{
		Path: "/v1/ws",
		ProviderFactory: func(r *http.Request) (string, error) {
			provider := strings.TrimSpace(r.URL.Query().Get("provider"))
			if provider == "" {
				provider = strings.TrimSpace(r.Header.Get("X-Clawgo-Provider"))
			}
			if provider == "" {
				provider = "aistudio"
			}
			return strings.ToLower(provider), nil
		},
		OnConnected:    providers.NotifyAIStudioRelayConnected,
		OnDisconnected: providers.NotifyAIStudioRelayDisconnected,
	})
	defer func() { _ = aistudioRelay.Stop(context.Background()) }()
	providers.SetAIStudioRelayManager(aistudioRelay)
	registryServer.SetProtectedRoute(aistudioRelay.Path(), aistudioRelay.Handler())
	bindAgentLoopHandlers := func(loop *agent.AgentLoop) {
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
	bindAgentLoopHandlers(agentLoop)
	var reloadMu sync.Mutex
	var applyReload func() error
	registryServer.SetConfigAfterHook(func() error {
		reloadMu.Lock()
		defer reloadMu.Unlock()
		if applyReload == nil {
			return fmt.Errorf("reload handler not ready")
		}
		return applyReload()
	})
	whatsAppBridge, whatsAppEmbedded := setupEmbeddedWhatsAppBridge(ctx, cfg)
	if whatsAppBridge != nil {
		registryServer.SetWhatsAppBridge(whatsAppBridge, embeddedWhatsAppBridgeBasePath)
	}
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
		fmt.Printf("Node registry server started on %s:%d\n", cfg.Gateway.Host, cfg.Gateway.Port)
	}

	if err := channelManager.StartAll(ctx); err != nil {
		fmt.Printf("Error starting channels: %v\n", err)
	}

	go agentLoop.Run(ctx)
	go runGatewayStartupCompactionCheck(ctx, agentLoop)
	go runGatewayBootstrapInit(ctx, cfg, agentLoop)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, gatewayNotifySignals()...)
	applyReload = func() error {
		fmt.Println("\nReloading config...")
		newCfg, err := config.LoadConfig(getConfigPath())
		if err != nil {
			return fmt.Errorf("load config: %w", err)
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
			fmt.Println("Config unchanged, skip reload")
			return nil
		}

		if cfg.Gateway.Host != newCfg.Gateway.Host || cfg.Gateway.Port != newCfg.Gateway.Port {
			fmt.Printf("Warning: gateway host/port change detected (%s:%d -> %s:%d); restart required to rebind listener\n",
				cfg.Gateway.Host, cfg.Gateway.Port, newCfg.Gateway.Host, newCfg.Gateway.Port)
		}

		if shouldEmbedWhatsAppBridge(newCfg) {
			newCfg.Channels.WhatsApp.BridgeURL = embeddedWhatsAppBridgeURL(newCfg)
		}

		runtimeSame := reflect.DeepEqual(cfg.Agents, newCfg.Agents) &&
			reflect.DeepEqual(cfg.Models, newCfg.Models) &&
			reflect.DeepEqual(cfg.Tools, newCfg.Tools) &&
			reflect.DeepEqual(cfg.Channels, newCfg.Channels)

		if runtimeSame {
			configureLogging(newCfg)
			sentinelService.Stop()
			sentinelService = sentinel.NewService(
				getConfigPath(),
				newCfg.WorkspacePath(),
				newCfg.Sentinel.IntervalSec,
				newCfg.Sentinel.AutoHeal,
				buildSentinelAlertHandler(newCfg, msgBus),
			)
			if newCfg.Sentinel.Enabled {
				sentinelService.SetManager(channelManager)
				sentinelService.Start()
			}
			cfg = newCfg
			runtimecfg.Set(cfg)
			registryServer.SetToken(cfg.Gateway.Token)
			registryServer.SetWorkspacePath(cfg.WorkspacePath())
			registryServer.SetLogFilePath(cfg.LogFilePath())
			fmt.Println("Config hot-reload applied (logging/metadata only)")
			return nil
		}

		newAgentLoop, newChannelManager, err := buildGatewayRuntime(ctx, newCfg, msgBus, cronService)
		if err != nil {
			return fmt.Errorf("init runtime: %w", err)
		}

		channelManager.StopAll(ctx)
		agentLoop.Stop()
		if whatsAppBridge != nil {
			whatsAppBridge.Stop()
		}

		newWhatsAppBridge, _ := setupEmbeddedWhatsAppBridge(ctx, newCfg)

		channelManager = newChannelManager
		agentLoop = newAgentLoop
		cfg = newCfg
		whatsAppBridge = newWhatsAppBridge
		whatsAppEmbedded = newWhatsAppBridge != nil
		runtimecfg.Set(cfg)
		bindAgentLoopHandlers(agentLoop)
		configureLogging(newCfg)
		registryServer.SetToken(cfg.Gateway.Token)
		registryServer.SetWorkspacePath(cfg.WorkspacePath())
		registryServer.SetLogFilePath(cfg.LogFilePath())
		registryServer.SetWhatsAppBridge(whatsAppBridge, embeddedWhatsAppBridgeBasePath)
		sentinelService.Stop()
		sentinelService = sentinel.NewService(
			getConfigPath(),
			newCfg.WorkspacePath(),
			newCfg.Sentinel.IntervalSec,
			newCfg.Sentinel.AutoHeal,
			buildSentinelAlertHandler(newCfg, msgBus),
		)
		if newCfg.Sentinel.Enabled {
			sentinelService.Start()
		}
		sentinelService.SetManager(channelManager)

		if err := channelManager.StartAll(ctx); err != nil {
			return fmt.Errorf("start channels: %w", err)
		}
		go agentLoop.Run(ctx)
		fmt.Println("Config hot-reload applied")
		return nil
	}

	for {
		select {
		case sig := <-sigChan:
			switch {
			case isGatewayReloadSignal(sig):
				reloadMu.Lock()
				err := applyReload()
				reloadMu.Unlock()
				if err != nil {
					fmt.Printf("Reload failed: %v\n", err)
				}
			default:
				fmt.Println("\nShutting down...")
				cancel()
				if whatsAppEmbedded && whatsAppBridge != nil {
					whatsAppBridge.Stop()
				}
				heartbeatService.Stop()
				sentinelService.Stop()
				cronService.Stop()
				agentLoop.Stop()
				channelManager.StopAll(ctx)
				fmt.Println("Gateway stopped")
				return
			}
		}
	}
}

const embeddedWhatsAppBridgeBasePath = "/whatsapp"

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
	switch runtime.GOOS {
	case "darwin":
		return gatewayInstallLaunchdService()
	case "windows":
		return gatewayInstallWindowsTask()
	}
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

	fmt.Printf("Gateway service registered: %s (%s)\n", gatewayServiceName, scope)
	fmt.Printf("  Unit file: %s\n", unitPath)
	fmt.Println("  Start service:   clawgo gateway start")
	fmt.Println("  Restart service: clawgo gateway restart")
	fmt.Println("  Stop service:    clawgo gateway stop")
	return nil
}

func gatewayServiceControlCmd(action string) error {
	switch runtime.GOOS {
	case "darwin":
		return gatewayLaunchdControl(action)
	case "windows":
		return gatewayWindowsTaskControl(action)
	}
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
	switch runtime.GOOS {
	case "linux":
	default:
		return "", "", fmt.Errorf("unsupported service manager for %s", runtime.GOOS)
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
	switch runtime.GOOS {
	case "darwin":
		return detectInstalledLaunchdService()
	case "windows":
		return detectInstalledWindowsTask()
	}
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

func gatewayLaunchdLabel() string { return "ai.clawgo.gateway" }

func gatewayWindowsTaskName() string { return "ClawGo Gateway" }

func detectLaunchdScopeAndPath() (string, string, error) {
	label := gatewayLaunchdLabel() + ".plist"
	switch gatewayScopePreference() {
	case "system":
		return "system", filepath.Join("/Library/LaunchDaemons", label), nil
	case "user":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", fmt.Errorf("resolve user home failed: %w", err)
		}
		return "user", filepath.Join(home, "Library", "LaunchAgents", label), nil
	}
	if os.Geteuid() == 0 {
		return "system", filepath.Join("/Library/LaunchDaemons", label), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve user home failed: %w", err)
	}
	return "user", filepath.Join(home, "Library", "LaunchAgents", label), nil
}

func detectInstalledLaunchdService() (string, string, error) {
	userScope, userPath, err := detectLaunchdScopeAndPath()
	if err != nil && gatewayScopePreference() == "user" {
		return "", "", err
	}
	systemPath := filepath.Join("/Library/LaunchDaemons", gatewayLaunchdLabel()+".plist")
	systemExists := fileExists(systemPath)
	userExists := fileExists(userPath)

	switch gatewayScopePreference() {
	case "system":
		if systemExists {
			return "system", systemPath, nil
		}
		return "", "", fmt.Errorf("launchd plist not found in system scope: %s", systemPath)
	case "user":
		if userExists {
			return userScope, userPath, nil
		}
		return "", "", fmt.Errorf("launchd plist not found in user scope: %s", userPath)
	}

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

func gatewayInstallLaunchdService() error {
	scope, plistPath, err := detectLaunchdScopeAndPath()
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
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return fmt.Errorf("create launchd directory failed: %w", err)
	}
	content := buildGatewayLaunchdPlist(exePath, configPath, workDir)
	if err := os.WriteFile(plistPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write launchd plist failed: %w", err)
	}
	_ = runLaunchctl(scope, "bootout", launchdDomainTarget(scope), plistPath)
	if err := runLaunchctl(scope, "bootstrap", launchdDomainTarget(scope), plistPath); err != nil {
		return err
	}
	if err := runLaunchctl(scope, "kickstart", "-k", launchdServiceTarget(scope)); err != nil {
		return err
	}
	fmt.Printf("✓ Gateway service registered: %s (%s)\n", gatewayLaunchdLabel(), scope)
	fmt.Printf("  Launchd plist: %s\n", plistPath)
	fmt.Println("  Start service:   clawgo gateway start")
	fmt.Println("  Restart service: clawgo gateway restart")
	fmt.Println("  Stop service:    clawgo gateway stop")
	return nil
}

func gatewayLaunchdControl(action string) error {
	scope, plistPath, err := detectInstalledLaunchdService()
	if err != nil {
		return err
	}
	switch action {
	case "start":
		_ = runLaunchctl(scope, "bootstrap", launchdDomainTarget(scope), plistPath)
		return runLaunchctl(scope, "kickstart", "-k", launchdServiceTarget(scope))
	case "stop":
		return runLaunchctl(scope, "bootout", launchdDomainTarget(scope), plistPath)
	case "restart":
		_ = runLaunchctl(scope, "bootout", launchdDomainTarget(scope), plistPath)
		if err := runLaunchctl(scope, "bootstrap", launchdDomainTarget(scope), plistPath); err != nil {
			return err
		}
		return runLaunchctl(scope, "kickstart", "-k", launchdServiceTarget(scope))
	case "status":
		return runLaunchctl(scope, "print", launchdServiceTarget(scope))
	default:
		return fmt.Errorf("unsupported action: %s", action)
	}
}

func buildGatewayLaunchdPlist(exePath, configPath, workDir string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>gateway</string>
    <string>run</string>
    <string>--config</string>
    <string>%s</string>
  </array>
  <key>WorkingDirectory</key>
  <string>%s</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, gatewayLaunchdLabel(), exePath, configPath, workDir, filepath.Join(filepath.Dir(configPath), "gateway.launchd.out.log"), filepath.Join(filepath.Dir(configPath), "gateway.launchd.err.log"))
}

func launchdDomainTarget(scope string) string {
	if scope == "system" {
		return "system"
	}
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func launchdServiceTarget(scope string) string {
	return launchdDomainTarget(scope) + "/" + gatewayLaunchdLabel()
}

func runLaunchctl(scope string, args ...string) error {
	cmd := exec.Command("launchctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launchctl %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

func gatewayInstallWindowsTask() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path failed: %w", err)
	}
	exePath, _ = filepath.Abs(exePath)
	configPath := getConfigPath()
	taskName := gatewayWindowsTaskName()
	command := fmt.Sprintf(`"%s" gateway run --config "%s"`, exePath, configPath)
	_ = runSCHTASKS("/Delete", "/TN", taskName, "/F")
	if err := runSCHTASKS("/Create", "/TN", taskName, "/SC", "ONLOGON", "/TR", command, "/F"); err != nil {
		return err
	}
	fmt.Printf("✓ Gateway service registered: %s (windows task)\n", taskName)
	fmt.Println("  Start service:   clawgo gateway start")
	fmt.Println("  Restart service: clawgo gateway restart")
	fmt.Println("  Stop service:    clawgo gateway stop")
	return nil
}

func gatewayWindowsTaskControl(action string) error {
	_, _, err := detectInstalledWindowsTask()
	if err != nil {
		return err
	}
	taskName := gatewayWindowsTaskName()
	switch action {
	case "start":
		return runSCHTASKS("/Run", "/TN", taskName)
	case "stop":
		return stopGatewayProcessByPIDFile()
	case "restart":
		_ = stopGatewayProcessByPIDFile()
		return runSCHTASKS("/Run", "/TN", taskName)
	case "status":
		return runSCHTASKS("/Query", "/TN", taskName, "/V", "/FO", "LIST")
	default:
		return fmt.Errorf("unsupported action: %s", action)
	}
}

func detectInstalledWindowsTask() (string, string, error) {
	taskName := gatewayWindowsTaskName()
	if err := runSCHTASKSQuiet("/Query", "/TN", taskName); err != nil {
		return "", "", fmt.Errorf("gateway service not registered. Run: clawgo gateway")
	}
	return "user", taskName, nil
}

func runSCHTASKS(args ...string) error {
	cmd := exec.Command("schtasks", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("schtasks %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

func runSCHTASKSQuiet(args ...string) error {
	cmd := exec.Command("schtasks", args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func stopGatewayProcessByPIDFile() error {
	pidPath := filepath.Join(filepath.Dir(getConfigPath()), "gateway.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return fmt.Errorf("gateway pid file not found: %w", err)
	}
	pid := strings.TrimSpace(string(data))
	if pid == "" {
		return fmt.Errorf("gateway pid file is empty")
	}
	cmd := exec.Command("taskkill", "/PID", pid, "/T", "/F")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("taskkill /PID %s failed: %w", pid, err)
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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
	fmt.Printf("  鈥?Tools: %d loaded\n", toolsInfo["count"])
	fmt.Printf("  鈥?Skills: %d/%d available\n", skillsInfo["available"], skillsInfo["total"])

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

func setupEmbeddedWhatsAppBridge(ctx context.Context, cfg *config.Config) (*channels.WhatsAppBridgeService, bool) {
	if !shouldStartEmbeddedWhatsAppBridge(cfg) {
		return nil, false
	}
	cfg.Channels.WhatsApp.BridgeURL = embeddedWhatsAppBridgeURL(cfg)
	stateDir := filepath.Join(filepath.Dir(getConfigPath()), "channels", "whatsapp")
	svc := channels.NewWhatsAppBridgeService(fmt.Sprintf("%s:%d", cfg.Gateway.Host, cfg.Gateway.Port), stateDir, false)
	if err := svc.StartEmbedded(ctx); err != nil {
		fmt.Printf("Error starting embedded WhatsApp bridge: %v\n", err)
		return nil, false
	}
	return svc, true
}

func shouldStartEmbeddedWhatsAppBridge(cfg *config.Config) bool {
	return cfg != nil && shouldEmbedWhatsAppBridge(cfg)
}

func shouldEmbedWhatsAppBridge(cfg *config.Config) bool {
	raw := strings.TrimSpace(cfg.Channels.WhatsApp.BridgeURL)
	if raw == "" {
		return true
	}
	hostPort := comparableBridgeHostPort(raw)
	if hostPort == "" {
		return false
	}
	if hostPort == "127.0.0.1:3001" || hostPort == "localhost:3001" {
		return true
	}
	return hostPort == comparableGatewayHostPort(cfg.Gateway.Host, cfg.Gateway.Port)
}

func embeddedWhatsAppBridgeURL(cfg *config.Config) string {
	host := strings.TrimSpace(cfg.Gateway.Host)
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return fmt.Sprintf("ws://%s:%d%s/ws", host, cfg.Gateway.Port, embeddedWhatsAppBridgeBasePath)
}

func comparableBridgeHostPort(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		return strings.ToLower(raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(u.Host))
}

func comparableGatewayHostPort(host string, port int) string {
	host = strings.TrimSpace(strings.ToLower(host))
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%d", host, port)
}
