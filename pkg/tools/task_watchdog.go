package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	minCommandTick = 1 * time.Second
	maxCommandTick = 45 * time.Second
	watchdogTick   = 1 * time.Second
	minWorldCycle  = 10 * time.Second
	maxWorldCycle  = 60 * time.Second
)

const GlobalWatchdogTick = watchdogTick

var ErrCommandNoProgress = errors.New("command no progress across tick rounds")
var ErrTaskWatchdogTimeout = errors.New("task watchdog timeout exceeded")

type commandRuntimePolicy struct {
	BaseTick        time.Duration
	StallRoundLimit int
	MaxRestarts     int
	Difficulty      int
}

func buildCommandRuntimePolicy(command string, baseTick time.Duration) commandRuntimePolicy {
	diff := commandDifficulty(command)
	cpu := runtime.NumCPU()

	// Baseline: kill/restart after 5 unchanged-progress ticks.
	stallLimit := 5
	// Difficulty adjustment (1..4) => +0..6 rounds.
	stallLimit += (diff - 1) * 2
	// Hardware adjustment: weaker CPU gets more patience.
	switch {
	case cpu <= 4:
		stallLimit += 5
	case cpu <= 8:
		stallLimit += 3
	case cpu <= 16:
		stallLimit += 1
	}
	if stallLimit < 5 {
		stallLimit = 5
	}
	if stallLimit > 24 {
		stallLimit = 24
	}

	// Restart budget: heavier tasks and weaker CPUs allow extra retries.
	restarts := 1
	if diff >= 3 {
		restarts++
	}
	if cpu <= 4 {
		restarts++
	}
	if restarts > 3 {
		restarts = 3
	}

	return commandRuntimePolicy{
		BaseTick:        normalizeCommandTick(baseTick),
		StallRoundLimit: stallLimit,
		MaxRestarts:     restarts,
		Difficulty:      diff,
	}
}

type commandWatchdog struct {
	mu           sync.Mutex
	watches      map[uint64]*watchedCommand
	waiters      []*watchWaiter
	nextID       uint64
	cpuTotal     int
	baseActive   int
	baseHeavy    int
	reservePct   float64
	usageRatio   float64
	lastSample   time.Time
	worldCycle   time.Duration
	nextSampleAt time.Time
	active       int
	activeHeavy  int
	queueLimit   int
	queuePath    string
}

type watchedCommand struct {
	id              uint64
	cmd             *exec.Cmd
	startedAt       time.Time
	baseTick        time.Duration
	stallRoundLimit int
	nextCheckAt     time.Time
	lastProgress    int
	stalledRounds   int
	progressFn      func() int
	stallNotify     chan int
	heavy           bool
	source          string
	label           string
}

type stalledCommand struct {
	cmd    *exec.Cmd
	rounds int
	notify chan int
}

type watchWaiter struct {
	id         uint64
	heavy      bool
	ready      chan struct{}
	source     string
	label      string
	enqueuedAt time.Time
}

var globalCommandWatchdog = newCommandWatchdog()
var reLoadAverage = regexp.MustCompile(`load averages?:\s*([0-9]+(?:[.,][0-9]+)?)`)

func newCommandWatchdog() *commandWatchdog {
	cpu := runtime.NumCPU()
	baseActive, baseHeavy, queueLimit := deriveWatchdogLimits(cpu)
	wd := &commandWatchdog{
		watches:    make(map[uint64]*watchedCommand),
		waiters:    make([]*watchWaiter, 0, queueLimit),
		cpuTotal:   cpu,
		baseActive: baseActive,
		baseHeavy:  baseHeavy,
		reservePct: 0.20,
		usageRatio: 0,
		worldCycle: 20 * time.Second,
		queueLimit: queueLimit,
	}
	go wd.loop()
	return wd
}

func deriveWatchdogLimits(cpu int) (maxActive, maxHeavy, queueLimit int) {
	if cpu <= 0 {
		cpu = 2
	}
	maxActive = cpu
	if maxActive < 2 {
		maxActive = 2
	}
	if maxActive > 12 {
		maxActive = 12
	}
	maxHeavy = cpu/4 + 1
	if maxHeavy < 1 {
		maxHeavy = 1
	}
	if maxHeavy > 4 {
		maxHeavy = 4
	}
	queueLimit = maxActive * 8
	if queueLimit < 16 {
		queueLimit = 16
	}
	return
}

func (wd *commandWatchdog) loop() {
	ticker := time.NewTicker(watchdogTick)
	defer ticker.Stop()
	for now := range ticker.C {
		wd.refreshSystemUsage(now)
		wd.tick(now)
	}
}

func (wd *commandWatchdog) refreshSystemUsage(now time.Time) {
	if wd == nil {
		return
	}
	wd.mu.Lock()
	if wd.nextSampleAt.IsZero() {
		wd.nextSampleAt = now
	}
	if now.Before(wd.nextSampleAt) {
		wd.mu.Unlock()
		return
	}
	wd.lastSample = now
	cpu := wd.cpuTotal
	cycle := wd.computeWorldCycleLocked()
	wd.worldCycle = cycle
	wd.nextSampleAt = now.Add(cycle)
	wd.mu.Unlock()

	usage := sampleSystemUsageRatio(cpu)

	wd.mu.Lock()
	wd.usageRatio = usage
	wd.mu.Unlock()
}

func (wd *commandWatchdog) computeWorldCycleLocked() time.Duration {
	if wd == nil {
		return 20 * time.Second
	}
	// Game-world style cycle:
	// base=20s; busier world => shorter cycle; idle world => longer cycle.
	cycle := 20 * time.Second
	pending := len(wd.waiters)
	if pending > 0 {
		cycle -= time.Duration(minInt(pending, 8)) * time.Second
	}
	if wd.active > wd.baseActive/2 {
		cycle -= 3 * time.Second
	}
	if wd.active == 0 && pending == 0 {
		cycle += 10 * time.Second
	}
	if cycle < minWorldCycle {
		cycle = minWorldCycle
	}
	if cycle > maxWorldCycle {
		cycle = maxWorldCycle
	}
	return cycle
}

func (wd *commandWatchdog) register(cmd *exec.Cmd, baseTick time.Duration, stallRoundLimit int, progressFn func() int, stallNotify chan int, heavy bool, source, label string) func() {
	if wd == nil || cmd == nil {
		return func() {}
	}
	base := normalizeCommandTick(baseTick)
	id := atomic.AddUint64(&wd.nextID, 1)
	w := &watchedCommand{
		id:              id,
		cmd:             cmd,
		startedAt:       time.Now(),
		baseTick:        base,
		stallRoundLimit: stallRoundLimit,
		nextCheckAt:     time.Now().Add(base),
		lastProgress:    safeProgress(progressFn),
		progressFn:      progressFn,
		stallNotify:     stallNotify,
		heavy:           heavy,
		source:          strings.TrimSpace(source),
		label:           strings.TrimSpace(label),
	}

	wd.mu.Lock()
	wd.watches[id] = w
	snap := wd.buildQueueSnapshotLocked()
	wd.mu.Unlock()
	wd.writeQueueSnapshot(snap)

	var once sync.Once
	return func() {
		once.Do(func() {
			wd.mu.Lock()
			delete(wd.watches, id)
			snap := wd.buildQueueSnapshotLocked()
			wd.mu.Unlock()
			wd.writeQueueSnapshot(snap)
		})
	}
}

func (wd *commandWatchdog) setQueuePath(path string) {
	if wd == nil {
		return
	}
	path = strings.TrimSpace(path)
	if path != "" {
		path = filepath.Clean(path)
	}
	wd.mu.Lock()
	changed := wd.queuePath != path
	wd.queuePath = path
	snap := wd.buildQueueSnapshotLocked()
	wd.mu.Unlock()
	if changed {
		wd.writeQueueSnapshot(snap)
	}
}

func (wd *commandWatchdog) acquireSlot(ctx context.Context, heavy bool, source, label string) (func(), error) {
	if wd == nil {
		return func() {}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	wd.mu.Lock()
	if wd.canAcquireSlotLocked(heavy) {
		wd.grantSlotLocked(heavy)
		snap := wd.buildQueueSnapshotLocked()
		wd.mu.Unlock()
		wd.writeQueueSnapshot(snap)
		return wd.releaseSlotFunc(heavy), nil
	}
	// Queue when slots are full; wait until a slot is available or context cancels.
	waitID := atomic.AddUint64(&wd.nextID, 1)
	w := &watchWaiter{
		id:         waitID,
		heavy:      heavy,
		ready:      make(chan struct{}, 1),
		source:     strings.TrimSpace(source),
		label:      strings.TrimSpace(label),
		enqueuedAt: time.Now(),
	}
	wd.waiters = append(wd.waiters, w)
	snap := wd.buildQueueSnapshotLocked()
	wd.mu.Unlock()
	wd.writeQueueSnapshot(snap)

	for {
		select {
		case <-ctx.Done():
			wd.mu.Lock()
			wd.removeWaiterLocked(waitID)
			snap := wd.buildQueueSnapshotLocked()
			wd.mu.Unlock()
			wd.writeQueueSnapshot(snap)
			return nil, ctx.Err()
		case <-w.ready:
			return wd.releaseSlotFunc(heavy), nil
		}
	}
}

func (wd *commandWatchdog) releaseSlotFunc(heavy bool) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			wd.mu.Lock()
			if wd.active > 0 {
				wd.active--
			}
			if heavy && wd.activeHeavy > 0 {
				wd.activeHeavy--
			}
			wd.scheduleWaitersLocked()
			snap := wd.buildQueueSnapshotLocked()
			wd.mu.Unlock()
			wd.writeQueueSnapshot(snap)
		})
	}
}

func (wd *commandWatchdog) canAcquireSlotLocked(heavy bool) bool {
	maxActive, maxHeavy := wd.dynamicLimitsLocked()
	if wd.active >= maxActive {
		return false
	}
	if heavy && wd.activeHeavy >= maxHeavy {
		return false
	}
	return true
}

func (wd *commandWatchdog) grantSlotLocked(heavy bool) {
	wd.active++
	if heavy {
		wd.activeHeavy++
	}
}

func (wd *commandWatchdog) dynamicLimitsLocked() (maxActive, maxHeavy int) {
	if wd == nil {
		return 1, 1
	}
	maxActive = computeDynamicActiveSlots(wd.cpuTotal, wd.reservePct, wd.usageRatio, wd.baseActive)
	maxHeavy = computeDynamicHeavySlots(maxActive, wd.baseHeavy)
	return
}

func computeDynamicActiveSlots(cpu int, reservePct, usageRatio float64, baseActive int) int {
	if cpu <= 0 {
		cpu = 1
	}
	if reservePct <= 0 {
		reservePct = 0.20
	}
	if reservePct > 0.90 {
		reservePct = 0.90
	}
	if usageRatio < 0 {
		usageRatio = 0
	}
	if usageRatio > 0.95 {
		usageRatio = 0.95
	}
	headroom := 1.0 - reservePct - usageRatio
	if headroom < 0 {
		headroom = 0
	}
	maxActive := int(float64(cpu) * headroom)
	if maxActive < 1 {
		maxActive = 1
	}
	if baseActive > 0 && maxActive > baseActive {
		maxActive = baseActive
	}
	return maxActive
}

func computeDynamicHeavySlots(maxActive, baseHeavy int) int {
	if maxActive <= 0 {
		return 1
	}
	maxHeavy := maxActive/2 + 1
	if maxHeavy < 1 {
		maxHeavy = 1
	}
	if baseHeavy > 0 && maxHeavy > baseHeavy {
		maxHeavy = baseHeavy
	}
	if maxHeavy > maxActive {
		maxHeavy = maxActive
	}
	return maxHeavy
}

func (wd *commandWatchdog) scheduleWaitersLocked() {
	if len(wd.waiters) == 0 {
		return
	}
	for {
		progress := false
		for i := 0; i < len(wd.waiters); {
			w := wd.waiters[i]
			if w == nil {
				wd.waiters = append(wd.waiters[:i], wd.waiters[i+1:]...)
				progress = true
				continue
			}
			if !wd.canAcquireSlotLocked(w.heavy) {
				i++
				continue
			}
			wd.grantSlotLocked(w.heavy)
			wd.waiters = append(wd.waiters[:i], wd.waiters[i+1:]...)
			select {
			case w.ready <- struct{}{}:
			default:
			}
			progress = true
		}
		if !progress {
			break
		}
	}
}

func (wd *commandWatchdog) removeWaiterLocked(id uint64) {
	if id == 0 || len(wd.waiters) == 0 {
		return
	}
	for i, w := range wd.waiters {
		if w == nil || w.id != id {
			continue
		}
		wd.waiters = append(wd.waiters[:i], wd.waiters[i+1:]...)
		return
	}
}

func (wd *commandWatchdog) tick(now time.Time) {
	if wd == nil {
		return
	}
	toStall := make([]stalledCommand, 0, 4)
	changed := false

	wd.mu.Lock()
	for id, w := range wd.watches {
		if w == nil {
			delete(wd.watches, id)
			changed = true
			continue
		}
		if now.Before(w.nextCheckAt) {
			continue
		}
		cur := safeProgress(w.progressFn)
		if cur > w.lastProgress {
			w.lastProgress = cur
			w.stalledRounds = 0
		} else {
			w.stalledRounds++
			changed = true
			if w.stallRoundLimit > 0 && w.stalledRounds >= w.stallRoundLimit {
				delete(wd.watches, id)
				changed = true
				toStall = append(toStall, stalledCommand{
					cmd:    w.cmd,
					rounds: w.stalledRounds,
					notify: w.stallNotify,
				})
				continue
			}
		}
		next := nextCommandTick(w.baseTick, now.Sub(w.startedAt))
		w.nextCheckAt = now.Add(next)
		changed = true
	}
	snap := wd.buildQueueSnapshotLocked()
	wd.mu.Unlock()

	if changed {
		wd.writeQueueSnapshot(snap)
	}

	for _, st := range toStall {
		if st.cmd != nil && st.cmd.Process != nil {
			_ = st.cmd.Process.Kill()
		}
		if st.notify != nil {
			select {
			case st.notify <- st.rounds:
			default:
			}
		}
	}
}

func safeProgress(progressFn func() int) (progress int) {
	if progressFn == nil {
		return 0
	}
	defer func() {
		if recover() != nil {
			progress = 0
		}
	}()
	progress = progressFn()
	if progress < 0 {
		return 0
	}
	return progress
}

func runCommandWithDynamicTick(ctx context.Context, cmd *exec.Cmd, source, label string, difficulty int, baseTick time.Duration, stallRoundLimit int, progressFn func() int) error {
	base := normalizeCommandTick(baseTick)
	heavy := difficulty >= 3
	releaseSlot, err := globalCommandWatchdog.acquireSlot(ctx, heavy, source, label)
	if err != nil {
		return err
	}
	defer releaseSlot()
	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stallNotify := make(chan int, 1)
	unwatch := globalCommandWatchdog.register(cmd, base, stallRoundLimit, progressFn, stallNotify, heavy, source, label)
	defer unwatch()

	for {
		select {
		case err := <-done:
			return err
		case stalledRounds := <-stallNotify:
			select {
			case err := <-done:
				return fmt.Errorf("%w: %d ticks without progress (%v)", ErrCommandNoProgress, stalledRounds, err)
			case <-time.After(2 * time.Second):
				return fmt.Errorf("%w: %d ticks without progress", ErrCommandNoProgress, stalledRounds)
			}
		case <-ctx.Done():
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			select {
			case err := <-done:
				if err != nil {
					return err
				}
			case <-time.After(2 * time.Second):
			}
			return ctx.Err()
		}
	}
}

type stringTaskResult struct {
	output string
	err    error
}

type stringTaskWatchdogOptions struct {
	ProgressFn func() int
	CanExtend  func() bool
}

// runStringTaskWithTaskWatchdog executes a string-returning task with the same
// tick pacing as the command watchdog, but only times out after a full timeout
// window without observable progress or an allowed extension signal.
func runStringTaskWithTaskWatchdog(
	ctx context.Context,
	timeoutSec int,
	baseTick time.Duration,
	opts stringTaskWatchdogOptions,
	run func(context.Context) (string, error),
) (string, error) {
	if run == nil {
		return "", fmt.Errorf("run function is nil")
	}
	if timeoutSec <= 0 {
		return run(ctx)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	timeout := time.Duration(timeoutSec) * time.Second
	lastProgressAt := time.Now()
	lastProgress := safeProgress(opts.ProgressFn)
	tick := normalizeCommandTick(baseTick)
	if tick <= 0 {
		tick = 2 * time.Second
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan stringTaskResult, 1)
	go func() {
		out, err := run(runCtx)
		done <- stringTaskResult{output: out, err: err}
	}()

	timer := time.NewTimer(tick)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			cancel()
			return "", ctx.Err()
		case res := <-done:
			return res.output, res.err
		case <-timer.C:
			if cur := safeProgress(opts.ProgressFn); cur > lastProgress {
				lastProgress = cur
				lastProgressAt = time.Now()
			}
			stalledFor := time.Since(lastProgressAt)
			if stalledFor >= timeout {
				if opts.CanExtend != nil && opts.CanExtend() {
					lastProgressAt = time.Now()
					stalledFor = 0
				} else {
					cancel()
					select {
					case res := <-done:
						if res.err != nil {
							return "", fmt.Errorf("%w: %v", ErrTaskWatchdogTimeout, res.err)
						}
					case <-time.After(2 * time.Second):
					}
					return "", fmt.Errorf("%w: %ds", ErrTaskWatchdogTimeout, timeoutSec)
				}
			}
			next := nextCommandTick(tick, stalledFor)
			if next <= 0 {
				next = tick
			}
			timer.Reset(next)
		}
	}
}

func (wd *commandWatchdog) buildQueueSnapshotLocked() map[string]interface{} {
	if wd == nil {
		return nil
	}
	maxActive, maxHeavy := wd.dynamicLimitsLocked()
	running := make([]map[string]interface{}, 0, len(wd.watches))
	for _, w := range wd.watches {
		if w == nil {
			continue
		}
		running = append(running, map[string]interface{}{
			"id":                w.id,
			"source":            queueNonEmpty(w.source, "exec"),
			"label":             w.label,
			"heavy":             w.heavy,
			"status":            "running",
			"started_at":        w.startedAt.UTC().Format(time.RFC3339),
			"next_check_at":     w.nextCheckAt.UTC().Format(time.RFC3339),
			"stalled_rounds":    w.stalledRounds,
			"stall_round_limit": w.stallRoundLimit,
			"last_progress":     w.lastProgress,
		})
	}
	waiting := make([]map[string]interface{}, 0, len(wd.waiters))
	for _, w := range wd.waiters {
		if w == nil {
			continue
		}
		waiting = append(waiting, map[string]interface{}{
			"id":          w.id,
			"source":      queueNonEmpty(w.source, "exec"),
			"label":       w.label,
			"heavy":       w.heavy,
			"status":      "waiting",
			"enqueued_at": w.enqueuedAt.UTC().Format(time.RFC3339),
		})
	}
	return map[string]interface{}{
		"time": time.Now().UTC().Format(time.RFC3339),
		"watchdog": map[string]interface{}{
			"cpu_total":       wd.cpuTotal,
			"reserve_pct":     wd.reservePct,
			"usage_ratio":     wd.usageRatio,
			"world_cycle_sec": int(wd.worldCycle.Seconds()),
			"next_sample_at": func() string {
				if wd.nextSampleAt.IsZero() {
					return ""
				}
				return wd.nextSampleAt.UTC().Format(time.RFC3339)
			}(),
			"max_active":   maxActive,
			"max_heavy":    maxHeavy,
			"active":       wd.active,
			"active_heavy": wd.activeHeavy,
			"waiting":      len(waiting),
			"running":      len(running),
		},
		"running": running,
		"waiting": waiting,
	}
}

func (wd *commandWatchdog) writeQueueSnapshot(snap map[string]interface{}) {
	if wd == nil || snap == nil {
		return
	}
	wd.mu.Lock()
	path := strings.TrimSpace(wd.queuePath)
	wd.mu.Unlock()
	if path == "" {
		return
	}
	raw, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = os.WriteFile(path, raw, 0644)
}

func queueNonEmpty(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func nextCommandTick(baseTick, elapsed time.Duration) time.Duration {
	base := normalizeCommandTick(baseTick)
	if elapsed < 0 {
		elapsed = 0
	}
	next := base + elapsed/8
	if next > maxCommandTick {
		return maxCommandTick
	}
	if next < base {
		return base
	}
	return next
}

func normalizeCommandTick(baseTick time.Duration) time.Duration {
	if baseTick < minCommandTick {
		return minCommandTick
	}
	if baseTick > maxCommandTick {
		return maxCommandTick
	}
	return baseTick
}

func commandDifficulty(command string) int {
	cmd := strings.ToLower(strings.TrimSpace(command))
	if cmd == "" {
		return 1
	}
	// 4: very heavy build / container graph.
	for _, p := range []string{"docker build", "docker compose build", "bazel build", "gradle build", "mvn package"} {
		if strings.Contains(cmd, p) {
			return 4
		}
	}
	// 3: compile/test/install heavy workloads.
	for _, p := range []string{"go test", "go build", "cargo build", "npm install", "npm ci", "pnpm install", "yarn install", "npm run build", "pnpm build", "yarn build"} {
		if strings.Contains(cmd, p) {
			return 3
		}
	}
	// 2: medium multi-step shell chains.
	if strings.Contains(cmd, "&&") || strings.Contains(cmd, "|") {
		return 2
	}
	return 1
}

func sampleSystemUsageRatio(cpu int) float64 {
	if cpu <= 0 {
		cpu = 1
	}
	load1, ok := readLoadAverage1()
	if !ok {
		return 0
	}
	ratio := load1 / float64(cpu)
	if ratio < 0 {
		return 0
	}
	if ratio > 0.95 {
		return 0.95
	}
	return ratio
}

func readLoadAverage1() (float64, bool) {
	// Linux fast path.
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(strings.TrimSpace(string(b)))
		if len(fields) > 0 {
			if v, err := strconv.ParseFloat(fields[0], 64); err == nil && v >= 0 {
				return v, true
			}
		}
	}

	// macOS/BSD fallback.
	if out, err := runCommandOutputWithTimeout(300*time.Millisecond, "sysctl", "-n", "vm.loadavg"); err == nil {
		fields := strings.Fields(strings.Trim(strings.TrimSpace(string(out)), "{}"))
		if len(fields) > 0 {
			if v, err := strconv.ParseFloat(strings.ReplaceAll(fields[0], ",", "."), 64); err == nil && v >= 0 {
				return v, true
			}
		}
	}
	if out, err := runCommandOutputWithTimeout(300*time.Millisecond, "uptime"); err == nil {
		m := reLoadAverage.FindStringSubmatch(strings.ToLower(string(out)))
		if len(m) >= 2 {
			if v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64); err == nil && v >= 0 {
				return v, true
			}
		}
	}
	return 0, false
}

func runCommandOutputWithTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	if timeout <= 0 {
		timeout = 300 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}
