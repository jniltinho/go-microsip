# Independent review of milestone M1 (Codex)

Each round reviews a sanitized extract made with `git archive` (no third-party sources except the two patched diago files, no fixtures, nothing from `base/`, no secrets or personal paths), with `codex exec --sandbox read-only --skip-git-repo-check --ephemeral`. A review is not call evidence; the evidence is in [the M1 receipt](../../receipts/go-m1-engine-2026-11-11.md).

| Round | Commit | Verdict | Report |
| --- | --- | --- | --- |
| 1 | `60cd1a8` | UNSOUND: 1 critical, 12 major, 1 minor | [codex-round1.md](codex-round1.md) |
| 2 | `2eedb96` | UNSOUND: of the 14, 6 resolved and 8 partially; 6 new major | [codex-round2.md](codex-round2.md) |
| 3 | `e856d17` | UNSOUND: earlier findings resolved or accepted except three partial; 3 new major, 2 minor | [codex-round3.md](codex-round3.md) |

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

## What was done about round 2

Round 2 found six of the fourteen resolved (3, 4, 5, 8, 10, 13), the others partially, and six new problems, most of them about which lock protects the database and when.

| # | Finding | Response |
| --- | --- | --- |
| N1 | Different sessions lock different files | Fixed. The instance lock is `instance.lock` beside the database, whatever the session: one lock per database. `TestTheInstanceLockBelongsToTheDatabase`. (Also closes the rest of finding 11.) |
| N2 | A start without a runtime directory skips an unfinished import | Fixed. Undoing an import needs go-microsip's own files only: the importer is built without the Rust application's paths when the session cannot reach them, and the journal records the configuration file it moved. A build with no lock refuses to start beside an unfinished import. `TestStartUndoesAnInterruptedImportInAnySession`. |
| N3 | `config import --apply` checks, then writes without the lock | Fixed. `run` and `config import --apply` share one step that takes the lock, undoes an interrupted import and keeps the lock until the store is closed; under another holder the import refuses. This is stricter than the Rust candidate, which stored settings beside a running application. `TestConfigImportNeedsTheInstanceLock`. |
| N4 | A stalled reporter outlives the database session | Fixed. Reporting has its own goroutine and queue; the loop that journals never waits for it, the history is closed before the lock is released, and events a stalled reader did not take are counted in a warning. (Also closes the rest of finding 12.) |
| N5 | Closing sound devices inside a result can hold up `Stopped` | Fixed. A device that was still opening when the call ended is closed by the goroutine that opened it, never on the actor, and the shutdown waits for that too, inside its time. If the sound service itself does not answer within the five seconds, `Stopped` is reported and the receipt says so. |
| N6 | A failing result abandons the rest and can revive an account | Fixed. Every result is applied on its own; when one fails in the actor the rest of its batch goes back to the queue and is applied at the stop; the failure path invalidates the account and the call first, so a registration that completes late is closed, not installed. `TestAFailingResultDoesNotAbandonTheOthers`. (Also closes the rest of finding 1.) |
| 6 | Catching up shifts the deadline of a missing packet further on | Fixed. Catch-up counts only the packets in sequence from the next one; packets behind a gap bring nothing forward. The test now delivers the late packet at 50 ms of its 60 ms; it fails on the round-2 code (3 of 3 runs). |
| 7 | Directory ownership, Windows | Not changed: the mode is checked, the owner is not, and Windows has no mode bits. Recorded as a shortcut in the code for the Windows adapters (M4). |
| 9 | New directories are not durably linked | Fixed in the code: a directory the import creates is synced into its parent. Still not tested: a power loss cannot be injected here. |
| 14 | Receipt wording | The lab now counts every notice of a phone to its last event, so "exactly once" covers the whole scenario. "Closed at `Stopped`" is worded as what is checked: no sound stream is open when the event stream closes, right after `Stopped`. The summary of round 1 is restated below. |

Of the fourteen findings of round 1, after both rounds: thirteen are fixed in code and one (7) is fixed for the mode and recorded as a shortcut for the owner and for Windows. Four fixes have no test of their own, because the lab cannot produce the situation: a shutdown while an Answer is in flight, a media direction that fails alone, a transfer whose verdict arrives with the other side's hang-up, and durability across a power loss.

## What was done about round 3

| # | Finding | Response |
| --- | --- | --- |
| 1 | Shutdown waits for an audio result only the blocked actor can settle | Fixed. Devices that are open and not yet started are handed over under a small lock: the actor takes them to start them, or the stop of the call's audio takes them and closes them, and neither waits for the other. `TestOpenedDevicesAreClosedWhoeverComesFirst` covers the three orders, the queued result overtaken by the stop among them. (Closes the rest of N5 and of finding 2.) |
| 2 | Recovering an earlier import redirects the next one | Fixed. The journal's configuration path is used inside the recovery only. `TestRecoveryDoesNotRedirectTheNextImport`. |
| 3 | The reporting queue can drop final snapshots and `Stopped` | Fixed. Reporting goes through `engine.Relay`, which applies the engine's own rule to a reader that is behind: intermediate states merged, final snapshots and `Stopped` always delivered, in order. `TestRelayKeepsFinalStatesForAReaderThatWasAway`. |
| 4 | The stalled-output warning can block `Run` | Fixed as best effort: the warning is sent without waiting for it, so the history is closed and the lock released whatever the output does. |
| 5 | A nil locker skips the unfinished-import check | Fixed. Without a locker the journal is still looked for, and `run` and `config import --apply` refuse beside one. The test covers a nil locker and a platform without the lock. (Closes the rest of N2.) |
| 14 | "History recovered after a kill" | Reworded: the store test closes the database cleanly and reopens it; it shows that calls left unfinished are closed as interrupted at the next start, not a kill. |
