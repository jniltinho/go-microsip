SOUND WITH CHANGES

Static review only. No project commands, builds, tests, or calls were run.

| id | severity | file:line | scenario | fix |
|---|---|---|---|---|
| C1 | major | `build/release/scan-publication.sh:35,173` | The fixture exception allows **any contents** at that path. Adding real contacts or completed call history to the fixture, while retaining its settings, escapes the database-name prohibition and need not match any content pattern. The fixture test (`internal/core/store/store_test.go:743–780`) does not establish that those tables contain no personal data. | Exempt the approved fixture’s exact content hash, not just its filename. |
| G1 | major | `docs/release/v0.2.0-rc.1.md:99–102` | The notes claim that the exact CI files were checked in VMs and cite a release receipt absent from this extract. The cutover receipt explicitly says these were local, dirty-tree artifacts, excludes VM runs, and says CI had not yet run (`docs/receipts/go-m5-cutover-2026-11-13.md:21–33,52`). | Mark exact-CI-artifact verification as pending until the receipt exists and records the checked hashes. |
| H1 | major | `docs/migration/parity-signoff.md:65` | The quoted words authorize archiving, renaming, releasing and later manual testing. They do not expressly select the scanner exceptions over squashing history. This sentence presents that interpretation as specific owner approval; the scanner comments subsequently rely on it. | Obtain explicit approval for the enumerated exceptions, or clearly record this as an inference rather than a quoted decision. |
| A1 | minor | `.github/workflows/release.yml:107–109,128` | `build-manifest.json` is excluded from the checksum-membership comparison but is published by `release/*`. Its unchecked fields can change without invalidating `SHA256SUMS`. This contradicts the “nothing … the checksums do not name” promise in `docs/release.md:30`. | Generate the manifest before the checksums, include it, and exclude only `SHA256SUMS` from the comparison. |
| A2 | minor | `.github/workflows/release.yml:97–117`; `build/go.mk:70–73` | The old AppImage verifier checked the runtime prefix, embedded executable equality and package contents (`archive/rust/packaging/appimage/verify.sh:14–42`). Neither the new smoke nor publication scan preserves those assertions. A different embedded executable that passes self-test would not be caught by an equality check. | Restore the applicable package assertions, particularly extracted-executable comparison and runtime verification. |
| B1 | minor | `build/qa/check.sh:16` | A Linux/no-cgo-only package with an import-resolution failure can be omitted by `go list`. The pipeline reports `grep`’s status, while `go vet` receives the remaining packages and can succeed. The other configurations need not select that file. | Capture and check `go list` separately before filtering its successful output. |
| C2 | minor | `build/release/ci-selftest.sh:121–129` | The new controls exercise an adjacent database filename and an exempt historical filename staged in the index. They never exercise the historical blob-ID exemption. Removing its index-presence guard (`scan-publication.sh:196`) would leave these controls green. | Add controls for an exempt blob reintroduced into the index, and an unrelated historical secret filename. |
| E1 | minor | `docker/asterisk/lab.sh:19–22`; `docker/asterisk/suite.sh:39–42` | These commands still build Rust targets from `.` although the root Dockerfile moved. They are broken commands, not merely retained image/project names. The lab README still advertises them. | Reject obsolete entry points with an explanatory message and label their README sections historical. |
| G2 | minor | `docs/DEVELOPMENT.md:75` | This promises both headless and window checks on both Linux artifacts. `go-smoke` invokes `headless.sh` only on the plain executable (`build/go.mk:71–73`). | Add the AppImage headless invocation or narrow the description. |
| G3 | minor | `README.md:183` | “MCP … not tried on Windows yet” contradicts the recorded Windows MCP run in `docs/receipts/go-m5-parity-2026-11-12.md:263`. | Describe that limited MCP verification separately from the still-unverified service. |
| F1 | minor | `build/linux/go-microsip.svg:2` | The currently packaged SVG still identifies itself as the “rust-microsip application icon.” | Update the product name in its comment. |

## Checked and found supported

- Release selection binds the workflow, commit, `main`, **push** event and successful conclusion (`release.yml:62–70`). PR and dispatch artifacts named `dist` do not qualify.
- Re-runs retain the commit binding. Missing or expired artifacts cause download failure; there is no fallback rebuild or alternate-run download.
- Manifest version/commit/dirty checks and required asset checks are present. Non-metadata published filenames must match the checksum list.
- Environment protection checks remain; only the publishing job receives `contents: write`. Shell inputs are appropriately quoted.
- Windows downloads the Linux job’s `dist`; unmatched copy globs fail rather than silently skipping assets.
- Historical filename exceptions are anchored and history-only. Blob exceptions require exact IDs absent from the index.
- `CHECK_GATES_CHANGES` is not supplied by release.yml; self-test assignments are subprocess-local.
- The sign-off does not claim all 140 rows proven: 83 remain unproven, task 7.2 stays open, and S4 remains undecided.

## Not checked

Runtime behavior, live GitHub protections/artifacts, omitted Rust vendor contents, or actual publication. These conclusions are not build, test, call, or release-acceptance evidence.