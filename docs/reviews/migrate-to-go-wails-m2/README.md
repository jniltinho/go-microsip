# Independent review of milestone M2 (Codex)

Each round reviews a sanitized extract made with `git archive` (no engine, no third-party sources, no images or build output, nothing from `base/`, no secrets or personal paths), with `codex exec --sandbox read-only --skip-git-repo-check --ephemeral`. A review is not call evidence: the evidence is in [the M2 receipt](../../receipts/go-m2-window-2026-11-11.md). The reviewer builds and runs nothing.

| Round | Commit | Verdict | Report |
| --- | --- | --- | --- |
| 1 | `b33616c` | SOUND WITH CHANGES: 4 major | [codex-round1.md](codex-round1.md) |
| 2 | `c12b375` | SOUND WITH CHANGES: 3 of the 4 resolved, 1 partially; 1 new major | [codex-round2.md](codex-round2.md) |

## What was done about round 1

| # | Finding | Response |
| --- | --- | --- |
| 1 | With an incomplete configuration, saving an account skipped the fields a file or the environment holds instead of refusing a change to them | Fixed. The edit is always compared with what is configured, complete or not, and a changed field held by a higher source is refused by name. `TestPhoneKeepsOverridesOfAnIncompleteConfiguration` has a file that holds the username and an environment that holds the transport. |
| 2 | The bound settings methods stored values the configuration reader refuses (an expiry of 0, a device name with a control character), so the next start failed | Fixed. `config.SQLiteEdits` and the device setters refuse what `Resolve` would refuse, and nothing is stored. Tested in the core (`TestPhoneKeepsOverridesOfAnIncompleteConfiguration`) and through `App.SaveSettings` (`TestThePageDrivesThePhone`). |
| 3 | A shutdown the engine did not take left both sides "quitting", so Quit could not be tried again | Fixed. The window clears its flag when the command is refused and quits at once when the engine has already stopped; the page clears its own flag on a refusal. The page's side is tested ("a refused shutdown can be tried again"); the window's is tested for the engine that has already stopped, and its refusal path is reasoned from the code, since a full command queue cannot be produced on demand. |
| 4 | On Linux, a guard that could not be installed still let the self-test pass as "not probed" | Fixed. A platform says whether its webview can be guarded; where it can, a guard that is not in place fails the self-test (`TestProbeVerdict`), and a normal start logs an error. A normal start still opens the window with the content security policy alone: refusing to start would leave a user without a phone on a system whose libraries differ. |

The receipt's wording was narrowed where the report found a claim stronger than its test: what the keyboard checks cover (they were extended to the history's search and to the contacts' list, favorites, search and editor, each reached with Tab and arrows: 46 checks), what the freeze of the page establishes (the kernel now has to report every web process stopped), what the order check compares, and the history's three calls (now counted row by row).

## What was done about round 2

| # | Finding | Response |
| --- | --- | --- |
| R1.4 | On Linux a window whose guard could not be installed still opened, with an error in the log | Fixed. Where the webview can be guarded and the guard is not in place, the window says so in a dialog and closes, and the command fails with that error. It is the stricter of the two ways the report offered; the owner can still choose to open such a window and accept it. The path needs a system whose libraries lack what the guard uses, so it is not exercised by a test: the self-test on Ubuntu 26.04, Debian 13 and the build container shows the guard in place there. |
| 1 | Saving an account while a configuration source cannot be read (a file with a syntax error) stored an empty `account.password_env`, which then made the next start fail even after the file was fixed | Fixed. `config.Draft` says whether the sources were read at all; when they were not, nothing is saved, the account and the skin alike, and the error names the file. `TestPhoneSavesNothingOverAnUnreadableConfiguration` checks the database stays empty and that the same edit resolves once the file is fixed; `TestSavingOverAnUnreadableConfigurationStoresNothing` does it through the bound method. The window does not yet say on its own that the configuration could not be read: it shows no account, and the error appears when something is saved (`doctor` reports it). |

Receipt: the page's test count is 27 (18 of the reducer and validation, 9 of the skins); the keyboard checks are described as what they are, with the places where the script still puts the focus itself; the runs in the receipt are attributed to the commit they ran on.
