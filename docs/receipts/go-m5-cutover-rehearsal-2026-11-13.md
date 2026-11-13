# Rehearsal of the cutover (task 7.5), 2026-11-13

A rehearsal, in a disposable local clone of the branch at commit `3f64b5d`. Nothing in the repository was moved and nothing of it was pushed. Task 7.5 is not done and not checked: it comes after the parity gate (7.4), which waits for the owner. This records what the move does and what it runs into, so that the cutover is not the first time it is tried.

## What was done in the clone

| Step | Result |
| --- | --- |
| `git mv` of `apps/`, `crates/`, `vendor/`, `packaging/`, `tests/`, `Cargo.toml`, `Cargo.lock`, `rust-toolchain.toml`, `deny.toml`, `Dockerfile`, `.dockerignore`, `Makefile`, `scripts/cargo-docker.sh`, `scripts/vm/` and `docker/rust-components*.sha256` to `archive/rust/`, as one commit | 273 files, every one a rename with no change of content (`git diff --diff-filter=R -M100%`: 273; 0 insertions, 0 deletions) |
| a second commit: a root `Makefile` that includes `build/go.mk` and names `check`, `test`, `build`, `dist`, `lab` and `image` after the `go-*` targets; `scripts/go-docker.sh` without `-mod=readonly` | `make help` lists them |
| `make check`, `make test`, `make dist` in the clone | all three pass; Go does not enter vendor mode (there is no `vendor/` at the root any more) |
| what the build produced | `dist/go/` alone: the Linux binary, the AppImage, the Windows `.exe` and `.zip`, the notices, the bill of materials, `SHA256SUMS` (all six sums verify) and the manifest; the binary says `go-microsip 0.3.0-m3` and the clone's commit. No `target/` and nothing built under `archive/`, which holds no Go file |
| both commits reverted | the tree is that of `3f64b5d` again (`git diff`: nothing) |
| the Rust tests on the reverted tree (`make test`, in Docker) | pass: 307 tests passed, none failed, 3 ignored, after the toolchain image was built again for the clone |

Not rehearsed: the lab and the VMs on the moved tree (the Go sources are the same files; `make lab` was not run there); a clone from the remote, as the task's check asks (this one was a local clone); CI.

## What the cutover runs into

1. **CI has never run on the Go branch.** `ci.yml` runs on pushes to `main` and on pull requests; the branch has neither. The first run will be the cutover's own.
2. **The publication scan fails on the Go tree.** `packaging/release/scan-publication.sh`, which CI runs first and `release.yml` runs before publishing, reports 12 findings on this branch. None is a secret or a personal path:
   - 1 in the tree: `internal/core/store/testdata/rust-0.1.0-rc.3.sqlite3`, the fictional database of `TestRustCandidateDatabaseOpens` (the scan forbids every `*.sqlite3`; `.gitignore` already makes the exception);
   - 6 in the history: file names with "secrets" in them (`internal/platform/secrets/*.go`, `build/qa/secrets.sh`), since renamed to `keyring`;
   - 5 in the history: old versions of `internal/core/store/store_test.go` and `store.go` with fictional paths in a home folder that the scan counts as personal. The tree itself was cleaned of them in commit `a25f60d`.

   The scan allows exceptions only as owner-approved lists in the script itself, so this is the owner's to decide: allow the fixture by its exact path and the eleven historical blobs and names, as was done once for two early commits; or merge the branch into `main` as one commit, which leaves the eleven out of `main`'s history (the branch on the remote would still have them) and still needs the fixture allowed.
3. **`packaging/release/` is not Rust's.** `scan-publication.sh`, `check-gates.sh` and their self-tests are what `ci.yml` and `release.yml` call, and the release gates of `AGENTS.md` rest on them. Moved to `archive/rust/packaging/` with the rest, the workflows' paths break. They should stay out of the rename (or move to `build/release/` in 7.6), and `release.yml` takes the version it checks the gates for from `Cargo.toml`, which the Go build does not have.
4. **The lab is shared.** `docker/asterisk/` stays where it is: `make go-lab`, `make go-call` and the VMs use it. Inside it `suite.sh`, `suite/` and the compose files of the Rust runners are the Rust suite's, and `lab.sh` has sub-commands that build the Rust runners from the root `Dockerfile`, which the move takes away. They are dead after the cutover; pruning them is 7.6's.
5. **Both workflows are Rust's** (`ci.yml`: the Rust verify stage, the AppImage check of `dist/rust-microsip`; `release.yml`: the version from `Cargo.toml`, the Rust artifacts). 7.6 replaces them; until it does, nothing on `main` is checked.
6. **The build image names the flag too**: `build/linux.Dockerfile` sets `GOFLAGS=-mod=readonly`. `scripts/go-docker.sh` overrides it, so the rehearsal passed without rebuilding the image; dropping it there changes the image, which 7.7's rebuild then uses.
7. **Documents and instructions that name the Rust build as current**: `AGENTS.md`, `README.md`, `docs/DEVELOPMENT.md`, `docs/release.md`, the `create-release` skill, and five documents with links into the moved folders. Listed for 7.6.

Nothing in the Go code or its tests reads a file from the moved folders: the tests passed after the move.
