package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func uninstallCmd() {
	purge := false
	removeBin := false

	for _, arg := range os.Args[2:] {
		switch arg {
		case "--purge":
			purge = true
		case "--remove-bin":
			removeBin = true
		}
	}

	if err := uninstallGatewayService(); err != nil {
		fmt.Printf("Gateway service uninstall warning: %v\n", err)
	} else {
		fmt.Println("✓ Gateway service uninstalled")
	}

	pidPath := filepath.Join(filepath.Dir(getConfigPath()), "gateway.pid")
	_ = os.Remove(pidPath)

	if purge {
		configDir := filepath.Dir(getConfigPath())
		if err := os.RemoveAll(configDir); err != nil {
			fmt.Printf("Failed to remove config directory %s: %v\n", configDir, err)
			os.Exit(1)
		}
		fmt.Printf("✓ Removed config/workspace directory: %s\n", configDir)
	}

	if removeBin {
		exePath, err := os.Executable()
		if err != nil {
			fmt.Printf("Failed to resolve executable path: %v\n", err)
			os.Exit(1)
		}
		if err := os.Remove(exePath); err != nil {
			fmt.Printf("Failed to remove executable %s: %v\n", exePath, err)
			os.Exit(1)
		}
		fmt.Printf("✓ Removed executable: %s\n", exePath)
	}
}

func uninstallGatewayService() error {
	switch runtime.GOOS {
	case "darwin":
		scope, plistPath, err := detectInstalledLaunchdService()
		if err != nil {
			return nil
		}
		_ = runLaunchctl(scope, "bootout", launchdDomainTarget(scope), plistPath)
		if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove launchd plist failed: %w", err)
		}
		return nil
	case "windows":
		_, taskName, err := detectInstalledWindowsTask()
		if err != nil {
			return nil
		}
		_ = stopGatewayProcessByPIDFile()
		if err := runSCHTASKS("/Delete", "/TN", taskName, "/F"); err != nil {
			return err
		}
		return nil
	}
	scope, unitPath, err := detectInstalledGatewayService()
	if err != nil {
		return nil
	}

	_ = runSystemctl(scope, "stop", gatewayServiceName)
	_ = runSystemctl(scope, "disable", gatewayServiceName)

	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit file failed: %w", err)
	}

	if err := runSystemctl(scope, "daemon-reload"); err != nil {
		return err
	}
	return nil
}
