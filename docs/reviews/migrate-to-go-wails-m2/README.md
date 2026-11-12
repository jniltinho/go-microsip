# Independent review of milestone M2 (Codex)

Each round reviews a sanitized extract made with `git archive` (no engine, no third-party sources, no images or build output, nothing from `base/`, no secrets or personal paths), with `codex exec --sandbox read-only --skip-git-repo-check --ephemeral`. A review is not call evidence: the evidence is in [the M2 receipt](../../receipts/go-m2-window-2026-11-11.md). The reviewer builds and runs nothing.

| Round | Commit | Verdict | Report |
| --- | --- | --- | --- |
| 1 | `b33616c` | SOUND WITH CHANGES: 4 major | [codex-round1.md](codex-round1.md) |
| 2 | `c12b375` | SOUND WITH CHANGES: 3 of the 4 resolved, 1 partially; 1 new major | [codex-round2.md](codex-round2.md) |
| 3 | `74e73d7` | SOUND WITH CHANGES: both open items resolved; 2 new major | [codex-round3.md](codex-round3.md) |
| 4 | `757cf47` | SOUND WITH CHANGES: 1 resolved, 1 partially; 1 new major | [codex-round4.md](codex-round4.md) |
| 5 | `eba3378` | SOUND WITH CHANGES: 1 resolved, 1 partially; 1 new major | [codex-round5.md](codex-round5.md) |
| 6 | `951c228` | SOUND WITH CHANGES: 1 resolved, 1 partially; 1 new major (the same matter) | [codex-round6.md](codex-round6.md) |
| 7 | `14351a3` | SOUND WITH CHANGES: resolved; 1 new major | [codex-round7.md](codex-round7.md) |
| 8 | `752ec66` | SOUND: resolved; no new problem | [codex-round8.md](codex-round8.md) |

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

## What was done about round 3

| # | Finding | Response |
| --- | --- | --- |
| 1 | Do-not-disturb saved before an account existed was shown on at the next start while the engine had it off | Fixed. What the engine is told at start is one function: do-not-disturb is applied whenever the settings could be read, account or not; the account registers only from a configuration that resolves. `TestSavedDoNotDisturbIsAppliedWithoutAnAccount` checks the commands for each case and a real second start on the same profile. It does not send a call to see it refused: the lab's engine scenarios cover the engine's refusal itself. |
| 2 | A value a file holds and that cannot be read (a transport in capitals) became a default: the account was offered as configured, saved "successfully", and registered from the default | Fixed. An account is offered as configured only when the whole configuration resolves, and an account is saved only if the configuration resolves with it; otherwise the save is refused with the file's error and nothing is stored. `TestPhoneRefusesAnAccountOverAnInvalidFileValue`. A form that completes an incomplete account, and one that repairs a value stored in the database, still go through. |

Receipt: the wrong-password check is described as the separate run it is, and the AppImage's figures are those of its log.

## What was done about round 4

| # | Finding | Response |
| --- | --- | --- |
| R3.2 | The check of the whole configuration was skipped when the edit had nothing to write, because a file held every field | Fixed. The check runs before the empty edit is reported as done. `TestPhoneRefusesAnAccountWhenTheFileHoldsEverything` has a file with every field and one unreadable value. |
| 1 | A registration asked from the window used the system's roots when the configured CA bundle had not been read: the phone started the engine without it | Fixed, in the engine too. The bundle is read whether or not an account is complete yet. When it cannot be used the engine is told (`Options.Untrusted`) and refuses a TLS registration as a certificate failure without contacting the server, so the window shows "Failed: certificate rejected" from an engine event, on its own at the start and when the person asks. A bundle configured after the start needs a restart: registering over TLS before that is refused (`ErrRestartForCA`). Tests: `TestATLSAccountDoesNotRegisterWithoutItsBundle` (a listener that must stay untouched; a TCP account on the same engine is free to try) and `TestPhoneDoesNotRegisterOverTLSWithoutItsBundle`. The engine's lab scenarios pass after the change (66 of 66). The refusal for a bundle changed after the start reaches the page as "Not possible in the current call state", which says too little; the log has the reason. |

Receipt: the sound figures of the binary are those of its log.

## What was done about round 5

| # | Finding | Response |
| --- | --- | --- |
| R4.1 | A `ca_file` that is not a valid setting (a relative path) reads as "no bundle", so comparing it with the engine's own let a TLS account register against the system's roots | Fixed. A TLS account does not register while the configuration does not resolve; the error names the setting. `TestPhoneDoesNotRegisterOverTLSWithoutItsBundle` adds a relative `ca_file` with a typed password. A form that completes an incomplete account is not affected: it saves first, and the save already requires the configuration to resolve. |
| 1 | Saving another account during a call changed what the window showed while the engine kept the old account, which stayed in use after the call | Fixed on both sides. During a call the page neither saves nor shows another account ("an account is not saved or shown during a call" in `state.test.ts`), and the core refuses to save or register one with the engine's own "busy" (`TestPhoneKeepsItsAccountDuringACall`); the skin can still change. |

Receipt: the keyboard table no longer names the confirmation dialogs, which the script does not walk through; the wrong-password check says what it asserts; sound figures are quoted from the log of the artifact they belong to.

After this round the branch that unites layout B, the AAA skins and the Matrix skin was merged (`704e1cb`), on the owner's word. The page the next round sees is that layout; the lab scenario of the real window was updated for it.

## What was done about round 6

| # | Finding | Response |
| --- | --- | --- |
| R5.1 / 1 | A call that arrives between the save of another account and its registration: the engine refuses the registration and keeps its account, while the window names the one it saved | Fixed where the report asked: with the engine's word. A registration event now carries the account it is about (`RegistrationChanged.Account`, `registration.account` for the page), and the header shows that account while the engine uses one; what is saved is shown only when the engine uses none. `TestRegistrationEventsNameTheEnginesAccount` (the engine), `TestEventsHaveTheContractsShape` (the event) and "the header names the engine's account, not one it has not taken" in `state.test.ts`, which lets a call come in while the save is on its way and refuses the registration. The saved account then waits: the settings stay open with the notice, and Register applies it once the call is over. The lab's engine scenarios and the window's lab call pass after the change (66 of 66, 17 of 17). |

Receipt: the keyboard and scale logs are those of the merged page (52 and 140 checks, seven skins), run again here; the page's test count is 31.

## What was done about round 7

| # | Finding | Response |
| --- | --- | --- |
| 1 | The microphone and the speaker chosen before an account existed were ignored at the next start: the engine started with the system's defaults while the settings showed the chosen ones | Fixed. The devices are read with the settings, whether or not an account is configured, like do-not-disturb and the CA bundle. `TestSavedSoundDevicesAreUsedWithoutAnAccount` checks what the engine is started with at the second start; it does not place a call to see the device opened. The lab call of the window, which names its devices in the configuration, passes after the change (17 of 17). |

After this finding, everything a phone reads at its start was checked for the same dependence on a complete account: do-not-disturb (round 3), the CA bundle (round 4) and the sound devices (this round) no longer have it; only the registration itself waits for an account and a password.

## Round 8

Sound, on `752ec66`, with nothing to change. What the report leaves standing is in the receipt:

- Windows has no guard against a navigation or a new window (the owner accepted the content security policy alone there), and what happens when the Linux guard cannot be installed is read from the code, not exercised.
- The keyboard and scale checks cover the operations they list, with the 150 % case simulated; the development backend is a simulation, not a replay of recorded events.
- The transfer's rendering and the agreement of call ids and versions between the engine and the page are not asserted.
- The rule against raw HTML is a source scan in the page's tests; no CI job runs it yet.
- System following the desktop is verified on GNOME only.
- Debian's and Windows' sound in the VMs failed; neither is passing evidence.
- Task 4.8 asks for the call walkthrough on the Windows executable in the Windows VM, which has not been done.

The reviewer built and ran nothing: the test runs, the lab calls and the VM checks are the receipt's.
