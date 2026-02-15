package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type RepoMapTool struct {
	workspace string
}

func NewRepoMapTool(workspace string) *RepoMapTool {
	return &RepoMapTool{workspace: workspace}
}

func (t *RepoMapTool) Name() string {
	return "repo_map"
}

func (t *RepoMapTool) Description() string {
	return "Build and query repository map to quickly locate target files/symbols before reading source."
}

func (t *RepoMapTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search file path or symbol keyword",
			},
			"max_results": map[string]interface{}{
				"type":        "integer",
				"default":     20,
				"description": "Maximum results to return",
			},
			"refresh": map[string]interface{}{
				"type":        "boolean",
				"description": "Force rebuild map cache",
				"default":     false,
			},
		},
	}
}

type repoMapCache struct {
	Workspace string         `json:"workspace"`
	UpdatedAt int64          `json:"updated_at"`
	Files     []repoMapEntry `json:"files"`
}

type repoMapEntry struct {
	Path    string   `json:"path"`
	Lang    string   `json:"lang"`
	Size    int64    `json:"size"`
	ModTime int64    `json:"mod_time"`
	Symbols []string `json:"symbols,omitempty"`
}

func (t *RepoMapTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	query, _ := args["query"].(string)
	maxResults := 20
	if raw, ok := args["max_results"].(float64); ok && raw > 0 {
		maxResults = int(raw)
	}
	forceRefresh, _ := args["refresh"].(bool)

	cache, err := t.loadOrBuildMap(forceRefresh)
	if err != nil {
		return "", err
	}
	if len(cache.Files) == 0 {
		return "Repo map is empty.", nil
	}

	results := t.filterRepoMap(cache.Files, query)
	if len(results) > maxResults {
		results = results[:maxResults]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Repo Map (updated: %s)\n", time.UnixMilli(cache.UpdatedAt).Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("Workspace: %s\n", cache.Workspace))
	sb.WriteString(fmt.Sprintf("Matched files: %d\n\n", len(results)))

	for _, item := range results {
		sb.WriteString(fmt.Sprintf("- %s [%s] (%d bytes)\n", item.Path, item.Lang, item.Size))
		if len(item.Symbols) > 0 {
			sb.WriteString(fmt.Sprintf("  symbols: %s\n", strings.Join(item.Symbols, ", ")))
		}
	}
	if len(results) == 0 {
		return "No files matched query.", nil
	}
	return sb.String(), nil
}

func (t *RepoMapTool) cachePath() string {
	return filepath.Join(t.workspace, ".clawgo", "repo_map.json")
}

func (t *RepoMapTool) loadOrBuildMap(force bool) (*repoMapCache, error) {
	if !force {
		if data, err := os.ReadFile(t.cachePath()); err == nil {
			var cache repoMapCache
			if err := json.Unmarshal(data, &cache); err == nil {
				if cache.Workspace == t.workspace && (time.Now().UnixMilli()-cache.UpdatedAt) < int64((10*time.Minute)/time.Millisecond) {
					return &cache, nil
				}
			}
		}
	}

	cache := &repoMapCache{
		Workspace: t.workspace,
		UpdatedAt: time.Now().UnixMilli(),
		Files:     []repoMapEntry{},
	}

	err := filepath.Walk(t.workspace, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "node_modules" || name == ".clawgo" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Size() > 512*1024 {
			return nil
		}

		rel, err := filepath.Rel(t.workspace, path)
		if err != nil {
			return nil
		}
		lang := langFromPath(rel)
		if lang == "" {
			return nil
		}

		entry := repoMapEntry{
			Path:    rel,
			Lang:    lang,
			Size:    info.Size(),
			ModTime: info.ModTime().UnixMilli(),
			Symbols: extractSymbols(path, lang),
		}
		cache.Files = append(cache.Files, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(cache.Files, func(i, j int) bool {
		return cache.Files[i].Path < cache.Files[j].Path
	})

	if err := os.MkdirAll(filepath.Dir(t.cachePath()), 0755); err == nil {
		if data, err := json.Marshal(cache); err == nil {
			_ = os.WriteFile(t.cachePath(), data, 0644)
		}
	}
	return cache, nil
}

func (t *RepoMapTool) filterRepoMap(files []repoMapEntry, query string) []repoMapEntry {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return files
	}

	type scored struct {
		item  repoMapEntry
		score int
	}
	items := []scored{}
	for _, file := range files {
		score := 0
		p := strings.ToLower(file.Path)
		if strings.Contains(p, q) {
			score += 5
		}
		for _, sym := range file.Symbols {
			if strings.Contains(strings.ToLower(sym), q) {
				score += 3
			}
		}
		if score > 0 {
			items = append(items, scored{item: file, score: score})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].item.Path < items[j].item.Path
		}
		return items[i].score > items[j].score
	})

	out := make([]repoMapEntry, 0, len(items))
	for _, item := range items {
		out = append(out, item.item)
	}
	return out
}

func langFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".sh":
		return "shell"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	default:
		return ""
	}
}

func extractSymbols(path, lang string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := string(data)
	out := []string{}

	switch lang {
	case "go":
		// Top-level functions
		reFunc := regexp.MustCompile(`(?m)^func\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
		for _, m := range reFunc.FindAllStringSubmatch(content, 20) {
			if len(m) > 1 {
				out = append(out, m[1])
			}
		}
		// Methods: func (r *Receiver) MethodName(...)
		reMethod := regexp.MustCompile(`(?m)^func\s+\([^)]+\)\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
		for _, m := range reMethod.FindAllStringSubmatch(content, 20) {
			if len(m) > 1 {
				out = append(out, m[1])
			}
		}
		// Types
		reType := regexp.MustCompile(`(?m)^type\s+([A-Za-z_][A-Za-z0-9_]*)\s+`)
		for _, m := range reType.FindAllStringSubmatch(content, 20) {
			if len(m) > 1 {
				out = append(out, m[1])
			}
		}
	case "python":
		// Functions and Classes
		re := regexp.MustCompile(`(?m)^(?:def|class)\s+([A-Za-z_][A-Za-z0-9_]*)`)
		for _, m := range re.FindAllStringSubmatch(content, 30) {
			if len(m) > 1 {
				out = append(out, m[1])
			}
		}
	case "javascript", "typescript":
		// function Name(...) or class Name ... or const Name = (...) =>
		re := regexp.MustCompile(`(?m)^(?:export\s+)?(?:function|class|const|let|var)\s+([A-Za-z_][A-Za-z0-9_]*)`)
		for _, m := range re.FindAllStringSubmatch(content, 30) {
			if len(m) > 1 {
				out = append(out, m[1])
			}
		}
	case "markdown":
		// Headers as symbols
		re := regexp.MustCompile(`(?m)^#+\s+(.+)$`)
		for _, m := range re.FindAllStringSubmatch(content, 20) {
			if len(m) > 1 {
				out = append(out, strings.TrimSpace(m[1]))
			}
		}
	}

	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	uniq := []string{}
	seen := make(map[string]bool)
	for _, s := range out {
		if !seen[s] {
			uniq = append(uniq, s)
			seen[s] = true
		}
	}
	return uniq
}
