package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/providers"
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
		fmt.Println("Config:", configPath, "[ok]")
	} else {
		fmt.Println("Config:", configPath, "[missing]")
	}

	workspace := cfg.WorkspacePath()
	if _, err := os.Stat(workspace); err == nil {
		fmt.Println("Workspace:", workspace, "[ok]")
	} else {
		fmt.Println("Workspace:", workspace, "[missing]")
	}

	if _, err := os.Stat(configPath); err == nil {
		activeProviderName, activeModel := config.ParseProviderModelRef(cfg.Agents.Defaults.Model.Primary)
		if activeProviderName == "" {
			activeProviderName = config.PrimaryProviderName(cfg)
		}
		activeProvider, _ := config.ProviderConfigByName(cfg, activeProviderName)
		fmt.Printf("Primary Model: %s\n", activeModel)
		fmt.Printf("Primary Provider: %s\n", activeProviderName)
		fmt.Printf("Provider Base URL: %s\n", activeProvider.APIBase)
		fmt.Printf("Responses Compact: %v\n", providers.ProviderSupportsResponsesCompact(cfg, activeProviderName))
		hasKey := strings.TrimSpace(activeProvider.APIKey) != ""
		status := "not set"
		if hasKey {
			status = "configured"
		}
		fmt.Printf("API Key Status: %s\n", status)
		fmt.Printf("Logging: %v\n", cfg.Logging.Enabled)
		if cfg.Logging.Enabled {
			fmt.Printf("Log File: %s\n", cfg.LogFilePath())
			fmt.Printf("Log Max Size: %d MB\n", cfg.Logging.MaxSizeMB)
			fmt.Printf("Log Retention: %d days\n", cfg.Logging.RetentionDays)
		}
	}
}

