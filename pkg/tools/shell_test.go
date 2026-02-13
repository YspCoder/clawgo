package tools

import (
	"testing"

	"clawgo/pkg/config"
)

func TestApplyRiskGate_DryRunCanBeBypassedWithForce(t *testing.T) {
	tool := &ExecTool{riskCfg: config.RiskConfig{
		Enabled:          true,
		AllowDestructive: true,
		RequireDryRun:    true,
		RequireForceFlag: false,
	}}

	msg, dryRun := tool.applyRiskGate("git clean -fd", true)
	if msg != "" || dryRun != "" {
		t.Fatalf("expected force=true to allow execution after dry-run step, got msg=%q dryRun=%q", msg, dryRun)
	}
}

func TestApplyRiskGate_RequiresDryRunWithoutForce(t *testing.T) {
	tool := &ExecTool{riskCfg: config.RiskConfig{
		Enabled:          true,
		AllowDestructive: true,
		RequireDryRun:    true,
		RequireForceFlag: false,
	}}

	msg, dryRun := tool.applyRiskGate("git clean -fd", false)
	if msg == "" {
		t.Fatal("expected dry-run block message")
	}
	if dryRun == "" {
		t.Fatal("expected dry-run command")
	}
}

func TestAssessCommandRisk_GitCleanIsDestructive(t *testing.T) {
	assessment := assessCommandRisk("git clean -fd")
	if assessment.Level != RiskDestructive {
		t.Fatalf("expected git clean to be destructive, got %s", assessment.Level)
	}
}
