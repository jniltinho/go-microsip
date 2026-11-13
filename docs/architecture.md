# Architecture

go-microsip is one binary: the desktop window (the default), the command line and a stdio MCP server, plus two modes without a window (`run` and `service`). Everything that is telephony, media, configuration or storage is in `internal/core`, which knows nothing about the desktop toolkit or about an operating system; front ends and operating-system adapters sit around it.

The decisions behind this are in the [design](../openspec/changes/migrate-to-go-wails/design.md) of the `migrate-to-go-wails` change (cited below as D2, D3, ...), and the requirements in its five specifications: [modular-core](../openspec/changes/migrate-to-go-wails/specs/modular-core/spec.md), [desktop-app](../openspec/changes/migrate-to-go-wails/specs/desktop-app/spec.md), [ui-themes](../openspec/changes/migrate-to-go-wails/specs/ui-themes/spec.md), [milestone-distribution](../openspec/changes/migrate-to-go-wails/specs/milestone-distribution/spec.md) and [implementation-migration](../openspec/changes/migrate-to-go-wails/specs/implementation-migration/spec.md). How to build and test is in the [developer guide](DEVELOPMENT.md).

## Layers

```
main.go                 the only place that wires operating-system adapters into the front ends
cmd/                    the command line (Cobra), one file per command                   front end
internal/gui/           the window on Wails v3; the only package that imports Wails      front end
internal/control/       local control of a running go-microsip: protocol, owner, grants  front end
internal/mcp/           the stdio MCP server; every tool is one control operation        front end
frontend/src/           the page the window shows (Vue 3): api.ts is the only module that
                        reaches the backend; state.ts is the reducer; themes/ holds the skins
internal/core/
  domain/               validated account settings and dial targets, registration and call states
                        with their transitions, the Command and Event types; no I/O, no library
  ports/                the interfaces through which the core reaches the system: MediaIO, Player,
                        AudioDevices, FileAccess, Locker, Secrets, CallNotice
  ports/memory/         the ports without an operating system, for tests and the lab runner
  app/                  what front ends call besides domain and ports: Doctor, Validate, Export,
                        Import, Run, and Phone (the softphone a window or the service drives)
  config/               six sources per field, with the source of each value; never persists a secret
  store/                SQLite: call history, contacts, the settings the user edited, the control ledger
  engine/               one account and one call; a single actor owns every state change
  sip/                  the account's signaling on sipgo and diago, and its refusals
  media/                the two G.711 codecs on 20 ms frames and the RTP layer's security settings
  rustimport/           the explicit import of the Rust application's data (D9)
internal/platform/
  audio/                capture and playback of 20 ms frames of 8 kHz mono audio, one backend per system
  keyring/              passwords in the user's keyring
  notify/               the desktop notification of a call that rings or was missed
  lock/                 the advisory file lock of one instance
  files/                who owns a file and who may touch it, for the configuration's checks
internal/buildinfo/     version, commit and which optional parts were linked in
internal/archtest/      the layering rules and the skins' token contract, as tests
internal/g0/            lab probes and scenarios against the disposable PBX (test infrastructure)
third_party/diago/      the vendored dialog and media library
```

`internal/archtest` (D2) parses the imports of every Go file of the project, tests excepted, and fails `make test` when a rule is broken. As the test states them:

- Only `internal/gui` may import Wails.
- The core is operating-system neutral: no file of `internal/core` is bound to an operating system by its name or a build constraint, and the core imports no front end and no platform adapter.
- The domain imports a short list of standard-library packages and nothing else of the project: no I/O, no library.
- Front ends (`cmd`, `internal/gui`, `internal/mcp`, `internal/control`) use only `domain`, `ports` and `app` of the core, import no platform adapter (only `main` wires those), and never import a SIP, media, audio, SQL or keyring library directly.
- cgo is allowed only in `internal/platform/audio` (D4) and `internal/gui`. No project file uses it today.
- `unsafe`, `syscall` and `golang.org/x/sys` stay in `internal/platform` and in the OS-bound files of `internal/control` and `internal/gui`. The one exception is `syscall` for the names of signals in `cmd` and `internal/g0`.

Each rule has a deliberate violation that must fire and an allowed neighbor that must not.

## Runtime model

- **The engine is an actor.** One goroutine owns every state change of the account and the call. Network operations (register, dial, answer, hold, transfer, hang-up) run beside it with their own time limits and post their results back, so the actor never waits for the network.
- **Commands in.** A front end hands a `domain.Command` to `Engine.Send`, which never blocks: beyond 64 queued commands it refuses, and the front end tells the user.
- **Events out.** `domain.Event`s leave through an outbox that never blocks the actor. For a reader that is slow, intermediate snapshots of a call and repeated registration states are merged into the newest; a call's last snapshot and `Stopped` are always delivered, and `Stopped` is last.
- **Shutdown.** `Shutdown` ends the call, unregisters, closes the sound devices and reports `Stopped` within five seconds, whatever the network or the sound service do. A failure in the actor is caught and reported as an engine failure, still followed by `Stopped`.
- **One instance.** Whoever runs the engine holds `instance.lock` beside the database; a second go-microsip on the same database refuses to start.

How each mode reaches the engine:

| Mode | Path |
| --- | --- |
| Window (`go-microsip`) | The page calls the methods bound on `gui.App`; each command method passes a `domain.Command` to the core's `app.Phone`. The engine's events reach the page as one Wails event, and the page renders from them through its reducer (D5). The window also owns the control socket. |
| `run` | `app.Run` starts the engine without a window: register, take calls, optionally place one. An interrupt or a termination signal becomes the engine's shutdown. It serves no control socket. |
| `service` | Owns the profile as the window does (instance lock, database, SIP, sound) and serves the control socket with a single-use bootstrap grant exported from the window. Nobody is there to ask: what the grant does not cover is refused. |
| Control commands (`status`, `call ...`, `dnd`, ...) | Clients of the control socket. They never start a softphone, open the database or register SIP. |
| `mcp` | A stdio server whose tools are each exactly one control operation, sent through the same socket as the command line. It opens no network port and needs no display. |

The control protocol, the command line, the MCP tools and grants are described in [agent-cli-mcp.md](agent-cli-mcp.md) (D10).

## Audio path

- The sound-device adapter (`internal/platform/audio`, behind `ports.AudioDevices`) captures and plays 20 ms frames of 8 kHz mono audio. Its device side and the engine's 20 ms media tick never share a lock: samples cross through preallocated single-producer single-consumer rings, so neither side blocks or allocates.
- Linux speaks the PulseAudio native protocol from Go, which PipeWire's `pipewire-pulse` serves too. Windows uses WASAPI in shared mode through COM. Neither needs a C compiler (D4). On another system every use reports that no audio service is reachable; a missing capability is reported, never simulated.
- Each call has two goroutines, neither of which touches the actor. One sends the microphone to the network on the 20 ms tick: silence when muted, and nothing of the microphone while the call is held. One receives through a fixed, bounded jitter buffer (three packets of delay, ten at most), decodes and plays.
- The codecs are G.711 A-law and mu-law. DTMF keys are RFC 4733 events on the same RTP stream as the audio; a call whose other side did not agree to them has its audio and refuses keys.
- A microphone that stops delivering is reported once; a direction of the media that ends on its own ends the call.
- Audio never crosses the webview, so a frozen page does not interrupt a call.

## Storage and configuration

- **Configuration** (`internal/core/config`, D8). Each field is resolved on its own from command-line overrides, the environment (`GO_MICROSIP_<SECTION>_<KEY>`), an explicit `.env` file, one TOML file, the values saved in SQLite, and built-in defaults; the first source that holds it wins and is recorded. Only explicit user edits are written back to SQLite.
- **Database** (`internal/core/store`). One SQLite file, `go-microsip.sqlite3` in the user's data directory, read and written with the pure-Go `modernc.org/sqlite`. It holds the call history (a call is journaled when it starts, so one cut short by a crash is closed as interrupted at the next start), contacts, the settings the user edited and the ledger of agent control (operations, grants, audit). It keeps the Rust application's schema and continues its migration numbering. A schema upgrade is one transaction after a backup; a database written by a newer release is refused untouched.
- **Passwords** (`internal/platform/keyring`, behind `ports.Secrets`). A password typed in the window is kept in the user's keyring and nowhere else: the Secret Service on Linux, the Credential Manager on Windows. The database holds only a reference that names the entry. Without a keyring the password lasts for the session.

## Security boundaries

- **Passwords** are never accepted as command-line arguments: any option whose name contains "pass" is refused before anything parses or echoes it. A `domain.Secret` prints as `[REDACTED]` in every format. Nothing the application writes (database, exported configuration, doctor report) contains a password; one comes from the keyring, from the environment variable named by `account.password_env`, from a private `account.password_file`, or from a literal in the user's own TOML file, which is kept in memory only and flagged. Errors of configuration and storage name fields, files and lines, never values. The engine's diagnostics carry no password, dialed number or DTMF key.
- **Files.** A configuration, `.env` or CA file that group or others can write is refused. A file that holds a secret must be a regular file owned by the user with no group or other permission; a password file must not be a symlink. The database is a 0600 file in a 0700 directory. On Windows the files adapter has no owner or mode bits to read and reports every file as private (a shortcut marked in `internal/platform/files`).
- **The page** (D6). The webview shows only the page embedded in the binary. Every asset is served with a content security policy: no inline or remote script, no `eval`, no remote style, font or image, no frame; requests go to the page's own origin only. On Linux a navigation guard also refuses every navigation to another origin and every new window; on other systems the content security policy alone confines the page. The page reaches the softphone only through the methods of `gui.App`, and there is no listening socket for the interface. `--self-test` checks the window, the call into Go and this confinement.
- **The control socket** (D10). A Unix socket in a directory of the session's runtime directory that only the user can enter; both ends ask the kernel who the other is. On Windows it is a socket of the Unix-domain family in the user's local application data, with the same question asked of the system. A change asked through it waits for a person at the window unless a grant covers it, and personal data needs a read grant. This is a consent boundary for agents, not a sandbox against programs already running as the user: see [agent-cli-mcp.md](agent-cli-mcp.md).
- **Signaling** (`internal/core/sip`). Requests are accepted only from the registrar. A TLS account trusts only its configured CA bundle, never the system roots. A TLS or TCP account listens on nothing: requests arrive on the connection the registration opened; only a UDP account binds a socket.
- **Media.** A secure-media account answers an offer without SRTP with 488 and ends a call whose answer is not SRTP before any audio is sent or played. RTP and RTCP are taken only from the negotiated address; replayed or forged SRTP and SRTCP packets are dropped and counted (replay window of 64).

What this version does not do, and the exceptions to the rules above, are stated in the README: [Limits of this version](../README.md#limits-of-this-version).

## Platforms

| System | Window | Sound | Keyring | Control |
| --- | --- | --- | --- | --- |
| Linux (Ubuntu 26.04 and later, Debian 13; x86_64) | GTK 4 and WebKitGTK 6.0 from the system, loaded at run time through purego, built with `CGO_ENABLED=0` (the default); `GO_WINDOW=cgo` links them at build time instead (D5) | PulseAudio protocol | Secret Service | Unix socket |
| Windows (x86_64) | WebView2, cross-built without cgo | WASAPI | Credential Manager | Unix-domain socket |
| macOS | not built | no backend | no adapter | none |

A build without a window (Linux with neither cgo nor the `purego` tag) keeps every other command. The core is checked with `go vet` for macOS so that it stays portable; nothing else exists for that system. The state of each platform, as tested, is in the README's [Platforms](../README.md#platforms).

## Third-party SIP and media stack

- `github.com/emiago/sipgo` provides SIP transactions and transports; `github.com/emiago/diago` provides dialogs and RTP on top of it (D3). Packets, RFC 4733 events, SRTP and the jitter buffer come from diago and from `github.com/pion/rtp`, `pion/rtcp` and `pion/srtp`; the core carries only the G.711 codecs and the configuration of that layer.
- diago is vendored at `v0.42.0` in `third_party/diago`, and `go.mod` replaces the upstream module with that copy. The G0 spike and later milestones found gaps between its media layer and the softphone's rules (no replay protection, RTP accepted from any address, a jitter buffer that did not recover from a long silence, and others); each is closed by a change marked `go-microsip:` and listed in [its README](../third_party/diago/README.go-microsip.md). `internal/core/media` turns the resulting guard on for every call.
- Rules the library does not enforce are the engine's and the endpoint's own: the 488 to a plain offer, the registrar-only filter, no listener on TLS and TCP accounts, and no microphone audio while a call is held.
- The dependency graph accepted for the Go rewrite, with its licenses and native code, is recorded in [ADR 0014](adr/0014-go-dependency-graph.md), written at the G0 spike; `make go-licenses` writes the notices and the bill of materials shipped with each build.
