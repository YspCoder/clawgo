package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	cfgpkg "github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/tools"
)

func (s *Server) handleWebUITools(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	toolsList := []map[string]interface{}{}
	if s.onToolsCatalog != nil {
		if items, ok := s.onToolsCatalog().([]map[string]interface{}); ok && items != nil {
			toolsList = items
		}
	}
	mcpItems := make([]map[string]interface{}, 0)
	for _, item := range toolsList {
		if strings.TrimSpace(fmt.Sprint(item["source"])) == "mcp" {
			mcpItems = append(mcpItems, item)
		}
	}
	serverChecks := []map[string]interface{}{}
	if strings.TrimSpace(s.configPath) != "" {
		if cfg, err := cfgpkg.LoadConfig(s.configPath); err == nil {
			serverChecks = buildMCPServerChecks(cfg)
		}
	}
	writeJSON(w, map[string]interface{}{
		"tools":             toolsList,
		"mcp_tools":         mcpItems,
		"mcp_server_checks": serverChecks,
	})
}

func buildMCPServerChecks(cfg *cfgpkg.Config) []map[string]interface{} {
	if cfg == nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Tools.MCP.Servers))
	for name := range cfg.Tools.MCP.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		server := cfg.Tools.MCP.Servers[name]
		transport := strings.ToLower(strings.TrimSpace(server.Transport))
		if transport == "" {
			transport = "stdio"
		}
		command := strings.TrimSpace(server.Command)
		status := "missing_command"
		message := "command is empty"
		resolved := ""
		missingCommand := false
		if !server.Enabled {
			status = "disabled"
			message = "server is disabled"
		} else if transport != "stdio" {
			status = "not_applicable"
			message = "command check not required for non-stdio transport"
		} else if command != "" {
			if filepath.IsAbs(command) {
				if info, err := os.Stat(command); err == nil && !info.IsDir() {
					status = "ok"
					message = "command found"
					resolved = command
				} else {
					status = "missing_command"
					message = fmt.Sprintf("command not found: %s", command)
					missingCommand = true
				}
			} else if path, err := exec.LookPath(command); err == nil {
				status = "ok"
				message = "command found"
				resolved = path
			} else {
				status = "missing_command"
				message = fmt.Sprintf("command not found in PATH: %s", command)
				missingCommand = true
			}
		}
		installSpec := inferMCPInstallSpec(server)
		items = append(items, map[string]interface{}{
			"name":            name,
			"enabled":         server.Enabled,
			"transport":       transport,
			"status":          status,
			"message":         message,
			"command":         command,
			"resolved":        resolved,
			"package":         installSpec.Package,
			"installer":       installSpec.Installer,
			"installable":     missingCommand && installSpec.AutoInstallSupported,
			"missing_command": missingCommand,
		})
	}
	return items
}

type mcpInstallSpec struct {
	Installer            string
	Package              string
	AutoInstallSupported bool
}

func inferMCPInstallSpec(server cfgpkg.MCPServerConfig) mcpInstallSpec {
	if pkgName := strings.TrimSpace(server.Package); pkgName != "" {
		return mcpInstallSpec{Installer: "npm", Package: pkgName, AutoInstallSupported: true}
	}
	command := strings.TrimSpace(server.Command)
	args := make([]string, 0, len(server.Args))
	for _, arg := range server.Args {
		if v := strings.TrimSpace(arg); v != "" {
			args = append(args, v)
		}
	}
	base := filepath.Base(command)
	switch base {
	case "npx":
		return mcpInstallSpec{Installer: "npm", Package: firstNonFlagArg(args), AutoInstallSupported: firstNonFlagArg(args) != ""}
	case "uvx":
		pkgName := firstNonFlagArg(args)
		return mcpInstallSpec{Installer: "uv", Package: pkgName, AutoInstallSupported: pkgName != ""}
	case "bunx":
		pkgName := firstNonFlagArg(args)
		return mcpInstallSpec{Installer: "bun", Package: pkgName, AutoInstallSupported: pkgName != ""}
	case "python", "python3":
		if len(args) >= 2 && args[0] == "-m" {
			return mcpInstallSpec{Installer: "pip", Package: strings.TrimSpace(args[1]), AutoInstallSupported: false}
		}
	}
	return mcpInstallSpec{}
}

func firstNonFlagArg(args []string) string {
	for _, arg := range args {
		item := strings.TrimSpace(arg)
		if item == "" || strings.HasPrefix(item, "-") {
			continue
		}
		return item
	}
	return ""
}

func (s *Server) handleWebUIMCPInstall(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Package   string `json:"package"`
		Installer string `json:"installer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	pkgName := strings.TrimSpace(body.Package)
	if pkgName == "" {
		http.Error(w, "package required", http.StatusBadRequest)
		return
	}
	out, binName, binPath, err := ensureMCPPackageInstalledWithInstaller(r.Context(), pkgName, body.Installer)
	if err != nil {
		msg := err.Error()
		if strings.TrimSpace(out) != "" {
			msg = strings.TrimSpace(out) + "\n" + msg
		}
		http.Error(w, strings.TrimSpace(msg), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":       true,
		"package":  pkgName,
		"output":   out,
		"bin_name": binName,
		"bin_path": binPath,
	})
}

func (s *Server) handleWebUIToolAllowlistGroups(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":     true,
		"groups": tools.ToolAllowlistGroups(),
	})
}
