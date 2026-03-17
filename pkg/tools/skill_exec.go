package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/YspCoder/clawgo/pkg/config"
)

type SkillExecTool struct {
	workspace string
}

func NewSkillExecTool(workspace string) *SkillExecTool {
	return &SkillExecTool{workspace: workspace}
}

func (t *SkillExecTool) Name() string { return "skill_exec" }

func (t *SkillExecTool) Description() string {
	return "Execute an atomic skill script from skills/<name>/scripts/*. Keeps core agent lean by delegating complex logic to scripts."
}

func (t *SkillExecTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"skill": map[string]interface{}{
				"type":        "string",
				"description": "Skill name under workspace/global skills",
			},
			"script": map[string]interface{}{
				"type":        "string",
				"description": "Script path relative to skill root, usually scripts/*.sh|*.py",
			},
			"args": map[string]interface{}{
				"type":        "array",
				"description": "String arguments",
				"items":       map[string]interface{}{"type": "string"},
			},
			"timeout_sec": map[string]interface{}{
				"type":        "integer",
				"default":     0,
				"description": "Deprecated. No hard timeout is enforced.",
			},
			"reason": map[string]interface{}{
				"type":        "string",
				"description": "Why this skill/script was selected for the current user request",
			},
		},
		"required": []string{"skill", "script"},
	}
}

func (t *SkillExecTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	skill := MapStringArg(args, "skill")
	script := MapStringArg(args, "script")
	reason := MapStringArg(args, "reason")
	callerAgent := MapStringArg(args, "caller_agent")
	callerScope := MapStringArg(args, "caller_scope")
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unspecified"
	}
	callerAgent = strings.TrimSpace(callerAgent)
	if callerAgent == "" {
		callerAgent = "main"
	}
	callerScope = strings.TrimSpace(callerScope)
	if callerScope == "" {
		callerScope = "main_agent"
	}
	if strings.TrimSpace(skill) == "" || strings.TrimSpace(script) == "" {
		err := fmt.Errorf("skill and script are required")
		t.writeAudit(skill, script, reason, callerAgent, callerScope, false, err.Error())
		return "", err
	}
	skillDir, err := t.resolveSkillDir(skill)
	if err != nil {
		t.writeAudit(skill, script, reason, callerAgent, callerScope, false, err.Error())
		return "", err
	}
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		err = fmt.Errorf("SKILL.md missing for skill: %s", skill)
		t.writeAudit(skill, script, reason, callerAgent, callerScope, false, err.Error())
		return "", err
	}

	relScript := filepath.Clean(script)
	if strings.Contains(relScript, "..") || filepath.IsAbs(relScript) {
		err := fmt.Errorf("script must be relative path inside skill directory")
		t.writeAudit(skill, script, reason, callerAgent, callerScope, false, err.Error())
		return "", err
	}
	if !strings.HasPrefix(relScript, "scripts"+string(os.PathSeparator)) {
		err := fmt.Errorf("script must be under scripts/ directory")
		t.writeAudit(skill, script, reason, callerAgent, callerScope, false, err.Error())
		return "", err
	}

	scriptPath := filepath.Join(skillDir, relScript)
	if _, err := os.Stat(scriptPath); err != nil {
		err = fmt.Errorf("script not found: %s", scriptPath)
		t.writeAudit(skill, script, reason, callerAgent, callerScope, false, err.Error())
		return "", err
	}

	cmdArgs := MapStringListArg(args, "args")

	commandLabel := relScript
	if len(cmdArgs) > 0 {
		commandLabel += " " + strings.Join(cmdArgs, " ")
	}
	cmd, err := buildSkillCommand(ctx, scriptPath, cmdArgs)
	if err != nil {
		t.writeAudit(skill, script, reason, callerAgent, callerScope, false, err.Error())
		return "", err
	}
	cmd.Dir = skillDir
	var stdout, stderr trackedOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\nSTDERR:\n" + stderr.String()
	}
	if runErr != nil {
		t.writeAudit(skill, script, reason, callerAgent, callerScope, false, runErr.Error())
		if strings.TrimSpace(output) != "" {
			return "", fmt.Errorf("skill execution failed: %w\n%s", runErr, output)
		}
		return "", fmt.Errorf("skill execution failed: %w", runErr)
	}

	out := strings.TrimSpace(output)
	if out == "" {
		out = "(no output)"
	}
	t.writeAudit(skill, script, reason, callerAgent, callerScope, true, "")
	return out, nil
}

func (t *SkillExecTool) writeAudit(skill, script, reason, callerAgent, callerScope string, ok bool, errText string) {
	if strings.TrimSpace(t.workspace) == "" {
		return
	}
	memDir := filepath.Join(t.workspace, "memory")
	_ = os.MkdirAll(memDir, 0755)
	row := fmt.Sprintf("{\"time\":%q,\"skill\":%q,\"script\":%q,\"reason\":%q,\"caller_agent\":%q,\"caller_scope\":%q,\"ok\":%t,\"error\":%q}\n",
		time.Now().UTC().Format(time.RFC3339),
		strings.TrimSpace(skill),
		strings.TrimSpace(script),
		strings.TrimSpace(reason),
		strings.TrimSpace(callerAgent),
		strings.TrimSpace(callerScope),
		ok,
		strings.TrimSpace(errText),
	)
	f, err := os.OpenFile(filepath.Join(memDir, "skill-audit.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(row)
}

func (t *SkillExecTool) resolveSkillDir(skill string) (string, error) {
	candidates := []string{
		filepath.Join(t.workspace, "skills", skill),
		filepath.Join(config.GetConfigDir(), "skills", skill),
	}
	for _, dir := range candidates {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir, nil
		}
	}
	return "", fmt.Errorf("skill not found: %s", skill)
}

func buildSkillCommand(ctx context.Context, scriptPath string, args []string) (*exec.Cmd, error) {
	ext := strings.ToLower(filepath.Ext(scriptPath))
	switch ext {
	case ".sh":
		return exec.CommandContext(ctx, "bash", append([]string{scriptPath}, args...)...), nil
	case ".py":
		return exec.CommandContext(ctx, "python3", append([]string{scriptPath}, args...)...), nil
	default:
		return nil, fmt.Errorf("unsupported script extension: %s", ext)
	}
}
