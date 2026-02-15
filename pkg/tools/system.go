package tools

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
)

type SystemInfoTool struct{}

func NewSystemInfoTool() *SystemInfoTool {
	return &SystemInfoTool{}
}

func (t *SystemInfoTool) Name() string {
	return "system_info"
}

func (t *SystemInfoTool) Description() string {
	return "Get current system status (CPU, RAM, Disk, Uptime)."
}

func (t *SystemInfoTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *SystemInfoTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	var sb strings.Builder
	sb.WriteString("System Info:\n")
	sb.WriteString(fmt.Sprintf("OS: %s %s\n", runtime.GOOS, runtime.GOARCH))

	// Uptime
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		var up, idle float64
		fmt.Sscanf(string(data), "%f %f", &up, &idle)
		days := int(up / 86400)
		hours := int(up) % 86400 / 3600
		mins := int(up) % 3600 / 60
		sb.WriteString(fmt.Sprintf("Uptime: %dd %dh %dm\n", days, hours, mins))
	}

	// Load Average
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		sb.WriteString(fmt.Sprintf("Load Avg: %s", string(data)))
	}

	// CPU Model
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "model name") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) > 1 {
					sb.WriteString(fmt.Sprintf("CPU: %s\n", strings.TrimSpace(parts[1])))
					break
				}
			}
		}
	}

	// RAM from /proc/meminfo
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		lines := strings.Split(string(data), "\n")
		var total, free, available uint64
		for _, line := range lines {
			if strings.HasPrefix(line, "MemTotal:") {
				fmt.Sscanf(line, "MemTotal: %d kB", &total)
			} else if strings.HasPrefix(line, "MemFree:") {
				fmt.Sscanf(line, "MemFree: %d kB", &free)
			} else if strings.HasPrefix(line, "MemAvailable:") {
				fmt.Sscanf(line, "MemAvailable: %d kB", &available)
			}
		}
		if total > 0 {
			if available == 0 {
				available = free
			}
			used := total - available
			sb.WriteString(fmt.Sprintf("RAM: Used %.2f GB / Total %.2f GB (%.2f%%)\n",
				float64(used)/1024/1024, float64(total)/1024/1024, float64(used)/float64(total)*100))
		}
	}

	// Disk usage for /
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		bsize := uint64(stat.Bsize)
		total := stat.Blocks * bsize
		free := stat.Bfree * bsize
		used := total - free
		sb.WriteString(fmt.Sprintf("Disk (/): Used %.2f GB / Total %.2f GB (%.2f%%)\n",
			float64(used)/1024/1024/1024, float64(total)/1024/1024/1024, float64(used)/float64(total)*100))
	}

	return sb.String(), nil
}
