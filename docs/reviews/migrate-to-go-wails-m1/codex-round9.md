## Earlier findings

- **Round 8, problem 1 — PARTIALLY RESOLVED.** The post-capture generation check rejects the completed mute/unmute or hold/resume cycle reported in round 8. The extended `TestPrivacyBetweenTwoTicksIsNotMissed` exercises that ordering. However, an unfinished `call.private` operation can still disclose private microphone samples; see problem 1 below.

  The remaining interleavings divide as follows:
  - Observed privacy flags suppress microphone output.
  - A different generation triggers flushing or rejects the captured frame.
  - A transition starting after successful post-capture validation can still allow the already-captured, **pre-transition** frame to leave.
  - An **unchanged odd generation** can incorrectly authorize audio captured during privacy.

- **Round 8’s media-failure assessment — RESOLVED within the recorded limitation.** The 500 ms grace remains a heuristic: later dialog termination can still produce `EndFailed`. The receipt now states this limitation and the missing independent-direction regression. It need not independently block closure.

## New problems

1. **MAJOR — an in-progress privacy transition can be mistaken for an already-handled generation.**

   **Files/functions:** `internal/core/engine/call.go:199–243`, `microphone.next`, `call.privacyState`, `call.private`; `call.go:119–134`, `startAudio`; `internal/core/engine/actor.go:675–737`, `hold` and `mute`.

   **Concrete scenario**, initially with both flags false and both generation counters zero:

   1. The actor begins mute-on. Its first `privacy.Add(1)` makes the generation **1**, then it is descheduled before `flag.Store(true)`.
   2. The sender ticks. It reads false flags and generation 1, records `m.changes = 1`, discards capture, and returns silence.
   3. On another tick, the sender again samples false flags and generation 1. The counters match, so it proceeds toward `Capture`.
   4. The actor executes `flag.Store(true)`, then is descheduled before its second increment. While mute is actually true, the device queues private speech.
   5. The sender captures that speech. Its second `privacyState` sees mute true but still generation **1**. `next` ignores the returned flags and accepts the unchanged generation.
   6. `startAudio` encodes and writes the private frame without another check.

   The same ordering applies to hold. This can transmit an entire **160-sample, 20 ms frame recorded during mute or hold**, not merely a pre-transition frame already in flight. Later suppression and flushing cannot retract it. Repeated transitions can repeat the disclosure.

   **Suggested fix:** Treat odd generations as changes still in progress: suppress output without acknowledging them as processed. Only authorize microphone audio against a stable even generation, retaining post-capture validation and the required subsequent flush. Add deterministic regressions that pause `private` between its atomic operations for both flags; the current test always completes each `private` call synchronously.

   **Must fix before M1 closure:** this remains microphone-audio disclosure, not an acceptable limitation.

No additional concrete closure blocker found in the other requested packages or the marked jitter-buffer changes.

## Receipt assessment

The receipt accurately qualifies component-only testing, simulated importer interruptions, untested power loss, clean store reopening, SQLite page-limit exhaustion, virtual devices, and device checks at event-channel closure. The configuration/store/importer test counts match: **19/24/14**.

Its privacy assurance remains too strong. The extended regression covers completed transitions inside capture, not interruption inside `private`. Neither it nor the lab establishes universal privacy safety or literally “nothing while held by construction.” Even apart from problem 1, an already-validated pre-transition frame can leave after hold starts. The receipt should distinguish those cases and retain the unresolved disclosure explicitly.

Not checked: builds, test execution, network, hardware, omitted dependencies or unavailable execution logs. The privacy disclosure blocks closure.

Verdict on M1: UNSOUND