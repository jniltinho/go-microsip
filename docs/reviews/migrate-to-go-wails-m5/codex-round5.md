UNSOUND — section 8, tasks 8.1–8.9 and their evidence: stopping a pending audio test can now block call-event delivery and shutdown indefinitely.
UNSOUND — call-path changes: the malformed-datagram cause is removed, but `resound` can wait indefinitely without restarting either direction.

## Status of the earlier findings

- **Round 4, finding 1 — partially resolved.** The malformed plain-RTP datagram is now dropped; `TestADatagramThatIsNotRTPDoesNotStopTheCallsAudio` covers the original scenario. The untested `resound` recovery remains incorrect; see finding 2 below.
- **Round 4, finding 2 — partially resolved.** Origin/session-name changes no longer rewrite the session, and the early-media fixture covers both. Equivalent reordered or repeated attributes still take the unsafe update path.
- **Round 4, finding 3 — partially resolved.** Pending starts are registered, stopped late-opened devices are closed, and starts after `Close` do nothing. `TestAnAudioTestStoppedWhileItOpensNeverPlays` covers those ordinary paths, but not the blocking and page-ownership problems below.
- **Round 4, finding 4 — resolved by inspection.** `watchAddress` persists, rearms after observing recovery, and rejects obsolete generations. The response correctly discloses that the loop itself has no test.
- **Round 4, finding 5 — accepted as a stated limitation.** README explicitly describes an elsewhere-answered call being notified and listed as missed.
- **Round 4, finding 6 — resolved.** The post-open stop check precedes prefill; `TestATonesSpeakerThatOpensAfterTheCallEndedPlaysNothing` targets the reported scenario.
- **Round 4, finding 7 — resolved by inspection.** `picked` catches `File.text()` rejection and shows a notice without dispatching an import. No component test establishes this.
- **Round 4, finding 8 — resolved.** The fixture freezes counts at its answer boundary, and the assertion covers that interval. The receipt now states the actual aggregate bound rather than claiming a measured per-second maximum.
- **Round 4, finding 9 — partially resolved.** Producer failures, empty measurement output and Windows `NOT MEASURED` now fail the target. An omitted page measurement can still pass; see finding 5.
- **Round 4, finding 10 — resolved.** README distinguishes SIP keepalive from quiet-media RTP and limits the VPN observation to one installation.
- **Round 3, finding 3 — resolved by inspection.** The second originate now checks command status and the same Asterisk error text as the first. This revised script has not been demonstrated in a supplied run.
- **Round 3, finding 5 — accepted as a stated limitation.** Firewall-dialog identification remains indirect. The response acknowledges the conditions used; it does not establish which foreground window received Escape.

## New findings

### 1. Major — stopping an opening audio test can prevent shutdown and call-event delivery

**Area:** task 8.4  
**Files/functions:** `internal/core/app/audiotest.go:46–59,108–115`, `StartAudioTest`, `StopAudioTest`; `phone.go:172–179,705–724`, event pump and `Close`.

Registering the pending test fixes the forgotten-stop problem, but now `StopAudioTest` waits unconditionally for its `done` channel. While opening, only the return of `AudioDevices.Open` can close that channel. Opening has neither cancellation nor a bound in this contract.

**Concrete scenario:** the audio service or device driver stalls while Settings opens its streams. The user closes the phone. `Close` waits in `StopAudioTest` **before sending Shutdown**, so the engine’s bounded shutdown cannot help. Alternatively, an incoming call reaches the event pump, which waits in the same stop before forwarding its ringing snapshot. Subsequent call events also remain withheld.

This is an unbounded I/O join, not a demonstrated mutex cycle. The new 300 ms test always allows opening to finish and therefore cannot detect it.

**Fix:** cancel pending playback immediately without making the event pump or shutdown join an unbounded opening operation. Retain ownership so any devices returned late are closed. Alternatively, make opening genuinely cancellable and bounded. Add a barrier-controlled opening test that requires shutdown/event delivery to proceed before releasing the opening operation.

### 2. Major — `resound` cancels a context that cannot interrupt its receiver

**Area:** call-path recovery  
**Files/functions:** `internal/core/engine/actor.go:675–695`, `resound`; `call.go:159–174`, receive pump; `third_party/diago/media/media_session.go:894`, `ReadRTP`.

`c.stop()` cancels the pumps’ context, but the receiver checks that context only around `reader.Read`. Cancellation does not interrupt that read. `resound` waits for both pumps before posting their replacement.

**Concrete scenario:** an early-media silence keepalive encounters a transient outbound route error. The PBX subsequently answers, but is now silent—for example, waiting for a digit after its early announcement. The sender has exited; the receiver is blocked waiting for another packet. `resound` cancels and waits indefinitely. No replacement sender consumes microphone frames or queued digits, and the retained early failure does not start an established-call failure timer. The call remains Established but unusable until incoming media or hangup breaks the wait.

There is also no fresh receiving pipeline after a terminal receive error: `WithAudioReaderJitterBuffer` wraps the existing reader, which is already the old jitter buffer. That buffer retains its terminal `readErr`; wrapping it does not recover reception. In that case the replacement reports the old error again and normally ends the call after the grace period.

**Fix:** the smallest safe policy is to end the failed call rather than attempt this restart. If recovery is retained, explicitly interrupt and retire the old receiving pipeline, join it within a bound, and construct a fresh pipeline rather than wrapping a failed jitter buffer. Test send failure with a silent receiver, terminal receive failure, and hangup during recovery.

### 3. Minor — equivalent SDP can still rewrite the running media session

**Area:** vendored early-media comparison  
**Files/functions:** `third_party/diago/dialog_media.go:252–270,1014–1028`, `checkEarlyMedia`, `sameMedia`.

The comparison is an ordered comparison of selected lines, not effective negotiated media.

**Concrete scenario:** the answer exchanges the order of two independent `a=rtpmap` lines, or repeats an otherwise unchanged `a=sendrecv`. Address, port, codecs, direction and keys remain equivalent, but `sameMedia` returns false. `RemoteSDP` then writes the live session while its readers use it.

This is outside the receipt’s limitation concerning **genuinely different** answer media. I am not claiming a reproduced crash.

**Fix:** compare effective settings within their SDP sections, or make the fallback update synchronized. Do not globally sort attributes across media sections. Add equivalent-order/duplicate cases alongside cases that change address, port, codec, direction and SDES keys.

### 4. Minor — a departed Settings page can stop its replacement page’s test

**Area:** Settings lifecycle  
**File/function:** `frontend/src/components/SettingsView.vue:99–115`, `toggleTest`, unmount handler.

Unmount already requests a stop. When the old start eventually returns, the `gone` branch issues another **global** stop, without identifying which test it owns.

**Concrete scenario:** opening takes several seconds. Leave Settings, return, and start another test while the old opening is completing. The backend has removed the old test from `p.test`, allowing the new test to register. The old page’s continuation then calls `stopAudioTest()` and stops the new page’s test—whether it is running or still opening.

This does not require clicks within a fraction of a second.

**Fix:** scope late cleanup to the start it owns, or serialize the pending start/stop lifecycle so an obsolete continuation cannot stop a later test. Add a leave/re-enter/start component test with independently controlled completions.

### 5. Minor — `go-measure` can accept a missing first-page sample

**Area:** measurement completeness  
**Files/functions:** `build/go.mk:141–143`, `go-measure`; `build/vm/measure.ps1:56–61,74–87`, `Once` and measurement loop.

The target now propagates failures correctly, but the Windows script need not report a missing page measurement as failure or `NOT MEASURED`.

**Concrete scenario:** one warm Go start creates a native window but never reports its first drawn page. `Once` waits 90 seconds and returns a non-null `Window` with null `Drawn`. Later starts succeed. The script prints ten window samples but only nine page samples, exits zero, and emits no `NOT MEASURED`. The revised target succeeds.

**Fix:** require both milestones for every Go start and require the expected sample counts. Return failure when either is missing.

The saved Windows measurement log contains ten page samples; this finding does not invalidate those recorded numbers.

## Checked and found supported

### Datagram handling and SRTP

- Only plain-RTP **packet parsing failures** are converted into counted drops. Socket errors and the short-buffer check still return errors.
- Resetting the packet makes `RTPSession.ReadRTP` skip it before updating reception statistics. Each retry performs another socket read: one bad datagram does not create a self-sustaining busy loop.
- A continuous stream of garbage consumes work per arriving datagram, but does not become valid audio or refresh valid-packet statistics. The change introduces no new inactivity timer that such traffic could reset.
- The SRTP distinction is deliberate and documented. Unauthenticated/replayed packets retain their existing counted-drop path; authenticated payload-parsing failures remain errors. I found no new plaintext fallback or SRTP bypass. This is source inspection, not runtime proof of secure early media.

### `resound` ownership and privacy

The **device ownership and final-call handoff** are better than the restart’s cancellation:

- The call retains ownership of `c.audio`; `resound` neither opens another device nor takes ownership away from cleanup.
- If the call ends while recovery waits, `stopAudio` marks it released, closes media to unblock reception, waits for pumps, and closes the devices.
- The posted restart checks release/final state. Actor serialization prevents that check from racing another actor transition midway through starting the replacement.
- If replacement starts first, subsequent cleanup cancels the replacement pumps and closes the same devices.
- `onEarly` is cleared only after establishment. Hold/mute flags survive, and the replacement microphone’s first privacy transition discards queued capture. I found no demonstrated pre-answer microphone leak in this change.

Thus the defect is failed/bounded recovery, not a demonstrated double-close or device left running after the final call.

### Audio-test locking and page changes

- `p.mu` is released before device opening and before waiting for `done`; `chosen.devices` can obtain it. I found no internal `Close`/start mutex inversion.
- A second start while the first is registered as pending opens nothing more.
- The ordinary stopped-opening path closes returned devices without starting `runAudioTest`; the closed-phone guard prevents subsequent starts.
- The Settings `gone` check prevents the original departed-page polling installation. Finding 4 concerns its additional stop, not that original timer defect.
- CSV read rejection now produces the stated notice and never reaches import dispatch.

### Address watcher and tone

- The watcher remains attached to the endpoint context, reports an observed present-to-absent transition once, and rearms after an observed recovery.
- The callback checks generation and registration state. A result from an obsolete account cannot alter the new generation.
- Recovery during a call retains the endpoint, so the persistent watcher remains available for another loss.
- The tone’s second stop check occurs after opening and before all three prefill frames. Its named delayed-opening test targets that ordering.

These watcher conclusions are by reading; there is no supplied two-loss loop test.

### Scripts and retained evidence

- `go-measure` quotes captured output correctly and preserves each Vagrant producer’s nonzero status. The final Windows `if` also returns success when its failure condition is absent; it does not turn an ordinary successful run into failure.
- The second PBX originate now captures and checks its result before claiming success.
- The saved reports contain 77 Asterisk, 10 FreePBX and eight impaired scenarios, with zero reported failures. The final AppImage log contains 29 passing subtests.
- The Windows log supports the ringing-to-missed notification sequence and still explicitly ends with **FAIL: audio**.
- The SBOM log records 81 components; the saved checksum manifest has six entries, including the SBOM. This is not verification of omitted artifact bytes.

### Claims requiring qualification

The response’s broad “fixed” claims for recovery, semantic SDP equality and pending-test lifecycle exceed what the code establishes, as findings 1–4 explain.

The parity receipt also retains two narrower evidence qualifications:

- Its CSV reducer test covers confirmation and rejected preview/header handling, **not rejection of `File.text()`**. That component path is now supported by code inspection only.
- The VPN gateway explanation remains an inference from the owner’s unrecorded observations, not an independently established cause.

The README’s narrowed keepalive and elsewhere-answered-call limitations are supported. The documented open work—different answer media, SRTP early-media runs, physical hardware and Windows gaps—should not be treated as newly proven. `make go-test` invokes the race detector, but neither that recipe nor this review establishes that the revised tests were actually run.

### Sensitive material

I found no apparent private SIP account, customer server name, real owner telephone number, credential or personal home path. The accounts and addresses inspected are lab/fixture data.

The extract **does contain public upstream identities and contact email addresses**, including the vendored README and font-license notices. Those are public attribution material, not evidence of an owner-account leak.

**Not checked:** no builds, tests, scripts, applications or VMs were run; omitted images, binaries, artifact hashes and unrecorded desktop calls were not independently verified.  
This review is not evidence that a call works; the two major lifecycle defects require fixes before acceptance.