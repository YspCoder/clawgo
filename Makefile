.PHONY: all build build-all install uninstall clean help test install-bootstrap-docs sync-embed-workspace cleanup-embed-workspace test-only clean-test-artifacts

# Build variables
BINARY_NAME=clawgo
BUILD_DIR=build
CMD_DIR=cmd/$(BINARY_NAME)
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
LDFLAGS=-ldflags "$(BASE_LDFLAGS)$(EXTRA_LDFLAGS)"

# Go variables
GO?=go
GOFLAGS?=-v

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
EMBED_WORKSPACE_DIR=$(CURDIR)/cmd/$(BINARY_NAME)/workspace

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
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_PATH) ./$(CMD_DIR)
	@echo "Build complete: $(BINARY_PATH)"
	@ln -sf $(BINARY_NAME)-$(PLATFORM)-$(ARCH) $(BUILD_DIR)/$(BINARY_NAME)

## build-all: Build clawgo for all platforms
build-all: sync-embed-workspace
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	@set -e; trap '$(MAKE) cleanup-embed-workspace' EXIT; \
	GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./$(CMD_DIR); \
	GOOS=linux GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./$(CMD_DIR); \
	GOOS=linux GOARCH=riscv64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-riscv64 ./$(CMD_DIR); \
# 	GOOS=darwin GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./$(CMD_DIR)
	GOOS=windows GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./$(CMD_DIR)
	@echo "All builds complete"

## sync-embed-workspace: Sync root workspace files into cmd/clawgo/workspace for go:embed
sync-embed-workspace:
	@echo "Syncing workspace seed files for embedding..."
	@if [ ! -d "$(WORKSPACE_SOURCE_DIR)" ]; then \
		echo "✗ Missing source workspace directory: $(WORKSPACE_SOURCE_DIR)"; \
		exit 1; \
	fi
	@mkdir -p "$(EMBED_WORKSPACE_DIR)"
	@rsync -a --delete "$(WORKSPACE_SOURCE_DIR)/" "$(EMBED_WORKSPACE_DIR)/"
	@echo "✓ Synced to $(EMBED_WORKSPACE_DIR)"

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

## test-only: Run tests without leaving build artifacts (cleans embed workspace and test cache)
test-only: sync-embed-workspace
	@echo "Running tests..."
	@set -e; trap '$(MAKE) cleanup-embed-workspace clean-test-artifacts' EXIT; \
	$(GO) test ./...

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

## test: Build and compile-check in Docker (Dockerfile.test)
test: sync-embed-workspace
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
	@echo "  make install            # Install to /usr/local/bin"
	@echo "  make install-user       # Install to ~/.local/bin"
	@echo "  make uninstall          # Remove from /usr/local/bin"
	@echo "  make install-skills     # Install skills to workspace"
	@echo ""
	@echo "Environment Variables:"
	@echo "  INSTALL_PREFIX          # Installation prefix (default: /usr/local)"
	@echo "  WORKSPACE_DIR           # Workspace directory (default: ~/.clawgo/workspace)"
	@echo "  VERSION                 # Version string (default: git describe)"
	@echo "  STRIP_SYMBOLS           # 1=strip debug/symbol info (default: 1)"
	@echo ""
	@echo "Current Configuration:"
	@echo "  Platform: $(PLATFORM)/$(ARCH)"
	@echo "  Binary: $(BINARY_PATH)"
	@echo "  Install Prefix: $(INSTALL_PREFIX)"
	@echo "  Workspace: $(WORKSPACE_DIR)"
