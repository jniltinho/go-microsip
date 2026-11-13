# Tasks

A task is checked only with evidence bound to a commit: a test and its run, a receipt under `docs/receipts/`, or a CI run. Nothing here is implemented yet.

## 1. Agreement and ground

- [ ] 1.1 Review this proposal with the owner and record the approval and the accepted assumptions (an unsigned zip, no `.dmg`; the minimum macOS the toolchain builds for, 13.0 today; verified only where a check ran: macOS 15 on the x86_64 VM, the hosted runners' versions); verify the record names each assumption and the date
- [ ] 1.2 Run the Codex review of the proposal, design, specs and tasks (read-only, sanitized context) and answer each finding; verify the report and the answers are under `docs/reviews/add-macos-universal/`
- [ ] 1.3 Settle with the owner what the macOS VM gets for the checks that need it: a capture device (the VM's settings or a virtual loopback device), a logged-in desktop session that the owner opens or sets to open by itself, and whether the macOS 26 VM is installed; verify the receipt lists each answer and that `ssh -o BatchMode=yes` to the address in `GO_MAC_SSH` prints the macOS version without a password prompt
- [ ] 1.4 Repeat the look of 2026-11-13 as a recorded run at one commit: both slices cross-built in the build image, the headless checks and `--self-test` on the VM; verify `docs/receipts/macos-ground-<date>.md` holds the commit, the commands, the macOS version and each result, with no address, user name or host key in it

## 2. The universal application

- [ ] 2.1 Add `build/macos/package` (from `goXterm/build/macapp`): the fat executable, `Info.plist` per design D2, `PkgInfo`, the icon, the licence and notices, zipped; verify its unit tests cover the fat header (two slices, alignment), the plist's keys, a minimum version taken from the slices, and a refusal of a slice that is not Mach-O
- [ ] 2.2 Add `make go-macos` and make it part of `go-dist`, the manifest and `SHA256SUMS`; verify on a host with only Docker that `make dist` leaves `go-microsip-X.Y.Z-macos-universal.zip`, that `sha256sum --check SHA256SUMS` passes and that the manifest names the file
- [ ] 2.3 Add `build/qa/macos.sh` and run it from `go-smoke`: the zip's file list, both architectures in the executable, the plist's declarations against the slices; verify it passes on the built zip and fails on a zip with one slice removed and on one with an extra file
- [ ] 2.4 Extend the notices, the SBOM and `build/release/scan-publication.sh` to the macOS zip (unpacked); verify the scan passes on the built set and that `ci-selftest.sh` has a control where a forbidden file inside the macOS zip fails it
- [ ] 2.5 Make `build/qa/check.sh` run `go vet` for the whole tree on `darwin/amd64` and `darwin/arm64`, and `build/qa/headless.sh` run without `timeout`; verify `make check` passes and that the headless script still passes on the Linux binary and the AppImage
- [ ] 2.6 Add the `macos` job to `ci.yml` on a hosted machine of each architecture (labels looked up then): checksum, unpack, `lipo -archs`, the headless script, `--self-test`; verify a green run of both on `main`, or the documented "built, not run" statement for an architecture GitHub does not host
- [ ] 2.7 Add the macOS zip to the files `release.yml` requires; verify with the workflow's own check run locally on a `dist` folder that a set without the zip is refused and a complete set passes

## 3. Sound

- [ ] 3.1 Spike CoreAudio from Go without cgo (design D4): list devices, open by UID, capture and play through the rings, on the VM with its capture device from 1.3; verify the shared adapter conformance test and the five-second stalled-consumer case pass there, that the callback path allocates nothing, and that the receipt records the measurements, or the failure and the question put to the owner
- [ ] 3.2 Implement `coreaudio_darwin.go` behind the audio port and remove macOS from `devices_other.go`; verify the adapter's tests pass on the Mac (cross-built test binary over SSH) and that `doctor --json` there reports audio available with its device counts
- [ ] 3.3 Report a device that goes away during a call without switching microphone; verify with the adapter's unplug test on the Mac and a lab call during which the capture device is removed: the call stays up and the window names the device
- [ ] 3.4 Read the microphone authorization and show the denied-microphone notice with macOS's setting name; verify by unit test for each authorization state and, on the Mac, that a call with the microphone denied shows the notice and one with it allowed sends audio
- [ ] 3.5 Run a five-minute call against the lab PBX from the Mac and record late-or-lost frames and the longest gap; verify the numbers are within the threshold in the receipt, or that the guide says sound on macOS is not verified and why
- [ ] 3.6 Document sound on macOS in `docs/README.md` (devices, the permission prompt, a terminal's attribution) and `docs/architecture.md`; verify every command the guide shows for macOS runs as written on the Mac

## 4. Keychain, control and folders

- [ ] 4.1 Add the macOS case to the default configuration and data folders in `internal/core` (design D7); verify by table tests for the folder chosen with and without `HOME`, that `internal/archtest` still passes, and that `doctor` on the Mac prints the Application Support paths
- [ ] 4.2 Implement `keyring_darwin.go` (design D5); verify the keyring's tests pass on the Mac in a logged-in session (set, get, delete, missing item), that a remote shell without the keychain gets the unavailable error, and that no test or log line contains a password
- [ ] 4.3 Implement `endpoint_darwin.go` (design D6): socket, owner lock, grant files, peer check; verify the control tests ported from Linux pass on the Mac, that a connection from a second user is refused, and that a path over the socket limit gives the documented error
- [ ] 4.4 Run the command line and the MCP server against a running window on the Mac; verify `status`, an unapproved `call dial` that waits for approval, and the MCP handshake with one tool call, recorded in the receipt
- [ ] 4.5 Document the Keychain, the folders and agent control on macOS in `docs/README.md` and `docs/agent-cli-mcp.md`; verify the test that checks documented commands against the command tree still passes

## 5. Window and desktop integration

- [ ] 5.1 Add the `_darwin` window glue (design D8): close hides to the menu bar, Command-Q and the application menu quit through the page's Quit, the Dock icon reopens the window; verify each on the Mac in a logged-in session, driven with `agent-desktop`, with a screenshot of the menu-bar item's menu in the receipt
- [ ] 5.2 Verify one instance per user on macOS: a second start brings the first window to the front and exits; verify on the Mac that one process remains
- [ ] 5.3 Handle `sip:` and `tel:` links from the bundle's declaration to the confirmation dialog; verify on the Mac that `open sip:600@pbx.invalid` reaches a running window and starts a stopped one, that nothing is dialed before confirmation, and record where macOS lets the user choose the application for these links
- [ ] 5.4 Spike the incoming-call notification for an unsigned bundle on macOS 15 (design D9) and implement it if macOS allows it; verify either the notify tests and a lab call whose Answer and Reject buttons act on the call, or the recorded refusal with the guide's sentence that macOS shows no notification for go-microsip
- [ ] 5.5 Run the skins and scale audits on the macOS window; verify the existing audits pass on screenshots taken on the Mac, or list each difference for the owner
- [ ] 5.6 Document the menu-bar item, links and notifications on macOS in `docs/README.md`; verify the text matches what 5.1 to 5.4 recorded

## 6. Checks on a Mac

- [ ] 6.1 Add `build/vm/mac.sh` and `make go-mac` (design D10): the zip and the cross-built test binaries copied over SSH with `BatchMode`, a host-key file under `artifacts/`, each check printing pass or fail; verify it stops when `GO_MAC_SSH` is unset, fails without prompting against a host that wants a password, and that its log contains no address, user name or key
- [ ] 6.2 Add a lab call to that target with the PBX on the host; verify a call both ways from the Mac to the lab with audio measured as packets, recorded with the zip's checksum
- [ ] 6.3 Add the macOS rows to `docs/release/manual-checklist.md`: five-minute sound on real hardware on Apple Silicon and on Intel, the microphone prompt, a headset, the notification's buttons, a link, the Keychain from the window; verify each row maps to a scenario of the specs
- [ ] 6.4 Record what macOS shows for a downloaded, quarantined copy and write the steps to open it in the guide; verify on the Mac, with a copy downloaded by a browser, that following the guide's steps opens the application

## 7. Integration and release

- [ ] 7.1 Update the platform tables and limits in `README.md`, `docs/README.md` and `docs/architecture.md`, and the build and release steps in `docs/DEVELOPMENT.md` and `docs/release.md`, claiming only what a receipt shows; verify each macOS claim links to its receipt and that items not run are listed as not verified
- [ ] 7.2 Run `make check test dist go-smoke` and the lab on the final tree; verify the Linux and Windows results are unchanged and the checksums of the full set are recorded
- [ ] 7.3 Run `make go-mac` on CI's files for one commit and the Codex review of the milestone; verify the receipt binds the run to the commit and the zip's checksum and that the review's findings are answered under `docs/reviews/add-macos-universal/`
- [ ] 7.4 Write the macOS part of the next release's notes: the file, that it is unsigned and not notarized, how to open it, what was and was not run on macOS; verify `build/release/notes.sh` accepts the notes and the publication scan passes

## Workflow follow-up

- Archive this change after its tasks are checked and the review requirements are met.
- A later change may add Developer ID signing and notarization if the owner decides for them.
