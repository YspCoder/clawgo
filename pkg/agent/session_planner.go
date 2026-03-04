package agent

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"clawgo/pkg/bus"
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

	replaced := strings.NewReplacer("；", ";", "\n", ";", "。然后", ";", " 然后 ", ";", " and then ", ";")
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
	for i, task := range tasks {
		wg.Add(1)
		go func(index int, t plannedTask) {
			defer wg.Done()
			subMsg := msg
			subMsg.Content = t.Content
			subMsg.Metadata = cloneMetadata(msg.Metadata)
			if subMsg.Metadata == nil {
				subMsg.Metadata = map[string]string{}
			}
			subMsg.Metadata["resource_keys"] = strings.Join(t.ResourceKeys, ",")
			subMsg.Metadata["planned_task_index"] = fmt.Sprintf("%d", t.Index)
			subMsg.Metadata["planned_task_total"] = fmt.Sprintf("%d", len(tasks))
			out, err := al.processMessage(ctx, subMsg)
			res := plannedTaskResult{Index: index, Task: t, Output: strings.TrimSpace(out)}
			if err != nil {
				res.ErrText = err.Error()
			}
			results[index] = res
		}(i, task)
	}
	wg.Wait()

	sort.SliceStable(results, func(i, j int) bool { return results[i].Task.Index < results[j].Task.Index })
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
