package configops

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"clawgo/pkg/config"
)

func LoadConfigAsMap(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			defaultCfg := config.DefaultConfig()
			defData, mErr := json.Marshal(defaultCfg)
			if mErr != nil {
				return nil, mErr
			}
			var cfgMap map[string]interface{}
			if uErr := json.Unmarshal(defData, &cfgMap); uErr != nil {
				return nil, uErr
			}
			return cfgMap, nil
		}
		return nil, err
	}

	var cfgMap map[string]interface{}
	if err := json.Unmarshal(data, &cfgMap); err != nil {
		return nil, err
	}
	return cfgMap, nil
}

func NormalizeConfigPath(path string) string {
	p := strings.TrimSpace(path)
	p = strings.Trim(p, ".")
	parts := strings.Split(p, ".")
	for i, part := range parts {
		if part == "enable" {
			parts[i] = "enabled"
		}
	}
	return strings.Join(parts, ".")
}

func ParseConfigValue(raw string) interface{} {
	v := strings.TrimSpace(raw)
	lv := strings.ToLower(v)
	if lv == "true" {
		return true
	}
	if lv == "false" {
		return false
	}
	if lv == "null" {
		return nil
	}
	if i, err := strconv.ParseInt(v, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil && strings.Contains(v, ".") {
		return f
	}
	if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
		return v[1 : len(v)-1]
	}
	return v
}

func SetMapValueByPath(root map[string]interface{}, path string, value interface{}) error {
	if path == "" {
		return fmt.Errorf("path is empty")
	}
	parts := strings.Split(path, ".")
	cur := root
	for i := 0; i < len(parts)-1; i++ {
		key := parts[i]
		if key == "" {
			return fmt.Errorf("invalid path: %s", path)
		}
		next, ok := cur[key]
		if !ok {
			child := map[string]interface{}{}
			cur[key] = child
			cur = child
			continue
		}
		child, ok := next.(map[string]interface{})
		if !ok {
			return fmt.Errorf("path segment is not object: %s", key)
		}
		cur = child
	}
	last := parts[len(parts)-1]
	if last == "" {
		return fmt.Errorf("invalid path: %s", path)
	}
	cur[last] = value
	return nil
}

func GetMapValueByPath(root map[string]interface{}, path string) (interface{}, bool) {
	if path == "" {
		return nil, false
	}
	parts := strings.Split(path, ".")
	var cur interface{} = root
	for _, key := range parts {
		obj, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		next, ok := obj[key]
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

func WriteConfigAtomicWithBackup(configPath string, data []byte) (string, error) {
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return "", err
	}

	backupPath := configPath + ".bak"
	if oldData, err := os.ReadFile(configPath); err == nil {
		if err := os.WriteFile(backupPath, oldData, 0644); err != nil {
			return "", fmt.Errorf("write backup failed: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read existing config failed: %w", err)
	}

	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return "", fmt.Errorf("write temp config failed: %w", err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("atomic replace config failed: %w", err)
	}
	return backupPath, nil
}

func RollbackConfigFromBackup(configPath, backupPath string) error {
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("read backup failed: %w", err)
	}
	tmpPath := configPath + ".rollback.tmp"
	if err := os.WriteFile(tmpPath, backupData, 0644); err != nil {
		return fmt.Errorf("write rollback temp failed: %w", err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rollback replace failed: %w", err)
	}
	return nil
}

func TriggerGatewayReload(configPath string, notRunningErr error) (bool, error) {
	pidPath := filepath.Join(filepath.Dir(configPath), "gateway.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return false, fmt.Errorf("%w (pid file not found: %s)", notRunningErr, pidPath)
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return true, fmt.Errorf("invalid gateway pid: %q", pidStr)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return true, fmt.Errorf("find process failed: %w", err)
	}
	if err := proc.Signal(syscall.SIGHUP); err != nil {
		return true, fmt.Errorf("send SIGHUP failed: %w", err)
	}
	return true, nil
}
