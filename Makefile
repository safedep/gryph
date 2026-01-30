GO := go
BIN_DIR := bin
BIN := $(BIN_DIR)/gryph
SHELL := /bin/bash
GITCOMMIT := $(shell git rev-parse HEAD)
VERSION := "$(shell git describe --tags --abbrev=0 2>/dev/null || echo v0.0.0)-$(shell git rev-parse --short HEAD)"

GO_CFLAGS=-X 'github.com/safedep/gryph/internal/version.Commit=$(GITCOMMIT)' -X 'github.com/safedep/gryph/internal/version.Version=$(VERSION)'
GO_LDFLAGS=-ldflags "-w $(GO_CFLAGS)"

.PHONY: all deps generate gryph clean test

all: gryph

# Install dependencies
deps:
	$(GO) mod download
	$(GO) mod tidy

# Generate ent code
generate:
	$(GO) generate ./storage/ent/...

# Build gryph binary
gryph: create_bin
	$(GO) build ${GO_LDFLAGS} -o $(BIN) ./cmd/gryph

create_bin:
	mkdir -p $(BIN_DIR)

clean:
	rm -rf $(BIN_DIR)

test:
	$(GO) test ./...

# Format code
fmt:
	$(GO) fmt ./...

# Run linter
lint:
	golangci-lint run

# Build for all platforms
build-all: create_bin
	GOOS=darwin GOARCH=amd64 $(GO) build ${GO_LDFLAGS} -o $(BIN_DIR)/gryph-darwin-amd64 ./cmd/gryph
	GOOS=darwin GOARCH=arm64 $(GO) build ${GO_LDFLAGS} -o $(BIN_DIR)/gryph-darwin-arm64 ./cmd/gryph
	GOOS=linux GOARCH=amd64 $(GO) build ${GO_LDFLAGS} -o $(BIN_DIR)/gryph-linux-amd64 ./cmd/gryph
	GOOS=windows GOARCH=amd64 $(GO) build ${GO_LDFLAGS} -o $(BIN_DIR)/gryph-windows-amd64.exe ./cmd/gryph
