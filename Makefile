.PHONY: test build clean

# Keep local verification builds identifiable without changing the Go fallback.
CLI_VERSION := $(shell cat VERSION)

test:
	go test ./...
	go vet ./...

build:
	go build -ldflags "-X github.com/starcat-app/starcat-cli/internal/mcp.Version=$(CLI_VERSION)" -o bin/starcat ./cmd/starcat

clean:
	go clean
