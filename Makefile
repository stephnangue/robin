BINARY := robin
PKG := github.com/snangue/robin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.Date=$(DATE)

.PHONY: build test race cover vet lint tidy docker clean run-file

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/robin

test:
	go test ./...

race:
	go test -race ./...

cover:
	go test -coverprofile=cover.out ./... && go tool cover -func=cover.out

vet:
	go vet ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed; skipping"

tidy:
	go mod tidy

docker:
	docker build -t $(BINARY):$(VERSION) -f deploy/Dockerfile .

clean:
	rm -rf bin cover.out

run-file:
	go run ./cmd/robin
