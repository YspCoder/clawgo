package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	var results []searchResult

	for _, file := range files {
		matches, err := t.searchFile(file, keywords)
		if err != nil {
			continue // skip unreadable files
		}
		results = append(results, matches...)
	}

	// Simple ranking: sort by score (number of keyword matches) desc
	// Ideally use a stable sort or more sophisticated scoring
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].score > results[i].score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	if len(results) == 0 {
		return fmt.Sprintf("No memory found for query: %s", query), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d memories for '%s':\n\n", len(results), query))
	for _, res := range results {
		relPath, _ := filepath.Rel(t.workspace, res.file)
		sb.WriteString(fmt.Sprintf("--- Source: %s:%d ---\n%s\n\n", relPath, res.lineNum, res.content))
	}

	return sb.String(), nil
}

func (t *MemorySearchTool) getMemoryFiles() []string {
	var files []string
	
	// Check main MEMORY.md
	mainMem := filepath.Join(t.workspace, "MEMORY.md")
	if _, err := os.Stat(mainMem); err == nil {
		files = append(files, mainMem)
	}

	// Check memory/ directory
	memDir := filepath.Join(t.workspace, "memory")
	entries, err := os.ReadDir(memDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
				files = append(files, filepath.Join(memDir, entry.Name()))
			}
		}
	}
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

			// Only keep if at least one keyword matched
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
