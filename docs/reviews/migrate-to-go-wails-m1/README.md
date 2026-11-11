# Independent review of milestone M1 (Codex)

Each round reviews a sanitized extract made with `git archive` (no third-party sources except the two patched diago files, no fixtures, nothing from `base/`, no secrets or personal paths), with `codex exec --sandbox read-only --skip-git-repo-check --ephemeral`. A review is not call evidence; the evidence is in [the M1 receipt](../../receipts/go-m1-engine-2026-11-11.md).

| Round | Commit | Verdict | Report |
| --- | --- | --- | --- |
| 1 | `60cd1a8` | UNSOUND: 1 critical, 12 major, 1 minor | [codex-round1.md](codex-round1.md) |

## What was done about round 1

| # | Finding | Response |
| --- | --- | --- |
| 1 | Shutdown can abandon results that own resources | Fixed. Results of work beside the actor are never dropped: the actor applies what arrived when it ends, and a result that arrives later is applied by the goroutine that brings it, one at a time. Every callback already releases what it holds when its account or call is gone. `TestResultsAreNeverDropped`. |
| 2 | The five-second shutdown is not enforced | Fixed. The sound devices are opened beside the actor; the shutdown shares its time out (the call's goodbye, the unregistration, the devices), bounds the 486 too, and stops the audio first. The lab's `stop` now requires the devices closed at the moment `Stopped` is reported and the whole within 5.5 s. |
| 3 | Shutdown races an incoming Answer | Fixed. While an Answer is on its way its 200 is the only final response; the shutdown sends none, and the answer's result hangs the dialog up. Not covered by a lab scenario (the window is a few milliseconds wide). |
| 4 | Media workers can die without changing the call | Fixed. A direction that ends on its own while the call is up is reported once to the actor, which hangs up and ends the call as failed. Not exercised by a test: the lab has no way to break RTP alone. |
| 5 | Transfer completion can be lost | Fixed. The verdict is kept apart from progress reports; a dialog that ends while a transfer waits for its verdict is ended by that verdict. Not covered by a lab scenario: Asterisk leaves the BYE to the transferor. |
| 6 | Jitter catch-up declares reordered packets lost early | Fixed. Catching up takes only a packet that is there. `TestJitterBufferWaitsForALatePacketBehindABurst` fails on the previous code (3 of 3 runs) and passes on this one. |
| 7 | Existing storage directories are not checked | Fixed for the mode: a directory that group or others can write is refused (`TestDirectoryOthersCanWriteIsRefused`). The owner is not checked and Windows has no mode bits; recorded as a shortcut for the Windows adapters. |
| 8 | Recovery runs without exclusive ownership (critical) | Fixed. Recovery takes go-microsip's instance lock; `Apply` holds it from before the recovery of an earlier attempt until after its own cleanup; a command that cannot take it refuses to write beside an unfinished import. `TestRecoveryNeedsTheInstanceLock`. |
| 9 | The import commit is not durable across power loss | Fixed in the code: both directories are synced before the journal says done and before it is removed. Not tested: a power loss cannot be injected here; the receipt says "process kill". |
| 10 | Recovery can orphan a staged credential | Fixed. While the keyring cannot be reached the journal keeps the reference and recovery reports it; the files are restored first. `TestStagedPasswordIsNeverOrphaned`. |
| 11 | `run` silently bypasses single-instance protection | Fixed. Without a runtime directory the lock lives beside the database; a lock that exists and cannot be taken stops the run; only a build with no lock at all (Windows, for now) runs with a warning. `TestRunIsASingleInstanceWithoutARuntimeDirectory`. |
| 12 | A blocked event consumer prevents the shutdown | Fixed. The end of the context stops the engine from its own goroutine; a reader that has stalled gets two seconds after the engine stopped. `TestAStalledReaderDoesNotKeepTheSoftphoneRunning`. |
| 13 | Interrupting an unanswered call returns 1 | Fixed. Being told to stop is not a failure. `TestStoppingBeforeTheAnswerIsNotAFailure`. |
| 14 | Receipt claims exceed the tests | Tests tightened (devices closed when `Stopped` is reported; tone in under 2 % of the frames during hold and mute; the loss of the microphone exactly once, in the lab and through the binary; the other microphone in no block at all; a killed import leaves its store open; recovery interrupted and recovery under lock). The receipt's wording is narrowed where a guarantee is not measured: nothing is measured on the wire during a hold, the kills are process kills and not power losses, and "disk full" is SQLite's full error forced with a page limit. |
