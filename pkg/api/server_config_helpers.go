package api

import (
	"sort"
	"strings"
)

func mergeJSONMap(base, override map[string]interface{}) map[string]interface{} {
	if base == nil {
		base = map[string]interface{}{}
	}
	for k, v := range override {
		if bv, ok := base[k]; ok {
			bm, ok1 := bv.(map[string]interface{})
			om, ok2 := v.(map[string]interface{})
			if ok1 && ok2 {
				base[k] = mergeJSONMap(bm, om)
				continue
			}
		}
		base[k] = v
	}
	return base
}

func getPathValue(m map[string]interface{}, path string) interface{} {
	if m == nil || strings.TrimSpace(path) == "" {
		return nil
	}
	parts := strings.Split(path, ".")
	var cur interface{} = m
	for _, p := range parts {
		node, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur = node[p]
	}
	return cur
}

func collectRiskyConfigPaths(oldMap, newMap map[string]interface{}) []string {
	paths := []string{
		"channels.telegram.token",
		"channels.telegram.allow_from",
		"channels.telegram.allow_chats",
		"models.providers.openai.api_base",
		"models.providers.openai.api_key",
		"runtime.providers.openai.api_base",
		"runtime.providers.openai.api_key",
		"gateway.token",
		"gateway.port",
	}
	seen := map[string]bool{}
	for _, path := range paths {
		seen[path] = true
	}
	for _, name := range collectProviderNames(oldMap, newMap) {
		for _, field := range []string{"api_base", "api_key"} {
			path := "models.providers." + name + "." + field
			if !seen[path] {
				paths = append(paths, path)
				seen[path] = true
			}
			normalizedPath := "runtime.providers." + name + "." + field
			if !seen[normalizedPath] {
				paths = append(paths, normalizedPath)
				seen[normalizedPath] = true
			}
		}
	}
	return paths
}

func collectProviderNames(maps ...map[string]interface{}) []string {
	seen := map[string]bool{}
	names := make([]string, 0)
	for _, root := range maps {
		models, _ := root["models"].(map[string]interface{})
		providers, _ := models["providers"].(map[string]interface{})
		for name := range providers {
			if strings.TrimSpace(name) == "" || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
		runtimeMap, _ := root["runtime"].(map[string]interface{})
		runtimeProviders, _ := runtimeMap["providers"].(map[string]interface{})
		for name := range runtimeProviders {
			if strings.TrimSpace(name) == "" || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func hotReloadFieldInfo() []map[string]interface{} {
	return []map[string]interface{}{
		{"path": "logging.*", "name": "Logging", "description": "Log level, persistence, and related settings"},
		{"path": "sentinel.*", "name": "Sentinel", "description": "Health checks and auto-heal behavior"},
		{"path": "agents.*", "name": "Agent", "description": "Models, policies, and default behavior"},
		{"path": "models.providers.*", "name": "Providers", "description": "LLM provider registry and auth settings"},
		{"path": "tools.*", "name": "Tools", "description": "Tool toggles and runtime options"},
		{"path": "channels.*", "name": "Channels", "description": "Telegram and other channel settings"},
		{"path": "cron.*", "name": "Cron", "description": "Global cron runtime settings"},
		{"path": "agents.defaults.heartbeat.*", "name": "Heartbeat", "description": "Heartbeat interval and prompt template"},
		{"path": "gateway.*", "name": "Gateway", "description": "Mostly hot-reloadable; host/port may require restart"},
	}
}
