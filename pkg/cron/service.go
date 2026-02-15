package cron

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"clawgo/pkg/lifecycle"
)

const (
	defaultRunLoopMinSleep              = 1 * time.Second
	defaultRunLoopMaxSleep              = 30 * time.Second
	defaultRetryBackoffBase             = 30 * time.Second
	defaultRetryBackoffMax              = 30 * time.Minute
	defaultMaxConsecutiveFailureRetries = 5
)

type RuntimeOptions struct {
	RunLoopMinSleep              time.Duration
	RunLoopMaxSleep              time.Duration
	RetryBackoffBase             time.Duration
	RetryBackoffMax              time.Duration
	MaxConsecutiveFailureRetries int64
	MaxWorkers                   int
}

func DefaultRuntimeOptions() RuntimeOptions {
	return RuntimeOptions{
		RunLoopMinSleep:              defaultRunLoopMinSleep,
		RunLoopMaxSleep:              defaultRunLoopMaxSleep,
		RetryBackoffBase:             defaultRetryBackoffBase,
		RetryBackoffMax:              defaultRetryBackoffMax,
		MaxConsecutiveFailureRetries: defaultMaxConsecutiveFailureRetries,
		MaxWorkers:                   1,
	}
}

func normalizeRuntimeOptions(opts RuntimeOptions) RuntimeOptions {
	def := DefaultRuntimeOptions()

	if opts.RunLoopMinSleep <= 0 {
		opts.RunLoopMinSleep = def.RunLoopMinSleep
	}
	if opts.RunLoopMaxSleep <= 0 {
		opts.RunLoopMaxSleep = def.RunLoopMaxSleep
	}
	if opts.RunLoopMinSleep > opts.RunLoopMaxSleep {
		opts.RunLoopMinSleep = opts.RunLoopMaxSleep
	}
	if opts.RetryBackoffBase <= 0 {
		opts.RetryBackoffBase = def.RetryBackoffBase
	}
	if opts.RetryBackoffMax <= 0 {
		opts.RetryBackoffMax = def.RetryBackoffMax
	}
	if opts.RetryBackoffBase > opts.RetryBackoffMax {
		opts.RetryBackoffBase = opts.RetryBackoffMax
	}
	if opts.MaxConsecutiveFailureRetries < 0 {
		opts.MaxConsecutiveFailureRetries = def.MaxConsecutiveFailureRetries
	}
	if opts.MaxWorkers <= 0 {
		opts.MaxWorkers = def.MaxWorkers
	}
	return opts
}

type CronSchedule struct {
	Kind    string `json:"kind"`
	AtMS    *int64 `json:"atMs,omitempty"`
	EveryMS *int64 `json:"everyMs,omitempty"`
	Expr    string `json:"expr,omitempty"`
	TZ      string `json:"tz,omitempty"`
}

type CronPayload struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Deliver bool   `json:"deliver"`
	Channel string `json:"channel,omitempty"`
	To      string `json:"to,omitempty"`
}

type CronJobState struct {
	NextRunAtMS         *int64 `json:"nextRunAtMs,omitempty"`
	LastRunAtMS         *int64 `json:"lastRunAtMs,omitempty"`
	LastStatus          string `json:"lastStatus,omitempty"`
	LastError           string `json:"lastError,omitempty"`
	LastDurationMS      int64  `json:"lastDurationMs,omitempty"`
	LastScheduleDelayMS int64  `json:"lastScheduleDelayMs,omitempty"`
	TotalRuns           int64  `json:"totalRuns,omitempty"`
	TotalFailures       int64  `json:"totalFailures,omitempty"`
	ConsecutiveFailures int64  `json:"consecutiveFailures,omitempty"`
}

type CronJob struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Enabled        bool         `json:"enabled"`
	Schedule       CronSchedule `json:"schedule"`
	Payload        CronPayload  `json:"payload"`
	State          CronJobState `json:"state"`
	CreatedAtMS    int64        `json:"createdAtMs"`
	UpdatedAtMS    int64        `json:"updatedAtMs"`
	DeleteAfterRun bool         `json:"deleteAfterRun"`
}

type CronStore struct {
	Version int       `json:"version"`
	Jobs    []CronJob `json:"jobs"`
}

type JobHandler func(job *CronJob) (string, error)

type CronService struct {
	storePath string
	store     *CronStore
	onJob     JobHandler
	opts      RuntimeOptions
	running   map[string]struct{}
	mu        sync.RWMutex
	runner    *lifecycle.LoopRunner
}

func NewCronService(storePath string, onJob JobHandler) *CronService {
	cs := &CronService{
		storePath: storePath,
		onJob:     onJob,
		opts:      DefaultRuntimeOptions(),
		running:   make(map[string]struct{}),
		runner:    lifecycle.NewLoopRunner(),
	}
	cs.loadStore()
	return cs
}

func (cs *CronService) SetRuntimeOptions(opts RuntimeOptions) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.opts = normalizeRuntimeOptions(opts)
}

func (cs *CronService) Start() error {
	if cs.runner.Running() {
		return nil
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	if err := cs.loadStore(); err != nil {
		return fmt.Errorf("failed to load store: %w", err)
	}

	if changed := cs.recomputeNextRuns(); changed {
		if err := cs.saveStore(); err != nil {
			return fmt.Errorf("failed to save store: %w", err)
		}
	}

	cs.runner.Start(cs.runLoop)

	return nil
}

func (cs *CronService) Stop() {
	cs.runner.Stop()
}

func (cs *CronService) runLoop(stopCh <-chan struct{}) {
	for {
		sleepFor := cs.nextSleepDuration(time.Now())
		timer := time.NewTimer(sleepFor)
		select {
		case <-stopCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
			cs.checkJobs()
		}
	}
}

func (cs *CronService) checkJobs() {
	if !cs.runner.Running() {
		return
	}

	cs.mu.RLock()
	now := time.Now().UnixMilli()
	dueIDs := make([]string, 0)

	for i := range cs.store.Jobs {
		job := &cs.store.Jobs[i]
		if job.Enabled && job.State.NextRunAtMS != nil && *job.State.NextRunAtMS <= now {
			dueIDs = append(dueIDs, job.ID)
		}
	}
	cs.mu.RUnlock()

	if len(dueIDs) == 0 {
		return
	}

	opts := cs.getRuntimeOptions()
	changed := false
	changedMu := sync.Mutex{}
	sem := make(chan struct{}, opts.MaxWorkers)
	var wg sync.WaitGroup
	for _, jobID := range dueIDs {
		if !cs.markJobRunning(jobID) {
			continue
		}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() {
				<-sem
				cs.unmarkJobRunning(id)
			}()
			if cs.executeJobByID(id) {
				changedMu.Lock()
				changed = true
				changedMu.Unlock()
			}
		}(jobID)
	}
	wg.Wait()

	if !changed {
		return
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()
	_ = cs.saveStore()
}

func (cs *CronService) executeJobByID(jobID string) bool {
	cs.mu.RLock()
	idx := cs.findJobIndexByIDLocked(jobID)
	if idx < 0 {
		cs.mu.RUnlock()
		return false
	}

	jobSnapshot := cs.store.Jobs[idx]
	if !jobSnapshot.Enabled || jobSnapshot.State.NextRunAtMS == nil {
		cs.mu.RUnlock()
		return false
	}
	plannedRun := *jobSnapshot.State.NextRunAtMS
	cs.mu.RUnlock()

	startTime := time.Now().UnixMilli()
	execStart := time.Now()

	var err error
	if cs.onJob != nil {
		_, err = cs.onJob(&jobSnapshot)
	}
	durationMS := time.Since(execStart).Milliseconds()

	cs.mu.Lock()
	defer cs.mu.Unlock()

	idx = cs.findJobIndexByIDLocked(jobID)
	if idx < 0 {
		return false
	}
	job := &cs.store.Jobs[idx]
	if !job.Enabled {
		return false
	}

	job.State.LastRunAtMS = &startTime
	job.UpdatedAtMS = time.Now().UnixMilli()
	job.State.LastDurationMS = durationMS
	job.State.LastScheduleDelayMS = maxInt64(0, startTime-plannedRun)
	job.State.TotalRuns++

	if err != nil {
		job.State.LastStatus = "error"
		job.State.LastError = err.Error()
		job.State.TotalFailures++
		job.State.ConsecutiveFailures++
	} else {
		job.State.LastStatus = "ok"
		job.State.LastError = ""
		job.State.ConsecutiveFailures = 0
	}

	if err != nil && job.State.ConsecutiveFailures <= cs.opts.MaxConsecutiveFailureRetries {
		retryAt := time.Now().Add(computeRetryBackoff(job.State.ConsecutiveFailures, cs.opts.RetryBackoffBase, cs.opts.RetryBackoffMax)).UnixMilli()
		job.State.NextRunAtMS = &retryAt
		return true
	}

	if job.Schedule.Kind == "at" {
		if job.DeleteAfterRun {
			cs.removeJobByIDUnsafe(job.ID)
		} else {
			job.Enabled = false
			job.State.NextRunAtMS = nil
		}
	} else {
		nextRun := cs.computeNextRunAfter(&job.Schedule, plannedRun, time.Now().UnixMilli())
		job.State.NextRunAtMS = nextRun
	}
	return true
}

func (cs *CronService) computeNextRun(schedule *CronSchedule, nowMS int64) *int64 {
	return cs.computeNextRunAfter(schedule, nowMS, nowMS)
}

func (cs *CronService) computeNextRunAfter(schedule *CronSchedule, baseMS int64, nowMS int64) *int64 {
	if schedule.Kind == "at" {
		if schedule.AtMS != nil && *schedule.AtMS > nowMS {
			return schedule.AtMS
		}
		return nil
	}

	if schedule.Kind == "every" {
		if schedule.EveryMS == nil || *schedule.EveryMS <= 0 {
			return nil
		}
		next := computeAlignedEveryNext(baseMS, nowMS, *schedule.EveryMS)
		return &next
	}

	if schedule.Kind == "cron" && schedule.Expr != "" {
		parser := NewSimpleCronParser(schedule.Expr)
		next := parser.Next(time.UnixMilli(nowMS))
		if !next.IsZero() {
			ms := next.UnixMilli()
			return &ms
		}
	}

	return nil
}

func (cs *CronService) recomputeNextRuns() bool {
	changed := false
	now := time.Now().UnixMilli()
	for i := range cs.store.Jobs {
		job := &cs.store.Jobs[i]
		oldNext := int64(0)
		oldSet := false
		if job.State.NextRunAtMS != nil {
			oldSet = true
			oldNext = *job.State.NextRunAtMS
		}

		if job.Enabled {
			job.State.NextRunAtMS = cs.computeNextRunAfter(&job.Schedule, now, now)
		}

		newSet := job.State.NextRunAtMS != nil
		newNext := int64(0)
		if newSet {
			newNext = *job.State.NextRunAtMS
		}
		if oldSet != newSet || oldNext != newNext {
			changed = true
		}
	}
	return changed
}

func (cs *CronService) getNextWakeMS() *int64 {
	var nextWake *int64
	for _, job := range cs.store.Jobs {
		if job.Enabled && job.State.NextRunAtMS != nil {
			if nextWake == nil || *job.State.NextRunAtMS < *nextWake {
				nextWake = job.State.NextRunAtMS
			}
		}
	}
	return nextWake
}

func (cs *CronService) Load() error {
	return cs.loadStore()
}

func (cs *CronService) loadStore() error {
	cs.store = &CronStore{
		Version: 1,
		Jobs:    []CronJob{},
	}

	data, err := os.ReadFile(cs.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return json.Unmarshal(data, cs.store)
}

func (cs *CronService) saveStore() error {
	dir := filepath.Dir(cs.storePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cs.store, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := cs.storePath + ".tmp"
	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, cs.storePath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	dirFile, err := os.Open(dir)
	if err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
}

func (cs *CronService) AddJob(name string, schedule CronSchedule, message string, deliver bool, channel, to string) (*CronJob, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	now := time.Now().UnixMilli()

	job := CronJob{
		ID:       generateID(),
		Name:     name,
		Enabled:  true,
		Schedule: schedule,
		Payload: CronPayload{
			Kind:    "agent_turn",
			Message: message,
			Deliver: deliver,
			Channel: channel,
			To:      to,
		},
		State: CronJobState{
			NextRunAtMS: cs.computeNextRun(&schedule, now),
		},
		CreatedAtMS:    now,
		UpdatedAtMS:    now,
		DeleteAfterRun: false,
	}

	cs.store.Jobs = append(cs.store.Jobs, job)
	if err := cs.saveStore(); err != nil {
		return nil, err
	}

	return &job, nil
}

func (cs *CronService) RemoveJob(jobID string) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	return cs.removeJobUnsafe(jobID)
}

func (cs *CronService) removeJobUnsafe(jobID string) bool {
	removed := cs.removeJobByIDUnsafe(jobID)
	if removed {
		_ = cs.saveStore()
	}
	return removed
}

func (cs *CronService) removeJobByIDUnsafe(jobID string) bool {
	idx := cs.findJobIndexByIDLocked(jobID)
	if idx < 0 {
		return false
	}
	lastIdx := len(cs.store.Jobs) - 1
	cs.store.Jobs[idx] = cs.store.Jobs[lastIdx]
	cs.store.Jobs = cs.store.Jobs[:lastIdx]
	return true
}

func (cs *CronService) findJobIndexByIDLocked(jobID string) int {
	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID == jobID {
			return i
		}
	}
	return -1
}

func (cs *CronService) nextSleepDuration(now time.Time) time.Duration {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	nextWake := cs.getNextWakeMS()
	if nextWake == nil {
		return cs.opts.RunLoopMaxSleep
	}

	sleep := time.Until(time.UnixMilli(*nextWake))
	if sleep < cs.opts.RunLoopMinSleep {
		return cs.opts.RunLoopMinSleep
	}
	if sleep > cs.opts.RunLoopMaxSleep {
		return cs.opts.RunLoopMaxSleep
	}
	return sleep
}

func (cs *CronService) getRuntimeOptions() RuntimeOptions {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.opts
}

func (cs *CronService) markJobRunning(jobID string) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if _, ok := cs.running[jobID]; ok {
		return false
	}
	cs.running[jobID] = struct{}{}
	return true
}

func (cs *CronService) unmarkJobRunning(jobID string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.running, jobID)
}

func computeAlignedEveryNext(baseMS, nowMS, intervalMS int64) int64 {
	if intervalMS <= 0 {
		return nowMS
	}
	next := baseMS + intervalMS
	if next > nowMS {
		return next
	}
	miss := (nowMS-next)/intervalMS + 1
	return next + miss*intervalMS
}

func computeRetryBackoff(consecutiveFailures int64, base, max time.Duration) time.Duration {
	if base <= 0 {
		base = defaultRetryBackoffBase
	}
	if max <= 0 {
		max = defaultRetryBackoffMax
	}
	if base > max {
		base = max
	}

	if consecutiveFailures <= 0 {
		return base
	}
	shift := consecutiveFailures - 1
	if shift > 16 {
		shift = 16
	}
	mult := math.Pow(2, float64(shift))
	backoff := time.Duration(float64(base) * mult)
	if backoff > max {
		return max
	}
	return backoff
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (cs *CronService) EnableJob(jobID string, enabled bool) *CronJob {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for i := range cs.store.Jobs {
		job := &cs.store.Jobs[i]
		if job.ID == jobID {
			job.Enabled = enabled
			job.UpdatedAtMS = time.Now().UnixMilli()

			if enabled {
				job.State.NextRunAtMS = cs.computeNextRun(&job.Schedule, time.Now().UnixMilli())
			} else {
				job.State.NextRunAtMS = nil
			}

			cs.saveStore()
			return job
		}
	}

	return nil
}

func (cs *CronService) ListJobs(includeDisabled bool) []CronJob {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	if includeDisabled {
		return cs.store.Jobs
	}

	var enabled []CronJob
	for _, job := range cs.store.Jobs {
		if job.Enabled {
			enabled = append(enabled, job)
		}
	}

	return enabled
}

func (cs *CronService) Status() map[string]interface{} {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	var enabledCount int
	var totalRuns int64
	var totalFailures int64
	var latestDelayMS int64
	var latestDurationMS int64
	for _, job := range cs.store.Jobs {
		if job.Enabled {
			enabledCount++
		}
		totalRuns += job.State.TotalRuns
		totalFailures += job.State.TotalFailures
		if job.State.LastScheduleDelayMS > latestDelayMS {
			latestDelayMS = job.State.LastScheduleDelayMS
		}
		if job.State.LastDurationMS > latestDurationMS {
			latestDurationMS = job.State.LastDurationMS
		}
	}

	return map[string]interface{}{
		"enabled":          cs.runner.Running(),
		"jobs":             len(cs.store.Jobs),
		"nextWakeAtMS":     cs.getNextWakeMS(),
		"enabledJobs":      enabledCount,
		"totalRuns":        totalRuns,
		"totalFailures":    totalFailures,
		"latestDelayMs":    latestDelayMS,
		"latestDurationMs": latestDurationMS,
	}
}

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
