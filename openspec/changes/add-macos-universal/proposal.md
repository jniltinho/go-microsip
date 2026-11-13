# Proposal

## Why

go-microsip is published for Linux and Windows only; macOS was left as "a later target that the core/adapter split prepares for" (`migrate-to-go-wails`). The owner now asks for a macOS universal version. A first look on 2026-11-13 shows the gap is the platform adapters, not the window: the application already cross-builds for macOS without cgo and its window opens on macOS 15, but it has no sound, no place for the password, no control transport and no package there (design.md, Context).

## What Changes

- **A universal macOS application**: `go-microsip.app` holding one executable with an arm64 and an x86_64 slice, published as `go-microsip-X.Y.Z-macos-universal.zip` beside the Linux and Windows files, built in the same Docker image without cgo and without Apple tools, named by `SHA256SUMS`, the build manifest, the notices and the bill of materials.
- **Sound on macOS**: an audio adapter for CoreAudio behind the existing port (device list, open by ID, capture and playback, a device that goes away), under the same threshold as the other systems, and a notice when macOS denies the microphone.
- **The password in the Keychain**: a keyring adapter for the login keychain, with the same errors as the other systems when it is locked or absent.
- **Agent control on macOS**: the control transport (same-user Unix-domain socket, one owner, grant files) so the command line and the MCP server reach the window as on Linux.
- **Desktop integration**: a menu-bar item with the tray's menu, an incoming-call notification with Answer and Reject where macOS allows it for an unsigned application, `sip:` and `tel:` links, one instance per user, and the macOS folders for the configuration and the data.
- **Checks**: a macOS job in CI on each architecture GitHub hosts for the published zip, `make` targets that run the checks over SSH on a Mac named by the environment (the owner's macOS VM), and macOS rows in the manual checklist.
- **Documentation and release**: the platform tables, the guide's install and limits sections, and the release workflow's list of required files.

Not in this change:

- A Developer ID signature and notarization: the owner decided on 2026-11-13 that none is needed, as in his other projects that publish Mac binaries. The application is unsigned beyond the linker's ad-hoc signature, the guide says how to open it, and what macOS withholds from unsigned code is measured and stated. A `.dmg`, the Mac App Store, automatic updates and start at login are out as well.
- Any change to what go-microsip does on Linux and Windows, to the SIP/media engine, or to the skins, with one stated exception: `doctor` prints three more paths on every system.
- A claim of support for a macOS version or an architecture that no check ran on.

## Capabilities

### New Capabilities

- `macos-integration`: what go-microsip does on macOS that depends on the system: sound devices and the microphone permission, the password in the Keychain, the agent-control transport, the menu-bar item, notifications, `sip:`/`tel:` links, the single instance and the folders it uses.
- `macos-distribution`: the universal application as a release artifact: how it is built, what the bundle holds, how it is signed and opened, which checks run on it and on which machines, and what the documentation may claim.

### Modified Capabilities

None. The capabilities of `migrate-to-go-wails` are not archived yet, so `openspec/specs/` holds no main spec to modify; its requirement "macOS artifacts SHALL NOT be claimed until a macOS build is delivered by a later change" is met by this change's `macos-distribution`, not edited. Its rule that the web interface blocks navigation cannot be kept on macOS as on Linux: `macos-integration` states the macOS rule and says that it qualifies that one, and the owner is asked to accept it in task 1.1.

## Impact

- **Code**: new `_darwin` files in `internal/platform/audio`, `internal/platform/keyring`, `internal/platform/notify`, `internal/control` and `internal/gui`; a packaging tool under `build/macos/` started from `goXterm/build/macapp`. Shared code changes too, with no change of behavior on Linux and Windows: the Linux control endpoint becomes the Unix one with per-system pieces; the choice of the default folders in `internal/core/config` and `internal/core/app` gains a macOS case; the denied-microphone error loses its Windows wording in the core, which each system now supplies where it is shown; `doctor` prints the paths it uses; `internal/archtest` learns that `purego` is system access.
- **Build and CI**: `build/go.mk` (`go-macos`, later part of `go-dist`; `GO_PKGS`), `build/qa/check.sh` (`go vet` for the whole tree on macOS, not only the core), `build/qa/headless.sh`, `build/dist-manifest.sh`, `scripts/go-licenses.sh`, the controls of `build/release/ci-selftest.sh`, `build/release/check-gates.sh` (this change's tasks gate a final version that carries the macOS file), `.github/workflows/ci.yml` (a macOS job and the upload list) and `release.yml` (one more required file).
- **Dependencies**: none new is expected: `github.com/ebitengine/purego` is already linked by the window; any library a spike selects is pinned, licence-checked and added to the notices.
- **Documentation**: `README.md`, `docs/README.md`, `docs/architecture.md`, `docs/DEVELOPMENT.md`, `docs/release.md`, `docs/release/manual-checklist.md`.
- **Machines**: the owner's macOS VM, reached over SSH with a key, its address given by an environment variable and never written in the repository; GitHub's hosted macOS runners.
- **Order**: this change builds on the Go application at the root. It does not wait for `migrate-to-go-wails` to be archived and does not check any of its tasks.
