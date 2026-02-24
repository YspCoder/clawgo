package tools

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"clawgo/pkg/logger"
)

type ToolRegistry struct {
	tools    map[string]Tool
	mu       sync.RWMutex
	snapshot atomic.Value // map[string]Tool (copy-on-write)
}

func NewToolRegistry() *ToolRegistry {
	r := &ToolRegistry{tools: make(map[string]Tool)}
	r.snapshot.Store(map[string]Tool{})
	return r
}

func (r *ToolRegistry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
	next := make(map[string]Tool, len(r.tools))
	for k, v := range r.tools {
		next[k] = v
	}
	r.snapshot.Store(next)
}

func (r *ToolRegistry) Get(name string) (Tool, bool) {
	cur, _ := r.snapshot.Load().(map[string]Tool)
	tool, ok := cur[name]
	return tool, ok
}

func (r *ToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	logger.InfoCF("tool", "Tool execution started",
		map[string]interface{}{
			"tool": name,
			"args": args,
		})

	tool, ok := r.Get(name)
	if !ok {
		logger.ErrorCF("tool", "Tool not found",
			map[string]interface{}{
				"tool": name,
			})
		return "", fmt.Errorf("tool '%s' not found", name)
	}

	start := time.Now()
	result, err := tool.Execute(ctx, args)
	duration := time.Since(start)

	if err != nil {
		logger.ErrorCF("tool", "Tool execution failed",
			map[string]interface{}{
				"tool":            name,
				"duration":        duration.Milliseconds(),
				logger.FieldError: err.Error(),
			})
	} else {
		logger.InfoCF("tool", "Tool execution completed",
			map[string]interface{}{
				"tool":                          name,
				"duration_ms":                   duration.Milliseconds(),
				logger.FieldOutputContentLength: len(result),
			})
	}

	return result, err
}

func (r *ToolRegistry) GetDefinitions() []map[string]interface{} {
	cur, _ := r.snapshot.Load().(map[string]Tool)
	definitions := make([]map[string]interface{}, 0, len(cur))
	for _, tool := range cur {
		definitions = append(definitions, ToolToSchema(tool))
	}
	return definitions
}

// List returns a list of all registered tool names.
func (r *ToolRegistry) List() []string {
	cur, _ := r.snapshot.Load().(map[string]Tool)
	names := make([]string, 0, len(cur))
	for name := range cur {
		names = append(names, name)
	}
	return names
}

// Count returns the number of registered tools.
func (r *ToolRegistry) Count() int {
	cur, _ := r.snapshot.Load().(map[string]Tool)
	return len(cur)
}

// GetSummaries returns human-readable summaries of all registered tools.
// Returns a slice of "name - description" strings.
func (r *ToolRegistry) GetSummaries() []string {
	cur, _ := r.snapshot.Load().(map[string]Tool)
	summaries := make([]string, 0, len(cur))
	for _, tool := range cur {
		summaries = append(summaries, fmt.Sprintf("- `%s` - %s", tool.Name(), tool.Description()))
	}
	return summaries
}
