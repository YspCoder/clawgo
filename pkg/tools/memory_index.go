package tools

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type memoryIndex struct {
	UpdatedAt int64                          `json:"updated_at"`
	Files     map[string]int64               `json:"files"`
	Entries   []memoryIndexEntry             `json:"entries"`
	Inverted  map[string][]int               `json:"inverted"`
	Meta      map[string]map[string][]string `json:"meta"`
}

type memoryIndexEntry struct {
	ID      string `json:"id"`
	File    string `json:"file"`
	LineNum int    `json:"line_num"`
	Content string `json:"content"`
}

func (t *MemorySearchTool) indexPath() string {
	return filepath.Join(t.workspace, "memory", ".index.json")
}

func (t *MemorySearchTool) loadOrBuildIndex(files []string) (*memoryIndex, error) {
	path := t.indexPath()

	if idx, ok := t.loadIndex(path); ok && !t.shouldRebuildIndex(idx, files) {
		return idx, nil
	}
	return t.buildAndSaveIndex(path, files)
}

func (t *MemorySearchTool) loadIndex(path string) (*memoryIndex, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var idx memoryIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, false
	}
	if idx.Files == nil || idx.Inverted == nil {
		return nil, false
	}
	return &idx, true
}

func (t *MemorySearchTool) shouldRebuildIndex(idx *memoryIndex, files []string) bool {
	if idx == nil {
		return true
	}
	if len(idx.Files) != len(files) {
		return true
	}
	for _, file := range files {
		st, err := os.Stat(file)
		if err != nil {
			return true
		}
		if prev, ok := idx.Files[file]; !ok || prev != st.ModTime().UnixMilli() {
			return true
		}
	}
	return false
}

func (t *MemorySearchTool) buildAndSaveIndex(path string, files []string) (*memoryIndex, error) {
	idx := &memoryIndex{
		UpdatedAt: time.Now().UnixMilli(),
		Files:     make(map[string]int64, len(files)),
		Entries:   []memoryIndexEntry{},
		Inverted:  map[string][]int{},
		Meta:      map[string]map[string][]string{"sections": {}},
	}

	for _, file := range files {
		st, err := os.Stat(file)
		if err != nil {
			continue
		}
		idx.Files[file] = st.ModTime().UnixMilli()

		blocks, sections, err := t.extractBlocks(file)
		if err != nil {
			continue
		}
		idx.Meta["sections"][file] = sections

		for _, block := range blocks {
			entry := memoryIndexEntry{
				ID:      hashEntryID(file, block.lineNum, block.content),
				File:    file,
				LineNum: block.lineNum,
				Content: block.content,
			}
			entryPos := len(idx.Entries)
			idx.Entries = append(idx.Entries, entry)

			tokens := tokenize(block.content)
			seen := map[string]struct{}{}
			for _, token := range tokens {
				if _, ok := seen[token]; ok {
					continue
				}
				seen[token] = struct{}{}
				idx.Inverted[token] = append(idx.Inverted[token], entryPos)
			}
		}
	}

	for token, ids := range idx.Inverted {
		sort.Ints(ids)
		idx.Inverted[token] = uniqueInt(ids)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return idx, nil
	}
	if data, err := json.Marshal(idx); err == nil {
		_ = os.WriteFile(path, data, 0644)
	}
	return idx, nil
}

func (t *MemorySearchTool) extractBlocks(path string) ([]searchResult, []string, error) {
	results, err := t.searchFile(path, []string{})
	if err != nil {
		return nil, nil, err
	}
	sections := []string{}
	for _, res := range results {
		if strings.HasPrefix(strings.TrimSpace(res.content), "[") {
			end := strings.Index(res.content, "]")
			if end > 1 {
				sections = append(sections, strings.TrimSpace(res.content[1:end]))
			}
		}
	}
	sections = uniqueStrings(sections)
	return results, sections, nil
}

func hashEntryID(file string, line int, content string) string {
	sum := sha1.Sum([]byte(file + ":" + strconv.Itoa(line) + ":" + content))
	return hex.EncodeToString(sum[:8])
}

func tokenize(s string) []string {
	normalized := strings.ToLower(s)
	repl := []string{",", ".", ";", ":", "\n", "\t", "(", ")", "[", "]", "{", "}", "\"", "'", "`", "/", "\\", "|", "-", "_"}
	for _, r := range repl {
		normalized = strings.ReplaceAll(normalized, r, " ")
	}
	parts := strings.Fields(normalized)
	return uniqueStrings(parts)
}

func uniqueInt(in []int) []int {
	if len(in) < 2 {
		return in
	}
	out := in[:1]
	for i := 1; i < len(in); i++ {
		if in[i] != in[i-1] {
			out = append(out, in[i])
		}
	}
	return out
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
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
