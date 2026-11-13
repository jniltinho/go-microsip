<div align="center">

<img src="apps/rust-microsip/assets/icons/rust-microsip.svg" width="96" alt="rust-microsip logo">

# rust-microsip

**A fast, native SIP softphone for Linux, written in Rust, with the compact MicroSIP layout you already know.**

[![Release](https://img.shields.io/github/v/release/jniltinho/rust-microsip?include_prereleases&sort=semver&color=blue)](https://github.com/jniltinho/rust-microsip/releases)
[![CI](https://github.com/jniltinho/rust-microsip/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/jniltinho/rust-microsip/actions/workflows/ci.yml)
[![License: GPL-3.0-or-later](https://img.shields.io/badge/license-GPL--3.0--or--later-blue)](LICENSE)

[Install](#install) · [Quick start](#quick-start) · [Screenshots](#screenshots) · [CLI and MCP](#cli-and-mcp) · [Build](#build-from-source) · [Docs](#documentation)

</div>

## Why rust-microsip

MicroSIP is loved for being small, fast and simple, but it is a Windows program. rust-microsip
brings the same idea to the Linux desktop as one native binary: no Wine, no Electron, no webview, no
database server. It starts in a fraction of a second, takes little memory, and follows the familiar
layout of keypad, call log, contacts and a status bar that shows whether you are **Online**.

Calls are encrypted when your PBX supports it (TLS signaling with SRTP), passwords can stay in the
desktop keyring, and AI agents can place or answer calls through a local CLI and an MCP server only
with your explicit approval.

rust-microsip is an independent project inspired by [MicroSIP](https://www.microsip.org/). It is not
affiliated with or endorsed by MicroSIP.

## Features

| Area | What you get |
|---|---|
| **Calls** | One SIP account over UDP, TCP or TLS; G.711 (PCMU/PCMA); hold, mute, blind transfer, DND; RFC 4733 DTMF; a ringback tone while the other side rings, or the audio the PBX plays before it answers |
| **Security** | TLS with the CA bundle you name (`ca_file`); SDES-SRTP/SRTCP, optional or mandatory, with downgrade refusal; the password never goes in arguments, logs or the database |
| **Interface** | MicroSIP-like window: keypad, Logs-style call history (double-click to call back), contacts with favorites and CSV import, status bar with an Online icon; dark and light themes |
| **Desktop** | Tray icon whose menu redials your last calls; closing the window keeps it in the tray; incoming-call notifications with Answer/Reject; `sip:`/`tel:` link handler; single instance |
| **Audio** | ALSA, PipeWire and PulseAudio through the system's default devices; audio test with microphone level; devices opened only during calls |
| **Configuration** | Settings in the window, or TOML, `.env` and environment with a clear precedence; `--doctor` shows where every value came from without secrets |
| **Agents** | `rust-microsip` CLI and `mcp --stdio`: every action waits for **Allow** in the window unless you issue a scoped, revocable grant; a headless `service` mode |
| **Diagnostics** | `RUST_MICROSIP_TRACE=1` prints SIP status codes, SDP shape and media counters, without numbers or credentials |

Status: **pre-release** ([latest](https://github.com/jniltinho/rust-microsip/releases)). Calls, audio and
desktop integration are tested against a disposable Asterisk lab and on clean Ubuntu 24.04/26.04, Fedora 44
and Arch Linux desktops. Windows has preview packages (MSI and zip) whose calls are not tested yet. See the
[roadmap](docs/README.md).

## Screenshots

<table>
  <tr>
    <th>Keypad (dark)</th>
    <th>Call log (dark)</th>
    <th>Settings (dark)</th>
  </tr>
  <tr>
    <td><img src="docs/prints/parity/rms-phone-dark.png" width="260" alt="Keypad with large left-aligned digits and an Online status bar, dark theme"></td>
    <td><img src="docs/prints/parity/rms-calls-selected-dark.png" width="260" alt="Call log with direction icons, missed calls in red and row actions, dark theme"></td>
    <td><img src="docs/prints/parity/rms-settings-dark.png" width="260" alt="Account settings with right-aligned labels, dark theme"></td>
  </tr>
  <tr>
    <th>Keypad (light)</th>
    <th>Call log (light)</th>
    <th>Contacts (light)</th>
  </tr>
  <tr>
    <td><img src="docs/prints/parity/rms-phone-light.png" width="260" alt="Keypad, light theme"></td>
    <td><img src="docs/prints/parity/rms-calls-light.png" width="260" alt="Call log, light theme"></td>
    <td><img src="docs/prints/parity/rms-contacts-light.png" width="260" alt="Contacts list, light theme"></td>
  </tr>
</table>

All screens use fictional data. More captures, in both themes and at 150% and 200% scaling, are in
[docs/prints/parity](docs/prints/parity/README.md).

## Install

Download from the [releases page](https://github.com/jniltinho/rust-microsip/releases):

- `rust-microsip-<version>-x86_64.AppImage`: make it executable and run it. It does not need `libfuse2`.
- `rust-microsip-<version>-x86_64`: the same program as a plain ELF.

Verify the download with `sha256sum --check --strict SHA256SUMS`.

**Windows (preview)**: `rust-microsip-<version>-x86_64.msi` installs for your user only, with no
administrator rights. `rust-microsip-<version>-windows-x86_64.zip` is a portable copy. Both are
unsigned, so SmartScreen may warn you; check them with `Get-FileHash <file> -Algorithm SHA256` first.

Requirements: x86_64 Linux with glibc 2.39 or newer (Ubuntu 24.04, Fedora 40, Debian 13, Arch and
later), `libasound.so.2` and, on X11, `libxkbcommon-x11`. Standard GNOME and KDE desktops already have
them; a minimal Xfce install may need `libxkbcommon-x11`.

## Quick start

1. Start rust-microsip and open **▼ → Settings**.
2. Fill in **SIP Server**, **Username**, **Domain** and **Password**, and pick UDP, TCP or TLS. Tick
   "Remember password in the system keyring" to keep it across restarts.
3. Press **Save and register**. The status bar turns green: **Online**.
4. Type a number and press **Call**, or double-click an entry in **Calls**.

Choose your headset in your desktop's sound settings and keep "System default" in rust-microsip.

Prefer a file? Create `$XDG_CONFIG_HOME/rust-microsip/config.toml` (mode 0600):

```toml
schema = 1

[account]
username = "1001"
domain = "sip.example.invalid"
server = "sip.example.invalid"
transport = "tls"        # udp | tcp | tls
media = "srtp"           # rtp | srtp (srtp requires tls)
password_env = "RUST_MICROSIP_SIP_PASSWORD"
```

Export the password in that environment variable before starting; passwords are never accepted as
command-line arguments. `rust-microsip --doctor` prints a sanitized report of the effective
configuration, and `rust-microsip config validate` and `config export` check and print it without
secrets.

## CLI and MCP

While the window runs, other local programs can drive it:

```sh
rust-microsip status
rust-microsip call dial 1002      # waits for "Allow" in the window
rust-microsip mcp --stdio         # MCP server for AI agents
```

Every change waits for **Allow** in the window, unless you issued a scoped, time- and quota-limited
grant in **Settings → Agent access**. `rust-microsip service` runs without a window from a
single-use bootstrap exported there. See [Controlling rust-microsip](docs/agent-cli-mcp-rust.md).

## go-microsip: the Go rewrite (in progress)

rust-microsip is being rewritten in Go with a Wails window as **go-microsip**
([plan](openspec/changes/migrate-to-go-wails/proposal.md),
[tasks](openspec/changes/migrate-to-go-wails/tasks.md), [receipts](docs/receipts/)). Until the
cutover the Rust application above is the product; go-microsip has no release yet and is built
from this repository:

```sh
make go-dist               # dist/go: Linux binary, AppImage, Windows .exe and .zip, notices, SBOM, SHA256SUMS
```

Both applications can be installed and run by the same user: each has its own name, settings,
database, lock and control socket, and neither touches the other's files. Nothing is imported by
itself; to bring the Rust application's account, contacts and history over, with both closed:

```sh
go-microsip import rust-microsip            # a preview; changes nothing
go-microsip import rust-microsip --apply    # --credential also copies the keyring password
```

Platform notes:

- **The AppImage without FUSE.** `go-microsip-<version>-x86_64.AppImage` is run directly
  where FUSE is available. Where it is not (a container, a minimal installation), run it
  with `APPIMAGE_EXTRACT_AND_RUN=1` in the environment, or with `--appimage-extract-and-run`
  as its first argument: it unpacks itself to a temporary folder and runs from there. The
  project's own checks in containers and in the test VMs run it this way.
- **Linux** (Ubuntu 26.04 and later, Debian 13; x86_64). The window uses the system's GTK 4 and
  WebKitGTK 6.0 (`libgtk-4-1`, `libwebkitgtk-6.0-4`), which the default GNOME desktop of both
  already has and another desktop may have to install, and the desktop's sound service (PipeWire
  or PulseAudio). `go-microsip --safe-gpu` starts it without GPU acceleration, which a
  virtual machine may need.
  - A password typed in Settings is kept in the desktop keyring (Secret Service) and nowhere
    else. Without a keyring, or with a locked one, it lasts for the session and is asked for
    again at the next start; that is also the case of a user who logs in without a password.
    `account.password_env` or a private `account.password_file` avoid the prompt.
  - An incoming call rings on the speaker chosen for calls and is shown as a desktop
    notification with **Answer** and **Reject** where the notification service offers buttons.
    With do-not-disturb on, nothing rings.
  - On a desktop with a tray (KDE, Ubuntu's GNOME) the close button hides the window to the tray
    icon, whose menu lists the latest calls; without one, the close button quits, asking first
    during a call.
  - `sip:` and `tel:` links open go-microsip, which asks before it calls. Making it the default
    handler is your choice in the desktop's settings.
  - The command line, the MCP server and the headless service work here:
    [Controlling go-microsip](docs/agent-cli-mcp.md).
- **Windows 11** (preview; x86_64). Windows 10 needs the WebView2 runtime, which Windows 11
  brings, and is not tested. The window opens with its settings,
  contacts and history, and the password is kept in the Credential Manager, where it outlives
  the application's folder. The close button hides the window to the notification area; a
  `sip:` or `tel:` link can be opened with go-microsip, which asks before it calls, once you
  choose it in Settings, Default apps; an incoming call is also a Windows notification with
  **Answer** and **Reject**; and the command line reaches the window as on Linux (the MCP
  server and the headless service are not tried on Windows yet). Calls over UDP and TCP are checked in a Windows VM against a lab PBX, with audio
  as packets both ways; sound on a real sound card, TLS and SRTP are not verified on Windows
  yet. At the first call Windows asks whether its firewall should let go-microsip accept
  connections: calls, incoming ones included, work whichever you answer.
- **macOS**: not built.

Limits of this version, known and accepted for it:

- **The network.** There is no STUN and no SIP keepalive: nothing is sent between two
  renewals of a registration to keep its path open (a call that only receives does send a
  frame of silence a second, see "Audio before the answer"). Calls work with a PBX that keeps the
  audio passing through itself and answers to the address packets come from (symmetric RTP),
  which is how the lab's Asterisk is set and what a default FreePBX extension asks for; a PBX
  that expects the softphone to know its public address is not supported. Behind a home
  router this was not tried yet.
- **One account**, and one call at a time.
- **Sound.** There is no volume in the window (the system's mixer has one for each
  application). A sound device chosen in the settings is used by the next call, and a call in
  progress keeps the ones it began with; the settings' audio test uses it at once: a tone on the chosen speaker and the level of the
  chosen microphone, for ten seconds or until it is stopped, and never during a call. The ring and the ringback are tones the softphone makes itself, on the speaker
  chosen for calls: there is no ringtone file to choose and no speaker of its own for ringing.
- **Audio before the answer.** What a PBX plays before a call is answered (early media: an
  operator's own ringing tone, or a message such as "this number is unavailable") is played
  in place of the softphone's ringback tone. Nothing of the microphone is sent before the
  answer or on hold; one frame of silence a second is. One VPN gateway was seen to close its
  tunnel after about twelve seconds that carried the other side's audio alone, and held
  with this frame: that is one installation's behavior, not a guarantee for others. With an account that
  requires SRTP, audio before the answer that is not encrypted is not played.
- **A call answered on another phone.** When a PBX rings this softphone and another phone
  of the same extension, and the other one answers, the call is not notified as missed and
  is listed as answered, with no duration. This rests on the PBX saying why it stopped
  ringing (the Reason header of its CANCEL, as Asterisk and FreePBX send it); from a PBX
  that does not say, such a call is a missed one.
- **A PBX without RFC 4733 keys.** A call whose answer does not agree to `telephone-event`
  has its audio; keys pressed during it are refused, and nothing is sent in their place.
- **A call nobody answers** ends after 60 seconds.
- **TLS needs a CA file.** A TLS account registers only with a CA bundle named in its
  settings (`ca_file`): the system's certificates are not used, so a PBX whose certificate
  comes from a public authority needs that authority's bundle named too. Without one the
  window says "certificate rejected". The Rust application uses the system's certificates
  when no bundle is named.
- **Signaling from the PBX alone.** Nothing is read that does not come from an address the
  server's name has, at the account's port. The name is resolved when the account connects,
  and again when something arrives from another address at that port. A PBX that answers
  from another port than the account's, or that sends its calls from an address its name
  does not have, is not heard.
- **A server name with several addresses.** Each request is sent to the address the name
  has at that moment, and the cancellation of a call is addressed separately from the call:
  with a name whose answer changes in between, a call canceled while it rings can ring on at
  the server that got it. Not seen with one PBX behind one name.
- **A network that goes away.** When the address the account registered from leaves the
  machine (a VPN that dropped, a cable pulled), the window says so within a few seconds and
  the account registers again by itself once the network is back: tried after 5 s, then at
  doubling delays, eight times; after that, "Register" in the window.
- **A transfer that the PBX accepts and then fails.** FreePBX takes the other party away
  before it reports the failure: go-microsip says the transfer failed and stays in a call
  with nobody in it until you hang up.
- **Secure media.** Unencrypted RTP is used unless SRTP is chosen for the account, and every
  call says which one it has. Three protections of the Rust candidate's own SRTP code are
  not in the library used here: the receiver bound to one source, the stop before the packet
  counter runs out, and the bounded list of unauthenticated sources.
- **Builds.** The build image pins its base by digest but not every package and tool in it.

`go-microsip doctor` prints a sanitized report of the configuration, the display and the sound;
`go-microsip --help` lists every command.

## Build from source

Only Docker with BuildKit is needed; Rust runs inside a pinned builder image, never on the host.

```sh
make dist                  # ELF, AppImage, SHA256SUMS, notices and SBOM in dist/
make check                 # fmt, clippy, tests, rustdoc
make suite                 # end-to-end calls against a disposable Asterisk lab
make windows               # Windows exes, per-user MSI and portable zip (preview)
```

The [development guide](docs/DEVELOPMENT.md) covers the layout, the lab and the release process.

## Documentation

- [Roadmap and receipts](docs/README.md): what is done, with evidence, and what remains.
- [Architecture](docs/architecture.md): crate layers from the domain to the window.
- [Controlling rust-microsip](docs/agent-cli-mcp-rust.md): CLI, MCP, grants and the headless service.
- [Controlling go-microsip](docs/agent-cli-mcp.md): the same for the Go rewrite.
- [Development guide](docs/DEVELOPMENT.md) and [release process](docs/release.md).
- OpenSpec changes: [Linux release](openspec/changes/create-native-rust-linux-release/proposal.md),
  [agent CLI/MCP](openspec/changes/add-agent-cli-mcp/proposal.md),
  [MicroSIP-like UI](openspec/changes/refine-native-ui/proposal.md),
  [Windows port](openspec/changes/add-windows-port/proposal.md).

## License

GPL-3.0-or-later. Third-party notices ship with every release. The MicroSIP name is used only to
describe the interface this project is inspired by.
