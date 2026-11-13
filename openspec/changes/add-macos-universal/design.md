# Design

## Context

See proposal.md for the motivation. What exists, as observed on 2026-11-13 at commit `79c10df` (a look before planning, not acceptance evidence; no receipt is recorded for it):

- **The tree already builds for macOS.** In the build image, `CGO_ENABLED=0 GOOS=darwin go build -tags "production purego"` succeeds for `amd64` and `arm64`, and `go vet ./...` passes for `darwin/arm64`. The window uses the toolkit's purego backend (`internal/gui/backend_darwin.go`), as the sibling goXterm ships it.
- **It runs on macOS 15.** On the owner's macOS 15.8.1 VM (x86_64, KVM), over SSH with a key: `version`, `doctor --json`, the MCP handshake, the password-argument refusal and the configuration commands of `build/qa/headless.sh` pass (with a stand-in for `timeout`, which macOS lacks); `--self-test` exits 0, so the window opened and the page called the backend. The two slices joined with `lipo` give one executable that reports `x86_64 arm64` and runs.
- **What the slices carry.** Both record a minimum macOS of 13.0. The arm64 slice has the linker's ad-hoc signature; the x86_64 slice and the joined file have none.
- **What is missing on macOS.** `doctor` reports "no audio service is reachable"; the keyring, notification and control packages compile to their "other system" stand-ins (`keyring_other.go`, `notify_other.go`, `devices_other.go`, `endpoint_other.go`); the self-test says navigation is not guarded and listening sockets are not checked "on this platform"; the default folders are the XDG ones; nothing packages an application bundle.
- **Limits of that VM.** It has one sound output and no input; nobody is logged in at its console, and the login keychain refuses a remote shell ("User interaction is not allowed"); being x86_64, it cannot run the arm64 slice. A second macOS VM of the owner (macOS 26) has no system installed.

Constraints that shape the approach: Docker-only builds with no host toolchain; cgo allowed only in `internal/platform/audio` and the Linux webview layer; `internal/core` free of OS APIs; one checked set of files per release, built once by CI; no secret, address or personal path in the repository; evidence before claims.

## Goals / Non-Goals

**Goals:**

- One universal `.app` built on Linux in the existing image, with no cgo and no Apple tool.
- The four missing adapters on macOS in pure Go, behind the existing ports, each with the tests the other systems have.
- Checks that run without a person: CI on both architectures, and a target that drives a Mac over SSH.
- Documentation that says exactly what was run where.

**Non-Goals:**

- Developer ID signing, notarization, a `.dmg`, automatic updates, start at login.
- A navigation guard for the macOS web view: as on Windows, the content security policy alone confines the page (owner's decision of 2026-11-11 for a platform without a guard); the self-test keeps saying so.
- Typing a password anywhere: no check logs in at the Mac's console, unlocks a keychain or authenticates SSH with a password.

## Decisions

### D1: Cross-build without cgo, join the slices in Go

`go-macos` builds `darwin/amd64` and `darwin/arm64` with `CGO_ENABLED=0 -tags "production purego"` and the release's `-ldflags`, then `build/macos/package` (a Go program adapted from `goXterm/build/macapp/main.go`, about 190 lines there) writes the fat Mach-O, `Info.plist`, `PkgInfo`, the `.icns` and the zip. It becomes part of `go-dist`, so the manifest, `SHA256SUMS`, the notices, the SBOM and the scan cover it.

- Alternative: build on a macOS runner with cgo. Rejected: it breaks the Docker-only rule and "CI builds once what a release publishes" would span two machines.
- Alternative: osxcross with Apple's SDK in the image. Rejected: the SDK's licence does not allow it on non-Apple hardware, and nothing needs it while every adapter is pure Go.

The packaging program checks its own output: both slices present and page-aligned, the plist's minimum version not below either slice's, and the zip's file list. A check script (`build/qa/macos.sh`, the counterpart of `build/qa/appimage.sh`) asserts the same from outside.

### D2: Bundle identity and declarations

Identifier `io.github.jniltinho.go-microsip` (the existing `appID`); executable `go-microsip`; `LSMinimumSystemVersion` taken from the slices (13.0 today) and never typed by hand; `NSMicrophoneUsageDescription`; `CFBundleURLTypes` for `sip` and `tel`; `NSHighResolutionCapable`; no `LSUIElement` (the application is in the Dock while its window is open). The licence and the notices sit in `Contents/Resources`.

### D3: Unsigned, with the steps written down

The Go linker signs the arm64 slice ad hoc, which is what Apple Silicon requires to run it; the bundle is otherwise unsigned. A downloaded copy is quarantined, and macOS 15 no longer offers "Open" from the context menu: the guide gives the `xattr -dr com.apple.quarantine` command and the Privacy & Security path, and task 6.4 records what macOS actually shows. Consequence to verify and document: macOS ties the microphone permission to the code's identity, so an unsigned application may be asked again after each update.

- Alternative: sign and notarize from Linux (`rcodesign`). Deferred, not rejected: it needs the owner's Apple Developer Program membership and credentials in CI. See Open Questions.

### D4: Sound through CoreAudio from Go

`internal/platform/audio/coreaudio_darwin.go`, through `purego` (already linked): AudioToolbox's AudioQueue for capture and playback at the device's rate, and the CoreAudio hardware property calls for the device list, the default devices, opening by device UID and the "device is gone" listener. Callbacks only copy samples through the existing preallocated single-producer/single-consumer rings; resampling and the 20 ms tick stay in the engine, as on the other systems (`migrate-to-go-wails` D4, which named "CoreAudio through purego" for this later change).

This is unproven until its spike (task 3.1) passes the shared adapter conformance test and the five-second stalled-consumer case: in particular that a callback from the audio thread enters Go without cgo, and without allocating on that path. If it fails, the fallback of D4 there (miniaudio, cgo) cannot be cross-built under D1, so the decision goes back to the owner instead of being taken silently.

The microphone permission is read through AVFoundation's authorization status before a capture opens: "denied" becomes the existing denied-microphone notice (`audio/denied.go`), worded for macOS's setting. A command run from a terminal is attributed by macOS to the terminal application; the guide says so.

### D5: The password in the login keychain

`internal/platform/keyring/keyring_darwin.go` calls Security.framework's generic-password item functions through `purego`: service `go-microsip`, account as on the other systems, the item accessible when the keychain is unlocked. Errors map to the existing typed ones: `errSecInteractionNotAllowed` and a locked keychain to "unavailable", a missing item to "not found".

- Alternative: the `security` command. Rejected: writing would put the password on a command line, which the project forbids.

### D6: Control over a Unix-domain socket in Application Support

`internal/control/endpoint_darwin.go`: the socket, the owner's lock and the grant files in `~/Library/Application Support/go-microsip/control`, directory `0700`, files `0600`, the peer's user id read from the socket and compared with the process's own. It mirrors the Windows layout (`%LOCALAPPDATA%\go-microsip\control`) because macOS has no `XDG_RUNTIME_DIR`.

- Alternative: the per-user temporary folder. Rejected: macOS may clean it while the application runs.
- The path of a Unix socket is limited to 104 bytes on macOS: a home folder long enough to exceed it gets a plain error naming the limit, and a test covers it.

### D7: macOS folders in the core, as data

`internal/core/config` and `internal/core/app` already take the system's name and the environment as values and choose `APPDATA`/`LOCALAPPDATA` for Windows. They gain a macOS case: configuration and database under `$HOME/Library/Application Support/go-microsip`. The core still imports no OS package. macOS was never released, so nothing migrates; `--config` keeps working as everywhere.

### D8: Window, menu bar, single instance, links

All through the toolkit, which already provides them on macOS: the system tray is the menu-bar item (the code in `gui.go` is shared; only the `_linux` host detection is Linux's), the single instance uses the existing `UniqueID`, and a link reaches the application as the toolkit's "opened with URL" event, which feeds the existing confirmation dialog. `internal/gui` gets a small `_darwin` file for what differs: closing hides to the menu bar, Command-Q and the application menu quit through the page's own Quit, and the Dock icon reopens the window.

macOS decides which application opens `tel:` and `sip:`; go-microsip only declares itself able (D2). Where the user chooses it is recorded from the VM and written in the guide.

### D9: Notifications where macOS allows them

First candidate: the user-notification centre through `purego`, with the Answer and Reject actions the other systems have. It is known to refuse applications without a usable signature on some versions, so the spike (task 5.4) records what an unsigned bundle gets on macOS 15. If it is refused, no second mechanism is added: the window already comes to the front and rings, the specification allows that, and the guide says it.

- Alternative: `osascript display notification`. Rejected: no buttons, and it is attributed to Script Editor.

### D10: Three places where checks run

1. **CI** (`ci.yml`, job `macos`, after `build`, like `windows`): on GitHub's hosted arm64 and x86_64 macOS machines, download `dist`, verify the checksum, unpack, `lipo -archs`, `build/qa/headless.sh` (made portable: no `timeout` dependency) and `--self-test`. The runner labels are looked up when the job is written, not assumed; if one architecture has no hosted machine, the specification's "architecture not available" scenario applies.
2. **A Mac over SSH** (`make go-mac`, `build/vm/mac.sh`): `GO_MAC_SSH` names the target (`user@host[:port]`); `BatchMode=yes`, a host-key file under the git-ignored `artifacts/`, and no other credential. It copies the zip and test binaries cross-built with `GOOS=darwin go test -c` (the pattern of `go-vm`), runs the headless checks, the self-test and the adapters' tests, and a call against the lab PBX started on the host (`lab.sh host-up`).
3. **A person at a Mac** (`docs/release/manual-checklist.md`): what needs hardware, a logged-in session or a click: five-minute sound, the microphone prompt, a headset, the notification's buttons, a link, the quarantine steps, and the Keychain from the window.

What the owner's VM cannot do today bounds item 2: a capture device has to be added to it (the VM's settings or a virtual loopback device) before any sound check means something there, and keychain and notification checks need a logged-in session that an agent must not create by typing a password. Task 1.3 settles each of these with the owner or moves the check to item 3.

### D11: Layering test

`internal/archtest` gains the new files in its list of places where raw memory and system calls are allowed (`_darwin` files of the adapters and of the control endpoint), and a control proving that a `purego` or `unsafe` import elsewhere still fails.

## Risks / Trade-offs

- [The AudioQueue path cannot meet the threshold from Go without cgo] → the spike comes first (task 3.1) and stops the change at a decision point instead of shipping silent sound.
- [The arm64 slice is never run with a window or sound before release] → CI runs it headless and with the self-test on a hosted arm64 machine; sound on Apple Silicon stays "not verified" in the guide until the manual checklist is filled on one.
- [An unsigned application is hard to open and loses its microphone permission on update] → documented with the exact steps; signing is an open question for the owner.
- [The purego window backend is younger than the cgo one] → the self-test runs on both architectures in CI on every commit; a regression blocks the artifact.
- [Checks on the VM depend on one machine of the owner] → the SSH target takes any Mac; nothing in the repository names the VM.
- [Hosted macOS runners change labels and versions] → the job pins labels, the receipt records the version each run had, and the guide claims only those.

## Migration Plan

1. Spikes first: sound (3.1) and notifications (5.4); each ends in a recorded result or a question to the owner.
2. Adapters and packaging land behind `GOOS=darwin`; Linux and Windows artifacts and checks are unchanged, which `make check test dist go-smoke` shows after each group.
3. The macOS zip joins `go-dist`, CI and the release workflow in one commit, so a tag never sees a set with five files and a workflow that expects six, or the reverse.
4. The first release that carries it says in its notes what was and was not run on macOS.

Rollback: remove the zip from `go-dist` and from the workflows' file lists (one commit); the `_darwin` files are inert on the other systems.

## Open Questions

- Developer ID signing and notarization: whether the owner wants them, with which Apple account, in a later change.
- Whether the owner's macOS 26 VM gets a system installed, to add a second verified version.
- Whether a `.dmg` is wanted beside the zip once the application is signed.
