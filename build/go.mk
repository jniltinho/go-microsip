# Go application targets (openspec/changes/migrate-to-go-wails), included by the top-level Makefile.
# Every recipe runs in the build image through scripts/go-docker.sh: the host needs only Docker and
# make. The image's build context is build/ alone. Go packages are named explicitly so nothing under
# base/, vendor/ or archive/ is ever compiled.

GO_IMAGE   ?= go-microsip-build:13
GO         := GO_IMAGE=$(GO_IMAGE) scripts/go-docker.sh
GO_PKGS    := . ./cmd/... ./internal/...
GO_VERSION ?= 0.2.0-g0
GO_COMMIT  := $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
GO_LDFLAGS := -s -w -X main.version=$(GO_VERSION) -X main.commit=$(GO_COMMIT)
# production: Wails never swaps the embedded page for a dev server named in the environment.
GO_TAGS    := -tags production
GO_DIST    ?= dist/go
GO_BIN     := $(GO_DIST)/go-microsip

.PHONY: go-image go-tidy go-embed-stub go-check go-test go-frontend go-build go-shell

go-image: ## Go: build the pinned build image (Debian 13, Go, Node, GTK4/WebKitGTK)
	docker build -t $(GO_IMAGE) -f build/linux.Dockerfile build

go-tidy: go-embed-stub ## Go: resolve modules and write go.sum
	GOFLAGS=-mod=mod $(GO) go mod tidy

# The binary embeds internal/gui/dist: a fresh checkout has no frontend build yet, so compile-only
# targets drop a placeholder page there (git-ignored; `make go-frontend` replaces it).
go-embed-stub:
	@test -e internal/gui/dist/index.html || { mkdir -p internal/gui/dist && echo '<!doctype html><title>go-microsip</title>' > internal/gui/dist/index.html; }

go-check: go-embed-stub ## Go: gofmt and go vet
	$(GO) sh -c 'test -z "$$(gofmt -l main.go cmd internal)" || { gofmt -l main.go cmd internal; echo "run gofmt -w"; exit 1; }; go vet $(GO_PKGS)'

go-test: go-embed-stub ## Go: unit tests with the race detector (includes the layering rules)
	$(GO) go test -race -count=1 $(GO_PKGS)

go-frontend: ## Go: build the Vue/Vite/Tailwind frontend into internal/gui/dist
	$(GO) sh -c 'cd frontend && npm ci --no-audit --no-fund && npm run build'

go-build: go-frontend ## Go: the Linux amd64 binary into dist/go/
	mkdir -p $(GO_DIST)
	$(GO) go build $(GO_TAGS) -trimpath -ldflags "$(GO_LDFLAGS)" -o $(GO_BIN) .

go-shell: ## Go: interactive shell in the build container
	GO_DOCKER_FLAGS=-it $(GO) bash
