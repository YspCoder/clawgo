// ClawGo - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 ClawGo contributors

package main

import (
	"embed"
	"errors"
	"fmt"
	"os"

	"clawgo/pkg/config"
	"clawgo/pkg/logger"
)

//go:embed workspace
var embeddedFiles embed.FS

const version = "0.1.0"
const logo = "🦞"
const gatewayServiceName = "clawgo-gateway.service"
const envRootPrompted = "CLAWGO_ROOT_PROMPTED"
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
		if shouldPromptGatewayRoot(os.Args) {
			maybePromptAndEscalateRoot("gateway")
		}
		gatewayCmd()
	case "status":
		statusCmd()
	case "config":
		configCmd()
	case "cron":
		cronCmd()
	case "login":
		loginCmd()
	case "channel":
		channelCmd()
	case "skills":
		skillsCmd()
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
