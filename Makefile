VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build test lint check-imports check-openapi vet goldens

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/llm-proxy ./cmd/llm-proxy

vet:
	go vet ./...

test:
	go test ./...

lint:
	golangci-lint run ./...

check-imports:
	golangci-lint run --enable-only depguard ./...

# Lint the OpenAPI document with a pinned linter. Needs Node; CI runs it.
check-openapi:
	npx --yes @redocly/cli@1.34.5 lint --config api/redocly.yaml api/openapi.yaml

# Rewrite the golden call records under testdata/fixtures. Read the diff.
goldens:
	go test ./internal/family/anthropic -run 'TestFixtureGoldens|TestScannerGolden' -update
