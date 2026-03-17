// ClawGo - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 ClawGo contributors

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MemoryStore manages persistent memory for the agent.
// - Long-term memory: MEMORY.md
// - Daily notes: memory/YYYY-MM-DD.md
type MemoryStore struct {
	workspace  string
	namespace  string
	memoryDir  string
	memoryFile string
}

// NewMemoryStore creates a new MemoryStore with the given workspace path.
// It ensures the memory directory exists.
func NewMemoryStore(workspace string) *MemoryStore {
	return NewMemoryStoreWithNamespace(workspace, "main")
}

func NewMemoryStoreWithNamespace(workspace, namespace string) *MemoryStore {
	ns := normalizeMemoryNamespace(namespace)
	baseDir := workspace
	if ns != "main" {
		baseDir = filepath.Join(workspace, "agents", ns)
	}

	memoryDir := filepath.Join(baseDir, "memory")
	memoryFile := filepath.Join(baseDir, "MEMORY.md")

	// Ensure memory directory exists
	os.MkdirAll(memoryDir, 0755)

	return &MemoryStore{
		workspace:  workspace,
		namespace:  ns,
		memoryDir:  memoryDir,
		memoryFile: memoryFile,
	}
}

// getTodayFile returns the path to today's daily note file (memory/YYYY-MM-DD.md).
func (ms *MemoryStore) getTodayFile() string {
	return filepath.Join(ms.memoryDir, time.Now().Format("2006-01-02")+".md")
}

// ReadLongTerm reads the long-term memory (MEMORY.md).
// Returns empty string if the file doesn't exist.
func (ms *MemoryStore) ReadLongTerm() string {
	if data, err := os.ReadFile(ms.memoryFile); err == nil {
		return string(data)
	}
	return ""
}

// AppendToday appends content to today's daily note.
// If the file doesn't exist, it creates a new file with a date header.
func (ms *MemoryStore) AppendToday(content string) error {
	todayFile := ms.getTodayFile()

	// Ensure memory directory exists
	os.MkdirAll(ms.memoryDir, 0755)

	var existingContent string
	if data, err := os.ReadFile(todayFile); err == nil {
		existingContent = string(data)
	}

	var newContent string
	if existingContent == "" {
		// Add header for new day
		header := fmt.Sprintf("# %s\n\n", time.Now().Format("2006-01-02"))
		newContent = header + content
	} else {
		// Append to existing content
		newContent = existingContent + "\n" + content
	}

	return os.WriteFile(todayFile, []byte(newContent), 0644)
}

// GetRecentDailyNotes returns daily notes from the last N days.
// Contents are joined with "---" separator.
func (ms *MemoryStore) GetRecentDailyNotes(days int) string {
	var notes []string

	for i := 0; i < days; i++ {
		date := time.Now().AddDate(0, 0, -i)

		path := filepath.Join(ms.memoryDir, date.Format("2006-01-02")+".md")
		if data, err := os.ReadFile(path); err == nil {
			notes = append(notes, string(data))
		}
	}

	if len(notes) == 0 {
		return ""
	}

	// Join with separator
	var result string
	for i, note := range notes {
		if i > 0 {
			result += "\n\n---\n\n"
		}
		result += note
	}
	return result
}

// GetMemoryContext returns formatted memory context for the agent prompt.
// Includes long-term memory and recent daily notes.
func (ms *MemoryStore) GetMemoryContext() string {
	var parts []string

	// Recent daily notes first (today + yesterday), to prioritize fresh context
	recentNotes := ms.GetRecentDailyNotes(2)
	if recentNotes != "" {
		parts = append(parts, "## Recent Daily Notes\n\n"+recentNotes)
	}

	// Long-term memory
	longTerm := ms.ReadLongTerm()
	if longTerm != "" {
		parts = append(parts, "## Long-term Memory\n\n"+longTerm)
	}

	if len(parts) == 0 {
		return ""
	}

	// Join parts with separator
	var result string
	for i, part := range parts {
		if i > 0 {
			result += "\n\n---\n\n"
		}
		result += part
	}
	return fmt.Sprintf("# Memory\n\n%s", result)
}

func normalizeMemoryNamespace(namespace string) string {
	namespace = strings.TrimSpace(strings.ToLower(namespace))
	if namespace == "" || namespace == "main" {
		return "main"
	}
	var sb strings.Builder
	for _, r := range namespace {
		switch {
		case r >= 'a' && r <= 'z':
			sb.WriteRune(r)
		case r >= '0' && r <= '9':
			sb.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			sb.WriteRune(r)
		case r == ' ':
			sb.WriteRune('-')
		}
	}
	out := strings.Trim(sb.String(), "-_.")
	if out == "" {
		return "main"
	}
	return out
}
