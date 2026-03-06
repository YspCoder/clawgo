package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"clawgo/pkg/config"
	"clawgo/pkg/runtimecfg"
)

func TestSubagentProfileStoreNormalization(t *testing.T) {
	store := NewSubagentProfileStore(t.TempDir())
	saved, err := store.Upsert(SubagentProfile{
		AgentID:         "Coder Agent",
		Name:            "  ",
		Role:            "coding",
		MemoryNamespace: "My Namespace",
		ToolAllowlist:   []string{" Read_File ", "memory_search", "READ_FILE"},
		Status:          "ACTIVE",
	})
	if err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	if saved.AgentID != "coder-agent" {
		t.Fatalf("unexpected agent_id: %s", saved.AgentID)
	}
	if saved.Name != "coder-agent" {
		t.Fatalf("unexpected default name: %s", saved.Name)
	}
	if saved.MemoryNamespace != "my-namespace" {
		t.Fatalf("unexpected memory namespace: %s", saved.MemoryNamespace)
	}
	if len(saved.ToolAllowlist) != 2 {
		t.Fatalf("unexpected allowlist size: %d (%v)", len(saved.ToolAllowlist), saved.ToolAllowlist)
	}
	for _, tool := range saved.ToolAllowlist {
		if tool != strings.ToLower(tool) {
			t.Fatalf("tool allowlist should be lowercase, got: %s", tool)
		}
	}
}

func TestSubagentManagerSpawnRejectsDisabledProfile(t *testing.T) {
	workspace := t.TempDir()
	manager := NewSubagentManager(nil, workspace, nil, nil)
	manager.SetRunFunc(func(ctx context.Context, task *SubagentTask) (string, error) {
		return "ok", nil
	})
	store := manager.ProfileStore()
	if store == nil {
		t.Fatalf("expected profile store to be available")
	}
	if _, err := store.Upsert(SubagentProfile{
		AgentID: "writer",
		Status:  "disabled",
	}); err != nil {
		t.Fatalf("failed to seed profile: %v", err)
	}

	_, err := manager.Spawn(context.Background(), SubagentSpawnOptions{
		Task:          "Write docs",
		AgentID:       "writer",
		OriginChannel: "cli",
		OriginChatID:  "direct",
	})
	if err == nil {
		t.Fatalf("expected disabled profile to block spawn")
	}
}

func TestSubagentManagerSpawnResolvesProfileByRole(t *testing.T) {
	workspace := t.TempDir()
	manager := NewSubagentManager(nil, workspace, nil, nil)
	store := manager.ProfileStore()
	if store == nil {
		t.Fatalf("expected profile store to be available")
	}
	if _, err := store.Upsert(SubagentProfile{
		AgentID:       "coder",
		Role:          "coding",
		Status:        "active",
		ToolAllowlist: []string{"read_file"},
	}); err != nil {
		t.Fatalf("failed to seed profile: %v", err)
	}

	_, err := manager.Spawn(context.Background(), SubagentSpawnOptions{
		Task:          "Implement feature",
		Role:          "coding",
		OriginChannel: "cli",
		OriginChatID:  "direct",
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}

	tasks := manager.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("expected one task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.AgentID != "coder" {
		t.Fatalf("expected agent_id to resolve to profile agent_id 'coder', got: %s", task.AgentID)
	}
	if task.Role != "coding" {
		t.Fatalf("expected task role to remain 'coding', got: %s", task.Role)
	}
	if len(task.ToolAllowlist) != 1 || task.ToolAllowlist[0] != "read_file" {
		t.Fatalf("expected allowlist from profile, got: %v", task.ToolAllowlist)
	}
	_ = waitSubagentDone(t, manager, 4*time.Second)
}

func TestSubagentProfileStoreReadsProfilesFromRuntimeConfig(t *testing.T) {
	runtimecfg.Set(config.DefaultConfig())
	t.Cleanup(func() {
		runtimecfg.Set(config.DefaultConfig())
	})

	cfg := config.DefaultConfig()
	cfg.Agents.Subagents["coder"] = config.SubagentConfig{
		Enabled:         true,
		DisplayName:     "Code Agent",
		Role:            "coding",
		SystemPrompt:    "write code",
		MemoryNamespace: "code-ns",
		Tools: config.SubagentToolsConfig{
			Allowlist: []string{"read_file", "shell"},
		},
		Runtime: config.SubagentRuntimeConfig{
			MaxRetries:     2,
			RetryBackoffMs: 2000,
			TimeoutSec:     120,
			MaxTaskChars:   4096,
			MaxResultChars: 2048,
		},
	}
	runtimecfg.Set(cfg)

	store := NewSubagentProfileStore(t.TempDir())
	profile, ok, err := store.Get("coder")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !ok {
		t.Fatalf("expected config-backed profile")
	}
	if profile.ManagedBy != "config.json" {
		t.Fatalf("expected config ownership, got: %s", profile.ManagedBy)
	}
	if profile.Name != "Code Agent" || profile.Role != "coding" {
		t.Fatalf("unexpected profile fields: %+v", profile)
	}
	if len(profile.ToolAllowlist) != 2 {
		t.Fatalf("expected merged allowlist, got: %v", profile.ToolAllowlist)
	}
}

func TestSubagentProfileStoreRejectsWritesForConfigManagedProfiles(t *testing.T) {
	runtimecfg.Set(config.DefaultConfig())
	t.Cleanup(func() {
		runtimecfg.Set(config.DefaultConfig())
	})

	cfg := config.DefaultConfig()
	cfg.Agents.Subagents["tester"] = config.SubagentConfig{
		Enabled: true,
		Role:    "test",
	}
	runtimecfg.Set(cfg)

	store := NewSubagentProfileStore(t.TempDir())
	if _, err := store.Upsert(SubagentProfile{AgentID: "tester"}); err == nil {
		t.Fatalf("expected config-managed upsert to fail")
	}
	if err := store.Delete("tester"); err == nil {
		t.Fatalf("expected config-managed delete to fail")
	}
}
