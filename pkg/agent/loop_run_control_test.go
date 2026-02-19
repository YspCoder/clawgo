package agent

import (
	"context"
	"testing"
	"time"

	"clawgo/pkg/bus"
)

func TestDetectRunControlIntent(t *testing.T) {
	t.Parallel()

	if got := normalizeRunWaitTimeout(0); got != defaultRunWaitTimeout {
		t.Fatalf("expected default timeout, got %s", got)
	}
}

func TestDetectRunControlIntentLatest(t *testing.T) {
	t.Parallel()

	if got := normalizeRunWaitTimeout(time.Second); got != minRunWaitTimeout {
		t.Fatalf("expected min timeout %s, got %s", minRunWaitTimeout, got)
	}
}

func TestParseRunWaitTimeout_MinClamp(t *testing.T) {
	t.Parallel()

	if got := normalizeRunWaitTimeout(maxRunWaitTimeout + time.Minute); got != maxRunWaitTimeout {
		t.Fatalf("expected max timeout %s, got %s", maxRunWaitTimeout, got)
	}
}

func TestParseRunWaitTimeout_MinuteUnit(t *testing.T) {
	t.Parallel()

	if got := normalizeRunWaitTimeout(2 * time.Minute); got != 2*time.Minute {
		t.Fatalf("expected 2m, got %s", got)
	}
}

func TestLatestRunStateBySession(t *testing.T) {
	t.Parallel()

	now := time.Now()
	al := &AgentLoop{
		runStates: map[string]*runState{
			"run-1-1": {
				runID:      "run-1-1",
				sessionKey: "s1",
				startedAt:  now.Add(-2 * time.Minute),
			},
			"run-1-2": {
				runID:      "run-1-2",
				sessionKey: "s1",
				startedAt:  now.Add(-time.Minute),
			},
			"run-2-1": {
				runID:      "run-2-1",
				sessionKey: "s2",
				startedAt:  now,
			},
		},
	}

	rs, ok := al.latestRunState("s1")
	if !ok {
		t.Fatalf("expected state for s1")
	}
	if rs.runID != "run-1-2" {
		t.Fatalf("unexpected run id: %s", rs.runID)
	}
}

func TestHandleSlashCommand_StatusRunLatest(t *testing.T) {
	t.Parallel()

	al := &AgentLoop{
		runStates: map[string]*runState{
			"run-100-1": {
				runID:      "run-100-1",
				sessionKey: "s1",
				status:     runStatusOK,
				acceptedAt: time.Now().Add(-time.Minute),
				startedAt:  time.Now().Add(-time.Minute),
				endedAt:    time.Now().Add(-30 * time.Second),
				done:       closedChan(),
			},
		},
	}
	handled, out, err := al.handleSlashCommand(context.Background(), bus.InboundMessage{
		Content:    "/status run latest",
		SessionKey: "s1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatalf("expected command handled")
	}
	if out == "" || !containsAnySubstring(out, "run-100-1", "Run ID: run-100-1") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestHandleSlashCommand_StatusWaitDoneRun(t *testing.T) {
	t.Parallel()

	al := &AgentLoop{
		runStates: map[string]*runState{
			"run-200-2": {
				runID:      "run-200-2",
				sessionKey: "s1",
				status:     runStatusOK,
				acceptedAt: time.Now().Add(-time.Minute),
				startedAt:  time.Now().Add(-time.Minute),
				endedAt:    time.Now().Add(-20 * time.Second),
				done:       closedChan(),
			},
		},
	}
	handled, out, err := al.handleSlashCommand(context.Background(), bus.InboundMessage{
		Content:    "/status wait run-200-2 3",
		SessionKey: "s1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatalf("expected command handled")
	}
	if out == "" || !containsAnySubstring(out, "run-200-2", "Run ID: run-200-2") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func closedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
