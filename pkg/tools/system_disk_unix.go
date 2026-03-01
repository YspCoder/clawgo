//go:build !windows

package tools

import "fmt"
import "syscall"

func diskUsageRoot() string {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return ""
	}
	bsize := uint64(stat.Bsize)
	total := stat.Blocks * bsize
	free := stat.Bfree * bsize
	used := total - free
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("Disk (/): Used %.2f GB / Total %.2f GB (%.2f%%)\n",
		float64(used)/1024/1024/1024, float64(total)/1024/1024/1024, float64(used)/float64(total)*100)
}
