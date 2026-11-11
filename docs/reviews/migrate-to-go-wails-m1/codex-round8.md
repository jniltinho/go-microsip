## Earlier findings

- **Round 7, problem 1 — PARTIALLY RESOLVED.** `call.private` brackets flag changes with generation increments, and `hold`/`mute` use it consistently. `microphone.next` now flushes a privacy interval completed before the sender samples that generation. `TestPrivacyBetweenTwoTicksIsNotMissed` covers that ordering, but not a transition between sampling and capture; see problem 1 below.

- **README’s media-failure change — RESOLVED for the reported ordering.** `actor.go:571–595`, `mediaGrace`/`mediaFailed`, gives dialog termination 500 ms to explain an audio failure, otherwise posting termination with another phase check. This addresses audio stopping shortly before the dialog ends without abandoning independent media-failure handling. The deadline is a heuristic: later dialog termination can still produce `EndFailed`. That can be recorded as a limitation; there is no targeted regression test.

## New problems

1. **MAJOR — privacy changes after sampling can still disclose microphone audio.**

   **Files/functions:** `internal/core/engine/call.go:119–134,198–219`, `startAudio` and `microphone.next`; `actor.go:723–735`, `mute`.

   **Concrete scenario:**
   1. During an established call, the sender reads `held=false`, `muted=false`, `privacy=0`; its microphone helper has already processed generation 0.
   2. The sender is descheduled before `Capture`.
   3. The actor processes mute-on, the microphone queues private speech, and the actor processes mute-off. The generation is now 4.
   4. The sender resumes using its previously sampled arguments. `next(false, false, 0)` sees neither privacy nor a generation change, skips `Discard`, and captures the queued private samples.
   5. `startAudio` encodes and transmits them without rechecking the generation.

   This is speech recorded **during mute**, not merely a pre-mute frame already in flight. Ordinary scheduling delay with independently running capture suffices. The next tick’s flush is too late.

   **Suggested fix:** Revalidate the generation after capture, before permitting that frame to be sent. Suppress the frame and preserve the required flush when a transition overlapped capture. Add a deterministic regression that pauses between state sampling and capture, completes mute/unmute while queuing distinctive private samples, then resumes.

   **Must fix before M1 closure:** microphone-audio disclosure is not an acceptable limitation.

No additional concrete closure blocker found in the other requested areas or the marked jitter-buffer changes.

## Receipt assessment

The receipt largely matches the inspected assertions: component-only privacy tests, simulated importer interruptions, clean store reopening, SQLite page-limit exhaustion, virtual audio, and device closure checked at event-channel closure. The counts—19 configuration, 24 store and 14 importer tests—match.

The privacy coverage remains incomplete: the new regression performs both transitions before sampling, so it cannot establish safety for the interleaving above. The component-only qualification is accurate, but should explicitly retain this unresolved gap. The media-grace repetitions do not replace an independent media-failure test.

Not checked: builds, test execution, network, physical hardware, omitted dependencies or unavailable execution logs. The remaining privacy disclosure blocks closure.

Verdict on M1: UNSOUND