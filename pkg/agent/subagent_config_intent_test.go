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
		"agent_id: coder",
		"role: coding",
		"display_name: Code Agent",
		"system_prompt_file: agents/coder/AGENT.md",
		"routing_keywords: [code fix]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "<nil>") {
		t.Fatalf("did not expect nil placeholders, got:\n%s", out)
	}
}
