package tools

import (
	"path/filepath"
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
	return normalizeMemoryNamespace(MapStringArg(args, "namespace"))
}
