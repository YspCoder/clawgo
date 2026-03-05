package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"clawgo/pkg/logger"
	"clawgo/pkg/providers"
	"clawgo/pkg/skills"
)

type ContextBuilder struct {
	workspace    string
	skillsLoader *skills.SkillsLoader
	memory       *MemoryStore
	toolsSummary func() []string // Function to get tool summaries dynamically
}

func getGlobalConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".clawgo")
}

func NewContextBuilder(workspace string, toolsSummaryFunc func() []string) *ContextBuilder {
	// Built-in skills: use the current project's skills directory.
	// Resolve from current working directory: skills/
	wd, _ := os.Getwd()
	builtinSkillsDir := filepath.Join(wd, "skills")
	globalSkillsDir := filepath.Join(getGlobalConfigDir(), "skills")

	return &ContextBuilder{
		workspace:    workspace,
		skillsLoader: skills.NewSkillsLoader(workspace, globalSkillsDir, builtinSkillsDir),
		memory:       NewMemoryStore(workspace),
		toolsSummary: toolsSummaryFunc,
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
- Long-term Memory: %s/MEMORY.md
- Daily Notes: %s/memory/YYYY-MM-DD.md
- Skills: %s/skills/{skill-name}/SKILL.md

%s`,
		now, runtime, workspacePath, workspacePath, workspacePath, workspacePath, toolsSection)
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
	return cb.BuildSystemPromptWithMemoryNamespace("main")
}

func (cb *ContextBuilder) BuildSystemPromptWithMemoryNamespace(memoryNamespace string) string {
	parts := []string{}

	// Core identity section
	parts = append(parts, cb.getIdentity())

	// Bootstrap files
	bootstrapContent := cb.LoadBootstrapFiles()
	if bootstrapContent != "" {
		parts = append(parts, bootstrapContent)
	}

	// Skills catalog (selection behavior is defined in workspace AGENTS.md/SOUL.md).
	skillsSummary := cb.skillsLoader.BuildSkillsSummary()
	if skillsSummary != "" {
		parts = append(parts, fmt.Sprintf("## Skills Catalog\n\n%s", skillsSummary))
	}

	// Memory context
	memStore := cb.memory
	if ns := normalizeMemoryNamespace(memoryNamespace); ns != "main" {
		memStore = NewMemoryStoreWithNamespace(cb.workspace, ns)
	}
	memoryContext := memStore.GetMemoryContext()
	if memoryContext != "" {
		parts = append(parts, memoryContext)
	}

	// Join with "---" separator
	return strings.Join(parts, "\n\n---\n\n")
}

func (cb *ContextBuilder) LoadBootstrapFiles() string {
	bootstrapFiles := []string{
		"AGENTS.md",
		"BOOT.md",
		"SOUL.md",
		"USER.md",
		"IDENTITY.md",
		"TOOLS.md",
	}

	var result string
	for _, filename := range bootstrapFiles {
		filePath := filepath.Join(cb.workspace, filename)
		if data, err := os.ReadFile(filePath); err == nil {
			result += fmt.Sprintf("## %s\n\n%s\n\n", filename, string(data))
		}
	}

	// BOOTSTRAP.md is first-run guidance; load only when identity/user is not initialized.
	if cb.shouldLoadBootstrap() {
		if data, err := os.ReadFile(filepath.Join(cb.workspace, "BOOTSTRAP.md")); err == nil {
			result += fmt.Sprintf("## %s\n\n%s\n\n", "BOOTSTRAP.md", string(data))
		}
	}

	return result
}

func (cb *ContextBuilder) shouldLoadBootstrap() bool {
	identityPath := filepath.Join(cb.workspace, "IDENTITY.md")
	userPath := filepath.Join(cb.workspace, "USER.md")
	identityData, idErr := os.ReadFile(identityPath)
	userData, userErr := os.ReadFile(userPath)
	if idErr != nil || userErr != nil {
		return true
	}
	idText := string(identityData)
	userText := string(userData)
	if idText == "" || userText == "" {
		return true
	}
	return false
}

func (cb *ContextBuilder) BuildMessages(history []providers.Message, summary string, currentMessage string, media []string, channel, chatID, responseLanguage string) []providers.Message {
	return cb.BuildMessagesWithMemoryNamespace(history, summary, currentMessage, media, channel, chatID, responseLanguage, "main")
}

func (cb *ContextBuilder) BuildMessagesWithMemoryNamespace(history []providers.Message, summary string, currentMessage string, media []string, channel, chatID, responseLanguage, memoryNamespace string) []providers.Message {
	messages := []providers.Message{}

	systemPrompt := cb.BuildSystemPromptWithMemoryNamespace(memoryNamespace)

	// Add Current Session info if provided
	if channel != "" && chatID != "" {
		systemPrompt += fmt.Sprintf("\n\n## Current Session\nChannel: %s\nChat ID: %s", channel, chatID)
	}
	if responseLanguage != "" {
		systemPrompt += fmt.Sprintf("\n\n## Response Language\nReply in %s unless user explicitly asks to switch language. Keep code identifiers and CLI commands unchanged.", responseLanguage)
	}

	// Log system prompt summary for debugging (debug mode only)
	logger.DebugCF("agent", logger.C0143,
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
	logger.DebugCF("agent", logger.C0144,
		map[string]interface{}{
			"preview": preview,
		})

	if summary != "" {
		systemPrompt += "\n\n## Summary of Previous Conversation\n\n" + summary
	}

	messages = append(messages, providers.Message{
		Role:    "system",
		Content: systemPrompt,
	})

	messages = append(messages, history...)

	messages = append(messages, providers.Message{
		Role:    "user",
		Content: currentMessage,
	})

	return messages
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
