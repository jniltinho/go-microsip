# Independent review of milestone M2 (Codex)

Each round reviews a sanitized extract made with `git archive` (no engine, no third-party sources, no images or build output, nothing from `base/`, no secrets or personal paths), with `codex exec --sandbox read-only --skip-git-repo-check --ephemeral`. A review is not call evidence: the evidence is in [the M2 receipt](../../receipts/go-m2-window-2026-11-11.md). The reviewer builds and runs nothing.

| Round | Commit | Verdict | Report |
| --- | --- | --- | --- |
| 1 | `b33616c` | SOUND WITH CHANGES: 4 major | [codex-round1.md](codex-round1.md) |

## What was done about round 1

| # | Finding | Response |
| --- | --- | --- |
| 1 | With an incomplete configuration, saving an account skipped the fields a file or the environment holds instead of refusing a change to them | Fixed. The edit is always compared with what is configured, complete or not, and a changed field held by a higher source is refused by name. `TestPhoneKeepsOverridesOfAnIncompleteConfiguration` has a file that holds the username and an environment that holds the transport. |
| 2 | The bound settings methods stored values the configuration reader refuses (an expiry of 0, a device name with a control character), so the next start failed | Fixed. `config.SQLiteEdits` and the device setters refuse what `Resolve` would refuse, and nothing is stored. Tested in the core (`TestPhoneKeepsOverridesOfAnIncompleteConfiguration`) and through `App.SaveSettings` (`TestThePageDrivesThePhone`). |
| 3 | A shutdown the engine did not take left both sides "quitting", so Quit could not be tried again | Fixed. The window clears its flag when the command is refused and quits at once when the engine has already stopped; the page clears its own flag on a refusal. The page's side is tested ("a refused shutdown can be tried again"); the window's is tested for the engine that has already stopped, and its refusal path is reasoned from the code, since a full command queue cannot be produced on demand. |
| 4 | On Linux, a guard that could not be installed still let the self-test pass as "not probed" | Fixed. A platform says whether its webview can be guarded; where it can, a guard that is not in place fails the self-test (`TestProbeVerdict`), and a normal start logs an error. A normal start still opens the window with the content security policy alone: refusing to start would leave a user without a phone on a system whose libraries differ. |

The receipt's wording was narrowed where the report found a claim stronger than its test: what the keyboard checks cover (they were extended to the history's search and to the contacts' list, favorites, search and editor, each reached with Tab and arrows: 46 checks), what the freeze of the page establishes (the kernel now has to report every web process stopped), what the order check compares, and the history's three calls (now counted row by row).
