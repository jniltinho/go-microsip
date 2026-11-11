# ADR 0014: Go dependency graph for the Wails rewrite

Date: 2026-11-11. Status: **accepted engineering boundary for the G0 spike of
`migrate-to-go-wails`; not a legal certification, vulnerability audit or release approval.**

## Context

The Go rewrite (`openspec/changes/migrate-to-go-wails`) replaces the Cargo graph of
[ADR 0012](0012-production-dependency-graph.md). The project stays GPL-3.0-or-later. The owner
asked for Go throughout, `modernc.org/sqlite` for storage, `emiago/sipgo` + `emiago/diago` as the
SIP base, and allowed cgo only where working audio needs it.

Versions and licenses below were read from the downloaded module sources (the license file of
each module), not from memory. "Linked" means `go list -deps` selects the module for a shipped
binary; "pinned for later" modules are not imported yet and enter `go.mod` with their first import.

## Decision

### Linked today

Desktop binary (`.`), Linux `-tags "production purego"` and Windows `-tags production`, both
`CGO_ENABLED=0`:

| Module | Version | License | Role |
| --- | --- | --- | --- |
| github.com/wailsapp/wails/v3 | v3.1.0 | MIT | window, bindings, events |
| github.com/ebitengine/purego | v0.11.1 | Apache-2.0 | loads GTK 4 / WebKitGTK 6.0 at run time (Linux) |
| github.com/go-ole/go-ole | v1.3.0 | MIT | COM on Windows (WebView2) |
| github.com/godbus/dbus/v5 | v5.2.2 | BSD-2-Clause | session bus (Linux) |
| github.com/spf13/cobra | v1.10.2 | Apache-2.0 | command line |
| github.com/pelletier/go-toml/v2 | v2.4.3 | MIT | configuration file (`internal/core/config`), pure Go |
| github.com/spf13/pflag | v1.0.10 | BSD-3-Clause | flags |
| github.com/inconshreveable/mousetrap | v1.1.0 | Apache-2.0 | cobra on Windows |
| github.com/modelcontextprotocol/go-sdk | v1.8.0 | Apache-2.0 | stdio MCP server |
| github.com/google/jsonschema-go | v0.4.3 | MIT | MCP tool schemas |
| github.com/yosida95/uritemplate/v3 | v3.0.2 | BSD-3-Clause | MCP SDK |
| github.com/segmentio/asm, encoding | v1.1.3, v0.5.4 | MIT | MCP SDK JSON |
| golang.org/x/oauth2, sync, sys, time | v0.35.0, v0.23.0, v0.48.0, v0.15.0 | BSD-3-Clause | Go project libraries |

SIP and media (linked by the G0 lab runner today, by the engine from M1):

| Module | Version | License | Role |
| --- | --- | --- | --- |
| github.com/emiago/sipgo | v1.7.0 | BSD-2-Clause | SIP transactions and transports |
| github.com/emiago/diago | v0.42.0, vendored with a patch | MPL-2.0 | dialogs, SDP, RTP, SDES-SRTP |
| github.com/emiago/dtls/v3 | v3.0.0-20260122183559-8b8d23e359c0 | MIT | pulled by diago (DTLS, unused) |
| github.com/pion/rtp, rtcp, srtp/v3, sdp/v3 | v1.10.1, v1.2.16, v3.0.10, v3.0.18 | MIT | packets, SRTP |
| github.com/pion/dtls/v3, ice/v4, stun/v3, turn/v5, mdns/v2, transport/v4, logging, randutil | as in `go.sum` | MIT | pulled by diago's WebRTC code |
| github.com/zaf/g711 | v1.4.0 | BSD-3-Clause | G.711 |
| github.com/icholy/digest | v1.2.0 | MIT | digest authentication |
| github.com/google/uuid | v1.6.0 | BSD-3-Clause | identifiers |
| github.com/gobwas/ws, httphead, pool | v1.4.0, v0.1.0, v0.2.1 | MIT | sipgo WebSocket transport (unused) |
| github.com/go-audio/riff | v1.0.0 | Apache-2.0 | diago WAV reader |
| github.com/wlynxg/anet | v0.0.5 | BSD-3-Clause | interface lookup |
| golang.org/x/crypto, net | v0.57.0, v0.60.0 | BSD-3-Clause | TLS helpers |

Audio (Linux adapter, `internal/platform/audio`):

| Module | Version | License | Role |
| --- | --- | --- | --- |
| github.com/jfreymuth/pulse | v0.1.3 | MIT | PulseAudio native protocol client, no cgo |

Embedded frontend (production bundle): `vue` 3.5.45 and its `@vue/*` packages, `@wailsio/runtime`
3.1.0 and their dependencies, all MIT except `entities` (BSD-2-Clause), `source-map-js`
(BSD-3-Clause) and `picocolors` (ISC). Build-only tools (Vite Plus, Tailwind) are not shipped.

### Pinned for later (not imported yet)

| Module | Version | License | For |
| --- | --- | --- | --- |
| modernc.org/sqlite | v1.62.0 | BSD-3-Clause | storage (M1), pure Go |
| github.com/zalando/go-keyring | v0.2.8 | MIT | Secret Service / Credential Manager (M4) |
| github.com/moutend/go-wca | v0.3.0 | MIT | Windows WASAPI, first audio candidate |
| github.com/gen2brain/malgo | v0.11.27 | Unlicense (miniaudio: public domain or MIT-0) | cgo audio fallback, only if a pure-Go backend fails |

### License compatibility

Every license above is compatible with distributing the combined work under GPL-3.0-or-later:
MIT, ISC, BSD-2-Clause, BSD-3-Clause and the Unlicense are permissive; Apache-2.0 is compatible
with GPL version 3; MPL-2.0 is compatible through its section 3.3 (diago does not carry the
"Incompatible With Secondary Licenses" notice). The MPL-2.0 file-level copyleft applies to diago's
files: the vendored copy and its changes are published in `third_party/diago` with the change list
in `README.go-microsip.md`.

`scripts/go-licenses.sh` writes `THIRD_PARTY_NOTICES.txt` from the modules and npm packages that
are actually linked and fails when a Go module has no license file. It ships in the AppImage and
in the Windows zip.

### Native code and cgo

- `CGO_ENABLED=0 go list -deps` reports **no cgo package** for the Linux desktop binary
  (`-tags "production purego"`), the Windows binary and the lab runner. The Linux window uses the
  experimental CGO-free backend that Wails 3.1.0 added; the cgo backend (`GO_WINDOW=cgo`) stays
  buildable as the fallback and then links only Wails' own four cgo packages.
- At run time the Linux binary loads the system's GTK 4, WebKitGTK 6.0, JavaScriptCore, libsoup 3
  and GLib (not bundled; the supported distributions' desktops provide them) and talks to the
  session's PulseAudio-protocol server over a socket. Windows uses the system WebView2.
- diago's Opus codec is the only cgo code in the graph (`with_opus_c` build tag,
  `gopkg.in/hraban/opus.v2`); the tag is never set, so it is not built.

### Not approved

Anything diago or sipgo can do that the softphone does not use (WebRTC, DTLS-SRTP, WebSocket
transports, Opus, recording, bridges) is linked as library code but is not enabled, tested or
approved here. Enabling any of it needs its own review.

## Consequences

- One toolchain, no C compiler for any shipped binary today. If the Windows WASAPI candidate fails
  its G0 threshold, `malgo` brings cgo back for that adapter alone (design D4).
- The diago patch must be re-applied on every upgrade and should be offered upstream.
- The CGO-free Wails Linux backend is marked experimental upstream; the G0 receipt records the
  checks it passed, and `GO_WINDOW=cgo` is the fallback.
