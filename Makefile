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

# Host ports for docker-compose.test.yml services. Exported so compose uses the
# same values; override when a port clashes with a local server, e.g.
#   make test-integration-84 MYSQL84_PORT=13307
export MYSQL80_PORT ?= 13306
export MYSQL84_PORT ?= 3307
export MYSQL90_PORT ?= 3308
export MYSQL97_PORT ?= 13309
export MYSQL2610_PORT ?= 13311
export MARIADB11_PORT ?= 3310
export MARIADB12_PORT ?= 13312

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

# End-to-end smoke tests of the built binary (REST, stdio, MCP Streamable HTTP) against a
# throwaway MySQL/MariaDB container. Override: make smoke-http SMOKE_MYSQL_VERSION=26.10
# (a MySQL tag) or SMOKE_MYSQL_VERSION=mariadb:11.4 (a full image).
SMOKE_MYSQL_VERSION ?= 8.0
smoke-http:
	@echo "$(BLUE)💨 Running MCP HTTP smoke tests against MySQL $(SMOKE_MYSQL_VERSION) (Docker required)...$(RESET)"
	@./scripts/smoke-mcp-http.sh $(SMOKE_MYSQL_VERSION)

integration:
	@echo "$(BLUE)🐋 Running integration tests (Docker required)...$(RESET)"
	go test -tags=integration ./internal/mysql -v

# Integration tests with Docker Compose
test-integration: test-mysql-up
	@echo "$(BLUE)🐋 Running full integration test suite...$(RESET)"
	@MYSQL_TEST_DSN="mcpuser:mcppass00@tcp(127.0.0.1:$(MYSQL80_PORT))/testdb?parseTime=true" \
		go test -tags=integration -v ./...; \
		TEST_EXIT=$$?; \
		docker compose -f docker-compose.test.yml down; \
		exit $$TEST_EXIT

test-integration-80: test-mysql-up
	@echo "$(BLUE)🐋 Running integration tests against MySQL 8.0...$(RESET)"
	@MYSQL_TEST_DSN="mcpuser:mcppass00@tcp(127.0.0.1:$(MYSQL80_PORT))/testdb?parseTime=true" \
		go test -tags=integration -v ./tests/integration/...; \
		TEST_EXIT=$$?; \
		docker compose -f docker-compose.test.yml down; \
		exit $$TEST_EXIT

test-integration-84:
	@echo "$(BLUE)🐋 Running integration tests against MySQL 8.4...$(RESET)"
	@docker compose -f docker-compose.test.yml up -d --wait --wait-timeout 90 mysql84
	@MYSQL_TEST_DSN="mcpuser:mcppass00@tcp(127.0.0.1:$(MYSQL84_PORT))/testdb?parseTime=true" \
		go test -tags=integration -v ./tests/integration/...; \
		TEST_EXIT=$$?; \
		docker compose -f docker-compose.test.yml stop mysql84; \
		exit $$TEST_EXIT

test-integration-90:
	@echo "$(BLUE)🐋 Running integration tests against MySQL 9.0...$(RESET)"
	@docker compose -f docker-compose.test.yml up -d --wait --wait-timeout 90 mysql90
	@MYSQL_TEST_DSN="mcpuser:mcppass00@tcp(127.0.0.1:$(MYSQL90_PORT))/testdb?parseTime=true" \
		go test -tags=integration -v ./tests/integration/...; \
		TEST_EXIT=$$?; \
		docker compose -f docker-compose.test.yml stop mysql90; \
		exit $$TEST_EXIT

test-integration-97:
	@echo "$(BLUE)🐋 Running integration tests against MySQL 9.7...$(RESET)"
	@docker compose -f docker-compose.test.yml up -d --wait --wait-timeout 90 mysql97
	@MYSQL_TEST_DSN="mcpuser:mcppass00@tcp(127.0.0.1:$(MYSQL97_PORT))/testdb?parseTime=true" \
		MYSQL_SAKILA_DSN="mcpuser:mcppass00@tcp(127.0.0.1:$(MYSQL97_PORT))/sakila?parseTime=true" \
		go test -tags=integration -v ./tests/integration/...; \
		TEST_EXIT=$$?; \
		docker compose -f docker-compose.test.yml stop mysql97; \
		exit $$TEST_EXIT

test-integration-2610:
	@echo "$(BLUE)🐋 Running integration tests against MySQL 26.10...$(RESET)"
	@docker compose -f docker-compose.test.yml up -d --wait --wait-timeout 90 mysql2610
	@MYSQL_TEST_DSN="mcpuser:mcppass00@tcp(127.0.0.1:$(MYSQL2610_PORT))/testdb?parseTime=true" \
		MYSQL_SAKILA_DSN="mcpuser:mcppass00@tcp(127.0.0.1:$(MYSQL2610_PORT))/sakila?parseTime=true" \
		go test -tags=integration -v ./tests/integration/...; \
		TEST_EXIT=$$?; \
		docker compose -f docker-compose.test.yml stop mysql2610; \
		exit $$TEST_EXIT

test-integration-all:
	@echo "$(BLUE)🐋 Running integration tests against all MySQL and MariaDB versions...$(RESET)"
	@$(MAKE) test-integration-80
	@$(MAKE) test-integration-84
	@$(MAKE) test-integration-90
	@$(MAKE) test-integration-97
	@$(MAKE) test-integration-2610
	@$(MAKE) test-integration-mariadb-11
	@$(MAKE) test-integration-mariadb-12
	@echo "$(GREEN)✔ All integration tests complete$(RESET)"

test-integration-mariadb-11:
	@echo "$(BLUE)🐋 Running integration tests against MariaDB 11.4...$(RESET)"
	@docker compose -f docker-compose.test.yml up -d --wait --wait-timeout 90 mariadb11
	@MYSQL_TEST_DSN="mcpuser:mcppass00@tcp(127.0.0.1:$(MARIADB11_PORT))/testdb?parseTime=true&charset=utf8mb4" \
		go test -tags=integration -v ./tests/integration/...; \
		TEST_EXIT=$$?; \
		docker compose -f docker-compose.test.yml stop mariadb11; \
		exit $$TEST_EXIT

test-integration-mariadb-12:
	@echo "$(BLUE)🐋 Running integration tests against MariaDB 12.3...$(RESET)"
	@docker compose -f docker-compose.test.yml up -d --wait --wait-timeout 90 mariadb12
	@MYSQL_TEST_DSN="mcpuser:mcppass00@tcp(127.0.0.1:$(MARIADB12_PORT))/testdb?parseTime=true&charset=utf8mb4" \
		MYSQL_SAKILA_DSN="mcpuser:mcppass00@tcp(127.0.0.1:$(MARIADB12_PORT))/sakila?parseTime=true&charset=utf8mb4" \
		go test -tags=integration -v ./tests/integration/...; \
		TEST_EXIT=$$?; \
		docker compose -f docker-compose.test.yml stop mariadb12; \
		exit $$TEST_EXIT

# SSH tunnel integration test (issue #79): requires mysql80 + ssh_bastion
test-integration-ssh:
	@echo "$(BLUE)🔐 Running SSH tunnel integration test...$(RESET)"
	@mkdir -p tests/integration/fixtures
	@echo "$(CYAN)🔑 Generating ephemeral SSH client key...$(RESET)"
	@rm -f tests/integration/fixtures/ssh_test_key tests/integration/fixtures/ssh_test_key.pub
	@ssh-keygen -t ed25519 -f tests/integration/fixtures/ssh_test_key -N "" -q
	@docker compose -f docker-compose.test.yml up -d --wait --wait-timeout 90 mysql80 ssh_bastion
	@echo "$(CYAN)🔑 Authorizing client key and fetching bastion host key...$(RESET)"
	@docker exec -i mysql-mcp-test-ssh-bastion sh -c 'cat >> /root/.ssh/authorized_keys' < tests/integration/fixtures/ssh_test_key.pub
	@sleep 2
	@ssh-keyscan -p 2222 localhost > tests/integration/fixtures/ssh_bastion_known_hosts 2>/dev/null
	@MYSQL_SSH_HOST=localhost MYSQL_SSH_PORT=2222 MYSQL_SSH_USER=root \
		MYSQL_SSH_KEY_PATH="$$(pwd)/tests/integration/fixtures/ssh_test_key" \
		MYSQL_SSH_KNOWN_HOSTS="$$(pwd)/tests/integration/fixtures/ssh_bastion_known_hosts" \
		MYSQL_SSH_TEST_DSN="mcpuser:mcppass00@tcp(mysql80:3306)/testdb?parseTime=true" \
		go test -tags=integration -v -run TestSSHTunnel ./tests/integration/...; \
		TEST_EXIT=$$?; \
		docker compose -f docker-compose.test.yml stop ssh_bastion mysql80; \
		rm -f tests/integration/fixtures/ssh_bastion_known_hosts tests/integration/fixtures/ssh_test_key tests/integration/fixtures/ssh_test_key.pub; \
		exit $$TEST_EXIT

# Sakila database integration tests
test-sakila: test-mysql-up
	@echo "$(BLUE)🎬 Running Sakila database integration tests...$(RESET)"
	@MYSQL_TEST_DSN="mcpuser:mcppass00@tcp(127.0.0.1:$(MYSQL80_PORT))/sakila?parseTime=true&multiStatements=true" \
		go test -tags=integration -v -run "Sakila" ./tests/integration/...; \
		TEST_EXIT=$$?; \
		docker compose -f docker-compose.test.yml down; \
		exit $$TEST_EXIT

test-sakila-84:
	@echo "$(BLUE)🎬 Running Sakila tests against MySQL 8.4...$(RESET)"
	@docker compose -f docker-compose.test.yml up -d --wait --wait-timeout 90 mysql84
	@MYSQL_TEST_DSN="mcpuser:mcppass00@tcp(127.0.0.1:$(MYSQL84_PORT))/sakila?parseTime=true&multiStatements=true" \
		go test -tags=integration -v -run "Sakila" ./tests/integration/...; \
		TEST_EXIT=$$?; \
		docker compose -f docker-compose.test.yml stop mysql84; \
		exit $$TEST_EXIT

test-sakila-90:
	@echo "$(BLUE)🎬 Running Sakila tests against MySQL 9.0...$(RESET)"
	@docker compose -f docker-compose.test.yml up -d --wait --wait-timeout 90 mysql90
	@MYSQL_TEST_DSN="mcpuser:mcppass00@tcp(127.0.0.1:$(MYSQL90_PORT))/sakila?parseTime=true&multiStatements=true" \
		go test -tags=integration -v -run "Sakila" ./tests/integration/...; \
		TEST_EXIT=$$?; \
		docker compose -f docker-compose.test.yml stop mysql90; \
		exit $$TEST_EXIT

# Sakila tests against local MySQL (no Docker). Set MYSQL_TEST_DSN or MYSQL_SAKILA_DSN, e.g.:
#   export MYSQL_TEST_DSN='user:pass@tcp(127.0.0.1:3306)/sakila?parseTime=true&multiStatements=true'
#   make test-sakila-local
test-sakila-local:
	@if [ -z "$$MYSQL_TEST_DSN" ] && [ -z "$$MYSQL_SAKILA_DSN" ]; then \
		echo "$(RED)Set MYSQL_TEST_DSN or MYSQL_SAKILA_DSN. Example for port 3306:$(RESET)"; \
		echo "  export MYSQL_TEST_DSN='mcpuser:mcppass00@tcp(127.0.0.1:3306)/sakila?parseTime=true&multiStatements=true'"; \
		exit 1; \
	fi
	@echo "$(BLUE)🎬 Running Sakila tests against local MySQL (DSN from env)...$(RESET)"
	@go test -tags=integration -v -run "Sakila" ./tests/integration/...

# Docker Compose helpers for test databases
test-mysql-up:
	@echo "$(CYAN)🐳 Starting MySQL test containers...$(RESET)"
	@docker compose -f docker-compose.test.yml up -d --wait --wait-timeout 90 mysql80

test-mysql-down:
	@echo "$(CYAN)🐳 Stopping MySQL test containers...$(RESET)"
	docker compose -f docker-compose.test.yml down

test-mysql-all-up:
	@echo "$(CYAN)🐳 Starting all MySQL test containers...$(RESET)"
	@docker compose -f docker-compose.test.yml up -d --wait --wait-timeout 90

test-mysql-logs:
	docker compose -f docker-compose.test.yml logs -f

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
# GitHub PR (requires gh CLI)
# ----------------------------------------
# Merge PR by number; always uses --merge for non-interactive use.
pr-merge:
	@if [ -z "$(PR)" ]; then echo "Usage: make pr-merge PR=<number>"; exit 1; fi
	@gh pr merge $(PR) --merge

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
	@echo "  make smoke-http        - Smoke-test the binary (REST, stdio, MCP HTTP) vs MySQL (SMOKE_MYSQL_VERSION=8.0)"
	@echo "  make integration       - Run basic integration tests"
	@echo "  make test-integration  - Run full integration suite (Docker Compose)"
	@echo "  make test-integration-80   - Test against MySQL 8.0"
	@echo "  make test-integration-ssh  - Test SSH tunnel (mysql80 + ssh_bastion)"
	@echo "  make test-integration-84  - Test against MySQL 8.4"
	@echo "  make test-integration-90  - Test against MySQL 9.0"
	@echo "  make test-integration-97  - Test against MySQL 9.7"
	@echo "  make test-integration-2610 - Test against MySQL 26.10"
	@echo "  make test-integration-mariadb-11 - Test against MariaDB 11.4"
	@echo "  make test-integration-mariadb-12 - Test against MariaDB 12.3"
	@echo "  make test-integration-all - Test against all MySQL and MariaDB versions"
	@echo "  make test-sakila       - Run Sakila database tests (MySQL 8.0)"
	@echo "  make test-sakila-local - Run Sakila tests vs local MySQL (set MYSQL_TEST_DSN; e.g. :3306)"
	@echo "  make test-sakila-84    - Run Sakila tests against MySQL 8.4"
	@echo "  make test-sakila-90    - Run Sakila tests against MySQL 9.0"
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
	@echo "$(CYAN)GitHub (requires gh):$(RESET)"
	@echo "  make pr-merge PR=n - Merge pull request n (e.g. make pr-merge PR=91)"
	@echo ""
