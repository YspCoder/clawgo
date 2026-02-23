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
	"sync"
	"time"

	"clawgo/pkg/config"
	"clawgo/pkg/logger"
)

const (
	maxMemoryContextChars   = 6000
	maxLongTermMemoryChars  = 2200
	maxRecentNotesChars     = 1600
	maxMemoryLayerPartChars = 1200
	maxMemoryDigestLines    = 14
)

// MemoryStore manages persistent memory for the agent.
// - Long-term memory: memory/MEMORY.md
// - Daily notes: memory/YYYYMM/YYYYMMDD.md
type MemoryStore struct {
	workspace        string
	memoryDir        string
	memoryFile       string
	layered          bool
	recentDays       int
	includeProfile   bool
	includeProject   bool
	includeProcedure bool
	mu               sync.Mutex
}

// NewMemoryStore creates a new MemoryStore with the given workspace path.
// It ensures the memory directory exists.
func NewMemoryStore(workspace string, cfg config.MemoryConfig) *MemoryStore {
	memoryDir := filepath.Join(workspace, "memory")
	memoryFile := filepath.Join(memoryDir, "MEMORY.md")

	// Ensure memory directory exists
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		logger.ErrorCF("memory", "Failed to create memory directory", map[string]interface{}{
			"memory_dir":      memoryDir,
			logger.FieldError: err.Error(),
		})
	}

	// Ensure MEMORY.md exists for first run (even without onboard).
	if _, err := os.Stat(memoryFile); os.IsNotExist(err) {
		initial := `# Long-term Memory

This file stores important information that should persist across sessions.

## User Information

(Important facts about user)

## Preferences

(User preferences learned over time)

## Important Notes

(Things to remember)
`
		if writeErr := os.WriteFile(memoryFile, []byte(initial), 0644); writeErr != nil {
			logger.ErrorCF("memory", "Failed to initialize MEMORY.md", map[string]interface{}{
				"memory_file":     memoryFile,
				logger.FieldError: writeErr.Error(),
			})
		}
	}

	if cfg.Layered {
		_ = os.MkdirAll(filepath.Join(memoryDir, "layers"), 0755)
		ensureLayerFile(filepath.Join(memoryDir, "layers", "profile.md"), "# User Profile\n\nStable user profile, preferences, identity traits.\n")
		ensureLayerFile(filepath.Join(memoryDir, "layers", "project.md"), "# Project Memory\n\nProject-specific architecture decisions and constraints.\n")
		ensureLayerFile(filepath.Join(memoryDir, "layers", "procedures.md"), "# Procedures Memory\n\nReusable workflows, command recipes, and runbooks.\n")
	}

	recentDays := cfg.RecentDays
	if recentDays <= 0 {
		recentDays = 3
	}

	return &MemoryStore{
		workspace:        workspace,
		memoryDir:        memoryDir,
		memoryFile:       memoryFile,
		layered:          cfg.Layered,
		recentDays:       recentDays,
		includeProfile:   cfg.Layers.Profile,
		includeProject:   cfg.Layers.Project,
		includeProcedure: cfg.Layers.Procedures,
	}
}

func ensureLayerFile(path, initial string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		_ = os.WriteFile(path, []byte(initial), 0644)
	}
}

// getTodayFile returns the path to today's daily note file (memory/YYYYMM/YYYYMMDD.md).
func (ms *MemoryStore) getTodayFile() string {
	today := time.Now().Format("20060102") // YYYYMMDD
	monthDir := today[:6]                  // YYYYMM
	filePath := filepath.Join(ms.memoryDir, monthDir, today+".md")
	return filePath
}

// ReadLongTerm reads the long-term memory (MEMORY.md).
// Returns empty string if the file doesn't exist.
func (ms *MemoryStore) ReadLongTerm() string {
	if data, err := os.ReadFile(ms.memoryFile); err == nil {
		return string(data)
	}
	return ""
}

// WriteLongTerm writes content to the long-term memory file (MEMORY.md).
func (ms *MemoryStore) WriteLongTerm(content string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if err := os.MkdirAll(ms.memoryDir, 0755); err != nil {
		return err
	}
	return atomicWriteFile(ms.memoryFile, []byte(content), 0644)
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
	ms.mu.Lock()
	defer ms.mu.Unlock()

	todayFile := ms.getTodayFile()

	// Ensure month directory exists
	monthDir := filepath.Dir(todayFile)
	if err := os.MkdirAll(monthDir, 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(todayFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	payload := content
	if info.Size() == 0 {
		// Add header for new day
		header := fmt.Sprintf("# %s\n\n", time.Now().Format("2006-01-02"))
		payload = header + content
	} else {
		payload = "\n" + content
	}

	_, err = f.WriteString(payload)
	return err
}

// GetRecentDailyNotes returns daily notes from the last N days.
// Contents are joined with "---" separator.
func (ms *MemoryStore) GetRecentDailyNotes(days int) string {
	var notes []string

	for i := 0; i < days; i++ {
		date := time.Now().AddDate(0, 0, -i)
		dateStr := date.Format("20060102") // YYYYMMDD
		monthDir := dateStr[:6]            // YYYYMM
		filePath := filepath.Join(ms.memoryDir, monthDir, dateStr+".md")

		if data, err := os.ReadFile(filePath); err == nil {
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

	if ms.layered {
		layerParts := ms.getLayeredContext()
		parts = append(parts, layerParts...)
	}

	// Long-term memory
	longTerm := ms.ReadLongTerm()
	if longTerm != "" {
		parts = append(parts, "## Long-term Memory (Digest)\n\n"+compressMemoryForPrompt(longTerm, maxMemoryDigestLines, maxLongTermMemoryChars))
	}

	// Recent daily notes
	recentNotes := ms.GetRecentDailyNotes(ms.recentDays)
	if recentNotes != "" {
		parts = append(parts, "## Recent Daily Notes (Digest)\n\n"+compressMemoryForPrompt(recentNotes, maxMemoryDigestLines, maxRecentNotesChars))
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
	return fmt.Sprintf("# Memory\n\n%s", truncateMemoryText(result, maxMemoryContextChars))
}

func (ms *MemoryStore) getLayeredContext() []string {
	parts := []string{}
	readLayer := func(filename, title string) {
		data, err := os.ReadFile(filepath.Join(ms.memoryDir, "layers", filename))
		if err != nil {
			return
		}
		content := string(data)
		if strings.TrimSpace(content) == "" {
			return
		}
		parts = append(parts, fmt.Sprintf("## %s (Digest)\n\n%s", title, compressMemoryForPrompt(content, maxMemoryDigestLines, maxMemoryLayerPartChars)))
	}

	if ms.includeProfile {
		readLayer("profile.md", "Memory Layer: Profile")
	}
	if ms.includeProject {
		readLayer("project.md", "Memory Layer: Project")
	}
	if ms.includeProcedure {
		readLayer("procedures.md", "Memory Layer: Procedures")
	}
	return parts
}

func truncateMemoryText(content string, maxChars int) string {
	if maxChars <= 0 {
		return strings.TrimSpace(content)
	}
	trimmed := strings.TrimSpace(content)
	runes := []rune(trimmed)
	if len(runes) <= maxChars {
		return trimmed
	}
	suffix := "\n\n...[truncated]"
	suffixRunes := []rune(suffix)
	if maxChars <= len(suffixRunes) {
		return string(runes[:maxChars])
	}
	return strings.TrimSpace(string(runes[:maxChars-len(suffixRunes)])) + suffix
}

func compressMemoryForPrompt(content string, maxLines, maxChars int) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	if maxLines <= 0 {
		maxLines = maxMemoryDigestLines
	}

	lines := strings.Split(trimmed, "\n")
	kept := make([]string, 0, maxLines)
	inParagraph := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			inParagraph = false
			continue
		}

		isHeading := strings.HasPrefix(line, "#")
		isBullet := strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ")
		isNumbered := isNumberedListLine(line)

		if isHeading || isBullet || isNumbered {
			kept = append(kept, line)
			inParagraph = false
		} else if !inParagraph {
			// Keep only the first line of each paragraph to form a compact digest.
			kept = append(kept, line)
			inParagraph = true
		}

		if len(kept) >= maxLines {
			break
		}
	}

	if len(kept) == 0 {
		for _, raw := range lines {
			line := strings.TrimSpace(raw)
			if line == "" {
				continue
			}
			kept = append(kept, line)
			if len(kept) >= maxLines {
				break
			}
		}
	}

	return truncateMemoryText(strings.Join(kept, "\n"), maxChars)
}

func isNumberedListLine(line string) bool {
	dot := strings.Index(line, ".")
	if dot <= 0 || dot >= len(line)-1 {
		return false
	}
	for i := 0; i < dot; i++ {
		if line[i] < '0' || line[i] > '9' {
			return false
		}
	}
	return line[dot+1] == ' '
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, perm); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
