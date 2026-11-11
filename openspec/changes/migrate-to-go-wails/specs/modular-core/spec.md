# Spec Delta

## Purpose

Defines the GUI-free, platform-neutral core of go-microsip and the contracts every front end and OS adapter uses, so telephony, media, configuration and storage can be reused by the desktop app, the CLI, the MCP server and a future macOS build.

## ADDED Requirements

### Requirement: GUI-free portable core
The core SHALL contain the domain model, SIP engine, media, configuration and storage, and SHALL NOT depend on the desktop toolkit, on a front end, or on an operating-system-specific API. An automated check SHALL fail the build when this boundary is broken.

#### Scenario: Core imports the desktop toolkit
- **WHEN** a core package gains an import of the desktop toolkit or of a front-end package
- **THEN** the layering check fails and names the offending file and import

#### Scenario: C interoperability confined to audio and the webview
- **WHEN** the core, the CLI, the MCP server and the control packages are compiled with C interoperability disabled
- **THEN** they compile, and the layering check fails on any project file that uses C interoperability outside the audio adapter and the desktop window's webview layer

#### Scenario: Core builds for an unsupported desktop
- **WHEN** the core packages are compiled for macOS without any GUI or OS adapter
- **THEN** the compilation succeeds

### Requirement: One command and event contract for every front end
The desktop window, the CLI and the MCP server SHALL drive the application only through one command/event contract owned by the core. Front ends SHALL NOT reach SIP, media, audio, storage or secret internals directly, and secrets SHALL be redacted from every command, event and log.

#### Scenario: Same call from two front ends
- **WHEN** a call is placed from the window and later from the CLI against the same lab extension
- **THEN** both produce the same sequence of call states and the same history record shape

#### Scenario: Front end reaches an internal
- **WHEN** a front-end package imports the storage, SIP, media or audio internals directly
- **THEN** the layering check fails and names the offending file and import

#### Scenario: Password in an event
- **WHEN** an account with a password registers with tracing enabled
- **THEN** no command, event or log line contains the password

### Requirement: Operating-system adapters behind core interfaces
Audio devices, secret storage, notifications, tray, single instance, URI handling and private-file checks SHALL be provided by per-OS adapters implementing interfaces the core owns. A missing adapter capability SHALL be reported as unavailable, never simulated.

#### Scenario: Desktop without a secret service
- **WHEN** the application starts on a Linux session with no secret service
- **THEN** saving a password to the keyring is shown as unavailable and no password is written to a file or to the database

#### Scenario: Session without a supported audio service
- **WHEN** the application starts on a session that offers no supported audio service
- **THEN** the doctor output and the window report that audio is unavailable and name the missing service, and no call is presented as having audio

#### Scenario: Adapter replaced in a test
- **WHEN** the core test suite runs with in-memory adapters
- **THEN** registration, call and storage tests pass without a sound device, a display or D-Bus

### Requirement: Telephony behavior parity
The core SHALL provide one enabled SIP account and one simultaneous call over UDP, TCP and TLS with registration, outgoing, incoming, reject, hangup, DTMF, mute, hold, DND, blind transfer and bounded reconnection, with the behavior the Rust candidate's `sip-telephony` requirements define.

#### Scenario: Outgoing call life cycle
- **WHEN** a registered account dials a lab extension that answers and then hangs up
- **THEN** the call passes through dialing, ringing, active and ended, and the final state is delivered to every front end

#### Scenario: Request from an unexpected peer
- **WHEN** a SIP request arrives from an address that is not the configured registrar or proxy
- **THEN** it is rejected and no call state changes

### Requirement: Media and secure-media parity
The core SHALL carry two-way G.711 PCMA/PCMU audio over RTP with RFC 4733 DTMF and SDES-SRTP, and SHALL offer or accept SRTP keys only over validated TLS. An account configured for secure media SHALL reject a plaintext media offer, and RTP SHALL be accepted only from the negotiated address.

#### Scenario: Two-way audio is measured
- **WHEN** a lab call is established with each codec in plain and secure modes
- **THEN** a tone sent in each direction is detected at the other end and the receipt records codec, mode and both measurements

#### Scenario: Tampered or replayed secure media
- **WHEN** a replayed or modified SRTP or SRTCP packet is injected into a secure lab call
- **THEN** the packet is discarded and counted, and it is neither decoded nor played

#### Scenario: Certificate outside the configured roots
- **WHEN** a TLS account with a configured CA bundle meets a server certificate that is trusted by the system store but not by the bundle, or that names another host
- **THEN** registration is refused and no credentials or media keys are sent

#### Scenario: Plaintext offer to a secure account
- **WHEN** a secure-media account receives an offer without SRTP keys
- **THEN** the call is refused and no audio is sent or played

### Requirement: Real-time audio path stays bounded
The audio device path SHALL never block, allocate unboundedly or wait on the user interface, and a stalled or closed front end SHALL NOT interrupt an active call's audio. On the lab network, at most 0.5 % of audio frames SHALL be late or lost at the device over a five-minute call, with no playback gap longer than 60 ms.

#### Scenario: Five-minute call within the threshold
- **WHEN** a five-minute lab call runs on Linux and on Windows
- **THEN** the measured late or lost frames and the longest gap are within the threshold and are recorded in the receipt

#### Scenario: Window is busy during a call
- **WHEN** the window is frozen for five seconds during an active lab call
- **THEN** audio continues in both directions within the threshold and the call state is correct when the window resumes

#### Scenario: Device disappears
- **WHEN** the selected capture device is unplugged during a call
- **THEN** the call stays up, the loss is reported, and no new microphone is used without the user's confirmation
