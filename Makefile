.PHONY: all build build-variants build-linux-slim build-all build-all-variants build-webui package-all install install-win uninstall clean help test test-docker install-bootstrap-docs sync-embed-workspace sync-embed-workspace-base sync-embed-webui cleanup-embed-workspace test-only clean-test-artifacts dev

# Build variables
BINARY_NAME=clawgo
BUILD_DIR=build
CMD_DIR=cmd
MAIN_GO=$(CMD_DIR)/main.go

# Version
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date +%FT%T%z)
STRIP_SYMBOLS?=1
BASE_LDFLAGS=-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)
EXTRA_LDFLAGS=
ifeq ($(STRIP_SYMBOLS),1)
	EXTRA_LDFLAGS+= -s -w
endif
LDFLAGS=-ldflags "$(BASE_LDFLAGS) $(EXTRA_LDFLAGS)"

# Go variables
GO?=go
GOFLAGS?=
BUILD_FLAGS?=-trimpath -buildvcs=false
COMPRESS_BINARY?=0
UPX_FLAGS?=--best --lzma

# Linux slim build knobs (opt-in, keeps default behavior unchanged)
LINUX_SLIM_TAGS?=purego,netgo,osusergo
LINUX_SLIM_CGO?=0
LINUX_SLIM_LDFLAGS?=-buildid=
LINUX_SLIM_BUILD_FLAGS?=-trimpath -buildvcs=false -tags "$(LINUX_SLIM_TAGS)"
LINUX_SLIM_PATH=$(BUILD_DIR)/$(BINARY_NAME)-linux-$(ARCH)-slim

# Cross-platform build matrix (space-separated GOOS/GOARCH pairs)
BUILD_TARGETS?=linux/amd64 linux/arm64 linux/riscv64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64
CHANNELS?=telegram discord feishu maixcam qq dingtalk whatsapp
CHANNEL_PACKAGE_VARIANTS?=full none $(CHANNELS)
empty:=
space:=$(empty) $(empty)
comma:=,
ALL_CHANNEL_OMIT_TAGS=$(subst $(space),$(comma),$(addprefix omit_,$(CHANNELS)))
FULL_BUILD_TAGS=with_tui
NOCHANNELS_TAGS=$(ALL_CHANNEL_OMIT_TAGS),with_tui

# Installation
INSTALL_PREFIX?=/usr/local
INSTALL_BIN_DIR=$(INSTALL_PREFIX)/bin
INSTALL_MAN_DIR=$(INSTALL_PREFIX)/share/man/man1

# Workspace and Skills
USER_HOME:=$(if $(SUDO_USER),$(shell eval echo ~$(SUDO_USER)),$(HOME))
CLAWGO_HOME?=$(USER_HOME)/.clawgo
WORKSPACE_DIR?=$(CLAWGO_HOME)/workspace
WORKSPACE_SKILLS_DIR=$(WORKSPACE_DIR)/skills
BUILTIN_SKILLS_DIR=$(CURDIR)/skills
WORKSPACE_SOURCE_DIR=$(CURDIR)/workspace
EMBED_WORKSPACE_DIR=$(CURDIR)/cmd/workspace
EMBED_WEBUI_DIR=$(EMBED_WORKSPACE_DIR)/webui
DEV_CONFIG?=$(if $(wildcard $(CURDIR)/config.json),$(CURDIR)/config.json,$(CLAWGO_HOME)/config.json)
DEV_ARGS?=--debug gateway run
DEV_WORKSPACE?=$(WORKSPACE_DIR)
DEV_WEBUI_DIR?=$(CURDIR)/webui
WEBUI_DIST_DIR=$(DEV_WEBUI_DIR)/dist
WEBUI_PACKAGE_LOCK=$(DEV_WEBUI_DIR)/package-lock.json
NPM?=npm

# OS detection
UNAME_S:=$(shell uname -s)
UNAME_M:=$(shell uname -m)

# Platform-specific settings
ifeq ($(UNAME_S),Linux)
	PLATFORM=linux
	ifeq ($(UNAME_M),x86_64)
		ARCH=amd64
	else ifeq ($(UNAME_M),aarch64)
		ARCH=arm64
	else ifeq ($(UNAME_M),riscv64)
		ARCH=riscv64
	else
		ARCH=$(UNAME_M)
	endif
else ifeq ($(UNAME_S),Darwin)
	PLATFORM=darwin
	ifeq ($(UNAME_M),x86_64)
		ARCH=amd64
	else ifeq ($(UNAME_M),arm64)
		ARCH=arm64
	else
		ARCH=$(UNAME_M)
	endif
else
	PLATFORM=$(UNAME_S)
	ARCH=$(UNAME_M)
endif

BINARY_PATH=$(BUILD_DIR)/$(BINARY_NAME)-$(PLATFORM)-$(ARCH)

# Default target
all: build

## build: Build the clawgo binary for current platform
build: sync-embed-workspace
	@echo "Building $(BINARY_NAME) for $(PLATFORM)/$(ARCH)..."
	@mkdir -p $(BUILD_DIR)
	@set -e; trap '$(MAKE) cleanup-embed-workspace' EXIT; \
	$(GO) build $(GOFLAGS) $(BUILD_FLAGS) -tags "$(FULL_BUILD_TAGS)" $(LDFLAGS) -o $(BINARY_PATH) ./$(CMD_DIR); \
	if [ "$(COMPRESS_BINARY)" = "1" ]; then \
		if command -v upx >/dev/null 2>&1; then \
			upx $(UPX_FLAGS) "$(BINARY_PATH)" >/dev/null; \
			echo "Compressed binary with upx ($(UPX_FLAGS))"; \
		else \
			echo "Warning: COMPRESS_BINARY=1 but upx not found, skipping compression"; \
		fi; \
	fi
	@echo "Build complete: $(BINARY_PATH)"
	@ln -sf $(BINARY_NAME)-$(PLATFORM)-$(ARCH) $(BUILD_DIR)/$(BINARY_NAME)

## build-variants: Build current-platform full, no-channel, and per-channel binaries
build-variants: sync-embed-workspace
	@echo "Building channel variants for $(PLATFORM)/$(ARCH): $(CHANNEL_PACKAGE_VARIANTS)"
	@mkdir -p $(BUILD_DIR)
	@set -e; trap '$(MAKE) cleanup-embed-workspace' EXIT; \
	for variant in $(CHANNEL_PACKAGE_VARIANTS); do \
		tags=""; \
		suffix=""; \
		if [ "$$variant" = "none" ]; then \
			tags="$(NOCHANNELS_TAGS)"; \
			suffix="-nochannels"; \
		elif [ "$$variant" = "full" ]; then \
			tags="$(FULL_BUILD_TAGS)"; \
		elif [ "$$variant" != "full" ]; then \
			for ch in $(CHANNELS); do \
				if [ "$$ch" != "$$variant" ]; then \
					tags="$${tags:+$$tags,}omit_$$ch"; \
				fi; \
			done; \
			suffix="-$$variant"; \
		fi; \
		out="$(BUILD_DIR)/$(BINARY_NAME)-$(PLATFORM)-$(ARCH)$$suffix"; \
		echo " -> $$variant"; \
		if [ -n "$$tags" ]; then \
			$(GO) build $(GOFLAGS) $(BUILD_FLAGS) -tags "$$tags" $(LDFLAGS) -o "$$out" ./$(CMD_DIR); \
		else \
			$(GO) build $(GOFLAGS) $(BUILD_FLAGS) $(LDFLAGS) -o "$$out" ./$(CMD_DIR); \
		fi; \
		if [ "$(COMPRESS_BINARY)" = "1" ] && command -v upx >/dev/null 2>&1; then \
			upx $(UPX_FLAGS) "$$out" >/dev/null; \
		fi; \
	done
	@echo "Variant builds complete: $(BUILD_DIR)"

## build-linux-slim: Build a Linux-only slim binary (no feature trimming, no channel disabling)
build-linux-slim: sync-embed-workspace
	@echo "Building $(BINARY_NAME) slim profile for linux/$(ARCH)..."
	@mkdir -p $(BUILD_DIR)
	@set -e; trap '$(MAKE) cleanup-embed-workspace' EXIT; \
	CGO_ENABLED=$(LINUX_SLIM_CGO) GOOS=linux GOARCH=$(ARCH) $(GO) build $(GOFLAGS) $(LINUX_SLIM_BUILD_FLAGS) -ldflags "$(BASE_LDFLAGS) $(EXTRA_LDFLAGS) $(LINUX_SLIM_LDFLAGS)" -o $(LINUX_SLIM_PATH) ./$(CMD_DIR); \
	if [ "$(COMPRESS_BINARY)" = "1" ]; then \
		if command -v upx >/dev/null 2>&1; then \
			upx $(UPX_FLAGS) "$(LINUX_SLIM_PATH)" >/dev/null; \
			echo "Compressed slim binary with upx ($(UPX_FLAGS))"; \
		else \
			echo "Warning: COMPRESS_BINARY=1 but upx not found, skipping compression"; \
		fi; \
	fi
	@echo "Build complete: $(LINUX_SLIM_PATH)"

## build-all: Build clawgo for all configured platforms (override with BUILD_TARGETS="linux/amd64 ...")
build-all: sync-embed-workspace
	@echo "Building for multiple platforms: $(BUILD_TARGETS)"
	@mkdir -p $(BUILD_DIR)
	@set -e; trap '$(MAKE) cleanup-embed-workspace' EXIT; \
	for target in $(BUILD_TARGETS); do \
		goos="$${target%/*}"; \
		goarch="$${target#*/}"; \
		out="$(BUILD_DIR)/$(BINARY_NAME)-$$goos-$$goarch"; \
		if [ "$$goos" = "windows" ]; then out="$$out.exe"; fi; \
		echo " -> $$goos/$$goarch"; \
		CGO_ENABLED=0 GOOS=$$goos GOARCH=$$goarch $(GO) build $(GOFLAGS) $(BUILD_FLAGS) -tags "$(FULL_BUILD_TAGS)" $(LDFLAGS) -o "$$out" ./$(CMD_DIR); \
		if [ "$(COMPRESS_BINARY)" = "1" ] && command -v upx >/dev/null 2>&1; then \
			upx $(UPX_FLAGS) "$$out" >/dev/null; \
		fi; \
	done
	@echo "All builds complete"

## build-all-variants: Build full, no-channel, and per-channel binaries for all configured platforms
build-all-variants: sync-embed-workspace
	@echo "Building all channel variants for multiple platforms: $(BUILD_TARGETS)"
	@mkdir -p $(BUILD_DIR)
	@set -e; trap '$(MAKE) cleanup-embed-workspace' EXIT; \
	for target in $(BUILD_TARGETS); do \
		goos="$${target%/*}"; \
		goarch="$${target#*/}"; \
		for variant in $(CHANNEL_PACKAGE_VARIANTS); do \
			tags=""; \
			suffix=""; \
			if [ "$$variant" = "none" ]; then \
				tags="$(NOCHANNELS_TAGS)"; \
				suffix="-nochannels"; \
			elif [ "$$variant" = "full" ]; then \
				tags="$(FULL_BUILD_TAGS)"; \
			elif [ "$$variant" != "full" ]; then \
				for ch in $(CHANNELS); do \
					if [ "$$ch" != "$$variant" ]; then \
						tags="$${tags:+$$tags,}omit_$$ch"; \
					fi; \
				done; \
				suffix="-$$variant"; \
			fi; \
			out="$(BUILD_DIR)/$(BINARY_NAME)-$$goos-$$goarch$$suffix"; \
			if [ "$$goos" = "windows" ]; then out="$$out.exe"; fi; \
			echo " -> $$goos/$$goarch [$$variant]"; \
			if [ -n "$$tags" ]; then \
				CGO_ENABLED=0 GOOS=$$goos GOARCH=$$goarch $(GO) build $(GOFLAGS) $(BUILD_FLAGS) -tags "$$tags" $(LDFLAGS) -o "$$out" ./$(CMD_DIR); \
			else \
				CGO_ENABLED=0 GOOS=$$goos GOARCH=$$goarch $(GO) build $(GOFLAGS) $(BUILD_FLAGS) $(LDFLAGS) -o "$$out" ./$(CMD_DIR); \
			fi; \
			if [ "$(COMPRESS_BINARY)" = "1" ] && command -v upx >/dev/null 2>&1; then \
				upx $(UPX_FLAGS) "$$out" >/dev/null; \
			fi; \
		done; \
	done
	@echo "All variant builds complete"

## build-webui: Install WebUI dependencies when needed and build dist assets
build-webui:
	@echo "Building WebUI..."
	@if [ ! -d "$(DEV_WEBUI_DIR)" ]; then \
		echo "✗ Missing WebUI directory: $(DEV_WEBUI_DIR)"; \
		exit 1; \
	fi
	@if ! command -v "$(NPM)" >/dev/null 2>&1; then \
		echo "✗ npm is required to build the WebUI"; \
		exit 1; \
	fi
	@set -e; \
	if [ ! -d "$(DEV_WEBUI_DIR)/node_modules" ]; then \
		echo "Installing WebUI dependencies..."; \
		if [ -f "$(WEBUI_PACKAGE_LOCK)" ]; then \
			(cd "$(DEV_WEBUI_DIR)" && "$(NPM)" ci); \
		else \
			(cd "$(DEV_WEBUI_DIR)" && "$(NPM)" install); \
		fi; \
	fi; \
	(cd "$(DEV_WEBUI_DIR)" && "$(NPM)" run build)

## package-all: Create compressed archives and checksums for full, no-channel, and per-channel build variants
package-all: build-all-variants
	@echo "Packaging build artifacts..."
	@set -e; cd $(BUILD_DIR); \
	for target in $(BUILD_TARGETS); do \
		goos="$${target%/*}"; \
		goarch="$${target#*/}"; \
		for variant in $(CHANNEL_PACKAGE_VARIANTS); do \
			suffix=""; \
			archive_suffix=""; \
			if [ "$$variant" = "none" ]; then \
				suffix="-nochannels"; \
				archive_suffix="-nochannels"; \
			elif [ "$$variant" != "full" ]; then \
				suffix="-$$variant"; \
				archive_suffix="-$$variant"; \
			fi; \
			bin="$(BINARY_NAME)-$$goos-$$goarch$$suffix"; \
			if [ "$$goos" = "windows" ]; then \
				bin="$$bin.exe"; \
				archive="$(BINARY_NAME)-$$goos-$$goarch$$archive_suffix.zip"; \
				zip -q -j "$$archive" "$$bin"; \
			else \
				archive="$(BINARY_NAME)-$$goos-$$goarch$$archive_suffix.tar.gz"; \
				tar -czf "$$archive" "$$bin"; \
			fi; \
		done; \
	done
	@set -e; cd $(BUILD_DIR); \
	if command -v sha256sum >/dev/null 2>&1; then \
		sha256sum *.tar.gz *.zip 2>/dev/null | tee checksums.txt || true; \
	elif command -v shasum >/dev/null 2>&1; then \
		shasum -a 256 *.tar.gz *.zip 2>/dev/null | tee checksums.txt || true; \
	fi
	@echo "Package complete: $(BUILD_DIR)"

## sync-embed-workspace: Sync workspace seed files and built WebUI into cmd/workspace for go:embed
sync-embed-workspace: sync-embed-workspace-base sync-embed-webui
	@echo "✓ Embed assets ready in $(EMBED_WORKSPACE_DIR)"

## sync-embed-workspace-base: Sync root workspace files into cmd/workspace for go:embed
sync-embed-workspace-base:
	@echo "Syncing workspace seed files for embedding..."
	@if [ ! -d "$(WORKSPACE_SOURCE_DIR)" ]; then \
		echo "✗ Missing source workspace directory: $(WORKSPACE_SOURCE_DIR)"; \
		exit 1; \
	fi
	@mkdir -p "$(EMBED_WORKSPACE_DIR)"
	@rsync -a --delete "$(WORKSPACE_SOURCE_DIR)/" "$(EMBED_WORKSPACE_DIR)/"
	@echo "✓ Synced workspace to $(EMBED_WORKSPACE_DIR)"

## sync-embed-webui: Build and sync WebUI dist into embedded workspace assets
sync-embed-webui: build-webui
	@if [ ! -d "$(WEBUI_DIST_DIR)" ]; then \
		echo "✗ Missing WebUI dist directory: $(WEBUI_DIST_DIR)"; \
		exit 1; \
	fi
	@mkdir -p "$(EMBED_WEBUI_DIR)"
	@rsync -a --delete "$(WEBUI_DIST_DIR)/" "$(EMBED_WEBUI_DIR)/"
	@echo "✓ Synced WebUI dist to $(EMBED_WEBUI_DIR)"

## cleanup-embed-workspace: Remove synced embed workspace artifacts
cleanup-embed-workspace:
	@rm -rf "$(EMBED_WORKSPACE_DIR)"
	@echo "✓ Cleaned embedded workspace artifacts"

## install: Install clawgo to system and copy builtin skills
install: build
	@echo "Installing $(BINARY_NAME)..."
	@mkdir -p "$(INSTALL_BIN_DIR)"
	@set -e; \
	PID_FILE="$(CLAWGO_HOME)/gateway.pid"; \
	if [ -f "$$PID_FILE" ]; then \
		PID="$$(cat "$$PID_FILE" 2>/dev/null || true)"; \
		if [ -n "$$PID" ] && kill -0 "$$PID" 2>/dev/null; then \
			echo "Detected running $(BINARY_NAME) process (PID=$$PID), stopping..."; \
			kill "$$PID" 2>/dev/null || true; \
			for _i in 1 2 3 4 5; do \
				if ! kill -0 "$$PID" 2>/dev/null; then \
					break; \
				fi; \
				sleep 1; \
			done; \
			if kill -0 "$$PID" 2>/dev/null; then \
				echo "Process still running, force killing PID=$$PID..."; \
				kill -9 "$$PID" 2>/dev/null || true; \
			fi; \
		fi; \
		rm -f "$$PID_FILE"; \
	fi; \
	RUNNING_PIDS="$$(pgrep -x $(BINARY_NAME) 2>/dev/null || true)"; \
	if [ -n "$$RUNNING_PIDS" ]; then \
		echo "Detected additional running $(BINARY_NAME) process(es): $$RUNNING_PIDS, stopping..."; \
		kill $$RUNNING_PIDS 2>/dev/null || true; \
		sleep 1; \
		RUNNING_PIDS="$$(pgrep -x $(BINARY_NAME) 2>/dev/null || true)"; \
		if [ -n "$$RUNNING_PIDS" ]; then \
			echo "Force killing remaining process(es): $$RUNNING_PIDS"; \
			kill -9 $$RUNNING_PIDS 2>/dev/null || true; \
		fi; \
	fi; \
	BIN_PATH="$(INSTALL_BIN_DIR)/$(BINARY_NAME)"; \
	OVERWRITE_BIN="y"; \
	RESET_WORKSPACE="n"; \
	if [ -f "$$BIN_PATH" ]; then \
		printf "Detected existing binary at %s. Overwrite executable? (y/n): " "$$BIN_PATH"; \
		read -r OVERWRITE_BIN; \
		printf "Reset workspace at %s (delete and reload)? (y/n): " "$(WORKSPACE_DIR)"; \
		read -r RESET_WORKSPACE; \
	fi; \
	if [ "$$OVERWRITE_BIN" = "y" ]; then \
		cp "$(BUILD_DIR)/$(BINARY_NAME)" "$$BIN_PATH"; \
		chmod +x "$$BIN_PATH"; \
		echo "Installed binary to $$BIN_PATH"; \
	else \
		echo "Skipped binary overwrite."; \
	fi; \
	if [ "$$RESET_WORKSPACE" = "y" ]; then \
		echo "Resetting workspace: $(WORKSPACE_DIR)"; \
		rm -rf "$(WORKSPACE_DIR)"; \
		mkdir -p "$(WORKSPACE_DIR)"; \
		rsync -a "$(WORKSPACE_SOURCE_DIR)/" "$(WORKSPACE_DIR)/"; \
		echo "Workspace reloaded from $(WORKSPACE_SOURCE_DIR)"; \
	fi
	@echo "Installation complete!"

## install-user: Install clawgo to ~/.local and copy builtin skills
install-user:
	@$(MAKE) install INSTALL_PREFIX=$(USER_HOME)/.local

## install-win: Prepare Windows install bundle (binary + PowerShell installer)
install-win: build-all
	@echo "Preparing Windows install bundle..."
	@mkdir -p "$(BUILD_DIR)/windows-install"
	@cp "$(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe" "$(BUILD_DIR)/windows-install/$(BINARY_NAME).exe"
	@printf '%s\n' \
	'$ErrorActionPreference = "Stop"' \
	'$targetDir = Join-Path $$env:USERPROFILE ".clawgo\\bin"' \
	'New-Item -ItemType Directory -Force -Path $$targetDir | Out-Null' \
	'Copy-Item -Force ".\\clawgo.exe" (Join-Path $$targetDir "clawgo.exe")' \
	'$$userPath = [Environment]::GetEnvironmentVariable("Path", "User")' \
	'if (-not ($$userPath -split ";" | Where-Object { $$_ -eq $$targetDir })) {' \
	'  [Environment]::SetEnvironmentVariable("Path", "$$userPath;$$targetDir", "User")' \
	'}' \
	'Write-Host "Installed clawgo to $$targetDir"' \
	'Write-Host "Reopen terminal then run: clawgo status"' \
	> "$(BUILD_DIR)/windows-install/install-win.ps1"
	@echo "✓ Bundle ready: $(BUILD_DIR)/windows-install"
	@echo "Windows usage: copy folder to Windows, run PowerShell as user: ./install-win.ps1"

## uninstall: Remove clawgo from system
uninstall:
	@echo "Uninstalling $(BINARY_NAME)..."
	@rm -f $(INSTALL_BIN_DIR)/$(BINARY_NAME)
	@echo "Removed binary from $(INSTALL_BIN_DIR)/$(BINARY_NAME)"
	@echo "Note: Only the executable file has been deleted."
	@echo "If you need to delete all configurations (config.json, workspace, etc.), run 'make uninstall-all'"

## uninstall-all: Remove clawgo and all data
uninstall-all:
	@echo "Removing workspace and skills..."
	@rm -rf $(CLAWGO_HOME)
	@echo "Removed workspace: $(CLAWGO_HOME)"
	@echo "Complete uninstallation done!"

## clean: Remove build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@echo "Clean complete"

## test: Run Go tests without leaving build artifacts (cleans embed workspace and test cache)
test: sync-embed-workspace
	@echo "Running tests..."
	@set -e; trap '$(MAKE) cleanup-embed-workspace clean-test-artifacts' EXIT; \
	$(GO) test ./...

## test-only: Backward-compatible alias for Go tests
test-only: test

## clean-test-artifacts: Remove test caches/artifacts generated by go test
clean-test-artifacts:
	@$(GO) clean -testcache

## deps: Update dependencies
deps:
	@$(GO) get -u ./...
	@$(GO) mod tidy

## run: Build and run clawgo
run: build
	@$(BUILD_DIR)/$(BINARY_NAME) $(ARGS)

## dev: Build WebUI, sync workspace, and run the local gateway in foreground for debugging
dev: build-webui sync-embed-workspace
	@if [ ! -f "$(DEV_CONFIG)" ]; then \
		echo "✗ Missing config file: $(DEV_CONFIG)"; \
		echo "  Override with: make dev DEV_CONFIG=/path/to/config.json"; \
		exit 1; \
	fi
	@if [ ! -d "$(DEV_WEBUI_DIR)" ]; then \
		echo "✗ Missing WebUI directory: $(DEV_WEBUI_DIR)"; \
		exit 1; \
	fi
	@set -e; trap '$(MAKE) -C $(CURDIR) cleanup-embed-workspace' EXIT; \
	echo "Syncing WebUI dist to $(DEV_WORKSPACE)/webui ..."; \
	mkdir -p "$(DEV_WORKSPACE)/webui"; \
	rsync -a --delete "$(DEV_WEBUI_DIR)/dist/" "$(DEV_WORKSPACE)/webui/"; \
	echo "Starting local gateway debug session..."; \
	echo "  Config: $(DEV_CONFIG)"; \
	echo "  WebUI:  $(DEV_WORKSPACE)/webui"; \
	echo "  Args:   $(DEV_ARGS)"; \
	CLAWGO_CONFIG="$(DEV_CONFIG)" $(GO) run $(GOFLAGS) ./$(CMD_DIR) $(DEV_ARGS)

## test-docker: Build and compile-check in Docker (Dockerfile.test)
test-docker: sync-embed-workspace
	@echo "Running Docker compile test..."
	@set -e; trap '$(MAKE) cleanup-embed-workspace' EXIT; \
	docker build -f Dockerfile.test -t clawgo:test .
	@echo "Docker compile test passed"

## help: Show this help message
help:
	@echo "clawgo Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
	@echo ""
	@echo "Examples:"
	@echo "  make build              # Build for current platform"
	@echo "  make dev                # Run local gateway in foreground with debug logs"
	@echo "  make install            # Install to /usr/local/bin"
	@echo "  make install-user       # Install to ~/.local/bin"
	@echo "  make uninstall          # Remove from /usr/local/bin"
	@echo "  make install-skills     # Install skills to workspace"
	@echo ""
	@echo "Environment Variables:"
	@echo "  INSTALL_PREFIX          # Installation prefix (default: /usr/local)"
	@echo "  WORKSPACE_DIR           # Workspace directory (default: ~/.clawgo/workspace)"
	@echo "  DEV_CONFIG              # Config path for make dev"
	@echo "  DEV_ARGS                # CLI args for make dev (default: --debug gateway run)"
	@echo "  DEV_WORKSPACE           # Workspace path for WebUI sync in make dev"
	@echo "  DEV_WEBUI_DIR           # WebUI source dir for make dev (default: ./webui)"
	@echo "  NPM                     # npm executable for WebUI build (default: npm)"
	@echo "  VERSION                 # Version string (default: git describe)"
	@echo "  STRIP_SYMBOLS           # 1=strip debug/symbol info (default: 1)"
	@echo ""
	@echo "Current Configuration:"
	@echo "  Platform: $(PLATFORM)/$(ARCH)"
	@echo "  Binary: $(BINARY_PATH)"
	@echo "  Install Prefix: $(INSTALL_PREFIX)"
	@echo "  Workspace: $(WORKSPACE_DIR)"
