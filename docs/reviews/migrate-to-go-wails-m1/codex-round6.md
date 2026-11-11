# Codex review of milestone M1, round 6 (2026-11-11)

Reviewed: commit `c2fe87a`, the same kind of sanitized extract, with the reports of rounds 1 to 5 and the responses to them. Command: `codex exec --sandbox read-only --skip-git-repo-check --ephemeral`. The report below is unedited.

## Earlier findings

Paths are relative to `src/`. Reviewed statically under the stated single-account, single-call, single-process design.

- **Round 5, problem 1 — RESOLVED.** `domain/state.go`, `CallSnapshot.Answered`, and `engine/actor.go`, `newCall`/`move`/`end`, preserve lifecycle timestamps through the final snapshot. `app/run.go`, `Run` and `journal.record`, now determine answer occurrence and duration from those timestamps, not from receiving Established. The snapshot-coalescing and history regressions cover the two components separately; they do not reproduce the complete delayed-consumer scenario through `Run`.

- **Round 5, problem 2 — RESOLVED.** `rustimport/rustimport.go:319–329`, `place`, falls back to copying into a private, synced file beside the destination and renaming locally. Recovery removes an interrupted destination-side copy. `TestConfigurationOnAnotherFileSystem` simulates rename failure rather than using an actual second filesystem.

## New problems

1. **MAJOR — microphone audio captured during hold can be transmitted after resume.**

   **File/function:** `internal/core/engine/call.go:116–134`, `startAudio`; supporting implementation: `internal/platform/audio/audio.go:76–84,115–149`, `ring.write`, `captured` and `Capture`.

   **Scenario:** During an established call, the user holds for two seconds, speaks privately during the first half-second, then becomes silent before resuming. The held send loop sleeps without calling `Capture`, but the audio adapter continues recording. Its 4,096-sample ring fills and rejects newer samples, retaining audio from the beginning of the hold.

   On resume, `Capture` trims that backlog **to 320 samples**, rather than discarding it completely. Those approximately 40 ms of held microphone audio are then encoded and sent to the peer. This requires only one normal call and a functioning audio service.

   **Suggested fix:** Drain and discard capture while held, and flush remaining held samples before permitting transmission on resume, with capture ownership remaining in the send goroutine. Add a buffered-capture regression using a distinctive signal produced only during hold, and assert that it is never transmitted after resume.

   **Must fix before M1 closure:** this discloses audio captured while the user expected privacy; it is not an acceptable limitation.

## Receipt assessment

The revised qualifications largely match the inspected assertions: simulated importer interruptions rather than actual process termination, no power-loss proof, clean store reopening, SQLite page-limit exhaustion, virtual sound devices, and device closure checked at event-channel closure. The counts of 19 configuration, 24 store and 14 importer tests match the sources.

The round-5 regression descriptions are appropriately bounded. However, the hold tests use `memory.Stream.Capture`, which generates samples on demand and has no independently filling capture queue. They establish the stated tone checks during hold and audio return afterward, but cannot detect the held-audio replay above. The receipt should explicitly record that gap until it is tested. Claimed execution results and numerical measurements remain unverified here.

Not checked: builds, test execution, network, hardware, omitted dependencies or unavailable execution logs. The hold-audio disclosure remains a closure blocker.
Verdict on M1: UNSOUND