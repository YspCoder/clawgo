//go:build !windows

package main

import (
	"os"
	"syscall"
)

func requestGatewayReloadSignal() error {
	return syscall.Kill(os.Getpid(), syscall.SIGHUP)
}
