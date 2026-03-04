package scheduling

import (
	"regexp"
	"strings"
)

var (
	reBracketResourceKeys = regexp.MustCompile(`(?i)\[\s*resource[_\s-]*keys?\s*:\s*([^\]]+)\]`)
	reBracketKeys         = regexp.MustCompile(`(?i)\[\s*keys?\s*:\s*([^\]]+)\]`)
	reLineResourceKeys    = regexp.MustCompile(`(?i)^\s*resource[_\s-]*keys?\s*[:=]\s*(.+)$`)
)

// DeriveResourceKeys derives lock keys from content.
// Explicit directives win; otherwise lightweight heuristic extraction is used.
func DeriveResourceKeys(content string) []string {
	raw := strings.TrimSpace(content)
	if raw == "" {
		return nil
	}
	if explicit := ParseExplicitResourceKeys(raw); len(explicit) > 0 {
		return explicit
	}

	lower := strings.ToLower(raw)
	keys := make([]string, 0, 8)
	hasRepo := false
	for _, token := range strings.Fields(lower) {
		t := strings.Trim(token, "`'\"()[]{}:;,，。！？")
		if t == "" {
			continue
		}
		if strings.Contains(t, "gitea.") || strings.Contains(t, "github.com") || strings.Count(t, "/") >= 1 {
			if strings.Contains(t, "github.com/") || strings.Contains(t, "gitea.") {
				keys = append(keys, "repo:"+t)
				hasRepo = true
			}
		}
		if strings.Contains(t, "/") || strings.HasSuffix(t, ".go") || strings.HasSuffix(t, ".md") || strings.HasSuffix(t, ".json") || strings.HasSuffix(t, ".yaml") || strings.HasSuffix(t, ".yml") {
			keys = append(keys, "file:"+t)
		}
		if t == "main" || strings.HasPrefix(t, "branch:") {
			keys = append(keys, "branch:"+strings.TrimPrefix(t, "branch:"))
		}
	}
	if !hasRepo {
		keys = append(keys, "repo:default")
	}
	if len(keys) == 0 {
		keys = append(keys, "scope:general")
	}
	return NormalizeResourceKeys(keys)
}

// ParseExplicitResourceKeys parses directive-style keys from content.
func ParseExplicitResourceKeys(content string) []string {
	raw := strings.TrimSpace(content)
	if raw == "" {
		return nil
	}
	if m := reBracketResourceKeys.FindStringSubmatch(raw); len(m) == 2 {
		return ParseResourceKeyList(m[1])
	}
	if m := reBracketKeys.FindStringSubmatch(raw); len(m) == 2 {
		return ParseResourceKeyList(m[1])
	}
	for _, line := range strings.Split(raw, "\n") {
		m := reLineResourceKeys.FindStringSubmatch(strings.TrimSpace(line))
		if len(m) == 2 {
			return ParseResourceKeyList(m[1])
		}
	}
	return nil
}

// ExtractResourceKeysDirective returns explicit keys and content without directive text.
func ExtractResourceKeysDirective(content string) (keys []string, cleaned string, found bool) {
	raw := strings.TrimSpace(content)
	if raw == "" {
		return nil, "", false
	}
	if m := reBracketResourceKeys.FindStringSubmatch(raw); len(m) == 2 {
		keys = ParseResourceKeyList(m[1])
		if len(keys) == 0 {
			return nil, raw, false
		}
		cleaned = strings.TrimSpace(reBracketResourceKeys.ReplaceAllString(raw, ""))
		return keys, cleaned, true
	}
	if m := reBracketKeys.FindStringSubmatch(raw); len(m) == 2 {
		keys = ParseResourceKeyList(m[1])
		if len(keys) == 0 {
			return nil, raw, false
		}
		cleaned = strings.TrimSpace(reBracketKeys.ReplaceAllString(raw, ""))
		return keys, cleaned, true
	}
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		m := reLineResourceKeys.FindStringSubmatch(strings.TrimSpace(line))
		if len(m) != 2 {
			continue
		}
		keys = ParseResourceKeyList(m[1])
		if len(keys) == 0 {
			break
		}
		trimmed := make([]string, 0, len(lines)-1)
		trimmed = append(trimmed, lines[:i]...)
		trimmed = append(trimmed, lines[i+1:]...)
		cleaned = strings.TrimSpace(strings.Join(trimmed, "\n"))
		return keys, cleaned, true
	}
	return nil, raw, false
}

// ParseResourceKeyList parses comma/newline/space separated keys.
func ParseResourceKeyList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	replacer := strings.NewReplacer("\n", ",", "；", ",", ";", ",", "，", ",")
	normalized := replacer.Replace(raw)
	parts := strings.Split(normalized, ",")
	keys := make([]string, 0, len(parts))
	for _, p := range parts {
		k := strings.TrimSpace(strings.Trim(p, "`'\""))
		if k == "" {
			continue
		}
		if !strings.Contains(k, ":") {
			k = "file:" + k
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		for _, p := range strings.Fields(raw) {
			if !strings.Contains(p, ":") {
				p = "file:" + p
			}
			keys = append(keys, p)
		}
	}
	return NormalizeResourceKeys(keys)
}

// NormalizeResourceKeys lowercases, trims and deduplicates keys.
func NormalizeResourceKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	out := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		n := strings.ToLower(strings.TrimSpace(k))
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}
