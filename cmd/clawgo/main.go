// ClawGo - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 ClawGo contributors

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"clawgo/pkg/configops"
	"clawgo/pkg/cron"
	"clawgo/pkg/heartbeat"
	"clawgo/pkg/logger"
	"clawgo/pkg/providers"
	"clawgo/pkg/sentinel"
	"clawgo/pkg/skills"
	"clawgo/pkg/voice"

	"github.com/chzyer/readline"
)

const version = "0.1.0"
const logo = "🦞"
const gatewayServiceName = "clawgo-gateway.service"
const envRootPrompted = "CLAWGO_ROOT_PROMPTED"
const envRootGranted = "CLAWGO_ROOT_GRANTED"

var globalConfigPathOverride string

var errGatewayNotRunning = errors.New("gateway not running")

func copyDirectory(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

func main() {
	globalConfigPathOverride = detectConfigPathFromArgs(os.Args)

	// Detect debug mode early
	for _, arg := range os.Args {
		if arg == "--debug" || arg == "-d" {
			config.SetDebugMode(true)
			logger.SetLevel(logger.DEBUG)
			break
		}
	}

	// Normalize global flags so command can appear after --config/--debug.
	os.Args = normalizeCLIArgs(os.Args)

	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	command := os.Args[1]
	// Remove --debug/-d from args for command handling if it's there?
	// Actually command handling already ignores them or handles them.
	// But onboard/login need to know config path.

	switch command {
	case "onboard":
		onboard()
	case "agent":
		agentCmd()
	case "gateway":
		if shouldPromptGatewayRoot(os.Args) {
			maybePromptAndEscalateRoot("gateway")
		}
		gatewayCmd()
	case "status":
		statusCmd()
	case "config":
		configCmd()
	case "cron":
		cronCmd()
	case "login":
		loginCmd()
	case "channel":
		channelCmd()
	case "skills":
		if len(os.Args) < 3 {
			skillsHelp()
			return
		}

		subcommand := os.Args[2]

		cfg, err := loadConfig()
		if err != nil {
			fmt.Printf("Error loading config: %v\n", err)
			os.Exit(1)
		}

		workspace := cfg.WorkspacePath()
		installer := skills.NewSkillInstaller(workspace)
		// 获取全局配置目录和内置 skills 目录
		globalDir := filepath.Dir(getConfigPath())
		globalSkillsDir := filepath.Join(globalDir, "skills")
		builtinSkillsDir := filepath.Join(globalDir, "clawgo", "skills")
		skillsLoader := skills.NewSkillsLoader(workspace, globalSkillsDir, builtinSkillsDir)

		switch subcommand {
		case "list":
			skillsListCmd(skillsLoader)
		case "install":
			skillsInstallCmd(installer)
		case "remove", "uninstall":
			if len(os.Args) < 4 {
				fmt.Println("Usage: clawgo skills remove <skill-name>")
				return
			}
			skillsRemoveCmd(installer, os.Args[3])
		case "install-builtin":
			skillsInstallBuiltinCmd(workspace)
		case "list-builtin":
			skillsListBuiltinCmd()
		case "search":
			skillsSearchCmd(installer)
		case "show":
			if len(os.Args) < 4 {
				fmt.Println("Usage: clawgo skills show <skill-name>")
				return
			}
			skillsShowCmd(skillsLoader, os.Args[3])
		default:
			fmt.Printf("Unknown skills command: %s\n", subcommand)
			skillsHelp()
		}
	case "version", "--version", "-v":
		fmt.Printf("%s clawgo v%s\n", logo, version)
	case "uninstall":
		uninstallCmd()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printHelp()
		os.Exit(1)
	}
}

func normalizeCLIArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}

	normalized := []string{args[0]}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--debug" || arg == "-d" {
			continue
		}
		if arg == "--config" {
			if i+1 < len(args) {
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "--config=") {
			continue
		}
		normalized = append(normalized, arg)
	}
	return normalized
}

func detectConfigPathFromArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--config" && i+1 < len(args) {
			return strings.TrimSpace(args[i+1])
		}
		if strings.HasPrefix(arg, "--config=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, "--config="))
		}
	}
	return ""
}

func printHelp() {
	fmt.Printf("%s clawgo - Personal AI Assistant v%s\n\n", logo, version)
	fmt.Println("Usage: clawgo <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  onboard     Initialize clawgo configuration and workspace")
	fmt.Println("  agent       Interact with the agent directly")
	fmt.Println("  gateway     Register/manage gateway service")
	fmt.Println("  status      Show clawgo status")
	fmt.Println("  config      Get/set config values")
	fmt.Println("  cron        Manage scheduled tasks")
	fmt.Println("  login       Configure CLIProxyAPI upstream")
	fmt.Println("  channel     Test and manage messaging channels")
	fmt.Println("  skills      Manage skills (install, list, remove)")
	fmt.Println("  uninstall   Uninstall clawgo components")
	fmt.Println("  version     Show version information")
	fmt.Println()
	fmt.Println("Global options:")
	fmt.Println("  --config <path>         Use custom config file")
	fmt.Println("  --debug, -d             Enable debug logging")
	fmt.Println()
	fmt.Println("Gateway service:")
	fmt.Println("  clawgo gateway                  # register service")
	fmt.Println("  clawgo gateway start|stop|restart|status")
	fmt.Println("  clawgo gateway run              # run foreground")
	fmt.Println()
	fmt.Println("Uninstall:")
	fmt.Println("  clawgo uninstall                # remove gateway service")
	fmt.Println("  clawgo uninstall --purge        # also remove config/workspace dir")
	fmt.Println("  clawgo uninstall --remove-bin   # also remove current executable")
}

func onboard() {
	configPath := getConfigPath()

	cfg := config.DefaultConfig()
	if strings.EqualFold(strings.TrimSpace(os.Getenv(envRootGranted)), "1") || strings.EqualFold(strings.TrimSpace(os.Getenv(envRootGranted)), "true") {
		applyMaximumPermissionPolicy(cfg)
	}
	configStatus, err := ensureConfigOnboard(configPath, cfg)
	if err != nil {
		fmt.Printf("Error preparing config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Config: %s (%s)\n", configPath, configStatus)

	workspace := cfg.WorkspacePath()
	if err := os.MkdirAll(workspace, 0755); err != nil {
		fmt.Printf("Error creating workspace: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0755); err != nil {
		fmt.Printf("Error creating memory directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "skills"), 0755); err != nil {
		fmt.Printf("Error creating skills directory: %v\n", err)
		os.Exit(1)
	}

	if err := createWorkspaceTemplates(workspace); err != nil {
		fmt.Printf("Error creating workspace templates: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s clawgo is ready!\n", logo)
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Configure CLIProxyAPI at", configPath)
	fmt.Println("     Ensure CLIProxyAPI is running: https://github.com/router-for-me/CLIProxyAPI")
	fmt.Println("  2. Chat: clawgo agent -m \"Hello!\"")
}

func ensureConfigOnboard(configPath string, defaults *config.Config) (string, error) {
	if defaults == nil {
		return "", fmt.Errorf("defaults is nil")
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := config.SaveConfig(configPath, defaults); err != nil {
			return "", err
		}
		return "created", nil
	} else if err != nil {
		return "", err
	}

	defaultData, err := json.Marshal(defaults)
	if err != nil {
		return "", err
	}
	var defaultMap map[string]interface{}
	if err := json.Unmarshal(defaultData, &defaultMap); err != nil {
		return "", err
	}

	existingMap, err := configops.LoadConfigAsMap(configPath)
	if err != nil {
		return "", err
	}

	changed := mergeMissingConfigValues(existingMap, defaultMap)
	if !changed {
		return "up-to-date", nil
	}

	mergedData, err := json.MarshalIndent(existingMap, "", "  ")
	if err != nil {
		return "", err
	}
	if _, err := configops.WriteConfigAtomicWithBackup(configPath, mergedData); err != nil {
		return "", err
	}
	return "updated (incremental)", nil
}

func mergeMissingConfigValues(dst map[string]interface{}, defaults map[string]interface{}) bool {
	changed := false
	for key, dv := range defaults {
		existing, ok := dst[key]
		if !ok {
			dst[key] = dv
			changed = true
			continue
		}

		dm, dIsMap := dv.(map[string]interface{})
		em, eIsMap := existing.(map[string]interface{})
		if dIsMap && eIsMap {
			if mergeMissingConfigValues(em, dm) {
				changed = true
			}
		}
	}
	return changed
}

func createWorkspaceTemplates(workspace string) error {
	managedFallbacks := map[string]string{
		"AGENTS.md": `# Autonomy Intent Policy

For autonomy-mode control messages, use semantic understanding first.

## Intent Parsing Priority
1. LLM semantic intent parsing (multi-language).
2. Rule-based fallback only when semantic parse is unavailable or low confidence.
`,
		"SOUL.md": `# Agent Core Behavior

The agent should behave as an autonomous collaborator, not a command-only bot.
`,
		"USER.md": `# User Preferences

- Prefer natural-language interaction over strict command syntax.
`,
	}

	templates := map[string]string{
		"IDENTITY.md": `# Identity

## Name
ClawGo 🦞

## Description
Ultra-lightweight personal AI assistant written in Go, inspired by nanobot.

## Version
0.1.0

## Purpose
- Provide intelligent AI assistance with minimal resource usage
- Support multiple LLM providers (OpenAI, Anthropic, Zhipu, etc.)
- Enable easy customization through skills system
- Run on minimal hardware ($10 boards, <10MB RAM)

## Capabilities

- Web search and content fetching
- File system operations (read, write, edit)
- Shell command execution
- Multi-channel messaging (Telegram, WhatsApp, Feishu)
- Skill-based extensibility
- Memory and context management

## Philosophy

- Simplicity over complexity
- Performance over features
- User control and privacy
- Transparent operation
- Community-driven development

## Goals

- Provide a fast, lightweight AI assistant
- Support offline-first operation where possible
- Enable easy customization and extension
- Maintain high quality responses
- Run efficiently on constrained hardware

## License
MIT License - Free and open source

## Repository
https://github.com/YspCoder/clawgo

## Contact
Issues: https://github.com/YspCoder/clawgo/issues
Discussions: https://github.com/YspCoder/clawgo/discussions

---

"Every bit helps, every bit matters."
- Clawgo
`,
	}

	managedDocs := []string{"AGENTS.md", "SOUL.md", "USER.md"}
	for _, filename := range managedDocs {
		filePath := filepath.Join(workspace, filename)
		_, statErr := os.Stat(filePath)
		exists := statErr == nil

		content, err := loadManagedDocTemplate(filename)
		if err != nil {
			if exists {
				fmt.Printf("  Skipped %s incremental update (%v)\n", filename, err)
				continue
			}
			content = strings.TrimSpace(managedFallbacks[filename])
			if content == "" {
				fmt.Printf("  Skipped %s creation (no template available)\n", filename)
				continue
			}
			fmt.Printf("  Created %s from builtin fallback\n", filename)
		}

		if err := upsertManagedBlock(filePath, filename, content); err != nil {
			return fmt.Errorf("failed to update %s incrementally: %w", filename, err)
		}
		if exists {
			fmt.Printf("  Synced %s (incremental)\n", filename)
		} else {
			fmt.Printf("  Created %s\n", filename)
		}
	}

	for filename, content := range templates {
		if filename == "AGENTS.md" || filename == "SOUL.md" || filename == "USER.md" {
			continue
		}
		filePath := filepath.Join(workspace, filename)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", filename, err)
			}
			fmt.Printf("  Created %s\n", filename)
		}
	}

	memoryDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		return fmt.Errorf("failed to create memory directory: %w", err)
	}
	memoryFile := filepath.Join(memoryDir, "MEMORY.md")
	if _, err := os.Stat(memoryFile); os.IsNotExist(err) {
		memoryContent := `# Long-term Memory

This file stores important information that should persist across sessions.

## User Information

(Important facts about user)

## Preferences

(User preferences learned over time)

## Important Notes

(Things to remember)

## Configuration

- Model preferences
- Channel settings
- Skills enabled
`
		if err := os.WriteFile(memoryFile, []byte(memoryContent), 0644); err != nil {
			return fmt.Errorf("failed to write memory file: %w", err)
		}
		fmt.Println("  Created memory/MEMORY.md")

		skillsDir := filepath.Join(workspace, "skills")
		if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
			if err := os.MkdirAll(skillsDir, 0755); err != nil {
				return fmt.Errorf("failed to create skills directory: %w", err)
			}
			fmt.Println("  Created skills/")
		}
	}
	return nil
}

func upsertManagedBlock(filePath, blockName, managedContent string) error {
	begin := fmt.Sprintf("# >>> CLAWGO MANAGED BLOCK: %s >>>", blockName)
	end := fmt.Sprintf("# <<< CLAWGO MANAGED BLOCK: %s <<<", blockName)
	block := fmt.Sprintf("%s\n%s\n%s\n", begin, strings.TrimSpace(managedContent), end)

	existing, err := os.ReadFile(filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return os.WriteFile(filePath, []byte(block), 0644)
	}

	text := string(existing)
	beginIdx := strings.Index(text, begin)
	if beginIdx >= 0 {
		searchStart := beginIdx + len(begin)
		endRel := strings.Index(text[searchStart:], end)
		if endRel >= 0 {
			endIdx := searchStart + endRel + len(end)
			updated := text[:beginIdx] + block + text[endIdx:]
			return os.WriteFile(filePath, []byte(updated), 0644)
		}
	}

	sep := "\n"
	if strings.TrimSpace(text) != "" {
		sep = "\n\n"
	}
	updated := text + sep + block
	return os.WriteFile(filePath, []byte(updated), 0644)
}

func loadManagedDocTemplate(filename string) (string, error) {
	candidates := []string{
		filepath.Join(".", filename),
		filepath.Join(filepath.Dir(getConfigPath()), "clawgo", filename),
	}

	if exePath, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exePath), filename))
	}

	seen := map[string]bool{}
	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err == nil {
			candidate = abs
		}
		if seen[candidate] {
			continue
		}
		seen[candidate] = true

		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		return content, nil
	}

	return "", fmt.Errorf("source template not found")
}

func agentCmd() {
	message := ""
	sessionKey := "cli:default"

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--debug", "-d":
			logger.SetLevel(logger.DEBUG)
			fmt.Println("🔍 Debug mode enabled")
		case "-m", "--message":
			if i+1 < len(args) {
				message = args[i+1]
				i++
			}
		case "-s", "--session":
			if i+1 < len(args) {
				sessionKey = args[i+1]
				i++
			}
		}
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	provider, err := providers.CreateProvider(cfg)
	if err != nil {
		fmt.Printf("Error creating provider: %v\n", err)
		os.Exit(1)
	}

	msgBus := bus.NewMessageBus()

	// Initialize CronService for tools (shared storage with gateway)
	cronStorePath := filepath.Join(filepath.Dir(getConfigPath()), "cron", "jobs.json")
	cronService := cron.NewCronService(cronStorePath, nil)
	configureCronServiceRuntime(cronService, cfg)

	agentLoop := agent.NewAgentLoop(cfg, msgBus, provider, cronService)

	// Print agent startup info (only for interactive mode)
	startupInfo := agentLoop.GetStartupInfo()
	logger.InfoCF("agent", "Agent initialized",
		map[string]interface{}{
			"tools_count":      startupInfo["tools"].(map[string]interface{})["count"],
			"skills_total":     startupInfo["skills"].(map[string]interface{})["total"],
			"skills_available": startupInfo["skills"].(map[string]interface{})["available"],
		})

	if message != "" {
		ctx := context.Background()
		response, err := agentLoop.ProcessDirect(ctx, message, sessionKey)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n%s %s\n", logo, response)
	} else {
		fmt.Printf("%s Interactive mode (Ctrl+C to exit)\n\n", logo)
		interactiveMode(agentLoop, sessionKey)
	}
}

func interactiveMode(agentLoop *agent.AgentLoop, sessionKey string) {
	prompt := fmt.Sprintf("%s You: ", logo)

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          prompt,
		HistoryFile:     filepath.Join(os.TempDir(), ".clawgo_history"),
		HistoryLimit:    100,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})

	if err != nil {
		fmt.Printf("Error initializing readline: %v\n", err)
		fmt.Println("Falling back to simple input mode...")
		simpleInteractiveMode(agentLoop, sessionKey)
		return
	}
	defer rl.Close()

	for {
		line, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt || err == io.EOF {
				fmt.Println("\nGoodbye!")
				return
			}
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		if input == "exit" || input == "quit" {
			fmt.Println("Goodbye!")
			return
		}

		ctx := context.Background()
		response, err := agentLoop.ProcessDirect(ctx, input, sessionKey)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		fmt.Printf("\n%s %s\n\n", logo, response)
	}
}

func simpleInteractiveMode(agentLoop *agent.AgentLoop, sessionKey string) {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(fmt.Sprintf("%s You: ", logo))
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Println("\nGoodbye!")
				return
			}
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		if input == "exit" || input == "quit" {
			fmt.Println("Goodbye!")
			return
		}

		ctx := context.Background()
		response, err := agentLoop.ProcessDirect(ctx, input, sessionKey)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		fmt.Printf("\n%s %s\n\n", logo, response)
	}
}

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
		// continue to foreground runtime below
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
	heartbeatService := heartbeat.NewHeartbeatService(
		cfg.WorkspacePath(),
		nil,
		30*60,
		true,
	)
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
	go runGatewayStartupSelfCheck(ctx, agentLoop, cfg.WorkspacePath())

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

func runGatewayStartupSelfCheck(parent context.Context, agentLoop *agent.AgentLoop, workspace string) {
	if agentLoop == nil {
		return
	}

	checkCtx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()

	prompt := buildGatewayStartupSelfCheckPrompt(workspace)
	report := agentLoop.RunStartupSelfCheckAllSessions(checkCtx, prompt, "gateway:startup-self-check")
	logger.InfoCF("gateway", "Startup self-check completed", map[string]interface{}{
		"sessions_total":     report.TotalSessions,
		"sessions_compacted": report.CompactedSessions,
		"sessions_checked":   report.CheckedSessions,
		"sessions_failed":    report.FailedSessions,
	})
}

func buildGatewayStartupSelfCheckPrompt(workspace string) string {
	now := time.Now().Format(time.RFC3339)
	notesPath := filepath.Join(workspace, "memory", "HEARTBEAT.md")
	notes := ""
	if data, err := os.ReadFile(notesPath); err == nil {
		notes = strings.TrimSpace(string(data))
	}

	var sb strings.Builder
	sb.WriteString("网关刚刚启动，请立即执行一次自检。\n")
	sb.WriteString("目标：基于你自己的历史记录与记忆，判断是否有未完成任务需要继续执行，或是否需要立即采取其他行动。\n")
	sb.WriteString("要求：\n")
	sb.WriteString("1) 先给出结论（继续执行 / 暂无待续任务 / 其他行动）。\n")
	sb.WriteString("2) 如果需要继续，请直接开始推进，并在关键节点自然汇报。\n")
	sb.WriteString("3) 如果无需继续，也请给出下一步建议。\n")
	sb.WriteString("4) 将本次结论简要写入 memory/MEMORY.md 便于下次启动继承。\n")
	sb.WriteString("\n")
	sb.WriteString("当前时间: ")
	sb.WriteString(now)
	if notes != "" {
		sb.WriteString("\n\n参考 HEARTBEAT.md:\n")
		sb.WriteString(notes)
	}
	return sb.String()
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
	cmd.Env = append(os.Environ(),
		envRootPrompted+"=1",
		envRootGranted+"=1",
	)

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
	// Only prompt on plain `clawgo gateway` registration flow.
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
	cfg.Tools.Shell.RestrictPath = false
	cfg.Tools.Shell.DeniedCmds = []string{"rm -rf /"}
	cfg.Tools.Shell.Risk.Enabled = false
	cfg.Tools.Shell.Risk.AllowDestructive = true
	cfg.Tools.Shell.Risk.RequireDryRun = false
	cfg.Tools.Shell.Risk.RequireForceFlag = false
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
	// Linux-only systemd integration
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
	fmt.Printf("  • Skills: %d/%d available\n",
		skillsInfo["available"],
		skillsInfo["total"])

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

	var transcriber *voice.GroqTranscriber
	if cfg.Providers.Proxy.APIKey != "" && strings.Contains(cfg.Providers.Proxy.APIBase, "groq.com") {
		transcriber = voice.NewGroqTranscriber(cfg.Providers.Proxy.APIKey)
		logger.InfoC("voice", "Groq voice transcription enabled via Proxy config")
	}

	if transcriber != nil {
		if telegramChannel, ok := channelManager.GetChannel("telegram"); ok {
			if tc, ok := telegramChannel.(*channels.TelegramChannel); ok {
				tc.SetTranscriber(transcriber)
				logger.InfoC("voice", "Groq transcription attached to Telegram channel")
			}
		}
		if discordChannel, ok := channelManager.GetChannel("discord"); ok {
			if dc, ok := discordChannel.(*channels.DiscordChannel); ok {
				dc.SetTranscriber(transcriber)
				logger.InfoC("voice", "Groq transcription attached to Discord channel")
			}
		}
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

func configCmd() {
	if len(os.Args) < 3 {
		configHelp()
		return
	}

	switch os.Args[2] {
	case "set":
		configSetCmd()
	case "get":
		configGetCmd()
	case "check":
		configCheckCmd()
	case "reload":
		configReloadCmd()
	default:
		fmt.Printf("Unknown config command: %s\n", os.Args[2])
		configHelp()
	}
}

func configHelp() {
	fmt.Println("\nConfig commands:")
	fmt.Println("  set <path> <value>     Set config value and trigger hot reload")
	fmt.Println("  get <path>             Get config value")
	fmt.Println("  check                  Validate current config")
	fmt.Println("  reload                 Trigger gateway hot reload")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  clawgo config set channels.telegram.enabled true")
	fmt.Println("  clawgo config set channels.telegram.enable true")
	fmt.Println("  clawgo config get providers.proxy.api_base")
	fmt.Println("  clawgo config check")
	fmt.Println("  clawgo config reload")
}

func configSetCmd() {
	if len(os.Args) < 5 {
		fmt.Println("Usage: clawgo config set <path> <value>")
		return
	}

	configPath := getConfigPath()
	cfgMap, err := loadConfigAsMap(configPath)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	path := normalizeConfigPath(os.Args[3])
	args := os.Args[4:]
	valueParts := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		part := args[i]
		if part == "--debug" || part == "-d" {
			continue
		}
		if part == "--config" {
			i++
			continue
		}
		if strings.HasPrefix(part, "--config=") {
			continue
		}
		valueParts = append(valueParts, part)
	}
	if len(valueParts) == 0 {
		fmt.Println("Error: value is required")
		return
	}
	value := parseConfigValue(strings.Join(valueParts, " "))
	if err := setMapValueByPath(cfgMap, path, value); err != nil {
		fmt.Printf("Error setting value: %v\n", err)
		return
	}

	data, err := json.MarshalIndent(cfgMap, "", "  ")
	if err != nil {
		fmt.Printf("Error serializing config: %v\n", err)
		return
	}
	backupPath, err := writeConfigAtomicWithBackup(configPath, data)
	if err != nil {
		fmt.Printf("Error writing config: %v\n", err)
		return
	}

	fmt.Printf("✓ Updated %s = %v\n", path, value)
	running, err := triggerGatewayReload()
	if err != nil {
		if running {
			if rbErr := rollbackConfigFromBackup(configPath, backupPath); rbErr != nil {
				fmt.Printf("Hot reload failed and rollback failed: %v\n", rbErr)
			} else {
				fmt.Printf("Hot reload failed, config rolled back: %v\n", err)
			}
			return
		}
		fmt.Printf("Updated config file. Hot reload not applied: %v\n", err)
	} else {
		fmt.Println("✓ Gateway hot reload signal sent")
	}
}

func configGetCmd() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: clawgo config get <path>")
		return
	}

	configPath := getConfigPath()
	cfgMap, err := loadConfigAsMap(configPath)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	path := normalizeConfigPath(os.Args[3])
	value, ok := getMapValueByPath(cfgMap, path)
	if !ok {
		fmt.Printf("Path not found: %s\n", path)
		return
	}

	data, err := json.Marshal(value)
	if err != nil {
		fmt.Printf("%v\n", value)
		return
	}
	fmt.Println(string(data))
}

func configReloadCmd() {
	if _, err := triggerGatewayReload(); err != nil {
		fmt.Printf("Hot reload not applied: %v\n", err)
		return
	}
	fmt.Println("✓ Gateway hot reload signal sent")
}

func configCheckCmd() {
	cfg, err := config.LoadConfig(getConfigPath())
	if err != nil {
		fmt.Printf("Config load failed: %v\n", err)
		return
	}
	validationErrors := config.Validate(cfg)
	if len(validationErrors) == 0 {
		fmt.Println("✓ Config validation passed")
		return
	}

	fmt.Println("✗ Config validation failed:")
	for _, ve := range validationErrors {
		fmt.Printf("  - %v\n", ve)
	}
}

func loadConfigAsMap(path string) (map[string]interface{}, error) {
	return configops.LoadConfigAsMap(path)
}

func normalizeConfigPath(path string) string {
	return configops.NormalizeConfigPath(path)
}

func parseConfigValue(raw string) interface{} {
	return configops.ParseConfigValue(raw)
}

func setMapValueByPath(root map[string]interface{}, path string, value interface{}) error {
	return configops.SetMapValueByPath(root, path, value)
}

func getMapValueByPath(root map[string]interface{}, path string) (interface{}, bool) {
	return configops.GetMapValueByPath(root, path)
}

func writeConfigAtomicWithBackup(configPath string, data []byte) (string, error) {
	return configops.WriteConfigAtomicWithBackup(configPath, data)
}

func rollbackConfigFromBackup(configPath, backupPath string) error {
	return configops.RollbackConfigFromBackup(configPath, backupPath)
}

func triggerGatewayReload() (bool, error) {
	return configops.TriggerGatewayReload(getConfigPath(), errGatewayNotRunning)
}

func statusCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	configPath := getConfigPath()

	fmt.Printf("%s clawgo Status\n\n", logo)

	if _, err := os.Stat(configPath); err == nil {
		fmt.Println("Config:", configPath, "✓")
	} else {
		fmt.Println("Config:", configPath, "✗")
	}

	workspace := cfg.WorkspacePath()
	if _, err := os.Stat(workspace); err == nil {
		fmt.Println("Workspace:", workspace, "✓")
	} else {
		fmt.Println("Workspace:", workspace, "✗")
	}

	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Model: %s\n", cfg.Agents.Defaults.Model)
		fmt.Printf("CLIProxyAPI Base: %s\n", cfg.Providers.Proxy.APIBase)
		hasKey := cfg.Providers.Proxy.APIKey != ""
		status := "not set"
		if hasKey {
			status = "✓"
		}
		fmt.Printf("CLIProxyAPI Key: %s\n", status)
		fmt.Printf("Logging: %v\n", cfg.Logging.Enabled)
		if cfg.Logging.Enabled {
			fmt.Printf("Log File: %s\n", cfg.LogFilePath())
			fmt.Printf("Log Max Size: %d MB\n", cfg.Logging.MaxSizeMB)
			fmt.Printf("Log Retention: %d days\n", cfg.Logging.RetentionDays)
		}
	}
}

func getConfigPath() string {
	if strings.TrimSpace(globalConfigPathOverride) != "" {
		return globalConfigPathOverride
	}
	if fromEnv := strings.TrimSpace(os.Getenv("CLAWGO_CONFIG")); fromEnv != "" {
		return fromEnv
	}
	args := os.Args
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--config" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, "--config=") {
			return strings.TrimPrefix(arg, "--config=")
		}
	}
	return filepath.Join(config.GetConfigDir(), "config.json")
}

func loadConfig() (*config.Config, error) {
	cfg, err := config.LoadConfig(getConfigPath())
	if err != nil {
		return nil, err
	}
	configureLogging(cfg)
	return cfg, nil
}

func configureLogging(cfg *config.Config) {
	if !cfg.Logging.Enabled {
		logger.DisableFileLogging()
		return
	}

	logFile := cfg.LogFilePath()
	if err := logger.EnableFileLoggingWithRotation(logFile, cfg.Logging.MaxSizeMB, cfg.Logging.RetentionDays); err != nil {
		fmt.Printf("Warning: failed to enable file logging: %v\n", err)
	}
}

func cronCmd() {
	if len(os.Args) < 3 {
		cronHelp()
		return
	}

	subcommand := os.Args[2]

	dataDir := filepath.Join(filepath.Dir(getConfigPath()), "cron")
	cronStorePath := filepath.Join(dataDir, "jobs.json")

	switch subcommand {
	case "list":
		cronListCmd(cronStorePath)
	case "add":
		cronAddCmd(cronStorePath)
	case "remove":
		if len(os.Args) < 4 {
			fmt.Println("Usage: clawgo cron remove <job_id>")
			return
		}
		cronRemoveCmd(cronStorePath, os.Args[3])
	case "enable":
		cronEnableCmd(cronStorePath, false)
	case "disable":
		cronEnableCmd(cronStorePath, true)
	default:
		fmt.Printf("Unknown cron command: %s\n", subcommand)
		cronHelp()
	}
}

func cronHelp() {
	fmt.Println("\nCron commands:")
	fmt.Println("  list              List all scheduled jobs")
	fmt.Println("  add              Add a new scheduled job")
	fmt.Println("  remove <id>       Remove a job by ID")
	fmt.Println("  enable <id>      Enable a job")
	fmt.Println("  disable <id>     Disable a job")
	fmt.Println()
	fmt.Println("Add options:")
	fmt.Println("  -n, --name       Job name")
	fmt.Println("  -m, --message    Message for agent")
	fmt.Println("  -e, --every      Run every N seconds")
	fmt.Println("  -c, --cron       Cron expression (e.g. '0 9 * * *')")
	fmt.Println("  -d, --deliver     Deliver response to channel")
	fmt.Println("  --to             Recipient for delivery")
	fmt.Println("  --channel        Channel for delivery")
}

func cronListCmd(storePath string) {
	cs := cron.NewCronService(storePath, nil)
	jobs := cs.ListJobs(false)

	if len(jobs) == 0 {
		fmt.Println("No scheduled jobs.")
		return
	}

	fmt.Println("\nScheduled Jobs:")
	fmt.Println("----------------")
	for _, job := range jobs {
		var schedule string
		if job.Schedule.Kind == "every" && job.Schedule.EveryMS != nil {
			schedule = fmt.Sprintf("every %ds", *job.Schedule.EveryMS/1000)
		} else if job.Schedule.Kind == "cron" {
			schedule = job.Schedule.Expr
		} else {
			schedule = "one-time"
		}

		nextRun := "scheduled"
		if job.State.NextRunAtMS != nil {
			nextTime := time.UnixMilli(*job.State.NextRunAtMS)
			nextRun = nextTime.Format("2006-01-02 15:04")
		}

		status := "enabled"
		if !job.Enabled {
			status = "disabled"
		}

		fmt.Printf("  %s (%s)\n", job.Name, job.ID)
		fmt.Printf("    Schedule: %s\n", schedule)
		fmt.Printf("    Status: %s\n", status)
		fmt.Printf("    Next run: %s\n", nextRun)
	}
}

func cronAddCmd(storePath string) {
	name := ""
	message := ""
	var everySec *int64
	cronExpr := ""
	deliver := false
	channel := ""
	to := ""

	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-n", "--name":
			if i+1 < len(args) {
				name = args[i+1]
				i++
			}
		case "-m", "--message":
			if i+1 < len(args) {
				message = args[i+1]
				i++
			}
		case "-e", "--every":
			if i+1 < len(args) {
				var sec int64
				fmt.Sscanf(args[i+1], "%d", &sec)
				everySec = &sec
				i++
			}
		case "-c", "--cron":
			if i+1 < len(args) {
				cronExpr = args[i+1]
				i++
			}
		case "-d", "--deliver":
			deliver = true
		case "--to":
			if i+1 < len(args) {
				to = args[i+1]
				i++
			}
		case "--channel":
			if i+1 < len(args) {
				channel = args[i+1]
				i++
			}
		}
	}

	if name == "" {
		fmt.Println("Error: --name is required")
		return
	}

	if message == "" {
		fmt.Println("Error: --message is required")
		return
	}

	if everySec == nil && cronExpr == "" {
		fmt.Println("Error: Either --every or --cron must be specified")
		return
	}

	var schedule cron.CronSchedule
	if everySec != nil {
		everyMS := *everySec * 1000
		schedule = cron.CronSchedule{
			Kind:    "every",
			EveryMS: &everyMS,
		}
	} else {
		schedule = cron.CronSchedule{
			Kind: "cron",
			Expr: cronExpr,
		}
	}

	cs := cron.NewCronService(storePath, nil)
	job, err := cs.AddJob(name, schedule, message, deliver, channel, to)
	if err != nil {
		fmt.Printf("Error adding job: %v\n", err)
		return
	}

	fmt.Printf("✓ Added job '%s' (%s)\n", job.Name, job.ID)
}

func cronRemoveCmd(storePath, jobID string) {
	cs := cron.NewCronService(storePath, nil)
	if cs.RemoveJob(jobID) {
		fmt.Printf("✓ Removed job %s\n", jobID)
	} else {
		fmt.Printf("✗ Job %s not found\n", jobID)
	}
}

func cronEnableCmd(storePath string, disable bool) {
	if len(os.Args) < 4 {
		fmt.Println("Usage: clawgo cron enable/disable <job_id>")
		return
	}

	jobID := os.Args[3]
	cs := cron.NewCronService(storePath, nil)
	enabled := !disable

	job := cs.EnableJob(jobID, enabled)
	if job != nil {
		status := "enabled"
		if disable {
			status = "disabled"
		}
		fmt.Printf("✓ Job '%s' %s\n", job.Name, status)
	} else {
		fmt.Printf("✗ Job %s not found\n", jobID)
	}
}

func skillsCmd() {
	if len(os.Args) < 3 {
		skillsHelp()
		return
	}

	subcommand := os.Args[2]

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	workspace := cfg.WorkspacePath()
	installer := skills.NewSkillInstaller(workspace)
	// 获取全局配置目录和内置 skills 目录
	globalDir := filepath.Dir(getConfigPath())
	globalSkillsDir := filepath.Join(globalDir, "skills")
	builtinSkillsDir := filepath.Join(globalDir, "clawgo", "skills")
	skillsLoader := skills.NewSkillsLoader(workspace, globalSkillsDir, builtinSkillsDir)

	switch subcommand {
	case "list":
		skillsListCmd(skillsLoader)
	case "install":
		skillsInstallCmd(installer)
	case "remove", "uninstall":
		if len(os.Args) < 4 {
			fmt.Println("Usage: clawgo skills remove <skill-name>")
			return
		}
		skillsRemoveCmd(installer, os.Args[3])
	case "search":
		skillsSearchCmd(installer)
	case "show":
		if len(os.Args) < 4 {
			fmt.Println("Usage: clawgo skills show <skill-name>")
			return
		}
		skillsShowCmd(skillsLoader, os.Args[3])
	default:
		fmt.Printf("Unknown skills command: %s\n", subcommand)
		skillsHelp()
	}
}

func skillsHelp() {
	fmt.Println("\nSkills commands:")
	fmt.Println("  list                    List installed skills")
	fmt.Println("  install <repo>          Install skill from GitHub")
	fmt.Println("  install-builtin          Install all builtin skills to workspace")
	fmt.Println("  list-builtin             List available builtin skills")
	fmt.Println("  remove <name>           Remove installed skill")
	fmt.Println("  search                  Search available skills")
	fmt.Println("  show <name>             Show skill details")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  clawgo skills list")
	fmt.Println("  clawgo skills install YspCoder/clawgo-skills/weather")
	fmt.Println("  clawgo skills install-builtin")
	fmt.Println("  clawgo skills list-builtin")
	fmt.Println("  clawgo skills remove weather")
}

func skillsListCmd(loader *skills.SkillsLoader) {
	allSkills := loader.ListSkills()

	if len(allSkills) == 0 {
		fmt.Println("No skills installed.")
		return
	}

	fmt.Println("\nInstalled Skills:")
	fmt.Println("------------------")
	for _, skill := range allSkills {
		fmt.Printf("  ✓ %s (%s)\n", skill.Name, skill.Source)
		if skill.Description != "" {
			fmt.Printf("    %s\n", skill.Description)
		}
	}
}

func skillsInstallCmd(installer *skills.SkillInstaller) {
	if len(os.Args) < 4 {
		fmt.Println("Usage: clawgo skills install <github-repo>")
		fmt.Println("Example: clawgo skills install YspCoder/clawgo-skills/weather")
		return
	}

	repo := os.Args[3]
	fmt.Printf("Installing skill from %s...\n", repo)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := installer.InstallFromGitHub(ctx, repo); err != nil {
		fmt.Printf("✗ Failed to install skill: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Skill '%s' installed successfully!\n", filepath.Base(repo))
}

func skillsRemoveCmd(installer *skills.SkillInstaller, skillName string) {
	fmt.Printf("Removing skill '%s'...\n", skillName)

	if err := installer.Uninstall(skillName); err != nil {
		fmt.Printf("✗ Failed to remove skill: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Skill '%s' removed successfully!\n", skillName)
}

func skillsInstallBuiltinCmd(workspace string) {
	builtinSkillsDir := detectBuiltinSkillsDir(workspace)
	workspaceSkillsDir := filepath.Join(workspace, "skills")

	fmt.Printf("Copying builtin skills to workspace...\n")

	skillsToInstall := []string{
		"weather",
		"news",
		"stock",
		"calculator",
	}

	for _, skillName := range skillsToInstall {
		builtinPath := filepath.Join(builtinSkillsDir, skillName)
		workspacePath := filepath.Join(workspaceSkillsDir, skillName)

		if _, err := os.Stat(builtinPath); err != nil {
			fmt.Printf("⊘ Builtin skill '%s' not found: %v\n", skillName, err)
			continue
		}

		if err := os.MkdirAll(workspacePath, 0755); err != nil {
			fmt.Printf("✗ Failed to create directory for %s: %v\n", skillName, err)
			continue
		}

		if err := copyDirectory(builtinPath, workspacePath); err != nil {
			fmt.Printf("✗ Failed to copy %s: %v\n", skillName, err)
		}
	}

	fmt.Println("\n✓ All builtin skills installed!")
	fmt.Println("Now you can use them in your workspace.")
}

func skillsListBuiltinCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}
	builtinSkillsDir := detectBuiltinSkillsDir(cfg.WorkspacePath())

	fmt.Println("\nAvailable Builtin Skills:")
	fmt.Println("-----------------------")

	entries, err := os.ReadDir(builtinSkillsDir)
	if err != nil {
		fmt.Printf("Error reading builtin skills: %v\n", err)
		return
	}

	if len(entries) == 0 {
		fmt.Println("No builtin skills available.")
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			skillName := entry.Name()
			skillFile := filepath.Join(builtinSkillsDir, skillName, "SKILL.md")

			description := "No description"
			if _, err := os.Stat(skillFile); err == nil {
				data, err := os.ReadFile(skillFile)
				if err == nil {
					content := string(data)
					if idx := strings.Index(content, "\n"); idx > 0 {
						firstLine := content[:idx]
						if strings.Contains(firstLine, "description:") {
							descLine := strings.Index(content[idx:], "\n")
							if descLine > 0 {
								description = strings.TrimSpace(content[idx+descLine : idx+descLine])
							}
						}
					}
				}
			}
			status := "✓"
			fmt.Printf("  %s  %s\n", status, entry.Name())
			if description != "" {
				fmt.Printf("     %s\n", description)
			}
		}
	}
}

func detectBuiltinSkillsDir(workspace string) string {
	candidates := []string{
		filepath.Join(".", "skills"),
		filepath.Join(filepath.Dir(workspace), "clawgo", "skills"),
		filepath.Join(config.GetConfigDir(), "clawgo", "skills"),
	}
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	// Fallback to repository-style path for error output consistency.
	return filepath.Join(".", "skills")
}

func skillsSearchCmd(installer *skills.SkillInstaller) {
	fmt.Println("Searching for available skills...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	availableSkills, err := installer.ListAvailableSkills(ctx)
	if err != nil {
		fmt.Printf("✗ Failed to fetch skills list: %v\n", err)
		return
	}

	if len(availableSkills) == 0 {
		fmt.Println("No skills available.")
		return
	}

	fmt.Printf("\nAvailable Skills (%d):\n", len(availableSkills))
	fmt.Println("--------------------")
	for _, skill := range availableSkills {
		fmt.Printf("  📦 %s\n", skill.Name)
		fmt.Printf("     %s\n", skill.Description)
		fmt.Printf("     Repo: %s\n", skill.Repository)
		if skill.Author != "" {
			fmt.Printf("     Author: %s\n", skill.Author)
		}
		if len(skill.Tags) > 0 {
			fmt.Printf("     Tags: %v\n", skill.Tags)
		}
		fmt.Println()
	}
}

func skillsShowCmd(loader *skills.SkillsLoader, skillName string) {
	content, ok := loader.LoadSkill(skillName)
	if !ok {
		fmt.Printf("✗ Skill '%s' not found\n", skillName)
		return
	}

	fmt.Printf("\n📦 Skill: %s\n", skillName)
	fmt.Println("----------------------")
	fmt.Println(content)
}

func loginCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Configuring CLIProxyAPI...")
	fmt.Printf("Current Base: %s\n", cfg.Providers.Proxy.APIBase)

	fmt.Print("Enter CLIProxyAPI Base URL (e.g. http://localhost:8080/v1): ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	apiBase := strings.TrimSpace(line)
	if apiBase != "" {
		cfg.Providers.Proxy.APIBase = apiBase
	}

	fmt.Print("Enter API Key (optional): ")
	fmt.Scanln(&cfg.Providers.Proxy.APIKey)

	if err := config.SaveConfig(getConfigPath(), cfg); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ CLIProxyAPI configuration saved.")
}

func configureProvider(cfg *config.Config, provider string) {
	// Deprecated: Migrated to CLIProxyAPI logic in loginCmd
}

func channelCmd() {
	if len(os.Args) < 3 {
		channelHelp()
		return
	}

	subcommand := os.Args[2]

	switch subcommand {
	case "test":
		channelTestCmd()
	default:
		fmt.Printf("Unknown channel command: %s\n", subcommand)
		channelHelp()
	}
}

func channelHelp() {
	fmt.Println("\nChannel commands:")
	fmt.Println("  test              Send a test message to a specific channel")
	fmt.Println()
	fmt.Println("Test options:")
	fmt.Println("  --to             Recipient ID")
	fmt.Println("  --channel        Channel name (telegram, discord, etc.)")
	fmt.Println("  -m, --message    Message to send")
}

func channelTestCmd() {
	to := ""
	channelName := ""
	message := "This is a test message from ClawGo 🦞"

	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--to":
			if i+1 < len(args) {
				to = args[i+1]
				i++
			}
		case "--channel":
			if i+1 < len(args) {
				channelName = args[i+1]
				i++
			}
		case "-m", "--message":
			if i+1 < len(args) {
				message = args[i+1]
				i++
			}
		}
	}

	if channelName == "" || to == "" {
		fmt.Println("Error: --channel and --to are required")
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	msgBus := bus.NewMessageBus()
	mgr, err := channels.NewManager(cfg, msgBus)
	if err != nil {
		fmt.Printf("Error creating channel manager: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	// Start the manager to initialize channels
	if err := mgr.StartAll(ctx); err != nil {
		fmt.Printf("Error starting channels: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Sending test message to %s (%s)...\n", channelName, to)
	if err := mgr.SendToChannel(ctx, channelName, to, message); err != nil {
		fmt.Printf("✗ Failed to send message: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Test message sent successfully!")
}

func uninstallCmd() {
	purge := false
	removeBin := false

	for _, arg := range os.Args[2:] {
		switch arg {
		case "--purge":
			purge = true
		case "--remove-bin":
			removeBin = true
		}
	}

	// 1) Remove gateway service if registered.
	if err := uninstallGatewayService(); err != nil {
		fmt.Printf("Gateway service uninstall warning: %v\n", err)
	} else {
		fmt.Println("✓ Gateway service uninstalled")
	}

	// 2) Remove runtime pid file.
	pidPath := filepath.Join(filepath.Dir(getConfigPath()), "gateway.pid")
	_ = os.Remove(pidPath)

	// 3) Optional purge config/workspace.
	if purge {
		configDir := filepath.Dir(getConfigPath())
		if err := os.RemoveAll(configDir); err != nil {
			fmt.Printf("Failed to remove config directory %s: %v\n", configDir, err)
			os.Exit(1)
		}
		fmt.Printf("✓ Removed config/workspace directory: %s\n", configDir)
	}

	// 4) Optional remove current executable.
	if removeBin {
		exePath, err := os.Executable()
		if err != nil {
			fmt.Printf("Failed to resolve executable path: %v\n", err)
			os.Exit(1)
		}
		if err := os.Remove(exePath); err != nil {
			fmt.Printf("Failed to remove executable %s: %v\n", exePath, err)
			os.Exit(1)
		}
		fmt.Printf("✓ Removed executable: %s\n", exePath)
	}
}

func uninstallGatewayService() error {
	scope, unitPath, err := detectInstalledGatewayService()
	if err != nil {
		// Service not present is not fatal for uninstall command.
		return nil
	}

	// Ignore stop/disable errors to keep uninstall idempotent.
	_ = runSystemctl(scope, "stop", gatewayServiceName)
	_ = runSystemctl(scope, "disable", gatewayServiceName)

	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit file failed: %w", err)
	}

	if err := runSystemctl(scope, "daemon-reload"); err != nil {
		return err
	}
	return nil
}
