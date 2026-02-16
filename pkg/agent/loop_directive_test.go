package agent

import (
	"testing"
	"time"
)

func TestParseTaskExecutionDirectives_RunCommand(t *testing.T) {
	d := parseTaskExecutionDirectives("/run 修复构建脚本 --stage-report")
	if d.task != "修复构建脚本" {
		t.Fatalf("unexpected task: %q", d.task)
	}
	if !d.stageReport {
		t.Fatalf("expected stage report enabled")
	}
}

func TestParseTaskExecutionDirectives_Default(t *testing.T) {
	d := parseTaskExecutionDirectives("帮我看看今天的日志异常")
	if d.task != "帮我看看今天的日志异常" {
		t.Fatalf("unexpected task: %q", d.task)
	}
	if d.stageReport {
		t.Fatalf("expected stage report disabled")
	}
}

func TestParseAutoLearnInterval(t *testing.T) {
	d, err := parseAutoLearnInterval("5m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 5*time.Minute {
		t.Fatalf("unexpected duration: %s", d)
	}

	d, err = parseAutoLearnInterval("2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 2*time.Minute {
		t.Fatalf("unexpected duration: %s", d)
	}
}

func TestParseAutoLearnInterval_Invalid(t *testing.T) {
	if _, err := parseAutoLearnInterval("oops"); err == nil {
		t.Fatalf("expected error")
	}
}

func TestParseAutoLearnIntent_FallbackCommand(t *testing.T) {
	intent, ok := parseAutoLearnIntent("autolearn start 5m")
	if !ok {
		t.Fatalf("expected intent")
	}
	if intent.action != "start" {
		t.Fatalf("unexpected action: %s", intent.action)
	}
	if intent.interval == nil || *intent.interval != 5*time.Minute {
		t.Fatalf("unexpected interval: %v", intent.interval)
	}
}

func TestParseAutoLearnIntent_StopFallbackCommand(t *testing.T) {
	intent, ok := parseAutoLearnIntent("autolearn stop")
	if !ok {
		t.Fatalf("expected intent")
	}
	if intent.action != "stop" {
		t.Fatalf("unexpected action: %s", intent.action)
	}
}

func TestParseAutoLearnIntent_NoNaturalLanguageFallback(t *testing.T) {
	if _, ok := parseAutoLearnIntent("请开始自动学习"); ok {
		t.Fatalf("expected no fallback match")
	}
}

func TestParseAutonomyIntent_FallbackCommand(t *testing.T) {
	intent, ok := parseAutonomyIntent("autonomy start 15m log clustering")
	if !ok {
		t.Fatalf("expected intent")
	}
	if intent.action != "start" {
		t.Fatalf("unexpected action: %s", intent.action)
	}
	if intent.idleInterval == nil || *intent.idleInterval != 15*time.Minute {
		t.Fatalf("unexpected interval: %v", intent.idleInterval)
	}
	if intent.focus != "log clustering" {
		t.Fatalf("unexpected focus: %q", intent.focus)
	}
}

func TestParseAutonomyIntent_StopFallbackCommand(t *testing.T) {
	intent, ok := parseAutonomyIntent("autonomy stop")
	if !ok {
		t.Fatalf("expected intent")
	}
	if intent.action != "stop" {
		t.Fatalf("unexpected action: %s", intent.action)
	}
}

func TestParseAutonomyIntent_StatusFallbackCommand(t *testing.T) {
	intent, ok := parseAutonomyIntent("autonomy status")
	if !ok {
		t.Fatalf("expected intent")
	}
	if intent.action != "status" {
		t.Fatalf("unexpected action: %s", intent.action)
	}
}

func TestParseAutonomyIdleInterval(t *testing.T) {
	d, err := parseAutonomyIdleInterval("45m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 45*time.Minute {
		t.Fatalf("unexpected duration: %s", d)
	}
}

func TestParseAutonomyIntent_NoNaturalLanguageFallback(t *testing.T) {
	if intent, ok := parseAutonomyIntent("请自动执行这个任务"); ok {
		t.Fatalf("expected no intent, got: %+v", intent)
	}
}

func TestParseAutonomyIntent_ClearFocusFallbackCommand(t *testing.T) {
	intent, ok := parseAutonomyIntent("autonomy clear_focus")
	if !ok {
		t.Fatalf("expected intent")
	}
	if intent.action != "clear_focus" {
		t.Fatalf("unexpected action: %s", intent.action)
	}
}

func TestExtractJSONObject_FromCodeFence(t *testing.T) {
	raw := extractJSONObject("```json\n{\"action\":\"start\",\"confidence\":0.95}\n```")
	if raw != "{\"action\":\"start\",\"confidence\":0.95}" {
		t.Fatalf("unexpected json: %q", raw)
	}
}

func TestExtractJSONObject_Invalid(t *testing.T) {
	if raw := extractJSONObject("no json here"); raw != "" {
		t.Fatalf("expected empty json, got: %q", raw)
	}
}
