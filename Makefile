# J.E.E.V.E.S. Platform 2.0 Makefile

# Variables
BINARY_DIR := bin
GO := go
GOFLAGS := -v
LDFLAGS := -w -s
PLATFORMS := linux/amd64 linux/arm64

# Agent names
AGENTS := collector-agent illuminance-agent light-agent occupancy-agent

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

# Install development tools
install-tools:
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Tools installed!"

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
	@echo "  make install-tools   - Install development tools (golangci-lint)"
	@echo "  make help            - Show this help message"
	@echo ""
	@echo "Example: Build for production deployment"
	@echo "  make clean && make build-all"
