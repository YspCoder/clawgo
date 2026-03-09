package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureSpecCodingDocsCreatesMissingFilesInProjectRoot(t *testing.T) {
	workspace := t.TempDir()
	projectRoot := t.TempDir()
	templatesDir := filepath.Join(workspace, "skills", "spec-coding", "templates")
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	for name, body := range map[string]string{
		"spec.md":      "# spec template",
		"tasks.md":     "# tasks template",
		"checklist.md": "# checklist template",
	} {
		if err := os.WriteFile(filepath.Join(templatesDir, name), []byte(body), 0644); err != nil {
			t.Fatalf("write template %s: %v", name, err)
		}
	}

	created, err := ensureSpecCodingDocs(workspace, projectRoot)
	if err != nil {
		t.Fatalf("ensureSpecCodingDocs failed: %v", err)
	}
	if len(created) != 3 {
		t.Fatalf("expected 3 created files, got %d: %#v", len(created), created)
	}
	for _, name := range []string{"spec.md", "tasks.md", "checklist.md"} {
		if _, err := os.Stat(filepath.Join(projectRoot, name)); err != nil {
			t.Fatalf("expected %s to be created: %v", name, err)
		}
	}
}

func TestEnsureSpecCodingDocsDoesNotOverwriteExistingFiles(t *testing.T) {
	workspace := t.TempDir()
	projectRoot := t.TempDir()
	templatesDir := filepath.Join(workspace, "skills", "spec-coding", "templates")
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	for _, name := range []string{"spec.md", "tasks.md", "checklist.md"} {
		if err := os.WriteFile(filepath.Join(templatesDir, name), []byte("template"), 0644); err != nil {
			t.Fatalf("write template %s: %v", name, err)
		}
	}
	existingPath := filepath.Join(projectRoot, "spec.md")
	if err := os.WriteFile(existingPath, []byte("custom spec"), 0644); err != nil {
		t.Fatalf("write existing spec: %v", err)
	}

	created, err := ensureSpecCodingDocs(workspace, projectRoot)
	if err != nil {
		t.Fatalf("ensureSpecCodingDocs failed: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("expected 2 created files, got %d: %#v", len(created), created)
	}
	data, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("read existing spec: %v", err)
	}
	if string(data) != "custom spec" {
		t.Fatalf("expected existing spec to be preserved, got %q", string(data))
	}
}

func TestSpecCodingTaskProgressUpdatesTasksFile(t *testing.T) {
	projectRoot := t.TempDir()
	tasksPath := filepath.Join(projectRoot, "tasks.md")
	initial := "# Task Breakdown (tasks.md)\n\n## Workstreams\n\n### 1.\n- [ ] base\n\n## Progress Notes\n"
	if err := os.WriteFile(tasksPath, []byte(initial), 0644); err != nil {
		t.Fatalf("write tasks.md: %v", err)
	}

	taskSummary, err := upsertSpecCodingTask(projectRoot, "请实现登录功能并补测试")
	if err != nil {
		t.Fatalf("upsertSpecCodingTask failed: %v", err)
	}
	if taskSummary.Summary == "" || taskSummary.ID == "" {
		t.Fatalf("expected task summary")
	}
	if err := completeSpecCodingTask(projectRoot, taskSummary, "登录功能已完成，测试已补齐"); err != nil {
		t.Fatalf("completeSpecCodingTask failed: %v", err)
	}

	data, err := os.ReadFile(tasksPath)
	if err != nil {
		t.Fatalf("read tasks.md: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, renderSpecCodingTaskLine(true, taskSummary)) {
		t.Fatalf("expected task to be checked, got:\n%s", text)
	}
	if !strings.Contains(text, "登录功能已完成") {
		t.Fatalf("expected progress note summary, got:\n%s", text)
	}
}

func TestSpecCodingTaskReopensCompletedTaskWhenIssueReturns(t *testing.T) {
	projectRoot := t.TempDir()
	tasksPath := filepath.Join(projectRoot, "tasks.md")
	taskSummary := "请实现登录功能并补测试"
	initial := "# Task Breakdown (tasks.md)\n\n## Current Coding Tasks\n- [x] " + taskSummary + "\n\n## Progress Notes\n"
	if err := os.WriteFile(tasksPath, []byte(initial), 0644); err != nil {
		t.Fatalf("write tasks.md: %v", err)
	}

	got, err := upsertSpecCodingTask(projectRoot, "登录功能还有问题，继续排查并修复")
	if err != nil {
		t.Fatalf("upsertSpecCodingTask failed: %v", err)
	}
	if got.Summary != taskSummary {
		t.Fatalf("expected reopened task summary %q, got %q", taskSummary, got.Summary)
	}

	data, err := os.ReadFile(tasksPath)
	if err != nil {
		t.Fatalf("read tasks.md: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, renderSpecCodingTaskLine(false, got)) {
		t.Fatalf("expected task to be reopened, got:\n%s", text)
	}
	if !strings.Contains(text, "reopened "+taskSummary) {
		t.Fatalf("expected reopened progress note, got:\n%s", text)
	}
}

func TestSpecCodingTaskUsesStableIDAcrossCompleteAndReopen(t *testing.T) {
	projectRoot := t.TempDir()
	tasksPath := filepath.Join(projectRoot, "tasks.md")
	initial := "# Task Breakdown (tasks.md)\n\n## Current Coding Tasks\n\n## Progress Notes\n"
	if err := os.WriteFile(tasksPath, []byte(initial), 0644); err != nil {
		t.Fatalf("write tasks.md: %v", err)
	}

	taskRef, err := upsertSpecCodingTask(projectRoot, "实现支付回调验签并补充回归测试")
	if err != nil {
		t.Fatalf("upsertSpecCodingTask failed: %v", err)
	}
	if taskRef.ID == "" || taskRef.Summary == "" {
		t.Fatalf("expected stable task ref, got %#v", taskRef)
	}
	if err := completeSpecCodingTask(projectRoot, taskRef, "支付回调验签已完成，测试已补充"); err != nil {
		t.Fatalf("completeSpecCodingTask failed: %v", err)
	}

	reopened, err := upsertSpecCodingTask(projectRoot, "支付回调验签回归失败，继续排查修复")
	if err != nil {
		t.Fatalf("upsertSpecCodingTask reopen failed: %v", err)
	}
	if reopened.ID != taskRef.ID {
		t.Fatalf("expected reopened task to keep id %q, got %q", taskRef.ID, reopened.ID)
	}

	data, err := os.ReadFile(tasksPath)
	if err != nil {
		t.Fatalf("read tasks.md: %v", err)
	}
	text := string(data)
	if strings.Count(text, "<!-- spec-task-id:"+taskRef.ID+" -->") != 1 {
		t.Fatalf("expected exactly one task line for stable id %q, got:\n%s", taskRef.ID, text)
	}
	if !strings.Contains(text, renderSpecCodingTaskLine(false, reopened)) {
		t.Fatalf("expected reopened task line with same id, got:\n%s", text)
	}
}

func TestSpecCodingChecklistUpdatesOnTaskCompletion(t *testing.T) {
	projectRoot := t.TempDir()
	checklistPath := filepath.Join(projectRoot, "checklist.md")
	initial := "# Verification Checklist (checklist.md)\n\n- [ ] Scope implemented\n- [ ] Edge cases reviewed\n- [ ] Tests added or updated where needed\n- [ ] Validation run\n- [ ] Docs / prompts / config updated if required\n- [ ] No known missing follow-up inside current scope\n"
	if err := os.WriteFile(checklistPath, []byte(initial), 0644); err != nil {
		t.Fatalf("write checklist.md: %v", err)
	}

	taskRef := specCodingTaskRef{Summary: "实现支付回调验签并补充测试"}
	if err := updateSpecCodingChecklist(projectRoot, taskRef, "已完成实现，go test 验证通过，并更新文档说明"); err != nil {
		t.Fatalf("updateSpecCodingChecklist failed: %v", err)
	}

	data, err := os.ReadFile(checklistPath)
	if err != nil {
		t.Fatalf("read checklist.md: %v", err)
	}
	text := string(data)
	for _, needle := range []string{
		"- [x] Scope implemented",
		"- [x] Tests added or updated where needed",
		"- [x] Validation run",
		"- [x] Docs / prompts / config updated if required",
		"- [x] No known missing follow-up inside current scope",
		"## Verification Notes",
		"verified 实现支付回调验签并补充测试",
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("expected checklist to contain %q, got:\n%s", needle, text)
		}
	}
}

func TestSpecCodingChecklistResetsWhenTaskReopens(t *testing.T) {
	projectRoot := t.TempDir()
	checklistPath := filepath.Join(projectRoot, "checklist.md")
	initial := "# Verification Checklist (checklist.md)\n\n- [x] Scope implemented\n- [x] Edge cases reviewed\n- [x] Tests added or updated where needed\n- [x] Validation run\n- [x] Docs / prompts / config updated if required\n- [x] No known missing follow-up inside current scope\n"
	if err := os.WriteFile(checklistPath, []byte(initial), 0644); err != nil {
		t.Fatalf("write checklist.md: %v", err)
	}

	taskRef := specCodingTaskRef{Summary: "实现支付回调验签并补充测试"}
	if err := resetSpecCodingChecklist(projectRoot, taskRef, "回归失败，继续排查"); err != nil {
		t.Fatalf("resetSpecCodingChecklist failed: %v", err)
	}

	data, err := os.ReadFile(checklistPath)
	if err != nil {
		t.Fatalf("read checklist.md: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "- [x] ") {
		t.Fatalf("expected all checklist items to reopen, got:\n%s", text)
	}
	if !strings.Contains(text, "reopened 实现支付回调验签并补充测试") {
		t.Fatalf("expected reopen note, got:\n%s", text)
	}
}
