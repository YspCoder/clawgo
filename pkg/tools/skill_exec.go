package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"clawgo/pkg/config"
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
				"default":     60,
				"description": "Execution timeout in seconds",
			},
		},
		"required": []string{"skill", "script"},
	}
}

func (t *SkillExecTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	skill, _ := args["skill"].(string)
	script, _ := args["script"].(string)
	if strings.TrimSpace(skill) == "" || strings.TrimSpace(script) == "" {
		return "", fmt.Errorf("skill and script are required")
	}

	timeoutSec := 60
	if raw, ok := args["timeout_sec"].(float64); ok && raw > 0 {
		timeoutSec = int(raw)
	}

	skillDir, err := t.resolveSkillDir(skill)
	if err != nil {
		t.writeAudit(skill, script, false, err.Error())
		return "", err
	}
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		err = fmt.Errorf("SKILL.md missing for skill: %s", skill)
		t.writeAudit(skill, script, false, err.Error())
		return "", err
	}

	relScript := filepath.Clean(script)
	if strings.Contains(relScript, "..") || filepath.IsAbs(relScript) {
		err := fmt.Errorf("script must be relative path inside skill directory")
		t.writeAudit(skill, script, false, err.Error())
		return "", err
	}
	if !strings.HasPrefix(relScript, "scripts"+string(os.PathSeparator)) {
		err := fmt.Errorf("script must be under scripts/ directory")
		t.writeAudit(skill, script, false, err.Error())
		return "", err
	}

	scriptPath := filepath.Join(skillDir, relScript)
	if _, err := os.Stat(scriptPath); err != nil {
		err = fmt.Errorf("script not found: %s", scriptPath)
		t.writeAudit(skill, script, false, err.Error())
		return "", err
	}

	cmdArgs := []string{}
	if rawArgs, ok := args["args"].([]interface{}); ok {
		for _, item := range rawArgs {
			if s, ok := item.(string); ok {
				cmdArgs = append(cmdArgs, s)
			}
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd, err := buildSkillCommand(runCtx, scriptPath, cmdArgs)
	if err != nil {
		t.writeAudit(skill, script, false, err.Error())
		return "", err
	}
	cmd.Dir = skillDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.writeAudit(skill, script, false, err.Error())
		return "", fmt.Errorf("skill execution failed: %w\n%s", err, string(output))
	}

	out := strings.TrimSpace(string(output))
	if out == "" {
		out = "(no output)"
	}
	t.writeAudit(skill, script, true, "")
	return out, nil
}

func (t *SkillExecTool) writeAudit(skill, script string, ok bool, errText string) {
	if strings.TrimSpace(t.workspace) == "" {
		return
	}
	memDir := filepath.Join(t.workspace, "memory")
	_ = os.MkdirAll(memDir, 0755)
	row := fmt.Sprintf("{\"time\":%q,\"skill\":%q,\"script\":%q,\"ok\":%t,\"error\":%q}\n",
		time.Now().UTC().Format(time.RFC3339),
		strings.TrimSpace(skill),
		strings.TrimSpace(script),
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
