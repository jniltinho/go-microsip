# Review of the cutover (task 7.9)

One round, by Codex (`codex exec`, read-only sandbox), on 2026-11-13, over an extract of commit `d366a0a` (the tree without `archive/rust/vendor/`) and the diff from the last commit before the cutover. It is a static review: nothing was built, run or called by the reviewer, and it is not evidence that a build, a test or a call works. The report is [codex-round1.md](codex-round1.md).

Verdict: **SOUND WITH CHANGES**. Three major and eight minor findings; all are answered below. The answers themselves have been read by nobody but their author: there was no second round.

| # | Severity | Finding | Response |
| --- | --- | --- | --- |
| C1 | major | The scan's exception for the test database was by path alone: any content at that path would pass | Fixed. In the index the exception holds only for the reviewed content (its blob id); a changed fixture fails. Control added: "the allowed fixture with other content" |
| G1 | major | The release notes said the exact CI files were checked in VMs and cited a receipt that did not exist | Fixed. The notes now say the tag is pushed only after that run, where it is recorded, and that checksums differing from the ones recorded mean the files were not checked. The receipt is written from the run, after CI builds the files |
| H1 | major | The record presented the scan's exceptions as the owner's specific decision; his words name the cutover, the rename and the release, not the exceptions | Fixed in the record, the receipt and the script: it is an inference from his go-ahead on the report that recommended them, said as one, and his to overturn |
| A1 | minor | `build-manifest.json` was published but not named by `SHA256SUMS` | Fixed. The manifest is written first and named by the checksums; the release workflow excludes only `SHA256SUMS` from the comparison |
| A2 | minor | The Rust build's AppImage check (runtime in front, the same program inside, the contents) had no successor | Fixed. `build/qa/appimage.sh`, run by `make go-smoke` and so by CI: the pinned runtime in front (the digest appimagetool writes into it apart), the program inside identical to the plain executable, and exactly the eight expected files. Tried with another executable: it fails |
| B1 | minor | In `build/qa/check.sh` a failing `go list` was hidden by the filter after it | Fixed: the list is taken on its own line |
| C2 | minor | No control exercised the exemption of historical blobs, or an unrelated secret-named file in history | Fixed: "an exempt historical blob back in the index" and "an unrelated secret-named file only in history" both fail the scan |
| E1 | minor | `lab.sh`'s Rust runner sub-commands and `suite.sh` build from a root Dockerfile that moved: broken commands, and the lab's README advertises them | Fixed: they stop with a message that says where the Rust tree went, and the lab's README says which of its sections are history. The lab's image and project names are kept |
| G2 | minor | The developer guide said `go-smoke` runs the headless checks on both Linux files | Fixed in the guide: the headless checks are on the executable, the self-test on both |
| G3 | minor | The README said the MCP server was not tried on Windows; the parity receipt records a run of it in the Windows VM | Fixed in the README; the headless service is still not tried on Windows |
| F1 | minor | The packaged icon's comment named rust-microsip | Fixed |

Supported by the review, as read: a tag can publish only files of the successful push run of `ci.yml` on `main` for the tagged commit (runs of pull requests and manual runs do not qualify, and there is no fallback build); the environment protection and the jobs' permissions are kept; the historical exemptions are anchored; the gate's change list cannot be set from the release workflow; the sign-off does not claim the 83 unproven rows.

Not reviewed: runtime behavior, GitHub's live settings, the vendored Rust dependencies, and the publication itself.
