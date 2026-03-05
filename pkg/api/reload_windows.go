//go:build windows

package api

// requestSelfReloadSignal is a no-op on Windows (no SIGHUP semantics).
func requestSelfReloadSignal() error {
	return nil
}
