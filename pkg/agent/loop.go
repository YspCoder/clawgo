// ClawGo - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 ClawGo contributors

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/cron"
	"github.com/YspCoder/clawgo/pkg/lifecycle"
	"github.com/YspCoder/clawgo/pkg/logger"
	"github.com/YspCoder/clawgo/pkg/providers"
	"github.com/YspCoder/clawgo/pkg/runtimecfg"
	"github.com/YspCoder/clawgo/pkg/scheduling"
	"github.com/YspCoder/clawgo/pkg/session"
	"github.com/YspCoder/clawgo/pkg/tools"
)

type AgentLoop struct {
	bus                          *bus.MessageBus
	cfg                          *config.Config
	provider                     providers.LLMProvider
	workspace                    string
	model                        string
	maxTokens                    int
	temperature                  float64
	maxIterations                int
	sessions                     *session.SessionManager
	contextBuilder               *ContextBuilder
	tools                        *tools.ToolRegistry
	compactionEnabled            bool
	compactionTrigger            int
	compactionKeepRecent         int
	compactionProtectLastN       int
	compactionTargetRatio        float64
	compactionPressureThreshold  float64
	compactionMaxSummaryChars    int
	compactionMaxTranscriptChars int
	heartbeatAckMaxChars         int
	heartbeatAckToken            string
	audit                        *triggerAudit
	running                      bool
	sessionScheduler             *SessionScheduler
	providerChain                []providerCandidate
	providerNames                []string
	providerPool                 map[string]providers.LLMProvider
	providerResponses            map[string]config.ProviderResponsesConfig
	providerMaxTokens            map[string]int
	providerTemperatures         map[string]float64
	telegramStreaming            bool
	providerMu                   sync.RWMutex
	sessionProvider              map[string]string
	streamMu                     sync.Mutex
	sessionStreamed              map[string]bool
	subagentManager              *tools.SubagentManager
	subagentRouter               *tools.SubagentRouter
	configPath                   string
	subagentDigestMu             sync.Mutex
	subagentDigestDelay          time.Duration
	subagentDigests              map[string]*subagentDigestState
	compactionRunner             *lifecycle.LoopRunner
	compactionMu                 sync.Mutex
	compactionSignal             chan struct{}
	compactionQueue              []string
	compactionQueued             map[string]struct{}
	compactionInflight           map[string]struct{}
	compactionDirty              map[string]struct{}
	runMu                        sync.Mutex
	runCancel                    context.CancelFunc
	runWG                        sync.WaitGroup
}

type providerCandidate struct {
	ref   string
	name  string
	model string
}

type subagentDigestItem struct {
	agentID       string
	reason        string
	status        string
	taskSummary   string
	resultSummary string
}

type subagentDigestState struct {
	channel string
	chatID  string
	items   map[string]subagentDigestItem
	dueAt   time.Time
}

func (al *AgentLoop) SetConfigPath(path string) {
	if al == nil {
		return
	}
	al.configPath = strings.TrimSpace(path)
}

// StartupCompactionReport provides startup memory/session maintenance stats.
type StartupCompactionReport struct {
	TotalSessions     int `json:"total_sessions"`
	CompactedSessions int `json:"compacted_sessions"`
}

func NewAgentLoop(cfg *config.Config, msgBus *bus.MessageBus, provider providers.LLMProvider, cs *cron.CronService) *AgentLoop {
	workspace := cfg.WorkspacePath()
	os.MkdirAll(workspace, 0755)

	sessionsManager := session.NewSessionManager(filepath.Join(filepath.Dir(cfg.WorkspacePath()), "agents", "main", "sessions"))

	toolsRegistry := tools.NewToolRegistry()
	processManager := tools.NewProcessManager(workspace)
	readTool := tools.NewReadFileTool(workspace)
	writeTool := tools.NewWriteFileTool(workspace)
	listTool := tools.NewListDirTool(workspace)
	toolsRegistry.Register(readTool)
	toolsRegistry.Register(writeTool)
	toolsRegistry.Register(listTool)
	toolsRegistry.Register(tools.NewExecTool(cfg.Tools.Shell, workspace, processManager))
	toolsRegistry.Register(tools.NewProcessTool(processManager))
	toolsRegistry.Register(tools.NewSkillExecTool(workspace))

	if cs != nil {
		toolsRegistry.Register(tools.NewRemindTool(cs))
		toolsRegistry.Register(tools.NewCronTool(cs))
	}

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

	braveAPIKey := cfg.Tools.Web.Search.APIKey
	toolsRegistry.Register(tools.NewWebSearchTool(braveAPIKey, cfg.Tools.Web.Search.MaxResults))
	webFetchTool := tools.NewWebFetchTool(50000)
	toolsRegistry.Register(webFetchTool)
	toolsRegistry.Register(tools.NewParallelFetchTool(webFetchTool, maxParallelCalls, parallelSafe))
	if cfg.Tools.MCP.Enabled {
		mcpTool := tools.NewMCPTool(workspace, cfg.Tools.MCP)
		toolsRegistry.Register(mcpTool)
		discoveryCtx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Tools.MCP.RequestTimeoutSec)*time.Second)
		for _, remoteTool := range mcpTool.DiscoverTools(discoveryCtx) {
			toolsRegistry.Register(remoteTool)
		}
		cancel()
	}

	// Register message tool
	messageTool := tools.NewMessageTool()
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
	toolsRegistry.Register(messageTool)

	// Register spawn tool
	subagentManager := tools.NewSubagentManager(provider, workspace, msgBus)
	subagentRouter := tools.NewSubagentRouter(subagentManager)
	spawnTool := tools.NewSpawnTool(subagentManager)
	toolsRegistry.Register(spawnTool)
	if store := subagentManager.ProfileStore(); store != nil {
		toolsRegistry.Register(tools.NewSubagentProfileTool(store))
	}
	toolsRegistry.Register(tools.NewSessionsTool(
		func(limit int) []tools.SessionInfo {
			sessions := alSessionListForTool(sessionsManager, limit)
			return sessions
		},
		func(key string, limit int) []providers.Message {
			h := sessionsManager.GetHistory(key)
			if limit > 0 && len(h) > limit {
				return h[len(h)-limit:]
			}
			return h
		},
	))
	toolsRegistry.Register(tools.NewSessionSearchTool(sessionsManager))

	// Register edit file tool
	editFileTool := tools.NewEditFileTool(workspace)
	toolsRegistry.Register(editFileTool)

	// Register memory tools
	memorySearchTool := tools.NewMemorySearchTool(workspace)
	toolsRegistry.Register(memorySearchTool)
	toolsRegistry.Register(tools.NewMemoryGetTool(workspace))
	toolsRegistry.Register(tools.NewMemoryWriteTool(workspace))

	// Register parallel execution tool (leveraging Go's concurrency)
	toolsRegistry.Register(tools.NewParallelTool(toolsRegistry, maxParallelCalls, parallelSafe))

	// Register browser tool (integrated Chromium support)
	toolsRegistry.Register(tools.NewBrowserTool())

	// Register camera tool
	toolsRegistry.Register(tools.NewCameraTool(workspace))
	// Register system info tool
	toolsRegistry.Register(tools.NewSystemInfoTool())

	loop := &AgentLoop{
		bus:                          msgBus,
		cfg:                          cfg,
		provider:                     provider,
		workspace:                    workspace,
		model:                        provider.GetDefaultModel(),
		maxTokens:                    cfg.Agents.Defaults.MaxTokens,
		temperature:                  cfg.Agents.Defaults.Temperature,
		maxIterations:                cfg.Agents.Defaults.MaxToolIterations,
		sessions:                     sessionsManager,
		contextBuilder:               NewContextBuilder(workspace, func() []string { return toolsRegistry.GetSummaries() }),
		tools:                        toolsRegistry,
		compactionEnabled:            cfg.Agents.Defaults.ContextCompaction.Enabled,
		compactionTrigger:            cfg.Agents.Defaults.ContextCompaction.TriggerMessages,
		compactionKeepRecent:         cfg.Agents.Defaults.ContextCompaction.KeepRecentMessages,
		compactionProtectLastN:       normalizePositiveInt(cfg.Agents.Defaults.ContextCompaction.ProtectLastN, 12),
		compactionTargetRatio:        normalizeCompactionRatio(cfg.Agents.Defaults.ContextCompaction.TargetRatio, 0.35),
		compactionPressureThreshold:  normalizeCompactionRatio(cfg.Agents.Defaults.ContextCompaction.PressureThreshold, 0.8),
		compactionMaxSummaryChars:    normalizePositiveInt(cfg.Agents.Defaults.ContextCompaction.MaxSummaryChars, 6000),
		compactionMaxTranscriptChars: normalizePositiveInt(cfg.Agents.Defaults.ContextCompaction.MaxTranscriptChars, 20000),
		heartbeatAckMaxChars:         cfg.Agents.Defaults.Heartbeat.AckMaxChars,
		heartbeatAckToken:            loadHeartbeatAckToken(workspace),
		audit:                        newTriggerAudit(workspace),
		running:                      false,
		sessionScheduler:             NewSessionScheduler(0),
		sessionProvider:              map[string]string{},
		sessionStreamed:              map[string]bool{},
		providerResponses:            map[string]config.ProviderResponsesConfig{},
		providerMaxTokens:            map[string]int{},
		providerTemperatures:         map[string]float64{},
		telegramStreaming:            cfg.Channels.Telegram.Streaming,
		subagentManager:              subagentManager,
		subagentRouter:               subagentRouter,
		subagentDigestDelay:          5 * time.Second,
		subagentDigests:              map[string]*subagentDigestState{},
		compactionRunner:             lifecycle.NewLoopRunner(),
		compactionSignal:             make(chan struct{}, 1),
		compactionQueued:             map[string]struct{}{},
		compactionInflight:           map[string]struct{}{},
		compactionDirty:              map[string]struct{}{},
	}
	if _, primaryModel := config.ParseProviderModelRef(cfg.Agents.Defaults.Model.Primary); strings.TrimSpace(primaryModel) != "" {
		loop.model = strings.TrimSpace(primaryModel)
	}
	go loop.runSubagentDigestTicker()
	// Initialize provider fallback chain (primary + inferred providers).
	loop.providerChain = []providerCandidate{}
	loop.providerPool = map[string]providers.LLMProvider{}
	loop.providerNames = []string{}
	primaryName := config.PrimaryProviderName(cfg)
	primaryRef := strings.TrimSpace(cfg.Agents.Defaults.Model.Primary)
	if primaryRef == "" {
		primaryRef = primaryName + "/" + loop.model
	}
	loop.providerPool[primaryName] = provider
	loop.providerChain = append(loop.providerChain, providerCandidate{
		ref:   primaryRef,
		name:  primaryName,
		model: loop.model,
	})
	loop.providerNames = append(loop.providerNames, primaryName)
	if pc, ok := config.ProviderConfigByName(cfg, primaryName); ok {
		loop.providerResponses[primaryName] = pc.Responses
		loop.providerMaxTokens[primaryName] = pc.MaxTokens
		loop.providerTemperatures[primaryName] = pc.Temperature
	}
	seenProviders := map[string]struct{}{primaryName: {}}
	providerConfigs := config.AllProviderConfigs(cfg)
	providerOrder := make([]string, 0, len(providerConfigs))
	for name := range providerConfigs {
		normalized := strings.TrimSpace(name)
		if normalized == "" {
			continue
		}
		providerOrder = append(providerOrder, normalized)
	}
	sort.SliceStable(providerOrder, func(i, j int) bool {
		ni := normalizeFallbackProviderName(providerOrder[i])
		nj := normalizeFallbackProviderName(providerOrder[j])
		pi := automaticFallbackPriority(ni)
		pj := automaticFallbackPriority(nj)
		if pi == pj {
			return ni < nj
		}
		return pi < pj
	})
	for _, rawName := range providerOrder {
		providerName := strings.TrimSpace(rawName)
		if providerName == "" {
			continue
		}
		providerName, _ = config.ParseProviderModelRef(providerName + "/_")
		if providerName == "" {
			continue
		}
		if _, dup := seenProviders[providerName]; dup {
			continue
		}
		modelName := ""
		if pc, ok := config.ProviderConfigByName(cfg, providerName); ok {
			if len(pc.Models) > 0 {
				modelName = strings.TrimSpace(pc.Models[0])
			}
			loop.providerResponses[providerName] = pc.Responses
			loop.providerMaxTokens[providerName] = pc.MaxTokens
			loop.providerTemperatures[providerName] = pc.Temperature
		}
		seenProviders[providerName] = struct{}{}
		loop.providerNames = append(loop.providerNames, providerName)
		ref := providerName
		if modelName != "" {
			ref += "/" + modelName
		}
		loop.providerChain = append(loop.providerChain, providerCandidate{
			ref:   ref,
			name:  providerName,
			model: modelName,
		})
	}

	// Inject recursive run logic so subagents can use full tool-calling flows.
	subagentManager.SetRunFunc(func(ctx context.Context, run *tools.SubagentRun) (string, error) {
		if run == nil {
			return "", fmt.Errorf("subagent run is nil")
		}
		if run.MaxToolIterations <= 0 {
			run.MaxToolIterations = loop.maxIterations
		}
		sessionKey := strings.TrimSpace(run.SessionKey)
		if sessionKey == "" {
			sessionKey = fmt.Sprintf("subagent:%s", strings.TrimSpace(run.ID))
		}
		taskInput := loop.buildSubagentRunInput(run)
		ns := normalizeMemoryNamespace(run.MemoryNS)
		ctx = withMemoryNamespaceContext(ctx, ns)
		ctx = withToolAllowlistContext(ctx, run.ToolAllowlist)
		channel := strings.TrimSpace(run.OriginChannel)
		if channel == "" {
			channel = "cli"
		}
		chatID := strings.TrimSpace(run.OriginChatID)
		if chatID == "" {
			chatID = "direct"
		}
		msg := bus.InboundMessage{
			Channel:    channel,
			SenderID:   "subagent",
			ChatID:     chatID,
			Content:    taskInput,
			SessionKey: sessionKey,
			Metadata: map[string]string{
				"memory_namespace": ns,
				"memory_ns":        ns,
				"disable_planning": "true",
				"trigger":          "subagent",
			},
		}
		return loop.processMessage(ctx, msg)
	})

	return loop
}

func (al *AgentLoop) buildSubagentRunInput(run *tools.SubagentRun) string {
	if run == nil {
		return ""
	}
	taskText := strings.TrimSpace(run.Task)
	if promptFile := strings.TrimSpace(run.SystemPromptFile); promptFile != "" {
		if promptText := al.readSubagentPromptFile(promptFile); promptText != "" {
			return fmt.Sprintf("Role Profile Policy (%s):\n%s\n\nTask:\n%s", promptFile, promptText, taskText)
		}
	}
	return taskText
}

func (al *AgentLoop) readSubagentPromptFile(relPath string) string {
	if al == nil {
		return ""
	}
	workspace := strings.TrimSpace(al.workspace)
	relPath = strings.TrimSpace(relPath)
	if workspace == "" || relPath == "" || filepath.IsAbs(relPath) {
		return ""
	}
	fullPath := filepath.Clean(filepath.Join(workspace, relPath))
	relToWorkspace, err := filepath.Rel(workspace, fullPath)
	if err != nil || strings.HasPrefix(relToWorkspace, "..") {
		return ""
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (al *AgentLoop) Run(ctx context.Context) error {
	al.runMu.Lock()
	if al.runCancel != nil {
		al.runMu.Unlock()
		return fmt.Errorf("agent loop already running")
	}
	runCtx, cancel := context.WithCancel(ctx)
	al.runCancel = cancel
	al.running = true
	al.runMu.Unlock()
	defer func() {
		al.runMu.Lock()
		al.running = false
		al.runCancel = nil
		al.runMu.Unlock()
	}()

	shards := al.buildSessionShards(runCtx)
	defer func() {
		for _, ch := range shards {
			close(ch)
		}
		al.runWG.Wait()
	}()

	for al.running {
		msg, ok := al.bus.ConsumeInbound(runCtx)
		if !ok {
			if runCtx.Err() != nil {
				return nil
			}
			continue
		}
		idx := sessionShardIndex(msg.SessionKey, len(shards))
		select {
		case shards[idx] <- msg:
		case <-runCtx.Done():
			return nil
		}
	}

	return nil
}

func (al *AgentLoop) Stop() {
	al.runMu.Lock()
	cancel := al.runCancel
	al.runMu.Unlock()
	if cancel != nil {
		cancel()
	}
	al.running = false
	al.runWG.Wait()
	if al.compactionRunner != nil {
		al.compactionRunner.Stop()
	}
}

func (al *AgentLoop) buildSessionShards(ctx context.Context) []chan bus.InboundMessage {
	count := sessionShardCount()
	shards := make([]chan bus.InboundMessage, count)
	for i := 0; i < count; i++ {
		shards[i] = make(chan bus.InboundMessage, 64)
		al.runWG.Add(1)
		go func(ch <-chan bus.InboundMessage) {
			defer al.runWG.Done()
			for msg := range ch {
				al.processInbound(ctx, msg)
			}
		}(shards[i])
	}
	logger.InfoCF("agent", logger.C0149, map[string]interface{}{"shards": count})
	return shards
}

func (al *AgentLoop) tryFallbackProviders(ctx context.Context, msg bus.InboundMessage, messages []providers.Message, toolDefs []providers.ToolDefinition, primaryErr error) (*providers.LLMResponse, string, int, error) {
	if len(al.providerChain) <= 1 {
		return nil, "", 0, primaryErr
	}
	lastErr := primaryErr
	attempts := 0
	candidateNames := make([]string, 0, len(al.providerChain)-1)
	for _, candidate := range al.providerChain[1:] {
		candidateNames = append(candidateNames, candidate.name)
	}
	ranked := make([]providerCandidate, 0, len(al.providerChain)-1)
	used := make([]bool, len(al.providerChain)-1)
	for _, name := range candidateNames {
		for idx, candidate := range al.providerChain[1:] {
			if used[idx] || candidate.name != name {
				continue
			}
			used[idx] = true
			ranked = append(ranked, candidate)
		}
	}
	for idx, candidate := range al.providerChain[1:] {
		if !used[idx] {
			ranked = append(ranked, candidate)
		}
	}
	for _, candidate := range ranked {
		p, candidateModel, err := al.ensureProviderCandidate(candidate)
		if err != nil {
			lastErr = err
			continue
		}
		attempts++
		fallbackOptions := al.buildResponsesOptionsForProvider(msg.SessionKey, candidate.name, int64(al.maxTokensForProvider(candidate.name)), al.temperatureForProvider(candidate.name))
		resp, err := p.Chat(ctx, messages, toolDefs, candidateModel, fallbackOptions)
		if err == nil {
			al.setSessionProvider(msg.SessionKey, candidate.name)
			logger.WarnCF("agent", logger.C0150, map[string]interface{}{"provider": candidate.name, "model": candidateModel, "ref": candidate.ref})
			return resp, candidate.name, attempts, nil
		}
		lastErr = err
	}
	return nil, "", attempts, lastErr
}

func (al *AgentLoop) ensureProviderCandidate(candidate providerCandidate) (providers.LLMProvider, string, error) {
	if al == nil {
		return nil, "", fmt.Errorf("agent loop is nil")
	}
	name := strings.TrimSpace(candidate.name)
	if name == "" {
		return nil, "", fmt.Errorf("fallback provider name is empty")
	}
	al.providerMu.RLock()
	existing := al.providerPool[name]
	al.providerMu.RUnlock()
	if existing != nil {
		model := strings.TrimSpace(candidate.model)
		if model == "" {
			model = strings.TrimSpace(existing.GetDefaultModel())
		}
		if model == "" {
			return nil, "", fmt.Errorf("fallback provider %q has no model configured", name)
		}
		return existing, model, nil
	}
	if al.cfg == nil {
		return nil, "", fmt.Errorf("config not available for fallback provider %q", name)
	}
	created, err := providers.CreateProviderByName(al.cfg, name)
	if err != nil {
		return nil, "", err
	}
	model := strings.TrimSpace(candidate.model)
	if model == "" {
		model = strings.TrimSpace(created.GetDefaultModel())
	}
	if model == "" {
		return nil, "", fmt.Errorf("fallback provider %q has no model configured", name)
	}
	al.providerMu.Lock()
	if existing := al.providerPool[name]; existing != nil {
		al.providerMu.Unlock()
		return existing, model, nil
	}
	al.providerPool[name] = created
	al.providerMu.Unlock()
	return created, model, nil
}

func (al *AgentLoop) providerCandidateByName(name string) (providerCandidate, bool) {
	if al == nil {
		return providerCandidate{}, false
	}
	target := strings.TrimSpace(name)
	if target == "" {
		return providerCandidate{}, false
	}
	for _, candidate := range al.providerChain {
		if strings.EqualFold(strings.TrimSpace(candidate.name), target) {
			return candidate, true
		}
	}
	return providerCandidate{}, false
}

func (al *AgentLoop) defaultProviderName() string {
	if al == nil || len(al.providerNames) == 0 {
		return ""
	}
	return strings.TrimSpace(al.providerNames[0])
}

func (al *AgentLoop) sessionProviderName(sessionKey string) string {
	name := strings.TrimSpace(al.getSessionProvider(sessionKey))
	if name == "" {
		name = al.defaultProviderName()
	}
	return name
}

func (al *AgentLoop) isKnownProviderName(name string) bool {
	target := strings.TrimSpace(name)
	if target == "" {
		return false
	}
	for _, item := range al.providerNames {
		if strings.EqualFold(strings.TrimSpace(item), target) {
			return true
		}
	}
	return false
}

func (al *AgentLoop) activeProviderForSession(sessionKey string) (providers.LLMProvider, string, string, error) {
	if al == nil {
		return nil, "", "", fmt.Errorf("agent loop is nil")
	}
	name := al.sessionProviderName(sessionKey)
	if name == "" {
		return al.provider, al.model, "", nil
	}
	if strings.EqualFold(name, al.defaultProviderName()) {
		model := strings.TrimSpace(al.model)
		if model == "" && al.provider != nil {
			model = strings.TrimSpace(al.provider.GetDefaultModel())
		}
		return al.provider, model, name, nil
	}
	candidate, ok := al.providerCandidateByName(name)
	if !ok {
		return al.provider, al.model, name, nil
	}
	p, model, err := al.ensureProviderCandidate(candidate)
	if err != nil {
		return nil, "", name, err
	}
	return p, model, name, nil
}

func automaticFallbackPriority(name string) int {
	switch normalizeFallbackProviderName(name) {
	case "claude":
		return 10
	case "codex":
		return 20
	case "gemini":
		return 30
	case "gemini-cli":
		return 40
	case "aistudio":
		return 50
	case "vertex":
		return 60
	case "antigravity":
		return 70
	case "qwen":
		return 80
	case "kimi":
		return 90
	case "iflow":
		return 100
	case "openai-compatibility":
		return 110
	default:
		return 1000
	}
}

func normalizeFallbackProviderName(name string) string {
	normalized, _ := config.ParseProviderModelRef(strings.TrimSpace(name) + "/_")
	return strings.TrimSpace(normalized)
}

func (al *AgentLoop) setSessionProvider(sessionKey, provider string) {
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return
	}
	al.providerMu.Lock()
	al.sessionProvider[key] = provider
	al.providerMu.Unlock()
}

func (al *AgentLoop) getSessionProvider(sessionKey string) string {
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return ""
	}
	al.providerMu.RLock()
	v := al.sessionProvider[key]
	al.providerMu.RUnlock()
	return v
}

func (al *AgentLoop) syncSessionDefaultProvider(sessionKey string) {
	if al == nil {
		return
	}
	current := strings.TrimSpace(al.getSessionProvider(sessionKey))
	if current == "" {
		if name := al.defaultProviderName(); name != "" {
			al.setSessionProvider(sessionKey, name)
		}
		return
	}
	if al.isKnownProviderName(current) {
		return
	}
	if name := al.defaultProviderName(); name != "" {
		al.setSessionProvider(sessionKey, name)
	}
}

func (al *AgentLoop) markSessionStreamed(sessionKey string) {
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return
	}
	al.streamMu.Lock()
	al.sessionStreamed[key] = true
	al.streamMu.Unlock()
}

func (al *AgentLoop) consumeSessionStreamed(sessionKey string) bool {
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return false
	}
	al.streamMu.Lock()
	defer al.streamMu.Unlock()
	v := al.sessionStreamed[key]
	delete(al.sessionStreamed, key)
	return v
}

func (al *AgentLoop) processInbound(ctx context.Context, msg bus.InboundMessage) {
	response, err := al.processPlannedMessage(ctx, msg)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			al.audit.Record(al.getTrigger(msg), msg.Channel, msg.SessionKey, true, err)
			return
		}
		response = fmt.Sprintf("Error processing message: %v", err)
	}

	trigger := al.getTrigger(msg)
	suppressed := false
	if response != "" {
		if msg.Channel == "telegram" && al.telegramStreaming && al.consumeSessionStreamed(msg.SessionKey) {
			suppressed = true
		} else if outbound, ok := al.prepareOutbound(msg, response); ok {
			al.bus.PublishOutbound(outbound)
		} else {
			suppressed = true
		}
	}
	if msg.Channel == "telegram" && suppressed {
		replyID := ""
		if msg.Metadata != nil {
			replyID = msg.Metadata["message_id"]
		}
		// Final pass uses full formatted content to stabilize rendering after plain streaming.
		al.bus.PublishOutbound(bus.OutboundMessage{Channel: msg.Channel, ChatID: msg.ChatID, Action: "finalize", Content: response, ReplyToID: replyID})
	}
	al.audit.Record(trigger, msg.Channel, msg.SessionKey, suppressed, err)
}

func sessionShardCount() int {
	// Keep ~20% CPU headroom for system/background work, then use a
	// sub-linear curve to avoid oversharding on high-core machines.
	n := runtime.GOMAXPROCS(0)
	if n <= 0 {
		n = runtime.NumCPU()
	}
	if n <= 0 {
		return 2
	}
	budget := int(math.Floor(float64(n) * 0.8))
	if budget < 1 {
		budget = 1
	}
	shards := int(math.Round(math.Sqrt(float64(budget)) * 2.2))
	if shards < 2 {
		shards = 2
	}
	if shards > 12 {
		shards = 12
	}
	return shards
}

func sessionShardIndex(sessionKey string, shardCount int) int {
	if shardCount <= 1 {
		return 0
	}
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		key = "default"
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() % uint32(shardCount))
}

var thinkTagPattern = regexp.MustCompile(`(?s)<think>.*?</think>`)

func (al *AgentLoop) getTrigger(msg bus.InboundMessage) string {
	if msg.Metadata != nil {
		if t := strings.TrimSpace(msg.Metadata["trigger"]); t != "" {
			return strings.ToLower(t)
		}
	}
	if msg.Channel == "system" {
		sid := strings.ToLower(strings.TrimSpace(msg.SenderID))
		if sid != "" {
			return sid
		}
		return "system"
	}
	return "user"
}

func (al *AgentLoop) shouldSuppressOutbound(msg bus.InboundMessage, response string) bool {
	if msg.Metadata == nil {
		return false
	}
	trigger := strings.ToLower(strings.TrimSpace(msg.Metadata["trigger"]))
	if trigger != "heartbeat" {
		return false
	}

	r := strings.TrimSpace(response)
	ackToken := strings.TrimSpace(al.heartbeatAckToken)
	if ackToken == "" {
		return false
	}
	if !strings.HasPrefix(r, ackToken) {
		return false
	}

	maxChars := al.heartbeatAckMaxChars
	if maxChars <= 0 {
		maxChars = 64
	}
	return len(r) <= maxChars
}

type llmTurnLoopConfig struct {
	ctx               context.Context
	triggerMsg        bus.InboundMessage
	sessionKey        string
	toolChannel       string
	toolChatID        string
	messages          []providers.Message
	media             []string
	mediaItems        []bus.MediaItem
	enableStreaming   bool
	errorLogCode      logger.CodeID
	logDirectResponse bool
}

type llmTurnLoopResult struct {
	messages        []providers.Message
	pendingPersist  []providers.Message
	finalContent    string
	iteration       int
	attemptCount    int
	restartCount    int
	failureCode     string
	hasToolActivity bool
}

func logLLMTurnRequest(iteration, maxIterations int, providerName, activeModel string, messages []providers.Message, providerToolDefs []providers.ToolDefinition, maxTokens int, temperature float64) {
	systemPromptLen := 0
	if len(messages) > 0 {
		systemPromptLen = len(messages[0].Content)
	}
	logger.DebugCF("agent", logger.C0152, map[string]interface{}{
		"iteration":         iteration,
		"max":               maxIterations,
		"provider":          providerName,
		"model":             activeModel,
		"messages_count":    len(messages),
		"tools_count":       len(providerToolDefs),
		"max_tokens":        maxTokens,
		"temperature":       temperature,
		"system_prompt_len": systemPromptLen,
	})
	if iteration == 1 {
		logger.DebugCF("agent", logger.C0153, map[string]interface{}{
			"iteration":     iteration,
			"messages_json": formatMessagesForLog(messages),
			"tools_json":    formatToolsForLog(providerToolDefs),
		})
	}
}

func logLLMDirectResponse(iteration int, finalContent string) {
	logger.InfoCF("agent", logger.C0156, map[string]interface{}{
		"iteration":     iteration,
		"content_chars": len(finalContent),
	})
}

func logLLMToolCalls(iteration int, toolCalls []providers.ToolCall) {
	toolNames := make([]string, 0, len(toolCalls))
	for _, tc := range toolCalls {
		toolNames = append(toolNames, tc.Name)
	}
	logger.InfoCF("agent", logger.C0157, map[string]interface{}{
		"tools":     toolNames,
		"count":     len(toolNames),
		"iteration": iteration,
	})
}

func buildAssistantToolCallMessage(response *providers.LLMResponse) providers.Message {
	assistantMsg := providers.Message{
		Role:    "assistant",
		Content: response.Content,
	}
	if response == nil {
		return assistantMsg
	}
	for _, tc := range response.ToolCalls {
		argumentsJSON, _ := json.Marshal(tc.Arguments)
		assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, providers.ToolCall{
			ID:   tc.ID,
			Type: "function",
			Function: &providers.FunctionCall{
				Name:      tc.Name,
				Arguments: string(argumentsJSON),
			},
		})
	}
	return assistantMsg
}

func (al *AgentLoop) executeResponseToolCalls(cfg llmTurnLoopConfig, iteration int, response *providers.LLMResponse) []providers.Message {
	if response == nil || len(response.ToolCalls) == 0 {
		return nil
	}
	results := make([]providers.Message, 0, len(response.ToolCalls))
	for _, tc := range response.ToolCalls {
		argsJSON, _ := json.Marshal(tc.Arguments)
		logger.InfoCF("agent", logger.C0172, map[string]interface{}{
			"tool":      tc.Name,
			"args":      truncate(string(argsJSON), 200),
			"iteration": iteration,
		})
		execArgs := withToolContextArgs(tc.Name, tc.Arguments, cfg.toolChannel, cfg.toolChatID)
		toolResult, toolErr := al.executeToolCall(cfg.ctx, tc.Name, execArgs, cfg.toolChannel, cfg.toolChatID)
		if toolErr != nil {
			toolResult = fmt.Sprintf("Error: %v", toolErr)
		}
		results = append(results, providers.Message{
			Role:       "tool",
			Content:    toolResult,
			ToolCallID: tc.ID,
		})
	}
	return results
}

func (al *AgentLoop) requestLLMResponse(cfg llmTurnLoopConfig, activeProvider providers.LLMProvider, activeModel string, messages []providers.Message, providerToolDefs []providers.ToolDefinition, options map[string]interface{}) (*providers.LLMResponse, int, error) {
	if cfg.enableStreaming {
		if sp, ok := activeProvider.(providers.StreamingLLMProvider); ok {
			return al.requestStreamingLLMResponse(cfg, sp, activeProvider, activeModel, messages, providerToolDefs, options)
		}
	}
	resp, err := activeProvider.Chat(cfg.ctx, messages, providerToolDefs, activeModel, options)
	return resp, 1, err
}

func (al *AgentLoop) requestStreamingLLMResponse(cfg llmTurnLoopConfig, streamer providers.StreamingLLMProvider, fallbackProvider providers.LLMProvider, activeModel string, messages []providers.Message, providerToolDefs []providers.ToolDefinition, options map[string]interface{}) (*providers.LLMResponse, int, error) {
	streamText := ""
	lastPush := time.Now().Add(-time.Second)
	type streamResult struct {
		resp *providers.LLMResponse
		err  error
	}
	streamCtx, cancel := context.WithCancel(cfg.ctx)
	defer cancel()
	resultCh := make(chan streamResult, 1)
	var deltaMu sync.Mutex
	firstDeltaSeen := false
	lastDeltaAt := time.Now()
	go func() {
		resp, err := streamer.ChatStream(streamCtx, messages, providerToolDefs, activeModel, options, func(delta string) {
			if strings.TrimSpace(delta) == "" {
				return
			}
			deltaMu.Lock()
			firstDeltaSeen = true
			lastDeltaAt = time.Now()
			deltaMu.Unlock()
			streamText += delta
			if time.Since(lastPush) < 450*time.Millisecond {
				return
			}
			if !shouldFlushTelegramStreamSnapshot(streamText) {
				return
			}
			lastPush = time.Now()
			replyID := ""
			if cfg.triggerMsg.Metadata != nil {
				replyID = cfg.triggerMsg.Metadata["message_id"]
			}
			al.bus.PublishOutbound(bus.OutboundMessage{
				Channel:   cfg.toolChannel,
				ChatID:    cfg.toolChatID,
				Content:   streamText,
				Action:    "stream",
				ReplyToID: replyID,
			})
			al.markSessionStreamed(cfg.sessionKey)
		})
		resultCh <- streamResult{resp: resp, err: err}
	}()

	firstDeltaTimeout, idleDeltaTimeout := streamMonitorTimeouts(cfg.ctx)
	staleTriggered := false
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case result := <-resultCh:
			if result.err == nil {
				return result.resp, 1, nil
			}
			deltaMu.Lock()
			streamStarted := firstDeltaSeen
			deltaMu.Unlock()
			if staleTriggered && streamStarted {
				return nil, 1, providers.NewProviderExecutionError("stream_stale", result.err.Error(), "stream", true, "")
			}
			if !streamStarted {
				resp, err := fallbackProvider.Chat(cfg.ctx, messages, providerToolDefs, activeModel, options)
				return resp, 2, err
			}
			return nil, 1, result.err
		case <-ticker.C:
			deltaMu.Lock()
			streamStarted := firstDeltaSeen
			idleFor := time.Since(lastDeltaAt)
			deltaMu.Unlock()
			timeout := firstDeltaTimeout
			if streamStarted {
				timeout = idleDeltaTimeout
			}
			if timeout > 0 && idleFor > timeout {
				staleTriggered = true
				cancel()
			}
		case <-cfg.ctx.Done():
			cancel()
			return nil, 1, cfg.ctx.Err()
		}
	}
}

func streamMonitorTimeouts(ctx context.Context) (time.Duration, time.Duration) {
	first := 20 * time.Second
	idle := 45 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 {
			firstMin := 5 * time.Second
			idleMin := 15 * time.Second
			if remaining < firstMin {
				firstMin = maxDuration(100*time.Millisecond, remaining/4)
			}
			if remaining < idleMin {
				idleMin = maxDuration(250*time.Millisecond, remaining/3)
			}
			first = clampDuration(remaining/4, firstMin, first)
			idle = clampDuration(remaining/3, idleMin, idle)
		}
	}
	return first, idle
}

func clampDuration(value, min, max time.Duration) time.Duration {
	if value < min {
		return min
	}
	if max > 0 && value > max {
		return max
	}
	return value
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func (al *AgentLoop) maxIterationsForContext(ctx context.Context) int {
	limit := al.maxIterations
	if limit < 1 {
		limit = 1
	}
	if budget, ok := tools.SubagentIterationBudget(ctx); ok && budget > 0 {
		return budget
	}
	return limit
}

func (al *AgentLoop) runLLMTurnLoop(cfg llmTurnLoopConfig) (llmTurnLoopResult, error) {
	result := llmTurnLoopResult{
		messages:       append([]providers.Message(nil), cfg.messages...),
		pendingPersist: make([]providers.Message, 0, 16),
	}
	maxAllowed := al.maxIterationsForContext(cfg.ctx)
	if maxAllowed < 1 {
		maxAllowed = 1
	}
	toolDefs := al.filteredToolDefinitionsForContext(cfg.ctx)
	providerToolDefs := al.buildProviderToolDefs(toolDefs)
	result.messages = injectResponsesMediaParts(result.messages, cfg.media, cfg.mediaItems)

	for result.iteration < maxAllowed {
		result.iteration++
		activeProvider, activeModel, providerName, err := al.activeProviderForSession(cfg.sessionKey)
		if err != nil {
			logger.ErrorCF("agent", cfg.errorLogCode, map[string]interface{}{
				"iteration": result.iteration,
				"error":     err.Error(),
			})
			return result, fmt.Errorf("resolve active provider: %w", err)
		}
		if activeProvider == nil {
			return result, fmt.Errorf("active provider unavailable for session %s", strings.TrimSpace(cfg.sessionKey))
		}

		maxTokens := al.maxTokensForProvider(providerName)
		temperature := al.temperatureForProvider(providerName)
		logLLMTurnRequest(result.iteration, al.maxIterations, providerName, activeModel, result.messages, providerToolDefs, maxTokens, temperature)

		options := al.buildResponsesOptions(cfg.sessionKey, int64(maxTokens), temperature)
		response, attempts, err := al.requestLLMResponse(cfg, activeProvider, activeModel, result.messages, providerToolDefs, options)
		result.attemptCount += attempts

		if err != nil {
			result.failureCode = classifyLLMFailureCode(err)
			if fb, _, fallbackAttempts, ferr := al.tryFallbackProviders(cfg.ctx, cfg.triggerMsg, result.messages, providerToolDefs, err); ferr == nil && fb != nil {
				response = fb
				result.attemptCount += fallbackAttempts
				err = nil
			} else {
				result.attemptCount += fallbackAttempts
				err = ferr
				result.failureCode = classifyLLMFailureCode(err)
			}
		}
		if err != nil {
			logger.ErrorCF("agent", cfg.errorLogCode, map[string]interface{}{
				"iteration": result.iteration,
				"error":     err.Error(),
			})
			return result, fmt.Errorf("LLM call failed: %w", err)
		}

		if len(response.ToolCalls) == 0 {
			result.finalContent = response.Content
			if cfg.logDirectResponse {
				logLLMDirectResponse(result.iteration, result.finalContent)
			}
			return result, nil
		}

		logLLMToolCalls(result.iteration, response.ToolCalls)

		assistantMsg := buildAssistantToolCallMessage(response)
		result.messages = append(result.messages, assistantMsg)
		result.pendingPersist = append(result.pendingPersist, assistantMsg)
		result.hasToolActivity = true

		for _, toolResultMsg := range al.executeResponseToolCalls(cfg, result.iteration, response) {
			result.messages = append(result.messages, toolResultMsg)
			result.pendingPersist = append(result.pendingPersist, toolResultMsg)
		}
	}

	result.failureCode = "retry_limit"
	return result, fmt.Errorf("max tool iterations exceeded (%d)", maxAllowed)
}

func (al *AgentLoop) logInboundMessageStart(msg bus.InboundMessage) {
	logger.InfoCF("agent", logger.C0171, map[string]interface{}{
		"channel":     msg.Channel,
		"chat_id":     msg.ChatID,
		"sender_id":   msg.SenderID,
		"session_key": msg.SessionKey,
		"preview":     truncate(msg.Content, 80),
	})
}

func (al *AgentLoop) prepareUserMessageContext(msg bus.InboundMessage, memoryNamespace string) ([]providers.Message, string) {
	history := al.sessions.GetPromptHistory(msg.SessionKey)
	summary := al.sessions.GetSummary(msg.SessionKey)
	al.sessions.AddMessage(msg.SessionKey, "user", msg.Content)
	if explicitPref := ExtractLanguagePreference(msg.Content); explicitPref != "" {
		al.sessions.SetPreferredLanguage(msg.SessionKey, explicitPref)
	}
	preferredLang, lastLang := al.sessions.GetLanguagePreferences(msg.SessionKey)
	responseLang := DetectResponseLanguage(msg.Content, preferredLang, lastLang)
	messages := al.contextBuilder.BuildMessagesWithMemoryNamespace(
		history,
		summary,
		msg.Content,
		nil,
		msg.Channel,
		msg.ChatID,
		responseLang,
		memoryNamespace,
	)
	return messages, responseLang
}

func (al *AgentLoop) finalizeUserMessage(sessionKey, responseLang string, pendingPersist []providers.Message, finalContent string) {
	for _, persisted := range pendingPersist {
		al.sessions.AddMessageFull(sessionKey, persisted)
	}
	al.sessions.AddMessageFull(sessionKey, providers.Message{
		Role:    "assistant",
		Content: finalContent,
	})
	al.sessions.SetLastLanguage(sessionKey, responseLang)
	al.enqueueSessionCompaction(sessionKey)
}

func (al *AgentLoop) prepareSystemMessageContext(sessionKey string, msg bus.InboundMessage, originChannel, originChatID string) ([]providers.Message, string) {
	history := al.sessions.GetPromptHistory(sessionKey)
	summary := al.sessions.GetSummary(sessionKey)
	preferredLang, lastLang := al.sessions.GetLanguagePreferences(sessionKey)
	responseLang := DetectResponseLanguage(msg.Content, preferredLang, lastLang)
	messages := al.contextBuilder.BuildMessages(
		history,
		summary,
		msg.Content,
		nil,
		originChannel,
		originChatID,
		responseLang,
	)
	return messages, responseLang
}

func (al *AgentLoop) finalizeSystemMessage(sessionKey, responseLang string, msg bus.InboundMessage, pendingPersist []providers.Message, finalContent string) {
	al.sessions.AddMessage(sessionKey, "user", fmt.Sprintf("[System: %s] %s", msg.SenderID, msg.Content))
	for _, persisted := range pendingPersist {
		al.sessions.AddMessageFull(sessionKey, persisted)
	}
	al.sessions.AddMessageFull(sessionKey, providers.Message{
		Role:    "assistant",
		Content: finalContent,
	})
	al.sessions.SetLastLanguage(sessionKey, responseLang)
	al.enqueueSessionCompaction(sessionKey)
}

func (al *AgentLoop) startSpecTaskForMessage(msg bus.InboundMessage) specCodingTaskRef {
	specTaskRef := specCodingTaskRef{}
	if err := al.maybeEnsureSpecCodingDocs(msg.Content); err != nil {
		logger.WarnCF("agent", logger.C0172, map[string]interface{}{
			"session_key": msg.SessionKey,
			"error":       err.Error(),
		})
	}
	taskRef, err := al.maybeStartSpecCodingTask(msg.Content)
	if err != nil {
		logger.WarnCF("agent", logger.C0172, map[string]interface{}{
			"session_key": msg.SessionKey,
			"error":       err.Error(),
		})
		return specTaskRef
	}
	return normalizeSpecCodingTaskRef(taskRef)
}

func (al *AgentLoop) reopenSpecTaskOnError(specTaskRef specCodingTaskRef, msg bus.InboundMessage, err error) {
	if specTaskRef.Summary == "" || err == nil {
		return
	}
	if rerr := al.maybeReopenSpecCodingTask(specTaskRef, msg.Content, err.Error()); rerr != nil {
		logger.WarnCF("agent", logger.C0172, map[string]interface{}{
			"session_key": msg.SessionKey,
			"error":       rerr.Error(),
		})
	}
}

func (al *AgentLoop) completeSpecTaskOnSuccess(specTaskRef specCodingTaskRef, msg bus.InboundMessage, output string) {
	if specTaskRef.Summary == "" {
		return
	}
	if err := al.maybeCompleteSpecCodingTask(specTaskRef, output); err != nil {
		logger.WarnCF("agent", logger.C0172, map[string]interface{}{
			"session_key": msg.SessionKey,
			"error":       err.Error(),
		})
	}
}

func (al *AgentLoop) recoverFinalContentAfterToolCalls(ctx context.Context, sessionKey string, messages []providers.Message, hasToolActivity bool) string {
	if !hasToolActivity {
		return ""
	}
	activeProvider, activeModel, providerName, err := al.activeProviderForSession(sessionKey)
	if err != nil {
		logger.WarnCF("agent", logger.C0172, map[string]interface{}{
			"session_key": sessionKey,
			"error":       err.Error(),
		})
		return ""
	}
	if activeProvider == nil {
		return ""
	}
	options := al.buildResponsesOptionsForProvider(sessionKey, providerName, int64(al.maxTokensForProvider(providerName)), 0.2)
	forced, ferr := activeProvider.Chat(ctx, messages, nil, activeModel, options)
	if ferr != nil || forced == nil {
		if ferr != nil {
			logger.WarnCF("agent", logger.C0172, map[string]interface{}{
				"session_key": sessionKey,
				"error":       ferr.Error(),
			})
		}
		return ""
	}
	return forced.Content
}

func sanitizeUserVisibleContent(finalContent string, iteration int) string {
	userContent := thinkTagPattern.ReplaceAllString(finalContent, "")
	if userContent == "" && finalContent != "" && iteration == 1 {
		return "Thinking process completed."
	}
	return userContent
}

func (al *AgentLoop) finalizeUserTurnResponse(ctx context.Context, msg bus.InboundMessage, responseLang string, loopResult llmTurnLoopResult) (string, string) {
	finalContent := loopResult.finalContent
	if finalContent == "" {
		if recovered := al.recoverFinalContentAfterToolCalls(ctx, msg.SessionKey, loopResult.messages, loopResult.hasToolActivity); recovered != "" {
			finalContent = recovered
		}
	}
	userContent := sanitizeUserVisibleContent(finalContent, loopResult.iteration)
	al.finalizeUserMessage(msg.SessionKey, responseLang, loopResult.pendingPersist, userContent)
	return finalContent, userContent
}

func (al *AgentLoop) maybeHandleAutoRoute(ctx context.Context, msg bus.InboundMessage, specTaskRef specCodingTaskRef) (string, error, bool) {
	routed, ok, routeErr := al.maybeAutoRoute(ctx, msg)
	if !ok {
		return "", nil, false
	}
	if routeErr != nil {
		al.reopenSpecTaskOnError(specTaskRef, msg, routeErr)
		return routed, routeErr, true
	}
	al.completeSpecTaskOnSuccess(specTaskRef, msg, routed)
	return routed, nil, true
}

func loadHeartbeatAckToken(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return ""
	}
	parse := func(text string) string {
		for _, line := range strings.Split(text, "\n") {
			t := strings.TrimSpace(line)
			if t == "" {
				continue
			}
			raw := strings.TrimLeft(t, "-*# ")
			lower := strings.ToLower(raw)
			if !strings.HasPrefix(lower, "heartbeat_ack_token:") {
				continue
			}
			v := strings.TrimSpace(raw[len("heartbeat_ack_token:"):])
			v = strings.Trim(v, "`\"' ")
			if v != "" {
				return v
			}
		}
		return ""
	}
	if b, err := os.ReadFile(filepath.Join(workspace, "AGENTS.md")); err == nil {
		if token := parse(string(b)); token != "" {
			return token
		}
	}
	if b, err := os.ReadFile(filepath.Join(workspace, "HEARTBEAT.md")); err == nil {
		if token := parse(string(b)); token != "" {
			return token
		}
	}
	return ""
}

func (al *AgentLoop) prepareOutbound(msg bus.InboundMessage, response string) (bus.OutboundMessage, bool) {
	if shouldDropNoReply(response) {
		if fallback, ok := fallbackSubagentNotification(msg); ok {
			response = fallback
		} else {
			return bus.OutboundMessage{}, false
		}
	}
	currentMsgID := ""
	if msg.Metadata != nil {
		currentMsgID = msg.Metadata["message_id"]
	}
	clean, replyToID := parseReplyTag(response, currentMsgID)
	clean = strings.TrimSpace(clean)
	if clean == "" {
		if fallback, ok := fallbackSubagentNotification(msg); ok {
			clean = fallback
		} else {
			return bus.OutboundMessage{}, false
		}
	}
	if al.shouldSuppressOutbound(msg, clean) {
		return bus.OutboundMessage{}, false
	}
	outbound := bus.OutboundMessage{
		Channel:       msg.Channel,
		ChatID:        msg.ChatID,
		Content:       clean,
		ReplyToID:     strings.TrimSpace(replyToID),
		ReplyToSender: strings.TrimSpace(msg.SenderID),
	}
	if msg.Channel == "system" {
		outbound.Channel, outbound.ChatID = resolveSystemOrigin(msg.ChatID)
	}
	return outbound, true
}

func (al *AgentLoop) ProcessDirect(ctx context.Context, content, sessionKey string) (string, error) {
	return al.ProcessDirectWithOptions(ctx, content, sessionKey, "cli", "direct", "main", nil)
}

func (al *AgentLoop) ProcessDirectWithOptions(ctx context.Context, content, sessionKey, channel, chatID, memoryNamespace string, toolAllowlist []string) (string, error) {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		channel = "cli"
	}
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		chatID = "direct"
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		sessionKey = "main"
	}
	al.syncSessionDefaultProvider(sessionKey)
	ns := normalizeMemoryNamespace(memoryNamespace)
	var metadata map[string]string
	if ns != "main" {
		metadata = map[string]string{
			"memory_namespace": ns,
			"memory_ns":        ns,
		}
	}
	ctx = withMemoryNamespaceContext(ctx, ns)
	ctx = withToolAllowlistContext(ctx, toolAllowlist)

	msg := bus.InboundMessage{
		Channel:    channel,
		SenderID:   "user",
		ChatID:     chatID,
		Content:    content,
		SessionKey: sessionKey,
		Metadata:   metadata,
	}

	return al.processPlannedMessage(ctx, msg)
}

func (al *AgentLoop) GetSessionHistory(sessionKey string) []providers.Message {
	return al.sessions.GetHistory(sessionKey)
}

func (al *AgentLoop) GetSessionHistoryWindow(sessionKey string, around, before, after, limit int) []providers.Message {
	return al.sessions.GetHistoryWindow(sessionKey, around, before, after, limit)
}

func (al *AgentLoop) SearchSessions(query string, kinds []string, excludeKey string, limit int) []session.SessionSearchResult {
	return al.sessions.Search(query, kinds, excludeKey, limit)
}

func (al *AgentLoop) processMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	if msg.SessionKey == "" {
		msg.SessionKey = "main"
	}
	memoryNamespace := resolveInboundMemoryNamespace(msg)
	ctx = withMemoryNamespaceContext(ctx, memoryNamespace)
	release, err := al.acquireSessionResources(ctx, &msg)
	if err != nil {
		return "", err
	}
	defer release()
	al.syncSessionDefaultProvider(msg.SessionKey)
	al.logInboundMessageStart(msg)

	// Route system messages to processSystemMessage
	if msg.Channel == "system" {
		return al.processSystemMessage(ctx, msg)
	}
	specTaskRef := al.startSpecTaskForMessage(msg)
	if routed, routeErr, handled := al.maybeHandleAutoRoute(ctx, msg, specTaskRef); handled {
		return routed, routeErr
	}

	messages, responseLang := al.prepareUserMessageContext(msg, memoryNamespace)

	loopResult, err := al.runLLMTurnLoop(llmTurnLoopConfig{
		ctx:               ctx,
		triggerMsg:        msg,
		sessionKey:        msg.SessionKey,
		toolChannel:       msg.Channel,
		toolChatID:        msg.ChatID,
		messages:          messages,
		media:             msg.Media,
		mediaItems:        msg.MediaItems,
		enableStreaming:   msg.Channel == "telegram" && al.telegramStreaming,
		errorLogCode:      logger.C0155,
		logDirectResponse: true,
	})
	if err != nil {
		tools.RecordSubagentExecutionStats(ctx, tools.SubagentExecutionStats{
			Iterations:  loopResult.iteration,
			Attempts:    loopResult.attemptCount,
			Restarts:    loopResult.restartCount,
			FailureCode: classifyLLMFailureCode(err),
		})
		al.reopenSpecTaskOnError(specTaskRef, msg, err)
		return "", err
	}
	tools.RecordSubagentExecutionStats(ctx, tools.SubagentExecutionStats{
		Iterations: loopResult.iteration,
		Attempts:   loopResult.attemptCount,
		Restarts:   loopResult.restartCount,
	})
	finalContent, userContent := al.finalizeUserTurnResponse(ctx, msg, responseLang, loopResult)

	// Log response preview (original content)
	responsePreview := truncate(finalContent, 120)
	logger.InfoCF("agent", logger.C0173,
		map[string]interface{}{
			"channel":      msg.Channel,
			"sender_id":    msg.SenderID,
			"preview":      responsePreview,
			"iterations":   loopResult.iteration,
			"final_length": len(finalContent),
			"user_length":  len(userContent),
		})

	al.completeSpecTaskOnSuccess(specTaskRef, msg, userContent)
	al.appendDailySummaryLog(msg, userContent)
	return userContent, nil
}

func (al *AgentLoop) acquireSessionResources(ctx context.Context, msg *bus.InboundMessage) (func(), error) {
	if al == nil || msg == nil || al.sessionScheduler == nil {
		return func() {}, nil
	}
	keys, cleaned := al.resolveMessageResourceKeys(msg)
	msg.Content = cleaned
	return al.sessionScheduler.Acquire(ctx, msg.SessionKey, keys)
}

func (al *AgentLoop) resolveMessageResourceKeys(msg *bus.InboundMessage) ([]string, string) {
	if msg == nil {
		return nil, ""
	}
	content := msg.Content
	if msg.Metadata != nil {
		if raw := strings.TrimSpace(msg.Metadata["resource_keys"]); raw != "" {
			if explicit := scheduling.ParseResourceKeyList(raw); len(explicit) > 0 {
				return explicit, content
			}
		}
	}
	if explicit, cleaned, ok := scheduling.ExtractResourceKeysDirective(content); ok {
		if strings.TrimSpace(cleaned) != "" {
			content = cleaned
		}
		return explicit, content
	}
	return scheduling.DeriveResourceKeys(content), content
}

func (al *AgentLoop) appendDailySummaryLog(msg bus.InboundMessage, response string) {
	if strings.TrimSpace(al.workspace) == "" {
		return
	}
	respText := strings.TrimSpace(response)
	if respText == "" {
		return
	}
	// Avoid noisy heartbeat/system boilerplate.
	lc := strings.ToLower(strings.TrimSpace(msg.Content))
	if strings.Contains(lc, "heartbeat") && strings.Contains(strings.ToLower(respText), "heartbeat_ok") {
		return
	}
	namespace := resolveInboundMemoryNamespace(msg)
	detailLine := al.buildDailySummaryEntry(msg, namespace, respText)
	ms := NewMemoryStoreWithNamespace(al.workspace, namespace)
	if err := ms.AppendToday(detailLine); err != nil {
		logger.WarnCF("agent", logger.C0158, map[string]interface{}{logger.FieldError: err.Error()})
	}
	if namespace != "main" {
		mainLine := al.buildMainMemorySubagentEntry(msg, namespace, respText)
		if err := NewMemoryStore(al.workspace).AppendToday(mainLine); err != nil {
			logger.WarnCF("agent", logger.C0158, map[string]interface{}{"target": "main", logger.FieldError: err.Error()})
		}
	}
}

func (al *AgentLoop) buildDailySummaryEntry(msg bus.InboundMessage, namespace, response string) string {
	title := al.dailySummaryTitle(msg, namespace)
	return fmt.Sprintf("## %s %s\n\n- Did: %s\n- Channel: %s\n- Session: %s",
		time.Now().Format("15:04"),
		title,
		truncate(strings.ReplaceAll(strings.TrimSpace(response), "\n", " "), 320),
		strings.TrimSpace(msg.Channel),
		strings.TrimSpace(msg.SessionKey),
	)
}

func (al *AgentLoop) buildMainMemorySubagentEntry(msg bus.InboundMessage, namespace, response string) string {
	title := al.dailySummaryTitle(msg, namespace)
	return fmt.Sprintf("## %s %s\n\n- Subagent: %s\n- Did: %s\n- Session: %s",
		time.Now().Format("15:04"),
		title,
		al.memoryAgentTitle(namespace),
		truncate(strings.ReplaceAll(strings.TrimSpace(response), "\n", " "), 220),
		strings.TrimSpace(msg.SessionKey),
	)
}

func (al *AgentLoop) dailySummaryTitle(msg bus.InboundMessage, namespace string) string {
	agentName := al.memoryAgentTitle(namespace)
	taskTitle := summarizeMemoryTaskTitle(msg.Content)
	if taskTitle == "" {
		return agentName
	}
	return fmt.Sprintf("%s | %s", agentName, taskTitle)
}

func (al *AgentLoop) memoryAgentTitle(namespace string) string {
	ns := normalizeMemoryNamespace(namespace)
	if ns == "main" {
		return "Main Agent"
	}
	cfg := runtimecfg.Get()
	if cfg != nil {
		if subcfg, ok := cfg.Agents.Subagents[ns]; ok {
			if name := strings.TrimSpace(subcfg.DisplayName); name != "" {
				return name
			}
		}
	}
	return strings.ReplaceAll(ns, "-", " ")
}

func summarizeMemoryTaskTitle(content string) string {
	text := strings.TrimSpace(content)
	if text == "" {
		return ""
	}
	if idx := strings.Index(text, "Task:\n"); idx >= 0 {
		text = strings.TrimSpace(text[idx+len("Task:\n"):])
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if idx := strings.Index(text, "\n"); idx >= 0 {
		text = text[:idx]
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if len(text) > 72 {
		return strings.TrimSpace(text[:69]) + "..."
	}
	return text
}

func (al *AgentLoop) processSystemMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	// Verify this is a system message
	if msg.Channel != "system" {
		return "", fmt.Errorf("processSystemMessage called with non-system message channel: %s", msg.Channel)
	}

	logger.InfoCF("agent", logger.C0159,
		map[string]interface{}{
			"sender_id": msg.SenderID,
			"chat_id":   msg.ChatID,
		})

	if al.handleSubagentSystemMessage(msg) {
		return "", nil
	}

	originChannel, originChatID := resolveSystemOrigin(msg.ChatID)

	// Use the origin session for context
	sessionKey := fmt.Sprintf("%s:%s", originChannel, originChatID)

	messages, responseLang := al.prepareSystemMessageContext(sessionKey, msg, originChannel, originChatID)

	loopResult, err := al.runLLMTurnLoop(llmTurnLoopConfig{
		ctx:               ctx,
		triggerMsg:        msg,
		sessionKey:        sessionKey,
		toolChannel:       originChannel,
		toolChatID:        originChatID,
		messages:          messages,
		errorLogCode:      logger.C0162,
		logDirectResponse: false,
	})
	if err != nil {
		return "", err
	}
	iteration := loopResult.iteration
	finalContent := loopResult.finalContent
	pendingPersist := loopResult.pendingPersist

	if finalContent == "" {
		finalContent = "Background task completed."
	}

	al.finalizeSystemMessage(sessionKey, responseLang, msg, pendingPersist, finalContent)

	logger.InfoCF("agent", logger.C0163,
		map[string]interface{}{
			"iterations":   iteration,
			"final_length": len(finalContent),
		})

	return finalContent, nil
}

func (al *AgentLoop) handleSubagentSystemMessage(msg bus.InboundMessage) bool {
	if !isSubagentSystemMessage(msg) {
		return false
	}
	reason := ""
	agentID := ""
	status := ""
	taskSummary := ""
	resultSummary := ""
	if msg.Metadata != nil {
		reason = strings.ToLower(strings.TrimSpace(msg.Metadata["notify_reason"]))
		agentID = strings.TrimSpace(msg.Metadata["agent_id"])
		status = strings.TrimSpace(msg.Metadata["status"])
	}
	if agentID == "" {
		agentID = strings.TrimSpace(strings.TrimPrefix(msg.SenderID, "subagent:"))
	}
	if taskSummary == "" || resultSummary == "" {
		taskSummary, resultSummary = parseSubagentSystemContent(msg.Content)
	}
	al.enqueueSubagentDigest(msg, subagentDigestItem{
		agentID:       agentID,
		reason:        reason,
		status:        status,
		taskSummary:   taskSummary,
		resultSummary: resultSummary,
	})
	return true
}

// truncate returns a truncated version of s with at most maxLen characters.
// If the string is truncated, "..." is appended to indicate truncation.
// If the string fits within maxLen, it is returned unchanged.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Reserve 3 chars for "..."
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func (al *AgentLoop) buildProviderToolDefs(toolDefs []map[string]interface{}) []providers.ToolDefinition {
	providerToolDefs := make([]providers.ToolDefinition, 0, len(toolDefs))
	for _, td := range toolDefs {
		fnRaw, ok := td["function"].(map[string]interface{})
		if !ok {
			continue
		}
		name := tools.MapStringArg(fnRaw, "name")
		description := tools.MapStringArg(fnRaw, "description")
		params, _ := fnRaw["parameters"].(map[string]interface{})
		if strings.TrimSpace(name) == "" {
			continue
		}
		if params == nil {
			params = map[string]interface{}{}
		}
		providerToolDefs = append(providerToolDefs, providers.ToolDefinition{
			Type: "function",
			Function: providers.ToolFunctionDefinition{
				Name:        name,
				Description: description,
				Parameters:  params,
			},
		})
	}
	return providerToolDefs
}

func (al *AgentLoop) filteredToolDefinitionsForContext(ctx context.Context) []map[string]interface{} {
	if al == nil || al.tools == nil {
		return nil
	}
	return filterToolDefinitionsByContext(ctx, al.tools.GetDefinitions())
}

func filterToolDefinitionsByContext(ctx context.Context, toolDefs []map[string]interface{}) []map[string]interface{} {
	allow := toolAllowlistFromContext(ctx)
	if len(allow) == 0 {
		return toolDefs
	}

	filtered := make([]map[string]interface{}, 0, len(toolDefs))
	for _, td := range toolDefs {
		fnRaw, ok := td["function"].(map[string]interface{})
		if !ok {
			continue
		}
		name := strings.ToLower(tools.MapStringArg(fnRaw, "name"))
		if name == "" {
			continue
		}
		if isToolNameAllowed(allow, name) || isImplicitlyAllowedSubagentTool(name) {
			filtered = append(filtered, td)
		}
	}
	return filtered
}

func (al *AgentLoop) buildResponsesOptions(sessionKey string, maxTokens int64, temperature float64) map[string]interface{} {
	providerName := strings.TrimSpace(al.getSessionProvider(sessionKey))
	if providerName == "" && len(al.providerNames) > 0 {
		providerName = al.providerNames[0]
	}
	return al.buildResponsesOptionsForProvider(sessionKey, providerName, maxTokens, temperature)
}

func (al *AgentLoop) buildResponsesOptionsForProvider(sessionKey, providerName string, maxTokens int64, temperature float64) map[string]interface{} {
	if maxTokens <= 0 {
		maxTokens = int64(al.maxTokensForProvider(providerName))
	}
	if math.IsNaN(temperature) {
		temperature = al.temperatureForProvider(providerName)
	}
	options := map[string]interface{}{
		"max_tokens":  maxTokens,
		"temperature": temperature,
	}
	if strings.EqualFold(strings.TrimSpace(providerName), "codex") {
		if key := strings.TrimSpace(sessionKey); key != "" {
			options["codex_execution_session"] = key
		}
	}
	responsesCfg := al.responsesConfigForProvider(providerName)
	responseTools := make([]map[string]interface{}, 0, 2)
	if responsesCfg.WebSearchEnabled {
		webTool := map[string]interface{}{"type": "web_search"}
		if contextSize := strings.TrimSpace(responsesCfg.WebSearchContextSize); contextSize != "" {
			webTool["search_context_size"] = contextSize
		}
		responseTools = append(responseTools, webTool)
	}
	if len(responsesCfg.FileSearchVectorStoreIDs) > 0 {
		fileSearch := map[string]interface{}{
			"type":             "file_search",
			"vector_store_ids": responsesCfg.FileSearchVectorStoreIDs,
		}
		if responsesCfg.FileSearchMaxNumResults > 0 {
			fileSearch["max_num_results"] = responsesCfg.FileSearchMaxNumResults
		}
		responseTools = append(responseTools, fileSearch)
	}
	if len(responseTools) > 0 {
		options["responses_tools"] = responseTools
	}
	if len(responsesCfg.Include) > 0 {
		options["responses_include"] = responsesCfg.Include
	}
	if responsesCfg.StreamIncludeUsage {
		options["responses_stream_options"] = map[string]interface{}{"include_usage": true}
	}
	return options
}

func (al *AgentLoop) maxTokensForProvider(name string) int {
	if al == nil {
		return 8192
	}
	providerName := strings.TrimSpace(name)
	if providerName != "" {
		if limit, ok := al.providerMaxTokens[providerName]; ok && limit > 0 {
			return limit
		}
	}
	if al.maxTokens > 0 {
		return al.maxTokens
	}
	return 8192
}

func (al *AgentLoop) maxTokensForSession(sessionKey string) int {
	if al == nil {
		return 8192
	}
	name := strings.TrimSpace(al.getSessionProvider(sessionKey))
	if name == "" && len(al.providerNames) > 0 {
		name = al.providerNames[0]
	}
	return al.maxTokensForProvider(name)
}

func (al *AgentLoop) temperatureForProvider(name string) float64 {
	if al == nil {
		return 0.7
	}
	providerName := strings.TrimSpace(name)
	if providerName != "" {
		if value, ok := al.providerTemperatures[providerName]; ok && value != 0 {
			return value
		}
	}
	if al.temperature != 0 {
		return al.temperature
	}
	return 0.7
}

func (al *AgentLoop) temperatureForSession(sessionKey string) float64 {
	if al == nil {
		return 0.7
	}
	name := strings.TrimSpace(al.getSessionProvider(sessionKey))
	if name == "" && len(al.providerNames) > 0 {
		name = al.providerNames[0]
	}
	return al.temperatureForProvider(name)
}

func (al *AgentLoop) responsesConfigForSession(sessionKey string) config.ProviderResponsesConfig {
	if al == nil {
		return config.ProviderResponsesConfig{}
	}
	name := strings.TrimSpace(al.getSessionProvider(sessionKey))
	if name == "" && len(al.providerNames) > 0 {
		name = al.providerNames[0]
	}
	return al.responsesConfigForProvider(name)
}

func (al *AgentLoop) responsesConfigForProvider(name string) config.ProviderResponsesConfig {
	if al == nil {
		return config.ProviderResponsesConfig{}
	}
	if name == "" {
		return config.ProviderResponsesConfig{}
	}
	if cfg, ok := al.providerResponses[name]; ok {
		return cfg
	}
	return config.ProviderResponsesConfig{}
}

func injectResponsesMediaParts(messages []providers.Message, media []string, mediaItems []bus.MediaItem) []providers.Message {
	if len(messages) == 0 || (len(media) == 0 && len(mediaItems) == 0) {
		return messages
	}
	last := len(messages) - 1
	if strings.ToLower(strings.TrimSpace(messages[last].Role)) != "user" {
		return messages
	}

	parts := make([]providers.MessageContentPart, 0, 1+len(media)+len(mediaItems))
	if strings.TrimSpace(messages[last].Content) != "" {
		parts = append(parts, providers.MessageContentPart{
			Type: "input_text",
			Text: messages[last].Content,
		})
	}

	// Fallback-only handling for raw media refs. Prefer structured media_items when present.
	if len(mediaItems) == 0 {
		for _, ref := range media {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			if isResponsesFileID(ref) {
				parts = append(parts, providers.MessageContentPart{
					Type:   "input_image",
					FileID: ref,
				})
				continue
			}
			if !isRemoteReference(ref) {
				continue
			}
			parts = append(parts, providers.MessageContentPart{
				Type:     "input_image",
				ImageURL: ref,
			})
		}
	}

	for _, item := range mediaItems {
		typ := strings.ToLower(strings.TrimSpace(item.Type))
		ref := strings.TrimSpace(item.Ref)
		path := strings.TrimSpace(item.Path)
		src := ref
		if src == "" {
			src = path
		}
		switch {
		case strings.Contains(typ, "image"):
			part := providers.MessageContentPart{Type: "input_image"}
			if isResponsesFileID(src) {
				part.FileID = src
			} else if isRemoteReference(src) {
				part.ImageURL = src
			}
			if part.FileID != "" || part.ImageURL != "" {
				parts = append(parts, part)
			}
		case strings.Contains(typ, "file"), strings.Contains(typ, "document"), strings.Contains(typ, "audio"), strings.Contains(typ, "video"):
			part := providers.MessageContentPart{Type: "input_file"}
			if isResponsesFileID(src) {
				part.FileID = src
			} else if isRemoteReference(src) {
				part.FileURL = src
			}
			if part.FileID != "" || part.FileURL != "" {
				parts = append(parts, part)
			}
		}
	}

	if len(parts) == 0 {
		return messages
	}
	messages[last].ContentParts = parts
	return messages
}

func isResponsesFileID(ref string) bool {
	return strings.HasPrefix(strings.TrimSpace(ref), "file_")
}

func isRemoteReference(ref string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(ref))
	return strings.HasPrefix(trimmed, "http://") ||
		strings.HasPrefix(trimmed, "https://") ||
		strings.HasPrefix(trimmed, "data:")
}

func (al *AgentLoop) ensureCompactionRunnerStarted() {
	if al == nil || !al.compactionEnabled || al.compactionRunner == nil {
		return
	}
	al.compactionRunner.Start(func(stop <-chan struct{}) {
		al.runCompactionLoop(stop)
	})
}

func (al *AgentLoop) signalCompactionWorker() {
	if al == nil || al.compactionSignal == nil {
		return
	}
	select {
	case al.compactionSignal <- struct{}{}:
	default:
	}
}

func (al *AgentLoop) enqueueSessionCompaction(sessionKey string) {
	if al == nil || !al.compactionEnabled {
		return
	}
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return
	}
	al.compactionMu.Lock()
	if _, ok := al.compactionInflight[key]; ok {
		al.compactionDirty[key] = struct{}{}
		al.compactionMu.Unlock()
		al.signalCompactionWorker()
		return
	}
	if _, ok := al.compactionQueued[key]; ok {
		al.compactionMu.Unlock()
		al.signalCompactionWorker()
		return
	}
	al.compactionQueue = append(al.compactionQueue, key)
	al.compactionQueued[key] = struct{}{}
	al.compactionMu.Unlock()
	al.ensureCompactionRunnerStarted()
	al.signalCompactionWorker()
}

func (al *AgentLoop) dequeueSessionCompaction() (string, bool) {
	if al == nil {
		return "", false
	}
	al.compactionMu.Lock()
	defer al.compactionMu.Unlock()
	if len(al.compactionQueue) == 0 {
		return "", false
	}
	key := al.compactionQueue[0]
	al.compactionQueue = al.compactionQueue[1:]
	delete(al.compactionQueued, key)
	al.compactionInflight[key] = struct{}{}
	return key, true
}

func (al *AgentLoop) finishSessionCompaction(sessionKey string, retry bool) {
	if al == nil {
		return
	}
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return
	}
	shouldWake := false
	al.compactionMu.Lock()
	delete(al.compactionInflight, key)
	if _, dirty := al.compactionDirty[key]; dirty {
		delete(al.compactionDirty, key)
		retry = true
	}
	if retry {
		if _, queued := al.compactionQueued[key]; !queued {
			al.compactionQueue = append(al.compactionQueue, key)
			al.compactionQueued[key] = struct{}{}
			shouldWake = true
		}
	}
	al.compactionMu.Unlock()
	if shouldWake {
		al.signalCompactionWorker()
	}
}

func (al *AgentLoop) runCompactionLoop(stop <-chan struct{}) {
	if al == nil {
		return
	}
	for {
		key, ok := al.dequeueSessionCompaction()
		if !ok {
			select {
			case <-stop:
				return
			case <-al.compactionSignal:
				continue
			}
		}
		retry := al.runSessionCompactionJob(stop, key)
		al.finishSessionCompaction(key, retry)
	}
}

func (al *AgentLoop) runSessionCompactionJob(stop <-chan struct{}, sessionKey string) bool {
	if al == nil || strings.TrimSpace(sessionKey) == "" {
		return false
	}
	jobCtx, cancel := al.newBackgroundCompactionContext(stop, sessionKey)
	defer cancel()
	applied, changed, needsRetry := al.compactSessionIfNeeded(jobCtx, sessionKey)
	if changed && !applied {
		return true
	}
	return needsRetry
}

func (al *AgentLoop) newBackgroundCompactionContext(stop <-chan struct{}, sessionKey string) (context.Context, context.CancelFunc) {
	base, baseCancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		select {
		case <-stop:
			baseCancel()
		case <-done:
		}
	}()
	timeout := al.compactionTimeoutForSession(sessionKey)
	ctx, timeoutCancel := context.WithTimeout(base, timeout)
	return ctx, func() {
		close(done)
		timeoutCancel()
		baseCancel()
	}
}

func (al *AgentLoop) compactionTimeoutForSession(sessionKey string) time.Duration {
	timeout := 30 * time.Second
	if al != nil && al.cfg != nil {
		name := strings.TrimSpace(al.getSessionProvider(sessionKey))
		if name == "" && len(al.providerNames) > 0 {
			name = al.providerNames[0]
		}
		if pc, ok := config.ProviderConfigByName(al.cfg, name); ok && pc.TimeoutSec > 0 {
			providerTimeout := time.Duration(pc.TimeoutSec) * time.Second
			if providerTimeout < timeout {
				timeout = providerTimeout
			}
		}
	}
	if timeout < 5*time.Second {
		timeout = 5 * time.Second
	}
	return timeout
}

func (al *AgentLoop) shouldCompactSnapshot(ctx context.Context, sessionKey string, snapshot session.SessionCompactionSnapshot) (bool, int, float64) {
	trigger := al.compactionTrigger
	if trigger <= 0 {
		trigger = 60
	}
	pressure := al.estimateCompactionPressure(ctx, sessionKey, snapshot.History, snapshot.Summary)
	if len(snapshot.History) <= trigger && pressure < al.compactionPressureThreshold {
		return false, trigger, pressure
	}
	return true, trigger, pressure
}

// GetStartupInfo returns information about loaded tools and skills for logging.
func (al *AgentLoop) compactSessionIfNeeded(ctx context.Context, sessionKey string) (applied bool, changed bool, needsRetry bool) {
	if !al.compactionEnabled {
		return false, false, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	snapshot := al.sessions.CompactionSnapshot(sessionKey)
	if len(snapshot.History) == 0 {
		return false, false, false
	}
	shouldCompact, trigger, _ := al.shouldCompactSnapshot(ctx, sessionKey, snapshot)
	if !shouldCompact {
		return false, false, false
	}
	keepRecent := al.compactionKeepCount(len(snapshot.History), trigger)
	if keepRecent >= len(snapshot.History) {
		return false, false, false
	}
	removed := len(snapshot.History) - keepRecent
	older := append([]providers.Message(nil), snapshot.History[:removed]...)
	recent := append([]providers.Message(nil), snapshot.History[removed:]...)
	summary := al.buildCompactionSummary(ctx, sessionKey, older, snapshot.Summary)
	if ctx.Err() != nil {
		return false, false, true
	}
	if summary == "" {
		summary = fmt.Sprintf("Key Facts\n- Runtime compaction removed %d older messages.\n\nDecisions\n\nOpen Items\n\nNext Steps", removed)
	}
	if al.sessions.ApplyCompactionIfUnchanged(sessionKey, snapshot.NextSeq, snapshot.Summary, recent, summary) {
		return true, false, false
	}
	next := al.sessions.CompactionSnapshot(sessionKey)
	shouldRetry, _, _ := al.shouldCompactSnapshot(context.Background(), sessionKey, next)
	return false, true, shouldRetry
}

// RunStartupSelfCheckAllSessions runs startup compaction checks across loaded sessions.
func (al *AgentLoop) RunStartupSelfCheckAllSessions(ctx context.Context) StartupCompactionReport {
	report := StartupCompactionReport{TotalSessions: al.sessions.Count()}
	if !al.compactionEnabled {
		return report
	}

	for _, key := range al.sessions.Keys() {
		select {
		case <-ctx.Done():
			return report
		default:
		}
		jobCtx, cancel := context.WithTimeout(ctx, al.compactionTimeoutForSession(key))
		applied, _, _ := al.compactSessionIfNeeded(jobCtx, key)
		cancel()
		if applied {
			report.CompactedSessions++
		}
	}

	return report
}

func (al *AgentLoop) buildCompactionSummary(ctx context.Context, sessionKey string, older []providers.Message, existingSummary string) string {
	if al == nil || al.provider == nil || len(older) == 0 {
		return strings.TrimSpace(existingSummary)
	}
	pruned := pruneCompactionMessages(older, al.compactionMaxTranscriptChars)
	if compactor, ok := al.provider.(providers.ResponsesCompactor); ok && compactor.SupportsResponsesCompact() {
		if summary, err := compactor.BuildSummaryViaResponsesCompact(ctx, al.model, existingSummary, pruned, al.compactionMaxSummaryChars); err == nil {
			return strings.TrimSpace(summary)
		}
	}

	payload, err := json.Marshal(compactionTranscript(pruned))
	if err != nil {
		return strings.TrimSpace(existingSummary)
	}
	resp, err := al.provider.Chat(ctx, []providers.Message{
		{
			Role: "system",
			Content: "You are compacting a previous conversation window for a future handoff. " +
				"Return concise markdown with exactly these sections: Key Facts, Decisions, Open Items, Next Steps. " +
				"Preserve concrete decisions, file paths, errors, and pending work. Do not address the user.",
		},
		{
			Role:    "user",
			Content: "Existing summary:\n" + strings.TrimSpace(existingSummary) + "\n\nEarlier conversation JSON:\n" + string(payload),
		},
	}, nil, al.model, map[string]interface{}{"max_tokens": 900})
	if err != nil || resp == nil {
		return strings.TrimSpace(existingSummary)
	}
	out := strings.TrimSpace(resp.Content)
	if al.compactionMaxSummaryChars > 0 && len(out) > al.compactionMaxSummaryChars {
		out = out[:al.compactionMaxSummaryChars]
	}
	return out
}

func pruneCompactionMessages(messages []providers.Message, maxTranscriptChars int) []providers.Message {
	out := make([]providers.Message, 0, len(messages))
	remaining := maxTranscriptChars
	for _, msg := range messages {
		cp := msg
		if strings.EqualFold(strings.TrimSpace(cp.Role), "tool") && len(cp.Content) > 400 {
			cp.Content = "[older tool output trimmed during context compaction]"
		} else if len(cp.Content) > 2000 {
			cp.Content = cp.Content[:2000] + "\n...[truncated for compaction]..."
		}
		if remaining > 0 && len(cp.Content) > remaining {
			cp.Content = strings.TrimSpace(cp.Content[:remaining]) + "\n...[truncated for compaction budget]..."
		}
		if remaining > 0 {
			remaining -= len(cp.Content)
			if remaining <= 0 {
				out = append(out, cp)
				break
			}
		}
		out = append(out, cp)
	}
	return out
}

func compactionTranscript(messages []providers.Message) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(messages))
	for _, msg := range messages {
		entry := map[string]interface{}{
			"role":    msg.Role,
			"content": strings.TrimSpace(msg.Content),
		}
		if strings.TrimSpace(msg.ToolCallID) != "" {
			entry["tool_call_id"] = msg.ToolCallID
		}
		if len(msg.ToolCalls) > 0 {
			entry["tool_calls"] = msg.ToolCalls
		}
		out = append(out, entry)
	}
	return out
}

func (al *AgentLoop) compactionKeepCount(historyLen, trigger int) int {
	if historyLen <= 0 {
		return 0
	}
	keep := al.compactionProtectLastN
	if keep <= 0 {
		keep = al.compactionKeepRecent
	}
	if keep <= 0 {
		keep = 12
	}
	if ratio := al.compactionTargetRatio; ratio > 0 {
		ratioKeep := int(math.Ceil(float64(historyLen) * ratio))
		if ratioKeep > keep {
			keep = ratioKeep
		}
	}
	if trigger > 0 && keep >= trigger {
		keep = maxLoopInt(trigger-1, al.compactionProtectLastN, 1)
	}
	if keep >= historyLen {
		keep = historyLen - 1
	}
	if keep < 1 {
		keep = 1
	}
	return keep
}

func (al *AgentLoop) estimateCompactionPressure(ctx context.Context, sessionKey string, history []providers.Message, summary string) float64 {
	maxTokens := al.maxTokensForSession(sessionKey)
	if maxTokens <= 0 {
		return 0
	}
	estimateMessages := make([]providers.Message, 0, len(history)+1)
	estimateMessages = append(estimateMessages, history...)
	if strings.TrimSpace(summary) != "" {
		estimateMessages = append([]providers.Message{{
			Role:    "system",
			Content: "Handoff summary:\n" + strings.TrimSpace(summary),
		}}, estimateMessages...)
	}
	if activeProvider, activeModel, _, err := al.activeProviderForSession(sessionKey); err == nil {
		if counter, ok := activeProvider.(providers.TokenCounter); ok {
			if usage, err := counter.CountTokens(ctx, estimateMessages, nil, activeModel, nil); err == nil && usage != nil {
				total := usage.TotalTokens
				if total <= 0 {
					total = usage.PromptTokens
				}
				if total > 0 {
					return float64(total) / float64(maxTokens)
				}
			}
		}
	}
	charCount := 0
	for _, msg := range estimateMessages {
		charCount += len(msg.Content)
	}
	approxTokens := charCount / 4
	if approxTokens <= 0 && charCount > 0 {
		approxTokens = 1
	}
	return float64(approxTokens) / float64(maxTokens)
}

func normalizePositiveInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func normalizeCompactionRatio(value, fallback float64) float64 {
	if value > 0 && value <= 1 {
		return value
	}
	return fallback
}

func maxLoopInt(values ...int) int {
	best := 0
	for _, value := range values {
		if value > best {
			best = value
		}
	}
	return best
}

func classifyLLMFailureCode(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if code := providers.ExecutionErrorCode(err); strings.TrimSpace(code) != "" {
		return strings.TrimSpace(code)
	}
	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(lower, "max tool iterations"):
		return "retry_limit"
	case strings.Contains(lower, "stream stale"):
		return "stream_stale"
	case strings.Contains(lower, "stream failed"):
		return "stream_failed"
	case strings.Contains(lower, "thinking budget exhausted"), strings.Contains(lower, "continuation exhausted"):
		return "continuation_exhausted"
	default:
		return ""
	}
}

func (al *AgentLoop) GetStartupInfo() map[string]interface{} {
	info := make(map[string]interface{})

	// Tools info
	_tools := al.tools.List()
	info["tools"] = map[string]interface{}{
		"count": len(_tools),
		"names": _tools,
	}

	// Skills info
	info["skills"] = al.contextBuilder.GetSkillsInfo()

	return info
}

func (al *AgentLoop) GetToolCatalog() []map[string]interface{} {
	if al == nil || al.tools == nil {
		return nil
	}
	items := al.tools.Catalog()
	sort.Slice(items, func(i, j int) bool {
		return fmt.Sprint(items[i]["name"]) < fmt.Sprint(items[j]["name"])
	})
	for _, item := range items {
		if fmt.Sprint(item["source"]) != "mcp" {
			item["source"] = "local"
		}
		name := strings.TrimSpace(fmt.Sprint(item["name"]))
		item["visibility"] = map[string]interface{}{
			"main_agent": true,
			"subagent":   subagentToolVisibilityMode(name),
		}
	}
	return items
}

func subagentToolVisibilityMode(name string) string {
	if isImplicitlyAllowedSubagentTool(name) {
		return "inherited"
	}
	return "allowlist"
}

// formatMessagesForLog formats messages for logging
func formatMessagesForLog(messages []providers.Message) string {
	if len(messages) == 0 {
		return "[]"
	}

	var result string
	result += "[\n"
	for i, msg := range messages {
		result += fmt.Sprintf("  [%d] Role: %s\n", i, msg.Role)
		if msg.ToolCalls != nil && len(msg.ToolCalls) > 0 {
			result += "  ToolCalls:\n"
			for _, tc := range msg.ToolCalls {
				result += fmt.Sprintf("    - ID: %s, Type: %s, Name: %s\n", tc.ID, tc.Type, tc.Name)
				if tc.Function != nil {
					result += fmt.Sprintf("      Arguments: %s\n", truncateString(tc.Function.Arguments, 200))
				}
			}
		}
		if msg.Content != "" {
			content := truncateString(msg.Content, 200)
			result += fmt.Sprintf("  Content: %s\n", content)
		}
		if msg.ToolCallID != "" {
			result += fmt.Sprintf("  ToolCallID: %s\n", msg.ToolCallID)
		}
		result += "\n"
	}
	result += "]"
	return result
}

// formatToolsForLog formats tool definitions for logging
func formatToolsForLog(tools []providers.ToolDefinition) string {
	if len(tools) == 0 {
		return "[]"
	}

	var result string
	result += "[\n"
	for i, tool := range tools {
		result += fmt.Sprintf("  [%d] Type: %s, Name: %s\n", i, tool.Type, tool.Function.Name)
		result += fmt.Sprintf("      Description: %s\n", tool.Function.Description)
		if len(tool.Function.Parameters) > 0 {
			result += fmt.Sprintf("      Parameters: %s\n", truncateString(fmt.Sprintf("%v", tool.Function.Parameters), 200))
		}
	}
	result += "]"
	return result
}

// truncateString truncates a string to max length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func withToolContextArgs(toolName string, args map[string]interface{}, channel, chatID string) map[string]interface{} {
	if channel == "" || chatID == "" {
		return args
	}
	switch toolName {
	case "message", "spawn", "remind":
	default:
		return args
	}

	next := make(map[string]interface{}, len(args)+2)
	for k, v := range args {
		next[k] = v
	}

	if toolName == "message" {
		if _, ok := next["channel"]; !ok {
			next["channel"] = channel
		}
		if _, hasChat := next["chat_id"]; !hasChat {
			if _, hasTo := next["to"]; !hasTo {
				next["chat_id"] = chatID
			}
		}
		return next
	}

	if _, ok := next["channel"]; !ok {
		next["channel"] = channel
	}
	if _, ok := next["chat_id"]; !ok {
		next["chat_id"] = chatID
	}
	return next
}

func withToolRuntimeArgs(ctx context.Context, toolName string, args map[string]interface{}) map[string]interface{} {
	if strings.TrimSpace(toolName) != "skill_exec" {
		return args
	}
	ns := normalizeMemoryNamespace(memoryNamespaceFromContext(ctx))
	callerAgent := ns
	callerScope := "subagent"
	if callerAgent == "" || callerAgent == "main" {
		callerAgent = "main"
		callerScope = "main_agent"
	}
	next := make(map[string]interface{}, len(args)+2)
	for k, v := range args {
		next[k] = v
	}
	next["caller_agent"] = callerAgent
	next["caller_scope"] = callerScope
	return next
}

func (al *AgentLoop) executeToolCall(ctx context.Context, toolName string, args map[string]interface{}, currentChannel, currentChatID string) (string, error) {
	if err := ensureToolAllowedByContext(ctx, toolName, args); err != nil {
		return "", err
	}
	args = withToolMemoryNamespaceArgs(toolName, args, memoryNamespaceFromContext(ctx))
	args = withToolRuntimeArgs(ctx, toolName, args)
	if shouldSuppressSelfMessageSend(toolName, args, currentChannel, currentChatID) {
		return "Suppressed message tool self-send in current chat; assistant will reply via normal outbound.", nil
	}
	return al.tools.Execute(ctx, toolName, args)
}

func withToolMemoryNamespaceArgs(toolName string, args map[string]interface{}, namespace string) map[string]interface{} {
	ns := normalizeMemoryNamespace(namespace)
	if ns == "main" {
		return args
	}
	switch strings.TrimSpace(toolName) {
	case "memory_search", "memory_get", "memory_write":
	default:
		return args
	}

	if raw := tools.MapStringArg(args, "namespace"); raw != "" {
		return args
	}
	next := make(map[string]interface{}, len(args)+1)
	for k, v := range args {
		next[k] = v
	}
	next["namespace"] = ns
	return next
}

type agentContextKey string

const memoryNamespaceContextKey agentContextKey = "memory_namespace"
const toolAllowlistContextKey agentContextKey = "tool_allowlist"

func withMemoryNamespaceContext(ctx context.Context, namespace string) context.Context {
	ns := normalizeMemoryNamespace(namespace)
	if ns == "main" {
		return ctx
	}
	return context.WithValue(ctx, memoryNamespaceContextKey, ns)
}

func memoryNamespaceFromContext(ctx context.Context) string {
	if ctx == nil {
		return "main"
	}
	raw, _ := ctx.Value(memoryNamespaceContextKey).(string)
	return normalizeMemoryNamespace(raw)
}

func withToolAllowlistContext(ctx context.Context, allowlist []string) context.Context {
	normalized := normalizeToolAllowlist(allowlist)
	if len(normalized) == 0 {
		return ctx
	}
	return context.WithValue(ctx, toolAllowlistContextKey, normalized)
}

func toolAllowlistFromContext(ctx context.Context) map[string]struct{} {
	if ctx == nil {
		return nil
	}
	raw, _ := ctx.Value(toolAllowlistContextKey).(map[string]struct{})
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func ensureToolAllowedByContext(ctx context.Context, toolName string, args map[string]interface{}) error {
	allow := toolAllowlistFromContext(ctx)
	if len(allow) == 0 {
		return nil
	}

	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "" {
		return fmt.Errorf("tool name is empty")
	}
	if !isToolNameAllowed(allow, name) && !isImplicitlyAllowedSubagentTool(name) {
		return fmt.Errorf("tool '%s' is not allowed by subagent profile", toolName)
	}

	if name == "parallel" {
		if err := validateParallelAllowlistArgs(allow, args); err != nil {
			return err
		}
	}
	return nil
}

func validateParallelAllowlistArgs(allow map[string]struct{}, args map[string]interface{}) error {
	callsRaw, ok := args["calls"].([]interface{})
	if !ok {
		return nil
	}
	for i, call := range callsRaw {
		m, ok := call.(map[string]interface{})
		if !ok {
			continue
		}
		tool := tools.MapStringArg(m, "tool")
		name := strings.ToLower(strings.TrimSpace(tool))
		if name == "" {
			continue
		}
		if !isToolNameAllowed(allow, name) && !isImplicitlyAllowedSubagentTool(name) {
			return fmt.Errorf("tool 'parallel' contains disallowed call[%d]: %s", i, tool)
		}
	}
	return nil
}

func isImplicitlyAllowedSubagentTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "skill_exec":
		return true
	default:
		return false
	}
}

func normalizeToolAllowlist(in []string) map[string]struct{} {
	expanded := tools.ExpandToolAllowlistEntries(in)
	if len(expanded) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(expanded))
	for _, item := range expanded {
		name := strings.ToLower(strings.TrimSpace(item))
		if name == "" {
			continue
		}
		out[name] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isToolNameAllowed(allow map[string]struct{}, name string) bool {
	if len(allow) == 0 {
		return true
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	if _, ok := allow["*"]; ok {
		return true
	}
	if _, ok := allow["all"]; ok {
		return true
	}
	_, ok := allow[name]
	return ok
}

func resolveInboundMemoryNamespace(msg bus.InboundMessage) string {
	if msg.Channel == "system" {
		return "main"
	}
	if msg.Metadata == nil {
		return "main"
	}
	if v := strings.TrimSpace(msg.Metadata["memory_namespace"]); v != "" {
		return normalizeMemoryNamespace(v)
	}
	if v := strings.TrimSpace(msg.Metadata["memory_ns"]); v != "" {
		return normalizeMemoryNamespace(v)
	}
	return "main"
}

func shouldSuppressSelfMessageSend(toolName string, args map[string]interface{}, currentChannel, currentChatID string) bool {
	if strings.TrimSpace(toolName) != "message" {
		return false
	}
	action := strings.ToLower(tools.MapStringArg(args, "action"))
	if action == "" {
		action = "send"
	}
	if action != "send" {
		return false
	}

	targetChannel, targetChat := resolveMessageToolTarget(args, currentChannel, currentChatID)
	return targetChannel == strings.TrimSpace(currentChannel) && targetChat == strings.TrimSpace(currentChatID)
}

func resolveMessageToolTarget(args map[string]interface{}, fallbackChannel, fallbackChatID string) (string, string) {
	channel := tools.MapStringArg(args, "channel")
	if channel == "" {
		channel = strings.TrimSpace(fallbackChannel)
	}

	chatID := tools.MapStringArg(args, "chat_id")
	if to := tools.MapStringArg(args, "to"); to != "" {
		chatID = to
	}
	if chatID == "" {
		chatID = strings.TrimSpace(fallbackChatID)
	}
	return channel, chatID
}

func shouldDropNoReply(text string) bool {
	t := strings.TrimSpace(text)
	return strings.EqualFold(t, "NO_REPLY")
}

func resolveSystemOrigin(chatID string) (string, string) {
	raw := strings.TrimSpace(chatID)
	if raw == "" {
		return "cli", "direct"
	}
	originChannel, originChatID, ok := strings.Cut(raw, ":")
	if !ok {
		return "cli", raw
	}
	originChannel = strings.TrimSpace(originChannel)
	originChatID = strings.TrimSpace(originChatID)
	switch {
	case originChannel == "" && originChatID == "":
		return "cli", "direct"
	case originChannel == "":
		return "cli", originChatID
	case originChatID == "":
		return originChannel, "direct"
	default:
		return originChannel, originChatID
	}
}

func isSubagentSystemMessage(msg bus.InboundMessage) bool {
	if msg.Channel != "system" {
		return false
	}
	if msg.Metadata != nil && strings.EqualFold(strings.TrimSpace(msg.Metadata["trigger"]), "subagent") {
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(msg.SenderID)), "subagent:")
}

func fallbackSubagentNotification(msg bus.InboundMessage) (string, bool) {
	if !isSubagentSystemMessage(msg) {
		return "", false
	}
	content := strings.TrimSpace(msg.Content)
	if content == "" {
		id := strings.TrimSpace(strings.TrimPrefix(msg.SenderID, "subagent:"))
		if id == "" {
			id = "unknown"
		}
		content = fmt.Sprintf("Subagent %s completed.", id)
	}
	return content, true
}

func parseSubagentSystemContent(content string) (string, string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", ""
	}
	lines := strings.Split(content, "\n")
	taskSummary := ""
	resultSummary := ""
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		lower := strings.ToLower(t)
		switch {
		case strings.HasPrefix(lower, "task:"):
			taskSummary = strings.TrimSpace(t[len("task:"):])
		case strings.HasPrefix(lower, "summary:"):
			resultSummary = strings.TrimSpace(t[len("summary:"):])
		}
	}
	if taskSummary == "" && len(lines) > 0 {
		taskSummary = summarizeSystemNotificationText(content, 120)
	}
	return taskSummary, resultSummary
}

func summarizeSystemNotificationText(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max-3]) + "..."
	}
	return s
}

func (al *AgentLoop) enqueueSubagentDigest(msg bus.InboundMessage, item subagentDigestItem) {
	if al == nil || al.bus == nil {
		return
	}
	originChannel, originChatID := resolveSystemOrigin(msg.ChatID)
	key := originChannel + "\x00" + originChatID
	delay := al.subagentDigestDelay
	if delay <= 0 {
		delay = 5 * time.Second
	}

	al.subagentDigestMu.Lock()
	state, ok := al.subagentDigests[key]
	if !ok || state == nil {
		state = &subagentDigestState{
			channel: originChannel,
			chatID:  originChatID,
			items:   map[string]subagentDigestItem{},
		}
		al.subagentDigests[key] = state
	}
	itemKey := subagentDigestItemKey(item)
	state.items[itemKey] = item
	state.dueAt = time.Now().Add(delay)
	al.subagentDigestMu.Unlock()
}

func subagentDigestItemKey(item subagentDigestItem) string {
	agentID := strings.ToLower(strings.TrimSpace(item.agentID))
	reason := strings.ToLower(strings.TrimSpace(item.reason))
	task := strings.ToLower(strings.TrimSpace(item.taskSummary))
	if agentID == "" {
		agentID = "subagent"
	}
	return agentID + "\x00" + reason + "\x00" + task
}

func (al *AgentLoop) flushSubagentDigest(key string) {
	if al == nil || al.bus == nil {
		return
	}
	al.subagentDigestMu.Lock()
	state := al.subagentDigests[key]
	delete(al.subagentDigests, key)
	al.subagentDigestMu.Unlock()
	if state == nil || len(state.items) == 0 {
		return
	}
	content := formatSubagentDigestSummary(state.items)
	if strings.TrimSpace(content) == "" {
		return
	}
	al.bus.PublishOutbound(bus.OutboundMessage{
		Channel: state.channel,
		ChatID:  state.chatID,
		Content: content,
	})
}

func (al *AgentLoop) runSubagentDigestTicker() {
	if al == nil {
		return
	}
	tick := time.Second
	if tick <= 0 {
		tick = time.Second
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for now := range ticker.C {
		al.flushDueSubagentDigests(now)
	}
}

func (al *AgentLoop) flushDueSubagentDigests(now time.Time) {
	if al == nil || al.bus == nil {
		return
	}
	dueKeys := make([]string, 0, 4)
	al.subagentDigestMu.Lock()
	for key, state := range al.subagentDigests {
		if state == nil || state.dueAt.IsZero() || now.Before(state.dueAt) {
			continue
		}
		dueKeys = append(dueKeys, key)
	}
	al.subagentDigestMu.Unlock()
	for _, key := range dueKeys {
		al.flushSubagentDigest(key)
	}
}

func formatSubagentDigestSummary(items map[string]subagentDigestItem) string {
	if len(items) == 0 {
		return ""
	}
	list := make([]subagentDigestItem, 0, len(items))
	completed := 0
	blocked := 0
	milestone := 0
	failed := 0
	for _, item := range items {
		list = append(list, item)
		switch {
		case strings.EqualFold(strings.TrimSpace(item.reason), "blocked"):
			blocked++
		case strings.EqualFold(strings.TrimSpace(item.reason), "milestone"):
			milestone++
		case strings.EqualFold(strings.TrimSpace(item.status), "failed"):
			failed++
		default:
			completed++
		}
	}
	sort.Slice(list, func(i, j int) bool {
		left := strings.TrimSpace(list[i].agentID)
		right := strings.TrimSpace(list[j].agentID)
		if left == right {
			return strings.TrimSpace(list[i].taskSummary) < strings.TrimSpace(list[j].taskSummary)
		}
		return left < right
	})
	var sb strings.Builder
	sb.WriteString("阶段总结")
	stats := make([]string, 0, 4)
	if completed > 0 {
		stats = append(stats, fmt.Sprintf("完成 %d", completed))
	}
	if blocked > 0 {
		stats = append(stats, fmt.Sprintf("受阻 %d", blocked))
	}
	if failed > 0 {
		stats = append(stats, fmt.Sprintf("失败 %d", failed))
	}
	if milestone > 0 {
		stats = append(stats, fmt.Sprintf("进展 %d", milestone))
	}
	if len(stats) > 0 {
		sb.WriteString("（" + strings.Join(stats, "，") + "）")
	}
	sb.WriteString("\n")
	for _, item := range list {
		agentLabel := strings.TrimSpace(item.agentID)
		if agentLabel == "" {
			agentLabel = "subagent"
		}
		statusText := "已完成"
		switch {
		case strings.EqualFold(strings.TrimSpace(item.reason), "blocked"):
			statusText = "受阻"
		case strings.EqualFold(strings.TrimSpace(item.reason), "milestone"):
			statusText = "有进展"
		case strings.EqualFold(strings.TrimSpace(item.status), "failed"):
			statusText = "失败"
		}
		sb.WriteString("- " + agentLabel + "：" + statusText)
		if task := strings.TrimSpace(item.taskSummary); task != "" {
			sb.WriteString("，任务：" + task)
		}
		if summary := strings.TrimSpace(item.resultSummary); summary != "" {
			label := "摘要"
			if statusText == "受阻" {
				label = "原因"
			} else if statusText == "有进展" {
				label = "进度"
			}
			sb.WriteString("，" + label + "：" + summary)
		}
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

func shouldFlushTelegramStreamSnapshot(s string) bool {
	s = strings.TrimRight(s, " \t")
	if s == "" {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(s)
	switch last {
	case '\n', '。', '！', '？', '.', '!', '?', ';', '；', ':', '：':
	default:
		return false
	}
	// Avoid flushing while code fences are still unbalanced.
	if strings.Count(s, "```")%2 == 1 {
		return false
	}
	// Avoid flushing while common inline markdown markers are unbalanced.
	if strings.Count(s, "**")%2 == 1 || strings.Count(s, "__")%2 == 1 || strings.Count(s, "~~")%2 == 1 {
		return false
	}
	// Rough guard for links/images: require bracket balance before flushing.
	if strings.Count(s, "[") != strings.Count(s, "]") || strings.Count(s, "(") != strings.Count(s, ")") {
		return false
	}
	return true
}

func parseReplyTag(text string, currentMessageID string) (content string, replyToID string) {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "[[") {
		return text, ""
	}
	end := strings.Index(t, "]]")
	if end <= 0 {
		return text, ""
	}
	rawTag := strings.TrimSpace(t[2:end])
	tag := strings.ToLower(rawTag)
	if strings.HasPrefix(tag, "reply_to_current") || strings.HasPrefix(tag, "reply_to") {
		content = strings.TrimSpace(t[end+2:])
		if strings.HasPrefix(tag, "reply_to:") {
			replyToID = strings.TrimSpace(rawTag[len("reply_to:"):])
		} else if strings.HasPrefix(tag, "reply_to_current") {
			replyToID = strings.TrimSpace(currentMessageID)
		}
		return content, replyToID
	}
	return text, ""
}

func alSessionListForTool(sm *session.SessionManager, limit int) []tools.SessionInfo {
	items := sm.List(limit)
	out := make([]tools.SessionInfo, 0, len(items))
	for i := range items {
		s := &items[i]
		out = append(out, tools.SessionInfo{
			Key:             s.Key,
			Kind:            s.Kind,
			Summary:         s.Summary,
			CompactionCount: s.CompactionCount,
			UpdatedAt:       s.Updated,
		})
	}
	return out
}
