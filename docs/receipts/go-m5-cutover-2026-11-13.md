# The cutover (tasks 7.5 and 7.6), 2026-11-13

The owner decided the parity gate and the cutover that day ([record](../migration/parity-signoff.md)). This is what was done and what was run on it. The files that are published are CI's, checked separately ([release receipt](go-m5-release-2026-11-13.md)); the runs below are of a local build of the moved tree, on the developer's machine, which was under heavy load from another program throughout.

## What moved

| Commit | What |
| --- | --- |
| `cc9503d` | 273 renames with no content changed (`git diff -M100% --diff-filter=R`: 273 files, 0 insertions, 0 deletions): `apps/`, `crates/`, `vendor/`, `packaging/`, `tests/`, `Cargo.toml`, `Cargo.lock`, `rust-toolchain.toml`, `deny.toml`, `Dockerfile`, `.dockerignore`, `Makefile`, `scripts/cargo-docker.sh`, `scripts/vm/` and `docker/rust-components*.sha256` to `archive/rust/`; the publication scan, the gate check and their two self-tests from `packaging/release/` to `build/release/`, because the workflows and go-microsip use them |
| `98579db` | a root `Makefile` that names `check`, `test`, `build`, `dist`, `lab`, `image` after the `go-` targets and adds `scan`; `-mod=readonly` leaves `scripts/go-docker.sh` and the build image |
| the commit after it | the workflows, the release tooling and the documents (task 7.6, below) |

Not moved: `docker/asterisk/`, the lab, which go-microsip uses. It keeps its image and project names and the sub-commands of the Rust runners, which no longer work ([archive/rust/README.md](../../archive/rust/README.md)).

## Reverting restores the Rust tree

In a fresh clone, `git revert` of `98579db` and `cc9503d` leaves a tree with no difference from the commit before the cutover (`git diff 3f3acc6 HEAD`: nothing). No Rust file changed between that commit and `3f64b5d`, the commit of [the rehearsal](go-m5-cutover-rehearsal-2026-11-13.md), where the Rust tests were run on the reverted tree and passed (307 passed, none failed, 3 ignored). The Rust build was not run again on this clone.

## The moved tree, built and run locally

On `98579db` with the changes of 7.6 in the working tree (so the manifest says `dirty`), version `0.2.0-rc.1`:

| Run | Result |
| --- | --- |
| `make check` | passes: gofmt and go vet for the purego, cgo, no-window and Windows builds and the core on darwin, with no `-mod` flag |
| `make test` | passes, with the race detector |
| `make dist` | `dist/go/`: the Linux binary, the AppImage, the Windows `.exe` and `.zip`, the notices, the bill of materials, `SHA256SUMS` and the manifest; nothing is compiled under `archive/`, which holds no Go file |
| `make lab` | 79 scenarios against the lab's Asterisk, none failed |
| `make go-appimage-call` | the walkthrough of the built AppImage: 30 steps pass; the two measured calls had 993 of 995 and 996 of 996 pieces of sound on the speaker |
| `make go-smoke` | the headless checks, and the window's self-test on the Linux binary and on the AppImage, pass |
| `make scan SCAN=<the eight files>` | the publication scan passes on the index, the history and the files, the AppImage and the zip unpacked |

Not run here: the VMs (they are run on CI's files) and FreePBX.

## The publication scan's exceptions

The scan failed on the Go tree with 13 findings, none a secret or a personal path. By the owner's decision of 2026-11-13 it has these exceptions, each as narrow as its finding, and `ci-selftest.sh` checks that they stay so:

| Finding | Exception |
| --- | --- |
| `internal/core/store/testdata/rust-0.1.0-rc.3.sqlite3`, the fictional database of the store's tests | that exact path, in the repository only; another database beside it fails, and so does any database in an artifact |
| `archive/rust/crates/platform/src/secrets.rs` | the path that was already allowed, with `archive/rust/` in front |
| six file names with "secrets" in the Go history (`internal/platform/secrets/*.go`, `build/qa/secrets.sh`, since renamed to `keyring`) | those names, in history only: one of them in the index fails |
| five historical versions of `internal/core/store/store.go` and `store_test.go` with fictional example paths (a one-letter user, "lab", an ellipsis) | those five blobs by id, while absent from the index |

## Continuous integration and the release

- `ci.yml` runs the publication scan with its controls, then in the build image the static checks, the tests with the race detector, the frontend's checks, the artifacts, the headless checks and the window's self-test on the built files, and the scan of those files; a Windows runner checks the `.exe` and the `.zip`. The Rust jobs are gone with the Rust build.
- `release.yml` no longer builds. For a tag on a commit of `main` with a green CI run it takes the files that run built, checks their sums, their version and commit, that nothing else is published, scans them, and publishes them with the notes of the tagged commit and the checksums. The owner's approval of the `release` environment is still required. The reason: task 7.7 asks that the files checked be the files published, and a second build would give other files.
- `check-gates.sh` reads the task list of `migrate-to-go-wails` for a final version.

CI's own result is in the release receipt: it has not run on a commit of this tree at the time of writing.
