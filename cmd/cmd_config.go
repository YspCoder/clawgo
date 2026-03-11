package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/configops"
	"github.com/YspCoder/clawgo/pkg/providers"
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
	fmt.Println("  clawgo config get models.providers.openai.api_base")
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

	fmt.Printf("鉁?Updated %s = %v\n", path, value)
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
		fmt.Println("鉁?Gateway hot reload signal sent")
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
	fmt.Println("鉁?Gateway hot reload signal sent")
}

func configCheckCmd() {
	cfg, err := config.LoadConfig(getConfigPath())
	if err != nil {
		fmt.Printf("Config load failed: %v\n", err)
		return
	}
	validationErrors := config.Validate(cfg)
	if len(validationErrors) == 0 {
		fmt.Println("鉁?Config validation passed")
		return
	}

	fmt.Println("鉁?Config validation failed:")
	for _, ve := range validationErrors {
		fmt.Printf("  - %v\n", ve)
	}
}

func providerCmd() {
	if len(os.Args) >= 3 {
		switch strings.TrimSpace(os.Args[2]) {
		case "login":
			providerLoginCmd()
			return
		case "list":
			providerListCmd()
			return
		case "use":
			providerUseCmd()
			return
		case "configure":
			// Continue into the interactive editor below.
		}
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	reader := bufio.NewReader(os.Stdin)
	defaultProvider, defaultModel := config.ParseProviderModelRef(cfg.Agents.Defaults.Model.Primary)
	if defaultProvider == "" {
		defaultProvider = config.PrimaryProviderName(cfg)
	}
	available := providerNames(cfg)
	fmt.Printf("Current primary provider: %s\n", defaultProvider)
	if defaultModel != "" {
		fmt.Printf("Current primary model: %s\n", defaultModel)
	}
	fmt.Printf("Available providers: %s\n", strings.Join(available, ", "))

	argName := ""
	if len(os.Args) >= 3 {
		argName = strings.TrimSpace(os.Args[2])
	}
	if argName == "" || strings.HasPrefix(argName, "-") {
		argName = defaultProvider
	}
	providerName := promptLine(reader, "Provider name to configure", argName)
	if providerName == "" {
		providerName = defaultProvider
	}

	pc := providerConfigByName(cfg, providerName)
	if pc.TimeoutSec <= 0 {
		pc.TimeoutSec = 90
	}
	if strings.TrimSpace(pc.Auth) == "" {
		pc.Auth = "bearer"
	}
	if len(pc.Models) == 0 {
		pc.Models = []string{"gpt-5.4"}
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
	pc.Auth = promptLine(reader, "auth (bearer/oauth/hybrid/none)", pc.Auth)
	timeoutRaw := promptLine(reader, "timeout_sec", fmt.Sprintf("%d", pc.TimeoutSec))
	pc.TimeoutSec = parseIntOrDefault(timeoutRaw, pc.TimeoutSec)
	pc.SupportsResponsesCompact = promptBool(reader, "supports_responses_compact", pc.SupportsResponsesCompact)
	if strings.EqualFold(strings.TrimSpace(pc.Auth), "oauth") || strings.EqualFold(strings.TrimSpace(pc.Auth), "hybrid") {
		pc.OAuth.Provider = promptLine(reader, "oauth.provider", firstNonEmptyString(pc.OAuth.Provider, "codex"))
		pc.OAuth.NetworkProxy = promptLine(reader, "oauth.network_proxy", pc.OAuth.NetworkProxy)
		pc.OAuth.CredentialFile = promptLine(reader, "oauth.credential_file", pc.OAuth.CredentialFile)
		pc.OAuth.CallbackPort = parseIntOrDefault(promptLine(reader, "oauth.callback_port", fmt.Sprintf("%d", defaultInt(pc.OAuth.CallbackPort, 1455))), defaultInt(pc.OAuth.CallbackPort, 1455))
		pc.OAuth.CooldownSec = parseIntOrDefault(promptLine(reader, "oauth.cooldown_sec", fmt.Sprintf("%d", defaultInt(pc.OAuth.CooldownSec, 900))), defaultInt(pc.OAuth.CooldownSec, 900))
	}

	setProviderConfigByName(cfg, providerName, pc)

	currentPrimaryProvider, currentPrimaryModel := config.ParseProviderModelRef(cfg.Agents.Defaults.Model.Primary)
	makePrimary := promptBool(reader, fmt.Sprintf("Set %s as agents.defaults.model.primary provider", providerName), providerName == currentPrimaryProvider)
	if makePrimary {
		targetModel := currentPrimaryModel
		if targetModel == "" && len(pc.Models) > 0 {
			targetModel = pc.Models[0]
		}
		cfg.Agents.Defaults.Model.Primary = providerName + "/" + targetModel
	}

	currentFallbacks := strings.Join(cfg.Agents.Defaults.Model.Fallbacks, ",")
	fallbackRaw := promptLine(reader, "agents.defaults.model.fallbacks (comma-separated provider/model refs)", currentFallbacks)
	fallbacks := parseCSV(fallbackRaw)
	valid := map[string]struct{}{}
	for _, name := range providerNames(cfg) {
		valid[name] = struct{}{}
	}
	filteredFallbacks := make([]string, 0, len(fallbacks))
	seen := map[string]struct{}{}
	defaultRef := strings.TrimSpace(cfg.Agents.Defaults.Model.Primary)
	for _, fb := range fallbacks {
		if fb == "" || fb == defaultRef {
			continue
		}
		fbProvider, fbModel := config.ParseProviderModelRef(fb)
		if fbProvider == "" || fbModel == "" {
			fmt.Printf("Skip invalid fallback provider/model ref: %s\n", fb)
			continue
		}
		if _, ok := valid[fbProvider]; !ok {
			fmt.Printf("Skip unknown fallback provider: %s\n", fb)
			continue
		}
		if _, ok := seen[fb]; ok {
			continue
		}
		seen[fb] = struct{}{}
		filteredFallbacks = append(filteredFallbacks, fb)
	}
	cfg.Agents.Defaults.Model.Fallbacks = filteredFallbacks

	if err := config.SaveConfig(getConfigPath(), cfg); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("鉁?Provider configuration saved.")
	running, err := triggerGatewayReload()
	if err != nil {
		if running {
			fmt.Printf("Hot reload not applied: %v\n", err)
			return
		}
		fmt.Printf("Gateway not running, reload skipped: %v\n", err)
		return
	}
	fmt.Println("鉁?Gateway hot reload signal sent")
}

func providerListCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
	primary := strings.TrimSpace(cfg.Agents.Defaults.Model.Primary)
	names := providerNames(cfg)
	for _, name := range names {
		pc, _ := config.ProviderConfigByName(cfg, name)
		models := strings.Join(pc.Models, ",")
		if models == "" {
			models = "-"
		}
		marker := " "
		if strings.HasPrefix(primary, name+"/") {
			marker = "*"
		}
		fmt.Printf("%s %s  auth=%s  models=%s  api_base=%s\n", marker, name, strings.TrimSpace(pc.Auth), models, strings.TrimSpace(pc.APIBase))
	}
	if primary != "" {
		fmt.Printf("\nPrimary: %s\n", primary)
	}
}

func providerUseCmd() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: clawgo provider use <provider/model>")
		return
	}
	ref := strings.TrimSpace(os.Args[3])
	providerName, modelName := config.ParseProviderModelRef(ref)
	if providerName == "" || modelName == "" {
		fmt.Println("Error: expected provider/model")
		return
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
	pc, ok := config.ProviderConfigByName(cfg, providerName)
	if !ok {
		fmt.Printf("Error: unknown provider %q\n", providerName)
		os.Exit(1)
	}
	foundModel := false
	for _, candidate := range pc.Models {
		if strings.TrimSpace(candidate) == modelName {
			foundModel = true
			break
		}
	}
	if !foundModel {
		fmt.Printf("Error: model %q not found in provider %q\n", modelName, providerName)
		os.Exit(1)
	}
	cfg.Agents.Defaults.Model.Primary = providerName + "/" + modelName
	if err := config.SaveConfig(getConfigPath(), cfg); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Primary model set to %s\n", cfg.Agents.Defaults.Model.Primary)
	if running, reloadErr := triggerGatewayReload(); reloadErr == nil {
		fmt.Println("Gateway hot reload signal sent")
	} else if running {
		fmt.Printf("Hot reload not applied: %v\n", reloadErr)
	}
}

func providerLoginCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	providerName, _ := config.ParseProviderModelRef(cfg.Agents.Defaults.Model.Primary)
	if providerName == "" {
		providerName = config.PrimaryProviderName(cfg)
	}
	manual := false
	noBrowser := false
	accountLabel := ""
	networkProxy := ""
	for i := 3; i < len(os.Args); i++ {
		arg := strings.TrimSpace(os.Args[i])
		switch arg {
		case "--manual":
			manual = true
		case "--no-browser":
			noBrowser = true
		case "--label":
			if i+1 < len(os.Args) {
				i++
				accountLabel = strings.TrimSpace(os.Args[i])
			}
		case "--proxy":
			if i+1 < len(os.Args) {
				i++
				networkProxy = strings.TrimSpace(os.Args[i])
			}
		case "":
		default:
			if strings.HasPrefix(arg, "--label=") {
				accountLabel = strings.TrimSpace(strings.TrimPrefix(arg, "--label="))
				continue
			}
			if strings.HasPrefix(arg, "--proxy=") {
				networkProxy = strings.TrimSpace(strings.TrimPrefix(arg, "--proxy="))
				continue
			}
			if !strings.HasPrefix(arg, "-") {
				providerName = arg
			}
		}
	}

	pc := providerConfigByName(cfg, providerName)
	if !strings.EqualFold(strings.TrimSpace(pc.Auth), "oauth") && !strings.EqualFold(strings.TrimSpace(pc.Auth), "hybrid") {
		fmt.Printf("Provider %s is not configured with auth=oauth/hybrid\n", providerName)
		os.Exit(1)
	}
	if manual {
		noBrowser = true
	}
	if manual && strings.TrimSpace(pc.OAuth.RedirectURL) == "" && pc.OAuth.CallbackPort <= 0 {
		pc.OAuth.CallbackPort = 1455
	}
	if strings.TrimSpace(networkProxy) == "" {
		networkProxy = strings.TrimSpace(pc.OAuth.NetworkProxy)
	}

	timeout := pc.TimeoutSec
	if timeout <= 0 {
		timeout = 90
	}
	oauth, err := providers.NewOAuthLoginManager(pc, time.Duration(timeout)*time.Second)
	if err != nil {
		fmt.Printf("Error preparing oauth login: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	session, models, err := oauth.Login(ctx, pc.APIBase, providers.OAuthLoginOptions{
		Manual:       manual,
		NoBrowser:    noBrowser,
		Reader:       os.Stdin,
		AccountLabel: accountLabel,
		NetworkProxy: networkProxy,
	})
	if err != nil {
		fmt.Printf("OAuth login failed: %v\n", err)
		os.Exit(1)
	}

	if len(models) > 0 {
		pc.Models = models
	}
	if session.CredentialFile != "" {
		pc.OAuth.CredentialFile = session.CredentialFile
		pc.OAuth.CredentialFiles = appendUniqueCSV(pc.OAuth.CredentialFiles, session.CredentialFile)
	} else if pc.OAuth.CredentialFile == "" {
		pc.OAuth.CredentialFile = oauth.CredentialFile()
		pc.OAuth.CredentialFiles = appendUniqueCSV(pc.OAuth.CredentialFiles, pc.OAuth.CredentialFile)
	}
	setProviderConfigByName(cfg, providerName, pc)

	if err := config.SaveConfig(getConfigPath(), cfg); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("OAuth login succeeded for provider %s\n", providerName)
	if manual {
		fmt.Println("Mode: manual callback URL paste")
	} else if noBrowser {
		fmt.Println("Mode: local callback listener without auto-opening browser")
	}
	if session.Email != "" {
		fmt.Printf("Account: %s\n", session.Email)
	}
	if session.NetworkProxy != "" {
		fmt.Printf("Network proxy: %s\n", session.NetworkProxy)
	}
	fmt.Printf("Credential file: %s\n", firstNonEmptyString(session.CredentialFile, oauth.CredentialFile()))
	if len(pc.OAuth.CredentialFiles) > 1 {
		fmt.Printf("OAuth accounts: %d\n", len(pc.OAuth.CredentialFiles))
	}
	if len(models) > 0 {
		fmt.Printf("Discovered models: %s\n", strings.Join(models, ", "))
	}
	if running, reloadErr := triggerGatewayReload(); reloadErr == nil {
		fmt.Println("Gateway hot reload signal sent")
	} else if running {
		fmt.Printf("Hot reload not applied: %v\n", reloadErr)
	}
}

func providerNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Models.Providers))
	for k := range cfg.Models.Providers {
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
	if pc, ok := config.ProviderConfigByName(cfg, name); ok {
		return pc
	}
	return config.ProviderConfig{
		TimeoutSec: 90,
		Auth:       "bearer",
		Models:     []string{"gpt-5.4"},
	}
}

func setProviderConfigByName(cfg *config.Config, name string, pc config.ProviderConfig) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if cfg.Models.Providers == nil {
		cfg.Models.Providers = map[string]config.ProviderConfig{}
	}
	cfg.Models.Providers[name] = pc
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

func defaultInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func appendUniqueCSV(values []string, value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return values
	}
	for _, item := range values {
		if strings.TrimSpace(item) == trimmed {
			return values
		}
	}
	return append(values, trimmed)
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
