package tools

import (
	"context"
)

// AliasTool exposes OpenClaw-compatible tool names while forwarding to existing implementations.
type AliasTool struct {
	name        string
	description string
	base        Tool
	argMap      map[string]string
}

func NewAliasTool(name, description string, base Tool, argMap map[string]string) *AliasTool {
	return &AliasTool{name: name, description: description, base: base, argMap: argMap}
}

func (t *AliasTool) Name() string { return t.name }
func (t *AliasTool) Description() string {
	if t.description != "" {
		return t.description
	}
	return t.base.Description()
}
func (t *AliasTool) Parameters() map[string]interface{} { return t.base.Parameters() }

func (t *AliasTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		args = map[string]interface{}{}
	}
	normalized := make(map[string]interface{}, len(args)+len(t.argMap))
	for key, value := range args {
		normalized[key] = value
	}
	if len(t.argMap) > 0 {
		for from, to := range t.argMap {
			if v, ok := normalized[from]; ok {
				if _, exists := normalized[to]; !exists {
					normalized[to] = v
				}
			}
		}
	}
	return t.base.Execute(ctx, normalized)
}
