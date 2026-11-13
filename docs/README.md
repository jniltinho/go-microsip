# Documentation

Guides, records and evidence of go-microsip. Start with:

- [Architecture](architecture.md): the layers, the runtime model and the security boundaries.
- [Developer guide](DEVELOPMENT.md): the Makefile, the Docker build, the tests, the lab and the VMs.
- [Controlling go-microsip](agent-cli-mcp.md): the command line, the MCP server, grants and the
  headless service.
- [Adding a skin](themes.md) and [the frontend](ui/go-frontend.md).
- [Release process](release.md) and the notes of each version under [release/](release/).

## Status

**Pre-release.** [`v0.2.0-rc.1`](release/v0.2.0-rc.1.md) is the first version of go-microsip, the
Go application that replaced rust-microsip on 2026-11-13. Its notes say what was checked and what
was not. Progress is tracked by the task list of the
[migration change](../openspec/changes/migrate-to-go-wails/tasks.md), which checks a task only
when its criteria have evidence. What is open for a final `v0.2.0`:

| Area | Open | Where |
| --- | --- | --- |
| Real machines | The manual checklist: sound cards and headsets, five-minute calls per codec, KDE, a real Windows machine, a home router | task 7.2 |
| Parity | 83 of the 140 behaviors compared with rust-microsip are partly shown or not shown | [matrix](migration/parity-matrix.md), [sign-off](migration/parity-signoff.md) |
| Decisions | The system's certificates for TLS, and an unknown skin in the configuration | [sign-off](migration/parity-signoff.md), S4a and S4b |
| First milestone | Three measurements never completed as written | tasks 2.1, 2.8, 2.10 |

## Records

- [Migration](migration/): the parity matrix against rust-microsip, the owner's decisions on its
  differences, and the sign-off of the gate.
- [Receipts](receipts/): evidence of each verified milestone (commands, hashes, measurements and
  limits). The ones named `go-*` are go-microsip's.
- [Reviews](reviews/): independent static reviews and their verdicts. A review is not evidence
  that a call works. The folders named `migrate-to-go-wails-*` are go-microsip's.
- [Screens](prints/go/): every screen in every skin, with fictional data.

## Records of rust-microsip

The Rust application is under [archive/rust](../archive/rust/README.md), with its own guides in
`archive/rust/docs/`. What it left in this folder is kept as history and describes that
application, with the paths its files had before the move: the
[architecture decision records](adr/), the receipts and reviews that are not named after the Go
migration, the notes of `v0.1.0-rc.1` to `rc.3` and the `v0.1.0` draft under [release/](release/),
[implementation-approval.md](implementation-approval.md),
[implementation-checkpoints.md](implementation-checkpoints.md),
[development-skills.md](development-skills.md), [reference-sources.md](reference-sources.md),
[secure-call-experiment.md](secure-call-experiment.md), [ui/g0-native-ui.md](ui/g0-native-ui.md),
[inventory/](inventory/), the screens under [prints/](prints/) outside `prints/go/`, and the
Rust-era changes under `openspec/changes/`, which the migration superseded.
