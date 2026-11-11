# Codex review of milestone M1, round 4 (2026-11-11)

Reviewed: commit `8483f9f`, the same kind of sanitized extract, with the reports of rounds 1 to 3 and the responses to them. Command: `codex exec --sandbox read-only --skip-git-repo-check --ephemeral`. The report below is unedited.

## Earlier findings

Paths are relative to `src/`. This review assumes one account, one active call at a time, and one process per database.

- **Round 3, problem 1 — RESOLVED.** `internal/core/engine/call.go`, `offer`/`take`/`stopAudio`, and `actor.go`, `established`: the mutex-protected handoff lets shutdown take and close offered devices without processing their queued callback. `settled` closes after that disposal; `TestOpenedDevicesAreClosedWhoeverComesFirst` covers this ordering.

- **Round 3, problem 2 — RESOLVED.** `internal/core/rustimport/rustimport.go`, `RecoverLocked`/`apply`: recovery uses a local configuration destination derived from the journal, leaving `i.Paths.Config` unchanged for the next import. `TestRecoveryDoesNotRedirectTheNextImport` checks restoration of the earlier destination and import into the current one.

- **Round 3, problem 3 — RESOLVED.** `internal/core/app/run.go`, `Run`, and `internal/core/engine/engine.go`, `Relay`/`outbox.emit`: reporting now retains final snapshots and `Stopped`, merging intermediate states and capping advisory notices instead. `TestRelayKeepsFinalStatesForAReaderThatWasAway` checks delayed consumption across 300 sequential calls.

- **Round 3, problem 4 — RESOLVED.** `internal/core/app/run.go`, `Run`: the timeout warning runs in its own goroutine, so a blocked warning no longer prevents deferred journal closure and lock release. Warning delivery is best-effort; a permanently blocked reporting callback can still outlive `Run`.

- **Round 3, problem 5 — RESOLVED.** `internal/core/app/rust.go`, `exclusive`: when importer construction fails because the locker is nil, the database path is located independently and `Pending()` is checked before permitting startup. The unsupported-lock path retains the equivalent refusal.

- **Round 2, N2 — RESOLVED.** `internal/core/app/rust.go`, `importer`/`exclusive`, and `internal/core/rustimport/rustimport.go`, `RecoverLocked`: destination recovery does not require Rust runtime paths, uses the journal’s configuration destination, and refuses pending recovery without locking capability. `syncDirs` also permits recovery when an unused configuration directory does not exist.

- **Round 2, N5 — PARTIALLY RESOLVED.** `internal/core/engine/call.go`, `offer`/`stopAudio`, disposes of unaccepted devices outside actor finalization, removing the reported blocking callback. However, `actor.end` retains completion channels only for the most recently ended call, allowing earlier cleanup to escape shutdown’s wait; see new problem 1.

- **Round 1, finding 2 — PARTIALLY RESOLVED.** `internal/core/engine/actor.go`, `established`/`shutdown`: opening is off the actor, local cancellation starts before network waits, and shutdown shares a bounded deadline. The recorded unresponsive-service exception is reasonable, but does not cover the responsive-service failure in new problem 1.

- **Round 1, finding 14 — RESOLVED for the outstanding evidentiary overstatement.** `internal/core/store/store_test.go`, `TestKilledMidCallIsRecoveredOnceAsInterrupted`, still explicitly closes the store, and the receipt now accurately describes clean close/reopen rather than an actual kill. `internal/g0/scenarios_engine.go`, `phone.note`/`phone.stop`/`engineMicrophoneLost`, supports the narrowed whole-scenario notice counting and device check at event-channel closure.

**Receipt assessment:** The substantive qualifications now match the inspected assertions: simulated importer interruption, clean store reopening, SQLite page-limit exhaustion, tone thresholds, and device checks at channel closure—not power-loss testing or physical-hardware evidence. Those scenarios do not establish the sequential-call cleanup case below. The test counts are also stale: the supplied sources contain 19 configuration, 24 store, and 13 importer top-level tests, rather than 18, 23, and 12; this is an editorial correction, not a milestone blocker.

## New problems

1. **MAJOR — a subsequent call can hide unfinished device cleanup from shutdown.**

   **Files/functions:** `internal/core/engine/actor.go:354–365`, `end`; `actor.go:793–846`, `shutdown`; `internal/core/engine/call.go:186–219`, `stopAudio`.

   **Concrete scenario:** Call A ends and starts asynchronous device closure, taking a finite 250 ms. Because `end` clears `a.call` immediately, call B can start meanwhile and receive an immediate busy response without opening audio. Ending B overwrites `a.released` and `a.settled` with B’s completion channels. A subsequent shutdown waits only for B and can emit `Stopped` while A’s devices remain open.

   These are sequential calls, not simultaneous calls, and the sound service answers well within five seconds. The microphone-to-network pump is canceled; the defect is reporting completed shutdown before device disposal finishes.

   **Suggested fix:** Retain or combine all outstanding cleanup completions instead of replacing them with the latest pair, and wait for them within the existing shutdown deadline. Add a deterministic regression with A’s closure gated, B ending immediately, and shutdown occurring before A’s closure is released.

   **Must fix before M1 closure:** this is outside the recorded unresponsive-service limitation.

Not checked: builds, test execution, network, hardware, omitted dependencies, or unavailable execution logs.
Verdict on M1: UNSOUND