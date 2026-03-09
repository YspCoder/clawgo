package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"clawgo/pkg/config"
)

type onboardOptions struct {
	syncWebUIOnly bool
}

func onboard() {
	opts := parseOnboardOptions(os.Args[2:])
	if opts.syncWebUIOnly {
		cfg := config.DefaultConfig()
		workspace := cfg.WorkspacePath()
		createWorkspaceTemplates(workspace, true)
		fmt.Printf("%s embedded WebUI refreshed in %s\n", logo, filepath.Join(workspace, "webui"))
		return
	}

	configPath := getConfigPath()

	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Config already exists at %s\n", configPath)
		fmt.Print("Overwrite? (y/n): ")
		var response string
		fmt.Scanln(&response)
		if response != "y" {
			fmt.Println("Aborted.")
			return
		}
	}

	cfg := config.DefaultConfig()
	if err := config.SaveConfig(configPath, cfg); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		os.Exit(1)
	}

	workspace := cfg.WorkspacePath()
	createWorkspaceTemplates(workspace, false)

	fmt.Printf("%s clawgo is ready!\n", logo)
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Configure CLIProxyAPI at", configPath)
	fmt.Println("     Ensure CLIProxyAPI is running: https://github.com/router-for-me/CLIProxyAPI")
	fmt.Println("     Set providers.<name>.models for Responses API")
	fmt.Println("  2. Chat: clawgo agent -m \"Hello!\"")
}

func ensureConfigOnboard(configPath string, defaults *config.Config) (string, error) {
	if defaults == nil {
		return "", fmt.Errorf("defaults is nil")
	}
	if defaults.Gateway.Token == "" {
		defaults.Gateway.Token = config.DefaultConfig().Gateway.Token
	}

	exists := true
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		exists = false
	} else if err != nil {
		return "", err
	}

	if err := config.SaveConfig(configPath, defaults); err != nil {
		return "", err
	}
	if exists {
		return "overwritten", nil
	}
	return "created", nil
}

func parseOnboardOptions(args []string) onboardOptions {
	var opts onboardOptions
	for _, arg := range args {
		if strings.EqualFold(strings.TrimSpace(arg), "--sync-webui") {
			opts.syncWebUIOnly = true
		}
	}
	return opts
}

func copyEmbeddedToTarget(targetDir string, overwrite func(relPath string) bool) error {
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	return fs.WalkDir(embeddedFiles, "workspace", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		data, err := embeddedFiles.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded file %s: %w", path, err)
		}

		relPath, err := filepath.Rel("workspace", path)
		if err != nil {
			return fmt.Errorf("failed to get relative path for %s: %w", path, err)
		}
		targetPath := filepath.Join(targetDir, relPath)
		if _, statErr := os.Stat(targetPath); statErr == nil {
			if overwrite == nil || !overwrite(relPath) {
				return nil
			}
		} else if !os.IsNotExist(statErr) {
			return statErr
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", filepath.Dir(targetPath), err)
		}
		if err := os.WriteFile(targetPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", targetPath, err)
		}
		fmt.Printf("  Created %s\n", relPath)
		return nil
	})
}

func createWorkspaceTemplates(workspace string, overwriteWebUI bool) {
	var overwrite func(relPath string) bool
	if overwriteWebUI {
		overwrite = func(relPath string) bool {
			relPath = filepath.ToSlash(relPath)
			return strings.HasPrefix(relPath, "webui/")
		}
	}
	err := copyEmbeddedToTarget(workspace, overwrite)
	if err != nil {
		fmt.Printf("Error copying workspace templates: %v\n", err)
	}
}
