# hcli — Hetzner Cloud + Robot CLI
#
#   make build        local binary ./hcli with version metadata
#   make install      go install into $(go env GOBIN) / $(go env GOPATH)/bin
#   make test         go test -race ./...
#   make lint         golangci-lint run
#   make fmt / vet    gofmt -w / go vet
#   make cross        all six release targets into dist/<os>_<arch>/
#   make completions  completion scripts into completions/
#   make dist         release archives + SHA256SUMS, as the CI builds them
#   make clean

GO      ?= go
BINARY  := hcli
PKG     := github.com/digiogithub/hcli/internal/version

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)

TARGETS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

EXE := $(shell $(GO) env GOEXE)

export CGO_ENABLED ?= 0

.PHONY: all build install test lint fmt fmt-check vet cross completions dist clean

all: build

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY)$(EXE) .

install:
	$(GO) install -trimpath -ldflags "$(LDFLAGS)" .

# -race needs cgo.
test:
	CGO_ENABLED=1 $(GO) test -race ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -s -w .

fmt-check:
	@out="$$(gofmt -s -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	$(GO) vet ./...

cross:
	@set -e; for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; ext=""; [ "$$os" = windows ] && ext=.exe; \
		echo "==> $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/$${os}_$${arch}/$(BINARY)$$ext .; \
	done

completions: build
	@mkdir -p completions
	./$(BINARY)$(EXE) completion bash > completions/$(BINARY).bash
	./$(BINARY)$(EXE) completion zsh > completions/$(BINARY).zsh
	./$(BINARY)$(EXE) completion fish > completions/$(BINARY).fish
	./$(BINARY)$(EXE) completion powershell > completions/$(BINARY).ps1

# Same archives as the release workflow (unsigned when built locally).
dist:
	VERSION=$(VERSION) COMMIT=$(COMMIT) GO=$(GO) scripts/release.sh stage $(TARGETS)
	scripts/release.sh archive
	scripts/release.sh sums

clean:
	rm -rf dist completions $(BINARY) $(BINARY).exe coverage.out
