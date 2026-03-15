package tools

import (
	"sort"
	"strings"
)

type ToolAllowlistGroup struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Aliases     []string `json:"aliases,omitempty"`
	Tools       []string `json:"tools"`
}

var defaultToolAllowlistGroups = []ToolAllowlistGroup{
	{
		Name:        "files_read",
		Description: "Read-only workspace file tools",
		Aliases:     []string{"file_read", "readonly_files"},
		Tools:       []string{"read_file", "list_dir", "repo_map", "read"},
	},
	{
		Name:        "files_write",
		Description: "Workspace file modification tools",
		Aliases:     []string{"file_write"},
		Tools:       []string{"write_file", "edit_file", "write", "edit"},
	},
	{
		Name:        "memory_read",
		Description: "Read-only memory tools",
		Aliases:     []string{"mem_read"},
		Tools:       []string{"memory_search", "memory_get"},
	},
	{
		Name:        "memory_write",
		Description: "Memory write tools",
		Aliases:     []string{"mem_write"},
		Tools:       []string{"memory_write"},
	},
	{
		Name:        "memory_all",
		Description: "All memory tools",
		Aliases:     []string{"memory"},
		Tools:       []string{"memory_search", "memory_get", "memory_write"},
	},
	{
		Name:        "agents",
		Description: "World/NPC and agent profile tools",
		Aliases:     []string{"agent", "agent_runtime"},
		Tools:       []string{"agent_profile", "world"},
	},
	{
		Name:        "skills",
		Description: "Skill script execution tools",
		Aliases:     []string{"skill", "skill_scripts"},
		Tools:       []string{"skill_exec"},
	},
}

func ToolAllowlistGroups() []ToolAllowlistGroup {
	out := make([]ToolAllowlistGroup, 0, len(defaultToolAllowlistGroups))
	for _, g := range defaultToolAllowlistGroups {
		item := ToolAllowlistGroup{
			Name:        strings.ToLower(strings.TrimSpace(g.Name)),
			Description: strings.TrimSpace(g.Description),
			Aliases:     normalizeAllowlistTokenList(g.Aliases),
			Tools:       normalizeAllowlistTokenList(g.Tools),
		}
		if item.Name == "" {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func ExpandToolAllowlistEntries(entries []string) []string {
	if len(entries) == 0 {
		return nil
	}
	groups := ToolAllowlistGroups()
	resolved := make(map[string][]string, len(groups))
	for _, g := range groups {
		if g.Name != "" {
			resolved[g.Name] = g.Tools
		}
		for _, alias := range g.Aliases {
			resolved[alias] = g.Tools
		}
	}

	out := map[string]struct{}{}
	for _, raw := range entries {
		token := normalizeAllowlistToken(raw)
		if token == "" {
			continue
		}
		if token == "*" || token == "all" {
			out[token] = struct{}{}
			continue
		}

		if groupName, isGroupToken := parseAllowlistGroupToken(token); isGroupToken {
			if members, ok := resolved[groupName]; ok {
				for _, name := range members {
					out[name] = struct{}{}
				}
				continue
			}
			// Keep unknown group token as-is to preserve user intent and avoid silent mutation.
			out[token] = struct{}{}
			continue
		}

		if members, ok := resolved[token]; ok {
			for _, name := range members {
				out[name] = struct{}{}
			}
			continue
		}
		out[token] = struct{}{}
	}

	if len(out) == 0 {
		return nil
	}
	result := make([]string, 0, len(out))
	for name := range out {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func parseAllowlistGroupToken(token string) (string, bool) {
	token = normalizeAllowlistToken(token)
	if token == "" {
		return "", false
	}
	if strings.HasPrefix(token, "group:") {
		v := normalizeAllowlistToken(strings.TrimPrefix(token, "group:"))
		if v != "" {
			return v, true
		}
		return "", false
	}
	if strings.HasPrefix(token, "@") {
		v := normalizeAllowlistToken(strings.TrimPrefix(token, "@"))
		if v != "" {
			return v, true
		}
		return "", false
	}
	return "", false
}

func normalizeAllowlistTokenList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, item := range in {
		v := normalizeAllowlistToken(item)
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

func normalizeAllowlistToken(in string) string {
	return strings.ToLower(strings.TrimSpace(in))
}
