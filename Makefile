GO      ?= go
BIN     := bin/commhub
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# CGO_ENABLED=0 is not an optimisation. It is enforced so a dependency that
# needs cgo fails the build loudly, instead of silently producing a binary that
# cannot cross-compile.
export CGO_ENABLED=0

.PHONY: build test vet fuzz lint golden run clean check licenses snapshot

build:
	$(GO) build -trimpath -ldflags="-s -w -X github.com/Neha611/commhub/internal/cli.Version=$(VERSION)" -o $(BIN) ./cmd/commhub

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

# The sanitiser is the boundary between remote text and the terminal, so it is
# fuzzed rather than only example-tested.
fuzz:
	$(GO) test ./internal/safe -run=Fuzz -fuzz=FuzzTextIsAlwaysRenderSafe -fuzztime=60s

golden:
	UPDATE_GOLDEN=1 $(GO) test ./internal/app -run TestGoldenDashboard

check: vet test

run: build
	./$(BIN)

licenses:
	./scripts/gen-third-party-licenses.sh

snapshot:
	goreleaser release --snapshot --clean --skip=publish

clean:
	rm -rf bin dist
