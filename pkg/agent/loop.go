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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
	pending      bool
	focus        string
}

type AgentLoop struct {
	bus            *bus.MessageBus
	provider       providers.LLMProvider
	workspace      string
	model          string
	modelFallbacks []string
	maxIterations  int
	sessions       *session.SessionManager
	contextBuilder *ContextBuilder
	tools          *tools.ToolRegistry
	orchestrator   *tools.Orchestrator
	running        atomic.Bool
	compactionCfg  config.ContextCompactionConfig
	llmCallTimeout time.Duration
	workersMu      sync.Mutex
	workers        map[string]*sessionWorker
	autoLearnMu    sync.Mutex
	autoLearners   map[string]*autoLearner
	autonomyMu     sync.Mutex
	autonomyBySess map[string]*autonomySession
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
}

func (sr *stageReporter) Publish(stage int, total int, status string, detail string) {
	if sr == nil || sr.onUpdate == nil {
		return
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		detail = "-"
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = "进度更新"
	}
	sr.onUpdate(fmt.Sprintf("[进度] %s：%s", status, detail))
}

func NewAgentLoop(cfg *config.Config, msgBus *bus.MessageBus, provider providers.LLMProvider, cs *cron.CronService) *AgentLoop {
	workspace := cfg.WorkspacePath()
	os.MkdirAll(workspace, 0755)
	logger.InfoCF("agent", "Agent workspace initialized", map[string]interface{}{
		"workspace": workspace,
	})

	toolsRegistry := tools.NewToolRegistry()
	toolsRegistry.Register(tools.NewReadFileTool(""))
	toolsRegistry.Register(tools.NewWriteFileTool(""))
	toolsRegistry.Register(tools.NewListDirTool(""))
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
	editFileTool := tools.NewEditFileTool("")
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

	loop := &AgentLoop{
		bus:            msgBus,
		provider:       provider,
		workspace:      workspace,
		model:          cfg.Agents.Defaults.Model,
		modelFallbacks: cfg.Agents.Defaults.ModelFallbacks,
		maxIterations:  cfg.Agents.Defaults.MaxToolIterations,
		sessions:       sessionsManager,
		contextBuilder: NewContextBuilder(workspace, cfg.Memory, func() []string { return toolsRegistry.GetSummaries() }),
		tools:          toolsRegistry,
		orchestrator:   orchestrator,
		compactionCfg:  cfg.Agents.Defaults.ContextCompaction,
		llmCallTimeout: time.Duration(cfg.Providers.Proxy.TimeoutSec) * time.Second,
		workers:        make(map[string]*sessionWorker),
		autoLearners:   make(map[string]*autoLearner),
		autonomyBySess: make(map[string]*autonomySession),
	}

	// 注入递归运行逻辑，使 subagent 具备 full tool-calling 能力
	subagentManager.SetRunFunc(func(ctx context.Context, task, channel, chatID string) (string, error) {
		sessionKey := fmt.Sprintf("subagent:%d", os.Getpid()) // 改用 PID 或随机数，避免 sessionKey 冲突
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
			Content: "Message queue is busy. Please try again shortly.",
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
					response = fmt.Sprintf("Error processing message: %v", err)
				}

				if response != "" {
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

func (al *AgentLoop) startAutonomy(msg bus.InboundMessage, idleInterval time.Duration, focus string) string {
	if msg.Channel == "cli" {
		return "自主模式需要在 gateway 运行模式下使用（持续消息循环）。"
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

	sessionCtx, cancel := context.WithCancel(context.Background())
	s := &autonomySession{
		cancel:       cancel,
		started:      time.Now(),
		idleInterval: idleInterval,
		lastUserAt:   time.Now(),
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
		return fmt.Sprintf("自主模式已开启，当前研究方向：%s。空闲超过 %s 会继续按该方向主动推进并汇报。", s.focus, idleInterval.Truncate(time.Second))
	}
	return fmt.Sprintf("自主模式已开启：自动拆解执行 + 阶段回报；空闲超过 %s 会主动推进并汇报。", idleInterval.Truncate(time.Second))
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

func (al *AgentLoop) autonomyStatus(sessionKey string) string {
	al.autonomyMu.Lock()
	defer al.autonomyMu.Unlock()

	s, ok := al.autonomyBySess[sessionKey]
	if !ok || s == nil {
		return "自主模式未开启。"
	}

	uptime := time.Since(s.started).Truncate(time.Second)
	idle := time.Since(s.lastUserAt).Truncate(time.Second)
	focus := strings.TrimSpace(s.focus)
	if focus == "" {
		focus = "未设置"
	}
	return fmt.Sprintf("自主模式运行中：空闲阈值 %s，已运行 %s，最近用户活跃距今 %s，自动推进 %d 轮。",
		s.idleInterval.Truncate(time.Second),
		uptime,
		idle,
		s.rounds,
	) + fmt.Sprintf(" 当前研究方向：%s。", focus)
}

func (al *AgentLoop) runAutonomyLoop(ctx context.Context, msg bus.InboundMessage) {
	ticker := time.NewTicker(20 * time.Second)
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
	al.autonomyMu.Lock()
	s, ok := al.autonomyBySess[msg.SessionKey]
	if !ok || s == nil {
		al.autonomyMu.Unlock()
		return false
	}

	now := time.Now()
	if s.pending || now.Sub(s.lastUserAt) < s.idleInterval || now.Sub(s.lastNudgeAt) < s.idleInterval {
		al.autonomyMu.Unlock()
		return true
	}

	s.rounds++
	round := s.rounds
	s.lastNudgeAt = now
	s.pending = true
	focus := strings.TrimSpace(s.focus)
	idleFor := now.Sub(s.lastUserAt).Truncate(time.Second)
	al.autonomyMu.Unlock()

	al.bus.PublishOutbound(bus.OutboundMessage{
		Channel: msg.Channel,
		ChatID:  msg.ChatID,
		Content: fmt.Sprintf("[自主模式] 你已空闲 %s，我已启动第 %d 轮自主推进。", idleFor, round),
	})

	al.bus.PublishInbound(bus.InboundMessage{
		Channel:    msg.Channel,
		SenderID:   "autonomy",
		ChatID:     msg.ChatID,
		SessionKey: msg.SessionKey,
		Content:    buildAutonomyFollowUpPrompt(round, focus),
		Metadata: map[string]string{
			"source": "autonomy",
			"round":  strconv.Itoa(round),
		},
	})

	return true
}

func (al *AgentLoop) finishAutonomyRound(sessionKey string) {
	al.autonomyMu.Lock()
	defer al.autonomyMu.Unlock()
	if s, ok := al.autonomyBySess[sessionKey]; ok && s != nil {
		s.pending = false
	}
}

func buildAutonomyFollowUpPrompt(round int, focus string) string {
	focus = strings.TrimSpace(focus)
	if focus == "" {
		return fmt.Sprintf("自主模式第 %d 轮推进：用户暂时未继续输入。请基于当前会话上下文和已完成工作，自主完成一个高价值下一步，并给出简短进展汇报。", round)
	}
	return fmt.Sprintf("自主模式第 %d 轮推进：用户暂时未继续输入。请优先围绕研究方向“%s”推进；如果该方向已完成，请明确说明已完成并转向其他高价值下一步，再给出简短进展汇报。", round, focus)
}

func buildAutonomyFocusPrompt(focus string) string {
	focus = strings.TrimSpace(focus)
	return fmt.Sprintf("自主模式已启动，本轮请优先围绕研究方向“%s”展开：先明确本轮目标，再执行并汇报阶段性进展与结果。", focus)
}

func (al *AgentLoop) startAutoLearner(msg bus.InboundMessage, interval time.Duration) string {
	if msg.Channel == "cli" {
		return "自动学习需要在 gateway 运行模式下使用（持续消息循环）。"
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

	learnerCtx, cancel := context.WithCancel(context.Background())
	learner := &autoLearner{
		cancel:   cancel,
		started:  time.Now(),
		interval: interval,
	}
	al.autoLearners[msg.SessionKey] = learner
	al.autoLearnMu.Unlock()

	go al.runAutoLearnerLoop(learnerCtx, msg)

	return fmt.Sprintf("自动学习已开启：每 %s 执行 1 轮。使用 /autolearn stop 可停止。", interval.Truncate(time.Second))
}

func (al *AgentLoop) runAutoLearnerLoop(ctx context.Context, msg bus.InboundMessage) {
	runOnce := func() bool {
		round, ok := al.bumpAutoLearnRound(msg.SessionKey)
		if !ok {
			return false
		}

		al.bus.PublishOutbound(bus.OutboundMessage{
			Channel: msg.Channel,
			ChatID:  msg.ChatID,
			Content: fmt.Sprintf("[自动学习] 第 %d 轮开始。", round),
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

func (al *AgentLoop) autoLearnerStatus(sessionKey string) string {
	al.autoLearnMu.Lock()
	defer al.autoLearnMu.Unlock()

	learner, ok := al.autoLearners[sessionKey]
	if !ok || learner == nil {
		return "自动学习未开启。使用 /autolearn start 可开启。"
	}

	uptime := time.Since(learner.started).Truncate(time.Second)
	return fmt.Sprintf("自动学习运行中：每 %s 一轮，已运行 %s，累计 %d 轮。",
		learner.interval.Truncate(time.Second),
		uptime,
		learner.rounds,
	)
}

func buildAutoLearnPrompt(round int) string {
	return fmt.Sprintf("自动学习模式第 %d 轮：无需用户下发任务。请基于当前会话与项目上下文，自主选择一个高价值的小任务并完成。要求：1) 明确本轮学习目标；2) 必要时调用工具执行；3) 将关键结论写入 memory/MEMORY.md；4) 输出简短进展报告。", round)
}

func buildAutonomyTaskPrompt(task string) string {
	return fmt.Sprintf("开启自主执行策略。请直接推进任务并在关键节点自然汇报进展，最后给出结果与下一步建议。\n\n用户任务：%s", strings.TrimSpace(task))
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

	// Route system messages to processSystemMessage
	if msg.Channel == "system" {
		return al.processSystemMessage(ctx, msg)
	}

	// Built-in slash commands (deterministic, no LLM roundtrip)
	if handled, result, err := al.handleSlashCommand(msg); handled {
		return result, err
	}

	al.noteAutonomyUserActivity(msg)

	if intent, ok := al.detectAutonomyIntent(ctx, msg.Content); ok {
		switch intent.action {
		case "start":
			idle := autonomyDefaultIdleInterval
			if intent.idleInterval != nil {
				idle = *intent.idleInterval
			}
			return al.startAutonomy(msg, idle, intent.focus), nil
		case "clear_focus":
			if al.clearAutonomyFocus(msg.SessionKey) {
				return "已确认：当前研究方向已完成，后续自主推进将转向其他高价值任务。", nil
			}
			return "自主模式当前未运行，无法清空研究方向。", nil
		case "stop":
			if al.stopAutonomy(msg.SessionKey) {
				return "自主模式已关闭。", nil
			}
			return "自主模式当前未运行。", nil
		case "status":
			return al.autonomyStatus(msg.SessionKey), nil
		}
	}

	if intent, ok := al.detectAutoLearnIntent(ctx, msg.Content); ok {
		switch intent.action {
		case "start":
			interval := autoLearnDefaultInterval
			if intent.interval != nil {
				interval = *intent.interval
			}
			return al.startAutoLearner(msg, interval), nil
		case "stop":
			if al.stopAutoLearner(msg.SessionKey) {
				return "自动学习已停止。", nil
			}
			return "自动学习当前未运行。", nil
		case "status":
			return al.autoLearnerStatus(msg.SessionKey), nil
		}
	}

	directives := parseTaskExecutionDirectives(msg.Content)
	if inferred, ok := al.inferTaskExecutionDirectives(ctx, msg.Content); ok {
		// Explicit /run/@run command always has higher priority than inferred directives.
		if !isExplicitRunCommand(msg.Content) {
			directives = inferred
		}
	}
	userPrompt := directives.task
	if strings.TrimSpace(userPrompt) == "" {
		userPrompt = msg.Content
	}
	if al.isAutonomyEnabled(msg.SessionKey) && !isSyntheticMessage(msg) {
		directives.stageReport = true
		userPrompt = buildAutonomyTaskPrompt(userPrompt)
	}

	var progress *stageReporter
	if directives.stageReport {
		progress = &stageReporter{
			onUpdate: func(content string) {
				al.bus.PublishOutbound(bus.OutboundMessage{
					Channel: msg.Channel,
					ChatID:  msg.ChatID,
					Content: content,
				})
			},
		}
		progress.Publish(1, 5, "开始", "已接收任务")
		progress.Publish(2, 5, "分析", "正在构建上下文")
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
		progress.Publish(3, 5, "执行", "正在运行任务")
	}

	finalContent, iteration, err := al.runLLMToolLoop(ctx, messages, msg.SessionKey, false, progress)
	if err != nil {
		if progress != nil {
			progress.Publish(5, 5, "失败", err.Error())
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

	// 使用 AddMessageFull 存储包含思考过程或工具调用的完整助手消息
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
		progress.Publish(4, 5, "收敛", "已生成最终回复")
		progress.Publish(5, 5, "完成", fmt.Sprintf("任务执行结束（迭代 %d）", iteration))
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

	// 如果 finalContent 中没有包含 tool calls (即最后一次 LLM 返回的结果)
	// 我们已经通过循环内部的 AddMessageFull 存储了前面的步骤
	// 这里的 AddMessageFull 会存储最终回复
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
			progress.Publish(3, 5, "执行", fmt.Sprintf("第 %d 轮推理", iteration))
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
					progress.Publish(3, 5, "执行", fmt.Sprintf("工具 %s 失败: %v", tc.Name, err))
				} else {
					progress.Publish(3, 5, "执行", fmt.Sprintf("工具 %s 完成", tc.Name))
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
	candidates := al.modelCandidates()
	var lastErr error

	for idx, model := range candidates {
		response, err := al.provider.Chat(ctx, messages, tools, model, options)
		if err == nil {
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
		if !isQuotaOrRateLimitError(err) {
			return nil, err
		}

		if idx < len(candidates)-1 {
			logger.WarnCF("agent", "Model quota/rate-limit reached, trying fallback model", map[string]interface{}{
				"failed_model":    model,
				"next_model":      candidates[idx+1],
				logger.FieldError: err.Error(),
			})
			continue
		}
	}

	return nil, fmt.Errorf("all configured models failed; last error: %w", lastErr)
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
	for _, m := range al.modelFallbacks {
		add(m)
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
	if messageCount < cfg.TriggerMessages {
		return nil
	}

	history := al.sessions.GetHistory(sessionKey)
	if len(history) < cfg.TriggerMessages {
		return nil
	}
	if cfg.KeepRecentMessages >= len(history) {
		return nil
	}

	summary := al.sessions.GetSummary(sessionKey)
	compactUntil := len(history) - cfg.KeepRecentMessages
	compactCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	newSummary, err := al.buildCompactedSummary(compactCtx, summary, history[:compactUntil], cfg.MaxTranscriptChars)
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
		"session_key":      sessionKey,
		"before_messages":  before,
		"after_messages":   after,
		"kept_recent":      cfg.KeepRecentMessages,
		"summary_chars":    len(newSummary),
		"trigger_messages": cfg.TriggerMessages,
	})
	return nil
}

func (al *AgentLoop) buildCompactedSummary(
	ctx context.Context,
	existingSummary string,
	messages []providers.Message,
	maxTranscriptChars int,
) (string, error) {
	transcript := formatCompactionTranscript(messages, maxTranscriptChars)
	if strings.TrimSpace(transcript) == "" {
		return strings.TrimSpace(existingSummary), nil
	}

	systemPrompt := "You are a conversation compactor. Merge prior summary and transcript into a concise, factual memory for future turns. Keep user preferences, constraints, decisions, unresolved tasks, and key technical context. Do not include speculative content."
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

func formatCompactionTranscript(messages []providers.Message, maxChars int) string {
	if maxChars <= 0 || len(messages) == 0 {
		return ""
	}

	var sb strings.Builder
	used := 0
	for _, m := range messages {
		role := strings.TrimSpace(m.Role)
		if role == "" {
			role = "unknown"
		}
		line := fmt.Sprintf("[%s] %s\n", role, strings.TrimSpace(m.Content))
		if len(line) > 1200 {
			line = truncateString(line, 1200) + "\n"
		}
		if used+len(line) > maxChars {
			remain := maxChars - used
			if remain > 16 {
				sb.WriteString(truncateString(line, remain))
			}
			break
		}
		sb.WriteString(line)
		used += len(line)
	}
	return strings.TrimSpace(sb.String())
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

func parseAutoLearnInterval(raw string) (time.Duration, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return autoLearnDefaultInterval, nil
	}
	if d, err := time.ParseDuration(text); err == nil {
		return d, nil
	}
	var n int
	if _, err := fmt.Sscanf(text, "%d", &n); err == nil && n > 0 {
		return time.Duration(n) * time.Minute, nil
	}
	return 0, fmt.Errorf("invalid interval: %s (examples: 5m, 30s, 2h)", raw)
}

func parseAutonomyIdleInterval(raw string) (time.Duration, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return autonomyDefaultIdleInterval, nil
	}
	if d, err := time.ParseDuration(text); err == nil {
		return d, nil
	}
	var n int
	if _, err := fmt.Sscanf(text, "%d", &n); err == nil && n > 0 {
		return time.Duration(n) * time.Minute, nil
	}
	return 0, fmt.Errorf("invalid idle interval: %s (examples: 30m, 1h)", raw)
}

func (al *AgentLoop) detectAutonomyIntent(ctx context.Context, content string) (autonomyIntent, bool) {
	if intent, ok := al.inferAutonomyIntent(ctx, content); ok {
		return intent, true
	}
	return parseAutonomyIntent(content)
}

func (al *AgentLoop) detectAutoLearnIntent(ctx context.Context, content string) (autoLearnIntent, bool) {
	if intent, ok := al.inferAutoLearnIntent(ctx, content); ok {
		return intent, true
	}
	return parseAutoLearnIntent(content)
}

func (al *AgentLoop) inferAutonomyIntent(ctx context.Context, content string) (autonomyIntent, bool) {
	text := strings.TrimSpace(content)
	if text == "" {
		return autonomyIntent{}, false
	}

	// Avoid adding noticeable latency for very long task messages.
	if len(text) > 800 {
		return autonomyIntent{}, false
	}

	systemPrompt := `You classify autonomy-control intent for an AI assistant.
Return JSON only, no markdown.
Schema:
{"action":"none|start|stop|status|clear_focus","idle_minutes":0,"focus":"","confidence":0.0}
Rules:
- "start": user asks assistant to enter autonomous/self-driven mode.
- "stop": user asks assistant to disable autonomous mode.
- "status": user asks autonomy mode status.
- "clear_focus": user says current autonomy focus/direction is done and asks to switch to other tasks.
- "none": anything else.
- confidence: 0..1`

	resp, err := al.callLLMWithModelFallback(ctx, []providers.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: text},
	}, nil, map[string]interface{}{
		"max_tokens":  220,
		"temperature": 0.0,
	})
	if err != nil || resp == nil {
		return autonomyIntent{}, false
	}

	raw := extractJSONObject(resp.Content)
	if raw == "" {
		return autonomyIntent{}, false
	}

	var parsed autonomyIntentLLMResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return autonomyIntent{}, false
	}

	action := strings.ToLower(strings.TrimSpace(parsed.Action))
	switch action {
	case "start", "stop", "status", "clear_focus":
	default:
		return autonomyIntent{}, false
	}

	if parsed.Confidence < 0.75 {
		return autonomyIntent{}, false
	}

	intent := autonomyIntent{
		action: action,
		focus:  strings.TrimSpace(parsed.Focus),
	}
	if parsed.IdleMinutes > 0 {
		d := time.Duration(parsed.IdleMinutes) * time.Minute
		intent.idleInterval = &d
	}
	return intent, true
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

func (al *AgentLoop) inferAutoLearnIntent(ctx context.Context, content string) (autoLearnIntent, bool) {
	text := strings.TrimSpace(content)
	if text == "" || len(text) > 800 {
		return autoLearnIntent{}, false
	}

	systemPrompt := `You classify auto-learning-control intent for an AI assistant.
Return JSON only.
Schema:
{"action":"none|start|stop|status","interval_minutes":0,"confidence":0.0}
Rules:
- "start": user asks assistant to start autonomous learning loop.
- "stop": user asks assistant to stop autonomous learning loop.
- "status": user asks learning loop status.
- "none": anything else.
- confidence: 0..1`

	resp, err := al.callLLMWithModelFallback(ctx, []providers.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: text},
	}, nil, map[string]interface{}{
		"max_tokens":  180,
		"temperature": 0.0,
	})
	if err != nil || resp == nil {
		return autoLearnIntent{}, false
	}

	raw := extractJSONObject(resp.Content)
	if raw == "" {
		return autoLearnIntent{}, false
	}

	var parsed autoLearnIntentLLMResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return autoLearnIntent{}, false
	}

	action := strings.ToLower(strings.TrimSpace(parsed.Action))
	switch action {
	case "start", "stop", "status":
	default:
		return autoLearnIntent{}, false
	}
	if parsed.Confidence < 0.75 {
		return autoLearnIntent{}, false
	}

	intent := autoLearnIntent{action: action}
	if parsed.IntervalMinutes > 0 {
		d := time.Duration(parsed.IntervalMinutes) * time.Minute
		intent.interval = &d
	}
	return intent, true
}

func (al *AgentLoop) inferTaskExecutionDirectives(ctx context.Context, content string) (taskExecutionDirectives, bool) {
	text := strings.TrimSpace(content)
	if text == "" || len(text) > 1200 {
		return taskExecutionDirectives{}, false
	}

	systemPrompt := `Extract execution directives from user message.
Return JSON only.
Schema:
{"task":"","stage_report":false,"confidence":0.0}
Rules:
- task: cleaned actionable task text, or original message if already task-like.
- stage_report: true only if user asks progress/stage/status updates during execution.
- confidence: 0..1`

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

func parseAutonomyIntent(content string) (autonomyIntent, bool) {
	text := strings.TrimSpace(content)
	if text == "" {
		return autonomyIntent{}, false
	}

	fields := strings.Fields(strings.ToLower(text))
	if len(fields) < 2 || fields[0] != "autonomy" {
		return autonomyIntent{}, false
	}
	intent := autonomyIntent{action: fields[1]}
	if intent.action == "start" && len(fields) >= 3 {
		if d, err := parseAutonomyIdleInterval(fields[2]); err == nil {
			intent.idleInterval = &d
			if len(fields) >= 4 {
				intent.focus = strings.TrimSpace(strings.Join(strings.Fields(text)[3:], " "))
			}
		} else {
			intent.focus = strings.TrimSpace(strings.Join(strings.Fields(text)[2:], " "))
		}
	}
	switch intent.action {
	case "start", "stop", "status", "clear_focus":
		return intent, true
	default:
		return autonomyIntent{}, false
	}
}

func parseAutoLearnIntent(content string) (autoLearnIntent, bool) {
	text := strings.TrimSpace(content)
	fields := strings.Fields(strings.ToLower(text))
	if len(fields) < 2 || fields[0] != "autolearn" {
		return autoLearnIntent{}, false
	}
	intent := autoLearnIntent{action: fields[1]}
	if intent.action == "start" && len(fields) >= 3 {
		if d, err := parseAutoLearnInterval(fields[2]); err == nil {
			intent.interval = &d
		}
	}
	switch intent.action {
	case "start", "stop", "status":
		return intent, true
	default:
		return autoLearnIntent{}, false
	}
}

func (al *AgentLoop) handleSlashCommand(msg bus.InboundMessage) (bool, string, error) {
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
		return true, "Slash commands:\n/help\n/status\n/run <task> [--stage-report]\n/autonomy start [idle]\n/autonomy stop\n/autonomy status\n/autolearn start [interval]\n/autolearn stop\n/autolearn status\n/config get <path>\n/config set <path> <value>\n/reload\n/pipeline list\n/pipeline status <pipeline_id>\n/pipeline ready <pipeline_id>", nil
	case "/stop":
		return true, "Stop command is handled by queue runtime. Send /stop from your channel session to interrupt current response.", nil
	case "/status":
		cfg, err := config.LoadConfig(al.getConfigPathForCommands())
		if err != nil {
			return true, "", fmt.Errorf("status failed: %w", err)
		}
		return true, fmt.Sprintf("Model: %s\nAPI Base: %s\nLogging: %v\nConfig: %s",
			cfg.Agents.Defaults.Model,
			cfg.Providers.Proxy.APIBase,
			cfg.Logging.Enabled,
			al.getConfigPathForCommands(),
		), nil
	case "/autolearn":
		if len(fields) < 2 {
			return true, "Usage: /autolearn start [interval] | /autolearn stop | /autolearn status", nil
		}
		switch strings.ToLower(fields[1]) {
		case "start":
			interval := autoLearnDefaultInterval
			if len(fields) >= 3 {
				d, err := parseAutoLearnInterval(fields[2])
				if err != nil {
					return true, "", err
				}
				interval = d
			}
			return true, al.startAutoLearner(msg, interval), nil
		case "stop":
			if al.stopAutoLearner(msg.SessionKey) {
				return true, "自动学习已停止。", nil
			}
			return true, "自动学习当前未运行。", nil
		case "status":
			return true, al.autoLearnerStatus(msg.SessionKey), nil
		default:
			return true, "Usage: /autolearn start [interval] | /autolearn stop | /autolearn status", nil
		}
	case "/autonomy":
		if len(fields) < 2 {
			return true, "Usage: /autonomy start [idle] | /autonomy stop | /autonomy status", nil
		}
		switch strings.ToLower(fields[1]) {
		case "start":
			idle := autonomyDefaultIdleInterval
			focus := ""
			if len(fields) >= 3 {
				d, err := parseAutonomyIdleInterval(fields[2])
				if err != nil {
					focus = strings.Join(fields[2:], " ")
				} else {
					idle = d
				}
			}
			if focus == "" && len(fields) >= 4 {
				focus = strings.Join(fields[3:], " ")
			}
			return true, al.startAutonomy(msg, idle, focus), nil
		case "stop":
			if al.stopAutonomy(msg.SessionKey) {
				return true, "自主模式已关闭。", nil
			}
			return true, "自主模式当前未运行。", nil
		case "status":
			return true, al.autonomyStatus(msg.SessionKey), nil
		default:
			return true, "Usage: /autonomy start [idle] | /autonomy stop | /autonomy status", nil
		}
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
	return filepath.Join(config.GetConfigDir(), "config.json")
}

func (al *AgentLoop) normalizeConfigPathForAgent(path string) string {
	return configops.NormalizeConfigPath(path)
}

func (al *AgentLoop) parseConfigValueForAgent(raw string) interface{} {
	return configops.ParseConfigValue(raw)
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
