package api

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/YspCoder/clawgo/pkg/tools"
)

func resolveRelativeFilePath(root, raw string) (string, string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", "", fmt.Errorf("workspace not configured")
	}
	clean := filepath.Clean(strings.TrimSpace(raw))
	if clean == "." || clean == "" || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", "", fmt.Errorf("invalid path")
	}
	full := filepath.Join(root, clean)
	cleanRoot := filepath.Clean(root)
	if full != cleanRoot {
		prefix := cleanRoot + string(os.PathSeparator)
		if !strings.HasPrefix(filepath.Clean(full), prefix) {
			return "", "", fmt.Errorf("invalid path")
		}
	}
	return clean, full, nil
}

func relativeFilePathStatus(err error) int {
	if err == nil {
		return 200
	}
	switch {
	case err.Error() == "workspace not configured":
		return 500
	default:
		return 400
	}
}

func readRelativeTextFile(root, raw string) (string, string, bool, error) {
	clean, full, err := resolveRelativeFilePath(root, raw)
	if err != nil {
		return "", "", false, err
	}
	b, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return clean, "", false, nil
		}
		return clean, "", false, err
	}
	return clean, string(b), true, nil
}

func writeRelativeTextFile(root, raw string, content string, ensureDir bool) (string, error) {
	clean, full, err := resolveRelativeFilePath(root, raw)
	if err != nil {
		return "", err
	}
	if ensureDir {
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		return "", err
	}
	return clean, nil
}

func (s *Server) memoryFilePath(name string) string {
	workspace := strings.TrimSpace(s.workspacePath)
	if workspace == "" {
		return ""
	}
	return filepath.Join(workspace, "memory", strings.TrimSpace(name))
}

func buildAgentTreeRoot(nodeID string, items []map[string]interface{}) map[string]interface{} {
	rootID := "main"
	nodesByID := make(map[string]map[string]interface{}, len(items)+1)
	for _, item := range items {
		id := strings.TrimSpace(stringFromMap(item, "agent_id"))
		if id == "" {
			continue
		}
		nodesByID[id] = map[string]interface{}{
			"agent_id":        id,
			"display_name":    stringFromMap(item, "display_name"),
			"role":            stringFromMap(item, "role"),
			"type":            stringFromMap(item, "type"),
			"transport":       fallbackString(stringFromMap(item, "transport"), "local"),
			"managed_by":      stringFromMap(item, "managed_by"),
			"node_id":         stringFromMap(item, "node_id"),
			"parent_agent_id": stringFromMap(item, "parent_agent_id"),
			"enabled":         boolFromMap(item, "enabled"),
			"children":        []map[string]interface{}{},
		}
	}
	root, ok := nodesByID[rootID]
	if !ok {
		root = map[string]interface{}{
			"agent_id":     rootID,
			"display_name": "Main Agent",
			"role":         "orchestrator",
			"type":         "agent",
			"transport":    "local",
			"managed_by":   "derived",
			"enabled":      true,
			"children":     []map[string]interface{}{},
		}
		nodesByID[rootID] = root
	}
	for _, item := range items {
		id := strings.TrimSpace(stringFromMap(item, "agent_id"))
		if id == "" || id == rootID {
			continue
		}
		parentID := strings.TrimSpace(stringFromMap(item, "parent_agent_id"))
		if parentID == "" {
			parentID = rootID
		}
		parent, ok := nodesByID[parentID]
		if !ok {
			parent = root
		}
		children, _ := parent["children"].([]map[string]interface{})
		parent["children"] = append(children, nodesByID[id])
	}
	return map[string]interface{}{
		"node_id":   nodeID,
		"agent_id":  root["agent_id"],
		"root":      root,
		"child_cnt": len(root["children"].([]map[string]interface{})),
	}
}

func stringFromMap(item map[string]interface{}, key string) string {
	return tools.MapStringArg(item, key)
}

func boolFromMap(item map[string]interface{}, key string) bool {
	if item == nil {
		return false
	}
	v, _ := tools.MapBoolArg(item, key)
	return v
}

func rawStringFromMap(item map[string]interface{}, key string) string {
	return tools.MapRawStringArg(item, key)
}

func stringListFromMap(item map[string]interface{}, key string) []string {
	return tools.MapStringListArg(item, key)
}

func intFromMap(item map[string]interface{}, key string, fallback int) int {
	return tools.MapIntArg(item, key, fallback)
}

func fallbackString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func detectLocalIP() string {
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, a := range addrs {
				var ip net.IP
				switch v := a.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if ip == nil || ip.IsLoopback() {
					continue
				}
				ip = ip.To4()
				if ip == nil {
					continue
				}
				return ip.String()
			}
		}
	}
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		if ua, ok := conn.LocalAddr().(*net.UDPAddr); ok && ua.IP != nil {
			if ip := ua.IP.To4(); ip != nil {
				return ip.String()
			}
		}
	}
	return ""
}

func normalizeCronJob(v interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]interface{}{"raw": fmt.Sprintf("%v", v)}
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]interface{}{"raw": string(b)}
	}
	out := map[string]interface{}{}
	for k, val := range m {
		out[k] = val
	}
	if sch, ok := m["schedule"].(map[string]interface{}); ok {
		kind := stringFromMap(sch, "kind")
		if expr := stringFromMap(sch, "expr"); expr != "" {
			out["expr"] = expr
		} else if strings.EqualFold(strings.TrimSpace(kind), "every") {
			if every := intFromMap(sch, "everyMs", 0); every > 0 {
				out["expr"] = fmt.Sprintf("@every %s", (time.Duration(every) * time.Millisecond).String())
			}
		} else if strings.EqualFold(strings.TrimSpace(kind), "at") {
			if at := intFromMap(sch, "atMs", 0); at > 0 {
				out["expr"] = time.UnixMilli(int64(at)).Format(time.RFC3339)
			}
		}
	}
	if payload, ok := m["payload"].(map[string]interface{}); ok {
		if msg, ok := payload["message"]; ok {
			out["message"] = msg
		}
		if d, ok := payload["deliver"]; ok {
			out["deliver"] = d
		}
		if c, ok := payload["channel"]; ok {
			out["channel"] = c
		}
		if to, ok := payload["to"]; ok {
			out["to"] = to
		}
	}
	return out
}

func normalizeCronJobs(v interface{}) []map[string]interface{} {
	b, err := json.Marshal(v)
	if err != nil {
		return []map[string]interface{}{}
	}
	var arr []interface{}
	if err := json.Unmarshal(b, &arr); err != nil {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, len(arr))
	for _, it := range arr {
		out = append(out, normalizeCronJob(it))
	}
	return out
}
