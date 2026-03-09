package agent

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var specCodingDocNames = []string{"spec.md", "tasks.md", "checklist.md"}
var reWhitespace = regexp.MustCompile(`\s+`)
var reSpecTaskMeta = regexp.MustCompile(`\s*<!--\s*spec-task-id:([a-f0-9]+)\s*-->`)
var reChecklistItem = regexp.MustCompile(`(?m)^- \[( |x)\] (.+)$`)

type specCodingTaskRef struct {
	ID      string
	Summary string
}

func (al *AgentLoop) maybeEnsureSpecCodingDocs(currentMessage string) error {
	if al == nil || al.contextBuilder == nil {
		return nil
	}
	if !al.contextBuilder.shouldUseSpecCoding(currentMessage) {
		return nil
	}
	projectRoot := al.contextBuilder.projectRootPath()
	if strings.TrimSpace(projectRoot) == "" {
		return nil
	}
	_, err := ensureSpecCodingDocs(al.workspace, projectRoot)
	return err
}

func (al *AgentLoop) maybeStartSpecCodingTask(currentMessage string) (specCodingTaskRef, error) {
	if al == nil || al.contextBuilder == nil || !al.contextBuilder.shouldUseSpecCoding(currentMessage) {
		return specCodingTaskRef{}, nil
	}
	projectRoot := al.contextBuilder.projectRootPath()
	if strings.TrimSpace(projectRoot) == "" {
		return specCodingTaskRef{}, nil
	}
	return upsertSpecCodingTask(projectRoot, currentMessage)
}

func (al *AgentLoop) maybeCompleteSpecCodingTask(taskRef specCodingTaskRef, response string) error {
	if al == nil || al.contextBuilder == nil {
		return nil
	}
	taskRef = normalizeSpecCodingTaskRef(taskRef)
	if taskRef.Summary == "" {
		return nil
	}
	projectRoot := al.contextBuilder.projectRootPath()
	if strings.TrimSpace(projectRoot) == "" {
		return nil
	}
	if err := completeSpecCodingTask(projectRoot, taskRef, response); err != nil {
		return err
	}
	return updateSpecCodingChecklist(projectRoot, taskRef, response)
}

func (al *AgentLoop) maybeReopenSpecCodingTask(taskRef specCodingTaskRef, currentMessage, reason string) error {
	if al == nil || al.contextBuilder == nil {
		return nil
	}
	taskRef = normalizeSpecCodingTaskRef(taskRef)
	if taskRef.Summary == "" {
		return nil
	}
	if strings.TrimSpace(reason) == "" && !shouldReopenSpecCodingTask(currentMessage) {
		return nil
	}
	projectRoot := al.contextBuilder.projectRootPath()
	if strings.TrimSpace(projectRoot) == "" {
		return nil
	}
	note := strings.TrimSpace(reason)
	if note == "" {
		note = strings.TrimSpace(currentMessage)
	}
	if err := reopenSpecCodingTask(projectRoot, taskRef, note); err != nil {
		return err
	}
	return resetSpecCodingChecklist(projectRoot, taskRef, note)
}

func ensureSpecCodingDocs(workspace, projectRoot string) ([]string, error) {
	workspace = strings.TrimSpace(workspace)
	projectRoot = strings.TrimSpace(projectRoot)
	if workspace == "" || projectRoot == "" {
		return nil, nil
	}
	projectRoot = filepath.Clean(projectRoot)
	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		return nil, err
	}

	templatesDir := filepath.Join(workspace, "skills", "spec-coding", "templates")
	created := make([]string, 0, len(specCodingDocNames))
	for _, name := range specCodingDocNames {
		targetPath := filepath.Join(projectRoot, name)
		if _, err := os.Stat(targetPath); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return created, err
		}
		templatePath := filepath.Join(templatesDir, name)
		data, err := os.ReadFile(templatePath)
		if err != nil {
			return created, fmt.Errorf("read spec-coding template %s failed: %w", templatePath, err)
		}
		if err := os.WriteFile(targetPath, data, 0644); err != nil {
			return created, err
		}
		created = append(created, targetPath)
	}
	return created, nil
}

func upsertSpecCodingTask(projectRoot, currentMessage string) (specCodingTaskRef, error) {
	projectRoot = strings.TrimSpace(projectRoot)
	taskSummary := summarizeSpecCodingTask(currentMessage)
	if projectRoot == "" || taskSummary == "" {
		return specCodingTaskRef{}, nil
	}
	taskID := stableSpecCodingTaskID(taskSummary)
	taskRef := specCodingTaskRef{ID: taskID, Summary: taskSummary}
	tasksPath := filepath.Join(projectRoot, "tasks.md")
	data, err := os.ReadFile(tasksPath)
	if err != nil {
		return specCodingTaskRef{}, err
	}
	text := string(data)
	if line, done, ok := findSpecCodingTaskLine(text, taskRef); ok {
		if done {
			if shouldReopenSpecCodingTask(currentMessage) {
				if err := reopenSpecCodingTask(projectRoot, taskRef, currentMessage); err != nil {
					return specCodingTaskRef{}, err
				}
			}
		}
		if strings.Contains(line, "<!--") {
			return specTaskRefFromLine(line), nil
		}
		return taskRef, nil
	}
	doneLine := renderSpecCodingTaskLine(true, taskRef)
	openLine := renderSpecCodingTaskLine(false, taskRef)
	if strings.Contains(text, "- [x] "+taskSummary) || strings.Contains(text, doneLine) {
		if shouldReopenSpecCodingTask(currentMessage) {
			if err := reopenSpecCodingTask(projectRoot, taskRef, currentMessage); err != nil {
				return specCodingTaskRef{}, err
			}
		}
		return taskRef, nil
	}
	if strings.Contains(text, "- [ ] "+taskSummary) || strings.Contains(text, openLine) {
		return taskRef, nil
	}
	if shouldReopenSpecCodingTask(currentMessage) {
		if reopened, err := maybeReopenCompletedTaskFromMessage(projectRoot, currentMessage, text); err != nil {
			return specCodingTaskRef{}, err
		} else if reopened.Summary != "" {
			return reopened, nil
		}
	}
	section := "## Current Coding Tasks\n"
	entry := openLine + "\n"
	if strings.Contains(text, section) {
		text = strings.Replace(text, section, section+entry, 1)
	} else if idx := strings.Index(text, "## Progress Notes"); idx >= 0 {
		text = text[:idx] + section + "\n" + entry + "\n" + text[idx:]
	} else {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += "\n" + section + "\n" + entry
	}
	if err := os.WriteFile(tasksPath, []byte(text), 0644); err != nil {
		return specCodingTaskRef{}, err
	}
	return taskRef, nil
}

func completeSpecCodingTask(projectRoot string, taskRef specCodingTaskRef, response string) error {
	projectRoot = strings.TrimSpace(projectRoot)
	taskRef = normalizeSpecCodingTaskRef(taskRef)
	if projectRoot == "" || taskRef.Summary == "" {
		return nil
	}
	tasksPath := filepath.Join(projectRoot, "tasks.md")
	data, err := os.ReadFile(tasksPath)
	if err != nil {
		return err
	}
	text := string(data)
	if line, done, ok := findSpecCodingTaskLine(text, taskRef); ok {
		if !done {
			text = strings.Replace(text, line, renderSpecCodingTaskLine(true, specTaskRefFromLine(line)), 1)
		}
	} else if strings.Contains(text, "- [ ] "+taskRef.Summary) {
		text = strings.Replace(text, "- [ ] "+taskRef.Summary, renderSpecCodingTaskLine(true, taskRef), 1)
	}
	note := fmt.Sprintf("- %s %s -> %s\n", time.Now().Format("2006-01-02 15:04"), taskRef.Summary, summarizeSpecCodingTask(response))
	if strings.Contains(text, note) {
		return nil
	}
	if strings.Contains(text, "## Progress Notes\n") {
		text = strings.Replace(text, "## Progress Notes\n", "## Progress Notes\n"+note, 1)
	} else {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += "\n## Progress Notes\n" + note
	}
	return os.WriteFile(tasksPath, []byte(text), 0644)
}

func maybeReopenCompletedTaskFromMessage(projectRoot, currentMessage, tasksText string) (specCodingTaskRef, error) {
	completed := extractSpecCodingTasks(tasksText, "- [x] ")
	if len(completed) == 0 {
		return specCodingTaskRef{}, nil
	}
	if len(completed) == 1 {
		return completed[0], reopenSpecCodingTask(projectRoot, completed[0], currentMessage)
	}
	best := specCodingTaskRef{}
	bestScore := 0
	for _, item := range completed {
		score := scoreSpecCodingTaskMatch(item.Summary, currentMessage)
		if score > bestScore {
			best = item
			bestScore = score
		}
	}
	if best.Summary == "" || bestScore == 0 {
		return specCodingTaskRef{}, nil
	}
	return best, reopenSpecCodingTask(projectRoot, best, currentMessage)
}

func reopenSpecCodingTask(projectRoot string, taskRef specCodingTaskRef, reason string) error {
	projectRoot = strings.TrimSpace(projectRoot)
	taskRef = normalizeSpecCodingTaskRef(taskRef)
	if projectRoot == "" || taskRef.Summary == "" {
		return nil
	}
	tasksPath := filepath.Join(projectRoot, "tasks.md")
	data, err := os.ReadFile(tasksPath)
	if err != nil {
		return err
	}
	text := string(data)
	if line, done, ok := findSpecCodingTaskLine(text, taskRef); ok {
		if done {
			text = strings.Replace(text, line, renderSpecCodingTaskLine(false, specTaskRefFromLine(line)), 1)
		}
	} else if strings.Contains(text, "- [x] "+taskRef.Summary) {
		text = strings.Replace(text, "- [x] "+taskRef.Summary, renderSpecCodingTaskLine(false, taskRef), 1)
	}
	note := fmt.Sprintf("- %s reopened %s -> %s\n", time.Now().Format("2006-01-02 15:04"), taskRef.Summary, summarizeSpecCodingTask(reason))
	if strings.Contains(text, note) {
		return nil
	}
	if strings.Contains(text, "## Progress Notes\n") {
		text = strings.Replace(text, "## Progress Notes\n", "## Progress Notes\n"+note, 1)
	} else {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += "\n## Progress Notes\n" + note
	}
	return os.WriteFile(tasksPath, []byte(text), 0644)
}

func summarizeSpecCodingTask(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n", " ")
	text = reWhitespace.ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)
	if len(text) > 120 {
		text = strings.TrimSpace(text[:117]) + "..."
	}
	return text
}

func shouldReopenSpecCodingTask(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	hints := []string{
		"bug", "issue", "problem", "broken", "fix again", "regression", "failing", "failure", "debug", "rework",
		"有问题", "有 bug", "修一下", "重新修复", "回归失败", "排查", "重做", "返工", "异常", "坏了",
	}
	for _, hint := range hints {
		if strings.Contains(text, hint) {
			return true
		}
	}
	return false
}

func updateSpecCodingChecklist(projectRoot string, taskRef specCodingTaskRef, response string) error {
	projectRoot = strings.TrimSpace(projectRoot)
	taskRef = normalizeSpecCodingTaskRef(taskRef)
	if projectRoot == "" || taskRef.Summary == "" {
		return nil
	}
	checklistPath := filepath.Join(projectRoot, "checklist.md")
	data, err := os.ReadFile(checklistPath)
	if err != nil {
		return err
	}
	text := string(data)
	checked := make(map[string]bool)
	checked["Scope implemented"] = true

	evidence := strings.ToLower(taskRef.Summary + "\n" + response)
	if containsAny(evidence, "test", "tests", "go test", "验证", "校验", "回归", "通过", "passed", "validated") {
		checked["Tests added or updated where needed"] = true
		checked["Validation run"] = true
	}
	if containsAny(evidence, "edge", "corner", "boundary", "边界", "异常场景", "边缘", "case reviewed") {
		checked["Edge cases reviewed"] = true
	}
	if containsAny(evidence, "doc", "docs", "readme", "prompt", "config", "文档", "配置", "说明") {
		checked["Docs / prompts / config updated if required"] = true
	}
	if !containsAny(evidence, "todo", "follow-up", "remaining", "pending", "未完成", "后续", "待处理", "blocker", "blocked") {
		checked["No known missing follow-up inside current scope"] = true
	}

	text = reChecklistItem.ReplaceAllStringFunc(text, func(line string) string {
		matches := reChecklistItem.FindStringSubmatch(line)
		if len(matches) != 3 {
			return line
		}
		label := strings.TrimSpace(matches[2])
		if checked[label] {
			return "- [x] " + label
		}
		return line
	})

	note := fmt.Sprintf("- %s verified %s -> %s\n", time.Now().Format("2006-01-02 15:04"), taskRef.Summary, summarizeChecklistEvidence(checked))
	text = upsertChecklistNotes(text, note)
	return os.WriteFile(checklistPath, []byte(text), 0644)
}

func resetSpecCodingChecklist(projectRoot string, taskRef specCodingTaskRef, reason string) error {
	projectRoot = strings.TrimSpace(projectRoot)
	taskRef = normalizeSpecCodingTaskRef(taskRef)
	if projectRoot == "" || taskRef.Summary == "" {
		return nil
	}
	checklistPath := filepath.Join(projectRoot, "checklist.md")
	data, err := os.ReadFile(checklistPath)
	if err != nil {
		return err
	}
	text := reChecklistItem.ReplaceAllString(string(data), "- [ ] $2")
	note := fmt.Sprintf("- %s reopened %s -> %s\n", time.Now().Format("2006-01-02 15:04"), taskRef.Summary, summarizeSpecCodingTask(reason))
	text = upsertChecklistNotes(text, note)
	return os.WriteFile(checklistPath, []byte(text), 0644)
}

func upsertChecklistNotes(text, note string) string {
	if strings.Contains(text, note) {
		return text
	}
	section := "## Verification Notes\n"
	if strings.Contains(text, section) {
		return strings.Replace(text, section, section+note, 1)
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + "\n" + section + note
}

func summarizeChecklistEvidence(checked map[string]bool) string {
	if len(checked) == 0 {
		return "no checklist items matched"
	}
	order := []string{
		"Scope implemented",
		"Edge cases reviewed",
		"Tests added or updated where needed",
		"Validation run",
		"Docs / prompts / config updated if required",
		"No known missing follow-up inside current scope",
	}
	items := make([]string, 0, len(order))
	for _, label := range order {
		if checked[label] {
			items = append(items, label)
		}
	}
	if len(items) == 0 {
		return "no checklist items matched"
	}
	return strings.Join(items, "; ")
}

func containsAny(text string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(text, strings.ToLower(strings.TrimSpace(part))) {
			return true
		}
	}
	return false
}

func extractSpecCodingTasks(tasksText, prefix string) []specCodingTaskRef {
	lines := strings.Split(tasksText, "\n")
	out := make([]specCodingTaskRef, 0, 4)
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, prefix) {
			continue
		}
		ref := specTaskRefFromLine(t)
		if ref.Summary == "" {
			continue
		}
		out = append(out, ref)
	}
	return out
}

func stableSpecCodingTaskID(summary string) string {
	summary = summarizeSpecCodingTask(summary)
	if summary == "" {
		return ""
	}
	sum := sha1.Sum([]byte(strings.ToLower(summary)))
	return hex.EncodeToString(sum[:])[:12]
}

func normalizeSpecCodingTaskRef(taskRef specCodingTaskRef) specCodingTaskRef {
	taskRef.Summary = summarizeSpecCodingTask(taskRef.Summary)
	taskRef.ID = strings.ToLower(strings.TrimSpace(taskRef.ID))
	if taskRef.Summary == "" {
		return specCodingTaskRef{}
	}
	if taskRef.ID == "" {
		taskRef.ID = stableSpecCodingTaskID(taskRef.Summary)
	}
	return taskRef
}

func renderSpecCodingTaskLine(done bool, taskRef specCodingTaskRef) string {
	taskRef = normalizeSpecCodingTaskRef(taskRef)
	if taskRef.Summary == "" {
		return ""
	}
	state := " "
	if done {
		state = "x"
	}
	return fmt.Sprintf("- [%s] %s <!-- spec-task-id:%s -->", state, taskRef.Summary, taskRef.ID)
}

func specTaskRefFromLine(line string) specCodingTaskRef {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "- [ ] ")
	line = strings.TrimPrefix(line, "- [x] ")
	ref := specCodingTaskRef{}
	if matches := reSpecTaskMeta.FindStringSubmatch(line); len(matches) == 2 {
		ref.ID = strings.ToLower(strings.TrimSpace(matches[1]))
		line = strings.TrimSpace(reSpecTaskMeta.ReplaceAllString(line, ""))
	}
	ref.Summary = summarizeSpecCodingTask(line)
	return normalizeSpecCodingTaskRef(ref)
}

func findSpecCodingTaskLine(tasksText string, taskRef specCodingTaskRef) (string, bool, bool) {
	taskRef = normalizeSpecCodingTaskRef(taskRef)
	if taskRef.Summary == "" {
		return "", false, false
	}
	for _, raw := range strings.Split(tasksText, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "- [ ] ") && !strings.HasPrefix(line, "- [x] ") {
			continue
		}
		ref := specTaskRefFromLine(line)
		if ref.Summary == "" {
			continue
		}
		if ref.ID != "" && ref.ID == taskRef.ID {
			return line, strings.HasPrefix(line, "- [x] "), true
		}
		if ref.Summary == taskRef.Summary {
			return line, strings.HasPrefix(line, "- [x] "), true
		}
	}
	return "", false, false
}

func scoreSpecCodingTaskMatch(taskSummary, currentMessage string) int {
	taskSummary = summarizeSpecCodingTask(taskSummary)
	currentMessage = summarizeSpecCodingTask(currentMessage)
	if taskSummary == "" || currentMessage == "" {
		return 0
	}
	if strings.Contains(currentMessage, taskSummary) || strings.Contains(taskSummary, currentMessage) {
		return len(taskSummary)
	}
	shorter := taskSummary
	longer := currentMessage
	if len([]rune(shorter)) > len([]rune(longer)) {
		shorter, longer = longer, shorter
	}
	runes := []rune(shorter)
	best := 0
	for i := 0; i < len(runes); i++ {
		for j := i + 2; j <= len(runes); j++ {
			frag := string(runes[i:j])
			if strings.TrimSpace(frag) == "" {
				continue
			}
			if strings.Contains(longer, frag) && len([]rune(frag)) > best {
				best = len([]rune(frag))
			}
		}
	}
	return best
}
