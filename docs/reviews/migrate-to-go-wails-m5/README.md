# Independent review of milestone M5's work (Codex)

Each round reviews a sanitized extract made with `git archive` (the Go code, the page's source, the build and lab scripts, the vendored SIP library, the documents and receipts with their logs, the change's planning artifacts and the Rust engine for comparison; no pictures, fonts or build output), with the diff since the commit the previous round read. Codex ran with `codex exec --sandbox read-only`; nothing was built or run by it. A review is not evidence that a call works.

| Round | Commit | Scope | Verdict | Report |
| --- | --- | --- | --- | --- |
| 4 | `c43ba37` | the answers to round 3 of M4, task 7.3, tasks 8.1 to 8.9, and the call-path changes made after real calls (early media, the frame of silence, the lost address, Contact and User-Agent, the change to the vendored library) | section 8: SOUND WITH CHANGES; call path: UNSOUND, 1 major; 9 minor in all (7 of round 3's 9 findings resolved, 2 in part) | [codex-round4.md](codex-round4.md) |
| 5 | `80a9b06` | the answers to round 4 | section 8: UNSOUND, 1 major; call path: UNSOUND, 1 major; 3 minor. Both major findings are defects of the answers to round 4, not of the work round 4 read (of the 12 earlier findings: 6 resolved, 4 in part, 2 accepted as stated limitations) | [codex-round5.md](codex-round5.md) |

## What was done about round 4

Where round 5 found an answer wrong, the row says so and the next section has what replaced it.

| # | Severity | Finding | Response |
| --- | --- | --- | --- |
| 1 | major | Audio that stopped before the answer (the receiving goroutine ended on an error) left the answered call without a receiving side, and the failure was forgotten | Fixed at its cause: one datagram on the audio port that is not an RTP packet, from the PBX's own address, was returned as an error which the library's readers kept, so nothing was received again, before the answer or in an answered call. The vendored library now drops and counts such a datagram (`DroppedMalformed`), as it does a packet from another address. `TestADatagramThatIsNotRTPDoesNotStopTheCallsAudio`: the PBX's audio is heard before the answer and after it. The failure is also kept (`earlyFailed`). The first answer then started the call's sound again at the answer; round 5 found that wrong (its finding 2). |
| 2 | minor | An answer that differed from the 183 only in the session's name was applied again to the live session | Fixed for the origin and the session's name; round 5 found the comparison still too strict (its finding 3). |
| 3 | minor | A sound test stopped while its devices were opening started anyway, also after `Close`; the settings page could start its polling after it was left | Fixed: the test is registered before its devices are opened, so a stop, a call or `Close` sees it; devices that open after the stop are closed and nothing plays; no test starts on a closed phone. `TestAnAudioTestStoppedWhileItOpensNeverPlays`. Round 5 found that the stop then waited for the sound service (its finding 1) and that the page's late stop could hit another page's test (its finding 4). |
| 4 | minor | After a registration recovered on the endpoint a call kept alive, its address was no longer watched | Fixed. The watcher lives as long as the endpoint and tells the actor once for each time the address goes. No test: the rule that decides (`assigned`) has one, the loop has none. |
| 5 | minor | A call answered on another phone of the same extension is notified here as missed | Not changed: recorded. The softphone does not read the Reason of the CANCEL that ends the ringing, and its outcomes have no "answered elsewhere". The README's limits say so; it is the owner's to decide whether to add it (it needs a new end reason through the core's contract, the history and the page). |
| 6 | minor | A tone whose speaker opened after the call was answered or over still queued three frames | Fixed. The stop is looked at again once the speaker is open, before anything is queued. `TestATonesSpeakerThatOpensAfterTheCallEndedPlaysNothing`. |
| 7 | minor | A CSV file that could not be read ended the import silently | Fixed. The page says "Not imported: the file could not be read". No test: the page's component has none. |
| 8 | minor | The early-media test looked at what was sent before the answer only once, and its bound did not show "one a second" | Fixed. The test's PBX records both counts at the moment it answers, and the test requires one to three packets, none of them anything but silence, for the two and a half seconds before the answer. The receipt's wording follows the test. |
| 9 | minor | `make go-measure` could succeed after a VM's measurement failed | Fixed for a failed script, no output and something not measured; round 5 found one more way (its finding 5). Checked with `make -n` only: the measurement was not run again. |
| 10 | minor | The README said "no keepalive" beside the frame of silence | Fixed. "No SIP keepalive", with the frame of silence named, and the VPN observation described as one installation's behavior. |

Round 3's two findings that round 4 found resolved in part:

| # | Finding | Response |
| --- | --- | --- |
| 3 | The second call `pbx.sh ring` originates did not check Asterisk's answer | Fixed: checked as the first one is. Not run again in the VM. |
| 5 | `Asked` in `call.ps1` identifies the firewall's question indirectly | Not changed: stated in the M4 receipt. The question cannot be read by UI automation; the script answers only when new firewall rules for go-microsip appeared and a window that is not the softphone's holds the keyboard. |

## What was done about round 5

| # | Severity | Finding | Response |
| --- | --- | --- | --- |
| 1 | major | Stopping a sound test whose devices were still opening waited for the sound service, without a bound, from `Close` (before the engine was told to shut down) and from the event pump (before a call's first event was passed on) | Fixed. `StopAudioTest` waits only for a test whose devices are open, which ends within moments; for one that is still opening it marks the stop and returns, and the devices are closed, unused, when they do open. `TestAStalledSoundServiceDoesNotHoldTheStopOrTheClose`: with a sound service that does not answer, the stop and `Close` return, and the devices that open afterwards are closed. |
| 2 | major | `resound` cancelled a context that does not interrupt a receiver waiting for a packet, and waited for it; and what it started again wrapped the reader that had failed | Fixed by removing it, as the report proposed. A call whose audio ended on its own before the answer is ended when it is answered (`established`: `terminate` with "failed"), as an answered call whose audio ends is; its microphone was never sent. And a frame of silence that cannot be sent is no longer a failure of the call: it is tried again a second later. No test brings about an audio failure before the answer that is not the malformed datagram, so the ending at the answer is not exercised by a test. |
| 3 | minor | Lines in another order, or a repeated line, made an answer with the same audio look different | Fixed. `sameMedia` compares the connection and attribute lines of each section as a set; the m= line, which holds the order of the payload types, starts a section and is compared with it. The early-media test's PBX answers with its attributes in another order and one of them twice, under the race detector. An answer that changes an address, a port, a codec, a direction or a key differs in one of those lines and is applied as upstream does; no test of its own for those. |
| 4 | minor | A settings page that was left could, when its start returned, stop the test of the page opened after it | Fixed. Leaving stops the start (which now takes effect at once, by finding 1's fix), and the late return does nothing more. No test: the page's component has none. |
| 5 | minor | A Windows start that opened a window and never drew its page was left out of the page's samples without failing | Fixed. Such a start is "NOT MEASURED", which fails the target. Not run again in the VM. |
