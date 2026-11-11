## Earlier findings

- **Round 9’s hold/mute privacy problem — PARTIALLY RESOLVED overall.** The reported half-written-transition race is **resolved**: `call.private` changes the flags and generation together through one atomic compare-and-swap. Neither `hold` nor `mute` exposes an intermediate privacy word.

  In `microphone.next`, the actor/sender interleavings now divide correctly:
  - A sampled hold suppresses microphone output; a sampled mute substitutes silence.
  - A completed transition between ticks changes the word and requires `Discard`.
  - A transition overlapping capture changes the second reading, rejecting that frame.
  - A transition overlapping `Discard` requires another discard on a subsequent tick.
  - A transition **after successful post-capture validation** can still allow one already-captured frame through: **160 samples, or 20 ms of pre-transition sound**. This bounds the audio content, not how soon the scheduled write occurs.

  These conclusions assume that `Discard` excludes subsequently delivered *old recordings*. The real adapter only approximates that contract with a 100 ms timer. Private recordings can therefore still escape, as detailed in problem 1.

  `TestPrivacyWordChangesAtOnce` checks successive word updates; `TestPrivacyBetweenTwoTicksIsNotMissed` covers the specified capture overlaps; the racing test exercises three concurrent parties. They support the atomic-word fix, but their microphones do not model delivery delayed beyond the adapter’s discard window.

- **Earlier jitter-buffer loss-deadline finding — PARTIALLY RESOLVED.** Counting the contiguous prefix fixes the previous packet-4 example. A sufficiently long prefix still advances a later missing packet’s deadline; see problem 2.

- **Previously recorded media-failure classification limitation — RESOLVED within that limitation.** The 500 ms grace remains a heuristic, accurately disclosed. Later dialog termination can still leave the call classified as failed; that can remain an M1 limitation.

## New problems

1. **MAJOR — delayed delivery can replay private microphone audio after unmute or resume.**

   **Files/functions:** `internal/platform/audio/audio.go:126–137,172–178`, `buffers.captured`/`Discard`; `internal/core/engine/call.go:202–230`, `microphone.next`.

   **Concrete scenario:**
   1. During mute, a capture callback receives a private 160-sample fragment, but is descheduled before entering `captured`.
   2. The actor unmutes. The sender observes the new word, discards its ring and starts the 100 ms rejection window.
   3. The callback resumes **120 ms after that discard**. `captured` checks delivery time—not recording time—and accepts the private fragment.
   4. Another fresh fragment supplies the 320 samples needed for priming. `Capture` returns the private first frame.
   5. Both privacy readings match the already-handled, unmuted word. `startAudio` encodes and sends the private samples.

   This needs neither multiple calls nor unusually slow physical hardware: delayed scheduling of the capture callback suffices. More delayed fragments can disclose more private audio; the exposure is **not limited to one pre-transition frame**.

   **Suggested fix:** Replace the assumed delay with a backend-confirmed capture flush/reset boundary, or acquisition timestamps/generations that reliably exclude old recordings. Add a deterministic regression delivering private samples **after** `captureDelay`; the present adapter test delivers them immediately after discard, inside the rejection window.

   **Must fix before closure.** Documenting an unmeasured timing assumption does not make microphone disclosure an acceptable limitation.

2. **MAJOR — catch-up still advances a known later gap’s loss deadline.**

   **File/function:** `third_party/diago/media/rtp_jitter_buffer.go:441–460`, `resetPlayoutTimer`.

   **Concrete scenario:** With 20 ms packets, delay 3 and maximum 10, start playout with packets `1,2,3`. Before packet 2’s release, queue `4,5,6,7,9,10`; packet 8 is delayed.

   Relative to packet 1’s release:
   - Packet 2 leaves around **20 ms**.
   - The contiguous prefix `3…7` has length 5, so `ready > delayPackets+1` resets the next deadline to “now.”
   - Packet 3 also leaves around **20 ms**, advancing subsequent decisions by 20 ms.
   - Missing packet 8 is declared lost around **120 ms**, rather than its original **140 ms** deadline.
   - Packet 8 arriving around **130 ms** is consequently discarded as late.

   This is the earlier failure mechanism with the gap farther along. The existing regression places the gap at packet 4, where the prefix never triggers this acceleration.

   **Suggested fix:** Preserve the loss deadline across a known buffered gap—for example, inhibit catch-up when buffered packets extend beyond the contiguous prefix, or maintain a separate loss timetable. Add the farther-gap regression above.

   **Must fix before closure:** the claimed deadline-preservation fix remains incomplete and can introduce avoidable repeated audio loss.

No additional concrete defects found in `app`, `rustimport`, `store` or `config` under the stated design.

## Receipt assessment

The receipt largely matches the inspected assertions: component-only privacy testing, virtual devices, simulated importer interruptions, untested power loss, clean database reopening, SQLite page-limit exhaustion, and device closure checked at event-channel closure. The configuration/store/importer test counts match **19/24/14**.

However, its unconditional privacy assurance exceeds the evidence. The 100 ms assumption is disclosed, but the tests do not establish that older private recordings cannot arrive afterward. The jitter regression likewise establishes the packet-4 case, not general preservation of later loss deadlines.

Not checked: builds, test execution, network, physical hardware, omitted dependencies or unavailable execution logs. Private microphone disclosure remains a closure blocker.

Verdict on M1: UNSOUND