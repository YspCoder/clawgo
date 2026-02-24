package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type MemorySearchTool struct {
	workspace string
	mu        sync.RWMutex
	cache     map[string]cachedMemoryFile
}

type memoryBlock struct {
	lineNum int
	content string
	heading string
	lower   string
}

type cachedMemoryFile struct {
	modTime time.Time
	blocks  []memoryBlock
}

func NewMemorySearchTool(workspace string) *MemorySearchTool {
	return &MemorySearchTool{
		workspace: workspace,
		cache:     make(map[string]cachedMemoryFile),
	}
}

func (t *MemorySearchTool) Name() string {
	return "memory_search"
}

func (t *MemorySearchTool) Description() string {
	return "Semantically search MEMORY.md and memory/*.md files for information. Returns relevant snippets (paragraphs) containing the query terms."
}

func (t *MemorySearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query keywords (e.g., 'docker deploy project')",
			},
			"maxResults": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of results to return",
				"default":     5,
			},
		},
		"required": []string{"query"},
	}
}

type searchResult struct {
	file    string
	lineNum int
	content string
	score   int
}

func (t *MemorySearchTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("query is required")
	}

	maxResults := 5
	if m, ok := args["maxResults"].(float64); ok {
		maxResults = int(m)
	}
	if maxResults <= 0 {
		maxResults = 5
	}

	keywords := strings.Fields(strings.ToLower(query))
	if len(keywords) == 0 {
		return "Please provide search keywords.", nil
	}

	files := t.getMemoryFiles()
	
	resultsChan := make(chan []searchResult, len(files))
	var wg sync.WaitGroup

	for _, file := range files {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(f string) {
			defer wg.Done()
			if ctx.Err() != nil {
				return
			}
			matches, err := t.searchFile(f, keywords)
			if err == nil {
				resultsChan <- matches
			}
		}(file)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	var allResults []searchResult
	for matches := range resultsChan {
		allResults = append(allResults, matches...)
	}

	sort.SliceStable(allResults, func(i, j int) bool {
		if allResults[i].score != allResults[j].score {
			return allResults[i].score > allResults[j].score
		}
		if allResults[i].file != allResults[j].file {
			return allResults[i].file < allResults[j].file
		}
		return allResults[i].lineNum < allResults[j].lineNum
	})

	if len(allResults) > maxResults {
		allResults = allResults[:maxResults]
	}

	if len(allResults) == 0 {
		return fmt.Sprintf("No memory found for query: %s", query), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d memories for '%s':\n\n", len(allResults), query))
	for _, res := range allResults {
		relPath, _ := filepath.Rel(t.workspace, res.file)
		lineEnd := res.lineNum + countLines(res.content) - 1
		if lineEnd < res.lineNum {
			lineEnd = res.lineNum
		}
		sb.WriteString(fmt.Sprintf("Source: %s#L%d-L%d\n%s\n\n", relPath, res.lineNum, lineEnd, res.content))
	}

	return sb.String(), nil
}

func (t *MemorySearchTool) getMemoryFiles() []string {
	var files []string

	// Check workspace MEMORY.md first
	mainMem := filepath.Join(t.workspace, "MEMORY.md")
	if _, err := os.Stat(mainMem); err == nil {
		files = append(files, mainMem)
	}

	// Backward-compatible location: memory/MEMORY.md
	legacyMem := filepath.Join(t.workspace, "memory", "MEMORY.md")
	if _, err := os.Stat(legacyMem); err == nil {
		files = append(files, legacyMem)
	}

	// Recursively include memory/**/*.md
	memDir := filepath.Join(t.workspace, "memory")
	_ = filepath.WalkDir(memDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			files = append(files, path)
		}
		return nil
	})

	return dedupeStrings(files)
}

func countLines(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}

func dedupeStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, v := range items {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// searchFile searches parsed markdown blocks with cache by file modtime.
func (t *MemorySearchTool) searchFile(path string, keywords []string) ([]searchResult, error) {
	blocks, err := t.getOrParseBlocks(path)
	if err != nil {
		return nil, err
	}
	results := make([]searchResult, 0, 8)
	for _, b := range blocks {
		score := 0
		for _, kw := range keywords {
			if strings.Contains(b.lower, kw) {
				score++
			}
			if b.heading != "" && strings.Contains(strings.ToLower(b.heading), kw) {
				score++
			}
		}
		if score == 0 {
			continue
		}
		content := b.content
		if b.heading != "" && !strings.HasPrefix(strings.TrimSpace(content), "#") {
			content = fmt.Sprintf("[%s]\n%s", b.heading, content)
		}
		results = append(results, searchResult{file: path, lineNum: b.lineNum, content: content, score: score})
	}
	return results, nil
}

func (t *MemorySearchTool) getOrParseBlocks(path string) ([]memoryBlock, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	mod := st.ModTime()
	t.mu.RLock()
	if c, ok := t.cache[path]; ok && c.modTime.Equal(mod) {
		blocks := c.blocks
		t.mu.RUnlock()
		return blocks, nil
	}
	t.mu.RUnlock()

	blocks, err := parseMarkdownBlocks(path)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.cache[path] = cachedMemoryFile{modTime: mod, blocks: blocks}
	t.mu.Unlock()
	return blocks, nil
}

func parseMarkdownBlocks(path string) ([]memoryBlock, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	blocks := make([]memoryBlock, 0, 32)
	scanner := bufio.NewScanner(file)
	var current strings.Builder
	blockStartLine := 1
	currentLine := 0
	lastHeading := ""

	flush := func() {
		content := strings.TrimSpace(current.String())
		if content == "" {
			current.Reset()
			return
		}
		blocks = append(blocks, memoryBlock{lineNum: blockStartLine, content: content, heading: lastHeading, lower: strings.ToLower(content)})
		current.Reset()
	}

	for scanner.Scan() {
		currentLine++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		isHeader := strings.HasPrefix(trimmed, "#")
		isEmpty := trimmed == ""
		isList := strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || (len(trimmed) > 3 && trimmed[1] == '.' && trimmed[2] == ' ')

		if isHeader {
			flush()
			lastHeading = strings.TrimLeft(trimmed, "# ")
			blockStartLine = currentLine
			current.WriteString(line + "\n")
			flush()
			continue
		}
		if isEmpty {
			flush()
			blockStartLine = currentLine + 1
			continue
		}
		if isList {
			flush()
			blockStartLine = currentLine
		}
		if current.Len() == 0 {
			blockStartLine = currentLine
		}
		current.WriteString(line + "\n")
	}
	flush()
	return blocks, nil
}
