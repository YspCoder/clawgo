package agent

import (
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"clawgo/pkg/config"
	"clawgo/pkg/logger"
	"clawgo/pkg/providers"
	"clawgo/pkg/skills"
)

type ContextBuilder struct {
	workspace     string
	skillsLoader  *skills.SkillsLoader
	memory        *MemoryStore
	toolsSummary  func() []string // Function to get tool summaries dynamically
	summaryPolicy systemSummaryPolicy
}

const (
	maxInlineMediaFileBytes     int64 = 5 * 1024 * 1024
	maxInlineMediaTotalBytes    int64 = 12 * 1024 * 1024
	maxSystemTaskSummaries            = 4
	maxSystemTaskSummariesChars       = 2400
)

func getGlobalConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".clawgo")
}

func NewContextBuilder(workspace string, memCfg config.MemoryConfig, summaryCfg config.SystemSummaryPolicyConfig, toolsSummaryFunc func() []string) *ContextBuilder {
	// Built-in skills: the current project's skills directory.
	// Use the skills/ directory under the current working directory.
	wd, _ := os.Getwd()
	builtinSkillsDir := filepath.Join(wd, "skills")
	globalSkillsDir := filepath.Join(getGlobalConfigDir(), "skills")

	return &ContextBuilder{
		workspace:     workspace,
		skillsLoader:  skills.NewSkillsLoader(workspace, globalSkillsDir, builtinSkillsDir),
		memory:        NewMemoryStore(workspace, memCfg),
		toolsSummary:  toolsSummaryFunc,
		summaryPolicy: systemSummaryPolicyFromConfig(summaryCfg),
	}
}

func (cb *ContextBuilder) getIdentity() string {
	now := time.Now().Format("2006-01-02 15:04 (Monday)")
	workspacePath, _ := filepath.Abs(filepath.Join(cb.workspace))
	runtime := fmt.Sprintf("%s %s, Go %s", runtime.GOOS, runtime.GOARCH, runtime.Version())

	// Build tools section dynamically
	toolsSection := cb.buildToolsSection()

	return fmt.Sprintf(`# clawgo 🦞

You are clawgo, a helpful AI assistant.

## Current Time
%s

## Runtime
%s

## Workspace
Your workspace is at: %s
- Memory: %s/memory/MEMORY.md
- Daily Notes: %s/memory/YYYYMM/YYYYMMDD.md
- Skills: %s/skills/{skill-name}/SKILL.md

%s

## Important Rules

1. **ALWAYS use tools** - When you need to perform an action (schedule reminders, send messages, execute commands, etc.), you MUST call the appropriate tool. Do NOT just say you'll do it or pretend to do it.

2. **Be helpful and accurate** - When using tools, briefly explain what you're doing.

3. **Memory** - When remembering something, write to %s/memory/MEMORY.md. Prompt memory context is digest-only; use memory_search to retrieve detailed notes when needed.`,
		now, runtime, workspacePath, workspacePath, workspacePath, workspacePath, toolsSection, workspacePath)
}

func (cb *ContextBuilder) buildToolsSection() string {
	if cb.toolsSummary == nil {
		return ""
	}

	summaries := cb.toolsSummary()
	if len(summaries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Available Tools\n\n")
	sb.WriteString("You have access to the following tools:\n\n")
	for _, s := range summaries {
		sb.WriteString(s)
		sb.WriteString("\n")
	}

	return sb.String()
}

func (cb *ContextBuilder) BuildSystemPrompt() string {
	parts := []string{}

	// Core identity section
	parts = append(parts, cb.getIdentity())

	// Bootstrap files
	bootstrapContent := cb.LoadBootstrapFiles()
	if bootstrapContent != "" {
		parts = append(parts, bootstrapContent)
	}

	// Skills - show summary, AI can read full content with read_file tool
	skillsSummary := cb.skillsLoader.BuildSkillsSummary()
	if skillsSummary != "" {
		parts = append(parts, fmt.Sprintf(`# Skills

The following skills extend your capabilities. To use a skill, read its SKILL.md file using the read_file tool.

%s`, skillsSummary))
	}

	// Memory context
	memoryContext := cb.memory.GetMemoryContext()
	if memoryContext != "" {
		parts = append(parts, memoryContext)
	}

	// Join with "---" separator
	return strings.Join(parts, "\n\n---\n\n")
}

func (cb *ContextBuilder) LoadBootstrapFiles() string {
	bootstrapFiles := []string{
		"AGENTS.md",
		"SOUL.md",
		"USER.md",
		"IDENTITY.md",
	}

	var result string
	for _, filename := range bootstrapFiles {
		filePath := filepath.Join(cb.workspace, filename)
		if data, err := os.ReadFile(filePath); err == nil {
			result += fmt.Sprintf("## %s\n\n%s\n\n", filename, string(data))
		}
	}

	return result
}

func (cb *ContextBuilder) BuildMessages(history []providers.Message, summary string, currentMessage string, media []string, channel, chatID string) []providers.Message {
	messages := []providers.Message{}

	systemPrompt := cb.BuildSystemPrompt()
	filteredHistory, systemSummaries := extractSystemTaskSummariesFromHistoryWithPolicy(history, cb.summaryPolicy)

	// Add Current Session info if provided
	if channel != "" && chatID != "" {
		systemPrompt += fmt.Sprintf("\n\n## Current Session\nChannel: %s\nChat ID: %s", channel, chatID)
	}

	// Log system prompt summary for debugging (debug mode only)
	logger.DebugCF("agent", "System prompt built",
		map[string]interface{}{
			"total_chars":   len(systemPrompt),
			"total_lines":   strings.Count(systemPrompt, "\n") + 1,
			"section_count": strings.Count(systemPrompt, "\n\n---\n\n") + 1,
		})

	// Log preview of system prompt (avoid logging huge content)
	preview := systemPrompt
	if len(preview) > 500 {
		preview = preview[:500] + "... (truncated)"
	}
	logger.DebugCF("agent", "System prompt preview",
		map[string]interface{}{
			logger.FieldPreview: preview,
		})

	if summary != "" {
		summary = sanitizeSummaryForPrompt(summary)
		if summary != "" {
			systemPrompt += "\n\n## Summary of Previous Conversation\n\n" + summary
		}
	}
	if len(systemSummaries) > 0 {
		systemPrompt += "\n\n## Recent System Task Summaries\n\n" + formatSystemTaskSummariesWithPolicy(systemSummaries, cb.summaryPolicy)
	}

	messages = append(messages, providers.Message{
		Role:    "system",
		Content: systemPrompt,
	})

	messages = append(messages, filteredHistory...)

	userMsg := providers.Message{
		Role:    "user",
		Content: currentMessage,
	}
	if len(media) > 0 {
		userMsg.ContentParts = buildUserContentParts(currentMessage, media)
	}
	messages = append(messages, userMsg)

	return messages
}

func sanitizeSummaryForPrompt(summary string) string {
	text := strings.TrimSpace(summary)
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		lower := strings.ToLower(strings.TrimSpace(line))
		if strings.Contains(lower, "autonomy round ") ||
			strings.Contains(lower, "auto-learn round ") ||
			strings.Contains(lower, "autonomy mode started.") ||
			strings.Contains(lower, "[system:") {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.TrimSpace(strings.Join(filtered, "\n"))
}

func extractSystemTaskSummariesFromHistory(history []providers.Message) ([]providers.Message, []string) {
	return extractSystemTaskSummariesFromHistoryWithPolicy(history, defaultSystemSummaryPolicy())
}

func extractSystemTaskSummariesFromHistoryWithPolicy(history []providers.Message, policy systemSummaryPolicy) ([]providers.Message, []string) {
	if len(history) == 0 {
		return nil, nil
	}
	filtered := make([]providers.Message, 0, len(history))
	summaries := make([]string, 0, maxSystemTaskSummaries)
	for _, msg := range history {
		if strings.EqualFold(strings.TrimSpace(msg.Role), "assistant") && isSystemTaskSummaryMessageWithPolicy(msg.Content, policy) {
			summaries = append(summaries, strings.TrimSpace(msg.Content))
			continue
		}
		filtered = append(filtered, msg)
	}
	if len(summaries) > maxSystemTaskSummaries {
		summaries = summaries[len(summaries)-maxSystemTaskSummaries:]
	}
	return filtered, summaries
}

func isSystemTaskSummaryMessage(content string) bool {
	return isSystemTaskSummaryMessageWithPolicy(content, defaultSystemSummaryPolicy())
}

func isSystemTaskSummaryMessageWithPolicy(content string, policy systemSummaryPolicy) bool {
	text := strings.TrimSpace(content)
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	marker := strings.ToLower(strings.TrimSpace(policy.marker))
	completed := strings.ToLower(strings.TrimSpace(policy.completedPrefix))
	outcome := strings.ToLower(strings.TrimSpace(policy.outcomePrefix))
	return strings.HasPrefix(lower, marker) ||
		(strings.Contains(lower, marker) && strings.Contains(lower, completed) && strings.Contains(lower, outcome))
}

func formatSystemTaskSummaries(summaries []string) string {
	return formatSystemTaskSummariesWithPolicy(summaries, defaultSystemSummaryPolicy())
}

func formatSystemTaskSummariesWithPolicy(summaries []string, policy systemSummaryPolicy) string {
	if len(summaries) == 0 {
		return ""
	}

	completedItems := make([]string, 0, len(summaries))
	changeItems := make([]string, 0, len(summaries))
	outcomeItems := make([]string, 0, len(summaries))
	for _, raw := range summaries {
		entry := parseSystemTaskSummaryWithPolicy(raw, policy)
		if entry.completed != "" {
			completedItems = append(completedItems, entry.completed)
		}
		if entry.changes != "" {
			changeItems = append(changeItems, entry.changes)
		}
		if entry.outcome != "" {
			outcomeItems = append(outcomeItems, entry.outcome)
		}
	}

	var sb strings.Builder
	writeSection := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("### " + title + "\n")
		for i, item := range items {
			sb.WriteString(fmt.Sprintf("- %d. %s\n", i+1, truncateString(item, maxSystemTaskSummariesChars)))
		}
	}

	writeSection(policy.completedSectionTitle, completedItems)
	writeSection(policy.changesSectionTitle, changeItems)
	writeSection(policy.outcomesSectionTitle, outcomeItems)

	if sb.Len() == 0 {
		for i, s := range summaries {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(fmt.Sprintf("- %d. %s\n", i+1, truncateString(strings.TrimSpace(s), maxSystemTaskSummariesChars)))
		}
	}
	return strings.TrimSpace(sb.String())
}

type systemTaskSummaryEntry struct {
	completed string
	changes   string
	outcome   string
}

func parseSystemTaskSummary(raw string) systemTaskSummaryEntry {
	return parseSystemTaskSummaryWithPolicy(raw, defaultSystemSummaryPolicy())
}

func parseSystemTaskSummaryWithPolicy(raw string, policy systemSummaryPolicy) systemTaskSummaryEntry {
	text := strings.TrimSpace(raw)
	if text == "" {
		return systemTaskSummaryEntry{}
	}
	lines := strings.Split(text, "\n")
	entry := systemTaskSummaryEntry{}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, strings.ToLower(policy.completedPrefix)):
			entry.completed = strings.TrimSpace(trimmed[len(policy.completedPrefix):])
		case strings.HasPrefix(lower, strings.ToLower(policy.changesPrefix)):
			entry.changes = strings.TrimSpace(trimmed[len(policy.changesPrefix):])
		case strings.HasPrefix(lower, strings.ToLower(policy.outcomePrefix)):
			entry.outcome = strings.TrimSpace(trimmed[len(policy.outcomePrefix):])
		}
	}

	firstUsefulLine := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(trimmed), strings.ToLower(policy.completedPrefix)) ||
			strings.HasPrefix(strings.ToLower(trimmed), strings.ToLower(policy.changesPrefix)) ||
			strings.HasPrefix(strings.ToLower(trimmed), strings.ToLower(policy.outcomePrefix)) {
			continue
		}
		firstUsefulLine = trimmed
		break
	}
	if firstUsefulLine == "" {
		firstUsefulLine = truncateString(text, 160)
	}
	if entry.completed == "" {
		entry.completed = firstUsefulLine
	}
	if entry.changes == "" {
		entry.changes = "No explicit file-level changes noted."
	}
	if entry.outcome == "" {
		entry.outcome = firstUsefulLine
	}
	return entry
}

type systemSummaryPolicy struct {
	marker                string
	completedPrefix       string
	changesPrefix         string
	outcomePrefix         string
	completedSectionTitle string
	changesSectionTitle   string
	outcomesSectionTitle  string
}

func defaultSystemSummaryPolicy() systemSummaryPolicy {
	return systemSummaryPolicy{
		marker:                "## System Task Summary",
		completedPrefix:       "- Completed:",
		changesPrefix:         "- Changes:",
		outcomePrefix:         "- Outcome:",
		completedSectionTitle: "Completed Actions",
		changesSectionTitle:   "Change Summaries",
		outcomesSectionTitle:  "Execution Outcomes",
	}
}

func systemSummaryPolicyFromConfig(cfg config.SystemSummaryPolicyConfig) systemSummaryPolicy {
	p := defaultSystemSummaryPolicy()
	p.marker = strings.TrimSpace(cfg.Marker)
	p.completedPrefix = strings.TrimSpace(cfg.CompletedPrefix)
	p.changesPrefix = strings.TrimSpace(cfg.ChangesPrefix)
	p.outcomePrefix = strings.TrimSpace(cfg.OutcomePrefix)
	p.completedSectionTitle = strings.TrimSpace(cfg.CompletedTitle)
	p.changesSectionTitle = strings.TrimSpace(cfg.ChangesTitle)
	p.outcomesSectionTitle = strings.TrimSpace(cfg.OutcomesTitle)
	if strings.TrimSpace(p.marker) == "" {
		p.marker = defaultSystemSummaryPolicy().marker
	}
	if strings.TrimSpace(p.completedPrefix) == "" {
		p.completedPrefix = defaultSystemSummaryPolicy().completedPrefix
	}
	if strings.TrimSpace(p.changesPrefix) == "" {
		p.changesPrefix = defaultSystemSummaryPolicy().changesPrefix
	}
	if strings.TrimSpace(p.outcomePrefix) == "" {
		p.outcomePrefix = defaultSystemSummaryPolicy().outcomePrefix
	}
	if strings.TrimSpace(p.completedSectionTitle) == "" {
		p.completedSectionTitle = defaultSystemSummaryPolicy().completedSectionTitle
	}
	if strings.TrimSpace(p.changesSectionTitle) == "" {
		p.changesSectionTitle = defaultSystemSummaryPolicy().changesSectionTitle
	}
	if strings.TrimSpace(p.outcomesSectionTitle) == "" {
		p.outcomesSectionTitle = defaultSystemSummaryPolicy().outcomesSectionTitle
	}
	return p
}

func (cb *ContextBuilder) AddToolResult(messages []providers.Message, toolCallID, toolName, result string) []providers.Message {
	messages = append(messages, providers.Message{
		Role:       "tool",
		Content:    result,
		ToolCallID: toolCallID,
	})
	return messages
}

func (cb *ContextBuilder) AddAssistantMessage(messages []providers.Message, content string, toolCalls []map[string]interface{}) []providers.Message {
	msg := providers.Message{
		Role:    "assistant",
		Content: content,
	}
	// Always add assistant message, whether or not it has tool calls
	messages = append(messages, msg)
	return messages
}

func (cb *ContextBuilder) loadSkills() string {
	allSkills := cb.skillsLoader.ListSkills()
	if len(allSkills) == 0 {
		return ""
	}

	var skillNames []string
	for _, s := range allSkills {
		skillNames = append(skillNames, s.Name)
	}

	content := cb.skillsLoader.LoadSkillsForContext(skillNames)
	if content == "" {
		return ""
	}

	return "# Skill Definitions\n\n" + content
}

// GetSkillsInfo returns information about loaded skills.
func (cb *ContextBuilder) GetSkillsInfo() map[string]interface{} {
	allSkills := cb.skillsLoader.ListSkills()
	skillNames := make([]string, 0, len(allSkills))
	for _, s := range allSkills {
		skillNames = append(skillNames, s.Name)
	}
	return map[string]interface{}{
		"total":     len(allSkills),
		"available": len(allSkills),
		"names":     skillNames,
	}
}

func buildUserContentParts(text string, media []string) []providers.MessageContentPart {
	parts := make([]providers.MessageContentPart, 0, 1+len(media))
	notes := make([]string, 0)
	var totalInlineBytes int64

	if strings.TrimSpace(text) != "" {
		parts = append(parts, providers.MessageContentPart{
			Type: "input_text",
			Text: text,
		})
	}
	for _, mediaPath := range media {
		p := strings.TrimSpace(mediaPath)
		if p == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(p), "http://") || strings.HasPrefix(strings.ToLower(p), "https://") {
			notes = append(notes, fmt.Sprintf("Attachment kept as URL only and not inlined: %s", p))
			continue
		}

		dataURL, mimeType, filename, sizeBytes, ok := buildFileDataURL(p)
		if !ok {
			notes = append(notes, fmt.Sprintf("Attachment could not be read and was skipped: %s", p))
			continue
		}
		if sizeBytes > maxInlineMediaFileBytes {
			notes = append(notes, fmt.Sprintf("Attachment too large and was not inlined (%s, %d bytes > %d bytes).", filename, sizeBytes, maxInlineMediaFileBytes))
			continue
		}
		if totalInlineBytes+sizeBytes > maxInlineMediaTotalBytes {
			notes = append(notes, fmt.Sprintf("Attachment skipped to keep request size bounded (%s).", filename))
			continue
		}
		totalInlineBytes += sizeBytes

		if strings.HasPrefix(mimeType, "image/") {
			parts = append(parts, providers.MessageContentPart{
				Type:     "input_image",
				ImageURL: dataURL,
				MIMEType: mimeType,
				Filename: filename,
			})
			continue
		}
		parts = append(parts, providers.MessageContentPart{
			Type:     "input_file",
			FileData: dataURL,
			MIMEType: mimeType,
			Filename: filename,
		})
	}

	if len(notes) > 0 {
		parts = append(parts, providers.MessageContentPart{
			Type: "input_text",
			Text: "Attachment handling notes:\n- " + strings.Join(notes, "\n- "),
		})
	}
	return parts
}

func buildFileDataURL(path string) (dataURL, mimeType, filename string, sizeBytes int64, ok bool) {
	stat, err := os.Stat(path)
	if err != nil || stat.IsDir() {
		return "", "", "", 0, false
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", "", "", 0, false
	}
	if len(content) == 0 {
		return "", "", "", 0, false
	}
	filename = filepath.Base(path)
	mimeType = detectMIMEType(path)
	encoded := base64.StdEncoding.EncodeToString(content)
	return fmt.Sprintf("data:%s;base64,%s", mimeType, encoded), mimeType, filename, stat.Size(), true
}

func detectMIMEType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		switch ext {
		case ".pdf":
			mimeType = "application/pdf"
		case ".doc":
			mimeType = "application/msword"
		case ".docx":
			mimeType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		case ".ppt":
			mimeType = "application/vnd.ms-powerpoint"
		case ".pptx":
			mimeType = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
		case ".xls":
			mimeType = "application/vnd.ms-excel"
		case ".xlsx":
			mimeType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		}
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return mimeType
}
