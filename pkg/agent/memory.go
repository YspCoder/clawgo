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
	"time"
)

// MemoryStore manages persistent memory for the agent.
// - Long-term memory: MEMORY.md (workspace root, compatible with OpenClaw)
// - Daily notes: memory/YYYY-MM-DD.md
// It also supports legacy locations for backward compatibility.
type MemoryStore struct {
	workspace       string
	memoryDir       string
	memoryFile      string
	legacyMemoryFile string
}

// NewMemoryStore creates a new MemoryStore with the given workspace path.
// It ensures the memory directory exists.
func NewMemoryStore(workspace string) *MemoryStore {
	memoryDir := filepath.Join(workspace, "memory")
	memoryFile := filepath.Join(workspace, "MEMORY.md")
	legacyMemoryFile := filepath.Join(memoryDir, "MEMORY.md")

	// Ensure memory directory exists
	os.MkdirAll(memoryDir, 0755)

	return &MemoryStore{
		workspace:        workspace,
		memoryDir:        memoryDir,
		memoryFile:       memoryFile,
		legacyMemoryFile: legacyMemoryFile,
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
	if data, err := os.ReadFile(ms.legacyMemoryFile); err == nil {
		return string(data)
	}
	return ""
}

// WriteLongTerm writes content to the long-term memory file (MEMORY.md).
func (ms *MemoryStore) WriteLongTerm(content string) error {
	return os.WriteFile(ms.memoryFile, []byte(content), 0644)
}

// ReadToday reads today's daily note.
// Returns empty string if the file doesn't exist.
func (ms *MemoryStore) ReadToday() string {
	todayFile := ms.getTodayFile()
	if data, err := os.ReadFile(todayFile); err == nil {
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

		// Preferred format: memory/YYYY-MM-DD.md
		newPath := filepath.Join(ms.memoryDir, date.Format("2006-01-02")+".md")
		if data, err := os.ReadFile(newPath); err == nil {
			notes = append(notes, string(data))
			continue
		}

		// Backward-compatible format: memory/YYYYMM/YYYYMMDD.md
		legacyDate := date.Format("20060102")
		legacyPath := filepath.Join(ms.memoryDir, legacyDate[:6], legacyDate+".md")
		if data, err := os.ReadFile(legacyPath); err == nil {
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

	// Long-term memory
	longTerm := ms.ReadLongTerm()
	if longTerm != "" {
		parts = append(parts, "## Long-term Memory\n\n"+longTerm)
	}

	// Recent daily notes (last 3 days)
	recentNotes := ms.GetRecentDailyNotes(3)
	if recentNotes != "" {
		parts = append(parts, "## Recent Daily Notes\n\n"+recentNotes)
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
