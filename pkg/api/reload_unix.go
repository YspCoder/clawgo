//go:build !windows

package api

import (
	"os"
	"syscall"
)

func requestSelfReloadSignal() error {
	return syscall.Kill(os.Getpid(), syscall.SIGHUP)
}
