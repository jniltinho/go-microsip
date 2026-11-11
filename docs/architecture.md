# Architecture

Two code bases live in this repository until the cutover of `openspec/changes/migrate-to-go-wails`: the Go application described first, which is where new work happens, and the frozen Rust candidate (`rust-microsip 0.1.0-rc.3`) described [below](#rust-candidate-frozen), which is the parity baseline and still builds.

## Go application

One binary is the desktop window (the default), the command line and the stdio MCP server. Everything that is telephony, media, configuration or storage is in `internal/core`, which knows nothing about the desktop toolkit or about an operating system; front ends and operating-system adapters sit around it.

```
main.go                 wires the operating-system adapters into the front ends; nothing else does
cmd/                    command line (Cobra): version, doctor, config, run, mcp           front end
internal/gui/           the window (Wails v3); the only package that imports Wails        front end
internal/mcp/           stdio MCP server                                                  front end
frontend/               Vue 3 + Vite Plus + Tailwind page shown by the window
internal/core/
  domain/               account settings, dial targets, states and transitions, the Command and
                        Event contract; standard library only, no I/O
  ports/                what the core needs from the system: AudioDevices, MediaIO, FileAccess;
                        ports/memory implements them for tests (a tone for a microphone, a meter
                        for a speaker)
  app/                  what front ends call: Doctor, Validate, Export, Import, Run
  config/               six layers (flags > environment > .env > TOML > SQLite > defaults) with the
                        source of each value, file-permission rules, doctor, import preview
  store/                SQLite in pure Go with the Rust schema: history with a crash journal,
                        contacts, CSV import, the settings the user saved, backups before upgrades
  rustimport/           the explicit import from the Rust application: snapshot under its instance
                        lock, journaled commit, recovery that keeps database and file together
  engine/               one actor: registration with bounded retry, the call, hold, mute, DTMF,
                        blind transfer, bounded shutdown
  sip/                  the account's endpoint over sipgo and diago: TLS, the registrar-only filter,
                        the SRTP rules
  media/                G.711 and the media guard (strict source address, SRTP replay window)
internal/platform/
  audio/                PulseAudio protocol on Linux (PipeWire serves it), WASAPI on Windows;
                        samples cross to the engine through lock-free rings
  files/                who owns a file and who may touch it
  lock/                 the advisory lock of one instance per user (flock)
internal/archtest/      the layering rules, as a test
internal/g0/            lab probes and scenarios against the disposable PBX (test infrastructure)
third_party/diago/      diago v0.42.0, vendored with marked patches (see its README)
build/                  build image, make targets, QA scripts, packaging, VM checks
```

`internal/archtest` fails the build when a rule is broken: only `internal/gui` imports Wails; the core imports no front end, no platform adapter and has no operating-system-bound file; the domain imports nothing but the standard library; front ends (`cmd`, `internal/gui`, `internal/mcp`, `internal/control`) reach the core only through `domain`, `ports` and `app`, and never import a SIP, media, audio, storage or keyring library; cgo is allowed only in `internal/platform/audio` and `internal/gui`.

What is wired today and what is not: the command line runs the whole core (`go-microsip run` is the softphone without a window); the window opens but is not connected to the engine yet (M2); the MCP server answers with one tool, and the control channel with grants and approvals is not ported yet (M4); passwords come from the environment or a private file, the keyring adapter is not ported yet (M4), so `import rust-microsip --credential` refuses until then; the import and the single instance of `run` work on Linux only, the Windows lock is not implemented yet.

### Runtime model

- **Front end.** It sends `domain.Command`s with `Engine.Send`, which never blocks: beyond 64 queued commands it refuses, and the front end tells the user. It reads `domain.Event`s from one channel.
- **Engine actor.** One goroutine owns every state change. Network operations (register, dial, answer, hold, transfer, hang-up) run beside it with their own time limits and report back, so the actor never waits for the network.
- **Events.** An outbox between the actor and the front end merges intermediate snapshots of a call and repeated registration states for a reader that is slow; a call's final snapshot and `Stopped` are never dropped, and `Stopped` is always last.
- **Call audio.** Two goroutines per call. One sends: a frame from the microphone every 20 ms on the RTP clock, silence when muted or when the microphone has nothing, nothing at all while the call is held. One receives: authenticate, a fixed jitter buffer (three packets of delay, ten at most) on a timetable, decode, play. RTP is taken only from the negotiated address.
- **Sound devices.** The audio service's threads and the engine never share a lock: samples cross through preallocated single-producer single-consumer rings. A microphone that stops delivering is reported once (`microphone_lost`); the call goes on in silence and no other device is opened.
- **Shutdown.** `Shutdown` ends the call, unregisters, closes the sound devices and reports `Stopped` within five seconds, also when the PBX does not answer. A panic in the actor is caught and reported as `engine_failed`, still followed by `Stopped`. `go-microsip run` turns an interrupt or a termination signal into this shutdown and exits 130 or 143.
- **History.** A call is journaled when it starts and completed when it ends; a row left open by a killed process is closed as `interrupted` at the next start.
- **One instance.** `run` holds `instance.lock` beside the database, so there is one go-microsip per database whatever session starts it; a second one refuses to start. Both imports hold this lock; the one from the Rust application also holds the Rust application's own while it copies.

### Security boundaries

- SIP requests are accepted only from the configured registrar, address and port; anything else is dropped without an answer.
- A TLS account trusts only its configured CA bundle, never the system roots, checks the host name, and opens no listening socket. The bundle is read through the same permission checks as the configuration and handed over as bytes.
- Media keys (SDES) travel only over that TLS. A secure-media account answers a plain offer with 488 and ends a call whose answer is not SRTP before any audio is sent or played. SRTP and SRTCP have a replay window; a packet from another address, replayed or failing authentication is dropped and counted.
- Passwords are never accepted as command-line arguments: any option whose name contains "pass" is refused with status 2 before anything is parsed or echoed. A `domain.Secret` prints as `[REDACTED]` in every format. Errors of configuration and storage name fields, files and lines, never values.
- A configuration, `.env` or CA file that group or others can write is refused; a file that holds a password must be private and owned by the user; a password file must not be a symlink. Nothing the application writes contains a password: the database holds at most a reference to a keyring entry.
- The database is a 0600 file in a 0700 directory; an upgrade is one transaction after a private backup; a database from a newer release is left untouched.
- Usage errors never echo an argument, and `run` never prints the other party's number.

Dependencies, licenses and native code are in [ADR 0014](adr/0014-go-dependency-graph.md); the evidence is in [receipts/](receipts/) (`go-g0-*`, `go-m1-*`).

## Rust candidate (frozen)

rust-microsip is a Cargo workspace split into layers with one-way dependencies. The domain layer knows nothing about sockets, devices, files or the UI toolkit, so the telephony engine, audio, storage and UI can each be replaced or ported without touching the others.

```
apps/rust-microsip (binary + egui UI)
 ├── crates/config     CLI, environment, .env, TOML, SQLite layer -> EffectiveConfig
 ├── crates/storage    SQLite settings, history, contacts, CSV import, backups
 ├── crates/platform   single instance/IPC, sip: links, notifications, Secret Service
 ├── crates/audio      cpal device bridge (ALSA today) -> MediaIo
 └── crates/telephony  engine: SIP (rsipstack), SDP, RTP/SRTP media
       └── crates/media    G.711, RTP header, RFC 4733 DTMF, SRTP adapter
all of the above depend on
 crates/core           domain types, state machine, Command/Event, MediaIo
```

| Crate | Responsibility | Platform-specific code |
| --- | --- | --- |
| `rust-microsip-core` | Validated account settings and dial targets, DTMF keys, registration/call states and allowed transitions, redacted `Command`/`Event` contract, the `MediaIo` trait. No I/O. | none |
| `rust-microsip-media` | G.711 µ-law/A-law, RTP packets, RFC 4733 events, SRTP/SRTCP AES-CM-128/HMAC-SHA1-80 with replay protection. | none |
| `rust-microsip-telephony` | `engine::EngineHandle`: one account and one call on a dedicated Tokio thread; registration, calls, hold, DTMF, mute, DND, blind transfer, SDES, jitter buffer. The `g0_*` modules are the earlier isolated lab probes. | none (Tokio and rustls are portable) |
| `rust-microsip-audio` | `DeviceMedia` implements `MediaIo` with lock-free rings and 8 kHz resampling. | cpal backend: ALSA on Linux; cpal also provides CoreAudio and WASAPI |
| `rust-microsip-config` | Six-layer resolution (flags > env > `.env` > TOML > SQLite > defaults) with provenance, secret handling, sanitized doctor/export/import; only user edits are written back. | XDG paths and Unix permissions |
| `rust-microsip-storage` | SQLite schema/migrations with pre-migration backups, settings, call history with crash recovery, contacts, CSV import, online backups. | Unix permissions |
| `rust-microsip-platform` | Single instance over a private Unix socket, `sip:`/`tel:` parsing, desktop notifications and Secret Service password storage over D-Bus (zbus with async-io). | Linux desktop services |
| `rust-microsip` (app) | Command-line front end and the egui/eframe window; wires config, audio and engine. | none beyond eframe/wgpu |

`crates/core/tests/layering.rs` enforces these boundaries from `cargo metadata`: each workspace package's normal dependencies are allowlisted and the UI module may not name SQL, SIP, TLS, Tokio, audio or D-Bus crates.

### Runtime model (Rust)

- **UI thread**: eframe event loop. It only sends `Command`s with a non-blocking `try_send` (a full queue yields `Notice::Overloaded`) and drains `Event`s each frame. The engine wakes it with `request_repaint`.
- **Engine thread**: a two-worker Tokio runtime owns the SIP endpoint, dialogs, timers and per-call media tasks. A single actor serializes all state changes; spawned tasks report back over a bounded internal channel.
- **Media**: one task per call ticks every 20 ms: capture → encode → (SRTP) → send, and receive → authenticate → decode → bounded jitter buffer → play. RTP is accepted only from the negotiated address.
- **Audio devices**: cpal callbacks only move samples through preallocated SPSC rings; resampling runs on the media tick, never in a device callback.
- **Supervision**: a panic in the engine actor is caught (the default `panic = "unwind"` is required) and reported as `Notice::EngineFailed`; a panic inside a spawned per-call task ends only that task; on shutdown the engine drains its event backlog (bounded) so `Stopped` is delivered.
- **Signals**: SIGTERM/SIGINT close the window through the normal path, so the engine ends calls and unregisters within a bounded shutdown.

### Security boundaries (Rust)

- SDES keys travel only over validated TLS; SRTP accounts reject plaintext offers (488) and a TLS account listens on no UDP port.
- Requests are accepted only from the configured registrar/proxy address over the configured transport.
- Passwords are never accepted as command-line arguments and are redacted from every `Debug` output. The application writes a password only to the system keyring (Secret Service) when the user opts in; files and the database hold at most a non-secret reference. Secret-bearing files must be private and owned by the user.
- A configured CA bundle replaces the system roots; if it cannot be read safely, the account is not registered.

See [ADR 0012](adr/0012-production-dependency-graph.md) for the dependency graph, licences and native libraries, and the [production engine receipt](receipts/production-engine-2026-10-13.md) for runtime evidence.

### Porting and UI changes (Rust)

- **macOS/Windows**: core, media, telephony and storage are portable Rust. Porting needs the cpal backend for the platform (already provided by cpal), platform paths/permissions in config, and packaging. Nothing in the engine depends on Linux APIs.
- **UI contract errors**: adapters keep typed errors (`SecretError`, `InstanceError`, `StorageError`, ...); what crosses into the window through `Backend` jobs is already user-safe text, so the UI never formats adapter internals.
- **Another UI toolkit** (for example Slint or a webview shell): implement the same `Backend` contract — send `Command`, poll `Event` — and keep the reducer in `apps/rust-microsip/src/ui/state.rs` as the model. The engine, audio and storage do not change.
- **Agent CLI/MCP and web**: separate OpenSpec changes reuse `core` and the engine through the same command/event contract.
