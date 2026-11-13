# Design

## Context

See proposal.md for the motivation. What exists, as observed on 2026-11-13 at commit `79c10df` (a look before planning, not acceptance evidence; no receipt is recorded for it):

- **The tree already builds for macOS.** In the build image, `CGO_ENABLED=0 GOOS=darwin go build -tags "production purego"` succeeds for `amd64` and `arm64`, and `go vet ./...` passes for `darwin/arm64`. The window uses the toolkit's purego backend (`internal/gui/backend_darwin.go`), as the sibling goXterm ships it.
- **It runs on macOS 15.** On the owner's macOS 15.8.1 VM (x86_64, KVM), over SSH with a key: `version`, `doctor --json`, the MCP handshake, the password-argument refusal and the configuration commands of `build/qa/headless.sh` pass (with a stand-in for `timeout`, which macOS lacks); `--self-test` exits 0, so the window opened and the page called the backend. The two slices joined with `lipo` give one executable that reports `x86_64 arm64` and runs.
- **What the slices carry.** Both record a minimum macOS of 13.0. The arm64 slice has the linker's ad-hoc signature and the x86_64 slice has none. A signature belongs to a slice: whether the arm64 one survives the joining unchanged was not looked at (the VM inspects its own architecture) and is a check of the packaging program (D1).
- **What is missing on macOS.** `doctor` reports "no audio service is reachable"; the keyring, notification and control packages compile to their "other system" stand-ins (`keyring_other.go`, `notify_other.go`, `devices_other.go`, `endpoint_other.go`); the self-test says navigation is not guarded and listening sockets are not checked "on this platform"; the default folders are the XDG ones; nothing packages an application bundle.
- **Limits of that VM.** It has one sound output and no input; nobody is logged in at its console, and the login keychain refuses a remote shell ("User interaction is not allowed"); being x86_64, it cannot run the arm64 slice. A second macOS VM of the owner (macOS 26) has no system installed.

What the sibling projects recorded on the same VM (their own records, read on 2026-11-13; not re-run here):

- **goXterm** ships a universal application built the same way and checked it on this macOS 15.8.1 VM (its milestone M22): the application opens and its Keychain item is written and read. It also found that **macOS 15's Local Network privacy blocked destinations on the VM's own subnet for an application without a Developer ID signature**. A softphone's PBX is very often on the local network, so it is measured before any adapter is written (task 2.5), to state the limit exactly.
- goXterm keeps its secret in the login keychain through `/usr/bin/security -i`, the value written to the tool's standard input, and reads the peer of its control socket with `LOCAL_PEERCRED`.
- **kvm-gui**, which runs the VM, has an MCP server and commands for the VM's screen, keys and pointer; `agent-desktop` runs on macOS as well. Either can drive a window in a logged-in session.

Constraints that shape the approach: Docker-only builds with no host toolchain; cgo allowed only in `internal/platform/audio` and the Linux webview layer; `internal/core` free of OS APIs; one checked set of files per release, built once by CI; no secret, address or personal path in the repository; evidence before claims.

The plan was reviewed by Codex on 2026-11-13 in two rounds (`docs/reviews/add-macos-universal/`); the decisions below carry its findings, cited as F01 to F21 for the first round and N01 to N14 for the second.

## Goals / Non-Goals

**Goals:**

- One universal `.app` built on Linux in the existing image, with no cgo and no Apple tool.
- The missing adapters on macOS in pure Go, behind the existing ports, each keeping the whole contract the other systems keep, with their tests.
- Checks that run without a person: CI on the architectures GitHub hosts, and a target that drives a Mac over SSH.
- Documentation that says exactly what was run where.

**Non-Goals:**

- Developer ID signing and notarization (owner's decision of 2026-11-13: not needed), a `.dmg`, automatic updates, start at login. Signing later would not be one more packaging command: a hardened runtime needs its own entitlements and a new round of the privacy checks (F13).
- A navigation guard for the macOS web view. The specs of `migrate-to-go-wails` ask for blocked navigation; on a system whose web view gives no guard the content security policy alone confines the page, by the owner's decision of 2026-11-11, which was taken for Windows. Extending it to macOS is asked of the owner in task 1.1, and the self-test reports the two checks it cannot make instead of counting them as passed (F19, specs: Window confinement on macOS).
- Typing a password anywhere: no check logs in at the Mac's console, unlocks a keychain or authenticates SSH with a password.

## Decisions

### D1: Cross-build without cgo, join the slices in Go

`go-macos` builds `darwin/amd64` and `darwin/arm64` with `CGO_ENABLED=0 -tags "production purego"` and the release's `-ldflags`, then `build/macos/package` (a Go program started from `goXterm/build/macapp/main.go`) writes the fat Mach-O, `Info.plist`, `PkgInfo`, the `.icns` and the zip.

- Alternative: build on a macOS runner with cgo. Rejected: it breaks the Docker-only rule and "CI builds once what a release publishes" would span two machines.
- Alternative: osxcross with Apple's SDK in the image. Rejected: the SDK's licence does not allow it on non-Apple hardware, and nothing needs it while every adapter is pure Go.

The sibling's program checks little; this one checks its own output and has tests for each (F17): each input is a Mach-O executable of the expected architecture; the set is exactly `x86_64` and `arm64`; every slice's bytes are in the fat file unchanged, at an aligned offset within bounds, so the arm64 slice's signature is intact; the executable's mode in the zip; the plist's minimum version not below either slice's; the version and commit of both slices agree; the zip's file list. `build/qa/macos.sh`, the counterpart of `build/qa/appimage.sh`, asserts the same from outside, and the CI job on arm64 runs `codesign --verify` on the unpacked executable.

What joining the checked set means in the existing build, named so that no edit is left to chance (F16): `GO_PKGS` and the formatting paths in `build/go.mk` gain the packaging program; `scripts/go-licenses.sh` discovers the dependency graphs of both darwin slices as well; `build/dist-manifest.sh` lists the zip; the explicit copy list of `ci.yml` that fills `publish/` gains it; `release.yml`'s required files gain it. The publication scan already unpacks every `.zip`: it needs controls, not a new mechanism.

### D2: Bundle identity and declarations

Identifier `io.github.jniltinho.go-microsip` (the existing `appID`); executable `go-microsip`; `LSMinimumSystemVersion` taken from the slices (13.0 today) and never typed by hand; `NSMicrophoneUsageDescription`; `NSLocalNetworkUsageDescription` (a PBX on the local network); `CFBundleURLTypes` for `sip` and `tel`; `NSHighResolutionCapable`; no `LSUIElement` (the application is in the Dock while its window is open). The licence and the notices sit in `Contents/Resources`.

### D3: Unsigned, with the steps written down

The Go linker signs the arm64 slice ad hoc, which is what Apple Silicon requires to run it; the bundle is otherwise unsigned. A downloaded copy is quarantined, and macOS 15 no longer offers "Open" from the context menu: the guide gives the Privacy & Security path first and the `xattr -dr com.apple.quarantine` command for that one application, never a way to turn Gatekeeper off, and task 7.4 records what macOS actually shows.

- Alternative: a Developer ID signature and notarization. Rejected by the owner on 2026-11-13: no signature is needed, as in his other projects that publish Mac binaries.

What macOS withholds from unsigned code is not assumed in either direction (the review marks "unsigned code cannot use the local network" as wrong when said as a rule, and each of the microphone, the local network and notifications as "must be measured"). Each is measured from the packaged application started in a desktop session, never from a tool run over SSH, which macOS treats differently: the microphone and the local network before any adapter is written (tasks 2.4 and 2.5), notifications in 5.4. The work does not stop for a block: whatever limit is measured is stated first among the macOS limits in the guide and in the release notes, with any way around it that the measurement finds.

### D4: Sound through CoreAudio from Go

`internal/platform/audio/coreaudio_darwin.go`, through `purego` (already linked): AudioToolbox's AudioQueue for capture and playback, and the CoreAudio hardware property calls for the device list, the default devices, opening by device UID and the "device is gone" listener. The review calls AudioQueue "a reasonable experimental candidate, not yet a demonstrated fit"; the spike decides.

**Format.** The port and the rings carry 8 kHz mono 16-bit frames of 20 ms, and the engine has no resampler for a device's rate: on Linux and Windows the sound service converts (F03). The first candidate here is the same: the queues are opened at 8 kHz mono 16-bit and AudioQueue converts to and from the device. If it does not convert, or not well enough, conversion goes into the adapter, with separate input and output rates and their drift, and that cost is counted in the spike before anything else is built on it.

**Discard.** `MediaIO.Discard` is a privacy rule: nothing recorded during a mute or a hold may be sent afterwards, "however late the device delivers it". Clearing the ring is not enough when a buffer recorded earlier is still inside the queue (F02), and neither is a generation number read when a late callback arrives, nor a flush, which delivers what is buffered instead of dropping it (N01). The cutover is therefore the capture queue itself:

- Every capture queue the adapter opens is an instance with a number, carried to the callback as its user data. A buffer belongs to the instance that enqueued it, for good: its ownership is fixed when it is handed to the queue, not looked up when it comes back.
- The callback publishes to the ring under the adapter's lock and only if its instance is the current one. Check and publication are one critical section, so a callback cannot pass the check, pause, and write after the cutover.
- `Discard`, under the same lock, retires the current instance and empties the ring; then, outside the lock, it stops the old queue at once and disposes of it, and opens and starts a new one. Whatever the old queue still held, a partly filled buffer and the converter's history included, belongs to a retired instance and is dropped; a new buffer began after the new queue started. A callback of a retired instance does not hand its buffer back to the queue.
- It fails closed: if the new queue cannot be opened, capture is lost (`CaptureLost`) and nothing is sent; it is never the old queue kept running.
- The lock orders publication and nothing more. Three further properties are the implementation's to keep and the tests' to show on a real queue, not only with simulated callbacks: a retired callback never hands a buffer back, the state a callback uses stays alive until the old queue's disposal has returned, and a reopening still under way cannot bring capture back after `Close`.
- Alternative: one queue, reset in place, with a generation per buffer. Not the first candidate: a reset can itself invoke callbacks and re-enqueues, which is where the races are. The spike measures what reopening costs at an unmute; if that cost is not acceptable the alternative is designed with the same tests.

The tests are adversarial, not one delayed buffer: a callback held between its check and its publication (through a hook, which must show that the two cannot be separated), a buffer that spans the cutover, callbacks arriving while the old queue is disposed of, a failure to open the new queue, discards repeated and a discard racing `Close`, and fresh audio after the barrier arriving while every marked sample from before it stays absent from what the engine sends.

**The callback path.** Callbacks arrive on a thread AudioToolbox owns. What must be measured is the whole path, not the sample copy alone (F04): `purego`'s callback bridge takes a lock and builds its arguments by reflection, and the ring's own lock is on that path as well. The spike measures it under contention and garbage collection on both architectures, registers each callback once (the bridge's table is finite and never freed), and checks structure layouts, state kept alive across collections, no callback after a queue is disposed, and repeated open and close.

**What the spike must show** (task 2.3; a failure of any is a result, not a reason to relax the rule). The spike is a prototype of the adapter behind a build tag, packaged in the bundle so that the measurements of 2.4 to 2.6 can use it; it is not the production adapter, which group 3 writes with its tests: the adapter tests against a signal-injection and render-observation fixture that has to be written for macOS, since the existing fixtures are Linux's and Windows's (`pulse_linux_test.go`, `wasapi_windows_test.go`); callback buffers of any size, never assumed to be 160 samples; device rates of 44.1 and 48 kHz and different rates in and out; mono and stereo devices; the threshold over five minutes while the user interface and the consumer of events are stalled for five seconds and the media goes on (a stalled media consumer is the negative control and must fail); no microphone opened by `OpenOutput`, the ring or the ringback; and the measured delay from capture to RTP and from RTP to the speaker, which the loss-and-gap threshold does not bound. No ceiling for that delay exists in the project: the numbers are recorded beside the other systems', and a ceiling is the owner's to set (Open Questions).

If the spike fails, the fallback named by `migrate-to-go-wails` D4 (miniaudio, cgo) cannot be cross-built under D1, so the decision goes back to the owner instead of being taken silently.

**Two architectures, one machine.** The callback bridge and the structure layouts differ by architecture, and the Mac at hand is x86_64 (N03). The decision of task 2.7 is therefore staged: it is taken on the x86_64 measurements, and sound on arm64 stays unmeasured, and so unclaimed and closed to a final release, until a person runs the same fixture on an Apple Silicon Mac (task 2.8). The receipt names which device or fixture supplied each rate and channel combination.

**Permission.** macOS answers "not determined", "allowed", "denied" or "restricted", and asks the user asynchronously the first time (F06). The adapter requests access before the first capture; while the question is open the call has no capture and nothing recorded before the answer is sent, and a call that ends or an application that quits meanwhile leaves nothing waiting; "denied" and "restricted" become the denied-microphone error. That error's identity becomes neutral in `internal/core/ports` (today its text names Windows) and each system's wording is supplied where it is shown; a test of the window's notice, not only of the adapter's error, pins the Windows wording before the change. A command run from a terminal is attributed by macOS to the terminal application, and a raw test binary does not carry the bundle's declarations: permission checks run through the packaged application (D10).

**Losing a device.** The port reports a lost microphone and nothing else (`CaptureLost`); this change keeps that contract and adds nothing about the speaker or the device's name (F05).

### D5: The password in the login keychain

`internal/platform/keyring/keyring_darwin.go` follows what goXterm runs on this system (`internal/secret/secret_darwin.go` there): `/usr/bin/security -i`, started directly and never through a shell, a generic password with service `go-microsip`, the account being the random reference the application already generates, in the login keychain, named by its constant file name so that no path of the user's home is ever an operand (F08, N04).

Standard input of that tool is a command language with a parser of its own, not a shell's, so every operand written to it is inert by construction (F07, N04): the value is ASCII hex; the reference is checked against the alphabet the application generates and refused otherwise; the service and the keychain name are constants; nothing else varies. An empty value is refused before the tool is started. The whole encoded command is bounded and refused, not truncated, beyond the tool's input limit. One process runs one command and then sees the end of its input, so no later command can replace a failure's status; the verbose mode is never used; standard output and standard error are read apart, parsed for what that one operation returns, and never logged, since hex is reversible.

The code that builds the command and reads the result is ordinary Go with no build constraint, so its tests run in the build image against a stand-in for the tool; `keyring_darwin.go` only wires it to `/usr/bin/security`, and what needs the real tool runs on the Mac (N07). Tests cover quotes, backslashes, line breaks, Unicode and a long value in the password, a reference outside the alphabet, and look for the plain and the encoded marker in logs and in the argument lists of running processes.

Naming the keychain does not prove that the tool never falls back to another one (N05, from the tool's published source, not from a run). The check on the Mac is a negative control: with the login keychain unusable and a matching decoy item in another keychain, read, write and delete must fail and leave the decoy untouched. If the tool does not guarantee that, the fallback below replaces it.

Every operation is bounded by the five seconds the Linux adapter allows, and the results map to the existing typed errors: status 44 is "not found"; a locked keychain and one that refuses interaction are "locked" or "unavailable" as on Linux; the deadline is "timeout". The callers stay as they are: a keychain that cannot be read makes the application ask for the password and warn, it does not fail a command.

- Known property, stated in the guide: an item made by the tool is readable by the tool, so any process of the same user can read it without a prompt. That is the exposure the Secret Service has on Linux while the collection is unlocked.
- Alternative: Security.framework's item functions through `purego`. It is the fallback if a criterion of 4.3 fails, the decoy control first; from an unsigned application its access list would ask again after every update.
- Rejected in any form: the password as a command-line argument.

### D6: Control over a Unix-domain socket in Application Support

The Linux endpoint is more than private modes and a peer check (F09). What it does today, read from `endpoint_linux.go`: the control folder must be a directory of this user that nobody else can enter; the lock file is opened without following a link; the owner's lock is taken before a stale socket is removed, and only a socket of this user is removed, so a competing start never unlinks a live endpoint; a client refuses a socket that is not a socket of this user; and both ends verify the peer before a request or a capability passes. It does not inspect an existing lock file's owner or mode, nor a socket's mode bits at dialing (N08). That contract, exactly, is shared and not copied: the file becomes the Unix implementation with no change of behavior on Linux, and what differs is per system: the folder, the peer's credentials (`LOCAL_PEERCRED` for the user and `LOCAL_PEERPID` for the process on macOS, through `golang.org/x/sys/unix`, already a dependency; an unknown process id is the existing value 0), and the checks only macOS needs, which stay in the macOS file: the path limit below and access-control entries on the folder beside its mode bits. Hardening the shared part is not in this change; if a control shows it is needed, it is a change of Linux behavior, said as one and tested there. The Linux tests then run for both, with macOS controls for two starts at once, a crashed owner, a link in place of the folder or the lock, a folder with wider permissions or an access-control entry for someone else, and a second user.

The folder is `~/Library/Application Support/go-microsip/control`, as Windows uses `%LOCALAPPDATA%\go-microsip\control`: macOS has no `XDG_RUNTIME_DIR`.

- Alternative: the per-user temporary folder, which goXterm uses. Not taken here: macOS removes files left untouched there for days, and a softphone stays open for days.
- A socket path may hold 103 bytes on macOS with Go's socket code (F10). A longer one gets a plain error naming the limit; the test measures the whole encoded path, multibyte names included, and the endpoint's tests use short private folders, because macOS's temporary folders are long.

### D7: macOS folders in the core, as data

Configuration and database go under `$HOME/Library/Application Support/go-microsip`. `internal/core/config` and `internal/core/app` choose folders by `runtime.GOOS`; a macOS case is added to that choice and nothing else moves. A seam that takes the system's name was considered and dropped: selecting the Windows branch on Linux would still use Linux's path rules, so it would test less than it seems and touch code that works (N09). The macOS case is tested natively, by the cross-built test binary on the Mac; `APPDATA` against `LOCALAPPDATA`, the XDG precedence, the refusal of a relative base and `--config` are untouched and keep their tests. The core still imports no OS package. macOS was never released, so nothing migrates.

Two additions reach every system and are said as such, not hidden under "no change" (N10): `doctor` prints the default configuration path, the database and the control folder, which the specification promises, as new fields that existing readers ignore; the SSH target keeps them out of retained logs (D10). Its `audio.available` keeps its meaning, that the sound service answered: a Mac with no input reports zero inputs beside it, as Linux and Windows do today.

### D8: Window, menu bar, single instance, links

What the shared code already does stays where it is: `gui.go` creates the tray and its menu, hides the window on close where a tray exists, brings the window up for a call and sends Quit through the page's confirmation. No macOS copy of that is written (F14).

What is not wired yet, and is work:

- **Links.** On Linux and Windows a link arrives as an argument and `App.link` takes a number that was already parsed. On macOS a link arrives as an event of the running application, also at a cold start, before the page exists. The application subscribes to the toolkit's "opened with URL" event, passes the raw text through `domain.ParseDialLink`, which treats it as data and never lets a link choose the signaling server, and hands the number to the existing `link`, which already keeps the last one until the page is there. Tests: both schemes, a hostile link, a link at a cold and at a warm start, and a profile owned by the headless service.
- **Quit.** Command-Q and the application menu must take the page's Quit path, which asks before it ends a call, and the bounded shutdown when the PBX does not answer. Tested with no call, with a call, and with an unresponsive PBX.
- **Dock.** Clicking the Dock icon reopens a hidden window.
- **Single instance.** The existing `UniqueID`; checked for two starts and for two users of one Mac.

macOS decides which application opens `tel:` and `sip:`; go-microsip only declares itself able (D2). Where the user chooses it is recorded from the VM and written in the guide.

### D9: Notifications where macOS allows them

The port is a lifecycle, not one message (F15): `Incoming`, `Close` when the call is answered, refused or over, `Missed` replacing the ringing notification with one that stays, actions delivered once, a button of an old notification doing nothing after a restart, and none of it ever blocking. The macOS adapter keeps all of it or is not shipped.

`notify.New()` runs for every command, the ones without a window included. The macOS adapter therefore does nothing at construction: the notification centre, its delegate and its handlers are created at the first `Incoming`, on the thread the toolkit requires, and a refusal by macOS leaves the process alive and the window ringing. `version`, `doctor` and `mcp` are checked for not touching it.

First candidate: the user-notification centre through `purego`, which means Objective-C delegates and completion handlers written through it, more than posting a message. What an unsigned bundle gets on macOS 15 is measured (task 5.4). If macOS refuses it, no second mechanism is added: the window already comes to the front and rings, the specification allows that, and the guide says it.

- Alternative: `osascript display notification`. Rejected: no buttons, and it is attributed to Script Editor.

### D10: Three places where checks run

1. **CI** (`ci.yml`, job `macos`, after `build`, like `windows`): on GitHub's hosted macOS machines of each architecture it offers, download `dist`, verify the checksum, unpack, check the slices one by one, `build/qa/headless.sh` (made portable in group 1: no `timeout` dependency) and `--self-test`. The runner labels are looked up when the job is written, not assumed. A hosted runner is evidence that each slice executes and the window opens; it is not evidence for sound, for notification buttons or for what a user sees at a first start.
2. **A Mac over SSH** (`make go-mac`, `build/vm/mac.sh`), brought forward to the first group so every later task can use it. `GO_MAC_SSH` names the target and `GO_MAC_KNOWN_HOSTS` a host-key file that the operator made and verified, kept outside the repository and outside `artifacts/` (F11). The script accepts only that key (`StrictHostKeyChecking=yes`), only public-key authentication, never a prompt; it checks the target's syntax and passes it as separate arguments; and it retains only its own pass or fail lines and values it selects and validates against a pattern before printing (the macOS version, the architecture, checksums), never the raw output of `ssh`, `scp`, `doctor` or a remote command. The boundary is everything the run leaves or shows, not one log file (N06): the script's and Make's own standard output and error (recipes do not echo, no command tracing), every file under `artifacts/`, temporary files, copied reports and screenshots. Controls with a fictional address, user name and key search all of that after a success, after each kind of failure and after an interrupted run.
3. **A person at a Mac** (`docs/release/manual-checklist.md`): what needs hardware or a click: five-minute sound on each architecture, the microphone question, a headset, a Bluetooth headset that changes rate, the notification's buttons, a link, the quarantine steps, the Keychain from the window, sleep and wake.

Two rules for item 2. A check that touches a permission (microphone, local network, notifications) runs through the packaged application, opened in the desktop session with `open`, and is driven through the application's own commands and self-test, with `agent-desktop` on the Mac or kvm-gui's screen and keys for what needs a click; a cross-built test binary run over SSH is used only for what has no permission behind it (rings, the control endpoint, the keychain's parsing with a stand-in tool). And the session must already be logged in: the owner turns on automatic login for that test VM, or logs in before a run.

What the owner's VM cannot do today bounds item 2: a capture device has to be added to it (the VM's settings or a virtual loopback device) before any sound check means something there, and a virtual input proves nothing about a real headset. Task 1.3 settles each of these with the owner or moves the check to item 3.

### D11: Layering test

`internal/archtest` counts `unsafe`, `syscall` and `golang.org/x/sys` as system access and does not know `purego` (F21). It learns it: an import of `purego` is allowed where system access already is (the adapters under `internal/platform`, the control endpoint's per-system files, `internal/gui`) and fails anywhere else, with a control for each. No wider exemption is added and the core stays as it is.

### D12: The gate of a final release

`build/release/check-gates.sh` requires the tasks of `migrate-to-go-wails` alone, so a final version could publish a macOS file that no gate covers (F01). In the commit that adds the zip to the checked set, this change's name joins the list the checker requires for a final version, with a negative control: a final version's notes without every task of this change checked are refused. A pre-release may carry the zip earlier and its notes list what was not run. A task of this change is never checked by a sentence in the documentation: 3.6 needs the measured call, and 7.3 needs the manual rows passed, not merely filled: every mandatory row on each architecture has a passing result, and "not applicable" is allowed only for the notification's buttons when 5.4 recorded macOS's refusal, with the fallback shown to work (N13).

The checker already refuses any open checkbox; what the transition adds is the requirement that this change's task labels be present, so its control is notes with no open checkbox that leave a macOS task out, run with the production list of required changes, beside a complete positive case and an artifact set of the old shape without the macOS file (N12). The checker verifies labels and syntax, not the truth of a receipt.

One consequence for the owner to accept in task 1.1: the release workflow publishes one set, so from the transition commit on, a final version of the Linux and Windows files also waits for the macOS gates. Pre-releases do not. A final version wanted for Linux and Windows alone has to be cut before that commit.

### D13: A softphone that stays open

Not in the first plan and added by the review (F20). Measured before anything is built: whether a hidden or covered window lets macOS slow the application during a call or while it waits for one (the engine's timers and the audio thread), and what happens at sleep and wake. Only if the measurement shows a loss is a power assertion taken, for the length of a call. After a wake the engine registers again by its existing retry; the check is that it does, and how long it takes. Also checked, and documented as found: an incoming call arriving while the application is hidden and idle, the sound service restarting, another application taking the device, a Bluetooth headset switching profile and rate when its microphone opens, and incoming signaling and media with macOS's firewall on.

## Risks / Trade-offs

- [The AudioQueue path cannot meet the threshold, or the discard rule, from Go without cgo] → the spike comes before the adapters (task 2.3) and stops the change at a decision point instead of shipping sound that is silent or leaks a muted sentence.
- [macOS blocks an unsigned application from the local network, where the PBX usually is] → measured before any adapter is written (task 2.5), with signaling and media; the owner decided against signing (D3), so a block becomes the first stated limit of the macOS file.
- [The arm64 slice is never run with sound before release] → CI runs it headless and with the self-test where GitHub hosts arm64; sound on Apple Silicon is a row of the manual checklist, which gates a final release (D12).
- [An unsigned application is hard to open and may lose its permissions on update] → documented with the exact steps; accepted with the owner's decision against signing.
- [The purego window backend is younger than the cgo one] → the self-test runs in CI on every commit; a regression blocks the artifact.
- [A check leaks the VM's address or key into a log] → the script logs only what it selects, with leak controls (D10).
- [Checks on the VM depend on one machine of the owner] → the SSH target takes any Mac; nothing in the repository names the VM.
- [Hosted macOS runners change labels and versions] → the job pins labels, the receipt records the version each run had, and the guide claims only those.
- [The plan is larger than it looked: several ports have contracts the first draft summarized] → the tasks are split into fixtures and native acceptance, and each group lands its tests, its documentation and a receipt.

## Migration Plan

1. Ground: approval, the SSH target, the VM's capture device and session (group 1).
2. A minimal bundle and the measurements that can change the plan: sound with a prototype of the adapter, the microphone question, the local network, a hidden window and a sleep (group 2). Each ends in a recorded result or a question to the owner. Nothing is added to CI or to a release before them.
3. Adapters behind `GOOS=darwin` (groups 3 to 5); Linux and Windows artifacts and checks are unchanged, which `make check test dist go-smoke` shows after each group.
4. The macOS zip joins `go-dist`, CI's upload list, the release workflow's required files and the gate checker in one commit (group 6), so a tag never meets a set with one file more or less than the workflow expects. Until that commit the release workflow publishes the Linux and Windows set as today.
5. The first release that carries it is a pre-release whose notes say what was and was not run on macOS; a final one, of every system's files, waits for the gates (D12).

Rollback: remove the zip from `go-dist`, from the workflows' file lists and from the gate checker (one commit); the `_darwin` files are inert on the other systems.

## Open Questions

- A ceiling for the delay from microphone to network and from network to speaker: the project has a loss-and-gap threshold and no delay limit on any system. The spike records the numbers; whether a limit is set, and for which systems, is the owner's.
- Whether the owner's macOS 26 VM gets a system installed, to add a second verified version.
- Whether a `.dmg` is wanted beside the zip.
