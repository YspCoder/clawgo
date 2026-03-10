package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/channels"
	"github.com/YspCoder/clawgo/pkg/config"

	qrterminal "github.com/mdp/qrterminal/v3"
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
	case "whatsapp":
		whatsAppChannelCmd()
	default:
		fmt.Printf("Unknown channel command: %s\n", subcommand)
		channelHelp()
	}
}

func channelHelp() {
	fmt.Println("\nChannel commands:")
	fmt.Println("  test              Send a test message to a specific channel")
	fmt.Println("  whatsapp          Run and inspect the built-in WhatsApp bridge")
	fmt.Println()
	fmt.Println("Test options:")
	fmt.Println("  --to             Recipient ID")
	fmt.Println("  --channel        Channel name (telegram, discord, etc.)")
	fmt.Println("  -m, --message    Message to send")
	fmt.Println()
	fmt.Println("WhatsApp bridge:")
	fmt.Println("  clawgo channel whatsapp bridge run")
	fmt.Println("  clawgo channel whatsapp bridge status")
	fmt.Println("  clawgo channel whatsapp bridge logout")
}

func channelTestCmd() {
	to := ""
	channelName := ""
	message := "This is a test message from ClawGo 馃"

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
		fmt.Printf("鉁?Failed to send message: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("鉁?Test message sent successfully!")
}

func whatsAppChannelCmd() {
	if len(os.Args) < 4 {
		whatsAppChannelHelp()
		return
	}
	if os.Args[3] != "bridge" {
		fmt.Printf("Unknown WhatsApp channel command: %s\n", os.Args[3])
		whatsAppChannelHelp()
		return
	}
	if len(os.Args) < 5 {
		whatsAppBridgeHelp()
		return
	}
	switch os.Args[4] {
	case "run":
		whatsAppBridgeRunCmd()
	case "status":
		whatsAppBridgeStatusCmd()
	case "logout":
		whatsAppBridgeLogoutCmd()
	default:
		fmt.Printf("Unknown WhatsApp bridge command: %s\n", os.Args[4])
		whatsAppBridgeHelp()
	}
}

func whatsAppChannelHelp() {
	fmt.Println("\nWhatsApp channel commands:")
	fmt.Println("  clawgo channel whatsapp bridge run")
	fmt.Println("  clawgo channel whatsapp bridge status")
	fmt.Println("  clawgo channel whatsapp bridge logout")
}

func whatsAppBridgeHelp() {
	fmt.Println("\nWhatsApp bridge commands:")
	fmt.Println("  run               Run the built-in local WhatsApp bridge with QR login")
	fmt.Println("  status            Show current WhatsApp bridge status")
	fmt.Println("  logout            Unlink the current WhatsApp companion session")
	fmt.Println()
	fmt.Println("Run options:")
	fmt.Println("  --addr <host:port>        Override listen address (defaults to channels.whatsapp.bridge_url host)")
	fmt.Println("  --state-dir <path>        Override session store directory")
	fmt.Println("  --no-print-qr            Disable terminal QR rendering")
}

func whatsAppBridgeRunCmd() {
	cfg, _ := loadConfig()
	bridgeURL := "ws://127.0.0.1:3001"
	if cfg != nil && strings.TrimSpace(cfg.Channels.WhatsApp.BridgeURL) != "" {
		bridgeURL = cfg.Channels.WhatsApp.BridgeURL
	}
	addr, err := channels.ParseWhatsAppBridgeListenAddr(bridgeURL)
	if err != nil {
		fmt.Printf("Error parsing WhatsApp bridge url: %v\n", err)
		os.Exit(1)
	}
	stateDir := filepath.Join(config.GetConfigDir(), "channels", "whatsapp")
	printQR := true
	args := os.Args[5:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--addr":
			if i+1 < len(args) {
				addr = strings.TrimSpace(args[i+1])
				i++
			}
		case "--state-dir":
			if i+1 < len(args) {
				stateDir = strings.TrimSpace(args[i+1])
				i++
			}
		case "--no-print-qr":
			printQR = false
		}
	}

	fmt.Printf("Starting WhatsApp bridge on %s\n", addr)
	fmt.Printf("Session store: %s\n", stateDir)
	statusURL, _ := channels.BridgeStatusURL(addr)
	fmt.Printf("Status endpoint: %s\n", statusURL)
	if printQR {
		fmt.Println("QR codes will be rendered below when login is required.")
	}

	svc := channels.NewWhatsAppBridgeService(addr, stateDir, printQR)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go renderWhatsAppBridgeQR(ctx, addr, printQR)
	go renderWhatsAppBridgeState(ctx, addr)
	if err := svc.Start(ctx); err != nil {
		fmt.Printf("WhatsApp bridge stopped with error: %v\n", err)
		os.Exit(1)
	}
}

func whatsAppBridgeStatusCmd() {
	cfg, _ := loadConfig()
	bridgeURL := "ws://127.0.0.1:3001"
	if cfg != nil && strings.TrimSpace(cfg.Channels.WhatsApp.BridgeURL) != "" {
		bridgeURL = cfg.Channels.WhatsApp.BridgeURL
	}
	args := os.Args[5:]
	if len(args) >= 2 && args[0] == "--url" {
		bridgeURL = strings.TrimSpace(args[1])
	}
	statusURL, err := channels.BridgeStatusURL(bridgeURL)
	if err != nil {
		fmt.Printf("Error building status url: %v\n", err)
		os.Exit(1)
	}
	status, err := fetchWhatsAppBridgeStatus(statusURL)
	if err != nil {
		fmt.Printf("Error fetching WhatsApp bridge status: %v\n", err)
		os.Exit(1)
	}
	data, _ := json.MarshalIndent(status, "", "  ")
	fmt.Println(string(data))
	if status.QRAvailable && strings.TrimSpace(status.QRCode) != "" {
		fmt.Println()
		fmt.Println("Scan this QR code with WhatsApp:")
		qrterminal.GenerateHalfBlock(status.QRCode, qrterminal.L, os.Stdout)
	}
}

func whatsAppBridgeLogoutCmd() {
	cfg, _ := loadConfig()
	bridgeURL := "ws://127.0.0.1:3001"
	if cfg != nil && strings.TrimSpace(cfg.Channels.WhatsApp.BridgeURL) != "" {
		bridgeURL = cfg.Channels.WhatsApp.BridgeURL
	}
	args := os.Args[5:]
	if len(args) >= 2 && args[0] == "--url" {
		bridgeURL = strings.TrimSpace(args[1])
	}
	logoutURL, err := channels.BridgeLogoutURL(bridgeURL)
	if err != nil {
		fmt.Printf("Error building logout url: %v\n", err)
		os.Exit(1)
	}
	req, _ := http.NewRequest(http.MethodPost, logoutURL, nil)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		fmt.Printf("Error calling WhatsApp bridge logout: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		fmt.Printf("WhatsApp bridge logout failed: %s\n", resp.Status)
		os.Exit(1)
	}
	fmt.Println("WhatsApp bridge logout requested successfully.")
}

func fetchWhatsAppBridgeStatus(statusURL string) (channels.WhatsAppBridgeStatus, error) {
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Get(statusURL)
	if err != nil {
		return channels.WhatsAppBridgeStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return channels.WhatsAppBridgeStatus{}, fmt.Errorf("status request failed: %s", resp.Status)
	}
	var status channels.WhatsAppBridgeStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return channels.WhatsAppBridgeStatus{}, err
	}
	return status, nil
}

func renderWhatsAppBridgeQR(ctx context.Context, bridgeURL string, enabled bool) {
	if !enabled {
		return
	}
	statusURL, err := channels.BridgeStatusURL(bridgeURL)
	if err != nil {
		return
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	lastQR := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			status, err := fetchWhatsAppBridgeStatus(statusURL)
			if err != nil {
				continue
			}
			if !status.QRAvailable || strings.TrimSpace(status.QRCode) == "" || status.QRCode == lastQR {
				continue
			}
			lastQR = status.QRCode
			fmt.Println()
			fmt.Println("Scan this QR code with WhatsApp:")
			qrterminal.GenerateHalfBlock(status.QRCode, qrterminal.L, os.Stdout)
			fmt.Println()
		}
	}
}

func renderWhatsAppBridgeState(ctx context.Context, bridgeURL string) {
	statusURL, err := channels.BridgeStatusURL(bridgeURL)
	if err != nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastSig string
	var lastPrintedUser string
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			status, err := fetchWhatsAppBridgeStatus(statusURL)
			if err != nil {
				continue
			}
			sig := fmt.Sprintf("%s|%t|%t|%s|%s", status.State, status.Connected, status.LoggedIn, status.UserJID, status.LastEvent)
			if sig == lastSig {
				continue
			}
			lastSig = sig

			switch {
			case status.QRAvailable:
				fmt.Println("Waiting for WhatsApp QR scan...")
			case status.State == "paired":
				fmt.Println("WhatsApp QR scanned. Finalizing companion link...")
			case status.Connected && status.LoggedIn:
				if status.UserJID != "" && status.UserJID != lastPrintedUser {
					fmt.Printf("WhatsApp connected as %s\n", status.UserJID)
					lastPrintedUser = status.UserJID
				} else {
					fmt.Println("WhatsApp bridge connected.")
				}
				fmt.Println("Bridge is ready. Start or keep `make dev` running to receive messages.")
			case status.State == "stored_session":
				fmt.Println("Existing WhatsApp session found. Reconnecting...")
			case status.State == "disconnected":
				fmt.Println("WhatsApp bridge disconnected. Waiting for reconnect...")
			case status.State == "logged_out":
				fmt.Println("WhatsApp session logged out. Restart bridge to scan a new QR code.")
			case status.LastError != "":
				fmt.Printf("WhatsApp bridge status: %s (%s)\n", status.State, status.LastError)
			case status.State != "":
				fmt.Printf("WhatsApp bridge status: %s\n", status.State)
			}
		}
	}
}
