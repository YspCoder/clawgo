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

func TestNewExecTool_LoadsAllowedCmdsIntoAllowPatterns(t *testing.T) {
	tool := NewExecTool(config.ShellConfig{AllowedCmds: []string{"echo"}}, ".")
	if len(tool.allowPatterns) != 1 {
		t.Fatalf("expected one allow pattern, got %d", len(tool.allowPatterns))
	}
}

func TestGuardCommand_BlocksCommandNotInAllowlist(t *testing.T) {
	tool := NewExecTool(config.ShellConfig{AllowedCmds: []string{"echo"}}, ".")
	if msg := tool.guardCommand("ls -la", "."); msg == "" {
		t.Fatal("expected allowlist to block command not in allowed_cmds")
	}

	if msg := tool.guardCommand("echo hi", "."); msg != "" {
		t.Fatalf("expected allowed command to pass guard, got %q", msg)
	}
}
