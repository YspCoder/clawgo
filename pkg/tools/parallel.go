package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

const (
	maxParallelToolCallsLimit = 8
	defaultParallelToolCalls  = 2
)

type parallelCall struct {
	Index    int
	Tool     string
	Args     map[string]interface{}
	ResultID string
}

type ParallelTool struct {
	registry         *ToolRegistry
	maxParallelCalls int
	parallelSafe     map[string]struct{}
}

func NewParallelTool(registry *ToolRegistry, maxParallelCalls int, parallelSafe map[string]struct{}) *ParallelTool {
	limit := normalizeParallelLimit(maxParallelCalls)
	return &ParallelTool{
		registry:         registry,
		maxParallelCalls: limit,
		parallelSafe:     normalizeSafeToolNames(parallelSafe),
	}
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

	calls := make([]parallelCall, 0, len(callsRaw))
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

		calls = append(calls, parallelCall{
			Index:    i,
			Tool:     toolName,
			Args:     toolArgs,
			ResultID: id,
		})
	}

	if len(calls) == 0 {
		return "", fmt.Errorf("no valid calls provided")
	}

	if !t.callsParallelSafe(calls) {
		return t.executeSerial(ctx, calls), nil
	}

	batches := buildParallelBatches(t.registry, calls)
	var output strings.Builder
	for _, batch := range batches {
		if len(batch) == 0 {
			continue
		}
		if len(batch) == 1 || t.maxParallelCalls <= 1 {
			output.WriteString(t.executeSerial(ctx, batch))
			continue
		}
		output.WriteString(t.executeParallel(ctx, batch))
	}
	return output.String(), nil
}

func (t *ParallelTool) executeSerial(ctx context.Context, calls []parallelCall) string {
	results := make([]parallelResult, 0, len(calls))
	for _, call := range calls {
		res, err := t.registry.Execute(ctx, call.Tool, call.Args)
		results = append(results, parallelResult{
			Index:    call.Index,
			ResultID: call.ResultID,
			Output:   formatToolResult(res, err),
		})
	}
	return formatParallelResults(results)
}

func (t *ParallelTool) executeParallel(ctx context.Context, calls []parallelCall) string {
	limit := t.maxParallelCalls
	if limit <= 0 {
		limit = defaultParallelToolCalls
	}
	if limit > len(calls) {
		limit = len(calls)
	}

	results := make([]parallelResult, len(calls))
	var wg sync.WaitGroup
	sem := make(chan struct{}, limit)
	for i, call := range calls {
		wg.Add(1)
		sem <- struct{}{}
		go func(index int, call parallelCall) {
			defer wg.Done()
			defer func() { <-sem }()
			res, err := t.registry.Execute(ctx, call.Tool, call.Args)
			results[index] = parallelResult{
				Index:    call.Index,
				ResultID: call.ResultID,
				Output:   formatToolResult(res, err),
			}
		}(i, call)
	}
	wg.Wait()
	return formatParallelResults(results)
}

func (t *ParallelTool) callsParallelSafe(calls []parallelCall) bool {
	for _, call := range calls {
		name := strings.ToLower(strings.TrimSpace(call.Tool))
		if name == "" {
			return false
		}
		if tool, ok := t.registry.Get(call.Tool); ok {
			if ps, ok := tool.(ParallelSafeTool); ok {
				if !ps.ParallelSafe() {
					return false
				}
				continue
			}
		}
		if _, ok := t.parallelSafe[name]; !ok {
			return false
		}
	}
	return true
}

func buildParallelBatches(registry *ToolRegistry, calls []parallelCall) [][]parallelCall {
	if len(calls) == 0 {
		return nil
	}
	batches := make([][]parallelCall, 0, len(calls))
	current := make([]parallelCall, 0, len(calls))
	used := map[string]struct{}{}

	flush := func() {
		if len(current) == 0 {
			return
		}
		batch := append([]parallelCall(nil), current...)
		batches = append(batches, batch)
		current = current[:0]
		used = map[string]struct{}{}
	}

	for _, call := range calls {
		keys := toolResourceKeys(registry, call.Tool, call.Args)
		if len(current) > 0 && hasResourceKeyConflict(used, keys) {
			flush()
		}
		current = append(current, call)
		for _, k := range keys {
			used[k] = struct{}{}
		}
	}
	flush()
	return batches
}

func toolResourceKeys(registry *ToolRegistry, name string, args map[string]interface{}) []string {
	raw := strings.TrimSpace(name)
	lower := strings.ToLower(raw)
	if raw == "" || registry == nil {
		return nil
	}
	tool, ok := registry.Get(raw)
	if !ok && lower != raw {
		tool, ok = registry.Get(lower)
	}
	if !ok || tool == nil {
		return nil
	}
	rs, ok := tool.(ResourceScopedTool)
	if !ok {
		return nil
	}
	return normalizeResourceKeys(rs.ResourceKeys(args))
}

func normalizeResourceKeys(keys []string) []string {
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

func hasResourceKeyConflict(used map[string]struct{}, keys []string) bool {
	if len(keys) == 0 || len(used) == 0 {
		return false
	}
	for _, k := range keys {
		if _, ok := used[k]; ok {
			return true
		}
	}
	return false
}

type parallelResult struct {
	Index    int
	ResultID string
	Output   string
}

func formatParallelResults(results []parallelResult) string {
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Index < results[j].Index
	})
	var output strings.Builder
	for _, res := range results {
		if res.ResultID == "" {
			continue
		}
		output.WriteString(fmt.Sprintf("--- Result for %s ---\n%s\n", res.ResultID, res.Output))
	}
	return output.String()
}

func formatToolResult(result string, err error) string {
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return result
}

func normalizeParallelLimit(limit int) int {
	if limit <= 0 {
		limit = defaultParallelToolCalls
	}
	if limit < 1 {
		limit = 1
	}
	if limit > maxParallelToolCallsLimit {
		limit = maxParallelToolCallsLimit
	}
	return limit
}

func normalizeSafeToolNames(names map[string]struct{}) map[string]struct{} {
	if len(names) == 0 {
		return map[string]struct{}{}
	}
	out := make(map[string]struct{}, len(names))
	for name := range names {
		n := strings.ToLower(strings.TrimSpace(name))
		if n == "" {
			continue
		}
		out[n] = struct{}{}
	}
	return out
}
