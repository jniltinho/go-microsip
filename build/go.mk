# Go application targets (openspec/changes/migrate-to-go-wails), included by the top-level Makefile.
# Every recipe runs in the build image through scripts/go-docker.sh: the host needs only Docker and
# make. The image's build context is build/ alone. Go packages are named explicitly so nothing under
# base/, vendor/, third_party/ or archive/ is ever compiled as part of this project.

GO_IMAGE   ?= go-microsip-build:13
GO         := GO_IMAGE=$(GO_IMAGE) scripts/go-docker.sh
GO_PKGS    := . ./cmd/... ./internal/... ./build/winicon
GO_VERSION ?= 0.3.0-m3
GO_COMMIT  := $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
GO_LDFLAGS := -s -w -X main.version=$(GO_VERSION) -X main.commit=$(GO_COMMIT)
GO_DIST    ?= dist/go
GO_BIN     := $(GO_DIST)/go-microsip
GO_NAME    := go-microsip-$(GO_VERSION)
# The Linux window backend: "purego" (default) loads GTK 4 and WebKitGTK 6.0 at run time and builds
# with CGO_ENABLED=0; "cgo" links them at build time. Both need the same system libraries to run.
# "production": Wails never swaps the embedded page for a dev server named in the environment.
GO_WINDOW  ?= purego
ifeq ($(GO_WINDOW),purego)
GO_LINUX   := CGO_ENABLED=0 go build -tags "production purego"
else
GO_LINUX   := CGO_ENABLED=1 go build -tags production
endif
# The window checks need WebKit's own sandbox (bubblewrap): user namespaces, an unmasked /proc and
# this user's passwd entry for the session bus.
GO_GUI_FLAGS := --init --security-opt seccomp=unconfined --security-opt apparmor=unconfined \
              --security-opt systempaths=unconfined -v /etc/passwd:/etc/passwd:ro -v /etc/group:/etc/group:ro
GO_GUI     := GO_DOCKER_FLAGS="$(GO_GUI_FLAGS)" $(GO)

.PHONY: go-image go-tidy go-embed-stub go-check go-test go-frontend go-build go-headless go-self-test \
	go-frontend-test go-offline-skins go-appimage go-windows go-licenses go-dist go-prints go-audio go-lab go-call go-window-call go-appimage-call \
	go-vm go-shell

go-image: ## Go: build the pinned build image (Debian 13, Go, Node, GTK4/WebKitGTK)
	docker build -t $(GO_IMAGE) -f build/linux.Dockerfile build

go-tidy: go-embed-stub ## Go: resolve modules and write go.sum
	GOFLAGS=-mod=mod $(GO) go mod tidy

# The binary embeds internal/gui/dist: a fresh checkout has no frontend build yet, so compile-only
# targets drop a placeholder page there (git-ignored; `make go-frontend` replaces it).
go-embed-stub:
	@test -e internal/gui/dist/index.html || { mkdir -p internal/gui/dist && echo '<!doctype html><title>go-microsip</title>' > internal/gui/dist/index.html; }

go-check: go-embed-stub ## Go: gofmt, and go vet for the CGO-free, cgo, no-window and Windows builds
	$(GO) build/qa/check.sh $(GO_PKGS)

go-test: go-embed-stub ## Go: unit tests with the race detector (includes the layering rules)
	$(GO) go test -race -count=1 $(GO_PKGS)

go-frontend: ## Go: build the Vue / Vite Plus / Tailwind frontend into internal/gui/dist
	$(GO) sh -c 'cd frontend && npm ci --no-audit --no-fund && npm run build'

go-frontend-test: ## Go: the page's format, lint and type checks and its tests (reducer, skins, no raw HTML rendering)
	$(GO) sh -c 'cd frontend && npm ci --no-audit --no-fund && npx vp check && npx vp test run'

go-build: go-frontend ## Go: the Linux amd64 binary into dist/go/ (GO_WINDOW=purego|cgo)
	mkdir -p $(GO_DIST)
	$(GO) sh -c '$(GO_LINUX) -trimpath -ldflags "$(GO_LDFLAGS)" -o $(GO_BIN) .'

go-headless: go-build ## Go: version, doctor and an MCP handshake with no display
	$(GO) build/qa/headless.sh $(GO_BIN)

go-self-test: go-build ## Go: the window opens under Xvfb and the page calls the backend
	$(GO_GUI) build/qa/self-test.sh $(GO_BIN)

go-offline-skins: go-build ## Go: the window starts in every skin in a container with no network
	GO_DOCKER_FLAGS="--network none $(GO_GUI_FLAGS)" $(GO) build/qa/offline-skins.sh $(GO_BIN) $(OUT)

go-prints: go-build ## Go: screenshots of the window into docs/prints/go/
	$(GO_GUI) build/qa/prints.sh $(GO_BIN) docs/prints/go

go-audio: ## Go: the audio adapter against PipeWire with virtual sound cards (SECONDS=300 for the G0 run)
	$(GO_GUI) build/qa/audio.sh $(SECONDS)

go-call: ## Go: a lab call through the binary with PipeWire's virtual sound cards (SECONDS=300 for the threshold run)
	sh docker/asterisk/go-call.sh $(SECONDS)

go-window-call: go-frontend ## Go: a lab call placed from the window with the keyboard and the mouse, the page frozen for 5 s of it
	sh docker/asterisk/go-call.sh $(or $(SECONDS),10) window

go-appimage-call: ## Go: the same call placed from the AppImage that go-dist left, with screenshots of each step
	sh docker/asterisk/go-call.sh $(or $(SECONDS),10) window $(GO_DIST)/$(GO_NAME)-x86_64.AppImage

go-lab: ## Go: the SIP/SRTP probes and the engine scenarios against the disposable Asterisk lab (SCENARIO=prefix)
	sh docker/asterisk/go-g0.sh $(SCENARIO)

go-licenses: go-frontend ## Go: third-party notices of the binary and the embedded frontend
	mkdir -p $(GO_DIST)
	$(GO) scripts/go-licenses.sh $(GO_DIST)/THIRD_PARTY_NOTICES.txt

# The AppImage: the binary, its desktop entry, icon and notices; GTK 4 and WebKitGTK 6.0 come from
# the system.
go-appimage: go-build go-licenses ## Go: the x86_64 AppImage
	$(GO) build/linux/appimage.sh $(GO_BIN) $(GO_DIST) $(GO_NAME)

# Windows amd64: WebView2, cross-built without cgo.
go-windows: go-frontend go-licenses ## Go: the Windows amd64 .exe and .zip
	$(GO) build/windows/package.sh $(GO_DIST) $(GO_NAME) "$(GO_LDFLAGS)"

# dist/go/: every artifact of this commit, with SHA256SUMS and a manifest. Run at each milestone.
go-dist: go-appimage go-windows ## Go: Linux binary, AppImage, Windows .exe and .zip, notices, SHA256SUMS
	cp $(GO_BIN) $(GO_DIST)/$(GO_NAME)-linux-amd64
	build/dist-manifest.sh $(GO_DIST) $(GO_NAME) $(GO_VERSION) $(GO_COMMIT) $(GO_WINDOW) $(GO_IMAGE)

# The one target that needs more than Docker on the host: Vagrant and VirtualBox. It checks what
# go-dist left in dist/go; the audio adapter's test is built for the guests here.
GO_VMS    ?= ubuntu2604 debian13 windows11
GO_VM_OUT ?= artifacts/go-vm
go-vm: ## Go: the artifacts of go-dist on clean desktop VMs (GO_VMS="ubuntu2604 debian13 windows11", SECONDS=300)
	$(GO) sh -c 'mkdir -p $(GO_DIST)/g0 && CGO_ENABLED=0 go test -c -o $(GO_DIST)/g0/audio.test ./internal/platform/audio && \
		CGO_ENABLED=0 GOOS=windows go test -c -o $(GO_DIST)/g0/audio.test.exe ./internal/platform/audio && \
		CGO_ENABLED=0 GOOS=windows go test -c -o $(GO_DIST)/g0/lock.test.exe ./internal/platform/lock'
	mkdir -p $(GO_VM_OUT)
	build/vm/run.sh $(GO_VERSION) "$(abspath $(GO_VM_OUT))" $(or $(SECONDS),300) $(GO_VMS)

go-shell: ## Go: interactive shell in the build container
	GO_DOCKER_FLAGS=-it $(GO) bash
