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
	"time"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/ekg"
	"github.com/YspCoder/clawgo/pkg/providers"
	"github.com/YspCoder/clawgo/pkg/scheduling"
)

type plannedTask struct {
	Index        int
	Total        int
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
	tasks := al.planSessionTasks(ctx, msg)
	if len(tasks) <= 1 {
		return al.processMessage(ctx, msg)
	}
	return al.runPlannedTasks(ctx, msg, tasks)
}

func (al *AgentLoop) planSessionTasks(ctx context.Context, msg bus.InboundMessage) []plannedTask {
	base := strings.TrimSpace(msg.Content)
	if base == "" {
		return nil
	}
	if msg.Metadata != nil {
		if planningDisabled(msg.Metadata["disable_planning"]) {
			return []plannedTask{{Index: 1, Content: base, ResourceKeys: scheduling.DeriveResourceKeys(base)}}
		}
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
	if refined, ok := al.inferPlannedSegments(ctx, base, segments); ok {
		segments = refined
	}

	out := make([]plannedTask, 0, len(segments))
	for i, seg := range segments {
		content := strings.TrimSpace(seg)
		if content == "" {
			continue
		}
		out = append(out, plannedTask{
			Index:        i + 1,
			Total:        0,
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
	for i := range out {
		out[i].Total = len(out)
	}
	return out
}

type plannerDecision struct {
	ShouldSplit bool     `json:"should_split"`
	Tasks       []string `json:"tasks"`
}

func (al *AgentLoop) inferPlannedSegments(ctx context.Context, content string, candidates []string) ([]string, bool) {
	if al == nil || al.provider == nil {
		return nil, false
	}
	content = strings.TrimSpace(content)
	if content == "" || len(candidates) <= 1 {
		return nil, false
	}
	plannerCtx := ctx
	if plannerCtx == nil {
		plannerCtx = context.Background()
	}
	var cancel context.CancelFunc
	plannerCtx, cancel = context.WithTimeout(plannerCtx, 8*time.Second)
	defer cancel()

	previewCandidates := candidates
	if len(previewCandidates) > 12 {
		previewCandidates = previewCandidates[:12]
	}
	var candidateList strings.Builder
	for i, item := range previewCandidates {
		candidateList.WriteString(fmt.Sprintf("%d. %s\n", i+1, strings.TrimSpace(item)))
	}

	resp, err := al.provider.Chat(plannerCtx, []providers.Message{
		{
			Role: "system",
			Content: "Decide whether the request should stay as one task or be split into a small number of high-level task groups. " +
				"Default to no split. Only split when the request contains multiple independent deliverables, roles, or workstreams. " +
				"Never split into fine-grained execution steps. Merge related steps. Return strict JSON only: " +
				`{"should_split":true|false,"tasks":["..."]}` +
				" If should_split is false, tasks must be empty. If true, tasks must contain 2 to 8 concise high-level task groups.",
		},
		{
			Role: "user",
			Content: fmt.Sprintf("Original request:\n%s\n\nRule-based candidate segments (%d shown of %d):\n%s",
				content,
				len(previewCandidates),
				len(candidates),
				strings.TrimSpace(candidateList.String()),
			),
		},
	}, nil, al.provider.GetDefaultModel(), map[string]interface{}{
		"max_tokens": 256,
	})
	if err != nil || resp == nil {
		return nil, false
	}
	decision, ok := parsePlannerDecision(resp.Content)
	if !ok {
		return nil, false
	}
	if !decision.ShouldSplit {
		return []string{content}, true
	}
	tasks := sanitizePlannerTasks(decision.Tasks)
	if len(tasks) < 2 || len(tasks) > 8 {
		return []string{content}, true
	}
	return tasks, true
}

func parsePlannerDecision(raw string) (plannerDecision, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return plannerDecision{}, false
	}
	if fenced := extractJSONObject(raw); fenced != "" {
		raw = fenced
	}
	var out plannerDecision
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return plannerDecision{}, false
	}
	return out, true
}

func extractJSONObject(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return ""
	}
	return strings.TrimSpace(raw[start : end+1])
}

func sanitizePlannerTasks(items []string) []string {
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		t := strings.TrimSpace(reLeadingNumber.ReplaceAllString(strings.TrimSpace(item), ""))
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, t)
	}
	return out
}

func planningDisabled(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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
	enrichedContent := al.enrichPlannedTaskContents(ctx, tasks)
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
			enriched := enrichedContent[index]
			subMsg.Content = enriched.content
			subMsg.Metadata = cloneMetadata(msg.Metadata)
			if subMsg.Metadata == nil {
				subMsg.Metadata = map[string]string{}
			}
			subMsg.Metadata["resource_keys"] = strings.Join(t.ResourceKeys, ",")
			subMsg.Metadata["planned_task_index"] = fmt.Sprintf("%d", t.Index)
			subMsg.Metadata["planned_task_total"] = fmt.Sprintf("%d", len(tasks))
			if enriched.extraChars > 0 {
				subMsg.Metadata["context_extra_chars"] = fmt.Sprintf("%d", enriched.extraChars)
			}
			if enriched.ekgChars > 0 {
				subMsg.Metadata["context_ekg_chars"] = fmt.Sprintf("%d", enriched.ekgChars)
			}
			if enriched.memoryChars > 0 {
				subMsg.Metadata["context_memory_chars"] = fmt.Sprintf("%d", enriched.memoryChars)
			}
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

type taskPromptHints struct {
	ekg    string
	memory string
}

type plannedTaskPrompt struct {
	content     string
	extraChars  int
	ekgChars    int
	memoryChars int
}

func (al *AgentLoop) enrichPlannedTaskContents(ctx context.Context, tasks []plannedTask) []plannedTaskPrompt {
	out := make([]plannedTaskPrompt, len(tasks))
	seen := make(map[string]struct{}, len(tasks)*2)
	remainingBudget := plannedTaskContextBudget(len(tasks))
	for i, task := range tasks {
		hints := al.collectTaskPromptHints(ctx, task)
		if len(tasks) > 1 {
			dedupeTaskPromptHints(&hints, seen)
			applyPromptBudget(&hints, &remainingBudget)
		}
		prompt := buildPlannedTaskPrompt(task.Content, hints)
		if len(tasks) > 1 && prompt.extraChars > 0 {
			remainingBudget -= prompt.extraChars
			if remainingBudget < 0 {
				remainingBudget = 0
			}
		}
		out[i] = prompt
	}
	return out
}

func (al *AgentLoop) enrichTaskContentWithMemoryAndEKG(ctx context.Context, task plannedTask) string {
	return buildPlannedTaskPrompt(task.Content, al.collectTaskPromptHints(ctx, task)).content
}

func (al *AgentLoop) collectTaskPromptHints(ctx context.Context, task plannedTask) taskPromptHints {
	hints := taskPromptHints{}
	if risk := al.ekgHintForTask(task); risk != "" {
		hints.ekg = risk
		hints.memory = al.memoryHintForTask(ctx, task, true)
		return hints
	}
	hints.memory = al.memoryHintForTask(ctx, task, false)
	return hints
}

func (al *AgentLoop) memoryHintForTask(ctx context.Context, task plannedTask, hasEKG bool) string {
	if al == nil || al.tools == nil {
		return ""
	}
	maxResults := 1
	maxChars := 360
	if task.Total > 1 {
		maxChars = 220
	}
	if hasEKG {
		maxChars = 160
	}
	args := map[string]interface{}{
		"query":      task.Content,
		"maxResults": maxResults,
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
	return compactMemoryHint(txt, maxChars)
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
	parts := []string{
		fmt.Sprintf("repeat_errsig=%s", truncate(errSig, 72)),
		fmt.Sprintf("backoff=%ds", advice.RetryBackoffSec),
	}
	if evt.Preview != "" {
		parts = append(parts, "related_task="+truncate(strings.TrimSpace(evt.Preview), 96))
	}
	if len(advice.Reason) > 0 {
		parts = append(parts, "reason="+truncate(strings.Join(advice.Reason, "+"), 64))
	}
	return strings.Join(parts, "; ")
}

type taskAuditErrorEvent struct {
	TaskID     string
	Source     string
	Channel    string
	Log        string
	Preview    string
	MatchScore int
	MatchRatio float64
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
	bestRatio := 0.0

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
		previewKW := tokenizeTaskText(preview)
		score := overlapScore(kw, previewKW)
		ratio := overlapRatio(kw, previewKW, score)
		if !isStrongTaskMatch(score, ratio) {
			continue
		}
		if score < bestScore || (score == bestScore && ratio < bestRatio) {
			continue
		}
		bestScore = score
		bestRatio = ratio
		best = taskAuditErrorEvent{
			TaskID:     strings.TrimSpace(fmt.Sprintf("%v", row["task_id"])),
			Source:     strings.TrimSpace(fmt.Sprintf("%v", row["source"])),
			Channel:    strings.TrimSpace(fmt.Sprintf("%v", row["channel"])),
			Log:        logText,
			Preview:    preview,
			MatchScore: score,
			MatchRatio: ratio,
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

func overlapRatio(a, b []string, score int) float64 {
	if score <= 0 || len(a) == 0 || len(b) == 0 {
		return 0
	}
	shorter := len(a)
	if len(b) < shorter {
		shorter = len(b)
	}
	if shorter <= 0 {
		return 0
	}
	return float64(score) / float64(shorter)
}

func isStrongTaskMatch(score int, ratio float64) bool {
	if score >= 4 {
		return true
	}
	if score < 2 {
		return false
	}
	return ratio >= 0.35
}

func compactMemoryHint(raw string, maxChars int) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "found ") {
			continue
		}
		if strings.HasPrefix(lower, "source: ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "Source: "))
			if line != "" {
				parts = append(parts, "src="+line)
			}
			continue
		}
		parts = append(parts, line)
		if len(parts) >= 2 {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return truncate(strings.Join(parts, " | "), maxChars)
}

func renderTaskPromptWithHints(taskContent string, hints taskPromptHints) string {
	return buildPlannedTaskPrompt(taskContent, hints).content
}

func buildPlannedTaskPrompt(taskContent string, hints taskPromptHints) plannedTaskPrompt {
	base := strings.TrimSpace(taskContent)
	if base == "" {
		return plannedTaskPrompt{}
	}
	if hints.ekg == "" && hints.memory == "" {
		return plannedTaskPrompt{content: base}
	}
	lines := make([]string, 0, 4)
	lines = append(lines, "Task Context:")
	if hints.ekg != "" {
		lines = append(lines, "EKG: "+hints.ekg)
	}
	if hints.memory != "" {
		lines = append(lines, "Memory: "+hints.memory)
	}
	lines = append(lines, "Task:", base)
	content := strings.Join(lines, "\n")
	return plannedTaskPrompt{
		content:     content,
		extraChars:  maxInt(len(content)-len(base), 0),
		ekgChars:    len(hints.ekg),
		memoryChars: len(hints.memory),
	}
}

func plannedTaskContextBudget(taskCount int) int {
	if taskCount <= 1 {
		return 1 << 30
	}
	return 320
}

func applyPromptBudget(hints *taskPromptHints, remaining *int) {
	if hints == nil || remaining == nil {
		return
	}
	if *remaining <= 0 {
		hints.ekg = ""
		hints.memory = ""
		return
	}
	needed := estimateHintChars(*hints)
	if needed <= *remaining {
		return
	}
	hints.memory = ""
	needed = estimateHintChars(*hints)
	if needed <= *remaining {
		return
	}
	hints.ekg = ""
}

func estimateHintChars(hints taskPromptHints) int {
	total := 0
	if hints.ekg != "" {
		total += len("Task Context:\nEKG: \nTask:\n") + len(hints.ekg)
	}
	if hints.memory != "" {
		total += len("Memory: \n") + len(hints.memory)
		if hints.ekg == "" {
			total += len("Task Context:\nTask:\n")
		}
	}
	return total
}

func dedupeTaskPromptHints(hints *taskPromptHints, seen map[string]struct{}) {
	if hints == nil || seen == nil {
		return
	}
	if hints.ekg != "" {
		key := "ekg:" + hints.ekg
		if _, ok := seen[key]; ok {
			hints.ekg = ""
		} else {
			seen[key] = struct{}{}
		}
	}
	if hints.memory != "" {
		key := "memory:" + hints.memory
		if _, ok := seen[key]; ok {
			hints.memory = ""
		} else {
			seen[key] = struct{}{}
		}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
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
