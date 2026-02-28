package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"clawgo/pkg/config"
	"clawgo/pkg/configops"
)

func configCmd() {
	if len(os.Args) < 3 {
		configHelp()
		return
	}

	switch os.Args[2] {
	case "set":
		configSetCmd()
	case "get":
		configGetCmd()
	case "check":
		configCheckCmd()
	case "reload":
		configReloadCmd()
	case "login":
		configLoginCmd()
	default:
		fmt.Printf("Unknown config command: %s\n", os.Args[2])
		configHelp()
	}
}

func configHelp() {
	fmt.Println("\nConfig commands:")
	fmt.Println("  set <path> <value>     Set config value and trigger hot reload")
	fmt.Println("  get <path>             Get config value")
	fmt.Println("  check                  Validate current config")
	fmt.Println("  reload                 Trigger gateway hot reload")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  clawgo config set channels.telegram.enabled true")
	fmt.Println("  clawgo config set channels.telegram.enable true")
	fmt.Println("  clawgo config get providers.proxy.api_base")
	fmt.Println("  clawgo config check")
	fmt.Println("  clawgo config reload")
}

func configSetCmd() {
	if len(os.Args) < 5 {
		fmt.Println("Usage: clawgo config set <path> <value>")
		return
	}

	configPath := getConfigPath()
	cfgMap, err := loadConfigAsMap(configPath)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	path := normalizeConfigPath(os.Args[3])
	args := os.Args[4:]
	valueParts := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		part := args[i]
		if part == "--debug" || part == "-d" {
			continue
		}
		if part == "--config" {
			i++
			continue
		}
		if strings.HasPrefix(part, "--config=") {
			continue
		}
		valueParts = append(valueParts, part)
	}
	if len(valueParts) == 0 {
		fmt.Println("Error: value is required")
		return
	}
	value := parseConfigValue(strings.Join(valueParts, " "))
	if err := setMapValueByPath(cfgMap, path, value); err != nil {
		fmt.Printf("Error setting value: %v\n", err)
		return
	}

	data, err := json.MarshalIndent(cfgMap, "", "  ")
	if err != nil {
		fmt.Printf("Error serializing config: %v\n", err)
		return
	}
	backupPath, err := writeConfigAtomicWithBackup(configPath, data)
	if err != nil {
		fmt.Printf("Error writing config: %v\n", err)
		return
	}

	fmt.Printf("✓ Updated %s = %v\n", path, value)
	running, err := triggerGatewayReload()
	if err != nil {
		if running {
			if rbErr := rollbackConfigFromBackup(configPath, backupPath); rbErr != nil {
				fmt.Printf("Hot reload failed and rollback failed: %v\n", rbErr)
			} else {
				fmt.Printf("Hot reload failed, config rolled back: %v\n", err)
			}
			return
		}
		fmt.Printf("Updated config file. Hot reload not applied: %v\n", err)
	} else {
		fmt.Println("✓ Gateway hot reload signal sent")
	}
}

func configGetCmd() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: clawgo config get <path>")
		return
	}

	configPath := getConfigPath()
	cfgMap, err := loadConfigAsMap(configPath)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	path := normalizeConfigPath(os.Args[3])
	value, ok := getMapValueByPath(cfgMap, path)
	if !ok {
		fmt.Printf("Path not found: %s\n", path)
		return
	}

	data, err := json.Marshal(value)
	if err != nil {
		fmt.Printf("%v\n", value)
		return
	}
	fmt.Println(string(data))
}

func configReloadCmd() {
	if _, err := triggerGatewayReload(); err != nil {
		fmt.Printf("Hot reload not applied: %v\n", err)
		return
	}
	fmt.Println("✓ Gateway hot reload signal sent")
}

func configCheckCmd() {
	cfg, err := config.LoadConfig(getConfigPath())
	if err != nil {
		fmt.Printf("Config load failed: %v\n", err)
		return
	}
	validationErrors := config.Validate(cfg)
	if len(validationErrors) == 0 {
		fmt.Println("✓ Config validation passed")
		return
	}

	fmt.Println("✗ Config validation failed:")
	for _, ve := range validationErrors {
		fmt.Printf("  - %v\n", ve)
	}
}

func configLoginCmd() {
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


func loadConfigAsMap(path string) (map[string]interface{}, error) {
	return configops.LoadConfigAsMap(path)
}

func normalizeConfigPath(path string) string {
	return configops.NormalizeConfigPath(path)
}

func parseConfigValue(raw string) interface{} {
	return configops.ParseConfigValue(raw)
}

func setMapValueByPath(root map[string]interface{}, path string, value interface{}) error {
	return configops.SetMapValueByPath(root, path, value)
}

func getMapValueByPath(root map[string]interface{}, path string) (interface{}, bool) {
	return configops.GetMapValueByPath(root, path)
}

func writeConfigAtomicWithBackup(configPath string, data []byte) (string, error) {
	return configops.WriteConfigAtomicWithBackup(configPath, data)
}

func rollbackConfigFromBackup(configPath, backupPath string) error {
	return configops.RollbackConfigFromBackup(configPath, backupPath)
}

func triggerGatewayReload() (bool, error) {
	return configops.TriggerGatewayReload(getConfigPath(), errGatewayNotRunning)
}
