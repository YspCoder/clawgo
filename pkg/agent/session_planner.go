package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"clawgo/pkg/bus"
	"clawgo/pkg/ekg"
	"clawgo/pkg/scheduling"
)

type plannedTask struct {
	Index        int
	Content      string
	ResourceKeys []string
}

type plannedTaskResult struct {
	Index   int
	Task    plannedTask
	Output  string
	Err     error
	ErrText string
}

var reLeadingNumber = regexp.MustCompile(`^\d+[\.)、]\s*`)

func (al *AgentLoop) processPlannedMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	tasks := al.planSessionTasks(msg)
	if len(tasks) <= 1 {
		return al.processMessage(ctx, msg)
	}
	return al.runPlannedTasks(ctx, msg, tasks)
}

func (al *AgentLoop) planSessionTasks(msg bus.InboundMessage) []plannedTask {
	base := strings.TrimSpace(msg.Content)
	if base == "" {
		return nil
	}
	if msg.Channel == "system" || msg.Channel == "internal" {
		return []plannedTask{{Index: 1, Content: base, ResourceKeys: scheduling.DeriveResourceKeys(base)}}
	}
	if msg.Metadata != nil {
		if strings.TrimSpace(msg.Metadata["trigger"]) != "" {
			return []plannedTask{{Index: 1, Content: base, ResourceKeys: scheduling.DeriveResourceKeys(base)}}
		}
	}
	if strings.HasPrefix(base, "/") {
		return []plannedTask{{Index: 1, Content: base, ResourceKeys: scheduling.DeriveResourceKeys(base)}}
	}

	segments := splitPlannedSegments(base)
	if len(segments) <= 1 {
		return []plannedTask{{Index: 1, Content: base, ResourceKeys: scheduling.DeriveResourceKeys(base)}}
	}

	out := make([]plannedTask, 0, len(segments))
	for i, seg := range segments {
		content := strings.TrimSpace(seg)
		if content == "" {
			continue
		}
		out = append(out, plannedTask{
			Index:        i + 1,
			Content:      content,
			ResourceKeys: scheduling.DeriveResourceKeys(content),
		})
	}
	if len(out) == 0 {
		return []plannedTask{{Index: 1, Content: base, ResourceKeys: scheduling.DeriveResourceKeys(base)}}
	}
	if len(out) == 1 {
		out[0].Content = base
		out[0].ResourceKeys = scheduling.DeriveResourceKeys(base)
	}
	return out
}

func splitPlannedSegments(content string) []string {
	lines := strings.Split(content, "\n")
	bullet := make([]string, 0, len(lines))
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") {
			bullet = append(bullet, strings.TrimSpace(t[2:]))
			continue
		}
		if reLeadingNumber.MatchString(t) {
			bullet = append(bullet, strings.TrimSpace(reLeadingNumber.ReplaceAllString(t, "")))
		}
	}
	if len(bullet) >= 2 {
		return bullet
	}

	// Only split implicit plans on strong separators. Plain newlines are often
	// just formatting inside a single request, and "然后/and then" frequently
	// describes execution order inside one task rather than separate tasks.
	replaced := strings.NewReplacer("；", ";")
	norm := replaced.Replace(content)
	parts := strings.Split(norm, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

func (al *AgentLoop) runPlannedTasks(ctx context.Context, msg bus.InboundMessage, tasks []plannedTask) (string, error) {
	results := make([]plannedTaskResult, len(tasks))
	var wg sync.WaitGroup
	var progressMu sync.Mutex
	completed := 0
	failed := 0
	milestones := plannedProgressMilestones(len(tasks))
	notified := make(map[int]struct{}, len(milestones))
	for i, task := range tasks {
		wg.Add(1)
		go func(index int, t plannedTask) {
			defer wg.Done()
			subMsg := msg
			subMsg.Content = al.enrichTaskContentWithMemoryAndEKG(ctx, t)
			subMsg.Metadata = cloneMetadata(msg.Metadata)
			if subMsg.Metadata == nil {
				subMsg.Metadata = map[string]string{}
			}
			subMsg.Metadata["resource_keys"] = strings.Join(t.ResourceKeys, ",")
			subMsg.Metadata["planned_task_index"] = fmt.Sprintf("%d", t.Index)
			subMsg.Metadata["planned_task_total"] = fmt.Sprintf("%d", len(tasks))
			out, err := al.processMessage(ctx, subMsg)
			res := plannedTaskResult{Index: index, Task: t, Output: strings.TrimSpace(out), Err: err}
			if err != nil {
				res.ErrText = err.Error()
			}
			results[index] = res
			progressMu.Lock()
			completed++
			if res.ErrText != "" && !isPlannedTaskCancellation(ctx, res) {
				failed++
			}
			snapshotCompleted := completed
			snapshotFailed := failed
			shouldNotify := shouldPublishPlannedTaskProgress(ctx, len(tasks), snapshotCompleted, res, milestones, notified)
			if shouldNotify && res.ErrText == "" {
				notified[snapshotCompleted] = struct{}{}
			}
			progressMu.Unlock()

			if shouldNotify {
				al.publishPlannedTaskProgress(msg, len(tasks), snapshotCompleted, snapshotFailed, res)
			}
		}(i, task)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("已自动拆解为 %d 个任务并执行：\n\n", len(results)))
	for _, r := range results {
		b.WriteString(fmt.Sprintf("[%d] %s\n", r.Task.Index, r.Task.Content))
		if r.ErrText != "" {
			b.WriteString("执行失败：" + r.ErrText + "\n\n")
			continue
		}
		if r.Output == "" {
			b.WriteString("(无输出)\n\n")
			continue
		}
		b.WriteString(r.Output + "\n\n")
	}
	return strings.TrimSpace(b.String()), nil
}

func plannedProgressMilestones(total int) []int {
	if total <= 3 {
		return nil
	}
	points := []float64{0.33, 0.66}
	out := make([]int, 0, len(points))
	seen := map[int]struct{}{}
	for _, p := range points {
		step := int(math.Round(float64(total) * p))
		if step <= 0 || step >= total {
			continue
		}
		if _, ok := seen[step]; ok {
			continue
		}
		seen[step] = struct{}{}
		out = append(out, step)
	}
	return out
}

func shouldPublishPlannedTaskProgress(ctx context.Context, total, completed int, res plannedTaskResult, milestones []int, notified map[int]struct{}) bool {
	if total <= 1 {
		return false
	}
	if isPlannedTaskCancellation(ctx, res) {
		return false
	}
	if strings.TrimSpace(res.ErrText) != "" {
		return true
	}
	if completed >= total {
		return false
	}
	for _, step := range milestones {
		if completed != step {
			continue
		}
		if _, ok := notified[step]; ok {
			return false
		}
		return true
	}
	return false
}

func isPlannedTaskCancellation(ctx context.Context, res plannedTaskResult) bool {
	if res.Err != nil && errors.Is(res.Err, context.Canceled) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(res.ErrText), context.Canceled.Error()) {
		return true
	}
	return ctx != nil && errors.Is(ctx.Err(), context.Canceled)
}

func (al *AgentLoop) publishPlannedTaskProgress(msg bus.InboundMessage, total, completed, failed int, res plannedTaskResult) {
	if al == nil || al.bus == nil || total <= 1 {
		return
	}
	if msg.Channel == "system" || msg.Channel == "internal" {
		return
	}
	idx := res.Task.Index
	if idx <= 0 {
		idx = res.Index + 1
	}
	status := "完成"
	body := strings.TrimSpace(res.Output)
	if res.ErrText != "" {
		status = "失败"
		body = strings.TrimSpace(res.ErrText)
	}
	if body == "" {
		body = "(无输出)"
	}
	body = summarizePlannedTaskProgressBody(body, 6, 320)
	content := fmt.Sprintf("阶段进度 %d/%d（失败 %d）\n最近任务：%d 已%s\n%s", completed, total, failed, idx, status, body)
	al.bus.PublishOutbound(bus.OutboundMessage{
		Channel: msg.Channel,
		ChatID:  msg.ChatID,
		Content: content,
	})
}

func summarizePlannedTaskProgressBody(body string, maxLines, maxChars int) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.TrimSpace(body)
	if body == "" {
		return "(无输出)"
	}
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
		if maxLines > 0 && len(out) >= maxLines {
			break
		}
	}
	if len(out) == 0 {
		return "(无输出)"
	}
	joined := strings.Join(out, "\n")
	if maxChars > 0 && len(joined) > maxChars {
		joined = truncate(joined, maxChars)
	}
	if len(lines) > len(out) && !strings.HasSuffix(joined, "...") {
		joined += "\n..."
	}
	return joined
}

func (al *AgentLoop) enrichTaskContentWithMemoryAndEKG(ctx context.Context, task plannedTask) string {
	base := strings.TrimSpace(task.Content)
	if base == "" {
		return base
	}
	hints := make([]string, 0, 2)
	if mem := al.memoryHintForTask(ctx, task); mem != "" {
		hints = append(hints, "Memory:\n"+mem)
	}
	if risk := al.ekgHintForTask(task); risk != "" {
		hints = append(hints, "EKG:\n"+risk)
	}
	if len(hints) == 0 {
		return base
	}
	return strings.TrimSpace(
		"Task Context (use it as constraints, avoid repeating known failures):\n" +
			strings.Join(hints, "\n\n") +
			"\n\nTask:\n" + base,
	)
}

func (al *AgentLoop) memoryHintForTask(ctx context.Context, task plannedTask) string {
	if al == nil || al.tools == nil {
		return ""
	}
	args := map[string]interface{}{
		"query":      task.Content,
		"maxResults": 2,
	}
	if ns := memoryNamespaceFromContext(ctx); ns != "main" {
		args["namespace"] = ns
	}
	res, err := al.tools.Execute(ctx, "memory_search", args)
	if err != nil {
		return ""
	}
	txt := strings.TrimSpace(res)
	if txt == "" || strings.HasPrefix(strings.ToLower(txt), "no memory found") {
		return ""
	}
	return truncate(txt, 1200)
}

func (al *AgentLoop) ekgHintForTask(task plannedTask) string {
	if al == nil || al.ekg == nil || strings.TrimSpace(al.workspace) == "" {
		return ""
	}
	evt, ok := al.findRecentRelatedErrorEvent(task.Content)
	if !ok {
		return ""
	}
	errSig := ekg.NormalizeErrorSignature(evt.Log)
	if errSig == "" {
		return ""
	}
	advice := al.ekg.GetAdvice(ekg.SignalContext{
		TaskID:  evt.TaskID,
		ErrSig:  errSig,
		Source:  evt.Source,
		Channel: evt.Channel,
	})
	if !advice.ShouldEscalate {
		return ""
	}
	reasons := strings.Join(advice.Reason, ", ")
	if strings.TrimSpace(reasons) == "" {
		reasons = "repeated error signature"
	}
	return fmt.Sprintf("Related repeated error signature detected (%s). Suggested retry backoff: %ds. Last error: %s",
		errSig, advice.RetryBackoffSec, truncate(strings.TrimSpace(evt.Log), 240))
}

type taskAuditErrorEvent struct {
	TaskID  string
	Source  string
	Channel string
	Log     string
	Preview string
}

func (al *AgentLoop) findRecentRelatedErrorEvent(taskContent string) (taskAuditErrorEvent, bool) {
	path := filepath.Join(strings.TrimSpace(al.workspace), "memory", "task-audit.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return taskAuditErrorEvent{}, false
	}
	defer f.Close()

	kw := tokenizeTaskText(taskContent)
	if len(kw) == 0 {
		return taskAuditErrorEvent{}, false
	}
	var best taskAuditErrorEvent
	bestScore := 0

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		var row map[string]interface{}
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", row["status"]))) != "error" {
			continue
		}
		logText := strings.TrimSpace(fmt.Sprintf("%v", row["log"]))
		if logText == "" {
			continue
		}
		preview := strings.TrimSpace(fmt.Sprintf("%v", row["input_preview"]))
		score := overlapScore(kw, tokenizeTaskText(preview))
		if score < 1 || score < bestScore {
			continue
		}
		bestScore = score
		best = taskAuditErrorEvent{
			TaskID:  strings.TrimSpace(fmt.Sprintf("%v", row["task_id"])),
			Source:  strings.TrimSpace(fmt.Sprintf("%v", row["source"])),
			Channel: strings.TrimSpace(fmt.Sprintf("%v", row["channel"])),
			Log:     logText,
			Preview: preview,
		}
	}
	if bestScore == 0 || strings.TrimSpace(best.TaskID) == "" {
		return taskAuditErrorEvent{}, false
	}
	return best, true
}

func tokenizeTaskText(s string) []string {
	normalized := strings.NewReplacer("\n", " ", "\t", " ", ",", " ", "，", " ", ".", " ", "。", " ", ":", " ", "：", " ", ";", " ", "；", " ").Replace(strings.ToLower(strings.TrimSpace(s)))
	parts := strings.Fields(normalized)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) < 3 {
			continue
		}
		out = append(out, p)
	}
	return out
}

func overlapScore(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := make(map[string]struct{}, len(a))
	for _, k := range a {
		set[k] = struct{}{}
	}
	score := 0
	for _, k := range b {
		if _, ok := set[k]; ok {
			score++
		}
	}
	return score
}

func cloneMetadata(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
