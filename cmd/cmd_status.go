package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/nodes"
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
		fmt.Printf("Nodes P2P: enabled=%t transport=%s\n", cfg.Gateway.Nodes.P2P.Enabled, strings.TrimSpace(cfg.Gateway.Nodes.P2P.Transport))
		fmt.Printf("Nodes P2P ICE: stun=%d ice=%d\n", len(cfg.Gateway.Nodes.P2P.STUNServers), len(cfg.Gateway.Nodes.P2P.ICEServers))
		ns := nodes.DefaultManager().List()
		if len(ns) > 0 {
			online := 0
			caps := map[string]int{"run": 0, "model": 0, "camera": 0, "screen": 0, "location": 0, "canvas": 0}
			for _, n := range ns {
				if n.Online {
					online++
				}
				if n.Capabilities.Run {
					caps["run"]++
				}
				if n.Capabilities.Model {
					caps["model"]++
				}
				if n.Capabilities.Camera {
					caps["camera"]++
				}
				if n.Capabilities.Screen {
					caps["screen"]++
				}
				if n.Capabilities.Location {
					caps["location"]++
				}
				if n.Capabilities.Canvas {
					caps["canvas"]++
				}
			}
			fmt.Printf("Nodes: total=%d online=%d\n", len(ns), online)
			fmt.Printf("Nodes Capabilities: run=%d model=%d camera=%d screen=%d location=%d canvas=%d\n", caps["run"], caps["model"], caps["camera"], caps["screen"], caps["location"], caps["canvas"])
		}
	}
}

