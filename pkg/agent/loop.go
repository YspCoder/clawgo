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
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
	"clawgo/pkg/configops"
	"clawgo/pkg/cron"
	"clawgo/pkg/logger"
	"clawgo/pkg/providers"
	"clawgo/pkg/session"
	"clawgo/pkg/tools"
)

var errGatewayNotRunningSlash = errors.New("gateway not running")

const autoLearnDefaultInterval = 10 * time.Minute
const autoLearnMinInterval = 30 * time.Second
const autonomyDefaultIdleInterval = 30 * time.Minute
const autonomyMinIdleInterval = 1 * time.Minute
const autonomyContinuousRunInterval = 20 * time.Second
const autonomyContinuousIdleThreshold = 20 * time.Second
const defaultRunStateTTL = 30 * time.Minute
const defaultRunStateMaxEntries = 500
const defaultRunWaitTimeout = 60 * time.Second
const minRunWaitTimeout = 5 * time.Second
const maxRunWaitTimeout = 15 * time.Minute
const toolLoopRepeatSignatureThreshold = 2
const toolLoopAllErrorRoundsThreshold = 2
const toolLoopMaxCallsPerIteration = 6
const toolLoopSingleCallTimeout = 20 * time.Second
const toolLoopMaxActDuration = 45 * time.Second
const toolLoopMinCallsPerIteration = 2
const toolLoopMinSingleCallTimeout = 8 * time.Second
const toolLoopMinActDuration = 18 * time.Second
const toolLoopMaxParallelCalls = 2
const finalizeDraftMinCharsForPolish = 90
const finalizeQualityThreshold = 0.72
const finalizeHeuristicHighThreshold = 0.82
const finalizeHeuristicLowThreshold = 0.48
const compactionAttemptTimeout = 8 * time.Second
const compactionRetryPerCandidate = 1

type sessionWorker struct {
	cancelMu    sync.Mutex
	cancel      context.CancelFunc
	queueMu     sync.Mutex
	queue       []bus.InboundMessage
	queueNotify chan struct{}
}

type autoLearner struct {
	cancel   context.CancelFunc
	started  time.Time
	interval time.Duration
	rounds   int
}

type autonomySession struct {
	cancel       context.CancelFunc
	started      time.Time
	idleInterval time.Duration
	rounds       int
	lastUserAt   time.Time
	lastNudgeAt  time.Time
	lastReportAt time.Time
	pending      bool
	pendingSince time.Time
	stallCount   int
	focus        string
}

type controlPolicy struct {
	intentMaxInputChars          int
	autonomyTickInterval         time.Duration
	autonomyMinRunInterval       time.Duration
	autonomyIdleThreshold        time.Duration
	autonomyMaxPendingDuration   time.Duration
	autonomyMaxConsecutiveStalls int
}

type runtimeControlStats struct {
	runAccepted             int64
	runCompleted            int64
	runFailed               int64
	runCanceled             int64
	runControlHandled       int64
	intentAutonomyMatched   int64
	intentAutonomyRejected  int64
	intentAutoLearnMatched  int64
	intentAutoLearnRejected int64
	autonomyRounds          int64
	autonomyStoppedByGuard  int64
	autoLearnRounds         int64
}

type runStatus string

const (
	runStatusAccepted runStatus = "accepted"
	runStatusRunning  runStatus = "running"
	runStatusOK       runStatus = "ok"
	runStatusError    runStatus = "error"
	runStatusCanceled runStatus = "canceled"
)

type agentRunLifecycle struct {
	runID           string
	acceptedAt      time.Time
	sessionKey      string
	channel         string
	senderID        string
	chatID          string
	synthetic       bool
	controlEligible bool
}

type runState struct {
	runID           string
	sessionKey      string
	channel         string
	chatID          string
	senderID        string
	synthetic       bool
	controlEligible bool
	status          runStatus
	acceptedAt      time.Time
	startedAt       time.Time
	endedAt         time.Time
	errMessage      string
	responseLen     int
	controlHandled  bool
	done            chan struct{}
}

type AgentLoop struct {
	bus               *bus.MessageBus
	provider          providers.LLMProvider
	providersByProxy  map[string]providers.LLMProvider
	modelsByProxy     map[string][]string
	proxy             string
	proxyFallbacks    []string
	workspace         string
	model             string
	maxIterations     int
	sessions          *session.SessionManager
	contextBuilder    *ContextBuilder
	tools             *tools.ToolRegistry
	orchestrator      *tools.Orchestrator
	running           atomic.Bool
	compactionCfg     config.ContextCompactionConfig
	llmCallTimeout    time.Duration
	workersMu         sync.Mutex
	workers           map[string]*sessionWorker
	autoLearnMu       sync.Mutex
	autoLearners      map[string]*autoLearner
	autonomyMu        sync.Mutex
	autonomyBySess    map[string]*autonomySession
	controlPolicy     controlPolicy
	parallelSafeTools map[string]struct{}
	maxParallelCalls  int
	controlStats      runtimeControlStats
	runSeq            atomic.Int64
	runStateMu        sync.Mutex
	runStates         map[string]*runState
	runStateTTL       time.Duration
	runStateMax       int
}

type taskExecutionDirectives struct {
	task        string
	stageReport bool
}

type runControlIntent struct {
	runID   string
	latest  bool
	wait    bool
	timeout time.Duration
}

type autoLearnIntent struct {
	enabled  *bool
	interval *time.Duration
}

type autonomyIntent struct {
	enabled      *bool
	clearFocus   bool
	idleInterval *time.Duration
	focus        string
}

type intentDetectionOutcome struct {
	matched    bool
	confidence float64
}

type autonomyIntentLLMResponse struct {
	Matched     bool    `json:"matched"`
	Enabled     *bool   `json:"enabled"`
	ClearFocus  bool    `json:"clear_focus"`
	IdleMinutes int     `json:"idle_minutes"`
	Focus       string  `json:"focus"`
	Confidence  float64 `json:"confidence"`
}

type autoLearnIntentLLMResponse struct {
	Matched         bool    `json:"matched"`
	Enabled         *bool   `json:"enabled"`
	IntervalMinutes int     `json:"interval_minutes"`
	Confidence      float64 `json:"confidence"`
}

type taskExecutionDirectivesLLMResponse struct {
	Task        string  `json:"task"`
	StageReport bool    `json:"stage_report"`
	Confidence  float64 `json:"confidence"`
}

type runControlIntentLLMResponse struct {
	Matched        bool    `json:"matched"`
	RunID          string  `json:"run_id"`
	Latest         bool    `json:"latest"`
	Wait           bool    `json:"wait"`
	TimeoutSeconds int     `json:"timeout_seconds"`
	Confidence     float64 `json:"confidence"`
}

var defaultParallelSafeToolNames = []string{"read_file", "list_files", "find_files", "grep_files", "memory_search", "web_search", "repo_map", "system_info"}

type stageReporter struct {
	onUpdate func(content string)
	localize func(content string) string
}

type StartupSelfCheckReport struct {
	TotalSessions     int
	CompactedSessions int
}

type tokenUsageTotals struct {
	input  int
	output int
	total  int
}

type tokenUsageTotalsKey struct{}

func defaultControlPolicy() controlPolicy {
	return controlPolicy{
		intentMaxInputChars:          1200,
		autonomyTickInterval:         autonomyContinuousRunInterval,
		autonomyMinRunInterval:       autonomyContinuousRunInterval,
		autonomyIdleThreshold:        autonomyContinuousIdleThreshold,
		autonomyMaxPendingDuration:   3 * time.Minute,
		autonomyMaxConsecutiveStalls: 3,
	}
}

func envFloat64(key string, fallback float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func loadControlPolicyFromConfig(base controlPolicy, rc config.RuntimeControlConfig) controlPolicy {
	p := base
	if rc.IntentMaxInputChars > 0 {
		p.intentMaxInputChars = rc.IntentMaxInputChars
	}
	if rc.AutonomyTickIntervalSec > 0 {
		p.autonomyTickInterval = time.Duration(rc.AutonomyTickIntervalSec) * time.Second
	}
	if rc.AutonomyMinRunIntervalSec > 0 {
		p.autonomyMinRunInterval = time.Duration(rc.AutonomyMinRunIntervalSec) * time.Second
	}
	if rc.AutonomyIdleThresholdSec > 0 {
		p.autonomyIdleThreshold = time.Duration(rc.AutonomyIdleThresholdSec) * time.Second
	}
	if rc.AutonomyMaxPendingDurationSec > 0 {
		p.autonomyMaxPendingDuration = time.Duration(rc.AutonomyMaxPendingDurationSec) * time.Second
	}
	if rc.AutonomyMaxConsecutiveStalls > 0 {
		p.autonomyMaxConsecutiveStalls = rc.AutonomyMaxConsecutiveStalls
	}
	return p
}

func loadRunStatePolicyFromConfig(rc config.RuntimeControlConfig) (time.Duration, int) {
	ttl := defaultRunStateTTL
	if rc.RunStateTTLSeconds > 0 {
		ttl = time.Duration(rc.RunStateTTLSeconds) * time.Second
	}
	maxEntries := defaultRunStateMaxEntries
	if rc.RunStateMax > 0 {
		maxEntries = rc.RunStateMax
	}
	return ttl, maxEntries
}

func normalizeKeywordList(values []string, fallback []string) []string {
	if len(values) == 0 {
		return append([]string(nil), fallback...)
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return append([]string(nil), fallback...)
	}
	return out
}

func loadToolParallelPolicyFromConfig(rc config.RuntimeControlConfig) (map[string]struct{}, int) {
	names := normalizeKeywordList(rc.ToolParallelSafeNames, defaultParallelSafeToolNames)
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		allowed[name] = struct{}{}
	}
	maxParallel := rc.ToolMaxParallelCalls
	if maxParallel <= 0 {
		maxParallel = toolLoopMaxParallelCalls
	}
	if maxParallel < 1 {
		maxParallel = 1
	}
	if maxParallel > 8 {
		maxParallel = 8
	}
	return allowed, maxParallel
}

// applyLegacyControlPolicyEnvOverrides keeps compatibility with older env names.
func applyLegacyControlPolicyEnvOverrides(base controlPolicy) controlPolicy {
	p := base
	p.intentMaxInputChars = envInt("CLAWGO_INTENT_MAX_INPUT_CHARS", p.intentMaxInputChars)
	p.autonomyTickInterval = envDuration("CLAWGO_AUTONOMY_TICK_INTERVAL", p.autonomyTickInterval)
	p.autonomyMinRunInterval = envDuration("CLAWGO_AUTONOMY_MIN_RUN_INTERVAL", p.autonomyMinRunInterval)
	p.autonomyIdleThreshold = envDuration("CLAWGO_AUTONOMY_IDLE_THRESHOLD", p.autonomyIdleThreshold)
	p.autonomyMaxPendingDuration = envDuration("CLAWGO_AUTONOMY_MAX_PENDING_DURATION", p.autonomyMaxPendingDuration)
	p.autonomyMaxConsecutiveStalls = envInt("CLAWGO_AUTONOMY_MAX_STALLS", p.autonomyMaxConsecutiveStalls)

	if p.intentMaxInputChars < 200 {
		p.intentMaxInputChars = base.intentMaxInputChars
	}
	if p.autonomyTickInterval < 5*time.Second {
		p.autonomyTickInterval = base.autonomyTickInterval
	}
	if p.autonomyMinRunInterval < 5*time.Second {
		p.autonomyMinRunInterval = base.autonomyMinRunInterval
	}
	if p.autonomyIdleThreshold < 5*time.Second {
		p.autonomyIdleThreshold = base.autonomyIdleThreshold
	}
	if p.autonomyMaxPendingDuration < 10*time.Second {
		p.autonomyMaxPendingDuration = base.autonomyMaxPendingDuration
	}
	if p.autonomyMaxConsecutiveStalls <= 0 {
		p.autonomyMaxConsecutiveStalls = base.autonomyMaxConsecutiveStalls
	}
	return p
}

func (sr *stageReporter) Publish(stage int, total int, status string, detail string) {
	if sr == nil || sr.onUpdate == nil {
		return
	}
	_ = stage
	_ = total
	detail = strings.TrimSpace(detail)
	if detail != "" {
		if sr.localize != nil {
			detail = sr.localize(detail)
		}
		sr.onUpdate(detail)
		return
	}
	status = strings.TrimSpace(status)
	if status != "" {
		if sr.localize != nil {
			status = sr.localize(status)
		}
		sr.onUpdate(status)
		return
	}
	fallback := "Processing update"
	if sr.localize != nil {
		fallback = sr.localize(fallback)
	}
	sr.onUpdate(fallback)
}

type userLanguageHint struct {
	sessionKey string
	content    string
}

type userLanguageHintKey struct{}

func NewAgentLoop(cfg *config.Config, msgBus *bus.MessageBus, provider providers.LLMProvider, cs *cron.CronService) *AgentLoop {
	workspace := cfg.WorkspacePath()
	os.MkdirAll(workspace, 0755)
	logger.InfoCF("agent", "Agent workspace initialized", map[string]interface{}{
		"workspace": workspace,
	})

	toolsRegistry := tools.NewToolRegistry()
	toolsRegistry.Register(tools.NewReadFileTool(workspace))
	toolsRegistry.Register(tools.NewWriteFileTool(workspace))
	toolsRegistry.Register(tools.NewListDirTool(workspace))
	toolsRegistry.Register(tools.NewExecTool(cfg.Tools.Shell, workspace))

	if cs != nil {
		toolsRegistry.Register(tools.NewRemindTool(cs))
	}

	braveAPIKey := cfg.Tools.Web.Search.APIKey
	toolsRegistry.Register(tools.NewWebSearchTool(braveAPIKey, cfg.Tools.Web.Search.MaxResults))
	webFetchTool := tools.NewWebFetchTool(50000)
	toolsRegistry.Register(webFetchTool)
	toolsRegistry.Register(tools.NewParallelFetchTool(webFetchTool))

	// Register message tool
	messageTool := tools.NewMessageTool()
	messageTool.SetSendCallback(func(channel, chatID, content string, buttons [][]bus.Button) error {
		msgBus.PublishOutbound(bus.OutboundMessage{
			Buttons: buttons,
			Channel: channel,
			ChatID:  chatID,
			Content: content,
		})
		return nil
	})
	toolsRegistry.Register(messageTool)

	// Register spawn tool
	orchestrator := tools.NewOrchestrator()
	subagentManager := tools.NewSubagentManager(provider, workspace, msgBus, orchestrator)
	spawnTool := tools.NewSpawnTool(subagentManager)
	toolsRegistry.Register(spawnTool)
	toolsRegistry.Register(tools.NewPipelineCreateTool(orchestrator))
	toolsRegistry.Register(tools.NewPipelineStatusTool(orchestrator))
	toolsRegistry.Register(tools.NewPipelineStateSetTool(orchestrator))
	toolsRegistry.Register(tools.NewPipelineDispatchTool(orchestrator, subagentManager))

	// Register edit file tool
	editFileTool := tools.NewEditFileTool(workspace)
	toolsRegistry.Register(editFileTool)

	// Register memory search tool
	memorySearchTool := tools.NewMemorySearchTool(workspace)
	toolsRegistry.Register(memorySearchTool)
	toolsRegistry.Register(tools.NewRepoMapTool(workspace))
	toolsRegistry.Register(tools.NewSkillExecTool(workspace))

	// Register parallel execution tool (leveraging Go's concurrency)
	toolsRegistry.Register(tools.NewParallelTool(toolsRegistry))

	// Register browser tool (integrated Chromium support)
	toolsRegistry.Register(tools.NewBrowserTool())

	// Register camera tool
	toolsRegistry.Register(tools.NewCameraTool(workspace))
	// Register system info tool
	toolsRegistry.Register(tools.NewSystemInfoTool())

	sessionsManager := session.NewSessionManager(filepath.Join(filepath.Dir(cfg.WorkspacePath()), "sessions"))

	providersByProxy, err := providers.CreateProviders(cfg)
	if err != nil {
		logger.WarnCF("agent", "Create providers map failed, fallback to single provider mode", map[string]interface{}{
			logger.FieldError: err.Error(),
		})
		providersByProxy = map[string]providers.LLMProvider{
			"proxy": provider,
		}
	}
	modelsByProxy := map[string][]string{}
	for _, name := range providers.ListProviderNames(cfg) {
		modelsByProxy[name] = providers.GetProviderModels(cfg, name)
	}

	primaryProxy := strings.TrimSpace(cfg.Agents.Defaults.Proxy)
	if primaryProxy == "" {
		primaryProxy = "proxy"
	}
	if p, ok := providersByProxy[primaryProxy]; ok {
		provider = p
	} else if p, ok := providersByProxy["proxy"]; ok {
		primaryProxy = "proxy"
		provider = p
	}
	defaultModel := defaultModelFromModels(modelsByProxy[primaryProxy], provider)
	policy := loadControlPolicyFromConfig(defaultControlPolicy(), cfg.Agents.Defaults.RuntimeControl)
	policy = applyLegacyControlPolicyEnvOverrides(policy)
	parallelSafeTools, maxParallelCalls := loadToolParallelPolicyFromConfig(cfg.Agents.Defaults.RuntimeControl)
	runStateTTL, runStateMax := loadRunStatePolicyFromConfig(cfg.Agents.Defaults.RuntimeControl)
	// Keep compatibility with older env names.
	runStateTTL = envDuration("CLAWGO_RUN_STATE_TTL", runStateTTL)
	if runStateTTL < 1*time.Minute {
		runStateTTL = defaultRunStateTTL
	}
	runStateMax = envInt("CLAWGO_RUN_STATE_MAX", runStateMax)
	if runStateMax <= 0 {
		runStateMax = defaultRunStateMaxEntries
	}

	loop := &AgentLoop{
		bus:               msgBus,
		provider:          provider,
		providersByProxy:  providersByProxy,
		modelsByProxy:     modelsByProxy,
		proxy:             primaryProxy,
		proxyFallbacks:    parseStringList(cfg.Agents.Defaults.ProxyFallbacks),
		workspace:         workspace,
		model:             defaultModel,
		maxIterations:     cfg.Agents.Defaults.MaxToolIterations,
		sessions:          sessionsManager,
		contextBuilder:    NewContextBuilder(workspace, cfg.Memory, func() []string { return toolsRegistry.GetSummaries() }),
		tools:             toolsRegistry,
		orchestrator:      orchestrator,
		compactionCfg:     cfg.Agents.Defaults.ContextCompaction,
		llmCallTimeout:    time.Duration(cfg.Providers.Proxy.TimeoutSec) * time.Second,
		workers:           make(map[string]*sessionWorker),
		autoLearners:      make(map[string]*autoLearner),
		autonomyBySess:    make(map[string]*autonomySession),
		controlPolicy:     policy,
		parallelSafeTools: parallelSafeTools,
		maxParallelCalls:  maxParallelCalls,
		runStates:         make(map[string]*runState),
		runStateTTL:       runStateTTL,
		runStateMax:       runStateMax,
	}
	logger.InfoCF("agent", "Control policy initialized", map[string]interface{}{
		"intent_max_input_chars":          policy.intentMaxInputChars,
		"autonomy_tick_interval":          policy.autonomyTickInterval.String(),
		"autonomy_min_run_interval":       policy.autonomyMinRunInterval.String(),
		"autonomy_idle_threshold":         policy.autonomyIdleThreshold.String(),
		"autonomy_max_pending_duration":   policy.autonomyMaxPendingDuration.String(),
		"autonomy_max_consecutive_stalls": policy.autonomyMaxConsecutiveStalls,
		"parallel_safe_tool_count":        len(parallelSafeTools),
		"tool_max_parallel_calls":         maxParallelCalls,
		"run_state_ttl":                   runStateTTL.String(),
		"run_state_max":                   runStateMax,
	})

	// Inject recursive run logic so subagent has full tool-calling capability.
	subagentManager.SetRunFunc(func(ctx context.Context, task, channel, chatID string) (string, error) {
		sessionKey := fmt.Sprintf("subagent:%d", os.Getpid()) // Use PID/random value to avoid session key collisions.
		return loop.ProcessDirect(ctx, task, sessionKey)
	})

	return loop
}

func (al *AgentLoop) Run(ctx context.Context) error {
	al.running.Store(true)

	for al.running.Load() {
		select {
		case <-ctx.Done():
			al.stopAllWorkers()
			al.stopAllAutoLearners()
			al.stopAllAutonomySessions()
			return nil
		default:
			msg, ok := al.bus.ConsumeInbound(ctx)
			if !ok {
				al.stopAllWorkers()
				al.stopAllAutoLearners()
				al.stopAllAutonomySessions()
				return nil
			}

			if isStopCommand(msg.Content) {
				al.handleStopCommand(msg)
				continue
			}

			al.enqueueMessage(ctx, msg)
		}
	}

	return nil
}

func (al *AgentLoop) Stop() {
	al.running.Store(false)
	al.stopAllWorkers()
	al.stopAllAutoLearners()
	al.stopAllAutonomySessions()
}

func isStopCommand(content string) bool {
	return strings.EqualFold(strings.TrimSpace(content), "/stop")
}

func (al *AgentLoop) handleStopCommand(msg bus.InboundMessage) {
	al.cancelSessionWorkerRun(msg.SessionKey)
}

func (al *AgentLoop) cancelSessionWorkerRun(sessionKey string) {
	worker := al.getWorker(sessionKey)
	if worker == nil {
		return
	}

	worker.cancelMu.Lock()
	cancel := worker.cancel
	worker.cancelMu.Unlock()

	if cancel == nil {
		return
	}

	cancel()
}

func (al *AgentLoop) enqueueMessage(ctx context.Context, msg bus.InboundMessage) {
	_ = ctx
	worker := al.getOrCreateWorker(ctx, msg.SessionKey)
	worker.enqueue(msg)
}

func (al *AgentLoop) getWorker(sessionKey string) *sessionWorker {
	al.workersMu.Lock()
	defer al.workersMu.Unlock()
	return al.workers[sessionKey]
}

func (al *AgentLoop) getOrCreateWorker(ctx context.Context, sessionKey string) *sessionWorker {
	al.workersMu.Lock()
	defer al.workersMu.Unlock()

	if w, ok := al.workers[sessionKey]; ok {
		return w
	}

	w := &sessionWorker{
		queue:       make([]bus.InboundMessage, 0, 16),
		queueNotify: make(chan struct{}, 1),
	}
	al.workers[sessionKey] = w

	go al.runSessionWorker(ctx, sessionKey, w)
	return w
}

func (al *AgentLoop) runSessionWorker(ctx context.Context, sessionKey string, worker *sessionWorker) {
	for {
		msg, ok := worker.dequeue(ctx)
		if !ok {
			al.clearWorkerCancel(worker)
			al.removeWorker(sessionKey, worker)
			return
		}

		func() {
			taskCtx, cancel := context.WithCancel(ctx)
			taskCtx, tokenTotals := withTokenUsageTotals(taskCtx)
			worker.cancelMu.Lock()
			worker.cancel = cancel
			worker.cancelMu.Unlock()

			defer func() {
				cancel()
				al.clearWorkerCancel(worker)
				if r := recover(); r != nil {
					logger.ErrorCF("agent", "Session worker recovered from panic", map[string]interface{}{
						"session_key": sessionKey,
						"panic":       fmt.Sprintf("%v", r),
					})
				}
			}()

			response, err := al.processMessage(taskCtx, msg)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				response = al.formatProcessingErrorMessage(taskCtx, msg, err)
			}

			if response != "" && shouldPublishSyntheticResponse(msg) {
				if al != nil && al.sessions != nil && tokenTotals != nil {
					al.sessions.AddTokenUsage(
						msg.SessionKey,
						tokenTotals.input,
						tokenTotals.output,
						tokenTotals.total,
					)
				}
				response += formatTokenUsageSuffix(
					tokenTotals,
				)
				al.bus.PublishOutbound(bus.OutboundMessage{
					Buttons: nil,
					Channel: msg.Channel,
					ChatID:  msg.ChatID,
					Content: response,
				})
			}
		}()
	}
}

func (w *sessionWorker) enqueue(msg bus.InboundMessage) {
	if w == nil {
		return
	}
	w.queueMu.Lock()
	w.queue = append(w.queue, msg)
	w.queueMu.Unlock()
	select {
	case w.queueNotify <- struct{}{}:
	default:
	}
}

func (w *sessionWorker) dequeue(ctx context.Context) (bus.InboundMessage, bool) {
	if w == nil {
		return bus.InboundMessage{}, false
	}
	for {
		w.queueMu.Lock()
		if len(w.queue) > 0 {
			msg := w.queue[0]
			w.queue[0] = bus.InboundMessage{}
			w.queue = w.queue[1:]
			w.queueMu.Unlock()
			return msg, true
		}
		w.queueMu.Unlock()

		select {
		case <-ctx.Done():
			return bus.InboundMessage{}, false
		case <-w.queueNotify:
		}
	}
}

func (al *AgentLoop) clearWorkerCancel(worker *sessionWorker) {
	worker.cancelMu.Lock()
	worker.cancel = nil
	worker.cancelMu.Unlock()
}

func (al *AgentLoop) formatProcessingErrorMessage(ctx context.Context, msg bus.InboundMessage, err error) string {
	return ""
}

func (al *AgentLoop) preferChineseUserFacingText(sessionKey, currentContent string) bool {
	zhCount, enCount := countLanguageSignals(currentContent)

	if al != nil && al.sessions != nil && strings.TrimSpace(sessionKey) != "" {
		history := al.sessions.GetHistory(sessionKey)
		seenUserTurns := 0
		for i := len(history) - 1; i >= 0 && seenUserTurns < 6; i-- {
			if history[i].Role != "user" {
				continue
			}
			seenUserTurns++
			z, e := countLanguageSignals(history[i].Content)
			zhCount += z
			enCount += e
		}
	}

	if zhCount == 0 && enCount == 0 {
		return false
	}
	return zhCount >= enCount
}

func countLanguageSignals(text string) (zhCount int, enCount int) {
	inEnglishWord := false
	for _, r := range text {
		if unicode.In(r, unicode.Han) {
			zhCount++
			inEnglishWord = false
			continue
		}
		if r <= unicode.MaxASCII && unicode.IsLetter(r) {
			if !inEnglishWord {
				enCount++
				inEnglishWord = true
			}
			continue
		}
		inEnglishWord = false
	}
	return zhCount, enCount
}

func (al *AgentLoop) removeWorker(sessionKey string, worker *sessionWorker) {
	al.workersMu.Lock()
	defer al.workersMu.Unlock()
	if cur, ok := al.workers[sessionKey]; ok && cur == worker {
		delete(al.workers, sessionKey)
	}
}

func (al *AgentLoop) stopAllWorkers() {
	al.workersMu.Lock()
	workers := make([]*sessionWorker, 0, len(al.workers))
	for _, w := range al.workers {
		workers = append(workers, w)
	}
	al.workersMu.Unlock()

	for _, w := range workers {
		w.cancelMu.Lock()
		cancel := w.cancel
		w.cancelMu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
}

func (al *AgentLoop) stopAllAutoLearners() {
	al.autoLearnMu.Lock()
	defer al.autoLearnMu.Unlock()

	for sessionKey, learner := range al.autoLearners {
		if learner != nil && learner.cancel != nil {
			learner.cancel()
		}
		delete(al.autoLearners, sessionKey)
	}
}

func (al *AgentLoop) stopAllAutonomySessions() {
	al.autonomyMu.Lock()
	defer al.autonomyMu.Unlock()

	for sessionKey, s := range al.autonomyBySess {
		if s != nil && s.cancel != nil {
			s.cancel()
		}
		delete(al.autonomyBySess, sessionKey)
	}
}

func (al *AgentLoop) startAutonomy(ctx context.Context, msg bus.InboundMessage, idleInterval time.Duration, focus string) {
	if msg.Channel == "cli" {
		return
	}

	if idleInterval <= 0 {
		idleInterval = autonomyDefaultIdleInterval
	}
	if idleInterval < autonomyMinIdleInterval {
		idleInterval = autonomyMinIdleInterval
	}

	al.autonomyMu.Lock()
	if old, ok := al.autonomyBySess[msg.SessionKey]; ok {
		if old != nil && old.cancel != nil {
			old.cancel()
		}
		delete(al.autonomyBySess, msg.SessionKey)
	}

	langCtx := withUserLanguageHint(context.Background(), msg.SessionKey, msg.Content)
	sessionCtx, cancel := context.WithCancel(langCtx)
	s := &autonomySession{
		cancel:       cancel,
		started:      time.Now(),
		idleInterval: idleInterval,
		lastUserAt:   time.Now(),
		lastReportAt: time.Now(),
		focus:        strings.TrimSpace(focus),
	}
	al.autonomyBySess[msg.SessionKey] = s
	al.autonomyMu.Unlock()

	go al.runAutonomyLoop(sessionCtx, msg)
	if s.focus != "" {
		al.bus.PublishInbound(bus.InboundMessage{
			Channel:    msg.Channel,
			SenderID:   "autonomy",
			ChatID:     msg.ChatID,
			SessionKey: msg.SessionKey,
			Content:    buildAutonomyFocusPrompt(s.focus),
			Metadata: map[string]string{
				"source": "autonomy",
				"round":  "0",
				"mode":   "focus_bootstrap",
			},
		})
	}
	return
}

func (al *AgentLoop) stopAutonomy(sessionKey string) bool {
	al.autonomyMu.Lock()
	defer al.autonomyMu.Unlock()

	s, ok := al.autonomyBySess[sessionKey]
	if !ok || s == nil {
		return false
	}
	if s.cancel != nil {
		s.cancel()
	}
	delete(al.autonomyBySess, sessionKey)
	return true
}

func (al *AgentLoop) clearAutonomyFocus(sessionKey string) bool {
	al.autonomyMu.Lock()
	defer al.autonomyMu.Unlock()

	s, ok := al.autonomyBySess[sessionKey]
	if !ok || s == nil {
		return false
	}
	s.focus = ""
	return true
}

func (al *AgentLoop) isAutonomyEnabled(sessionKey string) bool {
	al.autonomyMu.Lock()
	defer al.autonomyMu.Unlock()
	_, ok := al.autonomyBySess[sessionKey]
	return ok
}

func (al *AgentLoop) noteAutonomyUserActivity(msg bus.InboundMessage) {
	if isSyntheticMessage(msg) {
		return
	}
	al.autonomyMu.Lock()
	defer al.autonomyMu.Unlock()

	s, ok := al.autonomyBySess[msg.SessionKey]
	if !ok || s == nil {
		return
	}
	s.lastUserAt = time.Now()
}

func (al *AgentLoop) runAutonomyLoop(ctx context.Context, msg bus.InboundMessage) {
	tick := autonomyContinuousRunInterval
	if al != nil && al.controlPolicy.autonomyTickInterval > 0 {
		tick = al.controlPolicy.autonomyTickInterval
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !al.maybeRunAutonomyRound(msg) {
				return
			}
		}
	}
}

func (al *AgentLoop) maybeRunAutonomyRound(msg bus.InboundMessage) bool {
	policy := defaultControlPolicy()
	if al != nil {
		policy = al.controlPolicy
	}
	al.autonomyMu.Lock()
	s, ok := al.autonomyBySess[msg.SessionKey]
	if !ok || s == nil {
		al.autonomyMu.Unlock()
		return false
	}

	now := time.Now()
	if s.pending {
		if !s.pendingSince.IsZero() && now.Sub(s.pendingSince) > policy.autonomyMaxPendingDuration {
			s.pending = false
			s.pendingSince = time.Time{}
			s.stallCount++
			if s.stallCount >= policy.autonomyMaxConsecutiveStalls {
				if s.cancel != nil {
					s.cancel()
				}
				delete(al.autonomyBySess, msg.SessionKey)
				al.autonomyMu.Unlock()
				al.cancelSessionWorkerRun(msg.SessionKey)
				al.controlMetricAdd(&al.controlStats.autonomyStoppedByGuard, 1)
				return false
			}
		}
		al.autonomyMu.Unlock()
		return true
	}
	if now.Sub(s.lastUserAt) < policy.autonomyIdleThreshold ||
		now.Sub(s.lastNudgeAt) < policy.autonomyMinRunInterval {
		al.autonomyMu.Unlock()
		return true
	}

	s.rounds++
	round := s.rounds
	s.lastNudgeAt = now
	s.pending = true
	s.pendingSince = now
	reportDue := now.Sub(s.lastReportAt) >= s.idleInterval
	if reportDue {
		s.lastReportAt = now
	}
	focus := strings.TrimSpace(s.focus)
	al.autonomyMu.Unlock()
	al.controlMetricAdd(&al.controlStats.autonomyRounds, 1)

	al.bus.PublishInbound(bus.InboundMessage{
		Channel:    msg.Channel,
		SenderID:   "autonomy",
		ChatID:     msg.ChatID,
		SessionKey: msg.SessionKey,
		Content:    buildAutonomyFollowUpPrompt(round, focus, reportDue),
		Metadata: map[string]string{
			"source":     "autonomy",
			"round":      strconv.Itoa(round),
			"report_due": strconv.FormatBool(reportDue),
		},
	})

	return true
}

func (al *AgentLoop) finishAutonomyRound(sessionKey string) {
	al.autonomyMu.Lock()
	defer al.autonomyMu.Unlock()
	if s, ok := al.autonomyBySess[sessionKey]; ok && s != nil {
		s.pending = false
		s.pendingSince = time.Time{}
		s.stallCount = 0
	}
}

func buildAutonomyFollowUpPrompt(round int, focus string, reportDue bool) string {
	focus = strings.TrimSpace(focus)
	if focus == "" && reportDue {
		return fmt.Sprintf("Autonomy round %d: the user has not provided new input yet. Based on the current session context and completed work, autonomously complete one high-value next step and report progress or results in natural language.", round)
	}
	if focus == "" && !reportDue {
		return fmt.Sprintf("Autonomy round %d: the user has not provided new input yet. Based on the current session context and completed work, autonomously complete one high-value next step. This round is execution-only; do not send an external reply.", round)
	}
	if reportDue {
		return fmt.Sprintf("Autonomy round %d: the user has not provided new input yet. Prioritize progress around the focus \"%s\"; if that focus is complete, explain and move to another high-value next step. After completion, report progress or results in natural language.", round, focus)
	}
	return fmt.Sprintf("Autonomy round %d: the user has not provided new input yet. Prioritize progress around the focus \"%s\"; if that focus is complete, explain and move to another high-value next step. This round is execution-only; do not send an external reply.", round, focus)
}

func buildAutonomyFocusPrompt(focus string) string {
	focus = strings.TrimSpace(focus)
	return fmt.Sprintf("Autonomy mode started. For this round, prioritize the focus \"%s\": clarify the round goal first, then execute and report progress and results.", focus)
}

func (al *AgentLoop) startAutoLearner(ctx context.Context, msg bus.InboundMessage, interval time.Duration) {
	if msg.Channel == "cli" {
		return
	}

	if interval <= 0 {
		interval = autoLearnDefaultInterval
	}
	if interval < autoLearnMinInterval {
		interval = autoLearnMinInterval
	}

	al.autoLearnMu.Lock()
	if old, ok := al.autoLearners[msg.SessionKey]; ok {
		if old != nil && old.cancel != nil {
			old.cancel()
		}
		delete(al.autoLearners, msg.SessionKey)
	}

	langCtx := withUserLanguageHint(context.Background(), msg.SessionKey, msg.Content)
	learnerCtx, cancel := context.WithCancel(langCtx)
	learner := &autoLearner{
		cancel:   cancel,
		started:  time.Now(),
		interval: interval,
	}
	al.autoLearners[msg.SessionKey] = learner
	al.autoLearnMu.Unlock()

	go al.runAutoLearnerLoop(learnerCtx, msg)
	return
}

func (al *AgentLoop) runAutoLearnerLoop(ctx context.Context, msg bus.InboundMessage) {
	runOnce := func() bool {
		round, ok := al.bumpAutoLearnRound(msg.SessionKey)
		if !ok {
			return false
		}
		al.controlMetricAdd(&al.controlStats.autoLearnRounds, 1)

		al.bus.PublishInbound(bus.InboundMessage{
			Channel:    msg.Channel,
			SenderID:   "autolearn",
			ChatID:     msg.ChatID,
			SessionKey: msg.SessionKey,
			Content:    buildAutoLearnPrompt(round),
			Metadata: map[string]string{
				"source": "autolearn",
				"round":  strconv.Itoa(round),
			},
		})
		return true
	}

	if !runOnce() {
		return
	}

	ticker := time.NewTicker(al.autoLearnInterval(msg.SessionKey))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !runOnce() {
				return
			}
		}
	}
}

func (al *AgentLoop) autoLearnInterval(sessionKey string) time.Duration {
	al.autoLearnMu.Lock()
	defer al.autoLearnMu.Unlock()

	learner, ok := al.autoLearners[sessionKey]
	if !ok || learner == nil || learner.interval <= 0 {
		return autoLearnDefaultInterval
	}
	return learner.interval
}

func (al *AgentLoop) bumpAutoLearnRound(sessionKey string) (int, bool) {
	al.autoLearnMu.Lock()
	defer al.autoLearnMu.Unlock()

	learner, ok := al.autoLearners[sessionKey]
	if !ok || learner == nil {
		return 0, false
	}
	learner.rounds++
	return learner.rounds, true
}

func (al *AgentLoop) stopAutoLearner(sessionKey string) bool {
	al.autoLearnMu.Lock()
	defer al.autoLearnMu.Unlock()

	learner, ok := al.autoLearners[sessionKey]
	if !ok || learner == nil {
		return false
	}
	if learner.cancel != nil {
		learner.cancel()
	}
	delete(al.autoLearners, sessionKey)
	return true
}

func buildAutoLearnPrompt(round int) string {
	return fmt.Sprintf("Auto-learn round %d: no user task is required. Based on current session and project context, choose and complete one high-value small task autonomously. Requirements: 1) define the learning goal for this round; 2) call tools when needed; 3) write key conclusions to memory/MEMORY.md; 4) output a concise progress report.", round)
}

func buildAutonomyTaskPrompt(task string) string {
	return fmt.Sprintf("Enable autonomous execution strategy. Proceed with the task directly, report progress naturally at key points, and finally provide results plus next-step suggestions.\n\nUser task: %s", strings.TrimSpace(task))
}

func isSyntheticMessage(msg bus.InboundMessage) bool {
	if msg.SenderID == "autolearn" || msg.SenderID == "autonomy" {
		return true
	}
	if msg.Metadata == nil {
		return false
	}
	source := strings.ToLower(strings.TrimSpace(msg.Metadata["source"]))
	return source == "autolearn" || source == "autonomy"
}

func isAutonomySyntheticMessage(msg bus.InboundMessage) bool {
	if msg.SenderID == "autonomy" {
		return true
	}
	if msg.Metadata == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(msg.Metadata["source"]), "autonomy")
}

func shouldHandleControlIntents(msg bus.InboundMessage) bool {
	return !isSyntheticMessage(msg)
}

func (al *AgentLoop) beginAgentRun(msg bus.InboundMessage) agentRunLifecycle {
	seq := int64(1)
	if al != nil {
		seq = al.runSeq.Add(1)
	}
	acceptedAt := time.Now()
	run := agentRunLifecycle{
		runID:           fmt.Sprintf("run-%d-%d", acceptedAt.UnixNano(), seq),
		acceptedAt:      acceptedAt,
		sessionKey:      msg.SessionKey,
		channel:         msg.Channel,
		senderID:        msg.SenderID,
		chatID:          msg.ChatID,
		synthetic:       isSyntheticMessage(msg),
		controlEligible: shouldHandleControlIntents(msg),
	}
	if al != nil {
		al.controlMetricAdd(&al.controlStats.runAccepted, 1)
		al.recordRunAccepted(run)
	}
	logger.InfoCF("agent", "Run lifecycle accepted", map[string]interface{}{
		"run_id":            run.runID,
		"session_key":       run.sessionKey,
		logger.FieldChannel: run.channel,
		logger.FieldChatID:  run.chatID,
		"sender_id":         run.senderID,
		"synthetic":         run.synthetic,
		"control_eligible":  run.controlEligible,
		"accepted_at":       run.acceptedAt.Format(time.RFC3339Nano),
	})
	return run
}

func (al *AgentLoop) finishAgentRun(run agentRunLifecycle, err error, response string, controlHandled bool) {
	duration := time.Since(run.acceptedAt)
	if al != nil {
		if controlHandled {
			al.controlMetricAdd(&al.controlStats.runControlHandled, 1)
		}
		switch {
		case err == nil:
			al.controlMetricAdd(&al.controlStats.runCompleted, 1)
		case errors.Is(err, context.Canceled):
			al.controlMetricAdd(&al.controlStats.runCanceled, 1)
		default:
			al.controlMetricAdd(&al.controlStats.runFailed, 1)
		}
		al.recordRunFinished(run, err, response, controlHandled)
	}

	fields := map[string]interface{}{
		"run_id":            run.runID,
		"session_key":       run.sessionKey,
		logger.FieldChannel: run.channel,
		logger.FieldChatID:  run.chatID,
		"duration_ms":       duration.Milliseconds(),
		"response_len":      len(strings.TrimSpace(response)),
		"control_handled":   controlHandled,
	}
	if err != nil {
		fields[logger.FieldError] = err.Error()
		logger.WarnCF("agent", "Run lifecycle ended with error", fields)
		return
	}
	logger.InfoCF("agent", "Run lifecycle ended", fields)
}

func (al *AgentLoop) recordRunAccepted(run agentRunLifecycle) {
	if al == nil || strings.TrimSpace(run.runID) == "" {
		return
	}
	al.runStateMu.Lock()
	defer al.runStateMu.Unlock()
	rs := &runState{
		runID:           run.runID,
		sessionKey:      run.sessionKey,
		channel:         run.channel,
		chatID:          run.chatID,
		senderID:        run.senderID,
		synthetic:       run.synthetic,
		controlEligible: run.controlEligible,
		status:          runStatusRunning,
		acceptedAt:      run.acceptedAt,
		startedAt:       run.acceptedAt,
		done:            make(chan struct{}),
	}
	al.runStates[run.runID] = rs
	al.pruneRunStatesLocked(time.Now())
}

func (al *AgentLoop) recordRunFinished(run agentRunLifecycle, err error, response string, controlHandled bool) {
	if al == nil || strings.TrimSpace(run.runID) == "" {
		return
	}
	al.runStateMu.Lock()
	defer al.runStateMu.Unlock()
	rs, ok := al.runStates[run.runID]
	if !ok || rs == nil {
		rs = &runState{
			runID:      run.runID,
			sessionKey: run.sessionKey,
			channel:    run.channel,
			chatID:     run.chatID,
			senderID:   run.senderID,
			done:       make(chan struct{}),
		}
		al.runStates[run.runID] = rs
	}
	rs.endedAt = time.Now()
	rs.responseLen = len(strings.TrimSpace(response))
	rs.controlHandled = controlHandled
	if err == nil {
		rs.status = runStatusOK
	} else if errors.Is(err, context.Canceled) {
		rs.status = runStatusCanceled
		rs.errMessage = err.Error()
	} else {
		rs.status = runStatusError
		rs.errMessage = err.Error()
	}
	select {
	case <-rs.done:
	default:
		close(rs.done)
	}
	al.pruneRunStatesLocked(rs.endedAt)
}

func (al *AgentLoop) pruneRunStatesLocked(now time.Time) {
	if al == nil || al.runStates == nil {
		return
	}
	ttl := al.runStateTTL
	if ttl <= 0 {
		ttl = defaultRunStateTTL
	}
	maxEntries := al.runStateMax
	if maxEntries <= 0 {
		maxEntries = defaultRunStateMaxEntries
	}
	for id, rs := range al.runStates {
		if rs == nil {
			delete(al.runStates, id)
			continue
		}
		if rs.endedAt.IsZero() {
			continue
		}
		if now.Sub(rs.endedAt) > ttl {
			delete(al.runStates, id)
		}
	}
	if len(al.runStates) <= maxEntries {
		return
	}
	type pair struct {
		id string
		ts time.Time
	}
	items := make([]pair, 0, len(al.runStates))
	for id, rs := range al.runStates {
		ts := rs.startedAt
		if !rs.endedAt.IsZero() {
			ts = rs.endedAt
		}
		items = append(items, pair{id: id, ts: ts})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ts.Before(items[j].ts) })
	removeCount := len(items) - maxEntries
	for i := 0; i < removeCount; i++ {
		delete(al.runStates, items[i].id)
	}
}

func (al *AgentLoop) getRunState(runID string) (runState, bool) {
	if al == nil || strings.TrimSpace(runID) == "" {
		return runState{}, false
	}
	al.runStateMu.Lock()
	defer al.runStateMu.Unlock()
	rs, ok := al.runStates[runID]
	if !ok || rs == nil {
		return runState{}, false
	}
	return *rs, true
}

func (al *AgentLoop) latestRunState(sessionKey string) (runState, bool) {
	if al == nil {
		return runState{}, false
	}
	key := strings.TrimSpace(sessionKey)
	al.runStateMu.Lock()
	defer al.runStateMu.Unlock()

	var latest *runState
	var latestAt time.Time
	for _, rs := range al.runStates {
		if rs == nil {
			continue
		}
		if key != "" && rs.sessionKey != key {
			continue
		}
		candidateAt := rs.acceptedAt
		if !rs.startedAt.IsZero() {
			candidateAt = rs.startedAt
		}
		if !rs.endedAt.IsZero() {
			candidateAt = rs.endedAt
		}
		if latest == nil || candidateAt.After(latestAt) {
			cp := *rs
			latest = &cp
			latestAt = candidateAt
		}
	}
	if latest == nil {
		return runState{}, false
	}
	return *latest, true
}

func (al *AgentLoop) waitForRun(ctx context.Context, runID string) (runState, bool) {
	if al == nil || strings.TrimSpace(runID) == "" {
		return runState{}, false
	}
	al.runStateMu.Lock()
	rs, ok := al.runStates[runID]
	al.runStateMu.Unlock()
	if !ok || rs == nil {
		return runState{}, false
	}
	select {
	case <-ctx.Done():
		return runState{}, false
	case <-rs.done:
		return al.getRunState(runID)
	}
}

func normalizeRunWaitTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultRunWaitTimeout
	}
	if timeout < minRunWaitTimeout {
		return minRunWaitTimeout
	}
	if timeout > maxRunWaitTimeout {
		return maxRunWaitTimeout
	}
	return timeout
}

func containsAnySubstring(text string, values ...string) bool {
	for _, value := range values {
		if value != "" && strings.Contains(text, value) {
			return true
		}
	}
	return false
}

func (al *AgentLoop) executeRunControlIntent(ctx context.Context, sessionKey string, intent runControlIntent) string {
	var (
		rs    runState
		found bool
	)
	if intent.latest {
		rs, found = al.latestRunState(sessionKey)
	} else {
		rs, found = al.getRunState(intent.runID)
	}
	if !found {
		return ""
	}

	if intent.wait && (rs.status == runStatusAccepted || rs.status == runStatusRunning) {
		waitCtx, cancel := context.WithTimeout(ctx, intent.timeout)
		defer cancel()
		if waited, ok := al.waitForRun(waitCtx, rs.runID); ok {
			rs = waited
		} else {
			if latest, ok := al.getRunState(rs.runID); ok {
				rs = latest
			}
			return ""
		}
	}
	return ""
}

func (al *AgentLoop) inferRunControlIntent(ctx context.Context, content string) (runControlIntent, float64, bool) {
	text := strings.TrimSpace(content)
	if text == "" || strings.HasPrefix(text, "/") {
		return runControlIntent{}, 0, false
	}

	limit := defaultControlPolicy().intentMaxInputChars
	if al != nil && al.controlPolicy.intentMaxInputChars > 0 {
		limit = al.controlPolicy.intentMaxInputChars
	}
	if len(text) > limit {
		text = truncate(text, limit)
	}

	systemPrompt := al.withBootstrapPolicy(`Classify run-control intent. JSON only:
{"matched":false,"run_id":"","latest":false,"wait":false,"timeout_seconds":0,"confidence":0.0}
Set matched=true only for run status/wait/latest control; confidence in [0,1].`)

	resp, err := al.callLLMWithModelFallback(ctx, []providers.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: text},
	}, nil, map[string]interface{}{
		"max_tokens":  220,
		"temperature": 0.0,
	})
	if err != nil || resp == nil {
		return runControlIntent{}, 0, false
	}

	raw := extractJSONObject(resp.Content)
	if raw == "" {
		return runControlIntent{}, 0, false
	}

	var parsed runControlIntentLLMResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return runControlIntent{}, 0, false
	}
	if !parsed.Matched {
		return runControlIntent{}, parsed.Confidence, false
	}

	intent := runControlIntent{
		runID:   strings.ToLower(strings.TrimSpace(parsed.RunID)),
		latest:  parsed.Latest,
		wait:    parsed.Wait,
		timeout: defaultRunWaitTimeout,
	}
	if parsed.TimeoutSeconds > 0 {
		intent.timeout = time.Duration(parsed.TimeoutSeconds) * time.Second
	}
	intent.timeout = normalizeRunWaitTimeout(intent.timeout)

	if intent.runID == "" && !intent.latest {
		return runControlIntent{}, parsed.Confidence, false
	}
	return intent, parsed.Confidence, true
}

func (al *AgentLoop) handleNaturalRunControl(ctx context.Context, msg bus.InboundMessage) (bool, string) {
	intent, _, ok := al.inferRunControlIntent(ctx, msg.Content)
	if !ok {
		return false, ""
	}
	return true, al.executeRunControlIntent(ctx, msg.SessionKey, intent)
}

func (al *AgentLoop) controlMetricAdd(counter *int64, delta int64) {
	if al == nil || counter == nil {
		return
	}
	value := atomic.AddInt64(counter, delta)
	if value == 1 || value%20 == 0 {
		al.logControlStatsSnapshot()
	}
}

func (al *AgentLoop) logControlStatsSnapshot() {
	if al == nil {
		return
	}
	al.autonomyMu.Lock()
	autonomyActive := len(al.autonomyBySess)
	al.autonomyMu.Unlock()
	al.autoLearnMu.Lock()
	autoLearnActive := len(al.autoLearners)
	al.autoLearnMu.Unlock()
	al.runStateMu.Lock()
	runStatesTotal := len(al.runStates)
	al.runStateMu.Unlock()

	stats := map[string]interface{}{
		"run_accepted":              atomic.LoadInt64(&al.controlStats.runAccepted),
		"run_completed":             atomic.LoadInt64(&al.controlStats.runCompleted),
		"run_failed":                atomic.LoadInt64(&al.controlStats.runFailed),
		"run_canceled":              atomic.LoadInt64(&al.controlStats.runCanceled),
		"run_control_handled":       atomic.LoadInt64(&al.controlStats.runControlHandled),
		"intent_autonomy_matched":   atomic.LoadInt64(&al.controlStats.intentAutonomyMatched),
		"intent_autonomy_rejected":  atomic.LoadInt64(&al.controlStats.intentAutonomyRejected),
		"intent_autolearn_matched":  atomic.LoadInt64(&al.controlStats.intentAutoLearnMatched),
		"intent_autolearn_rejected": atomic.LoadInt64(&al.controlStats.intentAutoLearnRejected),
		"autonomy_rounds":           atomic.LoadInt64(&al.controlStats.autonomyRounds),
		"autonomy_stopped_by_guard": atomic.LoadInt64(&al.controlStats.autonomyStoppedByGuard),
		"autolearn_rounds":          atomic.LoadInt64(&al.controlStats.autoLearnRounds),
		"autonomy_active_sessions":  autonomyActive,
		"autolearn_active_sessions": autoLearnActive,
		"run_states_total":          runStatesTotal,
	}
	logger.InfoCF("agent", "Control runtime snapshot", stats)
}

func (al *AgentLoop) executeAutonomyIntent(ctx context.Context, msg bus.InboundMessage, intent autonomyIntent) {
	_ = ctx
	if intent.clearFocus {
		_ = al.clearAutonomyFocus(msg.SessionKey)
	}
	if intent.enabled == nil {
		return
	}
	if *intent.enabled {
		idle := autonomyDefaultIdleInterval
		if intent.idleInterval != nil {
			idle = *intent.idleInterval
		}
		al.startAutonomy(ctx, msg, idle, intent.focus)
		return
	}
	_ = al.stopAutonomy(msg.SessionKey)
}

func (al *AgentLoop) executeAutoLearnIntent(ctx context.Context, msg bus.InboundMessage, intent autoLearnIntent) {
	_ = ctx
	if intent.enabled == nil {
		return
	}
	if *intent.enabled {
		interval := autoLearnDefaultInterval
		if intent.interval != nil {
			interval = *intent.interval
		}
		al.startAutoLearner(ctx, msg, interval)
		return
	}
	_ = al.stopAutoLearner(msg.SessionKey)
}

func (al *AgentLoop) handleControlPlane(ctx context.Context, msg bus.InboundMessage) (handled bool, response string, err error) {
	if !shouldHandleControlIntents(msg) {
		return false, "", nil
	}

	// Deterministic commands first.
	if handled, result, cmdErr := al.handleSlashCommand(ctx, msg); handled {
		return true, result, cmdErr
	}

	al.noteAutonomyUserActivity(msg)

	if handled, result := al.handleNaturalRunControl(ctx, msg); handled {
		return true, result, nil
	}

	if intent, outcome := al.detectAutonomyIntent(ctx, msg.Content); outcome.matched {
		al.executeAutonomyIntent(ctx, msg, intent)
		return true, "", nil
	}

	if intent, outcome := al.detectAutoLearnIntent(ctx, msg.Content); outcome.matched {
		al.executeAutoLearnIntent(ctx, msg, intent)
		return true, "", nil
	}

	return false, "", nil
}

func withTokenUsageTotals(ctx context.Context) (context.Context, *tokenUsageTotals) {
	if ctx == nil {
		ctx = context.Background()
	}
	totals := &tokenUsageTotals{}
	return context.WithValue(ctx, tokenUsageTotalsKey{}, totals), totals
}

func tokenUsageTotalsFromContext(ctx context.Context) *tokenUsageTotals {
	if ctx == nil {
		return nil
	}
	totals, _ := ctx.Value(tokenUsageTotalsKey{}).(*tokenUsageTotals)
	return totals
}

func addTokenUsageToContext(ctx context.Context, usage *providers.UsageInfo) {
	totals := tokenUsageTotalsFromContext(ctx)
	if totals == nil || usage == nil {
		return
	}
	totals.input += usage.PromptTokens
	totals.output += usage.CompletionTokens
	if usage.TotalTokens > 0 {
		totals.total += usage.TotalTokens
	} else {
		totals.total += usage.PromptTokens + usage.CompletionTokens
	}
}

func formatTokenUsageSuffix(totals *tokenUsageTotals) string {
	formatTokenUnit := func(v int) string {
		switch {
		case v >= 1_000_000:
			f := float64(v) / 1_000_000
			s := strconv.FormatFloat(f, 'f', 1, 64)
			s = strings.TrimSuffix(s, ".0")
			return s + "m"
		case v >= 1_000:
			f := float64(v) / 1_000
			s := strconv.FormatFloat(f, 'f', 1, 64)
			s = strings.TrimSuffix(s, ".0")
			return s + "k"
		default:
			return strconv.Itoa(v)
		}
	}

	input := 0
	output := 0
	total := 0
	if totals != nil {
		input = totals.input
		output = totals.output
		total = totals.total
	}
	return fmt.Sprintf("\n\nUsage: in %s, out %s, total %s",
		formatTokenUnit(input), formatTokenUnit(output), formatTokenUnit(total))
}

func withUserLanguageHint(ctx context.Context, sessionKey, content string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, userLanguageHintKey{}, userLanguageHint{
		sessionKey: strings.TrimSpace(sessionKey),
		content:    content,
	})
}

func shouldPublishSyntheticResponse(msg bus.InboundMessage) bool {
	if !isSyntheticMessage(msg) {
		return true
	}
	if !isAutonomySyntheticMessage(msg) {
		return true
	}
	if msg.Metadata == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(msg.Metadata["report_due"]), "true")
}

func (al *AgentLoop) ProcessDirect(ctx context.Context, content, sessionKey string) (string, error) {
	msg := bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "user",
		ChatID:     "direct",
		Content:    content,
		SessionKey: sessionKey,
	}

	return al.processMessage(ctx, msg)
}

func (al *AgentLoop) processMessage(ctx context.Context, msg bus.InboundMessage) (response string, err error) {
	ctx = withUserLanguageHint(ctx, msg.SessionKey, msg.Content)
	run := al.beginAgentRun(msg)
	controlHandled := false
	defer func() {
		al.finishAgentRun(run, err, response, controlHandled)
	}()

	// Add message preview to log
	preview := truncate(msg.Content, 80)
	logger.InfoCF("agent", fmt.Sprintf("Processing message from %s:%s: %s", msg.Channel, msg.SenderID, preview),
		map[string]interface{}{
			logger.FieldChannel:  msg.Channel,
			logger.FieldChatID:   msg.ChatID,
			logger.FieldSenderID: msg.SenderID,
			"session_key":        msg.SessionKey,
		})

	if isAutonomySyntheticMessage(msg) {
		// Autonomy may have been stopped while synthetic messages are still queued.
		// Drop stale synthetic rounds so they don't block real user turns.
		if !al.isAutonomyEnabled(msg.SessionKey) {
			return "", nil
		}
		defer al.finishAutonomyRound(msg.SessionKey)
	}

	// Route system messages to processSystemMessage
	if msg.Channel == "system" {
		return al.processSystemMessage(ctx, msg)
	}

	if handled, controlResponse, controlErr := al.handleControlPlane(ctx, msg); handled {
		controlHandled = true
		return controlResponse, controlErr
	}

	directives := parseTaskExecutionDirectives(msg.Content)
	if run.controlEligible {
		if inferred, ok := al.inferTaskExecutionDirectives(ctx, msg.Content); ok {
			// Explicit /run/@run command always has higher priority than inferred directives.
			if !isExplicitRunCommand(msg.Content) {
				directives = inferred
			}
		}
	} else {
		// Synthetic messages should run quietly and only report final outcomes.
		directives.stageReport = false
	}
	userPrompt := directives.task
	if strings.TrimSpace(userPrompt) == "" {
		userPrompt = msg.Content
	}
	if al.isAutonomyEnabled(msg.SessionKey) && run.controlEligible {
		userPrompt = buildAutonomyTaskPrompt(userPrompt)
	}

	var progress *stageReporter
	if directives.stageReport {
		progress = &stageReporter{
			localize: func(content string) string {
				return content
			},
			onUpdate: func(content string) {
				al.bus.PublishOutbound(bus.OutboundMessage{
					Channel: msg.Channel,
					ChatID:  msg.ChatID,
					Content: content,
				})
			},
		}
		progress.Publish(1, 5, "start", "I received your task and will clarify the goal and constraints first.")
		progress.Publish(2, 5, "analysis", "I am building the context needed for execution.")
	}

	// Update tool contexts
	if tool, ok := al.tools.Get("message"); ok {
		if mt, ok := tool.(*tools.MessageTool); ok {
			mt.SetContext(msg.Channel, msg.ChatID)
		}
	}
	if tool, ok := al.tools.Get("spawn"); ok {
		if st, ok := tool.(*tools.SpawnTool); ok {
			st.SetContext(msg.Channel, msg.ChatID)
		}
	}

	history := al.sessions.GetHistory(msg.SessionKey)
	summary := al.sessions.GetSummary(msg.SessionKey)

	messages := al.contextBuilder.BuildMessages(
		history,
		summary,
		userPrompt,
		msg.Media,
		msg.Channel,
		msg.ChatID,
	)

	if progress != nil {
		progress.Publish(3, 5, "execution", "I am starting step-by-step execution.")
	}

	finalContent, iteration, err := al.runLLMToolLoop(ctx, messages, msg.SessionKey, false, progress)
	if err != nil {
		if progress != nil {
			progress.Publish(5, 5, "failure", err.Error())
		}
		return "", err
	}
	if finalContent == "" {
		finalContent = "Done."
	}

	// Filter out <think>...</think> content from user-facing response
	// Keep full content in debug logs if needed, but remove from final output
	re := regexp.MustCompile(`(?s)<think>.*?</think>`)
	userContent := re.ReplaceAllString(finalContent, "")
	userContent = strings.TrimSpace(userContent)
	if userContent == "" && finalContent != "" {
		// If only thoughts were present, maybe provide a generic "Done" or keep something?
		// For now, let's assume thoughts are auxiliary and empty response is okay if tools did work.
		// If no tools ran and only thoughts, user might be confused.
		if iteration == 1 {
			userContent = "Thinking process completed."
		}
	}

	al.sessions.AddMessage(msg.SessionKey, "user", msg.Content)

	// Use AddMessageFull to persist the complete assistant message, including thoughts/tool calls.
	al.sessions.AddMessageFull(msg.SessionKey, providers.Message{
		Role:    "assistant",
		Content: userContent,
	})

	if err := al.persistSessionWithCompaction(ctx, msg.SessionKey); err != nil {
		logger.WarnCF("agent", "Failed to save session metadata", map[string]interface{}{
			"session_key":     msg.SessionKey,
			logger.FieldError: err.Error(),
		})
	}

	// Log response preview (original content)
	responsePreview := truncate(finalContent, 120)
	logger.InfoCF("agent", fmt.Sprintf("Response to %s:%s: %s", msg.Channel, msg.SenderID, responsePreview),
		map[string]interface{}{
			"iterations":                          iteration,
			logger.FieldAssistantContentLength:    len(finalContent),
			logger.FieldUserResponseContentLength: len(userContent),
		})

	if progress != nil {
		progress.Publish(4, 5, "finalization", "Final response is ready.")
		progress.Publish(5, 5, "done", "Task completed.")
	}

	return userContent, nil
}

func (al *AgentLoop) processSystemMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	// Verify this is a system message
	if msg.Channel != "system" {
		return "", fmt.Errorf("processSystemMessage called with non-system message channel: %s", msg.Channel)
	}

	logger.InfoCF("agent", "Processing system message",
		map[string]interface{}{
			logger.FieldSenderID: msg.SenderID,
			logger.FieldChatID:   msg.ChatID,
		})

	// Parse origin from chat_id (format: "channel:chat_id")
	var originChannel, originChatID string
	if idx := strings.Index(msg.ChatID, ":"); idx > 0 {
		originChannel = msg.ChatID[:idx]
		originChatID = msg.ChatID[idx+1:]
	} else {
		// Fallback
		originChannel = "cli"
		originChatID = msg.ChatID
	}

	// Use the origin session for context
	sessionKey := fmt.Sprintf("%s:%s", originChannel, originChatID)

	// Update tool contexts to original channel/chatID
	if tool, ok := al.tools.Get("message"); ok {
		if mt, ok := tool.(*tools.MessageTool); ok {
			mt.SetContext(originChannel, originChatID)
		}
	}
	if tool, ok := al.tools.Get("spawn"); ok {
		if st, ok := tool.(*tools.SpawnTool); ok {
			st.SetContext(originChannel, originChatID)
		}
	}

	// Build messages with the announce content
	history := al.sessions.GetHistory(sessionKey)
	summary := al.sessions.GetSummary(sessionKey)
	messages := al.contextBuilder.BuildMessages(
		history,
		summary,
		msg.Content,
		nil,
		originChannel,
		originChatID,
	)

	finalContent, iteration, err := al.runLLMToolLoop(ctx, messages, sessionKey, true, nil)
	if err != nil {
		return "", err
	}

	if finalContent == "" {
		finalContent = "Background task completed."
	}

	// Save to session with system message marker
	al.sessions.AddMessage(sessionKey, "user", fmt.Sprintf("[System: %s] %s", msg.SenderID, msg.Content))

	// If finalContent has no tool calls (i.e., the final LLM output),
	// earlier steps were already stored via AddMessageFull in the loop.
	// This AddMessageFull stores the final reply.
	al.sessions.AddMessageFull(sessionKey, providers.Message{
		Role:    "assistant",
		Content: finalContent,
	})

	if err := al.persistSessionWithCompaction(ctx, sessionKey); err != nil {
		logger.WarnCF("agent", "Failed to save session metadata", map[string]interface{}{
			"session_key":     sessionKey,
			logger.FieldError: err.Error(),
		})
	}

	logger.InfoCF("agent", "System message processing completed",
		map[string]interface{}{
			"iterations":                       iteration,
			logger.FieldAssistantContentLength: len(finalContent),
		})

	return finalContent, nil
}

func (al *AgentLoop) runLLMToolLoop(
	ctx context.Context,
	messages []providers.Message,
	sessionKey string,
	systemMode bool,
	progress *stageReporter,
) (string, int, error) {
	messages = sanitizeMessagesForToolCalling(messages)

	state := toolLoopState{}

	for state.iteration < al.maxIterations {
		state.iteration++
		iteration := state.iteration
		if progress != nil {
			progress.Publish(3, 5, "execution", "正在执行下一轮。")
		}

		providerToolDefs, err := buildProviderToolDefs(al.tools.GetDefinitions())
		if err != nil {
			return "", iteration, fmt.Errorf("invalid tool definition: %w", err)
		}

		response, llmElapsed, err := al.planToolCalls(ctx, messages, providerToolDefs, iteration, systemMode)
		if err != nil {
			return "", iteration, fmt.Errorf("LLM call failed: %w", err)
		}

		doneLog := "LLM call completed"
		if systemMode {
			doneLog = "LLM call completed (system message)"
		}
		logger.InfoCF("agent", doneLog,
			map[string]interface{}{
				"iteration": iteration,
				"elapsed":   llmElapsed.String(),
				"model":     al.model,
			})

		if len(response.ToolCalls) == 0 {
			state.finalContent = response.Content
			state.consecutiveAllToolErrorRounds = 0
			state.repeatedToolCallRounds = 0
			state.lastToolCallSignature = ""
			if !systemMode {
				logger.InfoCF("agent", "LLM response without tool calls (direct answer)",
					map[string]interface{}{
						"iteration":                        iteration,
						logger.FieldAssistantContentLength: len(state.finalContent),
					})
			}
			break
		}

		currentSignature := toolCallsSignature(response.ToolCalls)
		if currentSignature != "" && currentSignature == state.lastToolCallSignature {
			state.repeatedToolCallRounds++
		} else {
			state.repeatedToolCallRounds = 0
			state.lastToolCallSignature = currentSignature
		}
		if state.repeatedToolCallRounds >= toolLoopRepeatSignatureThreshold {
			logger.WarnCF("agent", "Repeated tool-call pattern detected, forcing finalization", map[string]interface{}{
				"iteration":    iteration,
				"session_key":  sessionKey,
				"repeat_round": state.repeatedToolCallRounds,
			})
			break
		}

		toolNames := make([]string, 0, len(response.ToolCalls))
		for _, tc := range response.ToolCalls {
			toolNames = append(toolNames, tc.Name)
		}
		logger.InfoCF("agent", "LLM requested tool calls",
			map[string]interface{}{
				"tools":     toolNames,
				"count":     len(toolNames),
				"iteration": iteration,
			})

		budget := al.computeToolLoopBudget(state)
		outcome := al.actToolCalls(ctx, response.Content, response.ToolCalls, &messages, sessionKey, iteration, budget, systemMode, progress)
		state.lastToolResult = outcome.lastToolResult
		if outcome.executedCalls > 0 && outcome.roundToolErrors == outcome.executedCalls {
			state.consecutiveAllToolErrorRounds++
		} else {
			state.consecutiveAllToolErrorRounds = 0
		}
		if outcome.truncated && !systemMode {
			logger.WarnCF("agent", "Tool execution truncated by budget", map[string]interface{}{
				"iteration":      iteration,
				"session_key":    sessionKey,
				"executed_calls": outcome.executedCalls,
				"dropped_calls":  outcome.droppedCalls,
			})
		}
		if state.consecutiveAllToolErrorRounds >= toolLoopAllErrorRoundsThreshold {
			logger.WarnCF("agent", "Consecutive all-tool-error rounds detected, forcing finalization", map[string]interface{}{
				"iteration":           iteration,
				"session_key":         sessionKey,
				"error_rounds":        state.consecutiveAllToolErrorRounds,
				"tools_in_last_round": outcome.executedCalls,
			})
			break
		}
		if outcome.blockedLikely {
			finalResp, ferr := al.finalizeToolLoop(ctx, messages)
			if ferr == nil && finalResp != nil && strings.TrimSpace(finalResp.Content) != "" {
				state.finalContent = finalResp.Content
			}
			break
		}
	}

	// When max iterations are reached without a direct answer, ask once more without tools.
	// This avoids returning placeholder text to end users.
	if state.finalContent == "" && len(messages) > 0 {
		if !systemMode && state.iteration >= al.maxIterations {
			logger.WarnCF("agent", "Max tool iterations reached without final answer; forcing finalization pass", map[string]interface{}{
				"iteration":   state.iteration,
				"max":         al.maxIterations,
				"session_key": sessionKey,
			})
		}

		finalResp, err := al.finalizeToolLoop(ctx, messages)
		if err != nil {
			logger.WarnCF("agent", "Finalization pass failed", map[string]interface{}{
				"iteration":       state.iteration,
				"session_key":     sessionKey,
				logger.FieldError: err.Error(),
			})
		} else if strings.TrimSpace(finalResp.Content) != "" {
			state.finalContent = finalResp.Content
		}
	}

	if state.finalContent == "" {
		state.finalContent = strings.TrimSpace(state.lastToolResult)
	}

	return state.finalContent, state.iteration, nil
}

type toolLoopState struct {
	iteration                     int
	finalContent                  string
	lastToolResult                string
	lastToolCallSignature         string
	repeatedToolCallRounds        int
	consecutiveAllToolErrorRounds int
}

type toolActOutcome struct {
	roundToolErrors int
	lastToolResult  string
	executedCalls   int
	droppedCalls    int
	truncated       bool
	emptyResults    int
	retryableErrors int
	hardErrors      int
	blockedLikely   bool
	records         []toolExecutionRecord
}

type finalizeQualityResponse struct {
	Score float64 `json:"score"`
}

type toolExecutionRecord struct {
	Tool       string `json:"tool"`
	Status     string `json:"status"`
	ErrorType  string `json:"error_type,omitempty"`
	Retryable  bool   `json:"retryable,omitempty"`
	ErrMessage string `json:"error,omitempty"`
}

type toolLoopBudget struct {
	maxCallsPerIteration int
	singleCallTimeout    time.Duration
	maxActDuration       time.Duration
}

type toolCallExecResult struct {
	index  int
	call   providers.ToolCall
	result string
	err    error
}

func (al *AgentLoop) planToolCalls(
	ctx context.Context,
	messages []providers.Message,
	providerToolDefs []providers.ToolDefinition,
	iteration int,
	systemMode bool,
) (*providers.LLMResponse, time.Duration, error) {
	if !systemMode {
		logger.DebugCF("agent", "LLM iteration", map[string]interface{}{
			"iteration": iteration,
			"max":       al.maxIterations,
		})
	}

	messages = sanitizeMessagesForToolCalling(messages)
	systemPromptLen := 0
	if len(messages) > 0 {
		systemPromptLen = len(messages[0].Content)
	}
	logger.DebugCF("agent", "LLM request", map[string]interface{}{
		"iteration":         iteration,
		"model":             al.model,
		"messages_count":    len(messages),
		"tools_count":       len(providerToolDefs),
		"max_tokens":        8192,
		"temperature":       0.7,
		"system_prompt_len": systemPromptLen,
	})
	logger.DebugCF("agent", "Full LLM request", map[string]interface{}{
		"iteration":     iteration,
		"messages_json": formatMessagesForLog(messages),
		"tools_json":    formatToolsForLog(providerToolDefs),
	})

	llmStart := time.Now()
	llmCtx, cancelLLM := context.WithTimeout(ctx, al.llmCallTimeout)
	response, err := al.callLLMWithModelFallback(llmCtx, messages, providerToolDefs, map[string]interface{}{
		"max_tokens":  8192,
		"temperature": 0.7,
	})
	cancelLLM()
	llmElapsed := time.Since(llmStart)
	if err != nil {
		errLog := "LLM call failed"
		if systemMode {
			errLog = "LLM call failed in system message"
		}
		logger.ErrorCF("agent", errLog, map[string]interface{}{
			"iteration":       iteration,
			logger.FieldError: err.Error(),
			"elapsed":         llmElapsed.String(),
		})
		return nil, llmElapsed, err
	}
	return response, llmElapsed, nil
}

func (al *AgentLoop) actToolCalls(
	ctx context.Context,
	assistantContent string,
	toolCalls []providers.ToolCall,
	messages *[]providers.Message,
	sessionKey string,
	iteration int,
	budget toolLoopBudget,
	systemMode bool,
	progress *stageReporter,
) toolActOutcome {
	outcome := toolActOutcome{}
	if len(toolCalls) == 0 {
		return outcome
	}
	execCalls := toolCalls
	maxCalls := budget.maxCallsPerIteration
	if maxCalls <= 0 {
		maxCalls = toolLoopMaxCallsPerIteration
	}
	if len(execCalls) > maxCalls {
		outcome.truncated = true
		outcome.droppedCalls = len(execCalls) - maxCalls
		execCalls = execCalls[:maxCalls]
	}

	assistantMsg := providers.Message{
		Role:    "assistant",
		Content: assistantContent,
	}
	for _, tc := range execCalls {
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
	*messages = append(*messages, assistantMsg)
	al.sessions.AddMessageFull(sessionKey, assistantMsg)

	start := time.Now()
	maxActDuration := budget.maxActDuration
	if maxActDuration <= 0 {
		maxActDuration = toolLoopMaxActDuration
	}
	singleTimeout := budget.singleCallTimeout
	if singleTimeout <= 0 {
		singleTimeout = toolLoopSingleCallTimeout
	}
	roundCtx, cancelRound := context.WithTimeout(ctx, maxActDuration)
	defer cancelRound()

	parallel := al.shouldRunToolCallsInParallel(execCalls)
	results := al.executeToolCalls(roundCtx, execCalls, iteration, singleTimeout, parallel, systemMode, progress)
	if time.Since(start) >= maxActDuration && len(results) < len(execCalls) {
		outcome.truncated = true
		outcome.droppedCalls += len(execCalls) - len(results)
	}

	for i, execRes := range results {
		tc := execRes.call
		result := execRes.result
		err := execRes.err
		record := toolExecutionRecord{
			Tool:   tc.Name,
			Status: "ok",
		}
		if err != nil {
			result = fmt.Sprintf("Error: %v", err)
			outcome.roundToolErrors++
			record.Status = "error"
			record.ErrorType, record.Retryable, outcome.blockedLikely = classifyToolExecutionError(err, outcome.blockedLikely)
			record.ErrMessage = truncate(err.Error(), 240)
			if record.Retryable {
				outcome.retryableErrors++
			} else {
				outcome.hardErrors++
			}
		} else if strings.TrimSpace(result) == "" {
			outcome.emptyResults++
			record.Status = "empty"
		}
		if progress != nil {
			if err != nil {
				progress.Publish(3, 5, "execution", "工具执行失败。")
			} else {
				progress.Publish(3, 5, "execution", "工具执行完成。")
			}
		}
		outcome.lastToolResult = result

		toolResultMsg := providers.Message{
			Role:       "tool",
			Content:    result,
			ToolCallID: tc.ID,
		}
		*messages = append(*messages, toolResultMsg)
		if shouldPersistToolResultRecord(record, i, len(results)) {
			al.sessions.AddMessageFull(sessionKey, toolResultMsg)
		}
		outcome.executedCalls++
		outcome.records = append(outcome.records, record)
	}
	return outcome
}

func (al *AgentLoop) executeToolCalls(
	ctx context.Context,
	execCalls []providers.ToolCall,
	iteration int,
	singleTimeout time.Duration,
	parallel bool,
	systemMode bool,
	progress *stageReporter,
) []toolCallExecResult {
	if parallel {
		return al.executeToolCallsBatchedParallel(ctx, execCalls, iteration, singleTimeout, systemMode, progress)
	}
	return al.executeToolCallsSerial(ctx, execCalls, iteration, singleTimeout, systemMode, progress)
}

func (al *AgentLoop) executeToolCallsBatchedParallel(
	ctx context.Context,
	execCalls []providers.ToolCall,
	iteration int,
	singleTimeout time.Duration,
	systemMode bool,
	progress *stageReporter,
) []toolCallExecResult {
	batches := al.buildParallelBatches(execCalls)
	results := make([]toolCallExecResult, 0, len(execCalls))
	for _, batch := range batches {
		select {
		case <-ctx.Done():
			return results
		default:
		}
		if len(batch) <= 1 {
			if len(batch) == 1 {
				results = append(results, al.executeSingleToolCall(ctx, len(results), batch[0], iteration, singleTimeout, systemMode, progress))
			}
			continue
		}
		out := al.executeToolCallsParallel(ctx, batch, iteration, singleTimeout, systemMode, progress)
		results = append(results, out...)
	}
	return results
}

func (al *AgentLoop) executeToolCallsSerial(
	ctx context.Context,
	execCalls []providers.ToolCall,
	iteration int,
	singleTimeout time.Duration,
	systemMode bool,
	progress *stageReporter,
) []toolCallExecResult {
	results := make([]toolCallExecResult, 0, len(execCalls))
	for i, tc := range execCalls {
		select {
		case <-ctx.Done():
			return results
		default:
		}
		res := al.executeSingleToolCall(ctx, i, tc, iteration, singleTimeout, systemMode, progress)
		results = append(results, res)
	}
	return results
}

func (al *AgentLoop) executeToolCallsParallel(
	ctx context.Context,
	execCalls []providers.ToolCall,
	iteration int,
	singleTimeout time.Duration,
	systemMode bool,
	progress *stageReporter,
) []toolCallExecResult {
	results := make([]toolCallExecResult, len(execCalls))
	limit := al.maxToolParallelCalls()
	if limit <= 1 {
		return al.executeToolCallsSerial(ctx, execCalls, iteration, singleTimeout, systemMode, progress)
	}
	if len(execCalls) < limit {
		limit = len(execCalls)
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i, tc := range execCalls {
		select {
		case <-ctx.Done():
			goto wait
		default:
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(i int, tc providers.ToolCall) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = al.executeSingleToolCall(ctx, i, tc, iteration, singleTimeout, systemMode, progress)
		}(i, tc)
	}
wait:
	wg.Wait()
	out := make([]toolCallExecResult, 0, len(execCalls))
	for i := range results {
		if strings.TrimSpace(results[i].call.Name) == "" {
			continue
		}
		out = append(out, results[i])
	}
	return out
}

func (al *AgentLoop) buildParallelBatches(execCalls []providers.ToolCall) [][]providers.ToolCall {
	if len(execCalls) == 0 {
		return nil
	}
	batches := make([][]providers.ToolCall, 0, len(execCalls))
	current := make([]providers.ToolCall, 0, len(execCalls))
	used := map[string]struct{}{}

	flush := func() {
		if len(current) == 0 {
			return
		}
		batch := append([]providers.ToolCall(nil), current...)
		batches = append(batches, batch)
		current = current[:0]
		used = map[string]struct{}{}
	}

	for _, tc := range execCalls {
		keys := al.toolResourceKeys(tc.Name, tc.Arguments)
		if len(current) > 0 && hasResourceKeyConflict(used, keys) {
			flush()
		}
		current = append(current, tc)
		for _, k := range keys {
			used[k] = struct{}{}
		}
	}
	flush()
	return batches
}

func hasResourceKeyConflict(used map[string]struct{}, keys []string) bool {
	if len(keys) == 0 || len(used) == 0 {
		return false
	}
	for _, k := range keys {
		if _, ok := used[k]; ok {
			return true
		}
	}
	return false
}

func (al *AgentLoop) toolResourceKeys(name string, args map[string]interface{}) []string {
	raw := strings.TrimSpace(name)
	lower := strings.ToLower(raw)
	if raw == "" || al == nil || al.tools == nil {
		return nil
	}
	tool, ok := al.tools.Get(raw)
	if !ok && lower != raw {
		tool, ok = al.tools.Get(lower)
	}
	if !ok || tool == nil {
		return nil
	}
	rs, ok := tool.(tools.ResourceScopedTool)
	if !ok {
		return nil
	}
	return normalizeResourceKeys(rs.ResourceKeys(args))
}

func normalizeResourceKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	out := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		n := strings.ToLower(strings.TrimSpace(k))
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func (al *AgentLoop) executeSingleToolCall(
	ctx context.Context,
	index int,
	tc providers.ToolCall,
	iteration int,
	singleTimeout time.Duration,
	systemMode bool,
	progress *stageReporter,
) toolCallExecResult {
	if !systemMode {
		safeArgs := sanitizeSensitiveToolArgs(tc.Arguments)
		argsJSON, _ := json.Marshal(safeArgs)
		argsPreview := truncate(string(argsJSON), 200)
		logger.InfoCF("agent", fmt.Sprintf("Tool call: %s(%s)", tc.Name, argsPreview), map[string]interface{}{
			"tool":      tc.Name,
			"iteration": iteration,
		})
	}

	toolCtx, cancelTool := context.WithTimeout(ctx, singleTimeout)
	defer cancelTool()
	result, err := al.tools.Execute(toolCtx, tc.Name, tc.Arguments)
	if err != nil {
		result = fmt.Sprintf("Error: %v", err)
	}
	if progress != nil {
		if err != nil {
			progress.Publish(3, 5, "execution", "工具执行失败。")
		} else {
			progress.Publish(3, 5, "execution", "工具执行完成。")
		}
	}
	return toolCallExecResult{
		index:  index,
		call:   tc,
		result: result,
		err:    err,
	}
}

func (al *AgentLoop) shouldRunToolCallsInParallel(calls []providers.ToolCall) bool {
	if len(calls) <= 1 {
		return false
	}
	for _, c := range calls {
		if !al.isParallelSafeTool(c.Name) {
			return false
		}
	}
	return true
}

func (al *AgentLoop) isParallelSafeTool(name string) bool {
	raw := strings.TrimSpace(name)
	tool := strings.ToLower(raw)
	if raw == "" {
		return false
	}
	if al != nil && al.tools != nil {
		if t, ok := al.tools.Get(raw); ok {
			if ps, ok := t.(tools.ParallelSafeTool); ok {
				return ps.ParallelSafe()
			}
		}
		if raw != tool {
			if t, ok := al.tools.Get(tool); ok {
				if ps, ok := t.(tools.ParallelSafeTool); ok {
					return ps.ParallelSafe()
				}
			}
		}
	}
	allowed := al.effectiveParallelSafeTools()
	_, ok := allowed[tool]
	return ok
}

func (al *AgentLoop) effectiveParallelSafeTools() map[string]struct{} {
	if al == nil || len(al.parallelSafeTools) == 0 {
		m := make(map[string]struct{}, len(defaultParallelSafeToolNames))
		for _, name := range defaultParallelSafeToolNames {
			m[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
		}
		return m
	}
	return al.parallelSafeTools
}

func (al *AgentLoop) maxToolParallelCalls() int {
	if al == nil || al.maxParallelCalls <= 0 {
		return toolLoopMaxParallelCalls
	}
	return al.maxParallelCalls
}

func (al *AgentLoop) finalizeToolLoop(ctx context.Context, messages []providers.Message) (*providers.LLMResponse, error) {
	finalizeMessages := append([]providers.Message{}, messages...)
	finalizeMessages = append(finalizeMessages, providers.Message{
		Role:    "user",
		Content: "Provide final user response from current results. No tool calls.",
	})
	finalizeMessages = sanitizeMessagesForToolCalling(finalizeMessages)

	llmCtx, cancelLLM := context.WithTimeout(ctx, al.llmCallTimeout)
	draftResp, err := al.callLLMWithModelFallback(llmCtx, finalizeMessages, nil, map[string]interface{}{
		"max_tokens":  1024,
		"temperature": 0.3,
	})
	cancelLLM()
	if err != nil {
		return nil, err
	}
	if draftResp == nil || strings.TrimSpace(draftResp.Content) == "" {
		return draftResp, nil
	}
	// Gate polish by draft quality to avoid unnecessary extra LLM pass.
	if !shouldRunFinalizePolish(draftResp.Content) {
		return draftResp, nil
	}
	quality := al.assessFinalizeDraftQuality(ctx, draftResp.Content)
	if quality >= finalizeQualityThreshold {
		return draftResp, nil
	}

	polished, perr := al.polishFinalResponse(ctx, draftResp.Content)
	if perr != nil || strings.TrimSpace(polished) == "" {
		return draftResp, nil
	}
	return &providers.LLMResponse{
		Content: polished,
		Usage:   draftResp.Usage,
	}, nil
}

func shouldRunFinalizePolish(draft string) bool {
	text := strings.TrimSpace(draft)
	if len(text) < finalizeDraftMinCharsForPolish {
		return false
	}
	return containsAnySubstring(strings.ToLower(text), " - ", "1.", "2.", "next", "建议", "步骤", "\n")
}

func (al *AgentLoop) assessFinalizeDraftQuality(ctx context.Context, draft string) float64 {
	if strings.TrimSpace(draft) == "" {
		return 0
	}
	heuristic := localFinalizeDraftQualityScore(draft)
	if heuristic >= finalizeHeuristicHighThreshold || heuristic <= finalizeHeuristicLowThreshold || al == nil {
		return heuristic
	}

	// Only call LLM when heuristic is uncertain; keep this call lightweight.
	msgs := []providers.Message{
		{
			Role:    "user",
			Content: `Rate draft quality for user delivery. JSON only: {"score":0.0}. score in [0,1].`,
		},
		{
			Role:    "user",
			Content: draft,
		},
	}
	timeout := al.llmCallTimeout / 4
	if timeout < 2*time.Second {
		timeout = 2 * time.Second
	}
	qctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := al.callLLMWithModelFallback(qctx, msgs, nil, map[string]interface{}{
		"max_tokens":  28,
		"temperature": 0.0,
	})
	if err != nil || resp == nil {
		return heuristic
	}
	raw := extractJSONObject(resp.Content)
	if raw == "" {
		return heuristic
	}
	var parsed finalizeQualityResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return heuristic
	}
	if parsed.Score < 0 {
		return heuristic
	}
	if parsed.Score > 1 {
		parsed.Score = 1
	}
	// Blend heuristic and LLM score for stability while keeping calls cheap.
	return clamp01(0.6*heuristic + 0.4*parsed.Score)
}

func localFinalizeDraftQualityScore(draft string) float64 {
	text := strings.TrimSpace(draft)
	if text == "" {
		return 0
	}

	score := 0.0
	length := len(text)
	switch {
	case length >= 420:
		score += 0.36
	case length >= 260:
		score += 0.30
	case length >= 140:
		score += 0.22
	case length >= 80:
		score += 0.14
	default:
		score += 0.06
	}

	lower := strings.ToLower(text)
	if containsAnySubstring(lower, "\n-", "\n1.", "\n2.", "next", "steps", "建议", "步骤", "下一步") {
		score += 0.22
	}
	if containsAnySubstring(lower, "because", "therefore", "原因", "结论", "result", "建议") {
		score += 0.14
	}
	if containsAnySubstring(lower, "error", "failed", "unknown", "todo", "tbd") {
		score -= 0.08
	}

	lines := strings.Split(text, "\n")
	if hasExcessiveDuplicateLines(lines) {
		score -= 0.18
	}
	return clamp01(score)
}

func hasExcessiveDuplicateLines(lines []string) bool {
	if len(lines) < 3 {
		return false
	}
	seen := map[string]int{}
	dup := 0
	total := 0
	for _, line := range lines {
		n := strings.TrimSpace(strings.ToLower(line))
		if n == "" {
			continue
		}
		total++
		seen[n]++
		if seen[n] > 1 {
			dup++
		}
	}
	if total == 0 {
		return false
	}
	return float64(dup)/float64(total) > 0.25
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func (al *AgentLoop) polishFinalResponse(ctx context.Context, draft string) (string, error) {
	if al == nil || strings.TrimSpace(draft) == "" {
		return draft, nil
	}
	// Phase-2 polish: keep facts, remove repetition, and present concise actionable output.
	polishMessages := []providers.Message{
		{
			Role:    "system",
			Content: al.withBootstrapPolicy(`Rewrite for end users: preserve facts, concise/actionable, remove repetition/internal reasoning, plain text.`),
		},
		{
			Role:    "user",
			Content: draft,
		},
	}
	timeout := al.llmCallTimeout / 2
	if timeout < 3*time.Second {
		timeout = 3 * time.Second
	}
	llmCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := al.callLLMWithModelFallback(llmCtx, polishMessages, nil, map[string]interface{}{
		"max_tokens":  520,
		"temperature": 0.2,
	})
	if err != nil || resp == nil {
		return "", err
	}
	return strings.TrimSpace(resp.Content), nil
}

func classifyToolExecutionError(err error, blockedAlready bool) (errType string, retryable bool, blockedLikely bool) {
	blockedLikely = blockedAlready
	if err == nil {
		return "none", false, blockedLikely
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout", true, blockedLikely
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case containsAnySubstring(msg, "forbidden", "permission", "denied", "unauthorized", "not allowed"):
		return "permission", false, true
	case containsAnySubstring(msg, "missing", "required", "invalid argument", "bad argument", "invalid parameter", "parse"):
		return "invalid_input", false, true
	case containsAnySubstring(msg, "not found", "no such file", "does not exist"):
		return "not_found", false, blockedLikely
	case containsAnySubstring(msg, "timeout", "temporary", "temporarily", "unavailable", "connection reset", "connection refused", "rate limit", "429", "502", "503", "504"):
		return "transient", true, blockedLikely
	default:
		return "unknown", false, blockedLikely
	}
}

func (al *AgentLoop) computeToolLoopBudget(state toolLoopState) toolLoopBudget {
	b := toolLoopBudget{
		maxCallsPerIteration: toolLoopMaxCallsPerIteration,
		singleCallTimeout:    toolLoopSingleCallTimeout,
		maxActDuration:       toolLoopMaxActDuration,
	}
	// Early rounds can be slightly wider if no degradation signals.
	if state.iteration <= 1 && state.consecutiveAllToolErrorRounds == 0 && state.repeatedToolCallRounds == 0 {
		b.maxCallsPerIteration = toolLoopMaxCallsPerIteration + 1
		b.maxActDuration = toolLoopMaxActDuration + 10*time.Second
	}
	// If failures accumulate, tighten budget to force quicker convergence.
	if state.consecutiveAllToolErrorRounds > 0 || state.repeatedToolCallRounds > 0 {
		b.maxCallsPerIteration = toolLoopMaxCallsPerIteration - 2
		b.singleCallTimeout = toolLoopSingleCallTimeout - 6*time.Second
		b.maxActDuration = toolLoopMaxActDuration - 15*time.Second
	}
	// Near max iterations, force conservative execution.
	if al != nil && state.iteration >= al.maxIterations-1 {
		b.maxCallsPerIteration = toolLoopMinCallsPerIteration
		b.singleCallTimeout = toolLoopMinSingleCallTimeout
		b.maxActDuration = toolLoopMinActDuration
	}
	if b.maxCallsPerIteration > toolLoopMaxCallsPerIteration+2 {
		b.maxCallsPerIteration = toolLoopMaxCallsPerIteration + 2
	}
	if b.singleCallTimeout > toolLoopSingleCallTimeout+5*time.Second {
		b.singleCallTimeout = toolLoopSingleCallTimeout + 5*time.Second
	}
	if b.maxActDuration > toolLoopMaxActDuration+12*time.Second {
		b.maxActDuration = toolLoopMaxActDuration + 12*time.Second
	}
	if b.maxCallsPerIteration < toolLoopMinCallsPerIteration {
		b.maxCallsPerIteration = toolLoopMinCallsPerIteration
	}
	if b.singleCallTimeout < toolLoopMinSingleCallTimeout {
		b.singleCallTimeout = toolLoopMinSingleCallTimeout
	}
	if b.maxActDuration < toolLoopMinActDuration {
		b.maxActDuration = toolLoopMinActDuration
	}
	return b
}

func shouldPersistToolResultRecord(record toolExecutionRecord, index int, total int) bool {
	if total <= 0 || index < 0 || index >= total {
		return false
	}
	if record.Status == "error" || record.Status == "empty" {
		return true
	}
	return index == 0 || index == total-1
}

// sanitizeMessagesForToolCalling removes orphan tool-calling turns so provider-side
// validation won't fail when history was truncated in the middle of a tool chain.
func sanitizeMessagesForToolCalling(messages []providers.Message) []providers.Message {
	if len(messages) == 0 {
		return messages
	}

	out := make([]providers.Message, 0, len(messages))
	pendingToolIDs := map[string]struct{}{}
	lastToolCallIdx := -1

	resetPending := func() {
		pendingToolIDs = map[string]struct{}{}
		lastToolCallIdx = -1
	}

	rollbackToolCall := func() {
		if lastToolCallIdx >= 0 && lastToolCallIdx <= len(out) {
			// Drop the entire partial tool-call segment: assistant(tool_calls)
			// and any collected tool results that followed it.
			out = out[:lastToolCallIdx]
		}
		resetPending()
	}

	for _, msg := range messages {
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			continue
		}

		switch role {
		case "system":
			if len(out) == 0 {
				out = append(out, msg)
			}
		case "tool":
			if len(pendingToolIDs) == 0 || strings.TrimSpace(msg.ToolCallID) == "" {
				continue
			}
			if _, ok := pendingToolIDs[msg.ToolCallID]; !ok {
				continue
			}
			out = append(out, msg)
			delete(pendingToolIDs, msg.ToolCallID)
			if len(pendingToolIDs) == 0 {
				lastToolCallIdx = -1
			}
		case "assistant":
			if len(pendingToolIDs) > 0 {
				rollbackToolCall()
			}

			if len(msg.ToolCalls) == 0 {
				out = append(out, msg)
				continue
			}

			prevRole := ""
			for i := len(out) - 1; i >= 0; i-- {
				r := strings.TrimSpace(out[i].Role)
				if r != "" {
					prevRole = r
					break
				}
			}
			if prevRole != "user" && prevRole != "tool" {
				continue
			}

			out = append(out, msg)
			lastToolCallIdx = len(out) - 1
			pendingToolIDs = map[string]struct{}{}
			for _, tc := range msg.ToolCalls {
				id := strings.TrimSpace(tc.ID)
				if id != "" {
					pendingToolIDs[id] = struct{}{}
				}
			}
			if len(pendingToolIDs) == 0 {
				lastToolCallIdx = -1
			}
		default:
			if len(pendingToolIDs) > 0 {
				rollbackToolCall()
			}
			out = append(out, msg)
		}
	}

	if len(pendingToolIDs) > 0 {
		rollbackToolCall()
	}

	return out
}

func toolCallsSignature(calls []providers.ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	parts := make([]string, 0, len(calls))
	for _, tc := range calls {
		argsJSON, _ := json.Marshal(tc.Arguments)
		parts = append(parts, fmt.Sprintf("%s:%s", strings.TrimSpace(tc.Name), string(argsJSON)))
	}
	return strings.Join(parts, "|")
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

func sanitizeSensitiveToolArgs(args map[string]interface{}) map[string]interface{} {
	if len(args) == 0 {
		return map[string]interface{}{}
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return map[string]interface{}{"_masked": true}
	}
	redacted := redactSecretsInText(string(raw))
	out := map[string]interface{}{}
	if err := json.Unmarshal([]byte(redacted), &out); err != nil {
		return map[string]interface{}{"_masked": true}
	}
	return out
}

func redactSecretsInText(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	out := text
	authHeaderPattern := regexp.MustCompile(`(?i)(authorization\s*[:=]\s*["']?(?:bearer|token)\s+)([^\s"']+)`)
	out = authHeaderPattern.ReplaceAllString(out, "${1}[REDACTED]")

	keyValuePattern := regexp.MustCompile(`(?i)("?(?:token|api[_-]?key|password|passwd|secret|authorization|access[_-]?token|refresh[_-]?token)"?\s*:\s*")([^"]+)(")`)
	out = keyValuePattern.ReplaceAllString(out, "${1}[REDACTED]${3}")

	commonTokenPattern := regexp.MustCompile(`(?i)\b(ghp_[A-Za-z0-9_]+|glpat-[A-Za-z0-9_-]+)\b`)
	out = commonTokenPattern.ReplaceAllString(out, "[REDACTED]")

	return out
}

// GetStartupInfo returns information about loaded tools and skills for logging.
func (al *AgentLoop) GetStartupInfo() map[string]interface{} {
	info := make(map[string]interface{})

	// Tools info
	tools := al.tools.List()
	info["tools"] = map[string]interface{}{
		"count": len(tools),
		"names": tools,
	}

	// Skills info
	info["skills"] = al.contextBuilder.GetSkillsInfo()

	return info
}

func (al *AgentLoop) RunStartupSelfCheckAllSessions(ctx context.Context) StartupSelfCheckReport {
	report := StartupSelfCheckReport{}
	if al == nil || al.sessions == nil {
		return report
	}

	keys := al.sessions.ListSessionKeys()
	seen := make(map[string]struct{}, len(keys))
	sessions := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		sessions = append(sessions, key)
	}
	report.TotalSessions = len(sessions)

	// During startup, only run historical-session compaction checks to avoid extra self-check tasks.
	for _, sessionKey := range sessions {
		select {
		case <-ctx.Done():
			return report
		default:
		}

		before := al.sessions.MessageCount(sessionKey)
		if err := al.persistSessionWithCompaction(ctx, sessionKey); err != nil {
			logger.WarnCF("agent", "Startup compaction check failed", map[string]interface{}{
				"session_key":     sessionKey,
				logger.FieldError: err.Error(),
			})
		}
		after := al.sessions.MessageCount(sessionKey)
		if after < before {
			report.CompactedSessions++
		}
	}

	return report
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

func (al *AgentLoop) callLLMWithModelFallback(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	options map[string]interface{},
) (*providers.LLMResponse, error) {
	if len(al.providersByProxy) == 0 {
		candidates := al.modelCandidates()
		var lastErr error

		for idx, model := range candidates {
			response, err := al.provider.Chat(ctx, messages, tools, model, options)
			if err == nil {
				addTokenUsageToContext(ctx, response.Usage)
				if al.model != model {
					logger.WarnCF("agent", "Model switched after quota/rate-limit error", map[string]interface{}{
						"from_model": al.model,
						"to_model":   model,
					})
					al.model = model
				}
				return response, nil
			}

			lastErr = err
			if !shouldRetryWithFallbackModel(err) {
				return nil, err
			}

			if idx < len(candidates)-1 {
				logger.DebugCF("agent", "Model request failed, trying fallback model", map[string]interface{}{
					"failed_model":    model,
					"next_model":      candidates[idx+1],
					logger.FieldError: err.Error(),
				})
				continue
			}
		}

		return nil, fmt.Errorf("all configured models failed; last error: %w", lastErr)
	}

	proxyCandidates := al.proxyCandidates()
	var lastErr error

	for pidx, proxyName := range proxyCandidates {
		proxyProvider, ok := al.providersByProxy[proxyName]
		if !ok || proxyProvider == nil {
			continue
		}
		modelCandidates := al.modelCandidatesForProxy(proxyName)
		if len(modelCandidates) == 0 {
			continue
		}

		for midx, model := range modelCandidates {
			response, err := proxyProvider.Chat(ctx, messages, tools, model, options)
			if err == nil {
				addTokenUsageToContext(ctx, response.Usage)
				if al.proxy != proxyName {
					logger.WarnCF("agent", "Proxy switched after model unavailability", map[string]interface{}{
						"from_proxy": al.proxy,
						"to_proxy":   proxyName,
					})
					al.proxy = proxyName
					al.provider = proxyProvider
				}
				if al.model != model {
					logger.WarnCF("agent", "Model switched after availability error", map[string]interface{}{
						"from_model": al.model,
						"to_model":   model,
						"proxy":      proxyName,
					})
					al.model = model
				}
				return response, nil
			}

			lastErr = err
			if !shouldRetryWithFallbackModel(err) {
				return nil, err
			}

			if midx < len(modelCandidates)-1 {
				logger.DebugCF("agent", "Model request failed, trying next model in proxy", map[string]interface{}{
					"proxy":           proxyName,
					"failed_model":    model,
					"next_model":      modelCandidates[midx+1],
					logger.FieldError: err.Error(),
				})
				continue
			}

			if pidx < len(proxyCandidates)-1 {
				logger.DebugCF("agent", "All models failed in proxy, trying next proxy", map[string]interface{}{
					"failed_proxy":    proxyName,
					"next_proxy":      proxyCandidates[pidx+1],
					logger.FieldError: err.Error(),
				})
			}
		}
	}

	return nil, fmt.Errorf("all configured proxies/models failed; last error: %w", lastErr)
}

func (al *AgentLoop) modelCandidates() []string {
	candidates := []string{}
	seen := map[string]bool{}

	add := func(model string) {
		m := strings.TrimSpace(model)
		if m == "" || seen[m] {
			return
		}
		seen[m] = true
		candidates = append(candidates, m)
	}

	add(al.model)

	return candidates
}

func (al *AgentLoop) modelCandidatesForProxy(proxyName string) []string {
	candidates := []string{}
	seen := map[string]bool{}

	add := func(model string) {
		m := strings.TrimSpace(model)
		if m == "" || seen[m] {
			return
		}
		seen[m] = true
		candidates = append(candidates, m)
	}

	add(al.model)

	models := al.modelsByProxy[proxyName]
	for _, m := range models {
		add(m)
	}

	return candidates
}

func (al *AgentLoop) proxyCandidates() []string {
	candidates := []string{}
	seen := map[string]bool{}
	add := func(name string) {
		n := strings.TrimSpace(name)
		if n == "" || seen[n] {
			return
		}
		if _, ok := al.providersByProxy[n]; !ok {
			return
		}
		seen[n] = true
		candidates = append(candidates, n)
	}

	add(al.proxy)
	for _, n := range al.proxyFallbacks {
		add(n)
	}

	rest := make([]string, 0, len(al.providersByProxy))
	for name := range al.providersByProxy {
		if seen[name] {
			continue
		}
		rest = append(rest, name)
	}
	sort.Strings(rest)
	for _, name := range rest {
		add(name)
	}
	return candidates
}

func isQuotaOrRateLimitError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	keywords := []string{
		"status 429",
		"429",
		"insufficient_quota",
		"quota",
		"rate limit",
		"rate_limit",
		"too many requests",
		"billing",
	}

	for _, keyword := range keywords {
		if strings.Contains(msg, keyword) {
			return true
		}
	}
	return false
}

func isModelProviderSelectionError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	keywords := []string{
		"unknown provider",
		"unknown model",
		"unsupported model",
		"model not found",
		"no such model",
		"invalid model",
		"does not exist",
		"not available for model",
		"not allowed to use this model",
		"model is not available to your account",
		"access to this model is denied",
		"you do not have permission to use this model",
	}

	for _, keyword := range keywords {
		if strings.Contains(msg, keyword) {
			return true
		}
	}
	return false
}

func isForbiddenModelPermissionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "status 403") && !strings.Contains(msg, "403 forbidden") {
		return false
	}
	keywords := []string{
		"model",
		"permission",
		"forbidden",
		"access denied",
		"not allowed",
		"insufficient permissions",
	}
	for _, keyword := range keywords {
		if strings.Contains(msg, keyword) {
			return true
		}
	}
	return false
}

func shouldRetryWithFallbackModel(err error) bool {
	return isQuotaOrRateLimitError(err) || isModelProviderSelectionError(err) || isForbiddenModelPermissionError(err) || isGatewayTransientError(err) || isUpstreamAuthRoutingError(err) || isRequestTimeoutOrTransientNetworkError(err)
}

func isGatewayTransientError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	keywords := []string{
		"status 502",
		"status 503",
		"status 504",
		"status 524",
		"bad gateway",
		"service unavailable",
		"gateway timeout",
		"a timeout occurred",
		"error code: 524",
		"non-json response",
		"unexpected end of json input",
		"invalid character '<'",
	}

	for _, keyword := range keywords {
		if strings.Contains(msg, keyword) {
			return true
		}
	}
	return false
}

func isUpstreamAuthRoutingError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	keywords := []string{
		"auth_unavailable",
		"no auth available",
		"upstream auth unavailable",
	}

	for _, keyword := range keywords {
		if strings.Contains(msg, keyword) {
			return true
		}
	}
	return false
}

func isRequestTimeoutOrTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	keywords := []string{
		"context deadline exceeded",
		"deadline exceeded",
		"client.timeout exceeded",
		"i/o timeout",
		"connection reset by peer",
		"connection refused",
		"unexpected eof",
		"eof",
		"no such host",
		"network is unreachable",
		"temporary network error",
	}

	for _, keyword := range keywords {
		if strings.Contains(msg, keyword) {
			return true
		}
	}
	return false
}

func buildProviderToolDefs(toolDefs []map[string]interface{}) ([]providers.ToolDefinition, error) {
	providerToolDefs := make([]providers.ToolDefinition, 0, len(toolDefs))
	for i, td := range toolDefs {
		toolType, ok := td["type"].(string)
		if !ok || strings.TrimSpace(toolType) == "" {
			return nil, fmt.Errorf("tool[%d] missing/invalid type", i)
		}

		fnRaw, ok := td["function"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("tool[%d] missing/invalid function object", i)
		}

		name, ok := fnRaw["name"].(string)
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("tool[%d] missing/invalid function.name", i)
		}

		description, ok := fnRaw["description"].(string)
		if !ok {
			return nil, fmt.Errorf("tool[%d] missing/invalid function.description", i)
		}

		parameters, ok := fnRaw["parameters"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("tool[%d] missing/invalid function.parameters", i)
		}

		providerToolDefs = append(providerToolDefs, providers.ToolDefinition{
			Type: toolType,
			Function: providers.ToolFunctionDefinition{
				Name:        name,
				Description: description,
				Parameters:  parameters,
			},
		})
	}
	return providerToolDefs, nil
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

func (al *AgentLoop) persistSessionWithCompaction(ctx context.Context, sessionKey string) error {
	if err := al.maybeCompactContext(ctx, sessionKey); err != nil {
		logger.WarnCF("agent", "Context compaction skipped due to error", map[string]interface{}{
			"session_key":     sessionKey,
			logger.FieldError: err.Error(),
		})
	}
	return al.sessions.Save(al.sessions.GetOrCreate(sessionKey))
}

func (al *AgentLoop) maybeCompactContext(ctx context.Context, sessionKey string) error {
	cfg := al.compactionCfg
	if !cfg.Enabled {
		return nil
	}

	messageCount := al.sessions.MessageCount(sessionKey)
	history := al.sessions.GetHistory(sessionKey)
	summary := al.sessions.GetSummary(sessionKey)
	triggerByCount := messageCount >= cfg.TriggerMessages && len(history) >= cfg.TriggerMessages
	triggerBySize := shouldCompactBySize(summary, history, cfg.MaxTranscriptChars)
	if !triggerByCount && !triggerBySize {
		return nil
	}
	if cfg.KeepRecentMessages >= len(history) {
		return nil
	}

	compactUntil := len(history) - cfg.KeepRecentMessages
	compactCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	newSummary, err := al.buildCompactedSummary(compactCtx, summary, history[:compactUntil], cfg.MaxTranscriptChars, cfg.MaxSummaryChars, cfg.Mode)
	if err != nil {
		return err
	}
	newSummary = strings.TrimSpace(newSummary)
	if newSummary == "" {
		return nil
	}
	if len(newSummary) > cfg.MaxSummaryChars {
		newSummary = truncateString(newSummary, cfg.MaxSummaryChars)
	}

	before, after, err := al.sessions.CompactHistory(sessionKey, newSummary, cfg.KeepRecentMessages)
	if err != nil {
		return err
	}

	logger.InfoCF("agent", "Context compacted automatically", map[string]interface{}{
		"before":         before,
		"after":          after,
		"trigger_reason": compactionTriggerReason(triggerByCount, triggerBySize),
	})
	return nil
}

func (al *AgentLoop) buildCompactedSummary(
	ctx context.Context,
	existingSummary string,
	messages []providers.Message,
	maxTranscriptChars int,
	maxSummaryChars int,
	mode string,
) (string, error) {
	mode = normalizeCompactionMode(mode)
	transcript := formatCompactionTranscript(messages, maxTranscriptChars)
	if strings.TrimSpace(transcript) == "" {
		return strings.TrimSpace(existingSummary), nil
	}

	if mode == "responses_compact" || mode == "hybrid" {
		compactSummary, err := al.buildSummaryViaResponsesCompactWithFallback(ctx, existingSummary, messages, maxSummaryChars)
		if err == nil && strings.TrimSpace(compactSummary) != "" {
			if mode == "responses_compact" {
				return compactSummary, nil
			}
			existingSummary = strings.TrimSpace(existingSummary + "\n\n" + compactSummary)
		} else if mode == "responses_compact" {
			if err != nil {
				return "", err
			}
			return "", fmt.Errorf("responses_compact produced empty summary")
		} else if err != nil {
			logger.DebugCF("agent", "responses_compact failed in hybrid mode, fallback to summary mode", map[string]interface{}{
				logger.FieldError: err.Error(),
			})
		}
	}

	systemPrompt := al.withBootstrapPolicy(`You are a conversation compactor. Merge prior summary and transcript into a concise, factual memory for future turns. Keep user preferences, constraints, decisions, unresolved tasks, and key technical context. Do not include speculative content.`)
	userPrompt := fmt.Sprintf("Existing summary:\n%s\n\nTranscript to compact:\n%s\n\nReturn a compact markdown summary with sections: Key Facts, Decisions, Open Items, Next Steps.",
		strings.TrimSpace(existingSummary), transcript)

	resp, err := al.callLLMWithModelFallback(ctx, []providers.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}, nil, map[string]interface{}{
		"max_tokens":  1200,
		"temperature": 0.2,
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func (al *AgentLoop) buildSummaryViaResponsesCompactWithFallback(
	ctx context.Context,
	existingSummary string,
	messages []providers.Message,
	maxSummaryChars int,
) (string, error) {
	if len(al.providersByProxy) == 0 {
		compactor, ok := al.provider.(providers.ResponsesCompactor)
		if !ok || !compactor.SupportsResponsesCompact() {
			return "", fmt.Errorf("responses_compact mode requires provider support and protocol=responses")
		}

		modelCandidates := al.modelCandidates()
		var lastErr error
		for _, model := range modelCandidates {
			summary, err := al.tryResponsesCompactionOnCandidate(ctx, compactor, model, existingSummary, messages, maxSummaryChars)
			if err == nil {
				if al.model != model {
					logger.WarnCF("agent", "Compaction model switched after availability error", map[string]interface{}{
						"from_model": al.model,
						"to_model":   model,
					})
					al.model = model
				}
				return summary, nil
			}
			lastErr = err
			if !shouldRetryWithFallbackModel(err) {
				return "", err
			}
		}
		if lastErr != nil {
			return "", fmt.Errorf("all configured models failed for responses_compact; last error: %w", lastErr)
		}
		return "", fmt.Errorf("responses_compact produced empty summary")
	}

	proxyCandidates := al.proxyCandidates()
	var lastErr error
	foundCompactor := false
	for _, proxyName := range proxyCandidates {
		proxyProvider, ok := al.providersByProxy[proxyName]
		if !ok || proxyProvider == nil {
			continue
		}
		compactor, ok := proxyProvider.(providers.ResponsesCompactor)
		if !ok || !compactor.SupportsResponsesCompact() {
			continue
		}
		foundCompactor = true
		modelCandidates := al.modelCandidatesForProxy(proxyName)
		for _, model := range modelCandidates {
			summary, err := al.tryResponsesCompactionOnCandidate(ctx, compactor, model, existingSummary, messages, maxSummaryChars)
			if err == nil {
				if al.proxy != proxyName {
					logger.WarnCF("agent", "Compaction proxy switched after availability error", map[string]interface{}{
						"from_proxy": al.proxy,
						"to_proxy":   proxyName,
					})
					al.proxy = proxyName
					al.provider = proxyProvider
				}
				if al.model != model {
					logger.WarnCF("agent", "Compaction model switched after availability error", map[string]interface{}{
						"from_model": al.model,
						"to_model":   model,
						"proxy":      proxyName,
					})
					al.model = model
				}
				return summary, nil
			}
			lastErr = err
			if !shouldRetryWithFallbackModel(err) {
				return "", err
			}
		}
	}
	if !foundCompactor {
		return "", fmt.Errorf("responses_compact mode requires provider support and protocol=responses")
	}
	if lastErr != nil {
		return "", fmt.Errorf("all configured proxies/models failed for responses_compact; last error: %w", lastErr)
	}
	return "", fmt.Errorf("responses_compact produced empty summary")
}

func (al *AgentLoop) tryResponsesCompactionOnCandidate(
	ctx context.Context,
	compactor providers.ResponsesCompactor,
	model string,
	existingSummary string,
	messages []providers.Message,
	maxSummaryChars int,
) (string, error) {
	var lastErr error
	for attempt := 0; attempt <= compactionRetryPerCandidate; attempt++ {
		remaining := remainingTimeForCompaction(ctx)
		if remaining <= 0 {
			if lastErr != nil {
				return "", lastErr
			}
			return "", context.DeadlineExceeded
		}
		attemptCtx, cancel := context.WithTimeout(ctx, remaining)
		summary, err := compactor.BuildSummaryViaResponsesCompact(attemptCtx, model, existingSummary, messages, maxSummaryChars)
		cancel()
		if err == nil {
			if strings.TrimSpace(summary) == "" {
				return "", fmt.Errorf("responses_compact produced empty summary")
			}
			return summary, nil
		}
		lastErr = err
		if attempt < compactionRetryPerCandidate && shouldRetryWithFallbackModel(err) {
			continue
		}
		return "", err
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("responses_compact failed")
}

func remainingTimeForCompaction(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return compactionAttemptTimeout
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0
	}
	if remaining > compactionAttemptTimeout {
		return compactionAttemptTimeout
	}
	return remaining
}

func normalizeCompactionMode(raw string) string {
	switch strings.TrimSpace(raw) {
	case "", "summary":
		return "summary"
	case "responses_compact":
		return "responses_compact"
	case "hybrid":
		return "hybrid"
	default:
		return "summary"
	}
}

func formatCompactionTranscript(messages []providers.Message, maxChars int) string {
	if maxChars <= 0 || len(messages) == 0 {
		return ""
	}

	lines := make([]string, 0, len(messages))
	totalChars := 0
	for _, m := range messages {
		role := strings.TrimSpace(m.Role)
		if role == "" {
			role = "unknown"
		}
		line := fmt.Sprintf("[%s] %s\n", role, strings.TrimSpace(m.Content))
		maxLineLen := 1200
		if role == "tool" {
			maxLineLen = 420
		}
		if len(line) > maxLineLen {
			line = truncateString(line, maxLineLen-1) + "\n"
		}
		lines = append(lines, line)
		totalChars += len(line)
	}

	if totalChars <= maxChars {
		return strings.TrimSpace(strings.Join(lines, ""))
	}

	// Keep both early context and recent context when transcript is oversized.
	headBudget := maxChars / 3
	if headBudget < 256 {
		headBudget = maxChars / 2
	}
	tailBudget := maxChars - headBudget - 72
	if tailBudget < 128 {
		tailBudget = maxChars / 2
	}

	headEnd := 0
	usedHead := 0
	for i, line := range lines {
		if usedHead+len(line) > headBudget {
			break
		}
		usedHead += len(line)
		headEnd = i + 1
	}

	tailStart := len(lines)
	usedTail := 0
	for i := len(lines) - 1; i >= headEnd; i-- {
		line := lines[i]
		if usedTail+len(line) > tailBudget {
			break
		}
		usedTail += len(line)
		tailStart = i
	}

	var sb strings.Builder
	for i := 0; i < headEnd; i++ {
		sb.WriteString(lines[i])
	}
	omitted := tailStart - headEnd
	if omitted > 0 {
		sb.WriteString(fmt.Sprintf("...[%d messages omitted for compaction]...\n", omitted))
	}
	for i := tailStart; i < len(lines); i++ {
		sb.WriteString(lines[i])
	}

	out := strings.TrimSpace(sb.String())
	if len(out) > maxChars {
		out = truncateString(out, maxChars)
	}
	return out
}

func shouldCompactBySize(summary string, history []providers.Message, maxTranscriptChars int) bool {
	if maxTranscriptChars <= 0 || len(history) == 0 {
		return false
	}
	return estimateCompactionChars(summary, history) >= maxTranscriptChars
}

func estimateCompactionChars(summary string, history []providers.Message) int {
	total := len(strings.TrimSpace(summary))
	for _, msg := range history {
		total += len(strings.TrimSpace(msg.Role)) + len(strings.TrimSpace(msg.Content)) + 6
		if msg.ToolCallID != "" {
			total += len(msg.ToolCallID) + 8
		}
		for _, tc := range msg.ToolCalls {
			total += len(tc.ID) + len(tc.Type) + len(tc.Name)
			if tc.Function != nil {
				total += len(tc.Function.Name) + len(tc.Function.Arguments)
			}
		}
	}
	return total
}

func compactionTriggerReason(byCount, bySize bool) string {
	if byCount && bySize {
		return "count+size"
	}
	if byCount {
		return "count"
	}
	if bySize {
		return "size"
	}
	return "none"
}

func parseTaskExecutionDirectives(content string) taskExecutionDirectives {
	text := strings.TrimSpace(content)
	if text == "" {
		return taskExecutionDirectives{}
	}

	directive := taskExecutionDirectives{
		task: text,
	}

	fields := strings.Fields(text)
	if len(fields) > 0 {
		switch strings.ToLower(fields[0]) {
		case "/run", "@run":
			taskParts := make([]string, 0, len(fields)-1)
			for _, f := range fields[1:] {
				switch strings.ToLower(strings.TrimSpace(f)) {
				case "--stage-report":
					directive.stageReport = true
					continue
				case "--report=each-stage":
					directive.stageReport = true
					continue
				case "--report=off":
					directive.stageReport = false
					continue
				}
				taskParts = append(taskParts, f)
			}
			directive.task = strings.TrimSpace(strings.Join(taskParts, " "))
			return directive
		}
	}

	return directive
}

func isExplicitRunCommand(content string) bool {
	fields := strings.Fields(strings.TrimSpace(content))
	if len(fields) == 0 {
		return false
	}
	head := strings.ToLower(fields[0])
	return head == "/run" || head == "@run"
}

func (al *AgentLoop) detectAutonomyIntent(ctx context.Context, content string) (autonomyIntent, intentDetectionOutcome) {
	if intent, confidence, ok := al.inferAutonomyIntent(ctx, content); ok {
		al.controlMetricAdd(&al.controlStats.intentAutonomyMatched, 1)
		return intent, intentDetectionOutcome{matched: true, confidence: confidence}
	}
	al.controlMetricAdd(&al.controlStats.intentAutonomyRejected, 1)
	return autonomyIntent{}, intentDetectionOutcome{}
}

func (al *AgentLoop) detectAutoLearnIntent(ctx context.Context, content string) (autoLearnIntent, intentDetectionOutcome) {
	if intent, confidence, ok := al.inferAutoLearnIntent(ctx, content); ok {
		al.controlMetricAdd(&al.controlStats.intentAutoLearnMatched, 1)
		return intent, intentDetectionOutcome{matched: true, confidence: confidence}
	}
	al.controlMetricAdd(&al.controlStats.intentAutoLearnRejected, 1)
	return autoLearnIntent{}, intentDetectionOutcome{}
}

func (al *AgentLoop) inferAutonomyIntent(ctx context.Context, content string) (autonomyIntent, float64, bool) {
	text := strings.TrimSpace(content)
	if text == "" {
		return autonomyIntent{}, 0, false
	}

	limit := defaultControlPolicy().intentMaxInputChars
	if al != nil && al.controlPolicy.intentMaxInputChars > 0 {
		limit = al.controlPolicy.intentMaxInputChars
	}
	// Truncate long messages instead of skipping inference entirely.
	if len(text) > limit {
		text = truncate(text, limit)
	}

	systemPrompt := al.withBootstrapPolicy(`Classify autonomy control. JSON only:
{"matched":false,"enabled":null,"clear_focus":false,"idle_minutes":0,"focus":"","confidence":0.0}
matched=true only for autonomy control; confidence in [0,1].`)

	resp, err := al.callLLMWithModelFallback(ctx, []providers.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: text},
	}, nil, map[string]interface{}{
		"max_tokens":  220,
		"temperature": 0.0,
	})
	if err != nil || resp == nil {
		return autonomyIntent{}, 0, false
	}

	raw := extractJSONObject(resp.Content)
	if raw == "" {
		return autonomyIntent{}, 0, false
	}

	var parsed autonomyIntentLLMResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return autonomyIntent{}, 0, false
	}
	if !parsed.Matched {
		return autonomyIntent{}, 0, false
	}
	if parsed.Enabled == nil && !parsed.ClearFocus {
		return autonomyIntent{}, 0, false
	}
	intent := autonomyIntent{
		enabled:    parsed.Enabled,
		clearFocus: parsed.ClearFocus,
		focus:      strings.TrimSpace(parsed.Focus),
	}
	if parsed.IdleMinutes > 0 {
		d := time.Duration(parsed.IdleMinutes) * time.Minute
		intent.idleInterval = &d
	}
	return intent, parsed.Confidence, true
}

func extractJSONObject(text string) string {
	s := strings.TrimSpace(text)
	if s == "" {
		return ""
	}

	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}

	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return ""
	}
	return strings.TrimSpace(s[start : end+1])
}

func (al *AgentLoop) inferAutoLearnIntent(ctx context.Context, content string) (autoLearnIntent, float64, bool) {
	text := strings.TrimSpace(content)
	if text == "" {
		return autoLearnIntent{}, 0, false
	}
	limit := defaultControlPolicy().intentMaxInputChars
	if al != nil && al.controlPolicy.intentMaxInputChars > 0 {
		limit = al.controlPolicy.intentMaxInputChars
	}
	if len(text) > limit {
		text = truncate(text, limit)
	}

	systemPrompt := al.withBootstrapPolicy(`Classify auto-learning control. JSON only:
{"matched":false,"enabled":null,"interval_minutes":0,"confidence":0.0}
matched=true only for auto-learning control; confidence in [0,1].`)

	resp, err := al.callLLMWithModelFallback(ctx, []providers.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: text},
	}, nil, map[string]interface{}{
		"max_tokens":  180,
		"temperature": 0.0,
	})
	if err != nil || resp == nil {
		return autoLearnIntent{}, 0, false
	}

	raw := extractJSONObject(resp.Content)
	if raw == "" {
		return autoLearnIntent{}, 0, false
	}

	var parsed autoLearnIntentLLMResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return autoLearnIntent{}, 0, false
	}
	if !parsed.Matched || parsed.Enabled == nil {
		return autoLearnIntent{}, 0, false
	}

	intent := autoLearnIntent{enabled: parsed.Enabled}
	if parsed.IntervalMinutes > 0 {
		d := time.Duration(parsed.IntervalMinutes) * time.Minute
		intent.interval = &d
	}
	return intent, parsed.Confidence, true
}

func (al *AgentLoop) inferTaskExecutionDirectives(ctx context.Context, content string) (taskExecutionDirectives, bool) {
	text := strings.TrimSpace(content)
	if text == "" || len(text) > 1200 {
		return taskExecutionDirectives{}, false
	}

	systemPrompt := al.withBootstrapPolicy(`Extract execution directives. JSON only:
{"task":"","stage_report":false,"confidence":0.0}
task = actionable task text; stage_report=true only when user asks in-run progress; confidence in [0,1].`)

	resp, err := al.callLLMWithModelFallback(ctx, []providers.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: text},
	}, nil, map[string]interface{}{
		"max_tokens":  220,
		"temperature": 0.0,
	})
	if err != nil || resp == nil {
		return taskExecutionDirectives{}, false
	}

	raw := extractJSONObject(resp.Content)
	if raw == "" {
		return taskExecutionDirectives{}, false
	}

	var parsed taskExecutionDirectivesLLMResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return taskExecutionDirectives{}, false
	}
	if parsed.Confidence < 0.7 {
		return taskExecutionDirectives{}, false
	}

	task := strings.TrimSpace(parsed.Task)
	if task == "" {
		task = text
	}
	return taskExecutionDirectives{
		task:        task,
		stageReport: parsed.StageReport,
	}, true
}

func (al *AgentLoop) handleSlashCommand(ctx context.Context, msg bus.InboundMessage) (bool, string, error) {
	text := strings.TrimSpace(msg.Content)
	if !strings.HasPrefix(text, "/") {
		return false, "", nil
	}

	fields := strings.Fields(text)
	if len(fields) == 0 {
		return true, "", nil
	}

	switch fields[0] {
	case "/help":
		return true, "Slash commands:\n/help\n/status\n/status run [run_id|latest]\n/status wait <run_id|latest> [timeout_seconds]\n/run <task> [--stage-report]\n/config get <path>\n/config set <path> <value>\n/reload\n/pipeline list\n/pipeline status <pipeline_id>\n/pipeline ready <pipeline_id>", nil
	case "/stop":
		return true, "Stop command is handled by queue runtime. Send /stop from your channel session to interrupt current response.", nil
	case "/status":
		if len(fields) >= 2 {
			switch fields[1] {
			case "run":
				intent := runControlIntent{timeout: defaultRunWaitTimeout}
				if len(fields) >= 3 {
					target := strings.TrimSpace(fields[2])
					if strings.EqualFold(target, "latest") {
						intent.latest = true
					} else {
						intent.runID = target
					}
				} else {
					intent.latest = true
				}
				return true, al.executeRunControlIntent(ctx, msg.SessionKey, intent), nil
			case "wait":
				if len(fields) < 3 {
					return true, "Usage: /status wait <run_id|latest> [timeout_seconds]", nil
				}
				intent := runControlIntent{
					wait:    true,
					timeout: defaultRunWaitTimeout,
				}
				target := strings.TrimSpace(fields[2])
				if strings.EqualFold(target, "latest") {
					intent.latest = true
				} else {
					intent.runID = target
				}
				if len(fields) >= 4 {
					timeoutSec, parseErr := strconv.Atoi(strings.TrimSpace(fields[3]))
					if parseErr != nil || timeoutSec <= 0 {
						return true, "Usage: /status wait <run_id|latest> [timeout_seconds]", nil
					}
					intent.timeout = time.Duration(timeoutSec) * time.Second
					if intent.timeout > maxRunWaitTimeout {
						intent.timeout = maxRunWaitTimeout
					}
				}
				return true, al.executeRunControlIntent(ctx, msg.SessionKey, intent), nil
			}
		}

		return true, "Run status updated.", nil
	case "/reload":
		running, err := al.triggerGatewayReloadFromAgent()
		if err != nil {
			if running {
				return true, "", err
			}
			return true, "Hot reload did not take effect.", nil
		}
		return true, "Gateway hot-reload signal sent.", nil
	case "/config":
		if len(fields) < 2 {
			return true, "Usage: /config get <path> | /config set <path> <value>", nil
		}

		switch fields[1] {
		case "get":
			if len(fields) < 3 {
				return true, "Usage: /config get <path>", nil
			}
			cfgMap, err := al.loadConfigAsMapForAgent()
			if err != nil {
				return true, "", err
			}
			path := al.normalizeConfigPathForAgent(fields[2])
			value, ok := al.getMapValueByPathForAgent(cfgMap, path)
			if !ok {
				return true, "Config path not found.", nil
			}
			data, err := json.Marshal(value)
			if err != nil {
				return true, "Config value read.", nil
			}
			return true, string(data), nil
		case "set":
			if len(fields) < 4 {
				return true, "Usage: /config set <path> <value>", nil
			}
			cfgMap, err := al.loadConfigAsMapForAgent()
			if err != nil {
				return true, "", err
			}
			path := al.normalizeConfigPathForAgent(fields[2])
			value := al.parseConfigValueForAgent(strings.Join(fields[3:], " "))
			if err := al.setMapValueByPathForAgent(cfgMap, path, value); err != nil {
				return true, "", err
			}

			al.applyRuntimeModelConfig(path, value)

			data, err := json.MarshalIndent(cfgMap, "", "  ")
			if err != nil {
				return true, "", err
			}

			configPath := al.getConfigPathForCommands()
			backupPath, err := al.writeConfigAtomicWithBackupForAgent(configPath, data)
			if err != nil {
				return true, "", err
			}

			running, err := al.triggerGatewayReloadFromAgent()
			if err != nil {
				if running {
					if rbErr := al.rollbackConfigFromBackupForAgent(configPath, backupPath); rbErr != nil {
						return true, "", fmt.Errorf("hot reload failed and rollback failed: %w", rbErr)
					}
					return true, "", fmt.Errorf("hot reload failed, config rolled back: %w", err)
				}
				return true, "Config updated, but hot reload did not take effect.", nil
			}
			return true, "Config updated and hot reload triggered.", nil
		default:
			return true, "Usage: /config get <path> | /config set <path> <value>", nil
		}
	case "/pipeline":
		if al.orchestrator == nil {
			return true, "Pipeline orchestrator not enabled.", nil
		}
		if len(fields) < 2 {
			return true, "Usage: /pipeline list | /pipeline status <pipeline_id> | /pipeline ready <pipeline_id>", nil
		}
		switch fields[1] {
		case "list":
			items := al.orchestrator.ListPipelines()
			if len(items) == 0 {
				return true, "No pipelines found.", nil
			}
			entries := make([]map[string]interface{}, 0, len(items))
			for _, p := range items {
				entries = append(entries, map[string]interface{}{
					"id":     p.ID,
					"status": p.Status,
					"label":  p.Label,
				})
			}
			return true, "Pipeline list retrieved.", nil
		case "status":
			if len(fields) < 3 {
				return true, "Usage: /pipeline status <pipeline_id>", nil
			}
			s, err := al.orchestrator.SnapshotJSON(fields[2])
			return true, s, err
		case "ready":
			if len(fields) < 3 {
				return true, "Usage: /pipeline ready <pipeline_id>", nil
			}
			ready, err := al.orchestrator.ReadyTasks(fields[2])
			if err != nil {
				return true, "", err
			}
			if len(ready) == 0 {
				return true, "No ready tasks.", nil
			}
			tasks := make([]map[string]interface{}, 0, len(ready))
			for _, task := range ready {
				tasks = append(tasks, map[string]interface{}{
					"id":   task.ID,
					"role": task.Role,
					"goal": task.Goal,
				})
			}
			return true, "Ready task list retrieved.", nil
		default:
			return true, "Usage: /pipeline list | /pipeline status <pipeline_id> | /pipeline ready <pipeline_id>", nil
		}
	default:
		return false, "", nil
	}
}

func (al *AgentLoop) getConfigPathForCommands() string {
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

func (al *AgentLoop) normalizeConfigPathForAgent(path string) string {
	return configops.NormalizeConfigPath(path)
}

func (al *AgentLoop) parseConfigValueForAgent(raw string) interface{} {
	return configops.ParseConfigValue(raw)
}

func (al *AgentLoop) withBootstrapPolicy(taskPrompt string) string {
	taskPrompt = strings.TrimSpace(taskPrompt)
	bootstrapPolicy := strings.TrimSpace(al.loadBootstrapPolicyContext())
	if bootstrapPolicy == "" {
		return taskPrompt
	}
	return strings.TrimSpace(bootstrapPolicy + "\n\n" + taskPrompt)
}

func (al *AgentLoop) loadBootstrapPolicyContext() string {
	files := []string{"AGENTS.md", "SOUL.md", "USER.md"}
	sections := make([]string, 0, len(files))

	for _, filename := range files {
		filePath := filepath.Join(al.workspace, filename)
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}

		sections = append(sections, fmt.Sprintf("## %s\n%s", filename, content))
	}

	joined := strings.TrimSpace(strings.Join(sections, "\n\n"))
	if joined == "" {
		return ""
	}

	const maxPolicyChars = 6000
	if len(joined) > maxPolicyChars {
		return truncateString(joined, maxPolicyChars)
	}
	return joined
}

func (al *AgentLoop) loadConfigAsMapForAgent() (map[string]interface{}, error) {
	return configops.LoadConfigAsMap(al.getConfigPathForCommands())
}

func (al *AgentLoop) setMapValueByPathForAgent(root map[string]interface{}, path string, value interface{}) error {
	return configops.SetMapValueByPath(root, path, value)
}

func (al *AgentLoop) getMapValueByPathForAgent(root map[string]interface{}, path string) (interface{}, bool) {
	return configops.GetMapValueByPath(root, path)
}

func (al *AgentLoop) writeConfigAtomicWithBackupForAgent(configPath string, data []byte) (string, error) {
	return configops.WriteConfigAtomicWithBackup(configPath, data)
}

func (al *AgentLoop) rollbackConfigFromBackupForAgent(configPath, backupPath string) error {
	return configops.RollbackConfigFromBackup(configPath, backupPath)
}

func (al *AgentLoop) triggerGatewayReloadFromAgent() (bool, error) {
	return configops.TriggerGatewayReload(al.getConfigPathForCommands(), errGatewayNotRunningSlash)
}

func (al *AgentLoop) applyRuntimeModelConfig(path string, value interface{}) {
	switch path {
	case "agents.defaults.proxy":
		newProxy := strings.TrimSpace(fmt.Sprintf("%v", value))
		if newProxy != "" {
			al.proxy = newProxy
			if p, ok := al.providersByProxy[newProxy]; ok {
				al.provider = p
			}
			al.model = defaultModelFromModels(al.modelsByProxy[newProxy], al.provider)
		}
	case "agents.defaults.proxy_fallbacks":
		al.proxyFallbacks = parseStringList(value)
	}
}

func defaultModelFromModels(models []string, provider providers.LLMProvider) string {
	for _, m := range models {
		model := strings.TrimSpace(m)
		if model != "" {
			return model
		}
	}
	if provider != nil {
		return strings.TrimSpace(provider.GetDefaultModel())
	}
	return ""
}

func parseStringList(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.TrimSpace(item)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.TrimSpace(fmt.Sprintf("%v", item))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", value))
		if s == "" {
			return nil
		}
		return []string{s}
	}
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
