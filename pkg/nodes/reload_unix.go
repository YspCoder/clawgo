//go:build !windows

package nodes

import (
	"os"
	"syscall"
)

func requestSelfReloadSignal() error {
	return syscall.Kill(os.Getpid(), syscall.SIGHUP)
}
