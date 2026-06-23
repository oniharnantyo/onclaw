## onclaw Makefile
##
## All builds produce a fully static binary (CGO_ENABLED=0), stripped and
## trimmed (-s -w -trimpath), with version metadata injected via -ldflags.
APP         := onclaw
PKG         := github.com/oniharnantyo/onclaw
BIN_DIR     := bin
VERSION_PKG := $(PKG)/internal/version

# Version metadata — overridable from the environment, falls back to git or dev.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(VERSION_PKG).Version=$(VERSION) \
	-X $(VERSION_PKG).Commit=$(COMMIT) \
	-X $(VERSION_PKG).Date=$(DATE)
BUILD_FLAGS := -trimpath -ldflags="$(LDFLAGS)"

.DEFAULT_GOAL := build
.PHONY: build run test vet lint tidy fmt build-all release clean install help

build: ## Build the onclaw binary for the host (static, stripped)
	CGO_ENABLED=0 go build $(BUILD_FLAGS) -o $(BIN_DIR)/$(APP) .

run: build ## Build then run onclaw, e.g. `make run ARGS='version'`
	./$(BIN_DIR)/$(APP) $(ARGS)

test: ## Run all tests
	go test ./...

vet: ## Run go vet
	go vet ./...

fmt: ## Format Go sources
	gofmt -s -w .

lint: vet ## Run golangci-lint if installed, otherwise rely on go vet
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run ./... || echo "golangci-lint not installed; relying on 'go vet'"

tidy: ## Tidy and verify the module graph
	go mod tidy
	go mod verify

build-all: ## Cross-compile static binaries for linux amd64 / arm64 / armv7
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(BUILD_FLAGS) -o $(BIN_DIR)/$(APP)-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(BUILD_FLAGS) -o $(BIN_DIR)/$(APP)-linux-arm64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build $(BUILD_FLAGS) -o $(BIN_DIR)/$(APP)-linux-armv7 .

release: ## Build a local snapshot release with GoReleaser (no publish)
	goreleaser release --snapshot --clean

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) dist/

install: build ## Install the host binary to /usr/local/bin (override with DESTDIR=)
	install -d $(DESTDIR)/usr/local/bin
	install -m 0755 $(BIN_DIR)/$(APP) $(DESTDIR)/usr/local/bin/$(APP)

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
