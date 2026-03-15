package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

func resolveClawHubBinary(ctx context.Context) string {
	if p, err := exec.LookPath("clawhub"); err == nil {
		return p
	}
	prefix := strings.TrimSpace(npmGlobalPrefix(ctx))
	if prefix != "" {
		cand := filepath.Join(prefix, "bin", "clawhub")
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	cands := []string{
		"/usr/local/bin/clawhub",
		"/opt/homebrew/bin/clawhub",
		filepath.Join(os.Getenv("HOME"), ".npm-global", "bin", "clawhub"),
	}
	for _, cand := range cands {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	return ""
}

func npmGlobalPrefix(ctx context.Context) string {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "npm", "config", "get", "prefix").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func runInstallCommand(ctx context.Context, cmdline string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, "sh", "-c", cmdline)
	out, err := cmd.CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if err != nil {
		if msg == "" {
			msg = err.Error()
		}
		return msg, fmt.Errorf("%s", msg)
	}
	return msg, nil
}

func ensureNodeRuntime(ctx context.Context) (string, error) {
	if nodePath, err := exec.LookPath("node"); err == nil {
		if _, err := exec.LookPath("npm"); err == nil {
			if major, verr := detectNodeMajor(ctx, nodePath); verr == nil && major == 22 {
				return "node@22 and npm already installed", nil
			}
		}
	}

	var output []string
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("brew"); err != nil {
			return strings.Join(output, "\n"), fmt.Errorf("nodejs/npm missing and Homebrew not found; please install Homebrew then retry")
		}
		out, err := runInstallCommand(ctx, "brew install node@22 && brew link --overwrite --force node@22")
		if out != "" {
			output = append(output, out)
		}
		if err != nil {
			return strings.Join(output, "\n"), err
		}
	case "linux":
		var out string
		var err error
		switch {
		case commandExists("apt-get"):
			if commandExists("curl") {
				out, err = runInstallCommand(ctx, "curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && apt-get install -y nodejs")
			} else if commandExists("wget") {
				out, err = runInstallCommand(ctx, "wget -qO- https://deb.nodesource.com/setup_22.x | bash - && apt-get install -y nodejs")
			} else {
				err = fmt.Errorf("missing curl/wget required for NodeSource setup_22.x")
			}
		case commandExists("dnf"):
			if commandExists("curl") {
				out, err = runInstallCommand(ctx, "curl -fsSL https://rpm.nodesource.com/setup_22.x | bash - && dnf install -y nodejs")
			} else if commandExists("wget") {
				out, err = runInstallCommand(ctx, "wget -qO- https://rpm.nodesource.com/setup_22.x | bash - && dnf install -y nodejs")
			} else {
				err = fmt.Errorf("missing curl/wget required for NodeSource setup_22.x")
			}
		case commandExists("yum"):
			if commandExists("curl") {
				out, err = runInstallCommand(ctx, "curl -fsSL https://rpm.nodesource.com/setup_22.x | bash - && yum install -y nodejs")
			} else if commandExists("wget") {
				out, err = runInstallCommand(ctx, "wget -qO- https://rpm.nodesource.com/setup_22.x | bash - && yum install -y nodejs")
			} else {
				err = fmt.Errorf("missing curl/wget required for NodeSource setup_22.x")
			}
		case commandExists("pacman"):
			out, err = runInstallCommand(ctx, "pacman -Sy --noconfirm nodejs npm")
		case commandExists("apk"):
			out, err = runInstallCommand(ctx, "apk add --no-cache nodejs npm")
		default:
			return strings.Join(output, "\n"), fmt.Errorf("nodejs/npm missing and no supported package manager found")
		}
		if out != "" {
			output = append(output, out)
		}
		if err != nil {
			return strings.Join(output, "\n"), err
		}
	default:
		return strings.Join(output, "\n"), fmt.Errorf("unsupported OS for auto install: %s", runtime.GOOS)
	}

	if _, err := exec.LookPath("node"); err != nil {
		return strings.Join(output, "\n"), fmt.Errorf("node installation completed but `node` still not found in PATH")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		return strings.Join(output, "\n"), fmt.Errorf("node installation completed but `npm` still not found in PATH")
	}
	nodePath, _ := exec.LookPath("node")
	major, err := detectNodeMajor(ctx, nodePath)
	if err != nil {
		return strings.Join(output, "\n"), fmt.Errorf("failed to detect node major version: %w", err)
	}
	if major != 22 {
		return strings.Join(output, "\n"), fmt.Errorf("node version is %d, expected 22", major)
	}
	output = append(output, "node@22/npm installed")
	return strings.Join(output, "\n"), nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func detectNodeMajor(ctx context.Context, nodePath string) (int, error) {
	nodePath = strings.TrimSpace(nodePath)
	if nodePath == "" {
		return 0, fmt.Errorf("node path empty")
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, nodePath, "-p", "process.versions.node.split('.')[0]").Output()
	if err != nil {
		return 0, err
	}
	majorStr := strings.TrimSpace(string(out))
	if majorStr == "" {
		return 0, fmt.Errorf("empty node major version")
	}
	v, err := strconv.Atoi(majorStr)
	if err != nil {
		return 0, err
	}
	return v, nil
}

func ensureMCPPackageInstalled(ctx context.Context, pkgName string) (output string, binName string, binPath string, err error) {
	return ensureMCPPackageInstalledWithInstaller(ctx, pkgName, "npm")
}

func ensureMCPPackageInstalledWithInstaller(ctx context.Context, pkgName, installer string) (output string, binName string, binPath string, err error) {
	pkgName = strings.TrimSpace(pkgName)
	if pkgName == "" {
		return "", "", "", fmt.Errorf("package empty")
	}
	installer = strings.ToLower(strings.TrimSpace(installer))
	if installer == "" {
		installer = "npm"
	}
	outs := make([]string, 0, 4)
	switch installer {
	case "npm":
		nodeOut, err := ensureNodeRuntime(ctx)
		if nodeOut != "" {
			outs = append(outs, nodeOut)
		}
		if err != nil {
			return strings.Join(outs, "\n"), "", "", err
		}
		installOut, err := runInstallCommand(ctx, "npm i -g "+shellEscapeArg(pkgName))
		if installOut != "" {
			outs = append(outs, installOut)
		}
		if err != nil {
			return strings.Join(outs, "\n"), "", "", err
		}
		binName, err = resolveNpmPackageBin(ctx, pkgName)
		if err != nil {
			return strings.Join(outs, "\n"), "", "", err
		}
	case "uv":
		if !commandExists("uv") {
			return "", "", "", fmt.Errorf("uv is not installed; install uv first to auto-install %s", pkgName)
		}
		installOut, err := runInstallCommand(ctx, "uv tool install "+shellEscapeArg(pkgName))
		if installOut != "" {
			outs = append(outs, installOut)
		}
		if err != nil {
			return strings.Join(outs, "\n"), "", "", err
		}
		binName = guessSimpleCommandName(pkgName)
	case "bun":
		if !commandExists("bun") {
			return "", "", "", fmt.Errorf("bun is not installed; install bun first to auto-install %s", pkgName)
		}
		installOut, err := runInstallCommand(ctx, "bun add -g "+shellEscapeArg(pkgName))
		if installOut != "" {
			outs = append(outs, installOut)
		}
		if err != nil {
			return strings.Join(outs, "\n"), "", "", err
		}
		binName = guessSimpleCommandName(pkgName)
	default:
		return "", "", "", fmt.Errorf("unsupported installer: %s", installer)
	}
	binPath = resolveInstalledBinary(ctx, binName)
	if strings.TrimSpace(binPath) == "" {
		return strings.Join(outs, "\n"), binName, "", fmt.Errorf("installed %s but binary %q not found in PATH", pkgName, binName)
	}
	outs = append(outs, fmt.Sprintf("installed %s via %s", pkgName, installer))
	outs = append(outs, fmt.Sprintf("resolved binary: %s", binPath))
	return strings.Join(outs, "\n"), binName, binPath, nil
}

func guessSimpleCommandName(pkgName string) string {
	pkgName = strings.TrimSpace(pkgName)
	pkgName = strings.TrimPrefix(pkgName, "@")
	if idx := strings.LastIndex(pkgName, "/"); idx >= 0 {
		pkgName = pkgName[idx+1:]
	}
	return strings.TrimSpace(pkgName)
}

func resolveNpmPackageBin(ctx context.Context, pkgName string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "npm", "view", pkgName, "bin", "--json")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to query npm bin for %s: %w", pkgName, err)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "null" {
		return "", fmt.Errorf("npm package %s does not expose a bin", pkgName)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(out, &obj); err == nil && len(obj) > 0 {
		keys := make([]string, 0, len(obj))
		for key := range obj {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return keys[0], nil
	}
	var text string
	if err := json.Unmarshal(out, &text); err == nil && strings.TrimSpace(text) != "" {
		return strings.TrimSpace(text), nil
	}
	return "", fmt.Errorf("unable to resolve bin for npm package %s", pkgName)
}

func resolveInstalledBinary(ctx context.Context, binName string) string {
	binName = strings.TrimSpace(binName)
	if binName == "" {
		return ""
	}
	if p, err := exec.LookPath(binName); err == nil {
		return p
	}
	prefix := strings.TrimSpace(npmGlobalPrefix(ctx))
	if prefix != "" {
		cand := filepath.Join(prefix, "bin", binName)
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	cands := []string{
		filepath.Join("/usr/local/bin", binName),
		filepath.Join("/opt/homebrew/bin", binName),
		filepath.Join(os.Getenv("HOME"), ".npm-global", "bin", binName),
	}
	for _, cand := range cands {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	return ""
}

func shellEscapeArg(in string) string {
	if strings.TrimSpace(in) == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(in, "'", `'\''`) + "'"
}

func anyToString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		if v == nil {
			return ""
		}
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func derefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}
