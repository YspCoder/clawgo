package tools

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunStringTaskWithTaskWatchdogTimesOutWithoutExtension(t *testing.T) {
	t.Parallel()

	started := time.Now()
	_, err := runStringTaskWithTaskWatchdog(
		context.Background(),
		1,
		100*time.Millisecond,
		stringTaskWatchdogOptions{},
		func(ctx context.Context) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	)
	if !errors.Is(err, ErrTaskWatchdogTimeout) {
		t.Fatalf("expected ErrTaskWatchdogTimeout, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("expected watchdog timeout quickly, took %v", elapsed)
	}
}

func TestRunStringTaskWithTaskWatchdogAutoExtendsWhileRunning(t *testing.T) {
	t.Parallel()

	started := time.Now()
	out, err := runStringTaskWithTaskWatchdog(
		context.Background(),
		1,
		100*time.Millisecond,
		stringTaskWatchdogOptions{
			CanExtend: func() bool { return true },
		},
		func(ctx context.Context) (string, error) {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(1500 * time.Millisecond):
				return "ok", nil
			}
		},
	)
	if err != nil {
		t.Fatalf("expected auto-extended task to finish, got %v", err)
	}
	if out != "ok" {
		t.Fatalf("expected output ok, got %q", out)
	}
	if elapsed := time.Since(started); elapsed < time.Second {
		t.Fatalf("expected task to run past initial timeout window, took %v", elapsed)
	}
}

func TestRunStringTaskWithTaskWatchdogExtendsOnProgress(t *testing.T) {
	t.Parallel()

	var progress atomic.Int64
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				progress.Add(1)
			}
		}
	}()
	defer close(done)

	out, err := runStringTaskWithTaskWatchdog(
		context.Background(),
		1,
		100*time.Millisecond,
		stringTaskWatchdogOptions{
			ProgressFn: func() int { return int(progress.Load()) },
		},
		func(ctx context.Context) (string, error) {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(1500 * time.Millisecond):
				return "done", nil
			}
		},
	)
	if err != nil {
		t.Fatalf("expected progress-based extension to finish, got %v", err)
	}
	if out != "done" {
		t.Fatalf("expected output done, got %q", out)
	}
}
