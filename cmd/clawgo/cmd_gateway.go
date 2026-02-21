package main

import (
	"bufio"
	"context"
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
	"clawgo/pkg/bus"
	"clawgo/pkg/channels"
	"clawgo/pkg/config"
	"clawgo/pkg/cron"
	"clawgo/pkg/heartbeat"
	"clawgo/pkg/logger"
	"clawgo/pkg/providers"
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
	if strings.EqualFold(strings.TrimSpace(os.Getenv(envRootGranted)), "1") || strings.EqualFold(strings.TrimSpace(os.Getenv(envRootGranted)), "true") {
		applyMaximumPermissionPolicy(cfg)
	}

	msgBus := bus.NewMessageBus()
	cronStorePath := filepath.Join(filepath.Dir(getConfigPath()), "cron", "jobs.json")
	cronService := cron.NewCronService(cronStorePath, nil)
	configureCronServiceRuntime(cronService, cfg)
	heartbeatService := heartbeat.NewHeartbeatService(cfg.WorkspacePath(), nil, 30*60, true)
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

	go agentLoop.Run(ctx)
	go runGatewayStartupCompactionCheck(ctx, agentLoop)

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

			if reflect.DeepEqual(cfg, newCfg) {
				fmt.Println("✓ Config unchanged, skip reload")
				continue
			}

			runtimeSame := reflect.DeepEqual(cfg.Agents, newCfg.Agents) &&
				reflect.DeepEqual(cfg.Providers, newCfg.Providers) &&
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
			fmt.Println("✓ Config hot-reload applied")
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
	if info, err := os.Stat(systemPath); err == nil && !info.IsDir() {
		return "system", systemPath, nil
	}

	scope, userPath, err := userGatewayUnitPath()
	if err != nil {
		return "", "", err
	}
	if info, err := os.Stat(userPath); err == nil && !info.IsDir() {
		return scope, userPath, nil
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
