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

const perSessionQueueSize = 64
const autoLearnDefaultInterval = 10 * time.Minute
const autoLearnMinInterval = 30 * time.Second
const autonomyDefaultIdleInterval = 30 * time.Minute
const autonomyMinIdleInterval = 1 * time.Minute
const autonomyContinuousRunInterval = 20 * time.Second
const autonomyContinuousIdleThreshold = 20 * time.Second

type sessionWorker struct {
	queue    chan bus.InboundMessage
	cancelMu sync.Mutex
	cancel   context.CancelFunc
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
	intentHighConfidence          float64
	intentConfirmMinConfidence    float64
	intentMaxInputChars           int
	confirmTTL                    time.Duration
	confirmMaxClarificationTurns  int
	autonomyTickInterval          time.Duration
	autonomyMinRunInterval        time.Duration
	autonomyIdleThreshold         time.Duration
	autonomyMaxRoundsWithoutUser  int
	autonomyMaxPendingDuration    time.Duration
	autonomyMaxConsecutiveStalls  int
	autoLearnMaxRoundsWithoutUser int
}

type runtimeControlStats struct {
	intentAutonomyMatched       int64
	intentAutonomyNeedsConfirm  int64
	intentAutonomyRejected      int64
	intentAutoLearnMatched      int64
	intentAutoLearnNeedsConfirm int64
	intentAutoLearnRejected     int64
	confirmPrompts              int64
	confirmAccepted             int64
	confirmRejected             int64
	confirmExpired              int64
	autonomyRounds              int64
	autonomyStoppedByGuard      int64
	autoLearnRounds             int64
	autoLearnStoppedByGuard     int64
}

type AgentLoop struct {
	bus              *bus.MessageBus
	provider         providers.LLMProvider
	providersByProxy map[string]providers.LLMProvider
	modelsByProxy    map[string][]string
	proxy            string
	proxyFallbacks   []string
	workspace        string
	model            string
	maxIterations    int
	sessions         *session.SessionManager
	contextBuilder   *ContextBuilder
	tools            *tools.ToolRegistry
	orchestrator     *tools.Orchestrator
	running          atomic.Bool
	compactionCfg    config.ContextCompactionConfig
	llmCallTimeout   time.Duration
	workersMu        sync.Mutex
	workers          map[string]*sessionWorker
	autoLearnMu      sync.Mutex
	autoLearners     map[string]*autoLearner
	autonomyMu       sync.Mutex
	autonomyBySess   map[string]*autonomySession
	controlConfirmMu sync.Mutex
	controlConfirm   map[string]pendingControlConfirmation
	controlPolicy    controlPolicy
	controlStats     runtimeControlStats
}

type taskExecutionDirectives struct {
	task        string
	stageReport bool
}

type autoLearnIntent struct {
	action   string
	interval *time.Duration
}

type autonomyIntent struct {
	action       string
	idleInterval *time.Duration
	focus        string
}

type intentDetectionOutcome struct {
	matched      bool
	needsConfirm bool
	confidence   float64
}

type autonomyIntentLLMResponse struct {
	Action      string  `json:"action"`
	IdleMinutes int     `json:"idle_minutes"`
	Focus       string  `json:"focus"`
	Confidence  float64 `json:"confidence"`
}

type autoLearnIntentLLMResponse struct {
	Action          string  `json:"action"`
	IntervalMinutes int     `json:"interval_minutes"`
	Confidence      float64 `json:"confidence"`
}

type taskExecutionDirectivesLLMResponse struct {
	Task        string  `json:"task"`
	StageReport bool    `json:"stage_report"`
	Confidence  float64 `json:"confidence"`
}

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

type pendingControlConfirmation struct {
	intentType    string
	action        string
	idleInterval  *time.Duration
	focus         string
	interval      *time.Duration
	confidence    float64
	requestedAt   time.Time
	clarifyTurns  int
	originalInput string
}

func defaultControlPolicy() controlPolicy {
	return controlPolicy{
		intentHighConfidence:          0.75,
		intentConfirmMinConfidence:    0.45,
		intentMaxInputChars:           1200,
		confirmTTL:                    5 * time.Minute,
		confirmMaxClarificationTurns:  2,
		autonomyTickInterval:          autonomyContinuousRunInterval,
		autonomyMinRunInterval:        autonomyContinuousRunInterval,
		autonomyIdleThreshold:         autonomyContinuousIdleThreshold,
		autonomyMaxRoundsWithoutUser:  120,
		autonomyMaxPendingDuration:    3 * time.Minute,
		autonomyMaxConsecutiveStalls:  3,
		autoLearnMaxRoundsWithoutUser: 200,
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

func loadControlPolicyFromEnv(base controlPolicy) controlPolicy {
	p := base
	p.intentHighConfidence = envFloat64("CLAWGO_INTENT_HIGH_CONFIDENCE", p.intentHighConfidence)
	p.intentConfirmMinConfidence = envFloat64("CLAWGO_INTENT_CONFIRM_MIN_CONFIDENCE", p.intentConfirmMinConfidence)
	p.intentMaxInputChars = envInt("CLAWGO_INTENT_MAX_INPUT_CHARS", p.intentMaxInputChars)
	p.confirmTTL = envDuration("CLAWGO_CONFIRM_TTL", p.confirmTTL)
	p.confirmMaxClarificationTurns = envInt("CLAWGO_CONFIRM_MAX_CLARIFY_TURNS", p.confirmMaxClarificationTurns)
	p.autonomyTickInterval = envDuration("CLAWGO_AUTONOMY_TICK_INTERVAL", p.autonomyTickInterval)
	p.autonomyMinRunInterval = envDuration("CLAWGO_AUTONOMY_MIN_RUN_INTERVAL", p.autonomyMinRunInterval)
	p.autonomyIdleThreshold = envDuration("CLAWGO_AUTONOMY_IDLE_THRESHOLD", p.autonomyIdleThreshold)
	p.autonomyMaxRoundsWithoutUser = envInt("CLAWGO_AUTONOMY_MAX_ROUNDS_WITHOUT_USER", p.autonomyMaxRoundsWithoutUser)
	p.autonomyMaxPendingDuration = envDuration("CLAWGO_AUTONOMY_MAX_PENDING_DURATION", p.autonomyMaxPendingDuration)
	p.autonomyMaxConsecutiveStalls = envInt("CLAWGO_AUTONOMY_MAX_STALLS", p.autonomyMaxConsecutiveStalls)
	p.autoLearnMaxRoundsWithoutUser = envInt("CLAWGO_AUTOLEARN_MAX_ROUNDS_WITHOUT_USER", p.autoLearnMaxRoundsWithoutUser)

	if p.intentHighConfidence <= 0 || p.intentHighConfidence > 1 {
		p.intentHighConfidence = base.intentHighConfidence
	}
	if p.intentConfirmMinConfidence < 0 || p.intentConfirmMinConfidence >= p.intentHighConfidence {
		p.intentConfirmMinConfidence = base.intentConfirmMinConfidence
	}
	if p.intentMaxInputChars < 200 {
		p.intentMaxInputChars = base.intentMaxInputChars
	}
	if p.confirmTTL <= 0 {
		p.confirmTTL = base.confirmTTL
	}
	if p.confirmMaxClarificationTurns < 0 {
		p.confirmMaxClarificationTurns = base.confirmMaxClarificationTurns
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
	if p.autonomyMaxRoundsWithoutUser <= 0 {
		p.autonomyMaxRoundsWithoutUser = base.autonomyMaxRoundsWithoutUser
	}
	if p.autonomyMaxPendingDuration < 10*time.Second {
		p.autonomyMaxPendingDuration = base.autonomyMaxPendingDuration
	}
	if p.autonomyMaxConsecutiveStalls <= 0 {
		p.autonomyMaxConsecutiveStalls = base.autonomyMaxConsecutiveStalls
	}
	if p.autoLearnMaxRoundsWithoutUser <= 0 {
		p.autoLearnMaxRoundsWithoutUser = base.autoLearnMaxRoundsWithoutUser
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
	policy := loadControlPolicyFromEnv(defaultControlPolicy())

	loop := &AgentLoop{
		bus:              msgBus,
		provider:         provider,
		providersByProxy: providersByProxy,
		modelsByProxy:    modelsByProxy,
		proxy:            primaryProxy,
		proxyFallbacks:   parseStringList(cfg.Agents.Defaults.ProxyFallbacks),
		workspace:        workspace,
		model:            defaultModel,
		maxIterations:    cfg.Agents.Defaults.MaxToolIterations,
		sessions:         sessionsManager,
		contextBuilder:   NewContextBuilder(workspace, cfg.Memory, func() []string { return toolsRegistry.GetSummaries() }),
		tools:            toolsRegistry,
		orchestrator:     orchestrator,
		compactionCfg:    cfg.Agents.Defaults.ContextCompaction,
		llmCallTimeout:   time.Duration(cfg.Providers.Proxy.TimeoutSec) * time.Second,
		workers:          make(map[string]*sessionWorker),
		autoLearners:     make(map[string]*autoLearner),
		autonomyBySess:   make(map[string]*autonomySession),
		controlConfirm:   make(map[string]pendingControlConfirmation),
		controlPolicy:    policy,
	}
	logger.InfoCF("agent", "Control policy initialized", map[string]interface{}{
		"intent_high_confidence":            policy.intentHighConfidence,
		"intent_confirm_min_confidence":     policy.intentConfirmMinConfidence,
		"intent_max_input_chars":            policy.intentMaxInputChars,
		"confirm_ttl":                       policy.confirmTTL.String(),
		"confirm_max_clarification_turns":   policy.confirmMaxClarificationTurns,
		"autonomy_tick_interval":            policy.autonomyTickInterval.String(),
		"autonomy_min_run_interval":         policy.autonomyMinRunInterval.String(),
		"autonomy_idle_threshold":           policy.autonomyIdleThreshold.String(),
		"autonomy_max_rounds_without_user":  policy.autonomyMaxRoundsWithoutUser,
		"autonomy_max_pending_duration":     policy.autonomyMaxPendingDuration.String(),
		"autonomy_max_consecutive_stalls":   policy.autonomyMaxConsecutiveStalls,
		"autolearn_max_rounds_without_user": policy.autoLearnMaxRoundsWithoutUser,
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
	worker := al.getWorker(msg.SessionKey)
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
	worker := al.getOrCreateWorker(ctx, msg.SessionKey)
	select {
	case worker.queue <- msg:
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		al.bus.PublishOutbound(bus.OutboundMessage{
			Buttons: nil,
			Channel: msg.Channel,
			ChatID:  msg.ChatID,
			Content: al.localizeUserFacingText(ctx, msg.SessionKey, msg.Content, "The message queue is currently busy. Please try again shortly."),
		})
	}
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
		queue: make(chan bus.InboundMessage, perSessionQueueSize),
	}
	al.workers[sessionKey] = w

	go al.runSessionWorker(ctx, sessionKey, w)
	return w
}

func (al *AgentLoop) runSessionWorker(ctx context.Context, sessionKey string, worker *sessionWorker) {
	for {
		select {
		case <-ctx.Done():
			al.clearWorkerCancel(worker)
			al.removeWorker(sessionKey, worker)
			return
		case msg := <-worker.queue:
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
}

func (al *AgentLoop) clearWorkerCancel(worker *sessionWorker) {
	worker.cancelMu.Lock()
	worker.cancel = nil
	worker.cancelMu.Unlock()
}

func (al *AgentLoop) formatProcessingErrorMessage(ctx context.Context, msg bus.InboundMessage, err error) string {
	return al.localizeUserFacingText(ctx, msg.SessionKey, msg.Content, fmt.Sprintf("Error processing message: %v", err))
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
	for _, r := range text {
		if unicode.In(r, unicode.Han) {
			zhCount++
			continue
		}
		if r <= unicode.MaxASCII && unicode.IsLetter(r) {
			enCount++
		}
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

func (al *AgentLoop) startAutonomy(ctx context.Context, msg bus.InboundMessage, idleInterval time.Duration, focus string) string {
	if msg.Channel == "cli" {
		return al.naturalizeUserFacingText(ctx, "Autonomy mode requires gateway runtime mode (continuous message loop).")
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
	if s.focus != "" {
		return al.naturalizeUserFacingText(ctx, fmt.Sprintf("Autonomy mode is enabled. Current focus: %s. The system will continue in the background and report progress or results every %s.", s.focus, idleInterval.Truncate(time.Second)))
	}
	return al.naturalizeUserFacingText(ctx, fmt.Sprintf("Autonomy mode is enabled: automatic decomposition + background execution; reports progress or results every %s.", idleInterval.Truncate(time.Second)))
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

func (al *AgentLoop) autonomyStatus(ctx context.Context, sessionKey string) string {
	al.autonomyMu.Lock()
	defer al.autonomyMu.Unlock()

	s, ok := al.autonomyBySess[sessionKey]
	if !ok || s == nil {
		return al.naturalizeUserFacingText(ctx, "Autonomy mode is not enabled.")
	}

	uptime := time.Since(s.started).Truncate(time.Second)
	idle := time.Since(s.lastUserAt).Truncate(time.Second)
	focus := strings.TrimSpace(s.focus)
	if focus == "" {
		focus = "not set"
	}
	fallback := fmt.Sprintf("Autonomy mode is running: report interval %s, uptime %s, time since last user activity %s, automatic rounds %d.",
		s.idleInterval.Truncate(time.Second),
		uptime,
		idle,
		s.rounds,
	) + fmt.Sprintf(" Current focus: %s.", focus)
	return al.naturalizeUserFacingText(ctx, fallback)
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
				al.controlMetricAdd(&al.controlStats.autonomyStoppedByGuard, 1)
				al.bus.PublishOutbound(bus.OutboundMessage{
					Channel: msg.Channel,
					ChatID:  msg.ChatID,
					Content: al.naturalizeUserFacingText(context.Background(), "Autonomy mode stopped automatically because background rounds stalled repeatedly."),
				})
				return false
			}
		}
		al.autonomyMu.Unlock()
		return true
	}
	if policy.autonomyMaxRoundsWithoutUser > 0 &&
		s.rounds >= policy.autonomyMaxRoundsWithoutUser &&
		now.Sub(s.lastUserAt) >= policy.autonomyIdleThreshold {
		if s.cancel != nil {
			s.cancel()
		}
		delete(al.autonomyBySess, msg.SessionKey)
		al.autonomyMu.Unlock()
		al.controlMetricAdd(&al.controlStats.autonomyStoppedByGuard, 1)
		al.bus.PublishOutbound(bus.OutboundMessage{
			Channel: msg.Channel,
			ChatID:  msg.ChatID,
			Content: al.naturalizeUserFacingText(context.Background(), "Autonomy mode paused automatically after many unattended rounds. Send a new request to continue."),
		})
		return false
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

func (al *AgentLoop) startAutoLearner(ctx context.Context, msg bus.InboundMessage, interval time.Duration) string {
	if msg.Channel == "cli" {
		return al.naturalizeUserFacingText(ctx, "Auto-learn requires gateway runtime mode (continuous message loop).")
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

	return al.naturalizeUserFacingText(ctx, fmt.Sprintf("Auto-learn is enabled: one round every %s. Tell me in natural language whenever you want to stop it.", interval.Truncate(time.Second)))
}

func (al *AgentLoop) runAutoLearnerLoop(ctx context.Context, msg bus.InboundMessage) {
	runOnce := func() bool {
		round, ok := al.bumpAutoLearnRound(msg.SessionKey)
		if !ok {
			return false
		}
		al.controlMetricAdd(&al.controlStats.autoLearnRounds, 1)
		if al != nil && al.controlPolicy.autoLearnMaxRoundsWithoutUser > 0 && round > al.controlPolicy.autoLearnMaxRoundsWithoutUser {
			al.stopAutoLearner(msg.SessionKey)
			al.controlMetricAdd(&al.controlStats.autoLearnStoppedByGuard, 1)
			al.bus.PublishOutbound(bus.OutboundMessage{
				Channel: msg.Channel,
				ChatID:  msg.ChatID,
				Content: al.naturalizeUserFacingText(context.Background(), "Auto-learn stopped automatically after reaching the unattended round limit."),
			})
			return false
		}

		al.bus.PublishOutbound(bus.OutboundMessage{
			Channel: msg.Channel,
			ChatID:  msg.ChatID,
			Content: al.naturalizeUserFacingText(ctx, fmt.Sprintf("Auto-learn round %d started.", round)),
		})

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

func (al *AgentLoop) autoLearnerStatus(ctx context.Context, sessionKey string) string {
	al.autoLearnMu.Lock()
	defer al.autoLearnMu.Unlock()

	learner, ok := al.autoLearners[sessionKey]
	if !ok || learner == nil {
		return al.naturalizeUserFacingText(ctx, "Auto-learn is not enabled.")
	}

	uptime := time.Since(learner.started).Truncate(time.Second)
	fallback := fmt.Sprintf("Auto-learn is running: one round every %s, uptime %s, total rounds %d.",
		learner.interval.Truncate(time.Second),
		uptime,
		learner.rounds,
	)
	return al.naturalizeUserFacingText(ctx, fallback)
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
	al.controlConfirmMu.Lock()
	pendingConfirm := len(al.controlConfirm)
	al.controlConfirmMu.Unlock()

	stats := map[string]interface{}{
		"intent_autonomy_matched":          atomic.LoadInt64(&al.controlStats.intentAutonomyMatched),
		"intent_autonomy_needs_confirm":    atomic.LoadInt64(&al.controlStats.intentAutonomyNeedsConfirm),
		"intent_autonomy_rejected":         atomic.LoadInt64(&al.controlStats.intentAutonomyRejected),
		"intent_autolearn_matched":         atomic.LoadInt64(&al.controlStats.intentAutoLearnMatched),
		"intent_autolearn_needs_confirm":   atomic.LoadInt64(&al.controlStats.intentAutoLearnNeedsConfirm),
		"intent_autolearn_rejected":        atomic.LoadInt64(&al.controlStats.intentAutoLearnRejected),
		"confirm_prompts":                  atomic.LoadInt64(&al.controlStats.confirmPrompts),
		"confirm_accepted":                 atomic.LoadInt64(&al.controlStats.confirmAccepted),
		"confirm_rejected":                 atomic.LoadInt64(&al.controlStats.confirmRejected),
		"confirm_expired":                  atomic.LoadInt64(&al.controlStats.confirmExpired),
		"autonomy_rounds":                  atomic.LoadInt64(&al.controlStats.autonomyRounds),
		"autonomy_stopped_by_guard":        atomic.LoadInt64(&al.controlStats.autonomyStoppedByGuard),
		"autolearn_rounds":                 atomic.LoadInt64(&al.controlStats.autoLearnRounds),
		"autolearn_stopped_by_guard":       atomic.LoadInt64(&al.controlStats.autoLearnStoppedByGuard),
		"autonomy_active_sessions":         autonomyActive,
		"autolearn_active_sessions":        autoLearnActive,
		"pending_control_confirm_sessions": pendingConfirm,
	}
	logger.InfoCF("agent", "Control runtime snapshot", stats)
}

func (al *AgentLoop) executeAutonomyIntent(ctx context.Context, msg bus.InboundMessage, intent autonomyIntent) string {
	switch intent.action {
	case "start":
		idle := autonomyDefaultIdleInterval
		if intent.idleInterval != nil {
			idle = *intent.idleInterval
		}
		return al.startAutonomy(ctx, msg, idle, intent.focus)
	case "clear_focus":
		if al.clearAutonomyFocus(msg.SessionKey) {
			return al.naturalizeUserFacingText(ctx, "Confirmed: the current focus is complete. Subsequent autonomous rounds will shift to other high-value tasks.")
		}
		return al.naturalizeUserFacingText(ctx, "Autonomy mode is not running, so the focus cannot be cleared.")
	case "stop":
		if al.stopAutonomy(msg.SessionKey) {
			return al.naturalizeUserFacingText(ctx, "Autonomy mode stopped.")
		}
		return al.naturalizeUserFacingText(ctx, "Autonomy mode is not running.")
	case "status":
		return al.autonomyStatus(ctx, msg.SessionKey)
	default:
		return ""
	}
}

func (al *AgentLoop) executeAutoLearnIntent(ctx context.Context, msg bus.InboundMessage, intent autoLearnIntent) string {
	switch intent.action {
	case "start":
		interval := autoLearnDefaultInterval
		if intent.interval != nil {
			interval = *intent.interval
		}
		return al.startAutoLearner(ctx, msg, interval)
	case "stop":
		if al.stopAutoLearner(msg.SessionKey) {
			return al.naturalizeUserFacingText(ctx, "Auto-learn stopped.")
		}
		return al.naturalizeUserFacingText(ctx, "Auto-learn is not running.")
	case "status":
		return al.autoLearnerStatus(ctx, msg.SessionKey)
	default:
		return ""
	}
}

func (al *AgentLoop) handlePendingControlConfirmation(ctx context.Context, msg bus.InboundMessage) (bool, string) {
	pending, ok := al.getPendingControlConfirmation(msg.SessionKey)
	if !ok {
		return false, ""
	}
	policy := defaultControlPolicy()
	if al != nil {
		policy = al.controlPolicy
	}
	if time.Since(pending.requestedAt) > policy.confirmTTL {
		al.clearPendingControlConfirmation(msg.SessionKey)
		al.controlMetricAdd(&al.controlStats.confirmExpired, 1)
		return false, ""
	}

	decision, confident := al.classifyConfirmationReplyWithInference(ctx, msg.Content, pending)
	if !confident {
		if looksLikeNewTaskMessage(msg.Content) {
			al.clearPendingControlConfirmation(msg.SessionKey)
			return false, ""
		}
		pending.clarifyTurns++
		if pending.clarifyTurns > policy.confirmMaxClarificationTurns {
			al.clearPendingControlConfirmation(msg.SessionKey)
			return false, ""
		}
		al.setPendingControlConfirmation(msg.SessionKey, pending)
		al.controlMetricAdd(&al.controlStats.confirmPrompts, 1)
		return true, al.naturalizeUserFacingText(ctx, "I am checking a control action confirmation. Please reply with yes or no.")
	}

	al.clearPendingControlConfirmation(msg.SessionKey)
	if !decision {
		al.controlMetricAdd(&al.controlStats.confirmRejected, 1)
		return true, al.naturalizeUserFacingText(ctx, "Understood. I will not change autonomous control mode now.")
	}
	al.controlMetricAdd(&al.controlStats.confirmAccepted, 1)

	switch pending.intentType {
	case "autonomy":
		intent := autonomyIntent{
			action: pending.action,
			focus:  pending.focus,
		}
		if pending.idleInterval != nil {
			d := *pending.idleInterval
			intent.idleInterval = &d
		}
		return true, al.executeAutonomyIntent(ctx, msg, intent)
	case "autolearn":
		intent := autoLearnIntent{action: pending.action}
		if pending.interval != nil {
			d := *pending.interval
			intent.interval = &d
		}
		return true, al.executeAutoLearnIntent(ctx, msg, intent)
	default:
		return false, ""
	}
}

func looksLikeNewTaskMessage(content string) bool {
	text := strings.TrimSpace(content)
	if text == "" {
		return false
	}
	words := strings.Fields(text)
	return len(words) >= 6 || len(text) >= 28
}

func (al *AgentLoop) classifyConfirmationReplyWithInference(ctx context.Context, content string, pending pendingControlConfirmation) (decision bool, confident bool) {
	if decision, conf, ok := al.inferConfirmationDecision(ctx, content, pending); ok {
		if conf >= 0.7 {
			return decision, true
		}
	}
	return classifyConfirmationReplyLexical(content)
}

func classifyConfirmationReplyLexical(content string) (decision bool, confident bool) {
	normalized := strings.ToLower(strings.TrimSpace(content))
	if normalized == "" {
		return false, false
	}
	normalized = strings.NewReplacer("，", ",", "。", ".", "！", "!", "？", "?", "；", ";").Replace(normalized)
	normalized = strings.Trim(normalized, " \t\r\n.,!?;:~`'\"")

	yesSet := map[string]struct{}{
		"yes": {}, "y": {}, "ok": {}, "okay": {}, "sure": {}, "confirm": {}, "go ahead": {}, "do it": {},
		"是": {}, "好的": {}, "好": {}, "可以": {}, "行": {}, "确认": {}, "继续": {}, "开始吧": {},
	}
	noSet := map[string]struct{}{
		"no": {}, "n": {}, "cancel": {}, "stop": {}, "don't": {}, "do not": {},
		"不是": {}, "不用": {}, "不": {}, "先别": {}, "取消": {}, "不要": {},
	}
	if _, ok := yesSet[normalized]; ok {
		return true, true
	}
	if _, ok := noSet[normalized]; ok {
		return false, true
	}
	return false, false
}

type confirmationDecisionLLMResponse struct {
	Decision   string  `json:"decision"`
	Confidence float64 `json:"confidence"`
}

func (al *AgentLoop) inferConfirmationDecision(ctx context.Context, content string, pending pendingControlConfirmation) (decision bool, confidence float64, ok bool) {
	if al == nil || strings.TrimSpace(content) == "" {
		return false, 0, false
	}
	systemPrompt := al.withBootstrapPolicy(`Classify whether the user confirms a previously requested control action.
Return JSON only.
Schema:
{"decision":"yes|no|other","confidence":0.0}`)
	actionDesc := fmt.Sprintf("intent=%s action=%s", pending.intentType, pending.action)
	resp, err := al.callLLMWithModelFallback(ctx, []providers.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("Pending control: %s\nUser reply: %s", actionDesc, strings.TrimSpace(content))},
	}, nil, map[string]interface{}{
		"max_tokens":  90,
		"temperature": 0.0,
	})
	if err != nil || resp == nil {
		return false, 0, false
	}
	raw := extractJSONObject(resp.Content)
	if raw == "" {
		return false, 0, false
	}
	var parsed confirmationDecisionLLMResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return false, 0, false
	}
	switch strings.ToLower(strings.TrimSpace(parsed.Decision)) {
	case "yes":
		return true, parsed.Confidence, true
	case "no":
		return false, parsed.Confidence, true
	default:
		return false, parsed.Confidence, false
	}
}

func (al *AgentLoop) storePendingAutonomyConfirmation(sessionKey string, originalInput string, intent autonomyIntent, confidence float64) {
	if al == nil {
		return
	}
	pending := pendingControlConfirmation{
		intentType:    "autonomy",
		action:        intent.action,
		focus:         intent.focus,
		confidence:    confidence,
		requestedAt:   time.Now(),
		originalInput: strings.TrimSpace(originalInput),
	}
	if intent.idleInterval != nil {
		d := *intent.idleInterval
		pending.idleInterval = &d
	}
	al.controlConfirmMu.Lock()
	al.controlConfirm[sessionKey] = pending
	al.controlConfirmMu.Unlock()
	al.controlMetricAdd(&al.controlStats.confirmPrompts, 1)
}

func (al *AgentLoop) storePendingAutoLearnConfirmation(sessionKey string, originalInput string, intent autoLearnIntent, confidence float64) {
	if al == nil {
		return
	}
	pending := pendingControlConfirmation{
		intentType:    "autolearn",
		action:        intent.action,
		confidence:    confidence,
		requestedAt:   time.Now(),
		originalInput: strings.TrimSpace(originalInput),
	}
	if intent.interval != nil {
		d := *intent.interval
		pending.interval = &d
	}
	al.controlConfirmMu.Lock()
	al.controlConfirm[sessionKey] = pending
	al.controlConfirmMu.Unlock()
	al.controlMetricAdd(&al.controlStats.confirmPrompts, 1)
}

func (al *AgentLoop) setPendingControlConfirmation(sessionKey string, pending pendingControlConfirmation) {
	if al == nil {
		return
	}
	al.controlConfirmMu.Lock()
	al.controlConfirm[sessionKey] = pending
	al.controlConfirmMu.Unlock()
}

func (al *AgentLoop) clearPendingControlConfirmation(sessionKey string) {
	if al == nil {
		return
	}
	al.controlConfirmMu.Lock()
	delete(al.controlConfirm, sessionKey)
	al.controlConfirmMu.Unlock()
}

func (al *AgentLoop) getPendingControlConfirmation(sessionKey string) (pendingControlConfirmation, bool) {
	if al == nil {
		return pendingControlConfirmation{}, false
	}
	al.controlConfirmMu.Lock()
	defer al.controlConfirmMu.Unlock()
	pending, ok := al.controlConfirm[sessionKey]
	return pending, ok
}

func (al *AgentLoop) formatAutonomyConfirmationPrompt(intent autonomyIntent) string {
	switch intent.action {
	case "start":
		idleText := autonomyDefaultIdleInterval.Truncate(time.Second).String()
		if intent.idleInterval != nil {
			idleText = intent.idleInterval.Truncate(time.Second).String()
		}
		if strings.TrimSpace(intent.focus) != "" {
			return fmt.Sprintf("I inferred that you want to enable autonomy mode (idle interval %s, focus: %s). Reply \"yes\" to confirm or \"no\" to cancel.", idleText, strings.TrimSpace(intent.focus))
		}
		return fmt.Sprintf("I inferred that you want to enable autonomy mode (idle interval %s). Reply \"yes\" to confirm or \"no\" to cancel.", idleText)
	case "stop":
		return "I inferred that you want to stop autonomy mode. Reply \"yes\" to confirm or \"no\" to cancel."
	case "status":
		return "I inferred that you want autonomy status. Reply \"yes\" to confirm or \"no\" to cancel."
	case "clear_focus":
		return "I inferred that you want to clear the current autonomy focus. Reply \"yes\" to confirm or \"no\" to cancel."
	default:
		return "I inferred an autonomy control operation. Reply \"yes\" to confirm or \"no\" to cancel."
	}
}

func (al *AgentLoop) formatAutoLearnConfirmationPrompt(intent autoLearnIntent) string {
	switch intent.action {
	case "start":
		intervalText := autoLearnDefaultInterval.Truncate(time.Second).String()
		if intent.interval != nil {
			intervalText = intent.interval.Truncate(time.Second).String()
		}
		return fmt.Sprintf("I inferred that you want to start auto-learn (interval %s). Reply \"yes\" to confirm or \"no\" to cancel.", intervalText)
	case "stop":
		return "I inferred that you want to stop auto-learn. Reply \"yes\" to confirm or \"no\" to cancel."
	case "status":
		return "I inferred that you want auto-learn status. Reply \"yes\" to confirm or \"no\" to cancel."
	default:
		return "I inferred an auto-learn control operation. Reply \"yes\" to confirm or \"no\" to cancel."
	}
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
	input := 0
	output := 0
	total := 0
	if totals != nil {
		input = totals.input
		output = totals.output
		total = totals.total
	}
	return fmt.Sprintf("\n\nUsage: in %d, out %d, total %d",
		input, output, total)
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

func (al *AgentLoop) localizeUserFacingText(ctx context.Context, sessionKey, currentContent, fallback string) string {
	return al.naturalizeUserFacingText(withUserLanguageHint(ctx, sessionKey, currentContent), fallback)
}

func (al *AgentLoop) naturalizeUserFacingText(ctx context.Context, fallback string) string {
	text := strings.TrimSpace(fallback)
	if text == "" || ctx == nil {
		return fallback
	}
	if al == nil || (al.provider == nil && len(al.providersByProxy) == 0) {
		return fallback
	}

	targetLanguage := "English"
	if hint, ok := ctx.Value(userLanguageHintKey{}).(userLanguageHint); ok {
		if al.preferChineseUserFacingText(hint.sessionKey, hint.content) {
			targetLanguage = "Simplified Chinese"
		}
	}
	llmCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	systemPrompt := al.withBootstrapPolicy(fmt.Sprintf(`You rewrite assistant control/status replies in natural conversational %s.
Rules:
- Keep factual meaning unchanged.
- Use concise natural wording, no rigid templates.
- No markdown, no code block, no extra explanation.
- Return plain text only.`, targetLanguage))

	resp, err := al.callLLMWithModelFallback(llmCtx, []providers.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: text},
	}, nil, map[string]interface{}{
		"max_tokens":  180,
		"temperature": 0.4,
	})
	if err != nil || resp == nil {
		return fallback
	}

	out := strings.TrimSpace(resp.Content)
	if out == "" {
		return fallback
	}
	return out
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

func (al *AgentLoop) processMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	ctx = withUserLanguageHint(ctx, msg.SessionKey, msg.Content)

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
		defer al.finishAutonomyRound(msg.SessionKey)
	}

	controlEligible := shouldHandleControlIntents(msg)

	// Route system messages to processSystemMessage
	if msg.Channel == "system" {
		return al.processSystemMessage(ctx, msg)
	}

	// Built-in slash commands (deterministic, no LLM roundtrip)
	if controlEligible {
		if handled, result, err := al.handleSlashCommand(ctx, msg); handled {
			return result, err
		}
	}

	if controlEligible {
		al.noteAutonomyUserActivity(msg)
	}

	if controlEligible {
		if handled, response := al.handlePendingControlConfirmation(ctx, msg); handled {
			return response, nil
		}
	}

	if controlEligible {
		if intent, outcome := al.detectAutonomyIntent(ctx, msg.Content); outcome.matched {
			al.clearPendingControlConfirmation(msg.SessionKey)
			return al.executeAutonomyIntent(ctx, msg, intent), nil
		} else if outcome.needsConfirm {
			al.storePendingAutonomyConfirmation(msg.SessionKey, msg.Content, intent, outcome.confidence)
			return al.naturalizeUserFacingText(ctx, al.formatAutonomyConfirmationPrompt(intent)), nil
		}

		if intent, outcome := al.detectAutoLearnIntent(ctx, msg.Content); outcome.matched {
			al.clearPendingControlConfirmation(msg.SessionKey)
			return al.executeAutoLearnIntent(ctx, msg, intent), nil
		} else if outcome.needsConfirm {
			al.storePendingAutoLearnConfirmation(msg.SessionKey, msg.Content, intent, outcome.confidence)
			return al.naturalizeUserFacingText(ctx, al.formatAutoLearnConfirmationPrompt(intent)), nil
		}
	}

	directives := parseTaskExecutionDirectives(msg.Content)
	if controlEligible {
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
	if al.isAutonomyEnabled(msg.SessionKey) && controlEligible {
		userPrompt = buildAutonomyTaskPrompt(userPrompt)
	}

	var progress *stageReporter
	if directives.stageReport {
		progress = &stageReporter{
			localize: func(content string) string {
				return al.localizeUserFacingText(ctx, msg.SessionKey, msg.Content, content)
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
		nil,
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
		progress.Publish(5, 5, "done", fmt.Sprintf("Completed after %d iterations.", iteration))
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

	iteration := 0
	var finalContent string
	var lastToolResult string

	for iteration < al.maxIterations {
		iteration++
		if progress != nil {
			progress.Publish(3, 5, "execution", fmt.Sprintf("Running iteration %d.", iteration))
		}

		if !systemMode {
			logger.DebugCF("agent", "LLM iteration",
				map[string]interface{}{
					"iteration": iteration,
					"max":       al.maxIterations,
				})
		}

		providerToolDefs, err := buildProviderToolDefs(al.tools.GetDefinitions())
		if err != nil {
			return "", iteration, fmt.Errorf("invalid tool definition: %w", err)
		}

		messages = sanitizeMessagesForToolCalling(messages)

		systemPromptLen := 0
		if len(messages) > 0 {
			systemPromptLen = len(messages[0].Content)
		}
		logger.DebugCF("agent", "LLM request",
			map[string]interface{}{
				"iteration":         iteration,
				"model":             al.model,
				"messages_count":    len(messages),
				"tools_count":       len(providerToolDefs),
				"max_tokens":        8192,
				"temperature":       0.7,
				"system_prompt_len": systemPromptLen,
			})
		logger.DebugCF("agent", "Full LLM request",
			map[string]interface{}{
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
			logger.ErrorCF("agent", errLog,
				map[string]interface{}{
					"iteration":       iteration,
					logger.FieldError: err.Error(),
					"elapsed":         llmElapsed.String(),
				})
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
			finalContent = response.Content
			if !systemMode {
				logger.InfoCF("agent", "LLM response without tool calls (direct answer)",
					map[string]interface{}{
						"iteration":                        iteration,
						logger.FieldAssistantContentLength: len(finalContent),
					})
			}
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

		assistantMsg := providers.Message{
			Role:    "assistant",
			Content: response.Content,
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
		messages = append(messages, assistantMsg)
		al.sessions.AddMessageFull(sessionKey, assistantMsg)

		for _, tc := range response.ToolCalls {
			if !systemMode {
				argsJSON, _ := json.Marshal(tc.Arguments)
				argsPreview := truncate(string(argsJSON), 200)
				logger.InfoCF("agent", fmt.Sprintf("Tool call: %s(%s)", tc.Name, argsPreview),
					map[string]interface{}{
						"tool":      tc.Name,
						"iteration": iteration,
					})
			}

			result, err := al.tools.Execute(ctx, tc.Name, tc.Arguments)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}
			if progress != nil {
				if err != nil {
					progress.Publish(3, 5, "execution", fmt.Sprintf("Tool %s failed: %v", tc.Name, err))
				} else {
					progress.Publish(3, 5, "execution", fmt.Sprintf("Tool %s completed.", tc.Name))
				}
			}
			lastToolResult = result

			toolResultMsg := providers.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			}
			messages = append(messages, toolResultMsg)
			al.sessions.AddMessageFull(sessionKey, toolResultMsg)
		}
	}

	// When max iterations are reached without a direct answer, ask once more without tools.
	// This avoids returning placeholder text to end users.
	if finalContent == "" && len(messages) > 0 {
		if !systemMode && iteration >= al.maxIterations {
			logger.WarnCF("agent", "Max tool iterations reached without final answer; forcing finalization pass", map[string]interface{}{
				"iteration":   iteration,
				"max":         al.maxIterations,
				"session_key": sessionKey,
			})
		}

		finalizeMessages := append([]providers.Message{}, messages...)
		finalizeMessages = append(finalizeMessages, providers.Message{
			Role:    "user",
			Content: "Now provide your final response to the user based on the completed tool results. Do not call any tools.",
		})
		finalizeMessages = sanitizeMessagesForToolCalling(finalizeMessages)

		llmCtx, cancelLLM := context.WithTimeout(ctx, al.llmCallTimeout)
		finalResp, err := al.callLLMWithModelFallback(llmCtx, finalizeMessages, nil, map[string]interface{}{
			"max_tokens":  1024,
			"temperature": 0.3,
		})
		cancelLLM()
		if err != nil {
			logger.WarnCF("agent", "Finalization pass failed", map[string]interface{}{
				"iteration":       iteration,
				"session_key":     sessionKey,
				logger.FieldError: err.Error(),
			})
		} else if strings.TrimSpace(finalResp.Content) != "" {
			finalContent = finalResp.Content
		}
	}

	if finalContent == "" {
		finalContent = strings.TrimSpace(lastToolResult)
	}

	return finalContent, iteration, nil
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
	return isQuotaOrRateLimitError(err) || isModelProviderSelectionError(err) || isForbiddenModelPermissionError(err) || isGatewayTransientError(err) || isUpstreamAuthRoutingError(err)
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
		if compactor, ok := al.provider.(providers.ResponsesCompactor); ok && compactor.SupportsResponsesCompact() {
			compactSummary, err := compactor.BuildSummaryViaResponsesCompact(ctx, al.model, existingSummary, messages, maxSummaryChars)
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
			}
		} else if mode == "responses_compact" {
			return "", fmt.Errorf("responses_compact mode requires provider support and protocol=responses")
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
	policy := defaultControlPolicy()
	if al != nil {
		policy = al.controlPolicy
	}
	if intent, confidence, ok := al.inferAutonomyIntent(ctx, content); ok {
		if confidence >= policy.intentHighConfidence {
			al.controlMetricAdd(&al.controlStats.intentAutonomyMatched, 1)
			return intent, intentDetectionOutcome{matched: true, confidence: confidence}
		}
		if confidence >= policy.intentConfirmMinConfidence {
			al.controlMetricAdd(&al.controlStats.intentAutonomyNeedsConfirm, 1)
			return intent, intentDetectionOutcome{needsConfirm: true, confidence: confidence}
		}
		al.controlMetricAdd(&al.controlStats.intentAutonomyRejected, 1)
		return autonomyIntent{}, intentDetectionOutcome{}
	}
	return autonomyIntent{}, intentDetectionOutcome{}
}

func (al *AgentLoop) detectAutoLearnIntent(ctx context.Context, content string) (autoLearnIntent, intentDetectionOutcome) {
	policy := defaultControlPolicy()
	if al != nil {
		policy = al.controlPolicy
	}
	if intent, confidence, ok := al.inferAutoLearnIntent(ctx, content); ok {
		if confidence >= policy.intentHighConfidence {
			al.controlMetricAdd(&al.controlStats.intentAutoLearnMatched, 1)
			return intent, intentDetectionOutcome{matched: true, confidence: confidence}
		}
		if confidence >= policy.intentConfirmMinConfidence {
			al.controlMetricAdd(&al.controlStats.intentAutoLearnNeedsConfirm, 1)
			return intent, intentDetectionOutcome{needsConfirm: true, confidence: confidence}
		}
		al.controlMetricAdd(&al.controlStats.intentAutoLearnRejected, 1)
		return autoLearnIntent{}, intentDetectionOutcome{}
	}
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

	systemPrompt := al.withBootstrapPolicy(`You classify autonomy-control intent for an AI assistant.
Return JSON only, no markdown.
Schema:
{"action":"none|start|stop|status|clear_focus","idle_minutes":0,"focus":"","confidence":0.0}
Rules:
- "start": user asks assistant to enter autonomous/self-driven mode.
- "stop": user asks assistant to disable autonomous mode.
- "status": user asks autonomy mode status.
- "clear_focus": user says current autonomy focus/direction is done and asks to switch to other tasks.
- "none": anything else.
- confidence: 0..1`)

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

	action := strings.ToLower(strings.TrimSpace(parsed.Action))
	switch action {
	case "start", "stop", "status", "clear_focus":
	default:
		return autonomyIntent{}, 0, false
	}

	intent := autonomyIntent{
		action: action,
		focus:  strings.TrimSpace(parsed.Focus),
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

	systemPrompt := al.withBootstrapPolicy(`You classify auto-learning-control intent for an AI assistant.
Return JSON only.
Schema:
{"action":"none|start|stop|status","interval_minutes":0,"confidence":0.0}
Rules:
- "start": user asks assistant to start autonomous learning loop.
- "stop": user asks assistant to stop autonomous learning loop.
- "status": user asks learning loop status.
- "none": anything else.
- confidence: 0..1`)

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

	action := strings.ToLower(strings.TrimSpace(parsed.Action))
	switch action {
	case "start", "stop", "status":
	default:
		return autoLearnIntent{}, 0, false
	}

	intent := autoLearnIntent{action: action}
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

	systemPrompt := al.withBootstrapPolicy(`Extract execution directives from user message.
Return JSON only.
Schema:
{"task":"","stage_report":false,"confidence":0.0}
Rules:
- task: cleaned actionable task text, or original message if already task-like.
- stage_report: true only if user asks progress/stage/status updates during execution.
- confidence: 0..1`)

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
		return true, "Slash commands:\n/help\n/status\n/run <task> [--stage-report]\n/config get <path>\n/config set <path> <value>\n/reload\n/pipeline list\n/pipeline status <pipeline_id>\n/pipeline ready <pipeline_id>", nil
	case "/stop":
		return true, "Stop command is handled by queue runtime. Send /stop from your channel session to interrupt current response.", nil
	case "/status":
		cfg, err := config.LoadConfig(al.getConfigPathForCommands())
		if err != nil {
			return true, "", fmt.Errorf("status failed: %w", err)
		}
		activeProxy := strings.TrimSpace(al.proxy)
		if activeProxy == "" {
			activeProxy = "proxy"
		}
		activeBase := cfg.Providers.Proxy.APIBase
		if activeProxy != "proxy" {
			if p, ok := cfg.Providers.Proxies[activeProxy]; ok {
				activeBase = p.APIBase
			}
		}
		return true, fmt.Sprintf("Model: %s\nProxy: %s\nAPI Base: %s\nResponses Compact: %v\nLogging: %v\nConfig: %s",
			al.model,
			activeProxy,
			activeBase,
			providers.ProviderSupportsResponsesCompact(cfg, activeProxy),
			cfg.Logging.Enabled,
			al.getConfigPathForCommands(),
		), nil
	case "/reload":
		running, err := al.triggerGatewayReloadFromAgent()
		if err != nil {
			if running {
				return true, "", err
			}
			return true, fmt.Sprintf("Hot reload not applied: %v", err), nil
		}
		return true, "Gateway hot reload signal sent", nil
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
				return true, fmt.Sprintf("Path not found: %s", path), nil
			}
			data, err := json.Marshal(value)
			if err != nil {
				return true, fmt.Sprintf("%v", value), nil
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
				return true, fmt.Sprintf("Updated %s = %v\nHot reload not applied: %v", path, value, err), nil
			}
			return true, fmt.Sprintf("Updated %s = %v\nGateway hot reload signal sent", path, value), nil
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
			var sb strings.Builder
			sb.WriteString("Pipelines:\n")
			for _, p := range items {
				sb.WriteString(fmt.Sprintf("- %s [%s] %s\n", p.ID, p.Status, p.Label))
			}
			return true, sb.String(), nil
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
			var sb strings.Builder
			sb.WriteString("Ready tasks:\n")
			for _, task := range ready {
				sb.WriteString(fmt.Sprintf("- %s (%s) %s\n", task.ID, task.Role, task.Goal))
			}
			return true, sb.String(), nil
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

	return fmt.Sprintf(`Follow the workspace bootstrap policy while interpreting user language and intent.
Prioritize semantic understanding over fixed command words.

%s

%s`, bootstrapPolicy, taskPrompt)
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
