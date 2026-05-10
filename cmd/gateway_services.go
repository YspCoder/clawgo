package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func gatewayInstallServiceCmd() error {
	switch runtime.GOOS {
	case "darwin":
		return gatewayInstallLaunchdService()
	case "windows":
		return gatewayInstallWindowsTask()
	}
	scope, unitPath, err := detectGatewayServiceScopeAndPath()
	if err != nil {
		return err
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path failed: %w", err)
	}
	exePath, _ = filepath.Abs(exePath)
	configPath := getConfigPath()
	workDir := filepath.Dir(exePath)

	unitContent := buildGatewayUnitContent(scope, exePath, configPath, workDir)
	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		return fmt.Errorf("create service directory failed: %w", err)
	}
	if err := os.WriteFile(unitPath, []byte(unitContent), 0644); err != nil {
		return fmt.Errorf("write service unit failed: %w", err)
	}

	if err := runSystemctl(scope, "daemon-reload"); err != nil {
		return err
	}
	if err := runSystemctl(scope, "enable", gatewayServiceName); err != nil {
		return err
	}

	fmt.Printf("Gateway service registered: %s (%s)\n", gatewayServiceName, scope)
	fmt.Printf("  Unit file: %s\n", unitPath)
	fmt.Println("  Start service:   clawgo gateway start")
	fmt.Println("  Restart service: clawgo gateway restart")
	fmt.Println("  Stop service:    clawgo gateway stop")
	return nil
}

func gatewayServiceControlCmd(action string) error {
	switch runtime.GOOS {
	case "darwin":
		return gatewayLaunchdControl(action)
	case "windows":
		return gatewayWindowsTaskControl(action)
	}
	scope, _, err := detectInstalledGatewayService()
	if err != nil {
		return err
	}
	return runSystemctl(scope, action, gatewayServiceName)
}

func gatewayScopePreference() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("CLAWGO_GATEWAY_SCOPE")))
	if v == "user" || v == "system" {
		return v
	}
	return ""
}

func detectGatewayServiceScopeAndPath() (string, string, error) {
	switch runtime.GOOS {
	case "linux":
	default:
		return "", "", fmt.Errorf("unsupported service manager for %s", runtime.GOOS)
	}
	switch gatewayScopePreference() {
	case "user":
		return userGatewayUnitPath()
	case "system":
		return "system", "/etc/systemd/system/" + gatewayServiceName, nil
	}
	if os.Geteuid() == 0 {
		return "system", "/etc/systemd/system/" + gatewayServiceName, nil
	}
	return userGatewayUnitPath()
}

func userGatewayUnitPath() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve user home failed: %w", err)
	}
	return "user", filepath.Join(home, ".config", "systemd", "user", gatewayServiceName), nil
}

func detectInstalledGatewayService() (string, string, error) {
	switch runtime.GOOS {
	case "darwin":
		return detectInstalledLaunchdService()
	case "windows":
		return detectInstalledWindowsTask()
	}
	systemPath := "/etc/systemd/system/" + gatewayServiceName
	userScope, userPath, err := userGatewayUnitPath()
	if err != nil {
		return "", "", err
	}

	systemExists := false
	if info, err := os.Stat(systemPath); err == nil && !info.IsDir() {
		systemExists = true
	}

	userExists := false
	if info, err := os.Stat(userPath); err == nil && !info.IsDir() {
		userExists = true
	}

	preferredScope := gatewayScopePreference()
	switch preferredScope {
	case "system":
		if systemExists {
			return "system", systemPath, nil
		}
		return "", "", fmt.Errorf("gateway service unit not found in system scope: %s", systemPath)
	case "user":
		if userExists {
			return userScope, userPath, nil
		}
		return "", "", fmt.Errorf("gateway service unit not found in user scope: %s", userPath)
	}

	// Auto-pick scope by current privilege to avoid non-root users accidentally
	// selecting system scope when both unit files exist.
	if os.Geteuid() == 0 {
		if systemExists {
			return "system", systemPath, nil
		}
		if userExists {
			return userScope, userPath, nil
		}
	} else {
		if userExists {
			return userScope, userPath, nil
		}
		if systemExists {
			return "system", systemPath, nil
		}
	}

	return "", "", fmt.Errorf("gateway service not registered. Run: clawgo gateway")
}

func buildGatewayUnitContent(scope, exePath, configPath, workDir string) string {
	quotedExec := fmt.Sprintf("%q gateway run --config %q", exePath, configPath)
	installTarget := "default.target"
	if scope == "system" {
		installTarget = "multi-user.target"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = filepath.Dir(configPath)
	}

	return fmt.Sprintf(`[Unit]
Description=ClawGo Gateway
After=network.target

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s
Restart=always
RestartSec=3
Environment=CLAWGO_CONFIG=%s
Environment=HOME=%s

[Install]
WantedBy=%s
`, workDir, quotedExec, configPath, home, installTarget)
}

func runSystemctl(scope string, args ...string) error {
	cmdArgs := make([]string, 0, len(args)+1)
	if scope == "user" {
		cmdArgs = append(cmdArgs, "--user")
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command("systemctl", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if scope == "user" {
			return fmt.Errorf("systemctl --user %s failed: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("systemctl %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

func gatewayLaunchdLabel() string { return "ai.clawgo.gateway" }

func gatewayWindowsTaskName() string { return "ClawGo Gateway" }

func detectLaunchdScopeAndPath() (string, string, error) {
	label := gatewayLaunchdLabel() + ".plist"
	switch gatewayScopePreference() {
	case "system":
		return "system", filepath.Join("/Library/LaunchDaemons", label), nil
	case "user":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", fmt.Errorf("resolve user home failed: %w", err)
		}
		return "user", filepath.Join(home, "Library", "LaunchAgents", label), nil
	}
	if os.Geteuid() == 0 {
		return "system", filepath.Join("/Library/LaunchDaemons", label), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve user home failed: %w", err)
	}
	return "user", filepath.Join(home, "Library", "LaunchAgents", label), nil
}

func detectInstalledLaunchdService() (string, string, error) {
	userScope, userPath, err := detectLaunchdScopeAndPath()
	if err != nil && gatewayScopePreference() == "user" {
		return "", "", err
	}
	systemPath := filepath.Join("/Library/LaunchDaemons", gatewayLaunchdLabel()+".plist")
	systemExists := fileExists(systemPath)
	userExists := fileExists(userPath)

	switch gatewayScopePreference() {
	case "system":
		if systemExists {
			return "system", systemPath, nil
		}
		return "", "", fmt.Errorf("launchd plist not found in system scope: %s", systemPath)
	case "user":
		if userExists {
			return userScope, userPath, nil
		}
		return "", "", fmt.Errorf("launchd plist not found in user scope: %s", userPath)
	}

	if os.Geteuid() == 0 {
		if systemExists {
			return "system", systemPath, nil
		}
		if userExists {
			return userScope, userPath, nil
		}
	} else {
		if userExists {
			return userScope, userPath, nil
		}
		if systemExists {
			return "system", systemPath, nil
		}
	}
	return "", "", fmt.Errorf("gateway service not registered. Run: clawgo gateway")
}

func gatewayInstallLaunchdService() error {
	scope, plistPath, err := detectLaunchdScopeAndPath()
	if err != nil {
		return err
	}
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path failed: %w", err)
	}
	exePath, _ = filepath.Abs(exePath)
	configPath := getConfigPath()
	workDir := filepath.Dir(exePath)
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return fmt.Errorf("create launchd directory failed: %w", err)
	}
	content := buildGatewayLaunchdPlist(exePath, configPath, workDir)
	if err := os.WriteFile(plistPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write launchd plist failed: %w", err)
	}
	_ = runLaunchctl(scope, "bootout", launchdDomainTarget(scope), plistPath)
	if err := runLaunchctl(scope, "bootstrap", launchdDomainTarget(scope), plistPath); err != nil {
		return err
	}
	if err := runLaunchctl(scope, "kickstart", "-k", launchdServiceTarget(scope)); err != nil {
		return err
	}
	fmt.Printf("✓ Gateway service registered: %s (%s)\n", gatewayLaunchdLabel(), scope)
	fmt.Printf("  Launchd plist: %s\n", plistPath)
	fmt.Println("  Start service:   clawgo gateway start")
	fmt.Println("  Restart service: clawgo gateway restart")
	fmt.Println("  Stop service:    clawgo gateway stop")
	return nil
}

func gatewayLaunchdControl(action string) error {
	scope, plistPath, err := detectInstalledLaunchdService()
	if err != nil {
		return err
	}
	switch action {
	case "start":
		_ = runLaunchctl(scope, "bootstrap", launchdDomainTarget(scope), plistPath)
		return runLaunchctl(scope, "kickstart", "-k", launchdServiceTarget(scope))
	case "stop":
		return runLaunchctl(scope, "bootout", launchdDomainTarget(scope), plistPath)
	case "restart":
		_ = runLaunchctl(scope, "bootout", launchdDomainTarget(scope), plistPath)
		if err := runLaunchctl(scope, "bootstrap", launchdDomainTarget(scope), plistPath); err != nil {
			return err
		}
		return runLaunchctl(scope, "kickstart", "-k", launchdServiceTarget(scope))
	case "status":
		return runLaunchctl(scope, "print", launchdServiceTarget(scope))
	default:
		return fmt.Errorf("unsupported action: %s", action)
	}
}

func buildGatewayLaunchdPlist(exePath, configPath, workDir string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>gateway</string>
    <string>run</string>
    <string>--config</string>
    <string>%s</string>
  </array>
  <key>WorkingDirectory</key>
  <string>%s</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, gatewayLaunchdLabel(), exePath, configPath, workDir, filepath.Join(filepath.Dir(configPath), "gateway.launchd.out.log"), filepath.Join(filepath.Dir(configPath), "gateway.launchd.err.log"))
}

func launchdDomainTarget(scope string) string {
	if scope == "system" {
		return "system"
	}
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func launchdServiceTarget(scope string) string {
	return launchdDomainTarget(scope) + "/" + gatewayLaunchdLabel()
}

func runLaunchctl(scope string, args ...string) error {
	cmd := exec.Command("launchctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launchctl %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

func gatewayInstallWindowsTask() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path failed: %w", err)
	}
	exePath, _ = filepath.Abs(exePath)
	configPath := getConfigPath()
	taskName := gatewayWindowsTaskName()
	command := fmt.Sprintf(`"%s" gateway run --config "%s"`, exePath, configPath)
	_ = runSCHTASKS("/Delete", "/TN", taskName, "/F")
	if err := runSCHTASKS("/Create", "/TN", taskName, "/SC", "ONLOGON", "/TR", command, "/F"); err != nil {
		return err
	}
	fmt.Printf("✓ Gateway service registered: %s (windows task)\n", taskName)
	fmt.Println("  Start service:   clawgo gateway start")
	fmt.Println("  Restart service: clawgo gateway restart")
	fmt.Println("  Stop service:    clawgo gateway stop")
	return nil
}

func gatewayWindowsTaskControl(action string) error {
	_, _, err := detectInstalledWindowsTask()
	if err != nil {
		return err
	}
	taskName := gatewayWindowsTaskName()
	switch action {
	case "start":
		return runSCHTASKS("/Run", "/TN", taskName)
	case "stop":
		return stopGatewayProcessByPIDFile()
	case "restart":
		_ = stopGatewayProcessByPIDFile()
		return runSCHTASKS("/Run", "/TN", taskName)
	case "status":
		return runSCHTASKS("/Query", "/TN", taskName, "/V", "/FO", "LIST")
	default:
		return fmt.Errorf("unsupported action: %s", action)
	}
}

func detectInstalledWindowsTask() (string, string, error) {
	taskName := gatewayWindowsTaskName()
	if err := runSCHTASKSQuiet("/Query", "/TN", taskName); err != nil {
		return "", "", fmt.Errorf("gateway service not registered. Run: clawgo gateway")
	}
	return "user", taskName, nil
}

func runSCHTASKS(args ...string) error {
	cmd := exec.Command("schtasks", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("schtasks %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

func runSCHTASKSQuiet(args ...string) error {
	cmd := exec.Command("schtasks", args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func stopGatewayProcessByPIDFile() error {
	pidPath := filepath.Join(filepath.Dir(getConfigPath()), "gateway.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return fmt.Errorf("gateway pid file not found: %w", err)
	}
	pid := strings.TrimSpace(string(data))
	if pid == "" {
		return fmt.Errorf("gateway pid file is empty")
	}
	cmd := exec.Command("taskkill", "/PID", pid, "/T", "/F")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("taskkill /PID %s failed: %w", pid, err)
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
