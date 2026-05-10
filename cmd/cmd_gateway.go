package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/YspCoder/clawgo/pkg/api"
	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/cron"
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agentLoop, channelManager, err := buildGatewayRuntime(ctx, cfg, msgBus, cronService)
	if err != nil {
		fmt.Printf("Error initializing gateway runtime: %v\n", err)
		os.Exit(1)
	}

	state := &gatewayRuntimeState{
		cfg:              cfg,
		agentLoop:        agentLoop,
		channelManager:   channelManager,
		heartbeatService: buildHeartbeatService(cfg, msgBus),
		sentinelService: sentinel.NewService(
			getConfigPath(),
			cfg.WorkspacePath(),
			cfg.Sentinel.IntervalSec,
			cfg.Sentinel.AutoHeal,
			buildSentinelAlertHandler(cfg, msgBus),
		),
	}
	state.sentinelService.SetManager(state.channelManager)

	pidFile := filepath.Join(filepath.Dir(getConfigPath()), "gateway.pid")
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644); err != nil {
		fmt.Printf("Warning: failed to write PID file: %v\n", err)
	} else {
		defer os.Remove(pidFile)
	}

	enabledChannels := state.channelManager.GetEnabledChannels()
	if len(enabledChannels) > 0 {
		fmt.Printf("Channels enabled: %s\n", enabledChannels)
	} else {
		fmt.Println("Warning: no channels enabled")
	}

	fmt.Printf("Gateway started on %s:%d\n", state.cfg.Gateway.Host, state.cfg.Gateway.Port)
	fmt.Println("Press Ctrl+C to stop. Send SIGHUP to hot-reload config.")

	if err := cronService.Start(); err != nil {
		fmt.Printf("Error starting cron service: %v\n", err)
	}
	fmt.Println("Cron service started")

	if err := state.heartbeatService.Start(); err != nil {
		fmt.Printf("Error starting heartbeat service: %v\n", err)
	}
	fmt.Println("Heartbeat service started")
	if state.cfg.Sentinel.Enabled {
		state.sentinelService.Start()
		fmt.Println("Sentinel service started")
	}

	registryServer := api.NewServer(state.cfg.Gateway.Host, state.cfg.Gateway.Port, state.cfg.Gateway.Token)
	registryServer.SetGatewayVersion(version)
	registryServer.SetConfigPath(getConfigPath())
	registryServer.SetToken(state.cfg.Gateway.Token)
	registryServer.SetWorkspacePath(state.cfg.WorkspacePath())
	registryServer.SetLogFilePath(state.cfg.LogFilePath())

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

	bindAgentLoopHandlers(registryServer, state.agentLoop)
	triggerReload := newGatewayReloadTrigger(ctx, state, msgBus, cronService, registryServer)
	registryServer.SetConfigAfterHook(func(forceRuntimeReload bool) error {
		return triggerReload("api", forceRuntimeReload)
	})
	registryServer.SetMessageBus(msgBus)
	(&gatewayReloader{state: state, registryServer: registryServer}).bindWeixinChannel()
	bindCronHandler(registryServer, cronService)

	if err := registryServer.Start(ctx); err != nil {
		fmt.Printf("Error starting gateway server: %v\n", err)
	} else {
		fmt.Printf("Gateway server started on %s:%d\n", state.cfg.Gateway.Host, state.cfg.Gateway.Port)
	}

	if err := state.channelManager.StartAll(ctx); err != nil {
		fmt.Printf("Error starting channels: %v\n", err)
	}

	go state.agentLoop.Run(ctx)
	go runGatewayStartupCompactionCheck(ctx, state.agentLoop)
	go runGatewayBootstrapInit(ctx, state.cfg, state.agentLoop)

	stopConfigWatcher := startGatewayConfigWatcher(ctx, getConfigPath(), 500*time.Millisecond, 250*time.Millisecond, func() error {
		return triggerReload("watcher", false)
	})
	defer stopConfigWatcher()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, gatewayNotifySignals()...)

	for {
		select {
		case sig := <-sigChan:
			switch {
			case isGatewayReloadSignal(sig):
				err := triggerReload("signal", false)
				if err != nil {
					fmt.Printf("Reload failed: %v\n", err)
				}
			default:
				fmt.Println("\nShutting down...")
				cancel()
				state.heartbeatService.Stop()
				state.sentinelService.Stop()
				cronService.Stop()
				state.agentLoop.Stop()
				state.channelManager.StopAll(ctx)
				fmt.Println("Gateway stopped")
				return
			}
		}
	}
}
