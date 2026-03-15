package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/nodes"
	"github.com/YspCoder/clawgo/pkg/runtimecfg"
)

func TestAgentProfileStoreNormalization(t *testing.T) {
	store := NewAgentProfileStore(t.TempDir())
	saved, err := store.Upsert(AgentProfile{
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

func TestAgentProfileToolCreateParsesStringNumericArgs(t *testing.T) {
	store := NewAgentProfileStore(t.TempDir())
	tool := NewAgentProfileTool(store)

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":           "create",
		"agent_id":         "reviewer",
		"role":             "testing",
		"status":           "active",
		"prompt_file":      "agents/reviewer/AGENT.md",
		"tool_allowlist":   "shell,sessions",
		"max_retries":      "2",
		"retry_backoff_ms": "100",
		"timeout_sec":      "5",
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if !strings.Contains(out, "Created agent profile") {
		t.Fatalf("unexpected output: %s", out)
	}

	profile, ok, err := store.Get("reviewer")
	if err != nil || !ok {
		t.Fatalf("expected created profile, got ok=%v err=%v", ok, err)
	}
	if profile.MaxRetries != 2 || profile.TimeoutSec != 5 {
		t.Fatalf("unexpected numeric fields: %+v", profile)
	}
	if len(profile.ToolAllowlist) != 2 {
		t.Fatalf("unexpected allowlist: %+v", profile.ToolAllowlist)
	}
}

func TestAgentManagerSpawnRejectsDisabledProfile(t *testing.T) {
	workspace := t.TempDir()
	manager := NewAgentManager(nil, workspace, nil)
	manager.SetRunFunc(func(ctx context.Context, task *AgentTask) (string, error) {
		return "ok", nil
	})
	store := manager.ProfileStore()
	if store == nil {
		t.Fatalf("expected profile store to be available")
	}
	if _, err := store.Upsert(AgentProfile{
		AgentID: "writer",
		Status:  "disabled",
	}); err != nil {
		t.Fatalf("failed to seed profile: %v", err)
	}

	_, err := manager.Spawn(context.Background(), AgentSpawnOptions{
		Task:          "Write docs",
		AgentID:       "writer",
		Origin:        &OriginRef{Channel: "cli", ChatID: "direct"},
	})
	if err == nil {
		t.Fatalf("expected disabled profile to block spawn")
	}
}

func TestAgentManagerSpawnResolvesProfileByRole(t *testing.T) {
	workspace := t.TempDir()
	manager := NewAgentManager(nil, workspace, nil)
	store := manager.ProfileStore()
	if store == nil {
		t.Fatalf("expected profile store to be available")
	}
	if _, err := store.Upsert(AgentProfile{
		AgentID:       "coder",
		Role:          "coding",
		Status:        "active",
		ToolAllowlist: []string{"read_file"},
	}); err != nil {
		t.Fatalf("failed to seed profile: %v", err)
	}

	_, err := manager.Spawn(context.Background(), AgentSpawnOptions{
		Task:          "Implement feature",
		Role:          "coding",
		Origin:        &OriginRef{Channel: "cli", ChatID: "direct"},
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
	allowlist := ToolAllowlistFromPolicy(task.ExecutionPolicy)
	if len(allowlist) != 1 || allowlist[0] != "read_file" {
		t.Fatalf("expected allowlist from profile, got: %v", allowlist)
	}
	_ = waitAgentDone(t, manager, 4*time.Second)
}

func TestAgentProfileStoreReadsProfilesFromRuntimeConfig(t *testing.T) {
	runtimecfg.Set(config.DefaultConfig())
	t.Cleanup(func() {
		runtimecfg.Set(config.DefaultConfig())
	})

	cfg := config.DefaultConfig()
	cfg.Agents.Agents["coder"] = config.AgentConfig{
		Enabled:         true,
		DisplayName:     "Code Agent",
		Role:            "coding",
		PromptFile:      "agents/coder/AGENT.md",
		MemoryNamespace: "code-ns",
		Tools: config.AgentToolsConfig{
			Allowlist: []string{"read_file", "shell"},
		},
		Runtime: config.AgentRuntimeConfig{
			MaxRetries:     2,
			RetryBackoffMs: 2000,
			TimeoutSec:     120,
			MaxTaskChars:   4096,
			MaxResultChars: 2048,
		},
	}
	runtimecfg.Set(cfg)

	store := NewAgentProfileStore(t.TempDir())
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
	if profile.PromptFile != "agents/coder/AGENT.md" {
		t.Fatalf("expected prompt_file from config, got: %s", profile.PromptFile)
	}
	if len(profile.ToolAllowlist) != 2 {
		t.Fatalf("expected merged allowlist, got: %v", profile.ToolAllowlist)
	}
}

func TestAgentProfileStoreRejectsWritesForConfigManagedProfiles(t *testing.T) {
	runtimecfg.Set(config.DefaultConfig())
	t.Cleanup(func() {
		runtimecfg.Set(config.DefaultConfig())
	})

	cfg := config.DefaultConfig()
	cfg.Agents.Agents["tester"] = config.AgentConfig{
		Enabled:    true,
		Role:       "test",
		PromptFile: "agents/tester/AGENT.md",
	}
	runtimecfg.Set(cfg)

	store := NewAgentProfileStore(t.TempDir())
	if _, err := store.Upsert(AgentProfile{AgentID: "tester"}); err == nil {
		t.Fatalf("expected config-managed upsert to fail")
	}
	if err := store.Delete("tester"); err == nil {
		t.Fatalf("expected config-managed delete to fail")
	}
}

func TestAgentProfileStoreIncludesNodeMainBranchProfiles(t *testing.T) {
	runtimecfg.Set(config.DefaultConfig())
	t.Cleanup(func() {
		runtimecfg.Set(config.DefaultConfig())
		nodes.DefaultManager().Remove("edge-dev")
	})

	cfg := config.DefaultConfig()
	cfg.Agents.Agents["main"] = config.AgentConfig{
		Enabled:    true,
		Type:       "agent",
		PromptFile: "agents/main/AGENT.md",
	}
	runtimecfg.Set(cfg)

	nodes.DefaultManager().Upsert(nodes.NodeInfo{
		ID:     "edge-dev",
		Name:   "Edge Dev",
		Online: true,
		Agents: []nodes.AgentInfo{
			{ID: "main", DisplayName: "Main Agent", Role: "orchestrator", Type: "agent"},
			{ID: "coder", DisplayName: "Code Agent", Role: "code", Type: "agent"},
		},
		Capabilities: nodes.Capabilities{
			Model: true,
		},
	})

	store := NewAgentProfileStore(t.TempDir())
	profile, ok, err := store.Get(nodeBranchAgentID("edge-dev"))
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !ok {
		t.Fatalf("expected node-backed profile")
	}
	if profile.ManagedBy != "node_registry" || profile.Transport != "node" || profile.NodeID != "edge-dev" {
		t.Fatalf("unexpected node profile: %+v", profile)
	}
	if profile.ParentAgentID != "main" {
		t.Fatalf("expected main parent agent, got %+v", profile)
	}
	childProfile, ok, err := store.Get("node.edge-dev.coder")
	if err != nil {
		t.Fatalf("get child profile failed: %v", err)
	}
	if !ok {
		t.Fatalf("expected child node-backed profile")
	}
	if childProfile.ManagedBy != "node_registry" || childProfile.Transport != "node" || childProfile.NodeID != "edge-dev" {
		t.Fatalf("unexpected child node profile: %+v", childProfile)
	}
	if childProfile.ParentAgentID != "node.edge-dev.main" {
		t.Fatalf("expected child profile to attach to remote main, got %+v", childProfile)
	}
	if _, err := store.Upsert(AgentProfile{AgentID: profile.AgentID}); err == nil {
		t.Fatalf("expected node-managed upsert to fail")
	}
	if err := store.Delete(profile.AgentID); err == nil {
		t.Fatalf("expected node-managed delete to fail")
	}
}

func TestAgentProfileStoreExcludesLocalNodeMainBranchProfile(t *testing.T) {
	runtimecfg.Set(config.DefaultConfig())
	t.Cleanup(func() {
		runtimecfg.Set(config.DefaultConfig())
		nodes.DefaultManager().Remove("local")
	})

	cfg := config.DefaultConfig()
	cfg.Agents.Agents["main"] = config.AgentConfig{
		Enabled:    true,
		Type:       "agent",
		PromptFile: "agents/main/AGENT.md",
	}
	runtimecfg.Set(cfg)

	nodes.DefaultManager().Upsert(nodes.NodeInfo{
		ID:     "local",
		Name:   "Local",
		Online: true,
	})

	store := NewAgentProfileStore(t.TempDir())
	if profile, ok, err := store.Get(nodeBranchAgentID("local")); err != nil {
		t.Fatalf("get failed: %v", err)
	} else if ok {
		t.Fatalf("expected local node branch profile to be excluded, got %+v", profile)
	}

	items, err := store.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	for _, item := range items {
		if item.AgentID == nodeBranchAgentID("local") {
			t.Fatalf("local node branch profile should not appear in list")
		}
	}
}
