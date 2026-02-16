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

func TestParseTaskExecutionDirectives_NaturalLanguage(t *testing.T) {
	d := parseTaskExecutionDirectives("你可以自动运行任务：整理日志，但是每到一个阶段给我报告一下任务完成情况")
	if d.task != "整理日志" {
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

func TestParseAutoLearnIntent_StartNaturalLanguage(t *testing.T) {
	intent, ok := parseAutoLearnIntent("请开始自动学习，每5分钟执行一轮")
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

func TestParseAutoLearnIntent_StopNaturalLanguage(t *testing.T) {
	intent, ok := parseAutoLearnIntent("先暂停自动学习")
	if !ok {
		t.Fatalf("expected intent")
	}
	if intent.action != "stop" {
		t.Fatalf("unexpected action: %s", intent.action)
	}
}

func TestParseAutoLearnIntent_StatusNaturalLanguage(t *testing.T) {
	intent, ok := parseAutoLearnIntent("帮我看下自动学习状态")
	if !ok {
		t.Fatalf("expected intent")
	}
	if intent.action != "status" {
		t.Fatalf("unexpected action: %s", intent.action)
	}
}

func TestParseAutonomyIntent_StartNaturalLanguage(t *testing.T) {
	intent, ok := parseAutonomyIntent("以后你自动拆解并自动执行任务，每15分钟主动找我汇报一次，研究方向是日志异常聚类")
	if !ok {
		t.Fatalf("expected intent")
	}
	if intent.action != "start" {
		t.Fatalf("unexpected action: %s", intent.action)
	}
	if intent.idleInterval == nil || *intent.idleInterval != 15*time.Minute {
		t.Fatalf("unexpected interval: %v", intent.idleInterval)
	}
	if intent.focus != "日志异常聚类" {
		t.Fatalf("unexpected focus: %q", intent.focus)
	}
}

func TestParseAutonomyIntent_StopNaturalLanguage(t *testing.T) {
	intent, ok := parseAutonomyIntent("先不要主动找我，关闭自主模式")
	if !ok {
		t.Fatalf("expected intent")
	}
	if intent.action != "stop" {
		t.Fatalf("unexpected action: %s", intent.action)
	}
}

func TestParseAutonomyIntent_StatusNaturalLanguage(t *testing.T) {
	intent, ok := parseAutonomyIntent("帮我看下自主模式状态")
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

func TestParseAutonomyIntent_NoFalsePositiveOnSingleTask(t *testing.T) {
	if intent, ok := parseAutonomyIntent("请自动执行这个任务"); ok {
		t.Fatalf("expected no intent, got: %+v", intent)
	}
}

func TestExtractAutonomyFocus_EmptyWhenNotProvided(t *testing.T) {
	focus := extractAutonomyFocus("开启自主模式，每30分钟主动汇报")
	if focus != "" {
		t.Fatalf("expected empty focus, got: %q", focus)
	}
}

func TestExtractAutonomyFocus_KeepInnerBing(t *testing.T) {
	focus := extractAutonomyFocus("开启自主模式，研究方向是日志聚类并关联异常根因，并且每30分钟主动汇报")
	if focus != "日志聚类并关联异常根因" {
		t.Fatalf("unexpected focus: %q", focus)
	}
}

func TestParseAutonomyIntent_ClearFocusNaturalLanguage(t *testing.T) {
	intent, ok := parseAutonomyIntent("自主附带的方向执行完成了，可以去执行别的")
	if !ok {
		t.Fatalf("expected intent")
	}
	if intent.action != "clear_focus" {
		t.Fatalf("unexpected action: %s", intent.action)
	}
}
