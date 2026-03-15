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

	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/nodes"
	"github.com/YspCoder/clawgo/pkg/runtimecfg"
)

type AgentProfile struct {
	AgentID          string   `json:"agent_id"`
	Name             string   `json:"name"`
	Kind             string   `json:"kind,omitempty"`
	Transport        string   `json:"transport,omitempty"`
	NodeID           string   `json:"node_id,omitempty"`
	ParentAgentID    string   `json:"parent_agent_id,omitempty"`
	Role             string   `json:"role,omitempty"`
	Persona          string   `json:"persona,omitempty"`
	Traits           []string `json:"traits,omitempty"`
	Faction          string   `json:"faction,omitempty"`
	HomeLocation     string   `json:"home_location,omitempty"`
	DefaultGoals     []string `json:"default_goals,omitempty"`
	PerceptionScope  int      `json:"perception_scope,omitempty"`
	ScheduleHint     string   `json:"schedule_hint,omitempty"`
	WorldTags        []string `json:"world_tags,omitempty"`
	PromptFile       string   `json:"prompt_file,omitempty"`
	ToolAllowlist    []string `json:"tool_allowlist,omitempty"`
	MemoryNamespace  string   `json:"memory_namespace,omitempty"`
	MaxRetries       int      `json:"max_retries,omitempty"`
	RetryBackoff     int      `json:"retry_backoff_ms,omitempty"`
	TimeoutSec       int      `json:"timeout_sec,omitempty"`
	MaxTaskChars     int      `json:"max_task_chars,omitempty"`
	MaxResultChars   int      `json:"max_result_chars,omitempty"`
	Status           string   `json:"status"`
	CreatedAt        int64    `json:"created_at"`
	UpdatedAt        int64    `json:"updated_at"`
	ManagedBy        string   `json:"managed_by,omitempty"`
}

type AgentProfileStore struct {
	workspace string
	mu        sync.RWMutex
}

func NewAgentProfileStore(workspace string) *AgentProfileStore {
	return &AgentProfileStore{workspace: strings.TrimSpace(workspace)}
}

func (s *AgentProfileStore) profilesDir() string {
	return filepath.Join(s.workspace, "agents", "profiles")
}

func (s *AgentProfileStore) profilePath(agentID string) string {
	id := normalizeAgentIdentifier(agentID)
	return filepath.Join(s.profilesDir(), id+".json")
}

func (s *AgentProfileStore) List() ([]AgentProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	merged, err := s.mergedProfilesLocked()
	if err != nil {
		return nil, err
	}
	out := make([]AgentProfile, 0, len(merged))
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

func (s *AgentProfileStore) Get(agentID string) (*AgentProfile, bool, error) {
	id := normalizeAgentIdentifier(agentID)
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

func (s *AgentProfileStore) FindByRole(role string) (*AgentProfile, bool, error) {
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

func (s *AgentProfileStore) Upsert(profile AgentProfile) (*AgentProfile, error) {
	p := normalizeAgentProfile(profile)
	if p.AgentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if managed, ok := s.configProfileLocked(p.AgentID); ok {
		return nil, fmt.Errorf("agent profile %q is managed by %s", p.AgentID, managed.ManagedBy)
	}
	if managed, ok := s.nodeProfileLocked(p.AgentID); ok {
		return nil, fmt.Errorf("agent profile %q is managed by %s", p.AgentID, managed.ManagedBy)
	}

	now := time.Now().UnixMilli()
	path := s.profilePath(p.AgentID)
	existing := AgentProfile{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &existing)
	}
	existing = normalizeAgentProfile(existing)
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

func (s *AgentProfileStore) Delete(agentID string) error {
	id := normalizeAgentIdentifier(agentID)
	if id == "" {
		return fmt.Errorf("agent_id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if managed, ok := s.configProfileLocked(id); ok {
		return fmt.Errorf("agent profile %q is managed by %s", id, managed.ManagedBy)
	}
	if managed, ok := s.nodeProfileLocked(id); ok {
		return fmt.Errorf("agent profile %q is managed by %s", id, managed.ManagedBy)
	}

	err := os.Remove(s.profilePath(id))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func normalizeAgentProfile(in AgentProfile) AgentProfile {
	p := in
	p.AgentID = normalizeAgentIdentifier(p.AgentID)
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		p.Name = p.AgentID
	}
	p.Kind = normalizeProfileKind(p.Kind)
	p.Transport = normalizeProfileTransport(p.Transport)
	p.NodeID = strings.TrimSpace(p.NodeID)
	p.ParentAgentID = normalizeAgentIdentifier(p.ParentAgentID)
	p.Role = strings.TrimSpace(p.Role)
	p.Persona = strings.TrimSpace(p.Persona)
	p.Traits = normalizeStringList(p.Traits)
	p.Faction = strings.TrimSpace(p.Faction)
	p.HomeLocation = normalizeAgentIdentifier(p.HomeLocation)
	p.DefaultGoals = normalizeStringList(p.DefaultGoals)
	p.PerceptionScope = clampInt(p.PerceptionScope, 0, 10)
	p.ScheduleHint = strings.TrimSpace(p.ScheduleHint)
	p.WorldTags = normalizeStringList(p.WorldTags)
	p.PromptFile = strings.TrimSpace(p.PromptFile)
	p.MemoryNamespace = normalizeAgentIdentifier(p.MemoryNamespace)
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

func normalizeProfileTransport(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "local":
		return "local"
	case "node":
		return "node"
	default:
		return "local"
	}
}

func normalizeProfileKind(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "npc":
		return "npc"
	case "agent", "tool":
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return "npc"
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
	return normalizeStringList(MapStringListArg(map[string]interface{}{"items": raw}, "items"))
}

func (s *AgentProfileStore) mergedProfilesLocked() (map[string]AgentProfile, error) {
	merged := make(map[string]AgentProfile)
	for _, p := range s.configProfilesLocked() {
		merged[p.AgentID] = p
	}
	for _, p := range s.nodeProfilesLocked() {
		if _, exists := merged[p.AgentID]; exists {
			continue
		}
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

func (s *AgentProfileStore) fileProfilesLocked() ([]AgentProfile, error) {
	dir := s.profilesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []AgentProfile{}, nil
		}
		return nil, err
	}
	out := make([]AgentProfile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var p AgentProfile
		if err := json.Unmarshal(b, &p); err != nil {
			continue
		}
		out = append(out, normalizeAgentProfile(p))
	}
	return out, nil
}

func (s *AgentProfileStore) configProfilesLocked() []AgentProfile {
	cfg := runtimecfg.Get()
	if cfg == nil || len(cfg.Agents.Agents) == 0 {
		return nil
	}
	out := make([]AgentProfile, 0, len(cfg.Agents.Agents))
	for agentID, subcfg := range cfg.Agents.Agents {
		profile := profileFromConfig(agentID, subcfg)
		if profile.AgentID == "" {
			continue
		}
		out = append(out, profile)
	}
	return out
}

func (s *AgentProfileStore) configProfileLocked(agentID string) (AgentProfile, bool) {
	id := normalizeAgentIdentifier(agentID)
	if id == "" {
		return AgentProfile{}, false
	}
	cfg := runtimecfg.Get()
	if cfg == nil {
		return AgentProfile{}, false
	}
	subcfg, ok := cfg.Agents.Agents[id]
	if !ok {
		return AgentProfile{}, false
	}
	return profileFromConfig(id, subcfg), true
}

func (s *AgentProfileStore) nodeProfileLocked(agentID string) (AgentProfile, bool) {
	id := normalizeAgentIdentifier(agentID)
	if id == "" {
		return AgentProfile{}, false
	}
	cfg := runtimecfg.Get()
	parentAgentID := "main"
	_ = cfg
	for _, node := range nodes.DefaultManager().List() {
		if isLocalNode(node.ID) {
			continue
		}
		for _, profile := range profilesFromNode(node, parentAgentID) {
			if profile.AgentID == id {
				return profile, true
			}
		}
	}
	return AgentProfile{}, false
}

func profileFromConfig(agentID string, subcfg config.AgentConfig) AgentProfile {
	status := "active"
	if !subcfg.Enabled {
		status = "disabled"
	}
	return normalizeAgentProfile(AgentProfile{
		AgentID:          agentID,
		Name:             strings.TrimSpace(subcfg.DisplayName),
		Kind:             strings.TrimSpace(subcfg.Kind),
		Transport:        strings.TrimSpace(subcfg.Transport),
		NodeID:           strings.TrimSpace(subcfg.NodeID),
		ParentAgentID:    strings.TrimSpace(subcfg.ParentAgentID),
		Role:             strings.TrimSpace(subcfg.Role),
		Persona:          strings.TrimSpace(subcfg.Persona),
		Traits:           append([]string(nil), subcfg.Traits...),
		Faction:          strings.TrimSpace(subcfg.Faction),
		HomeLocation:     strings.TrimSpace(subcfg.HomeLocation),
		DefaultGoals:     append([]string(nil), subcfg.DefaultGoals...),
		PerceptionScope:  subcfg.PerceptionScope,
		ScheduleHint:     strings.TrimSpace(subcfg.ScheduleHint),
		WorldTags:        append([]string(nil), subcfg.WorldTags...),
		PromptFile:       strings.TrimSpace(subcfg.PromptFile),
		ToolAllowlist:    append([]string(nil), subcfg.Tools.Allowlist...),
		MemoryNamespace:  strings.TrimSpace(subcfg.MemoryNamespace),
		MaxRetries:       subcfg.Runtime.MaxRetries,
		RetryBackoff:     subcfg.Runtime.RetryBackoffMs,
		TimeoutSec:       subcfg.Runtime.TimeoutSec,
		MaxTaskChars:     subcfg.Runtime.MaxTaskChars,
		MaxResultChars:   subcfg.Runtime.MaxResultChars,
		Status:           status,
		ManagedBy:        "config.json",
	})
}

func (s *AgentProfileStore) nodeProfilesLocked() []AgentProfile {
	nodeItems := nodes.DefaultManager().List()
	if len(nodeItems) == 0 {
		return nil
	}
	cfg := runtimecfg.Get()
	parentAgentID := "main"
	_ = cfg
	out := make([]AgentProfile, 0, len(nodeItems))
	for _, node := range nodeItems {
		if isLocalNode(node.ID) {
			continue
		}
		profiles := profilesFromNode(node, parentAgentID)
		for _, profile := range profiles {
			if profile.AgentID == "" {
				continue
			}
			out = append(out, profile)
		}
	}
	return out
}

func profilesFromNode(node nodes.NodeInfo, parentAgentID string) []AgentProfile {
	name := strings.TrimSpace(node.Name)
	if name == "" {
		name = strings.TrimSpace(node.ID)
	}
	status := "active"
	if !node.Online {
		status = "disabled"
	}
	rootAgentID := nodeBranchAgentID(node.ID)
	if rootAgentID == "" {
		return nil
	}
	out := []AgentProfile{normalizeAgentProfile(AgentProfile{
		AgentID:         rootAgentID,
		Name:            name + " Main Agent",
		Transport:       "node",
		NodeID:          strings.TrimSpace(node.ID),
		ParentAgentID:   parentAgentID,
		Role:            "remote_main",
		MemoryNamespace: rootAgentID,
		Status:          status,
		ManagedBy:       "node_registry",
	})}
	for _, agent := range node.Agents {
		agentID := normalizeAgentIdentifier(agent.ID)
		if agentID == "" || agentID == "main" {
			continue
		}
		out = append(out, normalizeAgentProfile(AgentProfile{
			AgentID:         nodeChildAgentID(node.ID, agentID),
			Name:            nodeChildAgentDisplayName(name, agent),
			Transport:       "node",
			NodeID:          strings.TrimSpace(node.ID),
			ParentAgentID:   rootAgentID,
			Role:            strings.TrimSpace(agent.Role),
			MemoryNamespace: nodeChildAgentID(node.ID, agentID),
			Status:          status,
			ManagedBy:       "node_registry",
		}))
	}
	return out
}

func nodeBranchAgentID(nodeID string) string {
	id := normalizeAgentIdentifier(nodeID)
	if id == "" {
		return ""
	}
	return "node." + id + ".main"
}

func nodeChildAgentID(nodeID, agentID string) string {
	nodeID = normalizeAgentIdentifier(nodeID)
	agentID = normalizeAgentIdentifier(agentID)
	if nodeID == "" || agentID == "" {
		return ""
	}
	return "node." + nodeID + "." + agentID
}

func nodeChildAgentDisplayName(nodeName string, agent nodes.AgentInfo) string {
	base := strings.TrimSpace(agent.DisplayName)
	if base == "" {
		base = strings.TrimSpace(agent.ID)
	}
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return base
	}
	return nodeName + " / " + base
}

func isLocalNode(nodeID string) bool {
	return normalizeAgentIdentifier(nodeID) == "local"
}

type AgentProfileTool struct {
	store *AgentProfileStore
}

func NewAgentProfileTool(store *AgentProfileStore) *AgentProfileTool {
	return &AgentProfileTool{store: store}
}

func (t *AgentProfileTool) Name() string { return "agent_profile" }

func (t *AgentProfileTool) Description() string {
	return "Manage agent profiles: create/list/get/update/enable/disable/delete."
}

func (t *AgentProfileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string", "description": "create|list|get|update|enable|disable|delete"},
			"agent_id": map[string]interface{}{
				"type":        "string",
				"description": "Unique agent id, e.g. coder/writer/tester",
			},
			"name":               map[string]interface{}{"type": "string"},
			"kind":               map[string]interface{}{"type": "string", "description": "agent|npc|tool"},
			"role":               map[string]interface{}{"type": "string"},
			"persona":            map[string]interface{}{"type": "string"},
			"traits":             map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"faction":            map[string]interface{}{"type": "string"},
			"home_location":      map[string]interface{}{"type": "string"},
			"default_goals":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"perception_scope":   map[string]interface{}{"type": "integer"},
			"schedule_hint":      map[string]interface{}{"type": "string"},
			"world_tags":         map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"prompt_file":        map[string]interface{}{"type": "string"},
			"memory_namespace":   map[string]interface{}{"type": "string"},
			"status":             map[string]interface{}{"type": "string", "description": "active|disabled"},
			"tool_allowlist": map[string]interface{}{
				"type":        "array",
				"description": "Tool allowlist entries. Supports tool names, '*'/'all', and grouped tokens like 'group:files_read'.",
				"items":       map[string]interface{}{"type": "string"},
			},
			"max_retries":      map[string]interface{}{"type": "integer", "description": "Retry limit for agent task execution."},
			"retry_backoff_ms": map[string]interface{}{"type": "integer", "description": "Backoff between retries in milliseconds."},
			"timeout_sec":      map[string]interface{}{"type": "integer", "description": "Per-attempt timeout in seconds."},
			"max_task_chars":   map[string]interface{}{"type": "integer", "description": "Task input size quota (characters)."},
			"max_result_chars": map[string]interface{}{"type": "integer", "description": "Result output size quota (characters)."},
		},
		"required": []string{"action"},
	}
}

func (t *AgentProfileTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	_ = ctx
	if t.store == nil {
		return "agent profile store not available", nil
	}
	action := strings.ToLower(MapStringArg(args, "action"))
	agentID := normalizeAgentIdentifier(MapStringArg(args, "agent_id"))
	type agentProfileActionHandler func() (string, error)
	handlers := map[string]agentProfileActionHandler{
		"list": func() (string, error) {
			items, err := t.store.List()
			if err != nil {
				return "", err
			}
			if len(items) == 0 {
				return "No agent profiles.", nil
			}
			var sb strings.Builder
			sb.WriteString("Agent Profiles:\n")
			for i, p := range items {
				sb.WriteString(fmt.Sprintf("- #%d %s [%s] role=%s memory_ns=%s\n", i+1, p.AgentID, p.Status, p.Role, p.MemoryNamespace))
			}
			return strings.TrimSpace(sb.String()), nil
		},
		"get": func() (string, error) {
			if agentID == "" {
				return "agent_id is required", nil
			}
			p, ok, err := t.store.Get(agentID)
			if err != nil {
				return "", err
			}
			if !ok {
				return "agent profile not found", nil
			}
			b, _ := json.MarshalIndent(p, "", "  ")
			return string(b), nil
		},
		"create": func() (string, error) {
			if agentID == "" {
				return "agent_id is required", nil
			}
			if _, ok, err := t.store.Get(agentID); err != nil {
				return "", err
			} else if ok {
				return "agent profile already exists", nil
			}
			p := AgentProfile{
				AgentID:          agentID,
				Name:             stringArg(args, "name"),
				Kind:             stringArg(args, "kind"),
				Role:             stringArg(args, "role"),
				Persona:          stringArg(args, "persona"),
				Traits:           parseStringList(args["traits"]),
				Faction:          stringArg(args, "faction"),
				HomeLocation:     stringArg(args, "home_location"),
				DefaultGoals:     parseStringList(args["default_goals"]),
				PerceptionScope:  profileIntArg(args, "perception_scope"),
				ScheduleHint:     stringArg(args, "schedule_hint"),
				WorldTags:        parseStringList(args["world_tags"]),
				PromptFile:       stringArg(args, "prompt_file"),
				MemoryNamespace:  stringArg(args, "memory_namespace"),
				Status:           stringArg(args, "status"),
				ToolAllowlist:    parseStringList(args["tool_allowlist"]),
				MaxRetries:       profileIntArg(args, "max_retries"),
				RetryBackoff:     profileIntArg(args, "retry_backoff_ms"),
				TimeoutSec:       profileIntArg(args, "timeout_sec"),
				MaxTaskChars:     profileIntArg(args, "max_task_chars"),
				MaxResultChars:   profileIntArg(args, "max_result_chars"),
			}
			saved, err := t.store.Upsert(p)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Created agent profile: %s (role=%s status=%s)", saved.AgentID, saved.Role, saved.Status), nil
		},
		"update": func() (string, error) {
			if agentID == "" {
				return "agent_id is required", nil
			}
			existing, ok, err := t.store.Get(agentID)
			if err != nil {
				return "", err
			}
			if !ok {
				return "agent profile not found", nil
			}
			next := *existing
			if _, ok := args["name"]; ok {
				next.Name = stringArg(args, "name")
			}
			if _, ok := args["role"]; ok {
				next.Role = stringArg(args, "role")
			}
			if _, ok := args["kind"]; ok {
				next.Kind = stringArg(args, "kind")
			}
			if _, ok := args["persona"]; ok {
				next.Persona = stringArg(args, "persona")
			}
			if _, ok := args["traits"]; ok {
				next.Traits = parseStringList(args["traits"])
			}
			if _, ok := args["faction"]; ok {
				next.Faction = stringArg(args, "faction")
			}
			if _, ok := args["home_location"]; ok {
				next.HomeLocation = stringArg(args, "home_location")
			}
			if _, ok := args["default_goals"]; ok {
				next.DefaultGoals = parseStringList(args["default_goals"])
			}
			if _, ok := args["perception_scope"]; ok {
				next.PerceptionScope = profileIntArg(args, "perception_scope")
			}
			if _, ok := args["schedule_hint"]; ok {
				next.ScheduleHint = stringArg(args, "schedule_hint")
			}
			if _, ok := args["world_tags"]; ok {
				next.WorldTags = parseStringList(args["world_tags"])
			}
			if _, ok := args["prompt_file"]; ok {
				next.PromptFile = stringArg(args, "prompt_file")
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
			return fmt.Sprintf("Updated agent profile: %s (role=%s status=%s)", saved.AgentID, saved.Role, saved.Status), nil
		},
		"enable": func() (string, error) {
			if agentID == "" {
				return "agent_id is required", nil
			}
			existing, ok, err := t.store.Get(agentID)
			if err != nil {
				return "", err
			}
			if !ok {
				return "agent profile not found", nil
			}
			existing.Status = "active"
			saved, err := t.store.Upsert(*existing)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Agent profile %s set to %s", saved.AgentID, saved.Status), nil
		},
		"disable": func() (string, error) {
			if agentID == "" {
				return "agent_id is required", nil
			}
			existing, ok, err := t.store.Get(agentID)
			if err != nil {
				return "", err
			}
			if !ok {
				return "agent profile not found", nil
			}
			existing.Status = "disabled"
			saved, err := t.store.Upsert(*existing)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Agent profile %s set to %s", saved.AgentID, saved.Status), nil
		},
		"delete": func() (string, error) {
			if agentID == "" {
				return "agent_id is required", nil
			}
			if err := t.store.Delete(agentID); err != nil {
				return "", err
			}
			return fmt.Sprintf("Deleted agent profile: %s", agentID), nil
		},
	}
	if handler := handlers[action]; handler != nil {
		return handler()
	}
	return "unsupported action", nil
}

func stringArg(args map[string]interface{}, key string) string {
	return MapStringArg(args, key)
}

func profileIntArg(args map[string]interface{}, key string) int {
	return MapIntArg(args, key, 0)
}
