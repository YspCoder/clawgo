package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectPlanningFilesIncludesSpecDocs(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"spec.md":      "# spec\nscope",
		"tasks.md":     "# tasks\n- [ ] one",
		"checklist.md": "# checklist\n- [ ] verify",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	cb := &ContextBuilder{workspace: t.TempDir(), cwd: root}
	got := cb.LoadProjectPlanningFiles()
	if !strings.Contains(got, "spec.md") || !strings.Contains(got, "tasks.md") || !strings.Contains(got, "checklist.md") {
		t.Fatalf("expected project planning files in output, got:\n%s", got)
	}
}

func TestLoadProjectPlanningFilesTruncatesLargeDocs(t *testing.T) {
	root := t.TempDir()
	large := strings.Repeat("x", 4500)
	if err := os.WriteFile(filepath.Join(root, "spec.md"), []byte(large), 0644); err != nil {
		t.Fatalf("write spec.md: %v", err)
	}

	cb := &ContextBuilder{workspace: t.TempDir(), cwd: root}
	got := cb.LoadProjectPlanningFiles()
	if !strings.Contains(got, "[TRUNCATED]") {
		t.Fatalf("expected truncation marker, got:\n%s", got)
	}
}

func TestBuildMessagesOnlyLoadsProjectPlanningForCodingTasks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "spec.md"), []byte("# spec\ncoding plan"), 0644); err != nil {
		t.Fatalf("write spec.md: %v", err)
	}
	cb := NewContextBuilder(t.TempDir(), nil)
	cb.cwd = root

	coding := cb.BuildMessagesWithMemoryNamespace(nil, "", "请实现一个登录功能", nil, "cli", "direct", "", "main")
	if len(coding) == 0 || strings.Contains(coding[0].Content, "Active Project Planning") {
		t.Fatalf("did not expect small coding task to include project planning by default, got:\n%s", coding[0].Content)
	}

	heavyCoding := cb.BuildMessagesWithMemoryNamespace(nil, "", "请实现一个完整登录注册模块，涉及多文件改动并补测试", nil, "cli", "direct", "", "main")
	if len(heavyCoding) == 0 || !strings.Contains(heavyCoding[0].Content, "Active Project Planning") {
		t.Fatalf("expected substantial coding task to include project planning, got:\n%s", heavyCoding[0].Content)
	}

	explicitSpec := cb.BuildMessagesWithMemoryNamespace(nil, "", "这个改动不大，但请用 spec coding 流程来做", nil, "cli", "direct", "", "main")
	if len(explicitSpec) == 0 || !strings.Contains(explicitSpec[0].Content, "Active Project Planning") {
		t.Fatalf("expected explicit spec request to include project planning, got:\n%s", explicitSpec[0].Content)
	}

	nonCoding := cb.BuildMessagesWithMemoryNamespace(nil, "", "帮我总结一下今天的工作", nil, "cli", "direct", "", "main")
	if len(nonCoding) == 0 {
		t.Fatalf("expected system message")
	}
	if strings.Contains(nonCoding[0].Content, "Active Project Planning") {
		t.Fatalf("did not expect non-coding task to include project planning, got:\n%s", nonCoding[0].Content)
	}
}

func TestShouldUseSpecCodingRequiresExplicitAndNonTrivialCodingIntent(t *testing.T) {
	cb := &ContextBuilder{}
	cases := []struct {
		message string
		want    bool
	}{
		{message: "请实现一个完整支付模块，涉及多文件改动并补测试", want: true},
		{message: "修一下这个小 bug，顺手改一行就行", want: false},
		{message: "帮我总结这个接口问题", want: false},
		{message: "这个改动不大，但请用 spec coding 流程来做", want: true},
	}
	for _, tc := range cases {
		if got := cb.shouldUseSpecCoding(tc.message); got != tc.want {
			t.Fatalf("shouldUseSpecCoding(%q) = %v, want %v", tc.message, got, tc.want)
		}
	}
}
