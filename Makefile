VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/vojtechmares/coding-owl/internal/version.Version=$(VERSION)

.PHONY: build test lint generate

build:
	go build -ldflags "$(LDFLAGS)" -o bin/owl ./cmd/owl

test:
	go test ./...

lint:
	go vet ./...
	test -z "$$(gofmt -l .)"
	buf lint

generate:
	buf generate
