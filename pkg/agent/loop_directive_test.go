package agent

import (
	"testing"

	"clawgo/pkg/bus"
)

func TestParseTaskExecutionDirectives_RunCommand(t *testing.T) {
	d := parseTaskExecutionDirectives("/run fix build script --stage-report")
	if d.task != "fix build script" {
		t.Fatalf("unexpected task: %q", d.task)
	}
	if !d.stageReport {
		t.Fatalf("expected stage report enabled")
	}
}

func TestParseTaskExecutionDirectives_Default(t *testing.T) {
	d := parseTaskExecutionDirectives("Please check today's log anomalies")
	if d.task != "Please check today's log anomalies" {
		t.Fatalf("unexpected task: %q", d.task)
	}
	if d.stageReport {
		t.Fatalf("expected stage report disabled")
	}
}

func TestClassifyConfirmationReply(t *testing.T) {
	if ok, confident := classifyConfirmationReply("yes"); !confident || !ok {
		t.Fatalf("expected yes to confirm")
	}
	if ok, confident := classifyConfirmationReply("取消"); !confident || ok {
		t.Fatalf("expected cancel to reject")
	}
	if _, confident := classifyConfirmationReply("继续处理日志问题，不是这个"); confident {
		t.Fatalf("expected non-confirmation sentence to be non-confident")
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

func TestShouldHandleControlIntents_UserMessage(t *testing.T) {
	msg := bus.InboundMessage{
		SenderID: "user",
		Content:  "please enter autonomy mode",
	}
	if !shouldHandleControlIntents(msg) {
		t.Fatalf("expected user message to be control-eligible")
	}
}

func TestShouldHandleControlIntents_AutonomySyntheticSender(t *testing.T) {
	msg := bus.InboundMessage{
		SenderID: "autonomy",
		Content:  "autonomy round 1",
	}
	if shouldHandleControlIntents(msg) {
		t.Fatalf("expected autonomy synthetic message to be ignored for control intents")
	}
}

func TestShouldHandleControlIntents_AutoLearnSyntheticMetadata(t *testing.T) {
	msg := bus.InboundMessage{
		SenderID: "gateway",
		Content:  "auto-learn round 1",
		Metadata: map[string]string{
			"source": "autolearn",
		},
	}
	if shouldHandleControlIntents(msg) {
		t.Fatalf("expected autolearn synthetic metadata message to be ignored for control intents")
	}
}

func TestShouldPublishSyntheticResponse_AutonomyReportDue(t *testing.T) {
	msg := bus.InboundMessage{
		SenderID: "autonomy",
		Metadata: map[string]string{
			"source":     "autonomy",
			"report_due": "true",
		},
	}
	if !shouldPublishSyntheticResponse(msg) {
		t.Fatalf("expected autonomy report_due message to be published")
	}
}

func TestShouldPublishSyntheticResponse_AutonomySilentRound(t *testing.T) {
	msg := bus.InboundMessage{
		SenderID: "autonomy",
		Metadata: map[string]string{
			"source":     "autonomy",
			"report_due": "false",
		},
	}
	if shouldPublishSyntheticResponse(msg) {
		t.Fatalf("expected autonomy non-report round to be silent")
	}
}
