package main

import (
	"context"
	"fmt"
	"os"

	"clawgo/pkg/bus"
	"clawgo/pkg/channels"
)

func channelCmd() {
	if len(os.Args) < 3 {
		channelHelp()
		return
	}

	subcommand := os.Args[2]

	switch subcommand {
	case "test":
		channelTestCmd()
	default:
		fmt.Printf("Unknown channel command: %s\n", subcommand)
		channelHelp()
	}
}

func channelHelp() {
	fmt.Println("\nChannel commands:")
	fmt.Println("  test              Send a test message to a specific channel")
	fmt.Println()
	fmt.Println("Test options:")
	fmt.Println("  --to             Recipient ID")
	fmt.Println("  --channel        Channel name (telegram, discord, etc.)")
	fmt.Println("  -m, --message    Message to send")
}

func channelTestCmd() {
	to := ""
	channelName := ""
	message := "This is a test message from ClawGo 🦞"

	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--to":
			if i+1 < len(args) {
				to = args[i+1]
				i++
			}
		case "--channel":
			if i+1 < len(args) {
				channelName = args[i+1]
				i++
			}
		case "-m", "--message":
			if i+1 < len(args) {
				message = args[i+1]
				i++
			}
		}
	}

	if channelName == "" || to == "" {
		fmt.Println("Error: --channel and --to are required")
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	msgBus := bus.NewMessageBus()
	mgr, err := channels.NewManager(cfg, msgBus)
	if err != nil {
		fmt.Printf("Error creating channel manager: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	if err := mgr.StartAll(ctx); err != nil {
		fmt.Printf("Error starting channels: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Sending test message to %s (%s)...\n", channelName, to)
	if err := mgr.SendToChannel(ctx, channelName, to, message); err != nil {
		fmt.Printf("✗ Failed to send message: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Test message sent successfully!")
}
