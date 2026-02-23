package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type ParallelFetchTool struct {
	fetcher          *WebFetchTool
	maxParallelCalls int
	parallelSafe     map[string]struct{}
}

func NewParallelFetchTool(fetcher *WebFetchTool, maxParallelCalls int, parallelSafe map[string]struct{}) *ParallelFetchTool {
	limit := normalizeParallelLimit(maxParallelCalls)
	return &ParallelFetchTool{
		fetcher:          fetcher,
		maxParallelCalls: limit,
		parallelSafe:     normalizeSafeToolNames(parallelSafe),
	}
}

func (t *ParallelFetchTool) Name() string {
	return "parallel_fetch"
}

func (t *ParallelFetchTool) Description() string {
	return "Fetch multiple URLs concurrently. Useful for comparing information across different sites or gathering diverse sources quickly."
}

func (t *ParallelFetchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"urls": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": "List of URLs to fetch",
			},
		},
		"required": []string{"urls"},
	}
}

func (t *ParallelFetchTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	urlsRaw, ok := args["urls"].([]interface{})
	if !ok {
		return "", fmt.Errorf("urls must be an array")
	}

	maxParallel := t.maxParallelCalls
	if maxParallel <= 1 {
		return t.executeSerial(ctx, urlsRaw), nil
	}

	if !t.isParallelSafe() {
		return t.executeSerial(ctx, urlsRaw), nil
	}

	results := make([]string, len(urlsRaw))
	var wg sync.WaitGroup
	sem := make(chan struct{}, minParallelLimit(maxParallel, len(urlsRaw)))

	for i, u := range urlsRaw {
		urlStr, ok := u.(string)
		if !ok {
			results[i] = "Error: invalid url"
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(index int, url string) {
			defer wg.Done()
			defer func() { <-sem }()

			res, err := t.fetcher.Execute(ctx, map[string]interface{}{"url": url})
			if err != nil {
				results[index] = fmt.Sprintf("Error fetching %s: %v", url, err)
			} else {
				results[index] = res
			}
		}(i, urlStr)
	}

	wg.Wait()

	return formatFetchResults(results), nil
}

func (t *ParallelFetchTool) executeSerial(ctx context.Context, urlsRaw []interface{}) string {
	results := make([]string, len(urlsRaw))
	for i, u := range urlsRaw {
		urlStr, ok := u.(string)
		if !ok {
			results[i] = "Error: invalid url"
			continue
		}
		res, err := t.fetcher.Execute(ctx, map[string]interface{}{"url": urlStr})
		if err != nil {
			results[i] = fmt.Sprintf("Error fetching %s: %v", urlStr, err)
		} else {
			results[i] = res
		}
	}
	return formatFetchResults(results)
}

func (t *ParallelFetchTool) isParallelSafe() bool {
	if t.parallelSafe == nil {
		return false
	}
	if tool, ok := any(t.fetcher).(ParallelSafeTool); ok {
		return tool.ParallelSafe()
	}
	_, ok := t.parallelSafe["web_fetch"]
	return ok
}

func formatFetchResults(results []string) string {
	var output strings.Builder
	for i, res := range results {
		output.WriteString(fmt.Sprintf("=== Result %d ===\n%s\n\n", i+1, res))
	}
	return output.String()
}

func minParallelLimit(maxParallel, total int) int {
	if maxParallel <= 0 {
		return 1
	}
	if total <= 0 {
		return maxParallel
	}
	if maxParallel > total {
		return total
	}
	return maxParallel
}
