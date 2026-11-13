# Independent review of milestone M5's work (Codex)

Each round reviews a sanitized extract made with `git archive` (the Go code, the page's source, the build and lab scripts, the vendored SIP library, the documents and receipts with their logs, the change's planning artifacts and the Rust engine for comparison; no pictures, fonts or build output), with the diff since the commit the previous round read. Codex ran with `codex exec --sandbox read-only`; nothing was built or run by it. A review is not evidence that a call works.

| Round | Commit | Scope | Verdict | Report |
| --- | --- | --- | --- | --- |
| 4 | `c43ba37` | the answers to round 3 of M4, task 7.3, tasks 8.1 to 8.9, and the call-path changes made after real calls (early media, the frame of silence, the lost address, Contact and User-Agent, the change to the vendored library) | section 8: SOUND WITH CHANGES; call path: UNSOUND, 1 major; 9 minor in all (7 of round 3's 9 findings resolved, 2 in part) | [codex-round4.md](codex-round4.md) |

## What was done about round 4

| # | Severity | Finding | Response |
| --- | --- | --- | --- |
| 1 | major | Audio that stopped before the answer (the receiving goroutine ended on an error) left the answered call without a receiving side, and the failure was forgotten | Fixed, at its cause and at the place the review named. The cause: one datagram on the audio port that is not an RTP packet, from the PBX's own address, was returned as an error which the library's readers kept, so nothing was received again, before the answer or in an answered call. The vendored library now drops and counts such a datagram (`DroppedMalformed`), as it does a packet from another address. `TestADatagramThatIsNotRTPDoesNotStopTheCallsAudio`: the PBX's audio is heard before the answer and after it. And a failure of the audio before the answer is now kept (`earlyFailed`): the answer starts the call's sound again on the devices it has (`resound`), and a failure after that ends the call as for any answered call. No test injects a failure that survives the first fix, so `resound` is not exercised by a test. |
| 2 | minor | An answer that differed from the 183 only in the session's name was applied again to the live session | Fixed. `sameMedia` compares the connection, media and attribute lines only. The early-media test's PBX now answers with another origin version and another session name, under the race detector. An answer whose audio really differs is still applied in place, as upstream does: stated as open in the receipt. |
| 3 | minor | A sound test stopped while its devices were opening started anyway, also after `Close`; the settings page could start its polling after it was left | Fixed. The test is registered before its devices are opened, so a stop, a call or `Close` sees it; devices that open after the stop are closed and nothing plays; no test starts on a closed phone. The page stops a test whose start returns after the settings were left. `TestAnAudioTestStoppedWhileItOpensNeverPlays`, with a sound service that takes 300 ms to open. The page's part has no test. |
| 4 | minor | After a registration recovered on the endpoint a call kept alive, its address was no longer watched | Fixed. The watcher lives as long as the endpoint and tells the actor once for each time the address goes. No test: the rule that decides (`assigned`) has one, the loop has none. |
| 5 | minor | A call answered on another phone of the same extension is notified here as missed | Not changed: recorded. The softphone does not read the Reason of the CANCEL that ends the ringing, and its outcomes have no "answered elsewhere". The README's limits say so; it is the owner's to decide whether to add it (it needs a new end reason through the core's contract, the history and the page). |
| 6 | minor | A tone whose speaker opened after the call was answered or over still queued three frames | Fixed. The stop is looked at again once the speaker is open, before anything is queued. `TestATonesSpeakerThatOpensAfterTheCallEndedPlaysNothing`. |
| 7 | minor | A CSV file that could not be read ended the import silently | Fixed. The page says "Not imported: the file could not be read". No test: the page's component has none. |
| 8 | minor | The early-media test looked at what was sent before the answer only once, and its bound did not show "one a second" | Fixed. The test's PBX records both counts at the moment it answers, and the test requires one to three packets, none of them anything but silence, for the two and a half seconds before the answer. The receipt's wording follows the test. |
| 9 | minor | `make go-measure` could succeed after a VM's measurement failed | Fixed. The target fails when a VM's script fails, prints no measurement, or reports something not measured. Checked with `make -n` only: the measurement was not run again. |
| 10 | minor | The README said "no keepalive" beside the frame of silence | Fixed. "No SIP keepalive", with the frame of silence named, and the VPN observation described as one installation's behavior. |

Round 3's two findings that round 4 found resolved in part:

| # | Finding | Response |
| --- | --- | --- |
| 3 | The second call `pbx.sh ring` originates did not check Asterisk's answer | Fixed: checked as the first one is. Not run again in the VM. |
| 5 | `Asked` in `call.ps1` identifies the firewall's question indirectly | Not changed: stated in the M4 receipt. The question cannot be read by UI automation; the script answers only when new firewall rules for go-microsip appeared and a window that is not the softphone's holds the keyboard. |
