package cron

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestComputeAlignedEveryNext(t *testing.T) {
	base := int64(1_000)
	interval := int64(1_000)

	next := computeAlignedEveryNext(base, 1_100, interval)
	if next != 2_000 {
		t.Fatalf("unexpected next: %d", next)
	}

	next = computeAlignedEveryNext(base, 2_500, interval)
	if next != 3_000 {
		t.Fatalf("unexpected next after missed windows: %d", next)
	}
}

func TestComputeRetryBackoff(t *testing.T) {
	opts := DefaultRuntimeOptions()
	if got := computeRetryBackoff(1, opts.RetryBackoffBase, opts.RetryBackoffMax); got != opts.RetryBackoffBase {
		t.Fatalf("unexpected backoff for 1: %s", got)
	}

	if got := computeRetryBackoff(2, opts.RetryBackoffBase, opts.RetryBackoffMax); got != 2*opts.RetryBackoffBase {
		t.Fatalf("unexpected backoff for 2: %s", got)
	}

	got := computeRetryBackoff(20, opts.RetryBackoffBase, opts.RetryBackoffMax)
	if got != opts.RetryBackoffMax {
		t.Fatalf("backoff should cap at %s, got %s", opts.RetryBackoffMax, got)
	}
}

func TestNextSleepDuration(t *testing.T) {
	cs := &CronService{
		opts:    DefaultRuntimeOptions(),
		running: map[string]struct{}{},
		store: &CronStore{
			Jobs: []CronJob{},
		},
	}

	if got := cs.nextSleepDuration(time.Now()); got != cs.opts.RunLoopMaxSleep {
		t.Fatalf("expected max sleep when no jobs, got %s", got)
	}

	nowMS := time.Now().UnixMilli()
	soon := nowMS + 100
	cs.store.Jobs = []CronJob{
		{
			ID:      "1",
			Enabled: true,
			State: CronJobState{
				NextRunAtMS: &soon,
			},
		},
	}

	got := cs.nextSleepDuration(time.Now())
	if got != cs.opts.RunLoopMinSleep {
		t.Fatalf("expected min sleep for near due jobs, got %s", got)
	}
}

func TestCheckJobs_NoConcurrentRunForSameJob(t *testing.T) {
	var running int32
	var maxRunning int32
	var calls int32

	storePath := filepath.Join(t.TempDir(), "jobs.json")
	cs := NewCronService(storePath, func(job *CronJob) (string, error) {
		cur := atomic.AddInt32(&running, 1)
		for {
			prev := atomic.LoadInt32(&maxRunning)
			if cur <= prev || atomic.CompareAndSwapInt32(&maxRunning, prev, cur) {
				break
			}
		}
		time.Sleep(120 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		atomic.AddInt32(&calls, 1)
		return "ok", nil
	})
	cs.SetRuntimeOptions(RuntimeOptions{
		RunLoopMinSleep:              time.Second,
		RunLoopMaxSleep:              2 * time.Second,
		RetryBackoffBase:             time.Second,
		RetryBackoffMax:              5 * time.Second,
		MaxConsecutiveFailureRetries: 1,
		MaxWorkers:                   4,
	})

	now := time.Now().UnixMilli()
	every := int64(60_000)
	cs.mu.Lock()
	cs.store.Jobs = []CronJob{
		{
			ID:      "job-1",
			Enabled: true,
			Schedule: CronSchedule{
				Kind:    "every",
				EveryMS: &every,
			},
			State: CronJobState{
				NextRunAtMS: &now,
			},
		},
	}
	cs.mu.Unlock()

	cs.runner.Start(func(stop <-chan struct{}) { <-stop })
	defer cs.runner.Stop()

	go cs.checkJobs()
	time.Sleep(10 * time.Millisecond)
	cs.checkJobs()
	time.Sleep(220 * time.Millisecond)

	if atomic.LoadInt32(&maxRunning) > 1 {
		t.Fatalf("same job executed concurrently, max running=%d", atomic.LoadInt32(&maxRunning))
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected exactly one execution, got %d", atomic.LoadInt32(&calls))
	}
}

func TestSetRuntimeOptions_AffectsRetryBackoff(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	cs := NewCronService(storePath, func(job *CronJob) (string, error) {
		return "", fmt.Errorf("fail")
	})
	cs.SetRuntimeOptions(RuntimeOptions{
		RunLoopMinSleep:              time.Second,
		RunLoopMaxSleep:              2 * time.Second,
		RetryBackoffBase:             2 * time.Second,
		RetryBackoffMax:              2 * time.Second,
		MaxConsecutiveFailureRetries: 10,
		MaxWorkers:                   1,
	})

	now := time.Now().UnixMilli()
	every := int64(60_000)
	cs.mu.Lock()
	cs.store.Jobs = []CronJob{
		{
			ID:      "job-1",
			Enabled: true,
			Schedule: CronSchedule{
				Kind:    "every",
				EveryMS: &every,
			},
			State: CronJobState{
				NextRunAtMS: &now,
			},
		},
	}
	cs.mu.Unlock()

	cs.runner.Start(func(stop <-chan struct{}) { <-stop })
	defer cs.runner.Stop()

	before := time.Now().UnixMilli()
	cs.checkJobs()
	cs.mu.RLock()
	next1 := *cs.store.Jobs[0].State.NextRunAtMS
	cs.mu.RUnlock()
	delta1 := next1 - before
	if delta1 < 1800 || delta1 > 3500 {
		t.Fatalf("expected retry around 2s, got %dms", delta1)
	}

	cs.SetRuntimeOptions(RuntimeOptions{
		RunLoopMinSleep:              time.Second,
		RunLoopMaxSleep:              2 * time.Second,
		RetryBackoffBase:             5 * time.Second,
		RetryBackoffMax:              5 * time.Second,
		MaxConsecutiveFailureRetries: 10,
		MaxWorkers:                   1,
	})

	now2 := time.Now().UnixMilli()
	cs.mu.Lock()
	cs.store.Jobs[0].State.NextRunAtMS = &now2
	cs.mu.Unlock()

	before = time.Now().UnixMilli()
	cs.checkJobs()
	cs.mu.RLock()
	next2 := *cs.store.Jobs[0].State.NextRunAtMS
	cs.mu.RUnlock()
	delta2 := next2 - before
	if delta2 < 4800 || delta2 > 6500 {
		t.Fatalf("expected retry around 5s after hot update, got %dms", delta2)
	}
}

func TestSaveStore_IsAtomicAndValidJSON(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	cs := NewCronService(storePath, nil)

	at := time.Now().Add(10 * time.Minute).UnixMilli()
	_, err := cs.AddJob("atomic-write", CronSchedule{
		Kind: "at",
		AtMS: &at,
	}, "hello", false, "", "")
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	if _, err := os.Stat(storePath + ".tmp"); err == nil {
		t.Fatalf("unexpected temp file left behind")
	}

	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read store failed: %v", err)
	}
	var parsed CronStore
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid json store: %v", err)
	}
	if len(parsed.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(parsed.Jobs))
	}
}
