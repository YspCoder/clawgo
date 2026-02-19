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
)

type MemorySearchTool struct {
	workspace string
}

func NewMemorySearchTool(workspace string) *MemorySearchTool {
	return &MemorySearchTool{
		workspace: workspace,
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

	keywords := strings.Fields(strings.ToLower(query))
	if len(keywords) == 0 {
		return "Please provide search keywords.", nil
	}

	files := t.getMemoryFiles()
	if len(files) == 0 {
		return fmt.Sprintf("No memory files found for query: %s", query), nil
	}

	// Fast path: structured memory index.
	if idx, err := t.loadOrBuildIndex(files); err == nil && idx != nil {
		results := t.searchInIndex(idx, keywords)
		return t.renderSearchResults(query, results, maxResults), nil
	}

	resultsChan := make(chan []searchResult, len(files))
	var wg sync.WaitGroup

	// Search all files concurrently
	for _, file := range files {
		wg.Add(1)
		go func(f string) {
			defer wg.Done()
			matches, err := t.searchFile(f, keywords)
			if err == nil {
				resultsChan <- matches
			}
		}(file)
	}

	// Close channel asynchronously
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	var allResults []searchResult
	for matches := range resultsChan {
		allResults = append(allResults, matches...)
	}

	return t.renderSearchResults(query, allResults, maxResults), nil
}

func (t *MemorySearchTool) searchInIndex(idx *memoryIndex, keywords []string) []searchResult {
	type scoreItem struct {
		entry memoryIndexEntry
		score int
	}
	acc := make(map[int]int)
	for _, kw := range keywords {
		token := strings.ToLower(strings.TrimSpace(kw))
		for _, entryID := range idx.Inverted[token] {
			acc[entryID]++
		}
	}

	out := make([]scoreItem, 0, len(acc))
	for entryID, score := range acc {
		if entryID < 0 || entryID >= len(idx.Entries) || score <= 0 {
			continue
		}
		out = append(out, scoreItem{
			entry: idx.Entries[entryID],
			score: score,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score == out[j].score {
			return out[i].entry.LineNum < out[j].entry.LineNum
		}
		return out[i].score > out[j].score
	})

	results := make([]searchResult, 0, len(out))
	for _, item := range out {
		results = append(results, searchResult{
			file:    item.entry.File,
			lineNum: item.entry.LineNum,
			content: item.entry.Content,
			score:   item.score,
		})
	}
	return results
}

func (t *MemorySearchTool) renderSearchResults(query string, allResults []searchResult, maxResults int) string {
	sort.Slice(allResults, func(i, j int) bool {
		if allResults[i].score == allResults[j].score {
			return allResults[i].lineNum < allResults[j].lineNum
		}
		return allResults[i].score > allResults[j].score
	})

	if len(allResults) > maxResults {
		allResults = allResults[:maxResults]
	}
	if len(allResults) == 0 {
		return fmt.Sprintf("No memory found for query: %s", query)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d memories for '%s':\n\n", len(allResults), query))
	for _, res := range allResults {
		relPath, _ := filepath.Rel(t.workspace, res.file)
		sb.WriteString(fmt.Sprintf("--- Source: %s:%d ---\n%s\n\n", relPath, res.lineNum, res.content))
	}
	return sb.String()
}

func (t *MemorySearchTool) getMemoryFiles() []string {
	var files []string
	seen := map[string]struct{}{}

	addIfExists := func(path string) {
		if _, ok := seen[path]; ok {
			return
		}
		if _, err := os.Stat(path); err == nil {
			files = append(files, path)
			seen[path] = struct{}{}
		}
	}

	// Check long-term memory in both legacy and current locations.
	addIfExists(filepath.Join(t.workspace, "MEMORY.md"))
	addIfExists(filepath.Join(t.workspace, "memory", "MEMORY.md"))

	// Check memory/ directory recursively (e.g., memory/YYYYMM/YYYYMMDD.md).
	memDir := filepath.Join(t.workspace, "memory")
	_ = filepath.Walk(memDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(info.Name()), ".md") {
			if _, ok := seen[path]; !ok {
				files = append(files, path)
				seen[path] = struct{}{}
			}
		}
		return nil
	})
	return files
}

// searchFile parses the markdown file into blocks (paragraphs/list items) and searches them
func (t *MemorySearchTool) searchFile(path string, keywords []string) ([]searchResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var results []searchResult
	scanner := bufio.NewScanner(file)

	var currentBlock strings.Builder
	var blockStartLine int = 1
	var currentLineNum int = 0
	var lastHeading string

	processBlock := func() {
		content := strings.TrimSpace(currentBlock.String())
		if content != "" {
			lowerContent := strings.ToLower(content)
			score := 0
			// Calculate score: how many keywords are present?
			for _, kw := range keywords {
				if strings.Contains(lowerContent, kw) {
					score++
				}
			}

			// Add bonus if heading matches
			if lastHeading != "" {
				lowerHeading := strings.ToLower(lastHeading)
				for _, kw := range keywords {
					if strings.Contains(lowerHeading, kw) {
						score++
					}
				}
				// Prepend heading context if not already part of block
				if !strings.HasPrefix(content, "#") {
					content = fmt.Sprintf("[%s]\n%s", lastHeading, content)
				}
			}

			// Keep all blocks when keywords are empty (index build).
			if len(keywords) == 0 {
				score = 1
			}

			// Only keep if at least one keyword matched.
			if score > 0 {
				results = append(results, searchResult{
					file:    path,
					lineNum: blockStartLine,
					content: content,
					score:   score,
				})
			}
		}
		currentBlock.Reset()
	}

	for scanner.Scan() {
		currentLineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Markdown Block Logic:
		// 1. Headers start new blocks
		// 2. Empty lines separate blocks
		// 3. List items start new blocks (optional, but good for logs)

		isHeader := strings.HasPrefix(trimmed, "#")
		isEmpty := trimmed == ""
		isList := strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || (len(trimmed) > 3 && trimmed[1] == '.' && trimmed[2] == ' ')

		if isHeader {
			processBlock() // Flush previous
			lastHeading = strings.TrimLeft(trimmed, "# ")
			blockStartLine = currentLineNum
			currentBlock.WriteString(line + "\n")
			processBlock() // Headers are their own blocks too
			continue
		}

		if isEmpty {
			processBlock() // Flush previous
			blockStartLine = currentLineNum + 1
			continue
		}

		if isList {
			processBlock() // Flush previous (treat list items as atomic for better granularity)
			blockStartLine = currentLineNum
		}

		if currentBlock.Len() == 0 {
			blockStartLine = currentLineNum
		}
		currentBlock.WriteString(line + "\n")
	}

	processBlock() // Flush last block
	return results, nil
}
