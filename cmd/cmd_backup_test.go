package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnifiedBackupCreateAndImport(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	configPath := filepath.Join(root, "config", "config.json")
	agentsDir := filepath.Join(root, "agents", "main", "sessions")
	skillsDir := filepath.Join(workspace, "skills", "demo")
	memoryDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	_ = os.WriteFile(configPath, []byte(`{"gateway":{"token":"abc"}}`), 0644)
	_ = os.WriteFile(filepath.Join(workspace, "MEMORY.md"), []byte("long-term"), 0644)
	_ = os.WriteFile(filepath.Join(memoryDir, "2026-04-14.md"), []byte("daily-note"), 0644)
	_ = os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte("# demo"), 0644)
	_ = os.WriteFile(filepath.Join(agentsDir, "main.active.jsonl"), []byte("{\"type\":\"message\"}\n"), 0644)

	archive := filepath.Join(root, "backup.zip")
	files, err := createUnifiedBackup(workspace, configPath, archive)
	if err != nil {
		t.Fatalf("createUnifiedBackup: %v", err)
	}
	if files < 4 {
		t.Fatalf("expected backup files >= 4, got %d", files)
	}

	// Mutate files to ensure import actually restores prior state.
	_ = os.WriteFile(configPath, []byte(`{"gateway":{"token":"changed"}}`), 0644)
	_ = os.WriteFile(filepath.Join(workspace, "MEMORY.md"), []byte("changed-memory"), 0644)

	rollback, restored, err := importUnifiedBackup(workspace, configPath, archive)
	if err != nil {
		t.Fatalf("importUnifiedBackup: %v", err)
	}
	if restored < 4 {
		t.Fatalf("expected restored files >= 4, got %d", restored)
	}
	if strings.TrimSpace(rollback) == "" {
		t.Fatalf("expected rollback path")
	}
	if _, err := os.Stat(rollback); err != nil {
		t.Fatalf("rollback snapshot missing: %v", err)
	}
	cfgData, _ := os.ReadFile(configPath)
	if !strings.Contains(string(cfgData), `"abc"`) {
		t.Fatalf("config not restored, got %s", string(cfgData))
	}
	memData, _ := os.ReadFile(filepath.Join(workspace, "MEMORY.md"))
	if strings.TrimSpace(string(memData)) != "long-term" {
		t.Fatalf("memory not restored, got %s", string(memData))
	}
}
