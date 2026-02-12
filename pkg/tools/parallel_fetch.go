package tools

import (
	"context"
	"fmt"
	"sync"
)

type ParallelFetchTool struct {
	fetcher *WebFetchTool
}

func NewParallelFetchTool(fetcher *WebFetchTool) *ParallelFetchTool {
	return &ParallelFetchTool{fetcher: fetcher}
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

	results := make([]string, len(urlsRaw))
	var wg sync.WaitGroup

	for i, u := range urlsRaw {
		urlStr, ok := u.(string)
		if !ok {
			continue
		}

		wg.Add(1)
		go func(index int, url string) {
			defer wg.Done()
			res, err := t.fetcher.Execute(ctx, map[string]interface{}{"url": url})
			if err != nil {
				results[index] = fmt.Sprintf("Error fetching %s: %v", url, err)
			} else {
				results[index] = res
			}
		}(i, urlStr)
	}

	wg.Wait()

	var output string
	for i, res := range results {
		output += fmt.Sprintf("=== Result %d ===\n%s\n\n", i+1, res)
	}

	return output, nil
}
