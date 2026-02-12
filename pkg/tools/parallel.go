package tools

import (
	"context"
	"fmt"
	"sync"
)

type ParallelTool struct {
	registry *ToolRegistry
}

func NewParallelTool(registry *ToolRegistry) *ParallelTool {
	return &ParallelTool{registry: registry}
}

func (t *ParallelTool) Name() string {
	return "parallel"
}

func (t *ParallelTool) Description() string {
	return "Execute multiple tools concurrently using Go's goroutines. Returns a map of tool names/IDs to their results."
}

func (t *ParallelTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"calls": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"tool": map[string]interface{}{
							"type":        "string",
							"description": "Name of the tool to call",
						},
						"arguments": map[string]interface{}{
							"type":        "object",
							"description": "Arguments for the tool",
						},
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Optional unique ID to identify this specific result",
						},
					},
					"required": []string{"tool", "arguments"},
				},
			},
		},
		"required": []string{"calls"},
	}
}

func (t *ParallelTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	callsRaw, ok := args["calls"].([]interface{})
	if !ok {
		return "", fmt.Errorf("calls must be an array")
	}

	results := make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i, c := range callsRaw {
		call, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		toolName, _ := call["tool"].(string)
		toolArgs, _ := call["arguments"].(map[string]interface{})
		id, _ := call["id"].(string)
		if id == "" {
			id = fmt.Sprintf("call_%d_%s", i, toolName)
		}

		wg.Add(1)
		go func(id, name string, args map[string]interface{}) {
			defer wg.Done()
			res, err := t.registry.Execute(ctx, name, args)
			
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				results[id] = fmt.Sprintf("Error: %v", err)
			} else {
				results[id] = res
			}
		}(id, toolName, toolArgs)
	}

	wg.Wait()

	// Simple string representation for the agent
	var output string
	for id, res := range results {
		output += fmt.Sprintf("--- Result for %s ---\n%s\n", id, res)
	}

	return output, nil
}
