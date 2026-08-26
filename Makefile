GO := go
BIN_DIR := bin
BIN := $(BIN_DIR)/gryph
SHELL := /bin/bash
GITCOMMIT := $(shell git rev-parse HEAD)
VERSION := "$(shell git describe --tags --abbrev=0 2>/dev/null || echo v0.0.0)-$(shell git rev-parse --short HEAD)"

GO_CFLAGS=-X 'github.com/safedep/gryph/internal/version.Commit=$(GITCOMMIT)' -X 'github.com/safedep/gryph/internal/version.Version=$(VERSION)'
GO_LDFLAGS=-ldflags "-w $(GO_CFLAGS)"

.PHONY: all deps generate generate-schema verify-schema gryph clean test conformance conformance-json conformance-markdown

all: gryph

# Install dependencies
deps:
	$(GO) mod download
	$(GO) mod tidy

# Generate ent code
generate:
	$(GO) generate ./storage/ent/...

# Generate Event and Policy JSON Schemas
generate-schema:
	$(GO) run ./cmd/jsonschema-gen

# Update bundled pricing data from models.dev
update-pricing:
	$(GO) run pricing/scripts/update-pricing.go

# Verify Event and Policy JSON Schemas are up-to-date
verify-schema: generate-schema
	@git diff --exit-code schema/event.schema.json schema/policy.schema.json || (echo "ERROR: one or more JSON schemas are out of date. Run 'make generate-schema' and commit the result." && exit 1)

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

# AARM conformance suite. Builds the gryph binary (which the CLI invokes
# to drive the test runner) and the standalone conformance test binary
# (which the CLI prefers when present), then runs the chosen renderer.
conformance: gryph create_bin
	$(GO) test -c -o $(BIN_DIR)/gryph-conformance.test ./test/conformance/aarm/
	$(BIN) aarm conformance --format text

conformance-json: gryph create_bin
	$(GO) test -c -o $(BIN_DIR)/gryph-conformance.test ./test/conformance/aarm/
	$(BIN) aarm conformance --format json

conformance-markdown: gryph create_bin
	$(GO) test -c -o $(BIN_DIR)/gryph-conformance.test ./test/conformance/aarm/
	$(BIN) aarm conformance --format markdown

# Build for all platforms
build-all: create_bin
	GOOS=darwin GOARCH=amd64 $(GO) build ${GO_LDFLAGS} -o $(BIN_DIR)/gryph-darwin-amd64 ./cmd/gryph
	GOOS=darwin GOARCH=arm64 $(GO) build ${GO_LDFLAGS} -o $(BIN_DIR)/gryph-darwin-arm64 ./cmd/gryph
	GOOS=linux GOARCH=amd64 $(GO) build ${GO_LDFLAGS} -o $(BIN_DIR)/gryph-linux-amd64 ./cmd/gryph
	GOOS=windows GOARCH=amd64 $(GO) build ${GO_LDFLAGS} -o $(BIN_DIR)/gryph-windows-amd64.exe ./cmd/gryph
