package cron

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestCronService(t *testing.T, onJob JobHandler) *CronService {
	t.Helper()
	return NewCronService(filepath.Join(t.TempDir(), "cron.json"), onJob)
}

func TestAddListEnableAndUpdateJobPersistsStore(t *testing.T) {
	cs := newTestCronService(t, nil)
	at := time.Now().Add(time.Hour).UnixMilli()

	job, err := cs.AddJob("daily note", CronSchedule{Kind: "at", AtMS: &at}, "hello", true, "telegram", "chat-1")
	if err != nil {
		t.Fatalf("AddJob returned error: %v", err)
	}

	data, err := os.ReadFile(cs.storePath)
	if err != nil {
		t.Fatalf("store was not written: %v", err)
	}
	var stored CronStore
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("store JSON did not decode: %v", err)
	}
	if len(stored.Jobs) != 1 || stored.Jobs[0].ID != job.ID {
		t.Fatalf("stored jobs = %+v, want one persisted job %q", stored.Jobs, job.ID)
	}

	if got := cs.ListJobs(false); len(got) != 1 {
		t.Fatalf("ListJobs(false) returned %d jobs, want 1", len(got))
	}
	if disabled := cs.EnableJob(job.ID, false); disabled == nil || disabled.Enabled {
		t.Fatalf("EnableJob(false) = %+v, want disabled job", disabled)
	}
	if got := cs.ListJobs(false); len(got) != 0 {
		t.Fatalf("ListJobs(false) returned disabled jobs: %+v", got)
	}
	if got := cs.ListJobs(true); len(got) != 1 {
		t.Fatalf("ListJobs(true) returned %d jobs, want 1", len(got))
	}

	nextAt := time.Now().Add(2 * time.Hour).UnixMilli()
	name := "updated"
	msg := "new message"
	deliver := false
	channel := "feishu"
	to := "chat-2"
	deleteAfterRun := true
	updated, err := cs.UpdateJob(job.ID, UpdateJobInput{
		Name:           &name,
		Enabled:        boolPtr(true),
		Schedule:       &CronSchedule{Kind: "at", AtMS: &nextAt},
		Message:        &msg,
		Deliver:        &deliver,
		Channel:        &channel,
		To:             &to,
		DeleteAfterRun: &deleteAfterRun,
	})
	if err != nil {
		t.Fatalf("UpdateJob returned error: %v", err)
	}
	if updated.Name != name || updated.Payload.Message != msg || updated.Payload.Deliver != deliver ||
		updated.Payload.Channel != channel || updated.Payload.To != to || !updated.DeleteAfterRun || !updated.Enabled {
		t.Fatalf("updated job = %+v, want all fields updated", updated)
	}
	if updated.State.NextRunAtMS == nil || *updated.State.NextRunAtMS != nextAt {
		t.Fatalf("updated next run = %v, want %d", updated.State.NextRunAtMS, nextAt)
	}
}

func TestComputeNextRunForCronExpression(t *testing.T) {
	cs := newTestCronService(t, nil)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC).UnixMilli()
	now := time.Date(2026, 1, 1, 12, 2, 0, 0, time.UTC).UnixMilli()

	next := cs.computeNextRunAfter(&CronSchedule{Kind: "cron", Expr: "*/5 * * * *", TZ: "UTC"}, base, now)
	if next == nil {
		t.Fatal("computeNextRunAfter returned nil")
	}
	want := time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC).UnixMilli()
	if *next != want {
		t.Fatalf("next run = %s, want %s", time.UnixMilli(*next), time.UnixMilli(want))
	}
}

func TestFailureBackoffTracksConsecutiveFailures(t *testing.T) {
	cs := newTestCronService(t, func(job *CronJob) (string, error) {
		return "", errors.New("boom")
	})
	cs.SetRuntimeOptions(RuntimeOptions{
		RetryBackoffBase:             10 * time.Millisecond,
		RetryBackoffMax:              40 * time.Millisecond,
		MaxConsecutiveFailureRetries: 2,
		MaxWorkers:                   1,
	})

	now := time.Now().UnixMilli()
	next := now - 100
	cs.store.Jobs = []CronJob{{
		ID:      "job-1",
		Enabled: true,
		Schedule: CronSchedule{
			Kind: "cron",
			Expr: "@every 1h",
		},
		State: CronJobState{NextRunAtMS: &next},
	}}

	if !cs.executeJobByID("job-1") {
		t.Fatal("first executeJobByID returned false")
	}
	job := cs.GetJob("job-1")
	if job.State.LastStatus != "error" || job.State.TotalRuns != 1 || job.State.TotalFailures != 1 || job.State.ConsecutiveFailures != 1 {
		t.Fatalf("first failure state = %+v", job.State)
	}
	if job.State.NextRunAtMS == nil || *job.State.NextRunAtMS <= time.Now().UnixMilli() || *job.State.NextRunAtMS > time.Now().Add(time.Second).UnixMilli() {
		t.Fatalf("first retry next run = %v, want near-future backoff", job.State.NextRunAtMS)
	}

	cs.store.Jobs[0].State.NextRunAtMS = int64Ptr(time.Now().UnixMilli() - 100)
	if !cs.executeJobByID("job-1") {
		t.Fatal("second executeJobByID returned false")
	}
	job = cs.GetJob("job-1")
	if job.State.ConsecutiveFailures != 2 || job.State.TotalFailures != 2 {
		t.Fatalf("second failure state = %+v", job.State)
	}
}

func TestMaxWorkersLimitsConcurrentExecutions(t *testing.T) {
	var active int64
	var maxActive int64
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	cs := newTestCronService(t, func(job *CronJob) (string, error) {
		cur := atomic.AddInt64(&active, 1)
		for {
			old := atomic.LoadInt64(&maxActive)
			if cur <= old || atomic.CompareAndSwapInt64(&maxActive, old, cur) {
				break
			}
		}
		started <- struct{}{}
		<-release
		atomic.AddInt64(&active, -1)
		return "", nil
	})
	cs.SetRuntimeOptions(RuntimeOptions{MaxWorkers: 2, RunLoopMinSleep: time.Millisecond, RunLoopMaxSleep: time.Millisecond})

	due := time.Now().UnixMilli() - 100
	for i := 0; i < 3; i++ {
		cs.store.Jobs = append(cs.store.Jobs, CronJob{
			ID:      string(rune('a' + i)),
			Enabled: true,
			Schedule: CronSchedule{
				Kind: "cron",
				Expr: "@every 1h",
			},
			State: CronJobState{NextRunAtMS: &due},
		})
	}

	var wg sync.WaitGroup
	wg.Add(1)
	cs.runner.Start(func(stop <-chan struct{}) {
		defer wg.Done()
		cs.checkJobs()
	})

	<-started
	<-started
	select {
	case <-started:
		t.Fatal("third job started before a worker slot was released")
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	cs.Stop()

	if got := atomic.LoadInt64(&maxActive); got > 2 {
		t.Fatalf("max concurrent executions = %d, want <= 2", got)
	}
}

func TestDeleteAfterRunRemovesOneTimeJob(t *testing.T) {
	cs := newTestCronService(t, nil)
	due := time.Now().UnixMilli() - 100
	cs.store.Jobs = []CronJob{{
		ID:             "once",
		Enabled:        true,
		Schedule:       CronSchedule{Kind: "at", AtMS: &due},
		State:          CronJobState{NextRunAtMS: &due},
		DeleteAfterRun: true,
	}}

	if !cs.executeJobByID("once") {
		t.Fatal("executeJobByID returned false")
	}
	if got := cs.GetJob("once"); got != nil {
		t.Fatalf("GetJob returned %+v, want deleted job", got)
	}
}

func TestLoadStoreHandlesMissingAndCorruptFiles(t *testing.T) {
	cs := newTestCronService(t, nil)
	if err := cs.Load(); err != nil {
		t.Fatalf("Load missing store returned error: %v", err)
	}
	if len(cs.ListJobs(true)) != 0 {
		t.Fatalf("missing store loaded jobs: %+v", cs.ListJobs(true))
	}

	if err := os.WriteFile(cs.storePath, []byte("{not-json"), 0644); err != nil {
		t.Fatalf("write corrupt store: %v", err)
	}
	if err := cs.Load(); err == nil {
		t.Fatal("Load corrupt store returned nil error")
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func int64Ptr(v int64) *int64 {
	return &v
}
