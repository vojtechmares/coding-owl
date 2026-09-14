VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/vojtechmares/coding-owl/internal/version.Version=$(VERSION)

.PHONY: build test lint generate desktop

build:
	go build -ldflags "$(LDFLAGS)" -o bin/owl ./cmd/owl

# The desktop app is a build target of its own (ADR-0009). It needs the Wails
# CLI, Node and pnpm; wails doctor says what is missing. The frontend build
# empties dist, so the placeholder that keeps go:embed satisfied on a fresh
# checkout is put back afterwards.
desktop:
	cd cmd/owl-desktop && wails build -clean -ldflags "$(LDFLAGS)"
	@touch cmd/owl-desktop/frontend/dist/.gitkeep

test:
	go test ./...

lint:
	go vet ./...
	test -z "$$(gofmt -l .)"
	buf lint

generate:
	buf generate
