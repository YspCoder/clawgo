package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"clawgo/pkg/config"
	"clawgo/pkg/logger"
)

func copyDirectory(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

func normalizeCLIArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}

	normalized := []string{args[0]}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--debug" || arg == "-d" {
			continue
		}
		if arg == "--config" {
			if i+1 < len(args) {
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "--config=") {
			continue
		}
		normalized = append(normalized, arg)
	}
	return normalized
}

func detectConfigPathFromArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--config" && i+1 < len(args) {
			return strings.TrimSpace(args[i+1])
		}
		if strings.HasPrefix(arg, "--config=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, "--config="))
		}
	}
	return ""
}

func printHelp() {
	fmt.Printf("%s clawgo - Personal AI Assistant v%s\n\n", logo, version)
	fmt.Println("Usage: clawgo <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  onboard     Initialize clawgo configuration and workspace")
	fmt.Println("  agent       Interact with the agent directly")
	fmt.Println("  gateway     Register/manage gateway service")
	fmt.Println("  status      Show clawgo status")
	fmt.Println("  config      Get/set config values")
	fmt.Println("  cron        Manage scheduled tasks")
	fmt.Println("  login       Configure CLIProxyAPI upstream")
	fmt.Println("  channel     Test and manage messaging channels")
	fmt.Println("  skills      Manage skills (install, list, remove)")
	fmt.Println("  uninstall   Uninstall clawgo components")
	fmt.Println("  version     Show version information")
	fmt.Println()
	fmt.Println("Global options:")
	fmt.Println("  --config <path>         Use custom config file")
	fmt.Println("  --debug, -d             Enable debug logging")
	fmt.Println()
	fmt.Println("Gateway service:")
	fmt.Println("  clawgo gateway                  # register service")
	fmt.Println("  clawgo gateway start|stop|restart|status")
	fmt.Println("  clawgo gateway run              # run foreground")
	fmt.Println()
	fmt.Println("Uninstall:")
	fmt.Println("  clawgo uninstall                # remove gateway service")
	fmt.Println("  clawgo uninstall --purge        # also remove config/workspace dir")
	fmt.Println("  clawgo uninstall --remove-bin   # also remove current executable")
}

func getConfigPath() string {
	if strings.TrimSpace(globalConfigPathOverride) != "" {
		return globalConfigPathOverride
	}
	if fromEnv := strings.TrimSpace(os.Getenv("CLAWGO_CONFIG")); fromEnv != "" {
		return fromEnv
	}
	args := os.Args
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--config" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, "--config=") {
			return strings.TrimPrefix(arg, "--config=")
		}
	}
	return filepath.Join(config.GetConfigDir(), "config.json")
}

func loadConfig() (*config.Config, error) {
	cfg, err := config.LoadConfig(getConfigPath())
	if err != nil {
		return nil, err
	}
	configureLogging(cfg)
	return cfg, nil
}

func configureLogging(cfg *config.Config) {
	if !cfg.Logging.Enabled {
		logger.DisableFileLogging()
		return
	}

	logFile := cfg.LogFilePath()
	if err := logger.EnableFileLoggingWithRotation(logFile, cfg.Logging.MaxSizeMB, cfg.Logging.RetentionDays); err != nil {
		fmt.Printf("Warning: failed to enable file logging: %v\n", err)
	}
}
