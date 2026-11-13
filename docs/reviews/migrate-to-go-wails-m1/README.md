# Independent review of milestone M1 (Codex)

Each round reviews a sanitized extract made with `git archive` (no third-party sources except the two patched diago files, no fixtures, nothing from `base/`, no secrets or personal paths), with `codex exec --sandbox read-only --skip-git-repo-check --ephemeral`. A review is not call evidence; the evidence is in [the M1 receipt](../../receipts/go-m1-engine-2026-11-11.md).

| Round | Commit | Verdict | Report |
| --- | --- | --- | --- |
| 1 | `60cd1a8` | UNSOUND: 1 critical, 12 major, 1 minor | [codex-round1.md](codex-round1.md) |
| 2 | `2eedb96` | UNSOUND: of the 14, 6 resolved and 8 partially; 6 new major | [codex-round2.md](codex-round2.md) |
| 3 | `e856d17` | UNSOUND: earlier findings resolved or accepted except three partial; 3 new major, 2 minor | [codex-round3.md](codex-round3.md) |
| 4 | `8483f9f` | UNSOUND: everything earlier resolved or accepted; 1 new major | [codex-round4.md](codex-round4.md) |
| 5 | `cf8e470` | UNSOUND: everything earlier resolved; 1 new major, 1 minor | [codex-round5.md](codex-round5.md) |
| 6 | `c2fe87a` | UNSOUND: everything earlier resolved; 1 new major | [codex-round6.md](codex-round6.md) |
| 7 | `1d2ff41` | UNSOUND: round 6's fix partial (a hold or mute between two sender ticks); nothing else | [codex-round7.md](codex-round7.md) |
| 8 | `0ef5947` | UNSOUND: round 7's fix partial (a hold or mute between the sender's reading of the state and of the microphone); nothing else | [codex-round8.md](codex-round8.md) |
| 9 | `8da40b9` | UNSOUND: round 8's fix partial (the sender reading between the two steps of one change of hold or mute); nothing else | [codex-round9.md](codex-round9.md) |
| 10 | `9cf56bf` | UNSOUND: round 9's race resolved; 2 major (the adapter's 100 ms discard window, added in that commit; the jitter buffer's catch-up before a gap further on) | [codex-round10.md](codex-round10.md) |
| 11 | `0308c18` | SOUND: both problems of round 10 resolved; no new problem | [codex-round11.md](codex-round11.md) |

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

## What was done about round 4

| # | Finding | Response |
| --- | --- | --- |
| 1 | A later call can hide unfinished device cleanup from the shutdown | Fixed. The actor counts the calls whose sound devices are not closed yet, every one of them, and the shutdown waits for that count inside its time. `TestShutdownWaitsForTheDevicesOfEveryCall` gates the first call's close, ends a second call at once, and requires the wait to last until the gate opens. (Closes the rest of N5 and of finding 2.) |
| - | Test counts in the receipt | Corrected: 19 configuration, 24 store and 13 importer tests. |

## What was done about round 5

| # | Finding | Response |
| --- | --- | --- |
| 1 | Merging intermediate states can erase that a call was answered | Fixed. Every snapshot carries when the call began, was first established and ended (`StartedMS`, `AnsweredMS`, `EndedMS`), the final one included; the history and `run` read "answered" and the times from the snapshot, never from having seen "established". `TestTheFinalSnapshotSaysTheCallWasAnswered` merges the established snapshot away; `TestHistoryDoesNotDependOnSeeingTheCallEstablished` stores a call seen only ringing and ended; the lab's pair and reject scenarios check the final snapshot's times. |
| 2 | A configuration directory on another file system | Fixed. When the rename cannot reach, the file is copied beside its destination, synced and renamed there; a copy left on its way is removed by the recovery. `TestConfigurationOnAnotherFileSystem` forces the fallback (a real second file system is not used). |
| - | Receipt wording | "The voice is gone" now says what is asserted (at most 10 of 200 pieces, measured 0), and the sequential-cleanup test is described as what it is: a test of the count the shutdown waits for, not of a whole shutdown. |

## What was done about round 6

| # | Finding | Response |
| --- | --- | --- |
| 1 | Microphone audio recorded during a hold can be sent after the resume | Fixed, for a mute too, which had the same gap in small. The sound port has a new call, `Discard`, that drops everything the microphone recorded and did not hand out; the engine calls it when a hold or a mute ends, after reading and dropping one frame per tick meanwhile. The decision is one small type (`microphone` in `call.go`) that the test drives with a microphone that records on its own, whether it is read or not: `TestNothingRecordedOnHoldIsEverSent` (hold and mute), and `TestDiscardDropsEverythingCaptured` for the real adapter's ring. A first attempt that drained by reading until nothing came was caught by the lab: a microphone that always has a frame never ran dry and the call stayed silent after the resume. |
| - | Receipt | It now says that the lab's hold and mute scenarios use a microphone that produces audio only when asked, so they cannot see this; the unit tests above are what covers it. |

## What was done about round 7

| # | Finding | Response |
| --- | --- | --- |
| 1 | A hold or a mute entirely between two sender ticks is forgotten | Fixed. The actor counts every change of hold and mute, before and after it sets the flag; the send goroutine drops what the microphone holds whenever the count is one it has not acted on, whether or not it ever saw the flag set. `TestPrivacyBetweenTwoTicksIsNotMissed` mutes, records and unmutes (and the same with a hold) without a tick in between. The helper and the adapter's ring are tested each on its own; the two goroutines together are not driven by a test. |
| - | Found by the lab while this was verified | The fix of round 1's finding 4 ended a call as failed when its audio stopped a moment before the dialog reported the other side's hang-up (one lab run in about ten). A dialog now has half a second to turn out ended before stopped audio counts as a failure; the shutdown scenarios then passed 6 runs of 3 and the pair scenarios 3 runs of 2. |

## What was done about round 8

| # | Finding | Response |
| --- | --- | --- |
| 1 | A hold or a mute after the sender read the call's state and before it read the microphone | Fixed. The sender reads the count of changes again once it has the frame, and does not send a frame taken while the count moved; the count, still not acted on, makes the next tick drop what the device holds. As the count moves before the flag does, a frame that passes was recorded before any change began. `TestPrivacyBetweenTwoTicksIsNotMissed` now also mutes, records and unmutes (and the same with a hold) from inside the reading of the microphone; with the second reading switched off it fails. It is still the helper that is tested, with the actor's part played inside one goroutine. |
| - | Round 8 on `mediaGrace` | Recorded as a limitation in the receipt: a dialog that takes longer than half a second to report its end after the audio stopped is ended as failed. A media direction that fails alone still has no test of its own. |

## What was done about round 9

| # | Finding | Response |
| --- | --- | --- |
| 1 | A change of hold or mute seen half done: the count moved, the flag not yet | Fixed at the root of rounds 7 to 9: hold, mute and the count of their changes are now one atomic word, changed in one step, so there is no half-done change to see and the sender never reads one without the others. A frame is sent only if the word was the same, without hold or mute, when the device's queue was last dropped, before the frame was taken and after. `TestPrivacyWordChangesAtOnce` checks the word; `TestPrivacyBetweenTwoTicksIsNotMissed` gained a hold or mute that begins inside the reading of the microphone and stays; `TestNothingPrivateIsSentAgainstARacingActor` runs the device, the actor's changes and the sender's decision on three goroutines under the race detector, with every sample marked by what the call was when it was recorded, and fails with the second reading switched off. |
| - | Round 9 on the receipt's wording | Narrowed: a frame taken before a hold or a mute began can leave just after it (up to one 20 ms frame of audio from before the change); "nothing while held by construction" is gone. The racing test drives the helper and the word, not the engine's send loop on a call with a real sound device. |
| - | Found while answering | A real device delivers a sample some time after it recorded it, so sound from the last moments of a mute could reach the adapter's queue after the queue was dropped. The adapter's discard now also refuses what the device delivers in the next 100 ms, a fixed bound (five times the delay asked of the sound server) that is not measured on real hardware; `TestDiscardDropsEverythingCaptured` covers the late delivery. The first 100 ms after a resume or an unmute are therefore silence. |

## What was done about round 10

| # | Finding | Response |
| --- | --- | --- |
| 1 | The adapter's discard trusted a 100 ms window: a recording delivered later than that came out | Fixed. A discard no longer ends with time. From the discard on, the adapter drops everything the device delivers until the backend confirms that the device is past it. With PulseAudio and PipeWire the service is asked to drop what it holds, and its answer comes through the connection that delivers the recordings, in order: when it is here, every recording sent before it has been delivered and dropped. With WASAPI the capture thread confirms when it finds the device with nothing more to hand over, with nothing in its own hands. After the confirmation the first samples delivered are dropped by count, not by time: 100 ms at least, plus the latency the service reports for the device. If the service does not answer, nothing more is taken from the microphone and it is reported lost. `TestDiscardDropsEverythingCaptured` delivers the old recording 150 ms after the discard and then counts the samples out; `TestPulseLoopback` discards against the real service (confirmed in 0.1 ms, microphone back after 165 ms). |
| 2 | Catch-up before a gap further on brings the missing packet's deadline forward | Fixed. The buffer catches up only while nothing is queued behind a gap. `TestJitterBufferWaitsForALatePacketBehindABurst` gained the reviewer's case (packet 8 missing behind 4 to 7); it fails on the previous code. |

What remains and is recorded in the receipt: the count dropped after a confirmed discard stands for what a device has recorded and not handed over yet. It is taken from the service's own report, with a floor; it is not a recording time, which the protocol does not give. The WASAPI path is compiled and not run: Windows audio on real hardware is the open task 2.8.

## Round 11

Sound, on `0308c18`, with nothing to change. What the report leaves standing is already in the receipt as not covered:

- A frame taken before a hold or a mute began can leave after it: 20 ms of sound from before the change, at most.
- The privacy tests drive the decision of what is sent and the adapter each on its own, not a held or muted call on the engine with a real sound device.
- The count of samples dropped after a confirmed discard comes from the sound service's report and is not validated on physical devices; the Windows path is compiled, not run (task 2.8).

The reviewer did not build or run anything: the test runs, the lab and the sound measurements are the receipt's, not the review's.
