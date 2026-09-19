VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/vojtechmares/coding-owl/internal/version.Version=$(VERSION)

.PHONY: build test lint generate desktop desktop-fake

build:
	go build -ldflags "$(LDFLAGS)" -o bin/owl ./cmd/owl

# The desktop app is a build target of its own (ADR-0009). It needs the Wails
# CLI, Node and pnpm; wails doctor says what is missing. The frontend build
# empties dist, so the placeholder that keeps go:embed satisfied on a fresh
# checkout is put back afterwards.
desktop:
	cd cmd/owl-desktop && wails build -clean -ldflags "$(LDFLAGS)"
	@touch cmd/owl-desktop/frontend/dist/.gitkeep

# The same app over made-up state, for looking at the design without queueing
# real work (#36). VITE_FAKE puts a stand-in where the Wails bindings look for
# the daemon; without it the fake is not in the bundle at all. The build is
# left where `make desktop` leaves its own, so run that again before shipping
# anything from bin/.
desktop-fake:
	cd cmd/owl-desktop && VITE_FAKE=1 wails build -clean -ldflags "$(LDFLAGS)"
	@touch cmd/owl-desktop/frontend/dist/.gitkeep
	@echo "Built with fake data. open cmd/owl-desktop/build/bin/owl-desktop.app"

test:
	go test ./...

lint:
	go vet ./...
	test -z "$$(gofmt -l .)"
	buf lint

# The generated protobuf code, and the JSON schemas of the configuration files
# that the website publishes (issue #129). CI runs both again and fails on a
# difference, so a struct that changes without its schema being regenerated is
# caught in the pull request rather than by a user whose editor is quietly
# wrong.
generate:
	buf generate
	go run ./internal/config/schemagen
