# Proposal

## Why

The softphone is a Rust workspace with an egui/eframe window (release candidate `0.1.0-rc.3`). Every widget, dialog and theme is hand-drawn, so refining the interface or adding a skin costs bespoke drawing code per screen, and only three themes (system, light, dark) exist. The owner's other desktop products (`goXterm`, `kvm-gui`, `xenadmin/desktop-go`, `ad-manager`) have already moved to one stack — Go + Wails v3 with an embedded Vue/Vite/Tailwind frontend, one binary that is the desktop app, the CLI and the MCP server, and the retired Rust code kept under `archive/rust/`. Moving this project onto the same stack gives a richer, themeable UI, one toolchain to maintain across all the products, and a GUI-free core that a later macOS build can reuse.

## What Changes

- **BREAKING** — The product becomes `go-microsip`: a Rust-free Go reimplementation of the whole application (SIP, media, audio, configuration, storage, platform integration, CLI, MCP and UI). No Rust code runs in the Go binary (no FFI, no headless Rust process), and the project's own code is Go, kept free of cgo wherever that does not cost reliability: SQLite, SIP, media, secrets and the control transport use pure-Go libraries. Working audio takes priority over that preference, so the audio device adapter — and only it — may use cgo on an OS where the pure-Go backend does not meet the audio threshold. The other native dependency is the system webview the desktop toolkit links on Linux. The GitHub repository is renamed from `jniltinho/rust-microsip` to `jniltinho/go-microsip`; binary, package, desktop entry and configuration directory names follow.
- **Modular layout with a GUI-free core**: a portable `core` (domain, SIP engine, media, configuration, storage) that imports neither the desktop toolkit nor any OS-specific API; OS adapters (audio devices, secrets, notifications, tray, single instance, URI handling) behind core-owned interfaces; the Wails GUI, the CLI and the MCP server as front ends over the same command/event contract. The boundaries are enforced by an automated layering test.
- **Wails v3 desktop app** with an embedded Vue 3 + Vite Plus (`vp`) + Tailwind frontend replacing the egui window: every current screen (Dialpad, Calls, Contacts, in-call, incoming call, settings, grants/approvals, errors) reaches workflow parity, then is refined visually.
- **Theme system**: a skin registry with one stylesheet per skin sharing one token contract, shipping System, Light, Dark, Windows XP, macOS, Ubuntu and Windows 98, selectable live and persisted. Adding a skin is one stylesheet plus one registry entry.
- **Behavior parity** with the Rust candidate for telephony (one account, one call, UDP/TCP/TLS, registration, incoming/outgoing/reject/hangup, DTMF, mute, hold, DND, blind transfer), media (G.711, RTP, SDES-SRTP over validated TLS), configuration precedence, secrets handling, contacts/history, Linux and Windows integration, and the agent CLI/MCP control contract. Existing user data is brought over by an explicit, user-confirmed import that copies and never deletes.
- **Supported Linux baseline** (owner decision): Ubuntu 26.04 and later, and Debian 13, on x86_64. Ubuntu 24.04, the Rust candidate's target, is no longer a promised platform.
- **Milestone binaries**: every milestone produces `dist/` artifacts from Docker — Linux amd64 binary and `x86_64` AppImage, Windows amd64 `.exe` and `.zip`, with checksums. macOS is a later target that the core/adapter split prepares for.
- **Verification**: the existing disposable Asterisk Docker lab plus a FreePBX 17 interop target in a disposable VM; Linux desktop and Windows VMs (Vagrant/KVM via the local `kvm-gui`, or `dockur/windows`) on an isolated network; `agent-desktop` drives the built app and `agent-browser` drives the frontend; `codebase-memory` traces Rust-to-Go parity; `ai-memory` is consulted for prior decisions; **Codex reviews the proposal and each milestone read-only** with sanitized context.
- **Cutover**: after the recorded parity gate, the Rust workspace moves to `archive/rust/` as historical reference with no release line, and documentation, CI, release tooling, `AGENTS.md` and the OpenSpec project context are updated for Go.
- **Rust-era changes superseded**: `create-native-rust-linux-release`, `add-windows-port`, `refine-native-ui` and `add-agent-cli-mcp` stop receiving implementation work. Every requirement and scenario in their delta specs, pinned at the `0.1.0-rc.3` commit, is the parity baseline for this change: each one is either proven by go-microsip or dropped through a scope change the owner approves in writing. Their unfinished tasks are never marked complete. `add-web-wasm-softphone` is replaced by a Go web version (owner request of 2026-11-11): the same Vue frontend served by the Go binary, to be proposed as its own change on top of the core; this change keeps the frontend's backend access behind one module so the web build can reuse every screen.

### Non-goals

- New telephony features beyond the Rust candidate (multiple accounts or calls, conference, attended transfer, video, chat, BLF, TURN, Opus).
- A macOS build in this change; a Windows MSI installer (Windows ships as a portable `.exe`/`.zip` like the sibling projects).
- Pixel identity with the egui window: parity is workflow equivalence, and the look is deliberately redesigned.
- Publishing a final `v0.1.0` from Rust; the Go line continues as `0.2.0` pre-releases under the existing release gates.
- Touching the installed reference AppImage, personal settings, production PBXs or real credentials.

## Capabilities

### New Capabilities

- `modular-core`: the GUI-free, platform-neutral core and its contracts — layering rules, the command/event boundary shared by every front end, OS adapters behind interfaces, and telephony/media behavior parity proven by calls with real two-way audio.
- `desktop-app`: the single-binary desktop application — embedded web UI in a confined webview, the screens and their honest states, tray/notification/URI/single-instance integration, and the CLI and MCP modes that run without a window.
- `ui-themes`: the skin registry and token contract, the shipped skins (System, Light, Dark, Windows XP, macOS, Ubuntu, Windows 98), live switching, persistence and legibility rules.
- `implementation-migration`: the transition rules — coexistence with the Rust app, the explicit data import, the recorded parity gate, archiving Rust under `archive/rust/`, the repository rename, and the handling of the superseded Rust-era changes.
- `milestone-distribution`: Docker-only builds, the per-milestone `dist/` artifact set (Linux binary, AppImage, Windows `.exe`/`.zip`, checksums), the PBX and VM test matrix, and the independent Codex review of each milestone.

### Modified Capabilities

None. `openspec/specs/` is empty: no Rust-era change has been archived, so there is no main spec to modify. The Rust-era delta specs are referenced as the parity baseline by `modular-core`, `desktop-app` and `implementation-migration`.

## Impact

- **New code** at the repository root, beside the Cargo workspace until cutover (Go commands run with module mode forced, because the Rust `vendor/` directory would otherwise switch Go into vendor mode): `go.mod`, `main.go`, `cmd/` (CLI), `internal/core/…`, `internal/platform/…`, `internal/gui/`, `internal/mcp/`, `internal/archtest/`, `frontend/` (Vue SPA and `src/themes/`), `build/` (Docker image, AppImage and Windows packaging).
- **Unchanged until cutover**: `apps/`, `crates/`, `vendor/`, the Rust `Dockerfile`, `Makefile` targets and CI job keep building. At cutover they move to `archive/rust/`.
- **Dependencies** (pulled only inside the Docker build, licences recorded in the third-party notices; the project stays GPL-3.0-or-later): Go toolchain, Wails v3, Vue 3 + Vite + Tailwind, the `sipgo`/`diago` SIP stack and RTP/SRTP libraries, audio device backends per OS (pure Go preferred, cgo allowed for audio when needed), pure-Go SQLite (`modernc.org/sqlite`), TOML, Cobra and the Go MCP SDK. Candidates and the selection gate are in `design.md`.
- **User-visible renames**: binary `go-microsip`, configuration directory, desktop entry, keyring service, control socket and environment-variable prefix. Old data is imported on request, never moved or deleted.
- **Tooling and docs**: `Makefile`, CI and `release.yml`, `docs/` (architecture, development, release, receipts), `README.md`, `AGENTS.md`, the `create-release` skill and `openspec/config.yaml` are updated; `base/`, `dist/` and `artifacts/` stay local-only.
- **External action needing the owner's confirmation**: renaming the GitHub repository and updating the `origin` remote.
