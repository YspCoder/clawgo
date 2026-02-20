package main

import (
	"fmt"
	"os"
	"strings"

	"clawgo/pkg/providers"
)

func statusCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	configPath := getConfigPath()

	fmt.Printf("%s clawgo Status\n\n", logo)

	if _, err := os.Stat(configPath); err == nil {
		fmt.Println("Config:", configPath, "✓")
	} else {
		fmt.Println("Config:", configPath, "✗")
	}

	workspace := cfg.WorkspacePath()
	if _, err := os.Stat(workspace); err == nil {
		fmt.Println("Workspace:", workspace, "✓")
	} else {
		fmt.Println("Workspace:", workspace, "✗")
	}

	if _, err := os.Stat(configPath); err == nil {
		activeProvider := cfg.Providers.Proxy
		activeProxyName := "proxy"
		if name := strings.TrimSpace(cfg.Agents.Defaults.Proxy); name != "" && name != "proxy" {
			if p, ok := cfg.Providers.Proxies[name]; ok {
				activeProvider = p
				activeProxyName = name
			}
		}
		activeModel := ""
		for _, m := range activeProvider.Models {
			if s := strings.TrimSpace(m); s != "" {
				activeModel = s
				break
			}
		}
		fmt.Printf("Model: %s\n", activeModel)
		fmt.Printf("Proxy: %s\n", activeProxyName)
		fmt.Printf("CLIProxyAPI Base: %s\n", cfg.Providers.Proxy.APIBase)
		fmt.Printf("Supports /v1/responses/compact: %v\n", providers.ProviderSupportsResponsesCompact(cfg, activeProxyName))
		hasKey := cfg.Providers.Proxy.APIKey != ""
		status := "not set"
		if hasKey {
			status = "✓"
		}
		fmt.Printf("CLIProxyAPI Key: %s\n", status)
		fmt.Printf("Logging: %v\n", cfg.Logging.Enabled)
		if cfg.Logging.Enabled {
			fmt.Printf("Log File: %s\n", cfg.LogFilePath())
			fmt.Printf("Log Max Size: %d MB\n", cfg.Logging.MaxSizeMB)
			fmt.Printf("Log Retention: %d days\n", cfg.Logging.RetentionDays)
		}
	}
}
