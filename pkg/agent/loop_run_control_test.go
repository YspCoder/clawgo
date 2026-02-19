package agent

import (
	"context"
	"testing"
	"time"

	"clawgo/pkg/bus"
)

func TestDetectRunControlIntent(t *testing.T) {
	t.Parallel()

	intent, ok := detectRunControlIntent("请等待 run-123-7 120 秒后告诉我状态")
	if !ok {
		t.Fatalf("expected run control intent")
	}
	if intent.runID != "run-123-7" {
		t.Fatalf("unexpected run id: %s", intent.runID)
	}
	if !intent.wait {
		t.Fatalf("expected wait=true")
	}
	if intent.timeout != 120*time.Second {
		t.Fatalf("unexpected timeout: %s", intent.timeout)
	}
}

func TestDetectRunControlIntentLatest(t *testing.T) {
	t.Parallel()

	intent, ok := detectRunControlIntent("latest run status")
	if !ok {
		t.Fatalf("expected latest run status intent")
	}
	if !intent.latest {
		t.Fatalf("expected latest=true")
	}
	if intent.runID != "" {
		t.Fatalf("expected empty run id")
	}
}

func TestParseRunWaitTimeout_MinClamp(t *testing.T) {
	t.Parallel()

	got := parseRunWaitTimeout("wait run-1-1 1 s")
	if got != minRunWaitTimeout {
		t.Fatalf("expected min timeout %s, got %s", minRunWaitTimeout, got)
	}
}

func TestParseRunWaitTimeout_MinuteUnit(t *testing.T) {
	t.Parallel()

	got := parseRunWaitTimeout("等待 run-1-1 2 分钟")
	if got != 2*time.Minute {
		t.Fatalf("expected 2m, got %s", got)
	}
}

func TestDetectRunControlIntentIgnoresNonControlText(t *testing.T) {
	t.Parallel()

	if _, ok := detectRunControlIntent("帮我写一个README"); ok {
		t.Fatalf("did not expect run control intent")
	}
}

func TestDetectRunControlIntentWithCustomLexicon(t *testing.T) {
	t.Parallel()

	lex := runControlLexicon{
		latestKeywords:     []string{"newest"},
		waitKeywords:       []string{"block"},
		statusKeywords:     []string{"health"},
		runMentionKeywords: []string{"job"},
		minuteUnits:        map[string]struct{}{"mins": {}},
	}

	intent, ok := detectRunControlIntentWithLexicon("block run-55-1 for 2 mins and show health", lex)
	if !ok {
		t.Fatalf("expected intent with custom lexicon")
	}
	if !intent.wait {
		t.Fatalf("expected wait=true")
	}
	if intent.timeout != 2*time.Minute {
		t.Fatalf("unexpected timeout: %s", intent.timeout)
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
