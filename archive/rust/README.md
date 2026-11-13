# rust-microsip (archived)

This folder holds rust-microsip, the Rust application that this repository built and released
until `v0.1.0-rc.3`. On 2026-11-13 it was replaced by go-microsip, the Go application at the
repository's root, and moved here as it was, as a historical reference
([the plan](../../openspec/changes/migrate-to-go-wails/proposal.md),
[what the Go application was checked against](../../docs/migration/parity-matrix.md)).

- It is not built by the repository's `make` targets or by CI, not shipped in any artifact and not
  released. Its last release is [`v0.1.0-rc.3`](../../docs/release/v0.1.0-rc.3.md).
- It is not maintained: fixes go to go-microsip.
- The files are where they were, with `archive/rust/` in front: `apps/`, `crates/`, `vendor/`,
  `packaging/`, `tests/`, the Cargo and toolchain files, the `Dockerfile`, the `Makefile`,
  `scripts/` and `docs/` (the Rust guides: `DEVELOPMENT.md`, `architecture.md`,
  `agent-cli-mcp-rust.md`). Paths in those guides, and in the records under the repository's
  `docs/` written before the cutover, are the ones the files had at the root.
- Three things it used stayed at the root because go-microsip uses them too: the lab PBX
  (`docker/asterisk/`), the publication scan and the release gate check (now `build/release/`).

To build it again, the move has to be undone in a checkout: revert the two cutover commits
(`cutover: move the Rust workspace to archive/rust` and the one after it), then follow
`docs/DEVELOPMENT.md` of that tree. That was rehearsed before the cutover
([receipt](../../docs/receipts/go-m5-cutover-rehearsal-2026-11-13.md)).

To bring an installation's account, contacts and history to go-microsip:
`go-microsip import rust-microsip` (see the [README](../../README.md#coming-from-rust-microsip)).

The lab keeps the names it was given here (the image `rust-microsip-asterisk`, the compose
project `rust-microsip-lab`) and the sub-commands of `docker/asterisk/lab.sh` and
`docker/asterisk/suite.sh` that ran the Rust runners; those need this folder's `Dockerfile` at
the root and do not work from the cutover on.
