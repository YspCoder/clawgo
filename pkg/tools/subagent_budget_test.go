package tools

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/providers"
)

func TestSubagentRunPreservesRemainingIterationBudgetAcrossRetry(t *testing.T) {
	t.Parallel()

	manager := NewSubagentManager(nil, t.TempDir(), nil)
	attempts := 0
	manager.SetRunFunc(func(ctx context.Context, run *SubagentRun) (string, error) {
		attempts++
		budget, ok := SubagentIterationBudget(ctx)
		if !ok {
			t.Fatal("expected subagent iteration budget in context")
		}
		if attempts == 1 {
			if budget != 3 {
				t.Fatalf("expected first attempt budget 3, got %d", budget)
			}
			RecordSubagentExecutionStats(ctx, SubagentExecutionStats{
				Iterations:  2,
				Attempts:    1,
				FailureCode: "stream_failed",
			})
			return "", providers.NewProviderExecutionError("stream_failed", "stream failed", "stream", true, "test")
		}
		if budget != 1 {
			t.Fatalf("expected retry to inherit remaining budget 1, got %d", budget)
		}
		RecordSubagentExecutionStats(ctx, SubagentExecutionStats{
			Iterations: 1,
			Attempts:   1,
		})
		return "done", nil
	})

	run, err := manager.SpawnRun(context.Background(), SubagentSpawnOptions{
		Task:              "finish task",
		AgentID:           "tester",
		MaxRetries:        1,
		RetryBackoff:      1,
		MaxToolIterations: 3,
	})
	if err != nil {
		t.Fatalf("spawn run: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	finalRun, _, err := manager.waitRun(waitCtx, run.ID)
	if err != nil {
		t.Fatalf("wait run: %v", err)
	}
	if finalRun.Status != RuntimeStatusCompleted {
		t.Fatalf("expected completed run, got %+v", finalRun)
	}
	if finalRun.IterationCount != 3 {
		t.Fatalf("expected 3 consumed iterations, got %d", finalRun.IterationCount)
	}
	if finalRun.AttemptCount != 2 {
		t.Fatalf("expected 2 attempts, got %d", finalRun.AttemptCount)
	}
	events, err := manager.Events(run.ID, 10)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected persisted events")
	}
	foundFailure := false
	for _, evt := range events {
		if evt.Type == "attempt_failed" && evt.FailureCode == "stream_failed" {
			foundFailure = true
		}
	}
	if !foundFailure {
		t.Fatalf("expected stream_failed event, got %#v", events)
	}
}

func TestSubagentRunStopsWhenIterationBudgetExhausted(t *testing.T) {
	t.Parallel()

	manager := NewSubagentManager(nil, t.TempDir(), nil)
	manager.SetRunFunc(func(ctx context.Context, run *SubagentRun) (string, error) {
		RecordSubagentExecutionStats(ctx, SubagentExecutionStats{Iterations: 2, Attempts: 1})
		return "", errors.New("max tool iterations exceeded")
	})

	run, err := manager.SpawnRun(context.Background(), SubagentSpawnOptions{
		Task:              "exhaust budget",
		AgentID:           "tester",
		MaxRetries:        2,
		RetryBackoff:      1,
		MaxToolIterations: 2,
	})
	if err != nil {
		t.Fatalf("spawn run: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	finalRun, _, err := manager.waitRun(waitCtx, run.ID)
	if err != nil {
		t.Fatalf("wait run: %v", err)
	}
	if finalRun.Status != RuntimeStatusFailed {
		t.Fatalf("expected failed run, got %+v", finalRun)
	}
	if finalRun.LastFailureCode != "retry_limit" {
		t.Fatalf("expected retry_limit failure code, got %q", finalRun.LastFailureCode)
	}
	if finalRun.IterationCount != 2 {
		t.Fatalf("expected consumed iterations to stay at 2, got %d", finalRun.IterationCount)
	}
}
