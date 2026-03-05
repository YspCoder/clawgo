package tools

import (
	"path/filepath"
	"strings"
)

func normalizeMemoryNamespace(in string) string {
	v := normalizeSubagentIdentifier(in)
	if v == "" {
		return "main"
	}
	return v
}

func memoryNamespaceBaseDir(workspace, namespace string) string {
	ns := normalizeMemoryNamespace(namespace)
	if ns == "main" {
		return workspace
	}
	return filepath.Join(workspace, "agents", ns)
}

func parseMemoryNamespaceArg(args map[string]interface{}) string {
	if args == nil {
		return "main"
	}
	raw, _ := args["namespace"].(string)
	return normalizeMemoryNamespace(raw)
}

func isPathUnder(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}
