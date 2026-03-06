package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"clawgo/pkg/config"
	"clawgo/pkg/runtimecfg"
)

type SubagentProfile struct {
	AgentID         string   `json:"agent_id"`
	Name            string   `json:"name"`
	Role            string   `json:"role,omitempty"`
	SystemPrompt    string   `json:"system_prompt,omitempty"`
	ToolAllowlist   []string `json:"tool_allowlist,omitempty"`
	MemoryNamespace string   `json:"memory_namespace,omitempty"`
	MaxRetries      int      `json:"max_retries,omitempty"`
	RetryBackoff    int      `json:"retry_backoff_ms,omitempty"`
	TimeoutSec      int      `json:"timeout_sec,omitempty"`
	MaxTaskChars    int      `json:"max_task_chars,omitempty"`
	MaxResultChars  int      `json:"max_result_chars,omitempty"`
	Status          string   `json:"status"`
	CreatedAt       int64    `json:"created_at"`
	UpdatedAt       int64    `json:"updated_at"`
	ManagedBy       string   `json:"managed_by,omitempty"`
}

type SubagentProfileStore struct {
	workspace string
	mu        sync.RWMutex
}

func NewSubagentProfileStore(workspace string) *SubagentProfileStore {
	return &SubagentProfileStore{workspace: strings.TrimSpace(workspace)}
}

func (s *SubagentProfileStore) profilesDir() string {
	return filepath.Join(s.workspace, "agents", "profiles")
}

func (s *SubagentProfileStore) profilePath(agentID string) string {
	id := normalizeSubagentIdentifier(agentID)
	return filepath.Join(s.profilesDir(), id+".json")
}

func (s *SubagentProfileStore) List() ([]SubagentProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	merged, err := s.mergedProfilesLocked()
	if err != nil {
		return nil, err
	}
	out := make([]SubagentProfile, 0, len(merged))
	for _, p := range merged {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].AgentID < out[j].AgentID
	})
	return out, nil
}

func (s *SubagentProfileStore) Get(agentID string) (*SubagentProfile, bool, error) {
	id := normalizeSubagentIdentifier(agentID)
	if id == "" {
		return nil, false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	merged, err := s.mergedProfilesLocked()
	if err != nil {
		return nil, false, err
	}
	p, ok := merged[id]
	if !ok {
		return nil, false, nil
	}
	cp := p
	return &cp, true, nil
}

func (s *SubagentProfileStore) FindByRole(role string) (*SubagentProfile, bool, error) {
	target := strings.ToLower(strings.TrimSpace(role))
	if target == "" {
		return nil, false, nil
	}
	items, err := s.List()
	if err != nil {
		return nil, false, err
	}
	for _, p := range items {
		if strings.ToLower(strings.TrimSpace(p.Role)) == target {
			cp := p
			return &cp, true, nil
		}
	}
	return nil, false, nil
}

func (s *SubagentProfileStore) Upsert(profile SubagentProfile) (*SubagentProfile, error) {
	p := normalizeSubagentProfile(profile)
	if p.AgentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if managed, ok := s.configProfileLocked(p.AgentID); ok {
		return nil, fmt.Errorf("subagent profile %q is managed by %s", p.AgentID, managed.ManagedBy)
	}

	now := time.Now().UnixMilli()
	path := s.profilePath(p.AgentID)
	existing := SubagentProfile{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &existing)
	}
	existing = normalizeSubagentProfile(existing)
	if existing.CreatedAt > 0 {
		p.CreatedAt = existing.CreatedAt
	} else if p.CreatedAt <= 0 {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	if err := os.MkdirAll(s.profilesDir(), 0755); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *SubagentProfileStore) Delete(agentID string) error {
	id := normalizeSubagentIdentifier(agentID)
	if id == "" {
		return fmt.Errorf("agent_id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if managed, ok := s.configProfileLocked(id); ok {
		return fmt.Errorf("subagent profile %q is managed by %s", id, managed.ManagedBy)
	}

	err := os.Remove(s.profilePath(id))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func normalizeSubagentProfile(in SubagentProfile) SubagentProfile {
	p := in
	p.AgentID = normalizeSubagentIdentifier(p.AgentID)
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		p.Name = p.AgentID
	}
	p.Role = strings.TrimSpace(p.Role)
	p.SystemPrompt = strings.TrimSpace(p.SystemPrompt)
	p.MemoryNamespace = normalizeSubagentIdentifier(p.MemoryNamespace)
	if p.MemoryNamespace == "" {
		p.MemoryNamespace = p.AgentID
	}
	p.Status = normalizeProfileStatus(p.Status)
	p.ToolAllowlist = normalizeToolAllowlist(p.ToolAllowlist)
	p.ManagedBy = strings.TrimSpace(p.ManagedBy)
	p.MaxRetries = clampInt(p.MaxRetries, 0, 8)
	p.RetryBackoff = clampInt(p.RetryBackoff, 500, 120000)
	p.TimeoutSec = clampInt(p.TimeoutSec, 0, 3600)
	p.MaxTaskChars = clampInt(p.MaxTaskChars, 0, 400000)
	p.MaxResultChars = clampInt(p.MaxResultChars, 0, 400000)
	return p
}

func normalizeProfileStatus(s string) string {
	v := strings.ToLower(strings.TrimSpace(s))
	switch v {
	case "active", "disabled":
		return v
	default:
		return "active"
	}
}

func normalizeStringList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		v := strings.TrimSpace(item)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func normalizeToolAllowlist(in []string) []string {
	items := ExpandToolAllowlistEntries(normalizeStringList(in))
	if len(items) == 0 {
		return nil
	}
	items = normalizeStringList(items)
	sort.Strings(items)
	return items
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if max > 0 && v > max {
		return max
	}
	return v
}

func parseStringList(raw interface{}) []string {
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, _ := item.(string)
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return normalizeStringList(out)
}

func (s *SubagentProfileStore) mergedProfilesLocked() (map[string]SubagentProfile, error) {
	merged := make(map[string]SubagentProfile)
	for _, p := range s.configProfilesLocked() {
		merged[p.AgentID] = p
	}
	fileProfiles, err := s.fileProfilesLocked()
	if err != nil {
		return nil, err
	}
	for _, p := range fileProfiles {
		if _, exists := merged[p.AgentID]; exists {
			continue
		}
		merged[p.AgentID] = p
	}
	return merged, nil
}

func (s *SubagentProfileStore) fileProfilesLocked() ([]SubagentProfile, error) {
	dir := s.profilesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []SubagentProfile{}, nil
		}
		return nil, err
	}
	out := make([]SubagentProfile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var p SubagentProfile
		if err := json.Unmarshal(b, &p); err != nil {
			continue
		}
		out = append(out, normalizeSubagentProfile(p))
	}
	return out, nil
}

func (s *SubagentProfileStore) configProfilesLocked() []SubagentProfile {
	cfg := runtimecfg.Get()
	if cfg == nil || len(cfg.Agents.Subagents) == 0 {
		return nil
	}
	out := make([]SubagentProfile, 0, len(cfg.Agents.Subagents))
	for agentID, subcfg := range cfg.Agents.Subagents {
		profile := profileFromConfig(agentID, subcfg)
		if profile.AgentID == "" {
			continue
		}
		out = append(out, profile)
	}
	return out
}

func (s *SubagentProfileStore) configProfileLocked(agentID string) (SubagentProfile, bool) {
	id := normalizeSubagentIdentifier(agentID)
	if id == "" {
		return SubagentProfile{}, false
	}
	cfg := runtimecfg.Get()
	if cfg == nil {
		return SubagentProfile{}, false
	}
	subcfg, ok := cfg.Agents.Subagents[id]
	if !ok {
		return SubagentProfile{}, false
	}
	return profileFromConfig(id, subcfg), true
}

func profileFromConfig(agentID string, subcfg config.SubagentConfig) SubagentProfile {
	status := "active"
	if !subcfg.Enabled {
		status = "disabled"
	}
	return normalizeSubagentProfile(SubagentProfile{
		AgentID:         agentID,
		Name:            strings.TrimSpace(subcfg.DisplayName),
		Role:            strings.TrimSpace(subcfg.Role),
		SystemPrompt:    strings.TrimSpace(subcfg.SystemPrompt),
		ToolAllowlist:   append([]string(nil), subcfg.Tools.Allowlist...),
		MemoryNamespace: strings.TrimSpace(subcfg.MemoryNamespace),
		MaxRetries:      subcfg.Runtime.MaxRetries,
		RetryBackoff:    subcfg.Runtime.RetryBackoffMs,
		TimeoutSec:      subcfg.Runtime.TimeoutSec,
		MaxTaskChars:    subcfg.Runtime.MaxTaskChars,
		MaxResultChars:  subcfg.Runtime.MaxResultChars,
		Status:          status,
		ManagedBy:       "config.json",
	})
}

type SubagentProfileTool struct {
	store *SubagentProfileStore
}

func NewSubagentProfileTool(store *SubagentProfileStore) *SubagentProfileTool {
	return &SubagentProfileTool{store: store}
}

func (t *SubagentProfileTool) Name() string { return "subagent_profile" }

func (t *SubagentProfileTool) Description() string {
	return "Manage subagent profiles: create/list/get/update/enable/disable/delete."
}

func (t *SubagentProfileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string", "description": "create|list|get|update|enable|disable|delete"},
			"agent_id": map[string]interface{}{
				"type":        "string",
				"description": "Unique subagent id, e.g. coder/writer/tester",
			},
			"name":             map[string]interface{}{"type": "string"},
			"role":             map[string]interface{}{"type": "string"},
			"system_prompt":    map[string]interface{}{"type": "string"},
			"memory_namespace": map[string]interface{}{"type": "string"},
			"status":           map[string]interface{}{"type": "string", "description": "active|disabled"},
			"tool_allowlist": map[string]interface{}{
				"type":        "array",
				"description": "Tool allowlist entries. Supports tool names, '*'/'all', and grouped tokens like 'group:files_read' or '@pipeline'.",
				"items":       map[string]interface{}{"type": "string"},
			},
			"max_retries":      map[string]interface{}{"type": "integer", "description": "Retry limit for subagent task execution."},
			"retry_backoff_ms": map[string]interface{}{"type": "integer", "description": "Backoff between retries in milliseconds."},
			"timeout_sec":      map[string]interface{}{"type": "integer", "description": "Per-attempt timeout in seconds."},
			"max_task_chars":   map[string]interface{}{"type": "integer", "description": "Task input size quota (characters)."},
			"max_result_chars": map[string]interface{}{"type": "integer", "description": "Result output size quota (characters)."},
		},
		"required": []string{"action"},
	}
}

func (t *SubagentProfileTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	_ = ctx
	if t.store == nil {
		return "subagent profile store not available", nil
	}
	action, _ := args["action"].(string)
	action = strings.ToLower(strings.TrimSpace(action))
	agentID, _ := args["agent_id"].(string)
	agentID = normalizeSubagentIdentifier(agentID)

	switch action {
	case "list":
		items, err := t.store.List()
		if err != nil {
			return "", err
		}
		if len(items) == 0 {
			return "No subagent profiles.", nil
		}
		var sb strings.Builder
		sb.WriteString("Subagent Profiles:\n")
		for i, p := range items {
			sb.WriteString(fmt.Sprintf("- #%d %s [%s] role=%s memory_ns=%s\n", i+1, p.AgentID, p.Status, p.Role, p.MemoryNamespace))
		}
		return strings.TrimSpace(sb.String()), nil
	case "get":
		if agentID == "" {
			return "agent_id is required", nil
		}
		p, ok, err := t.store.Get(agentID)
		if err != nil {
			return "", err
		}
		if !ok {
			return "subagent profile not found", nil
		}
		b, _ := json.MarshalIndent(p, "", "  ")
		return string(b), nil
	case "create":
		if agentID == "" {
			return "agent_id is required", nil
		}
		if _, ok, err := t.store.Get(agentID); err != nil {
			return "", err
		} else if ok {
			return "subagent profile already exists", nil
		}
		p := SubagentProfile{
			AgentID:         agentID,
			Name:            stringArg(args, "name"),
			Role:            stringArg(args, "role"),
			SystemPrompt:    stringArg(args, "system_prompt"),
			MemoryNamespace: stringArg(args, "memory_namespace"),
			Status:          stringArg(args, "status"),
			ToolAllowlist:   parseStringList(args["tool_allowlist"]),
			MaxRetries:      profileIntArg(args, "max_retries"),
			RetryBackoff:    profileIntArg(args, "retry_backoff_ms"),
			TimeoutSec:      profileIntArg(args, "timeout_sec"),
			MaxTaskChars:    profileIntArg(args, "max_task_chars"),
			MaxResultChars:  profileIntArg(args, "max_result_chars"),
		}
		saved, err := t.store.Upsert(p)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Created subagent profile: %s (role=%s status=%s)", saved.AgentID, saved.Role, saved.Status), nil
	case "update":
		if agentID == "" {
			return "agent_id is required", nil
		}
		existing, ok, err := t.store.Get(agentID)
		if err != nil {
			return "", err
		}
		if !ok {
			return "subagent profile not found", nil
		}
		next := *existing
		if _, ok := args["name"]; ok {
			next.Name = stringArg(args, "name")
		}
		if _, ok := args["role"]; ok {
			next.Role = stringArg(args, "role")
		}
		if _, ok := args["system_prompt"]; ok {
			next.SystemPrompt = stringArg(args, "system_prompt")
		}
		if _, ok := args["memory_namespace"]; ok {
			next.MemoryNamespace = stringArg(args, "memory_namespace")
		}
		if _, ok := args["status"]; ok {
			next.Status = stringArg(args, "status")
		}
		if _, ok := args["tool_allowlist"]; ok {
			next.ToolAllowlist = parseStringList(args["tool_allowlist"])
		}
		if _, ok := args["max_retries"]; ok {
			next.MaxRetries = profileIntArg(args, "max_retries")
		}
		if _, ok := args["retry_backoff_ms"]; ok {
			next.RetryBackoff = profileIntArg(args, "retry_backoff_ms")
		}
		if _, ok := args["timeout_sec"]; ok {
			next.TimeoutSec = profileIntArg(args, "timeout_sec")
		}
		if _, ok := args["max_task_chars"]; ok {
			next.MaxTaskChars = profileIntArg(args, "max_task_chars")
		}
		if _, ok := args["max_result_chars"]; ok {
			next.MaxResultChars = profileIntArg(args, "max_result_chars")
		}
		saved, err := t.store.Upsert(next)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Updated subagent profile: %s (role=%s status=%s)", saved.AgentID, saved.Role, saved.Status), nil
	case "enable", "disable":
		if agentID == "" {
			return "agent_id is required", nil
		}
		existing, ok, err := t.store.Get(agentID)
		if err != nil {
			return "", err
		}
		if !ok {
			return "subagent profile not found", nil
		}
		if action == "enable" {
			existing.Status = "active"
		} else {
			existing.Status = "disabled"
		}
		saved, err := t.store.Upsert(*existing)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Subagent profile %s set to %s", saved.AgentID, saved.Status), nil
	case "delete":
		if agentID == "" {
			return "agent_id is required", nil
		}
		if err := t.store.Delete(agentID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Deleted subagent profile: %s", agentID), nil
	default:
		return "unsupported action", nil
	}
}

func stringArg(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func profileIntArg(args map[string]interface{}, key string) int {
	if args == nil {
		return 0
	}
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}
