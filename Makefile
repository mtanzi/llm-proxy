VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build test lint check-imports vet

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/llm-proxy ./cmd/llm-proxy

vet:
	go vet ./...

test:
	go test ./...

lint:
	golangci-lint run ./...

check-imports:
	./scripts/check-imports.sh
