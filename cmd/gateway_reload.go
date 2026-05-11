package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/YspCoder/clawgo/pkg/api"
	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/channels"
	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/cron"
	"github.com/YspCoder/clawgo/pkg/runtimecfg"
	"github.com/YspCoder/clawgo/pkg/sentinel"
)

type gatewayReloader struct {
	mu             sync.Mutex
	ctx            context.Context
	state          *gatewayRuntimeState
	msgBus         *bus.MessageBus
	cronService    *cron.CronService
	registryServer *api.Server
}

func newGatewayReloadTrigger(ctx context.Context, state *gatewayRuntimeState, msgBus *bus.MessageBus, cronService *cron.CronService, registryServer *api.Server) func(string, bool) error {
	reloader := &gatewayReloader{
		ctx:            ctx,
		state:          state,
		msgBus:         msgBus,
		cronService:    cronService,
		registryServer: registryServer,
	}
	return reloader.trigger
}

func (r *gatewayReloader) trigger(source string, forceRuntimeReload bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Printf("\nReloading config (source=%s)...\n", strings.TrimSpace(source))
	newCfg, err := config.LoadConfig(getConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv(envRootGranted)), "1") || strings.EqualFold(strings.TrimSpace(os.Getenv(envRootGranted)), "true") {
		applyMaximumPermissionPolicy(newCfg)
	}
	configureCronServiceRuntime(r.cronService, newCfg)
	r.state.heartbeatService.Stop()
	r.state.heartbeatService = buildHeartbeatService(newCfg, r.msgBus)
	if err := r.state.heartbeatService.Start(); err != nil {
		fmt.Printf("Error starting heartbeat service: %v\n", err)
	}

	if !forceRuntimeReload && reflect.DeepEqual(r.state.cfg, newCfg) {
		fmt.Println("Config unchanged, skip reload")
		return nil
	}

	if r.state.cfg.Gateway.Host != newCfg.Gateway.Host || r.state.cfg.Gateway.Port != newCfg.Gateway.Port {
		fmt.Printf("Warning: gateway host/port change detected (%s:%d -> %s:%d); restart required to rebind listener\n",
			r.state.cfg.Gateway.Host, r.state.cfg.Gateway.Port, newCfg.Gateway.Host, newCfg.Gateway.Port)
	}

	currentChannels := normalizeHotReloadChannelsConfig(r.state.cfg.Channels)
	nextChannels := normalizeHotReloadChannelsConfig(newCfg.Channels)
	runtimeSame := reflect.DeepEqual(r.state.cfg.Agents, newCfg.Agents) &&
		reflect.DeepEqual(r.state.cfg.Models, newCfg.Models) &&
		reflect.DeepEqual(r.state.cfg.Tools, newCfg.Tools) &&
		reflect.DeepEqual(currentChannels, nextChannels)

	if runtimeSame && !forceRuntimeReload {
		configureLogging(newCfg)
		r.state.sentinelService.Stop()
		r.state.sentinelService = sentinel.NewService(
			getConfigPath(),
			newCfg.WorkspacePath(),
			newCfg.Sentinel.IntervalSec,
			newCfg.Sentinel.AutoHeal,
			buildSentinelAlertHandler(newCfg, r.msgBus),
		)
		if newCfg.Sentinel.Enabled {
			r.state.sentinelService.SetManager(r.state.channelManager)
			r.state.sentinelService.Start()
		}
		r.state.cfg = newCfg
		runtimecfg.Set(r.state.cfg)
		r.bindRegistryMetadata()
		fmt.Println("Config hot-reload applied (logging/metadata only)")
		return nil
	}

	newAgentLoop, newChannelManager, err := buildGatewayRuntime(r.ctx, newCfg, r.msgBus, r.cronService)
	if err != nil {
		return fmt.Errorf("init runtime: %w", err)
	}

	r.state.channelManager.StopAll(r.ctx)
	r.state.agentLoop.Stop()
	r.state.channelManager = newChannelManager
	r.state.agentLoop = newAgentLoop
	r.state.cfg = newCfg
	runtimecfg.Set(r.state.cfg)
	bindAgentLoopHandlers(r.registryServer, r.state.agentLoop)
	configureLogging(newCfg)
	r.bindRegistryMetadata()
	r.bindWeixinChannel()
	r.state.sentinelService.Stop()
	r.state.sentinelService = sentinel.NewService(
		getConfigPath(),
		newCfg.WorkspacePath(),
		newCfg.Sentinel.IntervalSec,
		newCfg.Sentinel.AutoHeal,
		buildSentinelAlertHandler(newCfg, r.msgBus),
	)
	if newCfg.Sentinel.Enabled {
		r.state.sentinelService.Start()
	}
	r.state.sentinelService.SetManager(r.state.channelManager)

	if err := r.state.channelManager.StartAll(r.ctx); err != nil {
		return fmt.Errorf("start channels: %w", err)
	}
	go r.state.agentLoop.Run(r.ctx)
	fmt.Println("Config hot-reload applied")
	return nil
}

func (r *gatewayReloader) bindRegistryMetadata() {
	r.registryServer.SetToken(r.state.cfg.Gateway.Token)
	r.registryServer.SetWorkspacePath(r.state.cfg.WorkspacePath())
	r.registryServer.SetLogFilePath(r.state.cfg.LogFilePath())
}

func (r *gatewayReloader) bindWeixinChannel() {
	if rawWeixin, ok := r.state.channelManager.GetChannel("weixin"); ok {
		if weixinChannel, ok := rawWeixin.(*channels.WeixinChannel); ok {
			weixinChannel.SetConfigPath(getConfigPath())
			r.registryServer.SetWeixinChannel(weixinChannel)
		}
	} else {
		r.registryServer.SetWeixinChannel(nil)
	}
}

func normalizeHotReloadChannelsConfig(cfg config.ChannelsConfig) config.ChannelsConfig {
	cfg.Weixin.ContextToken = ""
	cfg.Weixin.GetUpdatesBuf = ""
	for i := range cfg.Weixin.Accounts {
		cfg.Weixin.Accounts[i].ContextToken = ""
		cfg.Weixin.Accounts[i].GetUpdatesBuf = ""
	}
	return cfg
}

type configFileFingerprint struct {
	Size        int64
	ModUnixNano int64
	SHA256      [32]byte
}

func readConfigFileFingerprint(path string) (configFileFingerprint, error) {
	info, err := os.Stat(path)
	if err != nil {
		return configFileFingerprint{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return configFileFingerprint{}, err
	}
	return configFileFingerprint{
		Size:        info.Size(),
		ModUnixNano: info.ModTime().UnixNano(),
		SHA256:      sha256.Sum256(content),
	}, nil
}

func (f configFileFingerprint) sameContent(other configFileFingerprint) bool {
	return f.Size == other.Size && f.SHA256 == other.SHA256
}

func startGatewayConfigWatcher(ctx context.Context, configPath string, debounce, pollInterval time.Duration, onContentChanged func() error) func() {
	if debounce <= 0 {
		debounce = 500 * time.Millisecond
	}
	if pollInterval <= 0 {
		pollInterval = 250 * time.Millisecond
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		last, err := readConfigFileFingerprint(configPath)
		haveLast := err == nil
		pending := false
		lastDetectedAt := time.Time{}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current, err := readConfigFileFingerprint(configPath)
				if err != nil {
					continue
				}
				if !haveLast {
					last = current
					haveLast = true
					continue
				}
				if !current.sameContent(last) {
					last = current
					pending = true
					lastDetectedAt = time.Now()
					continue
				}
				if pending && !lastDetectedAt.IsZero() && time.Since(lastDetectedAt) >= debounce {
					pending = false
					if onContentChanged != nil {
						if err := onContentChanged(); err != nil {
							fmt.Printf("Config watcher reload failed: %v\n", err)
						}
					}
				}
			}
		}
	}()
	return func() {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
}
