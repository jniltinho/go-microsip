# Developer guide

How to build, test and contribute to go-microsip. Project rules live in [`AGENTS.md`](../AGENTS.md); the [architecture](architecture.md) describes the layers; the [documentation index](README.md) lists the other records. The Rust application this one replaced is under [`archive/rust/`](../archive/rust/README.md) and is not built from here.

## Prerequisites

- Docker, make and git. Nothing else: Go and Node run inside the build image (`build/linux.Dockerfile`), never on the host. Do not install a host Go or Node toolchain for project builds.
- The lab targets also use Docker Compose and buildx (see the [lab's README](../docker/asterisk/README.md) for versions) and must run as an ordinary non-root user.
- The VM targets (`make go-vm`, `make go-freepbx`, `make go-measure`) are the only ones that need more than Docker: Vagrant and VirtualBox on the host.
- `base/` holds local-only references: never add it to Git, a build context or an artifact.

## Quick start

```sh
make image    # the build image, once (and again after build/linux.Dockerfile changes)
make check    # gofmt and go vet for every build
make test     # unit tests with the race detector
make dist     # dist/go: Linux binary, AppImage, Windows .exe and .zip, notices, SBOM, SHA256SUMS
```

`make help` lists every target. The root `Makefile` names the common ones without a prefix (`image`, `check`, `test`, `build`, `dist`, `lab`, `scan`); each is an alias of the `go-*` target in `build/go.mk`, which holds all the others.

## Docker workflow

`scripts/go-docker.sh` runs a command in the build image. Every recipe goes through it, and it can be used directly:

```sh
scripts/go-docker.sh go test ./internal/core/...
make go-shell                  # interactive shell in the container
```

- The checkout is mounted at `/src` and the container runs as your user. `base/` and `artifacts/` are hidden from the container by empty mounts.
- The image is Debian 13 pinned by digest, with Go, Node, the GTK 4 and WebKitGTK 6.0 headers, Xvfb, a session bus, PipeWire, GNOME Keyring and `appimagetool`. Debian 13 is the older of the two supported hosts, so a binary built there runs on both. The image's build context is `build/` alone, and `build/.dockerignore` sends none of it.
- **Build cache.** Go's build cache, the module cache and npm's cache persist between runs in the user's cache directory, in `go-microsip-build` (`~/.cache/go-microsip-build`). The wrapper creates it when it is missing.

| Variable | Default | Effect |
| --- | --- | --- |
| `GO_IMAGE` | `go-microsip-build:13` | Image tag to build and use. |
| `GO_CACHE` | the cache directory above | Where the caches are kept. |
| `GO_DOCKER_FLAGS` | empty | Extra `docker run` options (a network, a published port, `-it`). |
| `GOFLAGS` | `-buildvcs=false` | Passed to Go in the container. |
| `GO_WINDOW` | `purego` | Linux window backend of `make build`: `purego` loads GTK 4 and WebKitGTK 6.0 at run time and builds with `CGO_ENABLED=0`; `cgo` links them at build time. |
| `GO_DIST` | `dist/go` | Where artifacts are written. |

The version is `GO_VERSION` in `build/go.mk`; the binary gets it and the commit through `-ldflags`.

## Targets

**Checks and tests**

| Target | What it does |
| --- | --- |
| `make check` (`go-check`) | `build/qa/check.sh`: gofmt, then `go vet` for the CGO-free build with the window, the cgo build, the build without a window, Windows, and `internal/core` for macOS. |
| `make test` (`go-test`) | `go test -race -count=1` over `.`, `./cmd/...`, `./internal/...` and `./build/winicon`. Includes the layering rules. |
| `make go-frontend-test` | In `frontend/`: `vp check` (format, lint, types) and `vp test run`. |
| `make go-tidy` | `go mod tidy`: the one target that changes `go.mod` and `go.sum`. |

**Build and packaging**

| Target | What it does |
| --- | --- |
| `make go-frontend` | Builds the page (`npm ci`, `npm run build`) into `internal/gui/dist`, which the binary embeds. Compile-only targets drop a placeholder page there on a fresh checkout. |
| `make build` (`go-build`) | The Linux amd64 binary, `dist/go/go-microsip`. |
| `make go-licenses` | `scripts/go-licenses.sh`: `THIRD_PARTY_NOTICES.txt` (every Go module linked into the Linux or Windows binary, every npm package of the production bundle, every bundled font) and the same components as a CycloneDX bill of materials, `sbom.cdx.json`. A module without a license file fails it. |
| `make go-appimage` | `build/linux/appimage.sh`: the x86_64 AppImage with the binary, its desktop entry, icon and notices. GTK 4 and WebKitGTK 6.0 come from the system. |
| `make go-windows` | `build/windows/package.sh`: the Windows amd64 `.exe` (WebView2, cross-built without cgo, icon from `build/winicon`) and its portable `.zip`. |
| `make dist` (`go-dist`) | All of the above, plus `SHA256SUMS` and `build-manifest.json` (version, commit, whether the tree was dirty, window backend, image) from `build/dist-manifest.sh`. |

**Checks of the built binary** (scripts in `build/qa/`)

| Target | What it does |
| --- | --- |
| `make go-headless` | With no display: `version`, `doctor --json`, an MCP handshake on stdio, the refusal of a password-like option, and the configuration commands. |
| `make go-self-test` | Under Xvfb and a private session bus, `--self-test`: the window opens, the embedded page calls Go, and the page's confinement is reported. |
| `make go-smoke` | The two checks above on the Linux binary and the AppImage that `make dist` left, without building them again. CI runs it on the files it hands to the release. |
| `make go-offline-skins` | In a container with no network, the window starts in each skin with its bundled font; the built page is scanned for anything loaded from outside itself. `OUT=<dir>` keeps a picture of each skin. |
| `make go-keyring`, `make go-notify` | The keyring and notification adapters against a real service on a private session bus (GNOME Keyring; the test itself as the notification service), and against a session without one. |
| `make go-audio` | The audio adapter against PipeWire with virtual sound cards (`SECONDS=300` for the threshold run; 10 by default). |
| `make go-prints` | A screenshot of the window into `docs/prints/go/`. |

**Lab** (a disposable Asterisk PBX in Docker; see below)

| Target | What it does |
| --- | --- |
| `make lab` (`go-lab`) | `docker/asterisk/go-g0.sh`: the SIP/SRTP probes and the engine scenarios of `internal/g0`. `SCENARIO=engine/` runs the scenarios whose name starts with that prefix. |
| `make go-call` | `docker/asterisk/go-call.sh`: a call through the built binary (`go-microsip run --call 600`) with PipeWire's virtual sound cards, plain and secure. `SECONDS=300` for the threshold run. |
| `make go-window-call` | The same call placed from the window with the keyboard and the mouse, the page frozen for five seconds of it. |
| `make go-appimage-call` | The window call through the AppImage that `make dist` left, with screenshots of each step under the run directory's `out/`. |

**Virtual machines** (Vagrant and VirtualBox; scripts in `build/vm/`)

| Target | What it does |
| --- | --- |
| `make go-vm` | Checks what `make dist` left in `dist/go` on clean desktop VMs (`GO_VMS="ubuntu2604 debian13 windows11"`), writes the output to `artifacts/go-vm/` and destroys each VM (`KEEP=1` keeps them). `SECONDS` is the length of the sound measurement (300 by default). `GO_VM_LAB=1` also places calls from the Windows VM to a lab PBX on an auxiliary VM. |
| `make go-freepbx` | The lab's scenarios against FreePBX 17 installed in a VM, then again with the network impaired; output in `artifacts/go-freepbx/`. |
| `make go-measure` | Start-up time and memory in the kept desktop VMs (`make go-vm KEEP=1` first), beside the Rust application: it copies that application's AppImage and `.exe` from `dist/` and fails without them. |

## The lab

Calls are tested against a disposable Asterisk on an internal Docker network with fictional accounts: [docker/asterisk/README.md](../docker/asterisk/README.md) describes the PBX, its dialplan and its fixtures (much of that page is about the Rust probes that preceded this application). Never point a test at a real PBX or account.

```sh
sh docker/asterisk/lab.sh build    # the lab image, once
make lab                           # every scenario
make lab SCENARIO=engine/hold      # one group
make go-call SECONDS=300
```

Each run generates new fictional accounts and a new three-day lab CA under the git-ignored `artifacts/` (`artifacts/go-g0/<time>/`, `artifacts/go-call/<time>/`), builds its runner in the build image, mounts only those disposable paths, and tears the PBX down when it ends. The report of `make lab` is `out/report.json` in the run directory. The lab targets share one Compose project, so run one at a time.

The VMs of `make go-vm` are the two supported distributions with their default GNOME desktop and nothing installed for go-microsip, and Windows 11. They use VirtualBox NAT; with `GO_VM_LAB=1` the PBX runs on an auxiliary VM on a VirtualBox internal network that the host has no adapter on. Supported Linux targets are Ubuntu 26.04 and later and Debian 13.

## Repository layout

```
main.go             wires the operating-system adapters into the front ends
cmd/                the command line (Cobra), one file per command
internal/           the core, the platform adapters, the window, control, MCP and the layering test
frontend/           the page the window shows: Vue 3, Vite Plus, Tailwind; src/themes/ holds the skins
third_party/diago/  the vendored SIP dialog and media library, with marked local changes
build/              build image, go.mk, qa/ checks, linux/ and windows/ packaging, release/ tooling, vm/
scripts/            go-docker.sh (the container wrapper), go-licenses.sh (notices and SBOM)
docker/asterisk/    the disposable Asterisk lab
docs/               guides, ADRs, receipts, reviews, migration records, screenshots
openspec/           proposals, designs, specifications and task lists
archive/rust/       the Rust application this one replaced: historical reference, not built
.github/workflows/  ci.yml and release.yml
.claude/, .agents/  skills and commands for coding agents
```

`base/`, `dist/` and `artifacts/` are local-only and git-ignored: references, build output and run directories are never committed. [architecture.md](architecture.md) lists the packages under `internal/`.

`third_party/diago` is diago v0.42.0 copied without its examples and tests; `go.mod` replaces the upstream module with it. Every local change is marked `go-microsip:` in the source and listed in [its README](../third_party/diago/README.go-microsip.md), which also says how to update it. Its one test of its own (`TestSameMedia`) is not run by `make test`.

## How tests are organised

- **Unit tests** (`make test`) run with the race detector and need no display, sound device, session bus or network: the core's tests use the in-memory ports of `internal/core/ports/memory` (a tone generator for a microphone, a tone meter for a speaker). The tests in `cmd/` also check that every `go-microsip` command written in `README.md` and `docs/agent-cli-mcp.md` is a command of this build.
- **The layering test** (`internal/archtest`, part of `make test`) parses the imports of every Go file of the project and fails on a broken rule; each rule has a deliberate violation that must fire. A second test there checks that the skins, the page's registry and the configuration name the same skins with the same tokens. The rules are in [architecture.md](architecture.md#layers).
- **Adapter tests** that need a service are skipped by `make test` and run by their own targets: `make go-keyring`, `make go-notify`, `make go-audio`.
- **Frontend tests** (`make go-frontend-test`): the reducer, the mock backend's grants and the skins' token contract, plus the format, lint and type checks.
- **Lab scenarios** (`make lab`) drive the signaling endpoint and the engine against the PBX; `make go-call` adds real sound through the built binary.
- **The window walkthrough** (`make go-window-call`, `make go-appimage-call`) places a lab call from the built window.
- **VM checks** (`make go-vm`, `make go-freepbx`) run the artifacts on clean desktops and against FreePBX.

A screenshot, a generated interface or a review is not call-testing evidence. Receipts of executed runs are under [receipts/](receipts/).

## Frontend

`make go-frontend` builds the page the binary embeds; `make go-frontend-test` checks it. A development server with a mock backend and fictional data also runs in the build image:

```sh
GO_DOCKER_FLAGS="-p 127.0.0.1:5173:5173" scripts/go-docker.sh \
  sh -c 'cd frontend && npm ci --no-audit --no-fund && npx vp dev --host 0.0.0.0 --port 5173'
```

Do not run `make go-frontend` while it is up: `npm ci` replaces `node_modules` under it. [ui/go-frontend.md](ui/go-frontend.md) has the mock's scenarios, the screen checks and the contract between the page and Go; [themes.md](themes.md) lists every file a new skin touches.

## CI and releases

`.github/workflows/ci.yml` runs on `main` and on pull requests, with no secrets: the publication scan with its negative controls (`build/release/ci-selftest.sh`, `build/release/check-gates-selftest.sh`), then `make image`, `check`, `test`, `go-frontend-test`, `dist`, a check of the checksums and the manifest, `make go-smoke`, a scan of the artifacts, and a smoke of the `.exe` and the `.zip` on a Windows runner. The lab, the VMs and calls with real sound are not part of it.

The artifacts CI builds from a commit on `main` are the ones `.github/workflows/release.yml` publishes for a tag on that commit: the release workflow builds nothing. It requires a green CI run for the commit, a tag equal to `GO_VERSION`, the owner's approval and, for a final version, every gate checked (`build/release/check-gates.sh`). The process is in [release.md](release.md).

## Project rules

- English for code, comments, CLI messages, documentation and OpenSpec tasks.
- New code is Go. Exported Go APIs are documented and errors are typed. Never log a secret.
- Tests use fictional data and a disposable PBX, never production calls or credentials. Personal settings and an installed AppImage are real data: leave them alone.
- Requirements and decisions live in [`openspec/`](../openspec/changes/migrate-to-go-wails/design.md); a task is checked only with verifiable evidence.
- No absolute personal home path in documentation or examples: use repository-relative paths or neutral placeholders.

## Before pushing

```sh
make scan                  # the Git index and the whole history
make scan SCAN=dist/go     # also an artifact folder
```

`build/release/scan-publication.sh` checks what a push publishes: forbidden paths (`base/`, `artifacts/`, `dist/`, env files, databases, AppImages, keys, captures), private keys, tokens, literal passwords in TOML, the lab's generated passwords and personal home paths. A finding prints a path, a category and a count, never the match. It runs on the host with git; scanning an artifact folder also uses `python3` and `unsquashfs`, since AppImages and zips are unpacked, not run.
