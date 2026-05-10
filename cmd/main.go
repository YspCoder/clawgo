// ClawGo - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 ClawGo contributors

package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/YspCoder/clawgo/pkg/logger"
)

var version = "1.2.0"
var buildTime = "unknown"

const logo = ">"
const gatewayServiceName = "clawgo-gateway.service"
const envRootGranted = "CLAWGO_ROOT_GRANTED"

var globalConfigPathOverride string

var errGatewayNotRunning = errors.New("gateway not running")

func main() {
	globalConfigPathOverride = detectConfigPathFromArgs(os.Args)

	for _, arg := range os.Args {
		if arg == "--debug" || arg == "-d" {
			config.SetDebugMode(true)
			logger.SetLevel(logger.DEBUG)
			break
		}
	}

	os.Args = normalizeCLIArgs(os.Args)

	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "onboard":
		onboard()
	case "agent":
		agentCmd()
	case "gateway":
		gatewayCmd()
	case "status":
		statusCmd()
	case "provider":
		providerCmd()
	case "config":
		configCmd()
	case "cron":
		cronCmd()
	case "channel":
		channelCmd()
	case "skills":
		skillsCmd()
	case "backup":
		backupCmd()
	case "tui":
		tuiCmd()
	case "version", "--version", "-v":
		fmt.Printf("%s clawgo v%s\n", logo, version)
	case "uninstall":
		uninstallCmd()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printHelp()
		os.Exit(1)
	}
}
