//go:build windows

package main

import "os"

func gatewayNotifySignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func isGatewayReloadSignal(sig os.Signal) bool {
	return false
}
