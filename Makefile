.PHONY: all setup build dev clean test lint submodules check install install-llog launch test-ws plugins

# Default target
all: setup build

# ============================================================================
# Setup
# ============================================================================

# Initialize submodules and dependencies
setup: submodules check
	go mod download
	@echo "✓ Setup complete. Run 'make dev' to start development."

# Initialize/update git submodules
submodules:
	git submodule update --init --recursive

# Update submodules to latest upstream
submodules-update:
	git submodule update --remote --merge

# Verify wails is installed
check:
	@which wails > /dev/null || (echo "Error: wails not found. Install with: go install github.com/wailsapp/wails/v2/cmd/wails@latest" && exit 1)
	@echo "✓ Wails found: $$(wails version)"

# ============================================================================
# Development
# ============================================================================

# Run in development mode (hot reload)
dev: check
	wails dev

# Quick WebSocket test server (no Wails, for testing bridge)
test-ws:
	go run ./cmd/test-ws

# Run tests
test:
	go test ./...

# Run linter
lint:
	golangci-lint run

# ============================================================================
# Plugins
# ============================================================================

# Build modular plugins (concatenate src/*.js -> plugin.js)
plugins:
	./scripts/build-plugins.sh

# ============================================================================
# Build
# ============================================================================

# Build the Wails app (production)
build: check plugins
	wails build

# Build for current platform with debug info
build-debug: check
	wails build -debug

# Build for all platforms
build-all: check
	wails build -platform darwin/amd64
	wails build -platform darwin/arm64
	wails build -platform linux/amd64
	wails build -platform windows/amd64

# Install thymer-bar to system
install: build
	@echo "Installing to ~/.local/bin/thymer-bar"
	@mkdir -p ~/.local/bin
	@cp build/bin/thymer-bar ~/.local/bin/
	@echo "✓ Installed. Make sure ~/.local/bin is in your PATH"

# Build and launch (macOS) - kills existing instance first
launch: build
	@echo "Killing existing thymer-bar..."
	@-pkill -x thymer-bar 2>/dev/null || true
	@echo "Waiting for process to terminate..."
	@sleep 5
	@echo "Launching thymer-bar..."
	@open build/bin/thymer-bar.app

# Build and install llog CLI
install-llog:
	@echo "Building llog..."
	@go build -o ~/.local/bin/llog ./cmd/llog
	@echo "✓ Installed llog to ~/.local/bin/llog"

# ============================================================================
# Cleanup
# ============================================================================

# Clean build artifacts
clean:
	rm -rf build/bin
	go clean

# Deep clean (including Wails generated files)
clean-all: clean
	rm -rf frontend/dist
	rm -rf frontend/node_modules

# ============================================================================
# Utilities
# ============================================================================

# Generate code (if needed later)
generate:
	go generate ./...

# Check what ports are in use
ports:
	@echo "Checking thymer-bar ports..."
	@lsof -i :8496 2>/dev/null || echo "Port 8496 (HTTP/WS): free"
	@lsof -i :4222 2>/dev/null || echo "Port 4222 (NATS): free"

# Show status endpoint (requires running server)
status:
	@curl -s http://127.0.0.1:8496/status | jq . 2>/dev/null || echo "Server not running"

# Show help
help:
	@echo "thymer-bar Makefile"
	@echo ""
	@echo "Setup:"
	@echo "  make setup          - Initialize submodules and dependencies"
	@echo "  make check          - Verify wails is installed"
	@echo ""
	@echo "Development:"
	@echo "  make dev            - Run Wails app with hot reload"
	@echo "  make test-ws        - Run quick WebSocket test server (no Wails)"
	@echo "  make test           - Run Go tests"
	@echo "  make lint           - Run golangci-lint"
	@echo "  make plugins        - Build modular plugins (src/ -> plugin.js)"
	@echo ""
	@echo "Build:"
	@echo "  make build          - Build production binary (includes plugins)"
	@echo "  make build-debug    - Build with debug info"
	@echo "  make launch         - Build, kill existing, and launch (macOS)"
	@echo "  make install        - Build and install thymer-bar to ~/.local/bin"
	@echo "  make install-llog   - Build and install llog to ~/.local/bin"
	@echo ""
	@echo "Utilities:"
	@echo "  make ports          - Check if thymer-bar ports are in use"
	@echo "  make status         - Query running server status"
	@echo "  make clean          - Remove build artifacts"
