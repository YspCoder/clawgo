//go:build !with_tui

package main

import "fmt"

var tuiEnabled = false

func tuiCmd() {
	fmt.Println("TUI is not included in this build.")
	fmt.Println("Install the no-channel variant to use `clawgo tui`.")
}
