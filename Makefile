GO := go
BIN_DIR := bin
BIN := $(BIN_DIR)/gryph
SHELL := /bin/bash
GITCOMMIT := $(shell git rev-parse HEAD)
VERSION := "$(shell git describe --tags --abbrev=0 2>/dev/null || echo v0.0.0)-$(shell git rev-parse --short HEAD)"

GO_CFLAGS=-X 'github.com/safedep/gryph/internal/version.Commit=$(GITCOMMIT)' -X 'github.com/safedep/gryph/internal/version.Version=$(VERSION)'
GO_LDFLAGS=-ldflags "-w $(GO_CFLAGS)"

BENCHTIME ?= 50x

.PHONY: all deps generate generate-schema verify-schema gryph clean test conformance conformance-json conformance-markdown bench-hook redteam

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

# Hook latency benchmark through the real binary. Writes a Markdown report
# with the percentiles of each benchmark.
bench-hook:
	GRYPH_PERF_REPORT=$(CURDIR)/perf-reports/hook-latency.md $(GO) test -tags perf -run '^$$' -bench . -benchtime=$(BENCHTIME) -count=1 ./test/perf/

# Red-team suite: every bypass of the threat model against the protected
# paths under each provider, and the overhead of each provider on the hook
# path. As root it also runs the fanotify column. Writes a Markdown report.
redteam:
	GRYPH_REDTEAM_REPORT=$(CURDIR)/perf-reports/redteam.md $(GO) test -tags redteam -count=1 -v ./test/redteam/

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
