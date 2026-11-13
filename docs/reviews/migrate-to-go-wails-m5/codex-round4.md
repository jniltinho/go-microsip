SOUND WITH CHANGES — tasks 8.1–8.9: the source and saved lab, Windows and SBOM evidence support the narrowed acceptance; minor issues remain.
UNSOUND — call-path changes: an early-media receive failure can be forgotten, leaving an answered call permanently without receive audio.

## Round 3 findings

1. **Resolved.** `freepbx.sh setup` saves the IPv4 fence and blocks IPv6 on the lab interface. Restoration after reboot remains explicitly untested.
2. **Resolved.** Both fence scripts construct the replacement, including DROP, before switching traffic and removing the old chain.
3. **Partially resolved.** Boolean probes, the first originate and fence reporting now check failures. The newly added second originate in `pbx.sh ring` discards its output: an Asterisk error reported with exit zero still produces “called … again.”
4. **Resolved.** `run.sh` and `interop.sh` install exit cleanup, preserve the original status and report failed destruction. The teardown path has not been exercised.
5. **Partially resolved.** `Key` refuses unconfirmed foreground delivery. `Asked` still identifies the firewall question indirectly; another foreground window coinciding with new firewall rules can receive Escape.
6. **Resolved.** `desktop.ps1` requires the notifier’s matching callback acknowledgement, not merely test exit zero.
7. **Resolved.** `call.ps1` requires a notification while ringing before claiming removal; unreadable history is reported as unverified.
8. **Resolved.** Windows removal failures remain pending and are retried before another toast, rather than after it.
9. **Resolved.** `forget` no longer initializes WinRT. The saved Windows missed-call run also demonstrates successful notification submission and removal with this arrangement.

## New findings

### 1. Major — an early receiver can die without the answered call noticing

**Area:** call path  
**Files/functions:** `internal/core/engine/call.go:99–102,156–164`, `startAudio`; `actor.go:585–596,664–666`, `established`, `mediaFailed`.

A receive error terminates the receive goroutine. Its failure callback is ignored while the call is Ringing. At the answer, `established` treats non-nil `settled` as sufficient reason to reuse the audio and merely clears `onEarly`.

**Failing scenario:** after a 183, a short malformed RTP packet from the negotiated PBX address causes the reader to return an error. The PBX subsequently answers normally. The call becomes Established and can send the microphone, but its receiver is gone permanently. The shared `once` has also consumed that failure notification.

**Fix:** handle terminal pump failures during early media. Cancel and release that dial, or retain the failure and safely restart before declaring the audio usable. Add a test that injects a receive error before a subsequent 200 response.

### 2. Minor — even unchanged audio can still take the unsynchronized SDP-update path

**Area:** vendored early-media change  
**Files/functions:** `third_party/diago/dialog_media.go:252–270,1012`, `checkEarlyMedia`, `sameMedia`; `media/media_session.go`, `RemoteSDP`.

`sameMedia` compares every non-empty line except `o=`. A changed session label therefore triggers `RemoteSDP` on the live session, rewriting fields such as `Raddr`, mode and codec slices while RTP readers use them without `DialogMedia.mu`.

**Failing scenario:** the 183 and 200 describe identical codecs, addresses and security, but use different `s=` text. The live-session data race returns. This is broader than the documented open limitation concerning genuinely different answer media.

**Fix:** compare effective negotiated media, including security, rather than unrelated SDP metadata. Unsupported changes should be rejected or applied through a synchronized replacement—not an in-place update. Add a differing-session-label case under the race detector. I am not claiming a reproduced crash.

### 3. Minor — stopping the audio test does not cover a start still opening devices

**Task:** 8.4  
**Files/functions:** `internal/core/app/audiotest.go:30–58,103–110`, `StartAudioTest`, `StopAudioTest`; `phone.go`, `Close`; `frontend/src/components/SettingsView.vue:92–112`.

The devices open before `p.test` exists. A stop during that opening sees nothing. There is also no closed-phone check before the late start is installed. The component marks itself running only after awaiting that start.

**Failing scenario:** device opening takes several seconds. The user leaves Settings—or `Phone.Close` completes—before it returns. The late result can start the ten-second test anyway; the departed component can also install its polling timer afterward.

**Fix:** represent a pending start before opening, invalidate it on stop/close, and close late-opened devices without starting playback. Guard the component’s pending continuation on unmount. Test with a deliberately delayed open.

### 4. Minor — address monitoring disappears after recovery on an existing call

**Area:** lost-address recovery  
**File/functions:** `internal/core/engine/actor.go:273–288,335–353`, `watchAddress`, `reregister`.

The watcher returns after its first detected loss. Recovery through `reregister`, which preserves an endpoint supporting a call, starts renewal but not another address watcher.

**Failing scenario:** during a call, the address briefly disappears at a polling instant and returns before the retry. Re-registration succeeds on the same endpoint. A later address loss is no longer detected within seconds; registration can remain displayed as Registered until renewal.

**Fix:** keep one watcher alive for the endpoint’s lifetime, or explicitly rearm it without creating duplicate watchers. Test two losses separated by successful recovery during one call.

### 5. Minor — answering elsewhere produces a missed-call notification here

**Task:** 8.6  
**Files/functions:** `internal/core/engine/actor.go:702–709`, `watch`; `internal/gui/service.go:141–149`, `forward`.

A ringing dialog’s cancellation always becomes `EndCanceled`. The notification decision has no information distinguishing caller abandonment from completion on another phone.

**Failing scenario:** a PBX rings this softphone and another endpoint. The other endpoint answers; the PBX cancels this branch as completed elsewhere. This window announces “Missed call” despite the call having been answered.

**Fix:** preserve the cancellation/completion reason in the call outcome and suppress missed notifications for completion elsewhere. Add a PBX cancellation-reason test.

### 6. Minor — a canceled tone opening still queues fresh ringing frames

**Task:** 8.3  
**File/function:** `internal/core/engine/tone.go:115–140`, `sound`.

Cancellation is checked before `OpenOutput`, but not after it. The three initial frames are always queued.

**Failing scenario:** opening the output stalls; the user answers or refuses the call meanwhile. When opening eventually completes, the goroutine queues fresh ringing frames before observing the already-closed stop channel. Whether they become audible depends on device scheduling.

**Fix:** recheck cancellation immediately after opening and before prefill. A delayed-output test should require no `Play` calls after cancellation.

### 7. Minor — an actual CSV file-read failure has no user-facing error

**Task:** 8.7  
**File/function:** `frontend/src/components/ContactsTab.vue:30–36`, `picked`.

`file.text()` is awaited outside error handling. The reducer’s error handling only covers the subsequent preview request.

**Failing scenario:** a selected file disappears or its backing device becomes unreadable before reading completes. Import silently stops with an unhandled rejected promise rather than a notice.

**Fix:** catch the file-read failure and show “Not imported.” The named reducer test exercises a rejected preview/header, not a rejected `File.text()` operation.

### 8. Minor — the early-media test does not establish privacy over the whole pre-answer interval

**Area:** call-path evidence  
**File/function:** `internal/core/app/audiotest_test.go:433–451`, `TestAudioBeforeTheAnswerIsPlayedAndOnlySilenceIsSent`; parity receipt, “What real calls showed.”

The test checks the counters once, after the first keepalive, then waits for the answer without checking the remaining pre-answer interval. Its upper bound of three packets also does not establish the receipt’s “at most one packet a second.”

**Failing scenario:** a regression starts microphone transmission at 1.5 seconds, before the fixture’s answer at 2.5 seconds. The earlier silence assertion can pass; those leaked packets then satisfy the post-answer loudness assertion.

**Fix:** freeze and assert the PBX’s pre-answer counters at its answer boundary, and check pacing across that interval. The present production privacy logic appears correct; this is a test/evidence gap.

### 9. Minor — the measurement target can succeed after an incomplete measurement

**Task:** 7.3 / VM harness  
**Files/functions:** `build/go.mk:133–137`, `go-measure`; `build/vm/measure.ps1`, measurement loop.

The `vagrant … | grep` pipelines return the filter’s status, not the measurement’s. A failed Linux iteration can also be hidden by the following successful iteration. Windows leaves `$failed` zero for “NOT MEASURED.”

**Failing scenario:** Ubuntu emits some `measure:` lines and then fails, while Debian and Windows finish. `make go-measure` can exit successfully without Ubuntu’s complete measurements.

**Fix:** propagate producer failures and require the expected completed measurements. The supplied logs contain the stated measurements; this does not invalidate those recorded numbers.

### 10. Minor — README keepalive wording contradicts the implementation

**Area:** documentation  
**File:** `README.md:194,205–210`.

The network paragraph says there is “no keepalive”; the following paragraph describes the implemented periodic RTP silence.

**Failing scenario:** someone diagnosing idle network traffic reads the former as saying the softphone sends no maintenance packets, although an early or held media session does.

**Fix:** say there is no **SIP registration/NAT keepalive**, separately describing the quiet-media RTP frame. Describe preventing VPN expiry as the intention supported by one installation’s observation, not a general guarantee.

## Checked and found supported

- **Ownership and cleanup:** call-state mutations remain actor-owned; the audio loops use atomics for privacy and counters. The `offer`/`take` handoff prevents both sides owning unopened-for-playback devices. I found no demonstrated double-close or permanent device leak in the ordinary early cancellation, refusal, 183-to-failure and Shutdown paths. Vendored dialog callbacks close media too, and media closure is idempotent.
- **Microphone privacy:** `onEarly` and hold return no microphone frame; keepalive encoding uses a separate zero-filled frame. Clearing privacy forces queued capture to be discarded. I found no demonstrated microphone leak before answer, while held or muted.
- **SRTP:** the early handoff checks the 183’s body, and the final answer is checked before the actor enables microphone transmission. Plain early media is withheld for SRTP-required accounts by source inspection. This is **not runtime proof of SRTP early media**, which the receipt explicitly leaves open.
- **Tones and devices:** playback-only streams do not open capture. The saved ring scenarios support ordinary answer/cancel/reject/DND behavior and absence of generated tones from digital call audio. `chosen.devices` releases the settings lock before device opening; the audio test uses that selection. Level arithmetic avoids the `int16(-32768)` absolute-value overflow.
- **Storage and CSV:** the database-failure tests support continued phone/`run` operation with unavailable storage; service ordering refuses without a ledger before `Begin`. CSV preview stores nothing. Confirmation retains the exact text. `WithoutIssues` retains only already-accepted contacts; import revalidates and enforces the merged-number limit transactionally. Size limits exist in both page and backend. Names and refusal reasons are interpolated as text, not HTML.
- **Notifications and denial mapping:** Linux replacement, escaped caller text, button removal and normal urgency are supported by the bus test and saved run. The Windows log supports a ringing notification, subsequent removal, and one buttonless missed toast. The documented application-wide Windows clearing limitation remains explicit. HRESULT comparison correctly truncates to 32 bits and unwraps the OLE error.
- **SBOM:** the saved document contains 53 Go modules, 24 npm packages and four font entries. The generator uses the notices’ component stream and checks names in both directions; the saved manifest lists six checksum entries. Schema validation and per-artifact SBOMs are explicitly unclaimed.
- **Documents and runs:** the 77-scenario Asterisk, 10-scenario FreePBX and eight-scenario impaired reports support their named results. Measurement logs match the receipt, including the weaker Windows window milestone and locally built Rust comparison. The Windows audio failure and deferred manual work are disclosed rather than presented as passes. The parity receipt does not claim gate sign-off.
- **Owner’s desktop calls:** the receipt explicitly identifies them as unrecorded, non-lab observations. It does not present them as independently recorded runs. The gateway explanation remains an inference; I could not verify those calls or their cause. The early-media test source is not a substitute for seeing that run.
- **Sensitive data:** I found no apparent real SIP account, customer server, credential, owner telephone number or personal home path in the reviewed extract and diff. Lab addresses, fictional fixtures and public project/upstream identities are not treated as leaks.

**Not checked:** no builds, tests, scripts, applications or VMs were run; omitted pictures, binaries, actual artifact hashes and the complete Rust baseline specs were not independently validated.  
Real-hardware/manual gates and unrecorded desktop calls remain runtime-evidence gaps. This review does not prove that a call works.