package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"clawgo/pkg/config"
)

func loginCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Configuring CLIProxyAPI...")
	fmt.Printf("Current Base: %s\n", cfg.Providers.Proxy.APIBase)

	fmt.Print("Enter CLIProxyAPI Base URL (e.g. http://localhost:8080/v1): ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	apiBase := strings.TrimSpace(line)
	if apiBase != "" {
		cfg.Providers.Proxy.APIBase = apiBase
	}

	fmt.Print("Enter API Key (optional): ")
	fmt.Scanln(&cfg.Providers.Proxy.APIKey)

	if err := config.SaveConfig(getConfigPath(), cfg); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ CLIProxyAPI configuration saved.")
}

func configureProvider(cfg *config.Config, provider string) {
	// Deprecated: Migrated to CLIProxyAPI logic in loginCmd
}
