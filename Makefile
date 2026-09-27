GO ?= go
NPM ?= npm
PKG := github.com/uchabokeria/autokey
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X $(PKG)/cmd.version=$(VERSION)

.PHONY: all build build-static vet lint fmt test test-all web web-dev dashboard clean install

all: build

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o autokey .

build-static:
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o autokey .

vet:
	CGO_ENABLED=0 $(GO) vet ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -l . && gofumpt -l .

test:
	$(GO) test ./...

test-all:
	$(GO) test ./... && cd web && $(NPM) run build

web:
	cd web && $(NPM) install && $(NPM) run build

web-dev:
	cd web && $(NPM) run dev

# Serve the dashboard locally (vite proxy -> :8766).
dashboard: build
	./autokey dashboard

install: build-static
	install -m 0755 autokey /usr/local/bin/autokey

clean:
	rm -f autokey
	rm -rf internal/dashboard/dist
