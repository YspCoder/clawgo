package agent

import (
	"strings"
	"testing"
)

func TestSummarizePlannedTaskProgressBodyPreservesUsefulLines(t *testing.T) {
	t.Parallel()

	body := "subagent 已写入 config.json。\npath: /root/.clawgo/config.json\nagent_id: tester\nrole: testing\ndisplay_name: Test Agent\ntool_allowlist: [filesystem shell]\nrouting_keywords: [test qa]\nsystem_prompt_file: agents/tester/AGENT.md"
	out := summarizePlannedTaskProgressBody(body, 6, 320)

	if !strings.Contains(out, "subagent 已写入 config.json。") {
		t.Fatalf("expected title line, got:\n%s", out)
	}
	if !strings.Contains(out, "agent_id: tester") {
		t.Fatalf("expected agent id line, got:\n%s", out)
	}
	if strings.Contains(out, "subagent 已写入 config.json。 path:") {
		t.Fatalf("expected multi-line formatting, got:\n%s", out)
	}
}
