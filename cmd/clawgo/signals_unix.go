//go:build !windows

package main

import (
	"os"
	"syscall"
)

func gatewayNotifySignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
}

func isGatewayReloadSignal(sig os.Signal) bool {
	return sig == syscall.SIGHUP
}
