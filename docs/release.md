# Cutting a release

Binaries are published only by `.github/workflows/release.yml`, which runs when a tag `vX.Y.Z`
(or `vX.Y.Z-rc.N`) is pushed. It builds nothing: it publishes the files that `ci.yml` built from
the tagged commit on `main`, so the files that were checked are the files that are released.
`ci.yml` runs the checks on `main` and pull requests and keeps what it built for 30 days as the
workflow artifact `dist`. Nothing waits for an approval on GitHub: pushing the tag publishes, so
tagging is the owner's decision and an agent pushes a tag only when the owner asked for that
release.

## Rules enforced by the workflow

1. The tagged commit is **on `main`** and has a **successful `ci.yml` push run on `main`**; the
   artifacts are taken from that run.
2. The tag equals `GO_VERSION` in `build/go.mk`, and the artifacts' `build-manifest.json` names
   that version, the tagged commit and a clean checkout.
3. A **final** version (`vX.Y.Z`) needs `docs/release/vX.Y.Z.md` (release notes and gate
   checklist) that passes `build/release/check-gates.sh X.Y.Z`: no open checkbox in any task-list
   syntax, every task id of the [migration change](../openspec/changes/migrate-to-go-wails/tasks.md)
   listed as checked, `AG-01`..`AG-06` checked and an `Owner decision` line checked. Each item
   starts with its own label (`- [x] migrate-to-go-wails 7.2 ...`); `check-gates-selftest.sh`
   holds the negative controls. Never tick a gate without a receipt bound to a commit.
4. Only `vX.Y.Z-rc.N` tags are pre-releases (no gate check), for example to carry the exact files
   into the manual checklist; their notes say they are not a release. Any other suffix is refused.

The workflow then checks `SHA256SUMS`, that nothing is published that the checksums do not name,
runs the publication scan over the files (the AppImage and the zip unpacked) and publishes:

- `go-microsip-X.Y.Z-x86_64.AppImage` (Linux; needs the system's GTK 4 and WebKitGTK 6.0)
- `go-microsip-X.Y.Z-linux-amd64` (the same program as a plain executable)
- `go-microsip-X.Y.Z-windows-amd64.zip` (portable: the program, `LICENSE.txt` and
  `THIRD_PARTY_NOTICES.txt`)
- `go-microsip-X.Y.Z-windows-amd64.exe` (the program alone)
- `THIRD_PARTY_NOTICES.txt` and `sbom.cdx.json` (CycloneDX: the Go modules, the frontend packages
  and the fonts the executables contain)
- `build-manifest.json` (the version, the commit and the build image) and `SHA256SUMS`, which
  names every other file

The release's notes are `docs/release/vX.Y.Z.md` of the tagged commit followed by the checksums of
the published files (`build/release/notes.sh`, which makes the links to files of the repository
absolute at the tag: a release page cannot resolve relative ones; CI runs it on every commit). The Windows files are unsigned: say so in the notes, with the SmartScreen
prompt and the `Get-FileHash` check.

## Steps

1. Set `GO_VERSION` in `build/go.mk`, write `docs/release/vX.Y.Z.md` (notes, known limitations,
   rollback and, for a final version, the checked gate list with links to receipts), commit and
   push to `main`.
2. Wait for `ci` to pass on that commit:

   ```sh
   gh run list --workflow ci.yml --commit "$(git rev-parse HEAD)" -L 1
   ```

3. Check the files that will be published, on the machines they are for: download the run's
   artifact, put its files in `dist/go/` and run the VM checks on them. Record their checksums.

   ```sh
   gh run download <run-id> --name dist --dir dist/go
   KEEP=1 make go-vm GO_VM_LAB=1
   ```

4. Tag that same commit and push the tag:

   ```sh
   git tag -a vX.Y.Z -m "Release vX.Y.Z" <commit> && git push origin vX.Y.Z
   ```

5. Watch `gh run list --workflow release.yml -L 3`, check `gh release view vX.Y.Z`, download the assets
   and run `sha256sum --check SHA256SUMS`; the sums are those recorded in step 3.

The `create-release` skill (`.claude/skills/create-release/`) walks through the same steps.

## Local dry run

```sh
make dist                       # dist/go
make go-smoke                   # the headless checks and the window's self-test on those files
make scan SCAN=dist/go          # needs unsquashfs for the AppImage
```

`dist/go` of a local build is evidence for a milestone, never a release.

## Rollback

Delete the GitHub Release and the tag (`gh release delete vX.Y.Z --cleanup-tag`), publish a fixed
`vX.Y.Z+1`, and tell users to keep their settings: the database is never downgraded and a newer
schema is refused untouched by older builds. The repository's rename can be undone in its
settings (GitHub keeps redirecting the old address until a repository of that name is created).
