package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
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

func providerCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	reader := bufio.NewReader(os.Stdin)
	defaultProxy := strings.TrimSpace(cfg.Agents.Defaults.Proxy)
	if defaultProxy == "" {
		defaultProxy = "proxy"
	}
	available := providerNames(cfg)
	fmt.Printf("Current default provider: %s\n", defaultProxy)
	fmt.Printf("Available providers: %s\n", strings.Join(available, ", "))

	argName := ""
	if len(os.Args) >= 3 {
		argName = strings.TrimSpace(os.Args[2])
	}
	if argName == "" || strings.HasPrefix(argName, "-") {
		argName = defaultProxy
	}
	providerName := promptLine(reader, "Provider name to configure", argName)
	if providerName == "" {
		providerName = defaultProxy
	}

	pc := providerConfigByName(cfg, providerName)
	if pc.TimeoutSec <= 0 {
		pc.TimeoutSec = 90
	}
	if strings.TrimSpace(pc.Auth) == "" {
		pc.Auth = "bearer"
	}
	if len(pc.Models) == 0 {
		pc.Models = append([]string{}, cfg.Providers.Proxy.Models...)
	}
	if len(pc.Models) == 0 {
		pc.Models = []string{"glm-4.7"}
	}

	pc.APIBase = promptLine(reader, "api_base", pc.APIBase)
	apiKey := promptLine(reader, "api_key (leave empty to keep current)", "")
	if apiKey != "" {
		pc.APIKey = apiKey
	}
	modelsRaw := promptLine(reader, "models (comma-separated)", strings.Join(pc.Models, ","))
	if models := parseCSV(modelsRaw); len(models) > 0 {
		pc.Models = models
	}
	pc.Auth = promptLine(reader, "auth (bearer/oauth/none)", pc.Auth)
	timeoutRaw := promptLine(reader, "timeout_sec", fmt.Sprintf("%d", pc.TimeoutSec))
	pc.TimeoutSec = parseIntOrDefault(timeoutRaw, pc.TimeoutSec)
	pc.SupportsResponsesCompact = promptBool(reader, "supports_responses_compact", pc.SupportsResponsesCompact)

	setProviderConfigByName(cfg, providerName, pc)

	makeDefault := promptBool(reader, fmt.Sprintf("Set %s as agents.defaults.proxy", providerName), providerName == defaultProxy)
	if makeDefault {
		cfg.Agents.Defaults.Proxy = providerName
	}

	currentFallbacks := strings.Join(cfg.Agents.Defaults.ProxyFallbacks, ",")
	fallbackRaw := promptLine(reader, "agents.defaults.proxy_fallbacks (comma-separated names)", currentFallbacks)
	fallbacks := parseCSV(fallbackRaw)
	valid := map[string]struct{}{}
	for _, name := range providerNames(cfg) {
		valid[name] = struct{}{}
	}
	filteredFallbacks := make([]string, 0, len(fallbacks))
	seen := map[string]struct{}{}
	defaultName := strings.TrimSpace(cfg.Agents.Defaults.Proxy)
	for _, fb := range fallbacks {
		if fb == "" || fb == defaultName {
			continue
		}
		if _, ok := valid[fb]; !ok {
			fmt.Printf("Skip unknown fallback provider: %s\n", fb)
			continue
		}
		if _, ok := seen[fb]; ok {
			continue
		}
		seen[fb] = struct{}{}
		filteredFallbacks = append(filteredFallbacks, fb)
	}
	cfg.Agents.Defaults.ProxyFallbacks = filteredFallbacks

	if err := config.SaveConfig(getConfigPath(), cfg); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Provider configuration saved.")
	running, err := triggerGatewayReload()
	if err != nil {
		if running {
			fmt.Printf("Hot reload not applied: %v\n", err)
			return
		}
		fmt.Printf("Gateway not running, reload skipped: %v\n", err)
		return
	}
	fmt.Println("✓ Gateway hot reload signal sent")
}

func providerNames(cfg *config.Config) []string {
	names := []string{"proxy"}
	for k := range cfg.Providers.Proxies {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func providerConfigByName(cfg *config.Config, name string) config.ProviderConfig {
	name = strings.TrimSpace(name)
	if name == "" || name == "proxy" {
		return cfg.Providers.Proxy
	}
	if cfg.Providers.Proxies != nil {
		if pc, ok := cfg.Providers.Proxies[name]; ok {
			return pc
		}
	}
	return config.ProviderConfig{
		APIBase:    cfg.Providers.Proxy.APIBase,
		TimeoutSec: cfg.Providers.Proxy.TimeoutSec,
		Auth:       cfg.Providers.Proxy.Auth,
		Models:     append([]string{}, cfg.Providers.Proxy.Models...),
	}
}

func setProviderConfigByName(cfg *config.Config, name string, pc config.ProviderConfig) {
	name = strings.TrimSpace(name)
	if name == "" || name == "proxy" {
		cfg.Providers.Proxy = pc
		return
	}
	if cfg.Providers.Proxies == nil {
		cfg.Providers.Proxies = map[string]config.ProviderConfig{}
	}
	cfg.Providers.Proxies[name] = pc
}

func promptLine(reader *bufio.Reader, label, defaultValue string) string {
	label = strings.TrimSpace(label)
	if defaultValue != "" {
		fmt.Printf("%s [%s]: ", label, defaultValue)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultValue
	}
	return line
}

func promptBool(reader *bufio.Reader, label string, defaultValue bool) bool {
	def := "N"
	if defaultValue {
		def = "Y"
	}
	raw := promptLine(reader, label+" (y/n)", def)
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "y", "yes", "true", "1":
		return true
	case "n", "no", "false", "0":
		return false
	default:
		return defaultValue
	}
}

func parseCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func parseIntOrDefault(raw string, def int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return def
	}
	return v
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
