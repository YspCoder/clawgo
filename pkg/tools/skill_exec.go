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
		return "", err
	}

	relScript := filepath.Clean(script)
	if strings.Contains(relScript, "..") || filepath.IsAbs(relScript) {
		return "", fmt.Errorf("script must be relative path inside skill directory")
	}
	if !strings.HasPrefix(relScript, "scripts"+string(os.PathSeparator)) {
		return "", fmt.Errorf("script must be under scripts/ directory")
	}

	scriptPath := filepath.Join(skillDir, relScript)
	if _, err := os.Stat(scriptPath); err != nil {
		return "", fmt.Errorf("script not found: %s", scriptPath)
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
		return "", err
	}
	cmd.Dir = skillDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("skill execution failed: %w\n%s", err, string(output))
	}

	out := strings.TrimSpace(string(output))
	if out == "" {
		out = "(no output)"
	}
	return out, nil
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
