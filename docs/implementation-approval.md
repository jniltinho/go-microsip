# Approval to start implementation

On 2026-10-12, after resuming commit `14b46b9`, presenting the limits below, and requesting confirmation, the user approved starting implementation (translated from Portuguese: “you may start implementing”).

- Main change: `create-native-rust-linux-release`, starting with G0.
- Linked extension: `add-agent-cli-mcp`, local, authorized, and single-owner; its full implementation depends on G0.
- v0.1: Linux x86_64/Ubuntu 24.04, one enabled SIP account and one simultaneous call.
- ELF is the primary artifact; AppImage is additional and requires separate testing.
- Full-call Web/WASM, macOS/Windows, and advanced integrations remain later releases, as specified in proposal non-goals.
- Proposed own-code license direction is GPL-3.0-or-later; copying upstream resources without auditing is not authorized.

Approval permits implementation/testing, not a declaration that G0 passed, production calls, release publication, or visibility changes. `base/`, personal settings, and the installed AppImage remain untouched.

Subsequent instructions on the same date require English code/comments/CLI/documentation/tasks, commits and pushes at verified milestones, and use of the pinned andrius/asterisk repository/image for the disposable PBX. They do not waive release gates.

## Go/Wails migration (`migrate-to-go-wails`)

On 2026-11-11, after the proposal was presented with its decisions, risks and the Codex review result, the owner approved starting implementation with the G0 spike (translated from Portuguese: "approve and start G0"). The approved planning artifacts are commit `2aa0edd` on branch `migrate-to-go-wails`.

Assumptions presented with the proposal and accepted with the approval:

- Supported Linux baseline: Ubuntu 26.04 and later, and Debian 13, x86_64 (owner instruction of the same date).
- Everything in Go where possible, `modernc.org/sqlite` for storage, `emiago/sipgo` + `emiago/diago` as the SIP base; cgo is allowed in the audio adapter when needed for working audio (owner instructions of the same date).
- The Go line continues as `0.2.0` pre-releases; no final `v0.1.0` is cut from Rust.
- Windows ships as a portable `.exe`/`.zip` without an MSI installer.
- Square corners in every skin.
- New names: binary and directories `go-microsip`, environment prefix `GO_MICROSIP_`; Rust-application data is brought over only by the explicit import.

The approval permits implementation and testing of this change on its branch. It is not a declaration that G0 passed, and it does not cover renaming the GitHub repository, archiving the Rust code, publishing a release or downloading the closed-source `gophone` binary; each of those needs its own confirmation. `base/`, personal settings and the installed AppImage remain untouched.

On the same date, after seeing the spike window, the owner asked for a separate agent to build and refine the interface in parallel, to save time (translated from Portuguese: "start a separate agent to create the interface and refine it"). This authorizes the frontend part of milestones M2 and M3 (screens, skins, a mock backend) to start before the G0 decision is recorded. It does not change the G0 gate for the engine, and the screens are wired to the real core only after G0 and M1.

On 2026-11-11 the owner recorded the G0 decision: go, proceed to milestone M1 (see `docs/receipts/go-g0-2026-11-11.md`, "Decision", for what stays open).

On 2026-11-12, after the parity matrix was audited row by row and the decisions it left to the owner were laid out with a recommendation each (`docs/migration/parity-decisions.md`: nine differences already decided during the migration, seventeen not decided, twelve behaviors the Rust-era text requires and go-microsip does not have), the owner took every recommendation (translated from Portuguese: "yes, as recommended"). That document's "Decision" column is the record. It accepts the listed differences and the measured regressions, drops what it names as dropped (STUN for this release, a separate ringing device, a device change during a call, the flashing taskbar button, in-window volume), and asks for the work it names to be done before the parity gate. It is not the gate's sign-off: the gate still needs every row proven or dropped for one commit.
