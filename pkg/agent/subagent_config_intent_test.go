package agent

import (
	"strings"
	"testing"
)

func TestFormatCreatedSubagentForUserReadsNestedFields(t *testing.T) {
	t.Parallel()

	out := formatCreatedSubagentForUser(map[string]interface{}{
		"agent_id": "coder",
		"subagent": map[string]interface{}{
			"role":               "coding",
			"display_name":       "Code Agent",
			"system_prompt_file": "agents/coder/AGENT.md",
			"tools": map[string]interface{}{
				"allowlist": []interface{}{"filesystem", "shell"},
			},
		},
		"rules": []interface{}{
			map[string]interface{}{
				"agent_id": "coder",
				"keywords": []interface{}{"code", "fix"},
			},
		},
	}, "/tmp/config.json")

	for _, want := range []string{
		"subagent 已写入 config.json。",
		"path: /tmp/config.json",
		"agent_id: coder",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{
		"role:",
		"display_name:",
		"tool_allowlist:",
		"routing_keywords:",
		"system_prompt_file:",
		"<nil>",
	} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("did not expect %q in output, got:\n%s", unwanted, out)
		}
	}
}
