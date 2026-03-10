package agent

import (
	"context"
	"testing"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/providers"
)

func TestSplitPlannedSegmentsDoesNotSplitPlainNewlines(t *testing.T) {
	t.Parallel()

	content := "编写ai漫画创作平台demo\n让产品出方案，方案出完让前端后端开始编写，写完后交个测试过一下"
	got := splitPlannedSegments(content)
	if len(got) != 1 {
		t.Fatalf("expected 1 segment, got %d: %#v", len(got), got)
	}
}

func TestSplitPlannedSegmentsStillSplitsBullets(t *testing.T) {
	t.Parallel()

	content := "1. 先实现前端\n2. 再补测试"
	got := splitPlannedSegments(content)
	if len(got) != 2 {
		t.Fatalf("expected 2 segments, got %d: %#v", len(got), got)
	}
}

func TestSplitPlannedSegmentsStillSplitsSemicolons(t *testing.T) {
	t.Parallel()

	content := "先实现前端；再补测试"
	got := splitPlannedSegments(content)
	if len(got) != 2 {
		t.Fatalf("expected 2 segments, got %d: %#v", len(got), got)
	}
}

func TestPlanSessionTasksDisablePlanningMetadataSkipsBulletSplit(t *testing.T) {
	t.Parallel()

	al := &AgentLoop{}
	msg := bus.InboundMessage{
		Content: "Role Profile Policy:\n1. 先做代码审查\n2. 再执行测试\n\nTask:\n修复登录流程",
		Metadata: map[string]string{
			"disable_planning": "true",
		},
	}

	got := al.planSessionTasks(context.Background(), msg)
	if len(got) != 1 {
		t.Fatalf("expected 1 segment, got %d: %#v", len(got), got)
	}
	if got[0].Content != msg.Content {
		t.Fatalf("expected original content to be preserved, got: %#v", got[0].Content)
	}
}

func TestPlanSessionTasksUsesAIPlannerDecisionToAvoidOversplitting(t *testing.T) {
	t.Parallel()

	al := &AgentLoop{
		provider: plannerStubProvider{content: `{"should_split":false,"tasks":[]}`},
	}
	msg := bus.InboundMessage{
		Content: "1. 调研\n2. 写方案\n3. 设计数据库\n4. 写接口\n5. 写前端\n6. 写测试",
	}

	got := al.planSessionTasks(context.Background(), msg)
	if len(got) != 1 {
		t.Fatalf("expected AI planner to keep one task, got %d: %#v", len(got), got)
	}
	if got[0].Content != msg.Content {
		t.Fatalf("expected original content, got %#v", got[0].Content)
	}
}

func TestPlanSessionTasksUsesAIPlannerDecisionToSplitIntoHighLevelGroups(t *testing.T) {
	t.Parallel()

	al := &AgentLoop{
		provider: plannerStubProvider{content: `{"should_split":true,"tasks":["产品方案","研发实现","测试验收"]}`},
	}
	msg := bus.InboundMessage{
		Content: "1. 调研\n2. 写方案\n3. 设计数据库\n4. 写接口\n5. 写前端\n6. 写测试",
	}

	got := al.planSessionTasks(context.Background(), msg)
	if len(got) != 3 {
		t.Fatalf("expected 3 planned tasks, got %d: %#v", len(got), got)
	}
	if got[0].Content != "产品方案" || got[1].Content != "研发实现" || got[2].Content != "测试验收" {
		t.Fatalf("unexpected planned task contents: %#v", got)
	}
}

type plannerStubProvider struct {
	content string
	err     error
}

func (p plannerStubProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &providers.LLMResponse{Content: p.content}, nil
}

func (p plannerStubProvider) GetDefaultModel() string { return "stub" }
