# ----------------------------------------
# MySQL MCP Server – Makefile
# ----------------------------------------

APP_NAME = mysql-mcp-server
BIN_DIR = bin
BIN = $(BIN_DIR)/$(APP_NAME)
PKG = ./cmd/mysql-mcp-server

# Version information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Build flags for version injection
VERSION_FLAGS = -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)
LDFLAGS = -ldflags "$(VERSION_FLAGS)"
LDFLAGS_RELEASE = -ldflags "$(VERSION_FLAGS) -s -w"

# Colors
YELLOW=\033[1;33m
GREEN=\033[1;32m
BLUE=\033[1;34m
CYAN=\033[1;36m
RED=\033[1;31m
RESET=\033[0m

# Default target
.DEFAULT_GOAL := help

# ----------------------------------------
# Build / Run
# ----------------------------------------

build:
	@echo "$(CYAN)🔨 Building $(APP_NAME) $(VERSION)...$(RESET)"
	@mkdir -p $(BIN_DIR)
	@go build $(LDFLAGS) -o $(BIN) $(PKG)
	@echo "$(GREEN)✔ Build complete: $(BIN)$(RESET)"

run: build
	@echo "$(CYAN)🚀 Running $(APP_NAME)...$(RESET)"
	@$(BIN)

version:
	@echo "Version: $(VERSION)"
	@echo "Build Time: $(BUILD_TIME)"
	@echo "Git Commit: $(GIT_COMMIT)"

clean:
	@echo "$(YELLOW)🧹 Cleaning project...$(RESET)"
	rm -rf $(BIN_DIR)
	@echo "$(GREEN)✔ Clean complete$(RESET)"

# ----------------------------------------
# Testing
# ----------------------------------------

test:
	@echo "$(BLUE)🧪 Running unit tests...$(RESET)"
	go test ./... -v

test-security:
	@echo "$(BLUE)🔐 Running security tests...$(RESET)"
	go test -v ./tests/security/...

integration:
	@echo "$(BLUE)🐋 Running integration tests (Docker required)...$(RESET)"
	go test -tags=integration ./internal/mysql -v

# Integration tests with Docker Compose
test-integration: test-mysql-up
	@echo "$(BLUE)🐋 Running full integration test suite...$(RESET)"
	MYSQL_TEST_DSN="root:testpass@tcp(localhost:3306)/testdb?parseTime=true" \
		go test -tags=integration -v ./...
	@$(MAKE) test-mysql-down

test-integration-80: test-mysql-up
	@echo "$(BLUE)🐋 Running integration tests against MySQL 8.0...$(RESET)"
	MYSQL_TEST_DSN="root:testpass@tcp(localhost:3306)/testdb?parseTime=true" \
		go test -tags=integration -v ./tests/integration/...
	@$(MAKE) test-mysql-down

test-integration-84:
	@echo "$(BLUE)🐋 Running integration tests against MySQL 8.4...$(RESET)"
	@docker-compose -f docker-compose.test.yml up -d mysql84
	@echo "Waiting for MySQL 8.4 to be ready..."
	@sleep 15
	@MYSQL_TEST_DSN="root:testpass@tcp(localhost:3307)/testdb?parseTime=true" \
		go test -tags=integration -v ./tests/integration/...; \
		TEST_EXIT=$$?; \
		docker-compose -f docker-compose.test.yml stop mysql84; \
		exit $$TEST_EXIT

test-integration-90:
	@echo "$(BLUE)🐋 Running integration tests against MySQL 9.0...$(RESET)"
	@docker-compose -f docker-compose.test.yml up -d mysql90
	@echo "Waiting for MySQL 9.0 to be ready..."
	@sleep 15
	@MYSQL_TEST_DSN="root:testpass@tcp(localhost:3308)/testdb?parseTime=true" \
		go test -tags=integration -v ./tests/integration/...; \
		TEST_EXIT=$$?; \
		docker-compose -f docker-compose.test.yml stop mysql90; \
		exit $$TEST_EXIT

test-integration-all:
	@echo "$(BLUE)🐋 Running integration tests against all MySQL versions...$(RESET)"
	@$(MAKE) test-integration-80
	@$(MAKE) test-integration-84
	@$(MAKE) test-integration-90
	@echo "$(GREEN)✔ All integration tests complete$(RESET)"

# Docker Compose helpers for test databases
test-mysql-up:
	@echo "$(CYAN)🐳 Starting MySQL test containers...$(RESET)"
	docker-compose -f docker-compose.test.yml up -d mysql80
	@echo "Waiting for MySQL to be ready..."
	@sleep 15

test-mysql-down:
	@echo "$(CYAN)🐳 Stopping MySQL test containers...$(RESET)"
	docker-compose -f docker-compose.test.yml down

test-mysql-all-up:
	@echo "$(CYAN)🐳 Starting all MySQL test containers...$(RESET)"
	docker-compose -f docker-compose.test.yml up -d
	@echo "Waiting for MySQL containers to be ready..."
	@sleep 20

test-mysql-logs:
	docker-compose -f docker-compose.test.yml logs -f

# ----------------------------------------
# Code Quality
# ----------------------------------------

fmt:
	@echo "$(CYAN)🎨 Formatting Go code...$(RESET)"
	go fmt ./...
	@echo "$(GREEN)✔ Code formatted$(RESET)"

fmt-check:
	@echo "$(CYAN)🔍 Checking code formatting...$(RESET)"
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "$(RED)✘ Code is not formatted:$(RESET)"; \
		gofmt -l .; \
		exit 1; \
	fi
	@echo "$(GREEN)✔ Code is properly formatted$(RESET)"

lint:
	@echo "$(CYAN)🔍 Running linter...$(RESET)"
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "$(YELLOW)⚠ golangci-lint not installed, running go vet instead$(RESET)"; \
		go vet ./...; \
	fi
	@echo "$(GREEN)✔ Lint complete$(RESET)"

vet:
	@echo "$(CYAN)🔍 Running go vet...$(RESET)"
	go vet ./...
	@echo "$(GREEN)✔ Vet complete$(RESET)"

# ----------------------------------------
# Security
# ----------------------------------------

security:
	@echo "$(CYAN)🔒 Running security scan...$(RESET)"
	@if command -v gosec >/dev/null 2>&1; then \
		gosec -exclude-generated -severity medium ./...; \
	else \
		echo "$(YELLOW)⚠ gosec not installed. Install: go install github.com/securego/gosec/v2/cmd/gosec@latest$(RESET)"; \
	fi
	@echo "$(GREEN)✔ Security scan complete$(RESET)"

vuln:
	@echo "$(CYAN)🔒 Checking for vulnerabilities...$(RESET)"
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	else \
		echo "$(YELLOW)⚠ govulncheck not installed. Install: go install golang.org/x/vuln/cmd/govulncheck@latest$(RESET)"; \
	fi
	@echo "$(GREEN)✔ Vulnerability check complete$(RESET)"

# ----------------------------------------
# Testing with Coverage
# ----------------------------------------

coverage:
	@echo "$(BLUE)📊 Running tests with coverage...$(RESET)"
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out
	@echo "$(GREEN)✔ Coverage report generated$(RESET)"

coverage-html: coverage
	@echo "$(BLUE)📊 Generating HTML coverage report...$(RESET)"
	go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)✔ Open coverage.html in browser$(RESET)"

# ----------------------------------------
# Dependencies
# ----------------------------------------

deps:
	@echo "$(CYAN)📦 Downloading Go dependencies...$(RESET)"
	go mod tidy
	@echo "$(GREEN)✔ Dependencies updated$(RESET)"

# ----------------------------------------
# Docker Build
# ----------------------------------------

docker:
	@echo "$(CYAN)🐳 Building Docker image '$(APP_NAME)'...$(RESET)"
	docker build -t $(APP_NAME):latest .
	@echo "$(GREEN)✔ Docker image built$(RESET)"

# ----------------------------------------
# Release Build
# ----------------------------------------

release:
	@echo "$(CYAN)📦 Creating production release binaries $(VERSION)...$(RESET)"
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS_RELEASE) -o $(BIN).linux-amd64 $(PKG)
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS_RELEASE) -o $(BIN).linux-arm64 $(PKG)
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS_RELEASE) -o $(BIN).darwin-amd64 $(PKG)
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS_RELEASE) -o $(BIN).darwin-arm64 $(PKG)
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS_RELEASE) -o $(BIN).windows-amd64.exe $(PKG)
	@echo "$(GREEN)✔ Release artifacts ready in $(BIN_DIR)/$(RESET)"

# ----------------------------------------
# Full QA Pipeline
# ----------------------------------------

qa: fmt-check vet lint test
	@echo "$(GREEN)✅ QA checks passed!$(RESET)"

qa-full: fmt-check vet lint security vuln test coverage
	@echo "$(GREEN)✅ Full QA pipeline passed!$(RESET)"

# ----------------------------------------
# Pre-commit Hook
# ----------------------------------------

pre-commit: fmt lint test
	@echo "$(GREEN)✅ Pre-commit checks passed!$(RESET)"

install-hooks:
	@echo "$(CYAN)🔧 Installing git hooks...$(RESET)"
	@echo '#!/bin/bash\nmake pre-commit' > .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "$(GREEN)✔ Pre-commit hook installed$(RESET)"

# ----------------------------------------
# Help
# ----------------------------------------

help:
	@echo ""
	@echo "$(YELLOW)Available Make targets:$(RESET)"
	@echo ""
	@echo "$(CYAN)Build & Run:$(RESET)"
	@echo "  make build        - Build the server (version: $(VERSION))"
	@echo "  make run          - Build + run the server"
	@echo "  make clean        - Remove build artifacts"
	@echo "  make docker       - Build Docker image"
	@echo "  make release      - Build multi-platform binaries"
	@echo "  make version      - Show version information"
	@echo ""
	@echo "$(CYAN)Testing:$(RESET)"
	@echo "  make test              - Run unit tests"
	@echo "  make test-security     - Run security/validator tests"
	@echo "  make integration       - Run basic integration tests"
	@echo "  make test-integration  - Run full integration suite (Docker Compose)"
	@echo "  make test-integration-80  - Test against MySQL 8.0"
	@echo "  make test-integration-84  - Test against MySQL 8.4"
	@echo "  make test-integration-90  - Test against MySQL 9.0"
	@echo "  make test-integration-all - Test against all MySQL versions"
	@echo "  make coverage          - Run tests with coverage report"
	@echo "  make coverage-html     - Generate HTML coverage report"
	@echo ""
	@echo "$(CYAN)Test Database Management:$(RESET)"
	@echo "  make test-mysql-up     - Start MySQL 8.0 test container"
	@echo "  make test-mysql-all-up - Start all MySQL test containers"
	@echo "  make test-mysql-down   - Stop test containers"
	@echo "  make test-mysql-logs   - View test container logs"
	@echo ""
	@echo "$(CYAN)Code Quality:$(RESET)"
	@echo "  make fmt          - Format Go code"
	@echo "  make fmt-check    - Check if code is formatted"
	@echo "  make lint         - Run golangci-lint"
	@echo "  make vet          - Run go vet"
	@echo ""
	@echo "$(CYAN)Security:$(RESET)"
	@echo "  make security     - Run gosec security scanner"
	@echo "  make vuln         - Check for vulnerabilities"
	@echo ""
	@echo "$(CYAN)QA Pipeline:$(RESET)"
	@echo "  make qa           - Run quick QA (fmt, vet, lint, test)"
	@echo "  make qa-full      - Run full QA pipeline"
	@echo "  make pre-commit   - Run pre-commit checks"
	@echo "  make install-hooks- Install git pre-commit hook"
	@echo ""
	@echo "$(CYAN)Dependencies:$(RESET)"
	@echo "  make deps         - Download and tidy modules"
	@echo ""
