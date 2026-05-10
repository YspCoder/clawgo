package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
)

func BootstrapDefaultTools(ctx context.Context, opts BootstrapOptions) (*BootstrapResult, error) {
	if opts.Config == nil {
		return nil, fmt.Errorf("config is required")
	}
	workspace := strings.TrimSpace(opts.Workspace)
	if workspace == "" {
		workspace = opts.Config.WorkspacePath()
	}
	if ctx == nil {
		ctx = context.Background()
	}

	registry := NewToolRegistry()
	processManager := opts.ProcessManager
	if processManager == nil {
		processManager = NewProcessManager(workspace)
	}

	registerFilesystemTools(registry, workspace)
	registerShellTools(registry, opts, workspace, processManager)
	registerCronTools(registry, opts)

	maxParallelCalls, parallelSafe := bootstrapParallelConfig(opts.Config)
	registerWebTools(registry, opts, maxParallelCalls, parallelSafe)
	registerMCPTools(ctx, registry, opts, workspace)
	registerMessageTool(registry, opts.MessageBus)

	subagentManager, subagentRouter := registerSubagentTools(registry, opts, workspace)
	registerSessionTools(registry, opts)
	registerMemoryTools(registry, workspace)

	registry.Register(NewParallelTool(registry, maxParallelCalls, parallelSafe))
	registry.Register(NewBrowserTool())
	registry.Register(NewCameraTool(workspace))
	registry.Register(NewSystemInfoTool())

	return &BootstrapResult{
		Registry:        registry,
		ProcessManager:  processManager,
		SubagentManager: subagentManager,
		SubagentRouter:  subagentRouter,
	}, nil
}

func registerFilesystemTools(registry *ToolRegistry, workspace string) {
	registry.Register(NewReadFileTool(workspace))
	registry.Register(NewWriteFileTool(workspace))
	registry.Register(NewListDirTool(workspace))
	registry.Register(NewSkillExecTool(workspace))
	registry.Register(NewEditFileTool(workspace))
}

func registerShellTools(registry *ToolRegistry, opts BootstrapOptions, workspace string, processManager *ProcessManager) {
	registry.Register(NewExecTool(opts.Config.Tools.Shell, workspace, processManager))
	registry.Register(NewProcessTool(processManager))
}

func registerCronTools(registry *ToolRegistry, opts BootstrapOptions) {
	if opts.CronService == nil {
		return
	}
	registry.Register(NewRemindTool(opts.CronService))
	registry.Register(NewCronTool(opts.CronService))
}

func bootstrapParallelConfig(cfg *config.Config) (int, map[string]struct{}) {
	maxParallelCalls := cfg.Agents.Defaults.Execution.ToolMaxParallelCalls
	if maxParallelCalls <= 0 {
		maxParallelCalls = 4
	}
	parallelSafe := make(map[string]struct{})
	for _, name := range cfg.Agents.Defaults.Execution.ToolParallelSafeNames {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			parallelSafe[trimmed] = struct{}{}
		}
	}
	return maxParallelCalls, parallelSafe
}

func registerWebTools(registry *ToolRegistry, opts BootstrapOptions, maxParallelCalls int, parallelSafe map[string]struct{}) {
	searchCfg := opts.Config.Tools.Web.Search
	registry.Register(NewWebSearchTool(searchCfg.APIKey, searchCfg.MaxResults))
	webFetchTool := NewWebFetchTool(50000)
	registry.Register(webFetchTool)
	registry.Register(NewParallelFetchTool(webFetchTool, maxParallelCalls, parallelSafe))
}

func registerMCPTools(ctx context.Context, registry *ToolRegistry, opts BootstrapOptions, workspace string) {
	mcpCfg := opts.Config.Tools.MCP
	if !mcpCfg.Enabled {
		return
	}
	mcpTool := NewMCPTool(workspace, mcpCfg)
	registry.Register(mcpTool)
	timeoutSec := mcpCfg.RequestTimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 20
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()
	for _, remoteTool := range mcpTool.DiscoverTools(discoveryCtx) {
		registry.Register(remoteTool)
	}
}

func registerMessageTool(registry *ToolRegistry, msgBus *bus.MessageBus) {
	messageTool := NewMessageTool()
	if msgBus != nil {
		messageTool.SetSendCallback(func(channel, chatID, action, content, media, messageID, emoji string, buttons [][]bus.Button) error {
			msgBus.PublishOutbound(bus.OutboundMessage{
				Channel:   channel,
				ChatID:    chatID,
				Content:   content,
				Media:     media,
				Buttons:   buttons,
				Action:    action,
				MessageID: messageID,
				Emoji:     emoji,
			})
			return nil
		})
	}
	registry.Register(messageTool)
}

func registerSubagentTools(registry *ToolRegistry, opts BootstrapOptions, workspace string) (*SubagentManager, *SubagentRouter) {
	subagentManager := NewSubagentManager(opts.Provider, workspace, opts.MessageBus)
	subagentRouter := NewSubagentRouter(subagentManager)
	registry.Register(NewSpawnTool(subagentManager))
	if store := subagentManager.ProfileStore(); store != nil {
		registry.Register(NewSubagentProfileTool(store))
	}
	return subagentManager, subagentRouter
}

func registerSessionTools(registry *ToolRegistry, opts BootstrapOptions) {
	registry.Register(NewSessionsTool(opts.SessionList, opts.SessionHistory))
}

func registerMemoryTools(registry *ToolRegistry, workspace string) {
	registry.Register(NewMemorySearchTool(workspace))
	registry.Register(NewMemoryGetTool(workspace))
	registry.Register(NewMemoryWriteTool(workspace))
}
