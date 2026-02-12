package tools

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
)

type SystemInfoTool struct {}

func NewSystemInfoTool() *SystemInfoTool {
	return &SystemInfoTool{}
}

func (t *SystemInfoTool) Name() string {
	return "system_info"
}

func (t *SystemInfoTool) Description() string {
	return "Get current system status (CPU, RAM, Disk)."
}

func (t *SystemInfoTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{},
	}
}

func (t *SystemInfoTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	info := fmt.Sprintf("System Info:\n")
	info += fmt.Sprintf("OS: %s %s\n", runtime.GOOS, runtime.GOARCH)

	// Load Average
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		info += fmt.Sprintf("Load Avg: %s", string(data))
	} else {
		info += "Load Avg: N/A\n"
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
			// fallback if Available not present (older kernels)
			if available == 0 {
				available = free // very rough approximation
			}
			used := total - available
			info += fmt.Sprintf("RAM: Used %.2f GB / Total %.2f GB (%.2f%%)\n", 
				float64(used)/1024/1024, float64(total)/1024/1024, float64(used)/float64(total)*100)
		}
	} else {
		info += "RAM: N/A\n"
	}

	// Disk usage for /
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		// Cast to uint64 to avoid overflow/type mismatch
		bsize := uint64(stat.Bsize)
		total := stat.Blocks * bsize
		free := stat.Bfree * bsize
		used := total - free
		info += fmt.Sprintf("Disk (/): Used %.2f GB / Total %.2f GB (%.2f%%)\n",
			float64(used)/1024/1024/1024, float64(total)/1024/1024/1024, float64(used)/float64(total)*100)
	} else {
		info += "Disk: N/A\n"
	}

	return info, nil
}
