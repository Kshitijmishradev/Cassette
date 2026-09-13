# Cassette build targets.
#
# There is no dependency step because there are no dependencies. See go.mod.

BINARY  := cassette
PKG     := ./cmd/cassette
MODULE  := github.com/Kshitijmishradev/cassette

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null)

# Trim paths so release builds are reproducible regardless of where they were
# built, and stamp identity so a cassette can record which build produced it.
LDFLAGS := -s -w \
	-X '$(MODULE)/internal/buildinfo.Version=$(VERSION)' \
	-X '$(MODULE)/internal/buildinfo.Commit=$(COMMIT)'

GOFLAGS := -trimpath

.DEFAULT_GOAL := build

.PHONY: build
build: ## Build the binary into ./bin
	@mkdir -p bin
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(PKG)
	@echo "built bin/$(BINARY) $(VERSION)"

.PHONY: install
install: ## Install into GOPATH/bin
	go install $(GOFLAGS) -ldflags "$(LDFLAGS)" $(PKG)

.PHONY: test
test: ## Run all tests
	go test ./...

.PHONY: race
race: ## Run tests under the race detector
	go test -race ./...

.PHONY: bench
bench: ## Run benchmarks
	go test -run '^$$' -bench . -benchmem ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: fmt
fmt: ## Format all Go source
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## Fail if any file is unformatted
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then echo "unformatted files:"; echo "$$out"; exit 1; fi

.PHONY: check
check: fmt-check vet test ## Everything CI runs

.PHONY: verify-transparency
verify-transparency: build ## Diff a real MCP server run direct vs wrapped
	./scripts/verify-transparency.sh

# Release matrix. macOS arm64 first because that is where agents actually run.
PLATFORMS := darwin/arm64 darwin/amd64 linux/arm64 linux/amd64

.PHONY: dist
dist: ## Cross-compile release binaries into ./dist
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "  $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 \
			go build $(GOFLAGS) -ldflags "$(LDFLAGS)" \
			-o dist/$(BINARY)-$$os-$$arch $(PKG) || exit 1; \
	done
	@echo "release binaries in ./dist"

.PHONY: clean
clean: ## Remove build output
	rm -rf bin dist

.PHONY: help
help: ## List targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
