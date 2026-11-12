# Design

## Context

See `proposal.md` — Why. The current product is a Cargo workspace (about 39k lines of Rust): `crates/core` (domain, state machine, `Command`/`Event`, `MediaIo`), `crates/telephony` (engine on rsipstack), `crates/media` (G.711, RTP, RFC 4733, SRTP), `crates/audio` (cpal), `crates/config` (six-layer resolution), `crates/storage` (SQLite), `crates/platform` and `crates/platform-windows` (single instance, secrets, notifications, tray, URI), `crates/control` (agent IPC) and `apps/rust-microsip` (egui window, CLI, MCP). `docs/architecture.md` already describes a UI-replaceable design: a front end sends `Command` and receives `Event`. That contract is what the Go rewrite keeps.

Constraints that shape the approach:

- User decisions: everything moves to Go (no FFI, no headless Rust process); Wails for the GUI; the layout and theme system follow `goXterm`, `kvm-gui`, `xenadmin/desktop-go` and `ad-manager`; a `core` module that a later macOS build reuses; Rust archived only after completion; Codex validates the proposal and the implementation.
- Project rules that stay: Docker-only builds, `base/` local-only, fictional data and a disposable PBX, release requires real two-way audio, tags only on green CI, English artifacts, no personal home paths in published files.
- Owner preference recorded during planning: keep everything in Go where possible — `sipgo`/`diago` as the SIP base, `modernc.org/sqlite` for storage — but audio must work: cgo is accepted in the audio adapter whenever it is needed for reliable audio.
- Supported Linux desktops, by owner decision: Ubuntu 26.04 and later, and Debian 13 (x86_64). The Linux artifacts are built on the older of the two, Debian 13, so their C library and WebKitGTK requirements are met on both; older distributions are not promised.
- The sibling projects pin Wails `v3.1.0`, Vue 3, Vite Plus and Tailwind 4, `modernc.org/sqlite`, Cobra and `modelcontextprotocol/go-sdk`; this project uses the same versions unless the G0 spike finds a blocker.
- No Rust-era change is archived, so `openspec/specs/` is empty; the Rust-era delta specs are the parity baseline.

## Goals / Non-Goals

**Goals:**

- A layout the owner already maintains elsewhere, so the four products share build, packaging and skin conventions.
- A core that compiles with no GUI and no OS adapter, proven by a layering test.
- A media path whose real-time behavior does not depend on the webview.
- A go/no-go spike before any screen is built, because the SIP/SRTP/audio stack is the main risk, not the UI.
- Verifiable milestones, each ending with `dist/` artifacts, a Codex review and a status report.

**Non-Goals (design-level):**

- Byte-compatible reuse of Rust runtime state (locks, sockets, keyring entries); data crosses over by import only.
- Reusing the egui snapshot tests; the Go app gets its own screenshot tooling.
- A second audio or SIP backend "for flexibility"; one backend per concern, behind an interface.
- Changing the agent control contract's semantics (grants, approvals, audit); it is re-implemented, not redesigned.

## Decisions

### D1: Rust-free Go rewrite at the repository root, Rust beside it until cutover

The Go module (`github.com/jniltinho/go-microsip`) lives at the root next to the Cargo workspace: `go.mod`, `main.go`, `cmd/`, `internal/`, `frontend/`, `build/`. No Go path collides with `apps/` or `crates/`. The Rust `vendor/` directory does matter: with a `vendor/` directory at the module root the Go command selects vendor mode and fails on the missing `vendor/modules.txt`. Until cutover every Go invocation (Makefile, Docker image, CI) therefore sets `GOFLAGS=-mod=readonly`, and a check task proves that both the Go and the Rust workflows still pass side by side; after cutover `vendor/` is gone from the root and the flag is dropped. "Rust-free" is completed by "cgo only where audio needs it": every project package except `internal/platform/audio` and the Linux webview layer of `internal/gui` (the navigation guard needs the WebKitGTK C API, as in the sibling projects) is free of cgo (D4, D8), and the core, CLI, MCP and control packages compile with `CGO_ENABLED=0`. Complete application artifacts are validated with each OS's selected build settings instead. Native code in an artifact is limited to what Wails links for GTK/WebKitGTK on Linux and, on an OS where G0 selects it, the audio library. During coexistence the Go build uses `build/linux.Dockerfile` and `make go-*` targets; the Rust `Dockerfile`, `Makefile` targets and CI job stay as they are. At cutover a single commit moves the Rust tree to `archive/rust/` (as in `goXterm/archive/rust`) and the `go-*` targets become the default names.

- Alternative: a `desktop-go/` subdirectory moved to the root later (what goXterm did). Rejected: a second mass move and a module-path change for no benefit, since nothing collides here.
- Alternative: keep the Rust engine and put Wails on top through FFI or the existing control socket. Rejected by the user; it also keeps two toolchains and makes Windows/macOS cross-builds depend on the Rust ABI.

### D2: Package layout and layering

```
main.go                     mode dispatch: GUI by default, CLI/MCP otherwise
cmd/                        Cobra commands (doctor, config, import, call control, mcp)
internal/core/              portable, GUI-free
  domain/                   account, dial target, DTMF, states, Command/Event (no I/O)
  sip/                      registration, dialogs, transfer, reconnection
  media/                    G.711, RTP, RFC 4733, SRTP/SDES, jitter buffer
  engine/                   one actor: owns sip + media, serializes state changes
  config/                   flags > env > .env > TOML > SQLite > defaults, provenance, doctor
  store/                    SQLite schema/migrations, settings, history, contacts, CSV
  ports/                    interfaces: AudioDevices, SecretStore, Notifier, Tray, Instance, Links
  app/                      facade: builds engine/config/store from ports; Command in, Event out, read queries
internal/platform/          OS adapters implementing core/ports (build-tagged _linux/_windows/_darwin)
  audio/                    device I/O, one backend per OS; may use cgo (D4)
internal/control/           same-user local transport, grants, approvals, audit
internal/mcp/               stdio MCP server over internal/control
internal/gui/               Wails app, bound services, tray, embedded dist
internal/archtest/          layering, skin-token and notices tests
frontend/                   Vue SPA; src/themes/ holds the skins
build/                      Docker image, AppImage, Windows resources
```

`internal/archtest` (copied in spirit from `kvm-gui/internal/archtest/layers_test.go`) parses imports and fails when: anything outside `internal/gui` imports Wails; `internal/core/...` imports `internal/gui`, `internal/platform`, `internal/control`, `internal/mcp`, `cmd`, or names a build-tagged OS package; `internal/core/domain` imports anything that does I/O; any project file outside `internal/platform/audio` and `internal/gui` contains `import "C"`; or a front end (`cmd`, `internal/gui`, `internal/mcp`, `internal/control`) imports a core package other than `core/domain`, `core/ports` and `core/app`, or an audio/SIP/SQL/keyring library directly. Front ends get settings, contacts and history through `core/app` queries, never through `core/store`. Each rule has a deliberate-violation test. Only `main.go` wires `internal/platform` adapters into `core/app`. `GOOS=darwin go vet ./internal/core/...` runs in CI as the "future macOS" proof.

- Alternative: a flat `internal/core` as in kvm-gui. Rejected: SIP, media, config and store have different dependency sets, and the Rust layering test already separates them.

### D3: SIP and media stack — sipgo and diago as the base, confirmed by the G0 spike

Base chosen with the owner: `github.com/emiago/sipgo` (BSD-2-Clause, `v1.7.0` at planning time) for transactions and transports, and `github.com/emiago/diago` (MPL-2.0, `v0.42.0`) on top of it for dialogs and RTP. `github.com/pion/rtp` + `github.com/pion/srtp` (MIT) cover packets and SRTP wherever diago's media layer does not meet a criterion. All are compatible with GPL-3.0-or-later. `github.com/Gaurav-Gosain/sip` was also suggested and checked: despite the name it serves terminal (Bubble Tea) applications in a browser and has no SIP telephony, so it is not used. `github.com/emiago/gophone` (a CLI softphone by the sipgo/diago author) was checked too: its repository publishes only binaries and a README that states it is not open source, so there is no code to build on and nothing of it is linked or shipped. It is still useful in two ways: its feature list (register, dial, answer, RFC 4733 DTMF, blind transfer, SRTP) is an indication — not proof — that diago can carry a softphone, and its headless binary may serve as an optional scripted second endpoint in the lab (D12). Its README also ships a separate no-cgo build without audio, which suggests device audio is outside diago and supports giving go-microsip its own audio adapters (D4). Library capability claims are not taken from memory: the spike pins versions, reads their documentation, and proves each criterion against the lab.

G0 acceptance, all against the Asterisk lab with fictional extensions:

1. Register over UDP, TCP and TLS with a custom CA bundle replacing system roots. Negative proofs: a certificate from an unknown CA is refused; a certificate for the wrong host name is refused; a certificate that chains to a system-trusted root but not to the configured bundle is refused.
2. Outgoing and incoming call with measured two-way G.711 audio in both codecs.
3. SDES-SRTP `AES_CM_128_HMAC_SHA1_80` over TLS with SRTCP. Negative proofs by packet injection in the lab: a replayed SRTP packet, an SRTP packet with a modified payload or tag, and a replayed or tampered SRTCP packet are each dropped and counted, and none reaches the decoder. A secure account answers a plaintext offer with 488; a TLS account opens no UDP SIP listener.
4. RFC 4733 DTMF, hold/resume, blind transfer (REFER with NOTIFY tracking), re-registration after a dropped transport.
5. Requests accepted only from the configured registrar/proxy; RTP accepted only from the negotiated address.

If the dialog/media layer fails a criterion that cannot be fixed by configuration, the fallback is `sipgo` alone with the media layer ported from `crates/media` and `crates/telephony` onto `pion/rtp`/`pion/srtp`. The specs are library-agnostic either way. A "no path" result holds the migration at G0.

#### G0 finding (2026-11-11): diago passes with a small vendored patch and four engine-side rules

The spike (`internal/g0`, `docker/asterisk/go-g0.sh`) passed all 35 lab scenarios and its unit tests with `sipgo v1.7.0` and `diago v0.42.0`. Reading diago's source and running the negative probes showed gaps against the criteria, each closed without leaving the library:

- **No SRTP/SRTCP replay protection, and RTP accepted from any address** (criteria 3 and 5). diago creates receive contexts without replay options and never compares a packet's source with the negotiated address; its optional source lock trusts whoever sends four packets first. Fixed by a marked patch of about forty lines in a vendored copy (`third_party/diago`, `replace` in `go.mod`, described in its `README.go-microsip.md`): replay window on receive contexts, packets from another address dropped, unauthenticated packets dropped and counted instead of ending the stream. This is a middle path the original fallback did not foresee: far less code than porting a media layer, at the cost of carrying a patch until it is offered upstream.
- **A plaintext offer to a secure account is accepted** (criterion 3): diago falls back to plain RTP when the peer offers no keys. The engine inspects the offer and answers 488 itself.
- **Requests accepted from any address** (criterion 5): the engine installs a server middleware that drops requests not coming from the registrar's resolved addresses.
- **TLS and TCP accounts must not listen**: `Serve` would open a listener per transport; the engine registers the handler without serving, and incoming calls arrive on the connection the registration opened. Only a UDP account binds a socket.
- **Hold**: diago keeps sending in `sendonly`; the engine stops sending microphone audio while a call is held.

Media findings of M1 (`internal/core/media` tests): the `zaf/g711` mu-law encoder is one quantization step away from the reference at 508 samples, so the core carries its own 15-line encoder that matches the Rust candidate's reference hashes exactly (A-law and both decoders already match); a DTMF key is sent as 7 packets of 100 ms where the Rust candidate sent 8 of 120 ms; pion's SRTP with a 64-packet window passes every externally visible property the Rust adapter tested. Not carried over, and needing the owner's acceptance in the parity receipt: the Rust receiver bound itself to one SSRC, closed a key before its index space ran out, and bounded the number of unauthenticated sources it tracked.

Engine findings of M1 (`docs/receipts/go-m1-engine-2026-11-11.md`): driving the engine through Commands and Events in the lab found two more defects around diago that the endpoint-level G0 probes could not see. Its fixed jitter buffer never recovers from a silence longer than its window (a held call), which is patched in the vendored copy and pinned by a test; and it keeps the last response written as the dialog's, so a `180` sent beside the `200` makes a later BYE fail, which the engine avoids by ordering the two. The patch list in `third_party/diago/README.go-microsip.md` is the cost of this dependency and is reviewed at every update.

Other facts the engine must respect: sipgo takes the From user from the user-agent name, so the engine sets From explicitly to keep a product User-Agent; DTMF events reuse the audio stream's timestamp, so digits need audio (or silence) flowing; a transfer sent before the PBX has bridged the call is answered 400, so the engine refers only an established call. The replay, tamper and foreign-source proofs run on a loopback relay harness that stands on the path between two media sessions (the lab cannot spoof the PBX's address from another container); the secure calls through the PBX prove the patched path still interoperates.

- Alternative: write SIP from scratch. Rejected: largest possible new code for a solved problem.

### D4: Audio that works first — pure Go preferred, cgo accepted, chosen per OS by G0

Reliable two-way audio is the requirement; avoiding cgo is a preference (owner decision). Device capture/playback lives in `internal/platform/audio` behind `core/ports.AudioDevices`. For each OS, G0 runs the candidates below in order against the same adapter conformance test (enumerate, open by ID, capture/playback loopback, unplug) and the acceptance threshold, and the first that passes is used:

- **Linux**: (1) a Go client of the PulseAudio native protocol (candidate `github.com/jfreymuth/pulse`, MIT), which PipeWire's `pipewire-pulse` also serves — the audio service of the default desktop sessions on the supported Ubuntu 26.04+ and Debian 13 (confirmed per distribution in the G0 VMs, not assumed); (2) miniaudio through `github.com/gen2brain/malgo` (cgo), which also covers bare ALSA.
- **Windows**: (1) WASAPI through COM from Go (candidate `github.com/moutend/go-wca` on `go-ole`, MIT), shared mode, event-driven; (2) miniaudio through `malgo` (cgo), cross-built with `mingw-w64`.
- **macOS** (later change): CoreAudio through `purego`, or miniaudio, behind the same port.

Choosing the cgo backend needs no further approval: the G0 receipt records which backend each OS uses and why. If the pure-Go backend passes on one OS and not the other, the two may differ; if miniaudio is selected for both, it becomes the single backend and the pure-Go ones are dropped rather than kept as dead code. Library capability claims (device enumeration, capture, hot-plug notification, latency control) are unverified until the spike proves them.

Backend threads or callbacks only copy samples through preallocated single-producer/single-consumer rings; resampling to 8 kHz and the 20 ms media tick run in the engine, as in the Rust design. Ring, ringback and DTMF feedback tones are generated in Go from the same path.

Acceptance threshold, measured on the lab network over a five-minute call on Linux and on Windows: at most 0.5 % of 20 ms frames late or lost at the device, no playback gap longer than 60 ms, and the same run on the Rust candidate recorded beside it. The same threshold must hold while the window is frozen for five seconds, and unplugging the capture device must keep the call up without selecting another microphone.

Consequences: with the pure-Go backend the Windows build is a plain `CGO_ENABLED=0 GOOS=windows` cross-build; if miniaudio is selected for Windows, the build image gains `mingw-w64` and the `.exe` is cross-built with cgo. On Linux the binary already links native code for the webview, so a cgo audio backend adds no new build requirement there. A Linux session with no audio service the selected backend can reach is reported as "no audio service" by the doctor and the window, never shown as a working call.

- Alternative: forbid cgo entirely. Rejected by the owner: audio that works matters more than a cgo-free build.
- Alternative: audio in the webview (`getUserMedia`/WebAudio) streamed to Go. Rejected: puts the real-time path in JavaScript, depends on WebKitGTK media-stream/GStreamer/portal support inside an AppImage, and a frozen window would cut audio — against the `modular-core` real-time requirement.

### D5: Wails v3 window with bound services and events

`internal/gui` binds a small set of Go services to the SPA (send `Command`, query settings/contacts/history, run the import preview) and forwards core `Event`s as Wails events. The SPA holds a reducer equivalent to `apps/rust-microsip/src/ui/state.rs` and renders only from events. The UI thread never blocks the engine: commands go through the same bounded queue and overload notice as today. Audio never crosses the webview.

Tray, single instance and window management use Wails v3 facilities where they exist; notifications, secrets and `sip:`/`tel:` registration go through `internal/platform` so the CLI/MCP modes can use them without Wails. Go initializes every imported package before `main` runs, so headless safety cannot come from import order. Instead the GUI is constructed lazily: nothing in `internal/gui` or its imports may touch a display, GTK, WebKit or a graphics context at package initialization; the Wails application is created only inside the function the GUI mode calls. Wails `v3.1.0` also ships experimental CGO-free Linux and macOS backends (release notes of 2026-11-01). G0 finding: the CGO-free Linux backend (`-tags purego`, GTK 4 and WebKitGTK 6.0 loaded at run time) builds with `CGO_ENABLED=0`, opens the window under Xvfb, serves the embedded page, answers a bound call and passes the headless checks, so it is the default (`GO_WINDOW=purego`); the cgo backend stays one flag away (`GO_WINDOW=cgo`) and passed the same checks. The navigation guard of D6 is still to be built in M2 and must work without cgo; if it cannot, the Linux window falls back to the cgo backend and the guard of the sibling projects. Whether Wails v3 itself is free of display side effects at import is unverified and is a G0 check: `version`, `doctor --json` and a stdio MCP handshake must all succeed in a container with no display variables set (the sibling projects already ship this behavior).

Window: compact softphone proportions, resizable within limits, close-to-tray preserved from rc.2. Its arrangement is layout B of `docs/ui/layouts/` (owner's decision of 2026-11-11, replacing the MicroSIP-like tabs): a header with the account, the registration state, DND and the menu; a bottom navigation with four destinations (Home with a name-or-number search, favorites and recent calls; Calls; Contacts; Keypad); the current call takes the place of the keypad; an incoming call is a sheet at the bottom of the window.

### D6: Web trust boundary (desktop window)

The webview serves only the embedded `frontend` build (asset origin differs by platform in Wails v3: `wails://localhost` on Linux/macOS, `http://wails.localhost` on Windows — never hard-coded). Navigation and new windows are blocked; no backend HTTP listener exists for the UI; the Content-Security-Policy forbids remote scripts, styles, fonts and `eval`. Remote-controlled strings (display names, headers, contact imports) are rendered with text interpolation only — no `v-html`, enforced by a frontend lint rule. Links never open from remote content.

### D7: Skins — one stylesheet per skin, one token contract

Following `goXterm/frontend/src/themes`: `frontend/src/themes/index.ts` holds the registry (`id`, `label`, `dark`), each skin is a CSS file with a `:root[data-theme="<id>"]` block, and `<html data-theme>` selects it. `system` resolves to `light`/`dark` through `prefers-color-scheme` and follows changes live. Shipped ids: `light`, `dark`, `xp`, `macos`, `ubuntu`, `win98`, `matrix`.

Tokens cover surfaces and the backdrop, text, borders, accent, check marks, selection, focus, call-state colors and their chip tints, header, title, close-button, navigation, group, dialog, menu and scroll-bar chrome, fonts, bevel style, corner radii and control metrics; Tailwind utilities map to the tokens, so components contain no literal colors. `internal/archtest/skins_test.go` (as in kvm-gui) fails when skins define different token sets or fewer than the seven ship. A frontend test fails on hex/rgb literals outside `src/themes/`.

Look per skin, on layout B: `xp` (Luna: beige faces `#ece9d8`, glossy navy-outlined buttons, a blue title gradient with a red close button, navigation tabs that hang from the pane with an orange stripe, a blue window frame, Tahoma-like font), `win98` (gray `#c0c0c0`, 2-px bevels, navy title bar and selection, raised navigation tabs, dithered scroll track), `macos` (a light gray toolbar as the header, a centered title with the close disc at the left, white rounded controls, boxed groups, blue accent, system-font stack), `ubuntu` (Yaru light: a light gray header bar, orange focus, checks and the line over the selected destination, an aubergine ringing state, Ubuntu-like font stack), `matrix` (a dark terminal: phosphor green on near-black, monospace system fonts, glowing edges, a backdrop of scan lines and faint streaks that fall once for four seconds and not at all under `prefers-reduced-motion`), `light`/`dark` (the refined neutral default, drawn with layout B: round keys and pills, tinted state chips). Skins evoke a style with original CSS; no vendor logos, wallpapers, copied bitmaps or film artwork, nothing fetched from the network, and any bundled font is an open-licensed face recorded in the third-party notices (none is bundled today: every stack names system fonts with fallbacks).

Corners (owner's decisions of 2026-11-11, with layout B and with the request for skins closer to the real systems; they replace the square-corner rule this change had taken from the Rust-era `refine-native-ui` requirement): corners come from radius tokens and no component names a radius of its own. `--radius` is for cards, fields, buttons and wells, `--radius-pill` for keys, pills and avatars, and `--radius-window`, `--radius-menu`, `--tab-radius`, `--close-radius` and `--scroll-radius` for dialogs, menus, navigation tabs, the close button and the scroll thumb. `light`, `dark`, `macos` and `ubuntu` round them; `xp` rounds slightly, as Luna does (3 px controls, a window frame rounded at the top); `win98` and `matrix` keep every radius at zero. The frontend test names the skins that may round and fails when a source rounds a corner other than through a radius token.

Accessibility (same date): every skin meets WCAG AAA contrast. Text, labels and placeholders are at 7:1 on every background they are drawn on (disabled text is exempt); the edges of buttons, keys and fields, state icons, the selection fill, the scroll thumb and the focus ring are at 3:1; the focus ring is solid and at least 2 px; a pressed toggle differs from a released one by a 3:1 line and not only by its fill; call states are an icon and a label; no animation runs longer than five seconds or at all under `prefers-reduced-motion`. Where a system's own colors miss these numbers (Luna's title blue, Yaru's orange selection, the stock macOS control outlines), the skin uses a deeper tone. The token test and the rendered audit of the screenshot run enforce the numbers.

The stored setting stays the `ui.theme` key; its accepted values grow from three to eight (`system` and the seven skins) and unknown values resolve to `system`.

- Alternative: ad-manager's larger theme set (16 skins) from day one. Rejected: the user named six and later added Matrix; the registry makes more a one-file addition.

### D8: Configuration, storage and secrets keep their formats; names change

- Configuration keeps the six-layer precedence with provenance and the TOML keys. The directory becomes `go-microsip` under the XDG/`APPDATA` base; the environment prefix becomes `GO_MICROSIP_`. The old `RUST_MICROSIP_` names are not read; the import and the doctor output point this out.
- Storage is plain SQLite (the Rust app uses bundled, unencrypted SQLite with secrets outside the database), read and written with `modernc.org/sqlite` — the pure-Go SQLite the sibling projects use, so no C SQLite or cgo driver is linked on any platform. The Go store adopts the Rust schema and continues its migration numbering, so an imported database is a file copy followed by forward migrations, with the same pre-migration backup rule.
- Secrets: Secret Service on Linux and Credential Manager on Windows through `github.com/zalando/go-keyring` (CGO-free on both: D-Bus and the Windows credential API from Go), under a new service name. The database holds only the non-secret reference. Passwords are never accepted on the command line.

### D9: Explicit import from the Rust application

`go-microsip import rust-microsip` (and the same action in settings) locates the Rust application's configuration directory and database and prints a preview (account without password, counts of contacts and history rows, whether a keyring entry exists). With `--apply` or a confirmed dialog it runs this protocol:

1. **Quiesce both sides.** The import takes the Rust application's single-instance lock the same way the Rust application does and holds it until the snapshot of step 2 is complete, so the Rust application can neither be running nor start mid-copy; if the lock cannot be taken the import refuses. It also holds go-microsip's own instance lock for the whole import, with go-microsip's store closed (the window closes and reopens its store around the import).
2. **Snapshot.** Copy the configuration file and the database together with its `-wal` sidecar, if any, into a staging directory inside go-microsip's data directory. The source is only read; no SQLite connection is opened on the source path, so no sidecar is created there.
3. **Migrate and validate** the staged copy (integrity check, forward migrations, row counts equal to the preview), and convert the staged configuration file (same TOML keys; nothing else changes).
4. **Stage the credential**, only if the user ticked that option: allocate a fresh keyring reference, write it to the import journal and sync the journal, and only then write the password to go-microsip's keyring service under that reference and record the reference in the staged database. The Rust keyring entry is read, never changed.
5. **Seal.** Checkpoint the staged database's write-ahead log into the main file, close it, and confirm no `-wal` or `-shm` sidecar remains, so the migrations and the credential reference are inside the one file that will be renamed. Sync the staged files to disk.
6. **Commit.** Rename the previous go-microsip database and configuration file to timestamped backups, then rename the staged database and the staged configuration file into place. A journal file, synced before each rename, records the step reached and the backup names.

Recovery: on the next start, an unfinished journal before step 6 discards the staging directory and deletes the keyring entry named in the journal, whether or not the password write had happened (the reference is journaled first, so recovery always knows what to remove); an interruption anywhere inside step 6 restores both backups, so the database and the configuration file always come from the same side. Either way the previous go-microsip data is what the user sees, and the import can be run again. The keyring write is deliberately outside the atomic rename: an orphaned staged entry is the only possible leftover and recovery removes it. This satisfies both "preserve personal settings" and "never silently migrate".

- Alternative: read the Rust directories in place (goXterm's choice). Rejected: the product name changes, and two applications writing one database and one single-instance lock need coordination code the import avoids.

### D10: Agent control re-implemented with distinct identities

`internal/control` re-implements the `add-agent-cli-mcp` behavior — single owner, same-user transport (Unix socket with peer-credential check on Linux, per-user named pipe on Windows), hello/welcome versioning, grants, approvals shown in the window, idempotency keys, audit — with go-microsip's own socket/pipe name so it never collides with a running Rust instance. The MCP server uses `modelcontextprotocol/go-sdk` over stdio and exposes the same tool set and permission rules. The command line is Cobra throughout, as in the sibling projects (owner instruction): one file per command under `cmd/`, the MCP server as the `mcp` subcommand, options as Cobra flags. The Rust option-style commands therefore become subcommands (`--doctor` becomes `doctor`, `mcp --stdio` becomes `mcp`); each such difference is recorded in the parity matrix as an accepted difference. The wire framing may differ from the Rust IPC: no external client depends on it, because the CLI and MCP server ship in the same binary. The headless service of the Rust candidate (`service --grant-file FILE`: the profile's owner on a machine without a desktop session, started by a single-use bootstrap grant exported from the window, serving only what that grant allows and never asking anybody) is part of the re-implementation, as the `service` subcommand (owner's decision of 2026-11-12, when task 6.4 had left it out). It needs an explicit password source and does not read the keyring; while it runs, the window and links are turned away. On Windows the transport is not the named pipe named above but a socket of the Unix-domain family, which Windows has, in the user's local application data, with the system asked at both ends which process is at the other end and whose it is: the same-user rule is kept and the server, the client and their limits are the code Linux uses (an implementation choice of task 6.3, for the owner to confirm at the parity gate).

### D11: Build, packaging and milestone artifacts

One Docker image (`build/linux.Dockerfile`, on a Debian 13 base: Go, Node, GTK4/WebKitGTK 6.0, Xvfb, AppImage tooling; base image pinned by digest) builds everything; the Windows artifacts are a `CGO_ENABLED=0 GOOS=windows` cross-build, or a `mingw-w64` cgo cross-build if G0 selects the cgo audio backend for Windows (D4). `make go-dist` writes to `dist/`:

- `go-microsip-<version>-linux-amd64`
- `go-microsip-<version>-x86_64.AppImage` (runs with `--appimage-extract-and-run`, so no FUSE)
- `go-microsip-<version>-windows-amd64.exe` and `.zip`
- `SHA256SUMS`, a build manifest naming the commit, and the third-party notices generated from the Go and npm dependency graphs.

`dist/` stays git-ignored; milestone artifacts are local evidence, and a GitHub pre-release happens only through the `create-release` skill and its gates. Versions continue as `0.2.0-rc.N`.

### D12: Verification environment

- **PBX**: the existing `docker/asterisk` lab remains the deterministic primary target (its probes and suites are reused against the Go binary). A FreePBX 17 container (community image, pinned by digest, fictional extensions generated at start) is added under `docker/freepbx` as the interoperability target on an internal Docker network. The lab's existing probes remain the reference peer; the closed-source `gophone` binary may be added as an optional extra peer only with the owner's approval of the download, pinned by release tag and checksum, run inside a lab container, and never packaged or redistributed.
- **VMs**: the Linux matrix is Ubuntu 26.04 (default GNOME/Wayland session), Debian 13 (default GNOME/Wayland session) and one KDE Plasma session on either distribution — on X11 where the distribution still offers that session, otherwise Wayland — via Vagrant/libvirt or the local `kvm-gui`; Windows 11 via `dockur/windows` or KVM. VMs attach only to the isolated lab network; no personal host setting is changed and everything is torn down after a run.
- **Automation**: `agent-desktop` drives the built AppImage/exe through the accessibility tree (window, dial, hang up, tray, skin switch); `agent-browser` drives the SPA in dev mode with a recorded-event backend for the per-skin screenshot matrix, contrast audit and keyboard checks. Both run at the close of every milestone that changes the interface (M2, M3, M4), on that milestone's exact artifacts. Screenshots go to `docs/prints/go/`.
- **Parity tracing**: `docs/migration/parity-matrix.md` has two parts. Part A is the behavioral baseline: one row per requirement and scenario of the Rust-era delta specs pinned at the `0.1.0-rc.3` commit, each ending as proven (with evidence) or dropped (with the owner's written approval). Part B is the surface inventory that `codebase-memory` extracts from the indexed repository: each Rust `Command`, `Event`, CLI command, MCP tool, configuration key and environment variable with its Go counterpart. The gate needs both parts complete. `ai-memory` is consulted for earlier decisions and treated as untrusted history; nothing is written to it without an explicit request.
- **Review**: `codex exec` in a read-only sandbox reviews the proposal and each milestone diff with a sanitized prompt (no secrets, no personal paths, nothing from `base/`). Findings and resolutions are recorded under `docs/reviews/`. `AGENTS.md` is updated so Codex is the independent reviewer for this change.

### D13: Milestones

| Milestone | Content | Exit evidence |
| --- | --- | --- |
| M0 / G0 | Spike: SIP+SRTP+audio (D3, D4), empty Wails window, AppImage and cross-built `.exe` | G0 receipt with go/no-go, Codex review |
| M1 Core | `internal/core` complete with in-memory adapters, config, store, import, CLI | Lab suites green from the CLI, layering test, `dist/` |
| M2 Desktop | Wails window with every screen wired to events, Light/Dark | Lab calls from the window, agent-desktop smoke, `dist/` |
| M3 Skins | Seven skins, refinement, screenshot matrix, contrast audit | Matrix and audit receipts, `dist/` |
| M4 Integration | Tray, notifications, links, single instance, control + MCP, Windows adapters | Linux and Windows VM receipts, `dist/` |
| M5 Cutover | Parity gate, FreePBX interop, archive Rust, rename, docs, release candidate | Parity receipt, owner decision |

Each milestone ends with: shared format/vet/test run, `make go-dist`, Codex review, commit and push, and a status report of verified tasks.

### D14: Frontend on Vue 3 + Vite Plus, shared with a web version

The frontend is Vue 3 built with Vite Plus (`vp`) and Tailwind, as in the sibling projects (owner instruction). The owner also asked for a web version. It is a separate change, to be proposed on top of `internal/core` in place of the Rust-era `add-web-wasm-softphone`; this change only keeps the door open, at no extra cost:

- The page reaches the backend through one small module (`frontend/src/api.ts`). Screens never call the Wails runtime directly, so a web build swaps that module's transport (Wails bindings on the desktop, HTTP and WebSocket in a browser) through a Vite mode, as `goXterm` does with its `desktop` mode.
- The core's command/event contract carries no desktop type, so a `web` Cobra command can serve the same page and the same contract.
- The desktop rules of D6 (no listener, nothing remote) describe the desktop window only. A web mode listens on a socket by definition and needs its own trust boundary (authentication, TLS, origin checks) and its own audio path in the browser; both belong to that change, not to this one.

## Risks / Trade-offs

- [The Go SIP/SRTP stack does not reach the Rust engine's security behavior (SDES over validated TLS, 488 on plaintext, peer pinning)] → G0 tests each rule against the lab before any UI work; fallback is `sipgo` plus a ported media layer; "no path" stops the migration.
- [A pure-Go audio backend lacks enumeration, hot-plug or low enough latency, or the Linux session has no PulseAudio-protocol server] → G0 tests each OS's candidates in order against the threshold on real VMs and takes the first that passes, cgo miniaudio included; the port keeps the choice local to one package.
- [The cgo audio backend complicates the Windows cross-build] → only taken if the pure-Go backend fails; `mingw-w64` cross-build proven in G0 inside a Windows VM before it is adopted.
- [Garbage-collector pauses or scheduler jitter degrade 20 ms audio] → device callbacks touch only preallocated rings; the media tick allocates nothing per packet; G0 and M1 measure jitter and underruns over a five-minute call and compare with the Rust receipt.
- [WebKitGTK inside an AppImage: size, GPU/driver crashes, missing codecs] → reuse the sibling projects' AppImage recipe, `--safe-gpu` start option and webview guard; verify in clean Ubuntu 26.04 and Debian 13 VMs (GNOME and KDE sessions); record the size increase over the Rust AppImage as an accepted difference.
- [A rewrite silently drops a behavior] → the parity matrix has a row per baseline requirement and scenario plus the code-surface inventory; the gate needs evidence or an owner-approved scope change per row.
- [Retro skins hurt legibility or look like trademark copies] → contrast audit and state-not-by-color rule in `ui-themes`; original CSS only, no vendor assets.
- [Two applications confuse users during coexistence] → distinct names, directories, sockets and desktop entries; the import is explicit; documentation states which one is released.
- [Scope: about 39k lines re-implemented] → milestones are independently verifiable and each leaves a runnable binary; the Rust app keeps building until M5.
- [Superseded Rust tasks look abandoned] → each superseded change records why it stopped; nothing unverified is checked.

## Migration Plan

1. Approve this proposal (owner) after the Codex proposal review; update `AGENTS.md` and `openspec/config.yaml` for the Go direction.
2. M0/G0 spike and go/no-go. A "no-go" leaves the repository as it is, minus the spike branch.
3. M1–M4 beside the Rust workspace; the Rust CI job keeps running.
4. M5, in this order, each a separate commit or action: (a) record the parity gate; (b) move Rust to `archive/rust/` and make the Go targets the defaults; (c) switch CI, `release.yml`, the `create-release` skill and the docs to Go; (d) rebuild the artifacts from the resulting commit and re-run the smoke checklist on those exact files, because packaging and default builds changed after the gate; (e) with the owner's confirmation rename the GitHub repository and update `origin`; (f) close the superseded changes.
5. Cut `v0.2.0-rc.1` under the existing release gates, from the commit validated in 4(d).

Rollback: before step 4 nothing irreversible has happened — delete the Go tree or stop using it. Steps 4(b) and 4(c) are ordinary commits, reverted individually in reverse order (the archive move is a pure rename, so its revert restores the Rust build). Step 4(e) is an external action: it is undone by renaming the repository back on GitHub and resetting `origin`; GitHub redirects the old name in the meantime. The Rust candidate `0.1.0-rc.3` artifacts remain published throughout.

## Open Questions

- Which community FreePBX 17 image to pin, decided when the interop lab is built; it does not change the tasks.
- Whether the macOS skin also gets a dark variant (as goXterm ships); it is a one-file addition under the token contract.
- Exact compact window dimensions per skin title-bar style, settled during M2 refinement.
