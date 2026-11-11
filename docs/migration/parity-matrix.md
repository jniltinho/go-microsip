# Parity matrix: Rust candidate to go-microsip

Baseline: the Rust-era delta specs at tag `v0.1.0-rc.3` (commit `a75a601`). This file is the working record for the parity gate of `migrate-to-go-wails` (see its `implementation-migration` spec, "Recorded parity gate").

Rules: a row is **proven** only with runtime evidence linked in the Evidence column (a receipt, a test name plus its run, or a lab report); **dropped** only with the owner's written approval linked there. A review, a build or a screenshot is not call evidence. Empty status means not yet verified.

`add-web-wasm-softphone` is not part of the baseline: nothing of it was implemented in the Rust candidate and it is on hold.

## Part A: behavioral baseline

57 requirements and 83 scenarios in 14 spec files (140 rows).

| # | Change | Capability | Requirement | Scenario | Status | Go evidence |
| --- | --- | --- | --- | --- | --- | --- |
| A1 | add-agent-cli-mcp | agent-cli-mcp | Single owner and local modes | (requirement) |  |  |
| A2 | add-agent-cli-mcp | agent-cli-mcp | Single owner and local modes | AG-01 multiple clients |  |  |
| A3 | add-agent-cli-mcp | agent-cli-mcp | Structured CLI and real commands | (requirement) |  |  |
| A4 | add-agent-cli-mcp | agent-cli-mcp | Structured CLI and real commands | AG-02 CLI contract |  |  |
| A5 | add-agent-cli-mcp | agent-cli-mcp | Interoperable stdio MCP | (requirement) |  |  |
| A6 | add-agent-cli-mcp | agent-cli-mcp | Interoperable stdio MCP | AG-03 protocol and limits |  |  |
| A7 | add-agent-cli-mcp | agent-cli-mcp | Local authorization and data protection | (requirement) |  |  |
| A8 | add-agent-cli-mcp | agent-cli-mcp | Local authorization and data protection | AG-04 deny and grant |  |  |
| A9 | add-agent-cli-mcp | agent-cli-mcp | Idempotency, concurrency, audit | (requirement) |  |  |
| A10 | add-agent-cli-mcp | agent-cli-mcp | Idempotency, concurrency, audit | AG-05 replay and race |  |  |
| A11 | add-agent-cli-mcp | agent-cli-mcp | Proven telephony and independent client shutdown | (requirement) |  |  |
| A12 | add-agent-cli-mcp | agent-cli-mcp | Proven telephony and independent client shutdown | AG-06 real operation and disconnection |  |  |
| A13 | add-windows-port | agent-cli-mcp | Windows control channel | (requirement) |  |  |
| A14 | add-windows-port | agent-cli-mcp | Windows control channel | WIN-10 other user |  |  |
| A15 | add-windows-port | agent-cli-mcp | Windows control channel | WIN-10 approval still required |  |  |
| A16 | add-windows-port | agent-cli-mcp | Windows console executable | (requirement) |  |  |
| A17 | add-windows-port | agent-cli-mcp | Windows console executable | WIN-10 MCP client on Windows |  |  |
| A18 | add-windows-port | audio-media | Windows audio through WASAPI shared mode | (requirement) |  |  |
| A19 | add-windows-port | audio-media | Windows audio through WASAPI shared mode | WIN-08 five-minute call on Windows |  |  |
| A20 | add-windows-port | audio-media | Windows audio through WASAPI shared mode | WIN-08 device renamed or unplugged |  |  |
| A21 | add-windows-port | audio-media | Windows audio through WASAPI shared mode | WIN-08 microphone blocked |  |  |
| A22 | add-windows-port | config-storage | Windows folders and precedence | (requirement) |  |  |
| A23 | add-windows-port | config-storage | Windows folders and precedence | WIN-09 first run |  |  |
| A24 | add-windows-port | config-storage | Windows folders and precedence | WIN-09 cross-OS import |  |  |
| A25 | add-windows-port | config-storage | Windows folders and precedence | WIN-09 invalid folder variable |  |  |
| A26 | add-windows-port | reproducible-distribution | Windows cross build in Docker | (requirement) |  |  |
| A27 | add-windows-port | reproducible-distribution | Windows cross build in Docker | WIN-11 build on a Linux host |  |  |
| A28 | add-windows-port | reproducible-distribution | Windows release artifacts | (requirement) |  |  |
| A29 | add-windows-port | reproducible-distribution | Windows release artifacts | WIN-12 per-user MSI |  |  |
| A30 | add-windows-port | reproducible-distribution | Windows release artifacts | WIN-12 scanner on Windows artifacts |  |  |
| A31 | add-windows-port | reproducible-distribution | Windows quality gates | (requirement) |  |  |
| A32 | add-windows-port | reproducible-distribution | Windows quality gates | WIN-13 Windows regression |  |  |
| A33 | add-windows-port | reproducible-distribution | Windows quality gates | WIN-13 Linux release without Windows gates |  |  |
| A34 | add-windows-port | windows-integration | Supported Windows versions without runtimes | (requirement) |  |  |
| A35 | add-windows-port | windows-integration | Supported Windows versions without runtimes | WIN-01 clean machine |  |  |
| A36 | add-windows-port | windows-integration | Single instance and forwarding on Windows | (requirement) |  |  |
| A37 | add-windows-port | windows-integration | Single instance and forwarding on Windows | WIN-02 second launch with a link |  |  |
| A38 | add-windows-port | windows-integration | Single instance and forwarding on Windows | WIN-02 impostor pipe |  |  |
| A39 | add-windows-port | windows-integration | Notifications and tray on Windows | (requirement) |  |  |
| A40 | add-windows-port | windows-integration | Notifications and tray on Windows | WIN-03 answer from a toast |  |  |
| A41 | add-windows-port | windows-integration | Notifications and tray on Windows | WIN-03 Focus Assist on |  |  |
| A42 | add-windows-port | windows-integration | Opt-in links and autostart on Windows | (requirement) |  |  |
| A43 | add-windows-port | windows-integration | Opt-in links and autostart on Windows | WIN-04 malicious link |  |  |
| A44 | add-windows-port | windows-integration | Opt-in links and autostart on Windows | WIN-04 removal |  |  |
| A45 | add-windows-port | windows-integration | Session end and quit on Windows | (requirement) |  |  |
| A46 | add-windows-port | windows-integration | Session end and quit on Windows | WIN-05 sign-out during a call |  |  |
| A47 | add-windows-port | windows-integration | Secrets and private data on Windows | (requirement) |  |  |
| A48 | add-windows-port | windows-integration | Secrets and private data on Windows | WIN-06 another user |  |  |
| A49 | add-windows-port | windows-integration | Secrets and private data on Windows | WIN-06 Credential Manager unavailable |  |  |
| A50 | add-windows-port | windows-integration | Unsafe code confinement | (requirement) |  |  |
| A51 | add-windows-port | windows-integration | Unsafe code confinement | WIN-07 unsafe outside the adapter |  |  |
| A52 | create-native-rust-linux-release | audio-media | RTP and essential codecs | (requirement) |  |  |
| A53 | create-native-rust-linux-release | audio-media | RTP and essential codecs | AUD-01 bidirectional voice |  |  |
| A54 | create-native-rust-linux-release | audio-media | RTP and essential codecs | AUD-02 degraded network and incompatible codec |  |  |
| A55 | create-native-rust-linux-release | audio-media | Devices and hotplug | (requirement) |  |  |
| A56 | create-native-rust-linux-release | audio-media | Devices and hotplug | AUD-03 selection and headset removal |  |  |
| A57 | create-native-rust-linux-release | audio-media | Mute and local testing | (requirement) |  |  |
| A58 | create-native-rust-linux-release | audio-media | Mute and local testing | AUD-04 test and mute |  |  |
| A59 | create-native-rust-linux-release | audio-media | Secure media without downgrade | (requirement) |  |  |
| A60 | create-native-rust-linux-release | audio-media | Secure media without downgrade | AUD-05 mandatory SRTP |  |  |
| A61 | create-native-rust-linux-release | audio-media | NAT with explicit limits | (requirement) |  |  |
| A62 | create-native-rust-linux-release | audio-media | NAT with explicit limits | AUD-06 supported topology |  |  |
| A63 | create-native-rust-linux-release | config-storage | Deterministic precedence | (requirement) |  |  |
| A64 | create-native-rust-linux-release | config-storage | Deterministic precedence | CFG-01 precedence and provenance |  |  |
| A65 | create-native-rust-linux-release | config-storage | Deterministic precedence | CFG-02 invalid file |  |  |
| A66 | create-native-rust-linux-release | config-storage | Persistence and editing | (requirement) |  |  |
| A67 | create-native-rust-linux-release | config-storage | Persistence and editing | CFG-03 durable setting |  |  |
| A68 | create-native-rust-linux-release | config-storage | Persistence and editing | CFG-04 active external override |  |  |
| A69 | create-native-rust-linux-release | config-storage | Secrets outside the database | (requirement) |  |  |
| A70 | create-native-rust-linux-release | config-storage | Secrets outside the database | CFG-05 credentials and missing keyring |  |  |
| A71 | create-native-rust-linux-release | config-storage | Secrets outside the database | CFG-06 insecure secret file |  |  |
| A72 | create-native-rust-linux-release | config-storage | Migrations and failures preserve data | (requirement) |  |  |
| A73 | create-native-rust-linux-release | config-storage | Migrations and failures preserve data | CFG-07 interrupted migration |  |  |
| A74 | create-native-rust-linux-release | config-storage | Migrations and failures preserve data | CFG-08 unavailable or future database |  |  |
| A75 | create-native-rust-linux-release | config-storage | Deliberate import and export | (requirement) |  |  |
| A76 | create-native-rust-linux-release | config-storage | Deliberate import and export | CFG-09 import without touching legacy data |  |  |
| A77 | create-native-rust-linux-release | contacts-history | Local contacts and favorites | (requirement) |  |  |
| A78 | create-native-rust-linux-release | contacts-history | Local contacts and favorites | CH-01 contact and favorite |  |  |
| A79 | create-native-rust-linux-release | contacts-history | Validated atomic CSV | (requirement) |  |  |
| A80 | create-native-rust-linux-release | contacts-history | Validated atomic CSV | CH-02 invalid and duplicate CSV |  |  |
| A81 | create-native-rust-linux-release | contacts-history | Coherent history | (requirement) |  |  |
| A82 | create-native-rust-linux-release | contacts-history | Coherent history | CH-03 outcomes and recovery |  |  |
| A83 | create-native-rust-linux-release | contacts-history | Search and privacy | (requirement) |  |  |
| A84 | create-native-rust-linux-release | contacts-history | Search and privacy | CH-04 search and retention |  |  |
| A85 | create-native-rust-linux-release | linux-integration | Optional notifications and tray | (requirement) |  |  |
| A86 | create-native-rust-linux-release | linux-integration | Optional notifications and tray | LIN-01 desktop without tray |  |  |
| A87 | create-native-rust-linux-release | linux-integration | Confirmed external URI | (requirement) |  |  |
| A88 | create-native-rust-linux-release | linux-integration | Confirmed external URI | LIN-02 legitimate and malicious links |  |  |
| A89 | create-native-rust-linux-release | linux-integration | Local single instance | (requirement) |  |  |
| A90 | create-native-rust-linux-release | linux-integration | Local single instance | LIN-03 second instance |  |  |
| A91 | create-native-rust-linux-release | linux-integration | Safe close and quit | (requirement) |  |  |
| A92 | create-native-rust-linux-release | linux-integration | Safe close and quit | LIN-04 exit during a call |  |  |
| A93 | create-native-rust-linux-release | native-desktop | Native interface and reference layout | (requirement) |  |  |
| A94 | create-native-rust-linux-release | native-desktop | Native interface and reference layout | ND-01 comparable layout |  |  |
| A95 | create-native-rust-linux-release | native-desktop | Native interface and reference layout | ND-02 feature outside the release |  |  |
| A96 | create-native-rust-linux-release | native-desktop | Persistent themes | (requirement) |  |  |
| A97 | create-native-rust-linux-release | native-desktop | Persistent themes | ND-03 theme switching |  |  |
| A98 | create-native-rust-linux-release | native-desktop | Persistent themes | ND-04 system theme |  |  |
| A99 | create-native-rust-linux-release | native-desktop | Keyboard operation and scaling | (requirement) |  |  |
| A100 | create-native-rust-linux-release | native-desktop | Keyboard operation and scaling | ND-05 keyboard and HiDPI |  |  |
| A101 | create-native-rust-linux-release | native-desktop | Visible states and failures | (requirement) |  |  |
| A102 | create-native-rust-linux-release | native-desktop | Visible states and failures | ND-06 registration failure |  |  |
| A103 | create-native-rust-linux-release | reproducible-distribution | Executable with embedded resources | (requirement) |  |  |
| A104 | create-native-rust-linux-release | reproducible-distribution | Executable with embedded resources | DIST-01 clean machine |  |  |
| A105 | create-native-rust-linux-release | reproducible-distribution | Additional AppImage does not hide dependencies | (requirement) |  |  |
| A106 | create-native-rust-linux-release | reproducible-distribution | Additional AppImage does not hide dependencies | DIST-02 separate ELF and AppImage |  |  |
| A107 | create-native-rust-linux-release | reproducible-distribution | Docker build without host toolchain | (requirement) |  |  |
| A108 | create-native-rust-linux-release | reproducible-distribution | Docker build without host toolchain | DIST-03 clean-clone build |  |  |
| A109 | create-native-rust-linux-release | reproducible-distribution | Disposable Asterisk lab | (requirement) |  |  |
| A110 | create-native-rust-linux-release | reproducible-distribution | Disposable Asterisk lab | DIST-04 lab and isolation |  |  |
| A111 | create-native-rust-linux-release | reproducible-distribution | Disposable Asterisk lab | DIST-05 manual desktop profile |  |  |
| A112 | create-native-rust-linux-release | reproducible-distribution | Real gates and independent review | (requirement) |  |  |
| A113 | create-native-rust-linux-release | reproducible-distribution | Real gates and independent review | DIST-06 incomplete release blocked |  |  |
| A114 | create-native-rust-linux-release | reproducible-distribution | Traceable, private-by-default publication | (requirement) |  |  |
| A115 | create-native-rust-linux-release | reproducible-distribution | Traceable, private-by-default publication | DIST-07 pre-push audit |  |  |
| A116 | create-native-rust-linux-release | sip-telephony | Registration and transports | (requirement) |  |  |
| A117 | create-native-rust-linux-release | sip-telephony | Registration and transports | SIP-01 registration and renewal |  |  |
| A118 | create-native-rust-linux-release | sip-telephony | Registration and transports | SIP-02 invalid configuration and authentication |  |  |
| A119 | create-native-rust-linux-release | sip-telephony | Call lifecycle | (requirement) |  |  |
| A120 | create-native-rust-linux-release | sip-telephony | Call lifecycle | SIP-03 calls in both directions |  |  |
| A121 | create-native-rust-linux-release | sip-telephony | Call lifecycle | SIP-04 cancellation and answer race |  |  |
| A122 | create-native-rust-linux-release | sip-telephony | Call lifecycle | SIP-05 rejection and second call |  |  |
| A123 | create-native-rust-linux-release | sip-telephony | Call controls | (requirement) |  |  |
| A124 | create-native-rust-linux-release | sip-telephony | Call controls | SIP-06 DTMF and hold |  |  |
| A125 | create-native-rust-linux-release | sip-telephony | Call controls | SIP-07 do not disturb |  |  |
| A126 | create-native-rust-linux-release | sip-telephony | Confirmed blind transfer | (requirement) |  |  |
| A127 | create-native-rust-linux-release | sip-telephony | Confirmed blind transfer | SIP-08 transfer and failure |  |  |
| A128 | create-native-rust-linux-release | sip-telephony | Signaling security and recovery | (requirement) |  |  |
| A129 | create-native-rust-linux-release | sip-telephony | Signaling security and recovery | SIP-09 invalid certificate |  |  |
| A130 | create-native-rust-linux-release | sip-telephony | Signaling security and recovery | SIP-10 network and suspend |  |  |
| A131 | refine-native-ui | native-ui-refinement | Square-cornered visual language | (requirement) |  |  |
| A132 | refine-native-ui | native-ui-refinement | Square-cornered visual language | UIR-01 no rounded corners |  |  |
| A133 | refine-native-ui | native-ui-refinement | Square-cornered visual language | UIR-02 token contrast |  |  |
| A134 | refine-native-ui | native-ui-refinement | Minimalist icons with labels | (requirement) |  |  |
| A135 | refine-native-ui | native-ui-refinement | Minimalist icons with labels | UIR-03 icons present and accessible |  |  |
| A136 | refine-native-ui | native-ui-refinement | MicroSIP-compatible refined layout | (requirement) |  |  |
| A137 | refine-native-ui | native-ui-refinement | MicroSIP-compatible refined layout | UIR-04 comparable to MicroSIP |  |  |
| A138 | refine-native-ui | native-ui-refinement | MicroSIP-compatible refined layout | UIR-05 call security is always visible |  |  |
| A139 | refine-native-ui | native-ui-refinement | Visual regression and scaling | (requirement) |  |  |
| A140 | refine-native-ui | native-ui-refinement | Visual regression and scaling | UIR-06 snapshots and scale |  |  |

## Part B: code-surface inventory

Extracted on 2026-11-11 from a `codebase-memory` index of the repository (project generation `2026-11-11T11:38:46Z`, index mode full) and then read from source. Paths are repository-relative. A row is closed when the Go counterpart exists and its evidence is linked; a deliberate difference is recorded in the Go column and needs the owner's acceptance in the parity receipt.

### B1. Commands (front end to engine) — `crates/core/src/lib.rs:338-365`

| # | Rust item | Notes | Go counterpart | Evidence |
| --- | --- | --- | --- | --- |
| B1.1 | `Command::Register(AccountSettings, SecretString)` | password redacted from `Debug` |  |  |
| B1.2 | `Command::Unregister` |  |  |  |
| B1.3 | `Command::Dial(DialTarget)` | target 1..64 of alnum `*#+-_.`, `+` only first |  |  |
| B1.4 | `Command::Answer(CallId)` |  |  |  |
| B1.5 | `Command::Reject(CallId)` | 486 Busy Here |  |  |
| B1.6 | `Command::Hangup(CallId)` | cancel, reject or hang up |  |  |
| B1.7 | `Command::Dtmf(CallId, DtmfKey)` | RFC 4733; `0-9 * # A-D` |  |  |
| B1.8 | `Command::Hold(CallId)` |  |  |  |
| B1.9 | `Command::Resume(CallId)` |  |  |  |
| B1.10 | `Command::Transfer(CallId, DialTarget)` | blind transfer |  |  |
| B1.11 | `Command::SetMute(CallId, bool)` |  |  |  |
| B1.12 | `Command::SetDnd(bool)` | new incoming calls get 486 |  |  |
| B1.13 | `Command::Shutdown` | bounded stop |  |  |
| B1.14 | `AccountSettings` + `validate` (`lib.rs:68-136`) | display_name, username, auth_username, domain, server, port, transport, media, expires; SRTP requires TLS |  |  |
| B1.15 | `Transport` udp/tcp/tls, `MediaSecurity` PlainRtp/Srtp, `ValidationError` (5 variants) | default ports 5060/5060/5061 |  |  |

### B2. Events (engine to front end) — `crates/core/src/lib.rs:209-444`

| # | Rust item | Notes | Go counterpart | Evidence |
| --- | --- | --- | --- | --- |
| B2.1 | `Event::Registration(RegistrationState)` | Disabled, Registering, Registered, Failed(Unauthorized/Timeout/Certificate/Network/Rejected(code)) |  |  |
| B2.2 | `Event::Call(CallSnapshot)` | id, direction, remote, phase, muted, secure, transfer, version |  |  |
| B2.3 | `Event::Notice(Notice)` | Busy, NotRegistered, UnknownCall, InvalidState, AudioUnavailable, Overloaded, EngineFailed |  |  |
| B2.4 | `Event::Stopped` | always delivered after `Shutdown` |  |  |
| B2.5 | `CallPhase` + `can_transition_to` (`lib.rs:271-298`) | Dialing, Ringing, Established, Held, Terminating, Ended(EndReason) |  |  |
| B2.6 | `EndReason` | Normal, Canceled, Rejected, Busy, NoAnswer, Transferred, NotAcceptable, Failed |  |  |
| B2.7 | `TransferState` | Pending, Succeeded, Failed(code) |  |  |
| B2.8 | `MediaIo` trait (`lib.rs:424-444`) | capture/play 160-sample frames, start, stop |  |  |

### B3. Command line — `crates/config/src/cli.rs`, `apps/rust-microsip/src/{main,cli,service}.rs`

| # | Rust item | Notes | Go counterpart | Evidence |
| --- | --- | --- | --- | --- |
| B3.1 | no command: open the window | exit codes 0/1/2/4, 130/143 on signals |  |  |
| B3.2 | positional `sip:`/`tel:` link | fills the number, never dials; parsing rules in `crates/platform/src/uri.rs:48-77` |  |  |
| B3.3 | `--config PATH`, `--env-file PATH` | `=` form accepted for these two only |  |  |
| B3.4 | `--doctor` [`--json`] | sanitized report, exit 0 even when invalid |  |  |
| B3.5 | `-h/--help`, `-V/--version` |  |  |  |
| B3.6 | `config validate` | exit 0/1 |  |  |
| B3.7 | `config export` | TOML without secrets |  |  |
| B3.8 | `config import FILE` [`--apply`] | preview, then one-transaction replace |  |  |
| B3.9 | password guard | any `-…pass…` option is refused, exit 2, arguments never echoed |  |  |
| B3.10 | `status`, `operation get ID`, `devices list`, `config show` | control reads |  |  |
| B3.11 | `contacts search [TEXT]`, `history list` | `--limit`, `--cursor`, `--filter all/incoming/outgoing/missed` |  |  |
| B3.12 | `call dial TARGET\|-` | `-` reads one line from stdin |  |  |
| B3.13 | `call answer/reject/hangup CALL_ID VERSION` |  |  |  |
| B3.14 | `call mute/unmute/hold/resume CALL_ID VERSION` |  |  |  |
| B3.15 | `call dtmf CALL_ID VERSION DIGITS\|-`, `call transfer CALL_ID VERSION TARGET\|-` |  |  |  |
| B3.16 | `dnd on\|off` |  |  |  |
| B3.17 | control options `--json`, `--key`, `--grant-file FILE` | grant file: private, at most 512 bytes |  |  |
| B3.18 | control exit codes | 0 done/accepted, 2 invalid, 3 not authorized or awaiting approval, 4 unavailable, 5 timeout, 1 internal |  |  |
| B3.19 | `mcp --stdio` [`--grant-file FILE`] | exit 0/1/2 |  |  |
| B3.20 | `service --grant-file FILE [--config] [--env-file]` | headless owner; exit 0/1/2/3/4 |  |  |
| B3.21 | second binary `rust-microsip-cli` | same commands, console subsystem on Windows |  |  |

### B4. MCP tools — `apps/rust-microsip/src/mcp.rs:77-223`

| # | Tool | Kind and authorization | Go counterpart | Evidence |
| --- | --- | --- | --- | --- |
| B4.1 | `status_get` | read, no grant |  |  |
| B4.2 | `operation_get` | read, no grant |  |  |
| B4.3 | `devices_list` | read, `read:devices` |  |  |
| B4.4 | `contacts_search` | read, `read:contacts` |  |  |
| B4.5 | `history_list` | read, `read:history` |  |  |
| B4.6 | `config_show` | read, `read:config` |  |  |
| B4.7 | `call_dial` | effect; approval or `control:dial` with the exact destination and quota |  |  |
| B4.8 | `call_answer` | effect; approval or `control:answer` with the caller listed and quota |  |  |
| B4.9 | `call_reject` | effect; approval or `control:reject` |  |  |
| B4.10 | `call_hangup` | effect; approval or `control:hangup` |  |  |
| B4.11 | `call_dtmf` | effect; approval or `control:dtmf` |  |  |
| B4.12 | `call_mute` | effect; approval or `control:mute` |  |  |
| B4.13 | `call_hold` | effect; approval or `control:hold` |  |  |
| B4.14 | `call_transfer` | effect; approval or `control:transfer` with the exact destination |  |  |
| B4.15 | `dnd_set` | effect; approval or `control:dnd` |  |  |
| B4.16 | server limits | 1 MiB input line, 10 tool calls per second, strict schemas |  |  |

### B5. Local control protocol — `crates/control/src/lib.rs`, `crates/platform/src/control.rs`

| # | Rust item | Notes | Go counterpart | Evidence |
| --- | --- | --- | --- | --- |
| B5.1 | `Hello`/`Welcome`, `OwnerMode` gui/service, epoch | protocol name and schema version 1 | wire format may differ (design D10) |  |
| B5.2 | `Request` envelope and `validate` | request id, idempotency key required for mutations, capability redacted |  |  |
| B5.3 | 15 operations | the same set as the MCP tools (B4.1-B4.15) |  |  |
| B5.4 | `Response`, `Status` ok/accepted/approval_required/error |  |  |  |
| B5.5 | 18 error codes with exit codes and retryable flag | validation, grant_required, approval_denied, approval_expired, grant_expired, engine_unavailable, engine_owned_by_service, timeout, conflict, stale_epoch, result_expired, not_found, rate_limited, busy, storage_unavailable, invalid_state, unsupported, internal |  |  |
| B5.6 | Unix transport | private runtime directory, socket 0600, peer-credential check both sides |  |  |
| B5.7 | Windows transport | per-user named pipes (`crates/platform-windows/src/pipes.rs`) |  |  |
| B5.8 | transport limits | 16 connections, 10/s per connection, 50/s global, 5 s reply deadline, 1 MiB message |  |  |
| B5.9 | 13 grant scopes | read:contacts, read:history, read:devices, read:config, control:dial/answer/reject/hangup/dtmf/mute/hold/transfer/dnd |  |  |
| B5.10 | approvals | 60 s window, at most 8 pending, pause after deny-all |  |  |
| B5.11 | idempotency | same key and payload reports without repeating; different payload is `conflict`; earlier epoch is `stale_epoch` |  |  |
| B5.12 | operation ledger states | awaiting_approval, executing, succeeded, failed, denied, expired, outcome_unknown |  |  |
| B5.13 | grant issue limits | validity 1..60 minutes, call quota up to 100, capability stored only as a hash |  |  |
| B5.14 | single-instance protocol | `activate`, `dial <target>`; acks ok/busy/invalid/service; `InstanceError` (5 variants) |  |  |

### B6. Configuration keys — `crates/config/src/lib.rs:110-207, 717-782`

Precedence per field: flag > environment > `.env` > TOML > SQLite > default.

| # | Key | Type and default | Go counterpart | Evidence |
| --- | --- | --- | --- | --- |
| B6.1 | `schema` | integer, must be 1 |  |  |
| B6.2 | `account.display_name` | string, empty |  |  |
| B6.3 | `account.username` | string, required |  |  |
| B6.4 | `account.auth_username` | string, optional |  |  |
| B6.5 | `account.domain` | string, required |  |  |
| B6.6 | `account.server` | string, default the domain |  |  |
| B6.7 | `account.port` | integer, 5060 or 5061 for TLS |  |  |
| B6.8 | `account.transport` | `udp`/`tcp`/`tls`, default `udp` |  |  |
| B6.9 | `account.media` | `rtp`/`srtp`, default `rtp`; `srtp` requires `tls` |  |  |
| B6.10 | `account.expires` | integer 1..86400, default 300 |  |  |
| B6.11 | `account.password_env` | variable name, default `RUST_MICROSIP_SIP_PASSWORD` | default becomes `GO_MICROSIP_SIP_PASSWORD` |  |
| B6.12 | `account.password_file` | absolute path, optional |  |  |
| B6.13 | `account.ca_file` | absolute PEM path, optional; replaces system roots |  |  |
| B6.14 | `account.password` | TOML literal only, never saved or exported, file must be private |  |  |
| B6.15 | `ui.theme` | `system`/`light`/`dark`, default `dark` | values grow to the seven skins (design D7) |  |
| B6.16 | `ui.dnd` | boolean, default false |  |  |
| B6.17 | `audio.input_device`, `audio.output_device` | string, optional |  |  |
| B6.18 | file rules | size limits, ownership and permission checks (`crates/config/src/files.rs:71-220`) |  |  |
| B6.19 | password source order | env var, `.env`, password file, TOML literal, then prompt or keyring |  |  |
| B6.20 | write-back rule | only changed fields not held by a higher layer go to SQLite |  |  |
| B6.21 | doctor report shape (text and JSON) | `crates/config/src/doctor.rs:69-141` |  |  |
| B6.22 | SQLite storage of settings and `secret_ref` | `crates/storage/src/settings.rs:16-233` |  |  |

### B7. Environment variables — `crates/config/src/lib.rs:189-194, 517-543`

| # | Variable | Maps to | Go counterpart | Evidence |
| --- | --- | --- | --- | --- |
| B7.1 | `RUST_MICROSIP_ACCOUNT_{DISPLAY_NAME,USERNAME,AUTH_USERNAME,DOMAIN,SERVER,PORT,TRANSPORT,MEDIA,EXPIRES,PASSWORD_ENV,PASSWORD_FILE,CA_FILE}` | the `account.*` keys | prefix `GO_MICROSIP_` (design D8) |  |
| B7.2 | `RUST_MICROSIP_UI_{THEME,DND}` | `ui.*` | prefix `GO_MICROSIP_` |  |
| B7.3 | `RUST_MICROSIP_AUDIO_{INPUT_DEVICE,OUTPUT_DEVICE}` | `audio.*` | prefix `GO_MICROSIP_` |  |
| B7.4 | `RUST_MICROSIP_SIP_PASSWORD` | default password variable | `GO_MICROSIP_SIP_PASSWORD` |  |
| B7.5 | `RUST_MICROSIP_TRACE` | sanitized engine diagnostics | `GO_MICROSIP_TRACE` |  |
| B7.6 | `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_RUNTIME_DIR`, `HOME` | configuration, data and runtime directories |  |  |
| B7.7 | `APPDATA`, `LOCALAPPDATA` | Windows directories |  |  |
| B7.8 | `APPDIR`, `APPIMAGE` | desktop entry target |  |  |
| B7.9 | `.env` handling and syntax | only from `--env-file`; `crates/config/src/dotenv.rs:13-89` |  |  |

### Index coverage for part B

`check_index_coverage` reported no recorded issue and matching freshness for every file cited above: `crates/core/src/lib.rs`; `crates/config/src/{cli,lib,dotenv,files,import,doctor}.rs`; `crates/storage/src/settings.rs`; `crates/control/src/{lib,instance,caller}.rs`; `crates/platform/src/{control,lib,instance,uri}.rs`; `crates/platform-windows/src/pipes.rs`; `crates/telephony/src/engine/mod.rs`; `apps/rust-microsip/src/{main,cli,mcp,service,owner,signals}.rs`, `apps/rust-microsip/src/bin/rust-microsip-cli.rs`, `apps/rust-microsip/src/ui/state/{control,grants}.rs`. The tool is best-effort, so every row was also read from source.

### Open points for the parity gate

- The Go command line is Cobra with subcommands (owner instruction of 2026-11-11): `--doctor` becomes `doctor`, `mcp --stdio` becomes `mcp`, `-V` stays as `--version` plus a `version` command. Rows B3.4, B3.5 and B3.19 close as accepted differences, not as identical syntax.
- Media properties of the Rust SRTP adapter that the Go stack (pion/srtp inside the vendored diago) does not provide, each needing the owner's acceptance or an implementation: the receiver bound to a single SSRC (`authenticated_other_ssrc_cannot_expand_the_bound_receiver`), transmit paths closed before the RTP or SRTCP index space is exhausted (`maximum_*_index_closes_both_transmit_paths_before_reuse`), and the bounded bank of unauthenticated sources (`unauthenticated_source_churn_never_binds_or_retains_provider_sessions`). A DTMF key is 7 packets of 100 ms instead of 8 of 120 ms.
- Configuration (`internal/core/config`, task 3.6) ports the 13 tests of `crates/config` and differs from the Rust candidate in three recorded ways: the names (`go-microsip` directory, `GO_MICROSIP_` prefix; `RUST_MICROSIP_` variables are listed by the doctor as not read, design D8); `ui.theme` accepts seven values and an unknown one resolves to `system` instead of failing (design D7); and the doctor's text prints strings quoted once (`account.domain = "pbx.invalid" [toml]`) and its JSON gains `ignored_legacy_env`. On Windows file ownership and mode are not checked yet, as in the Rust candidate (`internal/platform/files`, to be closed with the Windows adapters).
- Storage (`internal/core/store`, task 3.7) keeps the Rust schema (version 5, the five migrations verbatim) and ports 21 of the 25 tests of `crates/storage`; a database written by `rust-microsip 0.1.0-rc.3` opens unchanged (`TestRustCandidateDatabaseOpens`). Recorded differences: the pre-migration backup is made with `VACUUM INTO` instead of the online backup API (same guarantees: consistent, private, never overwrites); `SetSecretRef("")` clears the reference instead of being an error. The tables of the control ledger, audit and grants exist, but their API and its 4 tests are not ported yet: they belong to the control channel (group 5).
- Six error codes are defined but never constructed in the Rust code (`approval_denied`, `approval_expired`, `grant_expired`, `engine_owned_by_service`, `result_expired`, `unsupported`); decide whether the Go contract keeps them.
- Some JSON values in `status_get`, `config_show` and `history_list` come from Rust debug formatting (for example `config_show.media` yields `plainrtp`, not `rtp`); the Go contract should define these strings deliberately.
- Not inventoried here and to be added before the gate: the window's `Backend` contract, tray and notification actions, the SQLite schema of contacts and history, the Windows pipe naming and file-permission rules, and the storage ledger internals.
