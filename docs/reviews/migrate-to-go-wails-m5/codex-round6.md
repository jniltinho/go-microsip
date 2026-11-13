SOUND — section 8 (tasks 8.1–8.9) and its stated evidence: the pending-open shutdown/event-pump blocker is removed; no new blocker found in this response-only review.
SOUND WITH CHANGES — call-path changes: the broken recovery is removed, but SDP set comparison introduces one minor false-equality case.

## Status of round 5’s findings

- **1 — resolved by inspection.** `StartAudioTest`/`StopAudioTest` have the necessary ordering; `TestAStalledSoundServiceDoesNotHoldTheStopOrTheClose` requires stop and shutdown to finish before releasing `OpenGate`, then checks late device closure.
- **2 — resolved by inspection.** `resound` is removed; retained `earlyFailed` causes termination without clearing `onEarly`. The README correctly acknowledges that no test exercises this termination branch.
- **3 — partially resolved.** The early-media fixture now covers reordered independent attributes and repeated `sendrecv`; blanket set normalization also conceals order-sensitive selection, described below.
- **4 — resolved by inspection.** The departed Settings page’s successful start continuation now returns without polling or issuing another global stop. No component test establishes this.
- **5 — resolved by inspection.** `measure.ps1:75–85` rejects a missing Go page milestone before accumulating samples; `build/go.mk:141–143` makes its `NOT MEASURED` output fail the target. No Windows rerun is supplied.

## New finding

### 1. Minor — SDP equality can conceal a different selected SDES key

**Area:** early-media SDP comparison.  
**Files/functions:** `third_party/diago/dialog_media.go:1015–1036`, `sameMedia`, and `:252–270`, `checkEarlyMedia`; `third_party/diago/media/media_session.go:568–620`, `RemoteSDP`.

`sameMedia` treats every section’s attributes as an unordered set, but the consuming parser does not: `RemoteSDP` selects the **first supported crypto attribute**.

**Concrete scenario:** a PBX’s 183 contains two supported AES-suite crypto lines with distinct valid keys, A followed by B. Its 200 contains exactly those lines reversed and starts encrypting with B. These multiple-key answers are noncanonical, but this code accepts them: neither `offersSRTP` nor `RemoteSDP` rejects the ambiguity.

The 183 selects A. The new comparison declares the 200 equal, skipping the update that would select B. Subsequent B-encrypted packets fail authentication and are dropped, leaving the answered call without received audio.

This is a minor compatibility/validation defect involving ambiguous SDP—not a demonstrated plaintext downgrade or an ordinary single-key answer.

**Fix:** reject ambiguous crypto selections before taking the equality shortcut, or preserve the parser’s first-supported-key selection when comparing. Add a focused reversed-key test. The same conservative treatment should cover conflicting direction attributes, whose parser uses the last occurrence.

## Checked and found supported

### Audio-test lifecycle

- `opened.Store(true)` precedes the stop-channel check. If stop returns after observing `opened == false`, it has already closed `stop`, so the subsequently opened devices take the close-without-playback path.
- If start passes that check before cancellation, a subsequent stop observes `opened == true` and joins `done`.
- Taking `p.test` under `p.mu` gives only one stopper ownership of closing `stop`. Error, cancelled-opening and running paths close `done` through mutually exclusive paths.
- Identity checks prevent an old start’s failure or running cleanup from unregistering its replacement.
- Device opening and the `done` wait occur outside `p.mu`; `Close` sets `closed` before stopping. A pending opening no longer holds event forwarding or prevents submission of engine shutdown.
- The delayed-opening test checks zero played frames; the gated-opening test checks stop/shutdown progress and eventual closure. Neither establishes real-driver timing.

### Failed early media and silence writes

- Actor serialization prevents the newly installed watcher from applying its callback between `move` and `terminate`. Competing watcher/hang-up completions converge on `end`, whose final-state guard prevents duplicate finalization.
- The `earlyFailed` branch never clears `onEarly`, so its surviving sender cannot send captured microphone audio while hang-up is pending.
- Final cleanup cancels the pumps, closes media to interrupt reception, joins the pumps and closes their devices. The removed recovery no longer waits for a receiver before initiating termination.
- Incoming ringing uses a tone, not the early-media pumps; it cannot acquire `earlyFailed` through that path.
- Ignoring the quiet keepalive’s write error does not create a busy loop: each quiet iteration sleeps 20 ms. Ordinary audio—including muted silence—and DTMF write errors still report failure. No new plaintext fallback was introduced.

### SDP sections, page and measurement

- Ordinary changes to connection addresses, media ports, payload lists, mappings, direction or keys remain unequal.
- Session-level and media-level lines are kept separate; moving a line between sections changes the comparison. Media-section order and exact `m=` contents remain significant. Empty sections are safe to compare.
- The departed page no longer stops its replacement’s test.
- A missing page milestone on either the first or a later Go start now prevents successful measurement output for that application.

## README claim strength

- **Round 5 response 3 is too strong:** a changed *selected* key need not change the attribute set, as finding 1 demonstrates.
- **Response 1’s “ends within moments” needs qualification:** an opened test still waits without a deadline for backend `Close`; the named fake’s immediate closure does not establish that timing for real drivers.
- The disclosed missing early-failure test, missing component test and absent Windows rerun are accurate limitations. The `go-test` recipe includes `-race`; inspecting it does not establish that the revised tests passed.

**Not checked:** no builds, tests, scripts, applications or VMs were run; omitted artifacts and runtime evidence were not independently verified.  
This review is not evidence that a call works.