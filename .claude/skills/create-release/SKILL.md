---
name: create-release
description: Create a go-microsip version tag and GitHub Release. Use when the user asks to release, tag, publish or cut a version. Releases are allowed only when CI on main is green for the tagged commit, and a final version also needs every release gate checked and the owner's decision.
---

# Releasing go-microsip

Binaries are published only by `.github/workflows/release.yml` on `v*.*.*` tags, and it builds
nothing: it publishes the files `ci.yml` built from the tagged commit on `main` (the workflow
artifact `dist`, kept 30 days). Checks (gofmt, go vet, tests with the race detector, the frontend's
checks, the artifacts, the window's self-test, the publication scan) run in `ci.yml` on `main`
and on pull requests. See `docs/release.md`.

## Release rules

- **Only tag a commit whose `ci` run on `main` is green.** The workflow refuses otherwise.
- **A final version needs the owner's explicit decision** and `docs/release/vX.Y.Z.md` with every
  gate checked (the workflow refuses any `- [ ]`). Never tick a gate without a receipt bound to a
  commit; human, VM and device gates are done only by people.
- `-rc.N` tags publish pre-releases (no gate check) whose notes say they are not a release.
- The owner approves the `release` environment on GitHub; an agent pushes the tag and waits.
- Run `make scan` before pushing; never publish `base/`, secrets or personal paths.

## Before you start

```bash
gh auth status
git status                                  # must be clean
git fetch origin --tags && git checkout main && git pull --ff-only
LAST=$(git describe --tags --abbrev=0 2>/dev/null || echo none); echo "Last tag: $LAST"
```

## Versioning

Semantic versions with a `v` prefix: `v0.2.0`, `v0.2.1` (fixes), `v0.3.0` (features),
`v0.2.0-rc.1` (pre-release). The tag must equal `GO_VERSION` in `build/go.mk`.

## Process

1. Review changes: `git log $LAST..HEAD --oneline`.
2. Set `GO_VERSION` in `build/go.mk` if needed and write `docs/release/vX.Y.Z.md` (sections
   below, limitations, rollback and, for a final version, the gate checklist with receipt links;
   confirm with the owner). Commit and push to `main`.
3. Require green CI on that commit and note the run:

   ```bash
   gh run list --workflow ci.yml --commit "$(git rev-parse HEAD)" --json databaseId,status,conclusion -q '.[0]'
   ```

4. Check the files that will be published: `gh run download <run-id> --name dist --dir dist/go`,
   then the VM checks on them (`KEEP=1 make go-vm GO_VM_LAB=1`). Record `SHA256SUMS` in a receipt.
5. Tag that commit and push (triggers `release.yml`, which waits for the owner's approval):

   ```bash
   git tag -a $NEXT -m "Release $NEXT" <commit> && git push origin $NEXT
   ```

6. Monitor and verify: `gh run list --workflow release.yml -L 3`, `gh release view $NEXT` (eight
   assets), download and `sha256sum --check SHA256SUMS` against the sums of step 4.

## Release notes

Categorize commits in this order; omit empty sections: **✨ New Features**, **🔧 Improvements**,
**🔒 Security**, **🧹 Cleanup**, **📚 Documentation**, then **📦 Downloads** (AppImage, Linux
executable, Windows zip and exe, notices, SBOM, `SHA256SUMS`) and **Full Changelog**:
`https://github.com/jniltinho/go-microsip/compare/vPREV...vNEXT`. The workflow appends the
checksums. State known limitations honestly (no claim of full MicroSIP parity).
