.PHONY: build clean fmt vet test install

BIN_DIR ?= bin
VERSION ?= $(shell git describe --tags --dirty --always 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

LDFLAGS = -s -w \
          -X iosuite.io/internal/version.Version=$(VERSION) \
          -X iosuite.io/internal/version.Commit=$(COMMIT)

# Pure-Go build. CGO=0 is the whole point — the legacy CLI shipped a
# CGO bridge to a C++ inference lib that broke cross-compilation. The
# new shape subprocesses real-esrgan-serve, so the iosuite binary
# itself has no native dependencies.
build:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" \
	    -o $(BIN_DIR)/iosuite ./cmd/iosuite

clean:
	rm -rf $(BIN_DIR)

fmt:
	go fmt ./...

vet:
	go vet ./...

test:
	go test ./...

install: build
	install -m 0755 $(BIN_DIR)/iosuite /usr/local/bin/iosuite
