package tools

import "context"

type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]interface{}
	Execute(ctx context.Context, args map[string]interface{}) (string, error)
}

// ParallelSafeTool is an optional capability interface.
// If implemented by a tool, AgentLoop should trust this declaration
// over name-based whitelist when deciding parallel execution safety.
type ParallelSafeTool interface {
	ParallelSafe() bool
}

// ResourceScopedTool is an optional capability interface.
// If implemented by a tool, AgentLoop can avoid running calls that touch
// the same resource keys in parallel.
type ResourceScopedTool interface {
	ResourceKeys(args map[string]interface{}) []string
}

func ToolToSchema(tool Tool) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        tool.Name(),
			"description": tool.Description(),
			"parameters":  tool.Parameters(),
		},
	}
}
