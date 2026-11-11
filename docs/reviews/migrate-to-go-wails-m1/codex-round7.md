# Codex review of milestone M1, round 7 (2026-11-11)

Reviewed: commit `1d2ff41`, the same kind of sanitized extract, with the reports of rounds 1 to 6 and the responses to them. Command: `codex exec --sandbox read-only --skip-git-repo-check --ephemeral`. The report below is unedited.

## Earlier findings

- **Round 6, problem 1 — PARTIALLY RESOLVED.** `MediaIO.Discard`, `buffers.Discard` and capture re-priming address the original queued-audio replay **when the sender observes the hold or mute**. `TestNothingRecordedOnHoldIsEverSent` covers backlog growth, partial frames and subsequent audio return; `TestDiscardDropsEverythingCaptured` checks the adapter’s ring flush. However, remembering the privacy interval still depends on a sampled boolean, leaving the ordering below uncovered.

## New problems

1. **MAJOR — a privacy interval entirely between sender ticks is forgotten.**

   **Files/functions:** `internal/core/engine/call.go:106–116,192–211`, `startAudio` and `microphone.next`; `internal/core/engine/actor.go:657–718`, `hold` and `mute`.

   **Scenario:** During one established call, the sender is between calls to `mic.next`—for example, occupied with queued DTMF, whose branch continues without sampling microphone state. The actor processes mute-on, the microphone queues private samples, and the actor processes mute-off before the next microphone tick.

   The next tick sees `held=false, muted=false`. Because no tick observed mute-on, `m.private` remains false: `Discard` is skipped and queued private samples can be returned by `Capture`, encoded and transmitted. Even a short mute interval between ordinary ticks can encounter this ordering. A hold/resume cycle missed by the sender has the same structural gap.

   **Suggested fix:** Record privacy transitions independently of sender sampling—for example, an actor-updated atomic generation that tells the capture-owning goroutine a flush is required, even when the current flags are already false. Keep `Capture` and `Discard` on that goroutine. Add a deterministic regression that queues private samples and completes mute/unmute without an intervening microphone tick, then verifies no private samples escape.

   **Must fix before M1 closure:** this remains microphone-audio disclosure, not an acceptable limitation.

No additional concrete closure blocker found in the other requested areas.

## Receipt assessment

The receipt appropriately qualifies simulated importer interruptions, untested power loss, clean store reopening, SQLite page-limit exhaustion, virtual devices and closure checked at event-channel closure. The test counts—19 configuration, 24 store and 14 importer tests—match.

However, saying post-hold/mute replay “is covered” needs qualification: the new tests establish flushing for **observed** transitions, not the missed-transition ordering above. They also test the helper and adapter separately rather than the concurrent sender/actor interaction.

Not checked: builds, test execution, network, hardware, omitted dependency behavior or unavailable execution logs. The privacy defect remains a closure blocker.

Verdict on M1: UNSOUND