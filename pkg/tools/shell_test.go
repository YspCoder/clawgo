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

func TestGuardCommand_AllowlistIsCaseInsensitive(t *testing.T) {
	tool := NewExecTool(config.ShellConfig{AllowedCmds: []string{"ECHO"}}, ".")
	if msg := tool.guardCommand("echo hi", "."); msg != "" {
		t.Fatalf("expected case-insensitive allowlist match, got %q", msg)
	}
}

func TestGuardCommand_DenylistIsCaseInsensitive(t *testing.T) {
	tool := NewExecTool(config.ShellConfig{DeniedCmds: []string{"RM"}}, ".")
	if msg := tool.guardCommand("rm -f tmp.txt", "."); msg == "" {
		t.Fatal("expected case-insensitive denylist match to block command")
	}
}

func TestApplyRiskGate_RequireDryRunWithoutStrategyStillBlocks(t *testing.T) {
	tool := &ExecTool{riskCfg: config.RiskConfig{
		Enabled:          true,
		AllowDestructive: true,
		RequireDryRun:    true,
		RequireForceFlag: false,
	}}

	msg, dryRun := tool.applyRiskGate("rm -rf tmp", false)
	if msg == "" {
		t.Fatal("expected destructive command without dry-run strategy to be blocked")
	}
	if dryRun != "" {
		t.Fatalf("expected no dry-run command for rm -rf, got %q", dryRun)
	}
}

func TestSetAllowPatterns_IsCaseInsensitive(t *testing.T) {
	tool := &ExecTool{}
	if err := tool.SetAllowPatterns([]string{`^ECHO\b`}); err != nil {
		t.Fatalf("SetAllowPatterns returned error: %v", err)
	}

	if msg := tool.guardCommand("echo hi", "."); msg != "" {
		t.Fatalf("expected case-insensitive allow pattern to match, got %q", msg)
	}
}

func TestGuardCommand_BlocksRootWipeVariants(t *testing.T) {
	tool := &ExecTool{}
	cases := []string{
		"rm -rf /",
		"rm -fr /",
		"rm --no-preserve-root -rf /",
	}
	for _, c := range cases {
		if msg := tool.guardCommand(c, "."); msg == "" {
			t.Fatalf("expected root wipe variant to be blocked: %s", c)
		}
	}
}
