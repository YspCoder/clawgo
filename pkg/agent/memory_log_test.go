package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/runtimecfg"
)

func TestAppendDailySummaryLogUsesAgentNamespaceAndTitle(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Agents.Agents["coder"] = config.AgentConfig{
		Enabled:     true,
		DisplayName: "Code Agent",
		PromptFile:  "agents/coder/AGENT.md",
	}
	runtimecfg.Set(cfg)
	t.Cleanup(func() { runtimecfg.Set(config.DefaultConfig()) })

	loop := &AgentLoop{workspace: workspace}
	loop.appendDailySummaryLog(bus.InboundMessage{
		Channel:    "cli",
		SessionKey: "agent:coder:agent-1",
		Content:    "Role Profile Policy (agents/coder/AGENT.md):\n...\n\nTask:\n修复登录接口并补测试\nextra details",
		Metadata: map[string]string{
			"memory_ns": "coder",
		},
	}, "完成了登录接口修复、增加回归测试，并验证通过。")

	entries, err := os.ReadFile(filepath.Join(workspace, "agents", "coder", "memory", currentDateFileName()))
	if err != nil {
		t.Fatalf("read namespaced daily note failed: %v", err)
	}
	content := string(entries)
	if !strings.Contains(content, "Code Agent | 修复登录接口并补测试") {
		t.Fatalf("expected agent title + task summary, got %s", content)
	}
	if !strings.Contains(content, "- Did: 完成了登录接口修复、增加回归测试，并验证通过。") {
		t.Fatalf("expected did summary, got %s", content)
	}
	mainToday := filepath.Join(workspace, "memory", currentDateFileName())
	mainEntries, err := os.ReadFile(mainToday)
	if err != nil {
		t.Fatalf("expected main memory summary to be written, got %v", err)
	}
	mainContent := string(mainEntries)
	if !strings.Contains(mainContent, "Code Agent | 修复登录接口并补测试") {
		t.Fatalf("expected main memory to include agent title, got %s", mainContent)
	}
	if !strings.Contains(mainContent, "- Agent: Code Agent") {
		t.Fatalf("expected main memory to include agent name, got %s", mainContent)
	}
	if !strings.Contains(mainContent, "- Did: 完成了登录接口修复、增加回归测试，并验证通过。") {
		t.Fatalf("expected main memory to include summary, got %s", mainContent)
	}
}

func currentDateFileName() string {
	return time.Now().Format("2006-01-02") + ".md"
}
