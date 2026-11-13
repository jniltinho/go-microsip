# Codex review of milestone M1, round 3 (2026-11-11)

Reviewed: commit `e856d17`, the same kind of sanitized extract, with the reports of rounds 1 and 2 and the responses to them. Command: `codex exec --sandbox read-only --skip-git-repo-check --ephemeral`. The report below is unedited.

## Earlier findings

Paths are relative to `src/`. This is a static review of the single-account, single-simultaneous-call design.

### Round 2

- **N1 — RESOLVED.** `internal/core/app/rust.go`, `InstanceLock`: the lock now lives beside the database and does not depend on `XDG_RUNTIME_DIR`. `TestTheInstanceLockBelongsToTheDatabase` checks this identity and refusal under another holder.

- **N2 — PARTIALLY RESOLVED.** `internal/core/app/rust.go`, `importer`/`exclusive`, and `internal/core/rustimport/rustimport.go`, `RecoverLocked`: ordinary headless recovery no longer requires Rust runtime paths, and recovery uses the recorded configuration path. However, `Locks == nil` still bypasses the pending-journal check; see new problem 5.

- **N3 — RESOLVED.** `internal/core/app/app.go`, `Import`, and `rust.go`, `exclusive`: configuration replacement now holds the instance lock across recovery, `ReplaceConfig`, and store closure. Journal absence no longer permits writing under another lock holder.

- **N4 — RESOLVED.** `internal/core/app/run.go`, `Run`: journaling is now performed by the event-draining loop, not the reporting goroutine, and `j.close()` runs before the deferred lock release. A surviving reporter cannot resume database writes after ownership ends; its new queue has a separate defect below.

- **N5 — PARTIALLY RESOLVED.** `internal/core/engine/actor.go`, `established`/`shutdown`: stale audio disposal no longer blocks actor finalization inside `Audio.Close`. However, shutdown can wait for a queued audio callback that it will not process until after that wait expires; see new problem 1.

- **N6 — RESOLVED.** `internal/core/engine/actor.go`, `results`/`release`, and `engine.go`, `apply`: a failing callback returns the unprocessed batch to the mailbox, exit callbacks are isolated individually, and failure invalidates account generation and call state. Late successful connections therefore take the disposal path rather than restarting the account.

### Round 1

- **1 — RESOLVED.** `internal/core/engine/actor.go`, `run`/`results`/`release`, and `engine.go`, `post`/`apply`: queued and late results remain accounted for, including the remainder of a batch interrupted by panic. The previously identified account-revival path is invalidated before exit results run.

- **2 — PARTIALLY RESOLVED.** `internal/core/engine/actor.go`, `established`/`shutdown`, and `call.go`, `stopAudio`: opening, network waits and device closure are off the actor or bounded, and local cancellation starts first. The unresponsive-service exception is now recorded, but the healthy-service handoff failure in new problem 1 is not covered by that exception.

- **6 — RESOLVED.** `third_party/diago/media/rtp_jitter_buffer.go`, `resetPlayoutTimer`: catch-up now counts only the contiguous playable prefix, so packets beyond a gap cannot themselves trigger acceleration. `TestJitterBufferWaitsForALatePacketBehindABurst` now supplies packet 4 at approximately 50 ms, addressing the specific round-2 counterexample.

- **7 — ACCEPTED AS RECORDED LIMITATION.** `internal/core/store/store.go`, `Open`/`createPrivateFile`: group/other-writable destination directories are rejected. Ownership verification and Windows protection remain explicitly deferred; this is not a complete private-directory guarantee.

- **9 — RESOLVED.** `internal/core/rustimport/rustimport.go`, `mkdirDurable`/`syncDirs`/`apply`/`RecoverLocked`: newly created directory links are synced through their parents, and destination directories are synced before completion or removal of recovery instructions. This addresses the identified ordering gap in code, not empirical power-loss verification.

- **11 — RESOLVED.** `internal/core/app/rust.go`, `InstanceLock`/`exclusive`, and `run.go`, `Run`: the supported Linux path uses one database-bound lock and fails on ordinary acquisition errors. Warning-only operation without a locking capability remains a recorded platform limitation.

- **12 — RESOLVED.** `internal/core/app/run.go`, `Run`: context cancellation independently requests engine shutdown, and a blocked reporter no longer performs or retains journal operations. This fixes the microphone-continuing-to-transmit scenario; bounded application return still has the diagnostic-output caveat in new problem 4.

- **14 — PARTIALLY RESOLVED.** `internal/g0/scenarios_engine.go`, `phone.note`/`phone.stop`/`engineMicrophoneLost`, now checks whole-scenario notice uniqueness and closure at event-channel closure, matching the narrowed receipt wording. However, `internal/core/store/store_test.go`, `TestKilledMidCallIsRecoveredOnceAsInterrupted`, explicitly calls `s.Close()`: that establishes recovery of unfinished rows after reopening, not the receipt’s claimed history recovery “after a kill.”

**Receipt assessment:** The revised shutdown measurements, notice counting, tone thresholds, SQLite page-limit qualification, and explicitly simulated importer kills now largely match the inspected assertions. The receipt should narrow the storage-kill wording and include the outstanding audio-handoff case below; the reported executions and numerical results remain unverified in this review.

## New problems

1. **MAJOR — shutdown waits for an audio result that only the blocked actor can settle.**

   **File/functions:** `internal/core/engine/actor.go:519–555,843–849`, `established`/`shutdown`.

   **Scenario:** `Audio.Open` succeeds, observes `c.over == false`, and posts its callback. Shutdown wins the next actor selection before that callback runs. `stopAudio` cannot close the unattached stream, while shutdown waits for `settled`, which only the queued callback can complete. The five-second budget expires; finalization then launches asynchronous disposal and immediately emits `Stopped`, potentially with the stream still open—even though the sound service answered normally.

   **Suggested fix:** Make cancellation settle and dispose of unaccepted opens independently of actor callback processing, or process disposal results during the bounded shutdown wait. Track completion of that disposal before reporting `Stopped`; add a deterministic queued-result-versus-Shutdown test.

   **Must fix before M1 closure.**

2. **MAJOR — recovering an earlier import changes the destination of the next import.**

   **File/functions:** `internal/core/rustimport/rustimport.go:410–412,486–500`, `RecoverLocked`/`Apply`/`apply`; reached through `internal/core/app/rust.go`, `ImportRust`.

   **Scenario:** An import using configuration home A is interrupted. A later `import rust-microsip --apply` uses the same data home but configuration home B. Recovery correctly restores A, but permanently assigns A’s path to `i.Paths.Config`; the subsequent import then backs up and replaces A instead of writing B. B’s configuration remains unchanged while its shared database is replaced.

   **Suggested fix:** Use the journal’s configuration path only within recovery, without changing the importer’s subsequent destination. Add a recovery-then-apply test with different old and current configuration homes.

   **Must fix before M1 closure.**

3. **MAJOR — the new reporting queue drops final snapshots and `Stopped` indiscriminately.**

   **File/function:** `internal/core/app/run.go:159–164`, `Run`.

   **Scenario:** A long-lived headless run receives calls sequentially while its output is stalled. Once 1,024 events have queued, a call’s final snapshot takes the same dropping branch as an advisory event; shutdown can drop `Stopped` too. If output resumes while the application remains running, the missing final state is permanently unavailable through `report`, despite the engine having retained and delivered it correctly.

   **Suggested fix:** Preserve final snapshots and `Stopped` in the reporting layer, merging intermediate states and dropping advisory events instead. Test queue saturation followed by consumer recovery, separately from the documented timeout for permanently blocked output.

   History is still journaled; this is an event-contract failure, not database data loss. **Must fix before M1 closure.**

4. **MINOR — the stalled-output warning can itself prevent `Run` from returning.**

   **File/functions:** `internal/core/app/run.go:198–205`, `Run`; `internal/core/app/app.go`, `Options.warn`; `cmd/root.go`, `session.options`.

   **Scenario:** The CLI runs with stdout and stderr redirected into the same stalled pipe. Reporting blocks, the engine stops, and `outputGrace` expires; `o.warn` then writes synchronously into that same full pipe. `Run` remains blocked before its deferred journal closure and lock release.

   **Suggested fix:** Keep potentially blocking diagnostics outside the protected shutdown lifetime and make warning delivery bounded or best-effort. Alternatively, explicitly record that bounded shutdown covers the engine, not application exit when diagnostic output blocks.

   This can be recorded as a limitation: the call has stopped, although the process and database session remain open.

5. **MINOR — a nil locking adapter skips the unfinished-import safety check.**

   **File/functions:** `internal/core/app/rust.go:39–46,69–72`, `importer`/`exclusive`.

   **Scenario:** A caller supplies the documented `Locks == nil` configuration with a valid database directory containing an interrupted import. `importer` returns `ErrImportUnsupported`; `exclusive` warns and returns success before checking `Pending()`, allowing `Run` or configuration import to use the unsettled files. The non-nil adapter returning `errors.ErrUnsupported` correctly refuses this situation.

   **Suggested fix:** Locate the destination paths and check for an unfinished journal even when no locking adapter exists; refuse startup when recovery cannot run. Extend the unsupported-lock test to cover `nil`.

   The shipped Linux entry point supplies an adapter, so this is not an additional blocker for that binary, but the broader no-lock safety claim needs correction.

Not checked: builds, test execution, network, hardware, omitted dependencies or unavailable receipt logs. Problems 1–3 remain milestone blockers.
Verdict on M1: UNSOUND