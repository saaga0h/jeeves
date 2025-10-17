# J.E.E.V.E.S. Platform 2.0 Makefile

# Variables
BINARY_DIR := bin
GO := go
GOFLAGS := -v
LDFLAGS := -w -s
PLATFORMS := linux/amd64 linux/arm64

# Agent names
AGENTS := collector-agent illuminance-agent light-agent occupancy-agent behavior-agent observer-agent

.PHONY: all build build-all clean test test-coverage lint fmt deps help
.PHONY: run-collector run-illuminance run-light run-occupancy install-tools

# Default target
all: build

# Build for current platform
build:
	@echo "Building all agents for current platform..."
	@mkdir -p $(BINARY_DIR)
	@for agent in $(AGENTS); do \
		echo "Building $$agent..."; \
		$(GO) build $(GOFLAGS) -o $(BINARY_DIR)/$$agent ./cmd/$$agent/; \
	done
	@echo "Build complete! Binaries in $(BINARY_DIR)/"

# Build for all platforms
build-all:
	@echo "Building all agents for all platforms..."
	@mkdir -p $(BINARY_DIR)
	@for agent in $(AGENTS); do \
		for platform in $(PLATFORMS); do \
			GOOS=$${platform%/*}; \
			GOARCH=$${platform#*/}; \
			output=$(BINARY_DIR)/$$agent-$$GOOS-$$GOARCH; \
			echo "Building $$agent for $$GOOS/$$GOARCH..."; \
			GOOS=$$GOOS GOARCH=$$GOARCH $(GO) build -ldflags="$(LDFLAGS)" \
				-o $$output ./cmd/$$agent/; \
		done; \
	done
	@echo "Multi-arch build complete! Binaries in $(BINARY_DIR)/"

# Run tests
test:
	@echo "Running tests with race detector..."
	$(GO) test -v -race ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GO) test -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Lint code (requires golangci-lint)
lint:
	@echo "Running linter..."
	golangci-lint run ./...

# Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download
	$(GO) mod verify
	$(GO) mod tidy

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BINARY_DIR)
	rm -f coverage.out coverage.html
	@echo "Clean complete!"

# Development targets - run agents locally
run-collector:
	@echo "Running collector agent..."
	$(GO) run ./cmd/collector-agent/

run-illuminance:
	@echo "Running illuminance agent..."
	$(GO) run ./cmd/illuminance-agent/

run-light:
	@echo "Running light agent..."
	$(GO) run ./cmd/light-agent/

run-occupancy:
	@echo "Running occupancy agent..."
	$(GO) run ./cmd/occupancy-agent/

run-behavior:
	@echo "Running behavior agent..."
	$(GO) run ./cmd/behavior-agent/

run-observer:
	@echo "running observer agent..."
	$(GO) run ./cmd/observer-agent/

# Install development tools
install-tools:
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Tools installed!"

# Security scanning with Trivy
security-install:
	@echo "🔧 Installing Trivy..."
	@if ! command -v trivy >/dev/null 2>&1; then \
		if [[ "$$OSTYPE" == "darwin"* ]]; then \
			brew install trivy; \
		elif command -v apt-get >/dev/null 2>&1; then \
			sudo apt-get update && sudo apt-get install -y wget apt-transport-https gnupg lsb-release && \
			wget -qO - https://aquasecurity.github.io/trivy-repo/deb/public.key | sudo apt-key add - && \
			echo "deb https://aquasecurity.github.io/trivy-repo/deb $$(lsb_release -sc) main" | sudo tee -a /etc/apt/sources.list.d/trivy.list && \
			sudo apt-get update && sudo apt-get install -y trivy; \
		elif command -v yum >/dev/null 2>&1; then \
			sudo yum install -y wget && \
			wget https://github.com/aquasecurity/trivy/releases/download/v0.46.0/trivy_0.46.0_Linux-64bit.rpm && \
			sudo rpm -ivh trivy_0.46.0_Linux-64bit.rpm; \
		else \
			echo "Install Trivy manually: https://aquasecurity.github.io/trivy/latest/getting-started/installation/"; \
			exit 1; \
		fi; \
	else \
		echo "Trivy already installed"; \
	fi

security: security-install
	@echo "Running security scans..."
	@echo ""
	@echo "Scanning filesystem for vulnerabilities..."
	@trivy fs . --severity CRITICAL,HIGH,MEDIUM --format table --quiet
	@echo ""
	@echo "Scanning for misconfigurations..."
	@trivy config . --severity CRITICAL,HIGH,MEDIUM --format table --quiet
	@echo ""
	@echo "Scanning for secrets..."
	@trivy fs . --scanners secret --format table --quiet
	@echo ""
	@echo "Summary scan (CRITICAL & HIGH only)..."
	@trivy fs . --severity CRITICAL,HIGH --format table --quiet
	@echo ""
	@echo "Security scan complete!"

security-quick:
	@echo "🚀 Quick security scan (CRITICAL & HIGH only)..."
	@trivy fs . --severity CRITICAL,HIGH --format table --quiet
	@echo "Quick scan complete!"

security-go:
	@echo "Scanning Go dependencies..."
	@trivy fs . --scanners vuln --format table --quiet | grep -E "(go\.mod|Total|OS|Library)" || echo "No Go vulnerabilities found"

# Help
help:
	@echo "J.E.E.V.E.S. Platform 2.0 - Available Makefile targets:"
	@echo ""
	@echo "  make build           - Build all agents for current platform"
	@echo "  make build-all       - Build all agents for all platforms (amd64, arm64)"
	@echo "  make test            - Run tests with race detector"
	@echo "  make test-coverage   - Run tests with coverage report"
	@echo "  make lint            - Run linter (requires golangci-lint)"
	@echo "  make fmt             - Format all Go code"
	@echo "  make deps            - Download and verify dependencies"
	@echo "  make clean           - Remove build artifacts"
	@echo ""
	@echo "  make run-collector   - Run collector agent locally"
	@echo "  make run-illuminance - Run illuminance agent locally"
	@echo "  make run-light       - Run light agent locally"
	@echo "  make run-occupancy   - Run occupancy agent locally"
	@echo ""
	@echo "  make security-install - Install Trivy for security scanning"
	@echo "  make security        - Run full security scan (requires Trivy)"
	@echo "  make security-quick  - Run quick security scan (CRITICAL & HIGH only)"
	@echo "  make security-go     - Scan Go dependencies for vulnerabilities"
	@echo ""
	@echo "  make install-tools   - Install development tools (golangci-lint)"
	@echo "  make help            - Show this help message"
	@echo ""
	@echo "Example: Build for production deployment"
	@echo "  make clean && make build-all"
