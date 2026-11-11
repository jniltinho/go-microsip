# Codex review of milestone M1, round 5 (2026-11-11)

Reviewed: commit `cf8e470`, the same kind of sanitized extract, with the reports of rounds 1 to 4 and the responses to them. Command: `codex exec --sandbox read-only --skip-git-repo-check --ephemeral`. The report below is unedited.

## Earlier findings

Static review; paths are relative to `src/`. Assumed one account, one active call and one process per database.

- **Round 4, problem 1 — RESOLVED.** `internal/core/engine/actor.go:356–374`, `end`, now counts every ended call’s cleanup and waits for both `released` and any `settled` completion. `shutdown:802–847` waits for that aggregate within its existing deadline. A later call no longer hides earlier cleanup. `TestShutdownWaitsForTheDevicesOfEveryCall` exercises the gated first closure followed by an immediately ended second call. It tests `cleanup.Wait` directly, not an actual Shutdown command or `Stopped`.

- **Round 2, N5 — RESOLVED.** Device disposal remains outside actor finalization, and the aggregate cleanup count includes previous calls and outstanding opens. The reported callback-blocking and forgotten-completion paths are addressed.

- **Round 1, finding 2 — RESOLVED within the recorded limitation.** Opening remains off the actor; local cancellation precedes bounded network waits; shutdown now waits for every call’s tracked cleanup. This is not an unconditional device-closure guarantee: the documented exception for an unresponsive sound service remains.

## New problems

1. **MAJOR — merging intermediate states can erase the evidence that a call was answered.**

   **Files/functions:** `internal/core/engine/engine.go:173–181`, `outbox.emit`; `internal/core/app/run.go:169–183,253–291`, `Run` and `journal.record`.

   **Scenario:** `Run` is delayed writing the initial history row, while the event deliverer waits to send Ringing. The call becomes Established, then an RTP pump fails and `mediaFailed` invokes `terminate`. The pending Established snapshot is replaced by Terminating before the application reads it; the final Failed snapshot is retained.

   Both application consumers infer “ever answered” solely from receiving Established or Held. Consequently, history records this answered call as failed with no answer time or duration, and `Run` returns `ErrCall` saying it was not answered. Retaining the final snapshot does not recover the missing information.

   **Suggested fix:** Preserve answer occurrence and lifecycle timestamps in subsequent snapshots, including the final one, and use them in journaling and `Run`. Add a regression that delays consumption across Established → Terminating → Ended and checks the stored outcome and duration.

   **Must fix before M1 closure:** this corrupts call history within the supported design.

2. **MINOR — importing a configuration across filesystems always fails.**

   **File/function:** `internal/core/rustimport/rustimport.go:692–698`, `apply`.

   **Scenario:** `XDG_DATA_HOME` and `XDG_CONFIG_HOME` reside on different filesystems. The staged configuration lives under the data directory; renaming it into the configuration directory fails with `EXDEV`. Recovery restores the previous pair, but the import cannot succeed.

   **Suggested fix:** Stage the configuration on its destination filesystem and include that staging file in recovery, or explicitly document the same-filesystem requirement.

   **Can be recorded as an M1 limitation:** this is a safe refusal, not demonstrated data loss.

**Receipt assessment:** The corrected 19 configuration, 24 store and 13 importer test counts match the sources. The qualifications concerning simulated importer interruption, clean store reopening, SQLite page-limit exhaustion, virtual audio and closure checked at event-channel closure are appropriate. The sequential-cleanup regression does not itself establish end-to-end shutdown timing, and the existing application scenarios do not cover problem 1. Also, “the voice is gone” is stronger than the binary assertion, which permits up to ten detected voice pieces during its two-second post-unplug measurement.

Not checked: builds, test execution, network, hardware, omitted dependencies or unavailable execution logs. Problem 1 remains a closure blocker.

Verdict on M1: UNSOUND