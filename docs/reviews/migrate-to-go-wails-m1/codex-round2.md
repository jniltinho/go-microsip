# Codex review of milestone M1, round 2 (2026-11-11)

Reviewed: commit `2eedb96`, the same kind of sanitized extract as round 1, with the round-1 report and the responses to it. Command: `codex exec --sandbox read-only --skip-git-repo-check --ephemeral`. The report below is unedited.

## Round-1 findings

Paths below are relative to `src/`. This is a static review of the single-account, single-call design.

1. **PARTIALLY RESOLVED — resource-owning results.**  
   `internal/core/engine/engine.go`, `post`/`take`, and `actor.go`, `run`: normal shutdown now drains queued results and serializes late callbacks; stale endpoint, dialog and audio results have cleanup paths. However, a callback panic still abandons the remainder of an already-taken batch, and the actor-panic path does not invalidate late account results.

2. **PARTIALLY RESOLVED — bounded shutdown.**  
   `internal/core/engine/actor.go`, `established`/`shutdown`, and `call.go`, `stopAudio`: opening audio is off the actor, rejection is bounded, and cleanup starts before network waits. Nevertheless, timing out the cleanup wait still permits `Stopped` with devices open, outstanding opens are not included in that wait, and queued audio disposal can block finalization outside the shutdown budget.

3. **RESOLVED — Answer racing Shutdown.**  
   `internal/core/engine/actor.go`, `answer`/`reject`/`hangup`/`shutdown`: `answering` now gives Answer ownership of the final response, so shutdown does not also send 486. A successful answer arriving after the final snapshot is disposed through `closeDialog`; the omitted library’s Answer-versus-Close behavior remains unverified.

4. **RESOLVED — media workers dying silently.**  
   `internal/core/engine/call.go`, `startAudio`, and `actor.go`, `mediaFailed`/`terminate`: terminal read, write and DTMF-write errors notify the actor once and terminate the call. The eventual `end` cancels the companion pump and releases audio, although these independent-direction failures still lack a targeted test.

5. **RESOLVED — transfer completion loss.**  
   `internal/core/engine/actor.go`, `transfer`/`watch`: terminal status is stored atomically, separately from progress, so progress cannot crowd it out. `watch` defers finalization while transfer completion is pending, and the completion callback handles either ordering of successful NOTIFY and dialog termination.

6. **PARTIALLY RESOLVED — premature jitter loss decisions.**  
   `third_party/diago/media/rtp_jitter_buffer.go`, `ReadRTP`/`resetPlayoutTimer`: the immediate missing-packet case is fixed, but accelerating an available packet still shifts the deadline of a following missing packet forward. With the new test’s sequence `1,2,3,5…10`, packet 4 arriving around 50 ms can still be discarded before its original approximately 60 ms deadline; the test supplies it at 30 ms and therefore misses this case.

7. **PARTIALLY RESOLVED — private storage directories.**  
   `internal/core/store/store.go`, `Open`/`createPrivateFile`: existing directories writable by group or others are now refused. Directory ownership is still unchecked, and Windows protection remains deferred, as the shortcut explicitly acknowledges.

8. **RESOLVED — unlocked recovery within the importer.**  
   `internal/core/rustimport/rustimport.go`, `Recover`/`Apply`/`RecoverLocked`: recovery takes the own-instance lock, and `Apply` retains it across initial recovery, import and failure cleanup without recursively acquiring it. The backup-existence checks also support restarting restoration after a consumed backup; application-level lock identity and recovery bypasses remain separate problems below.

9. **PARTIALLY RESOLVED — power-loss durability.**  
   `internal/core/rustimport/rustimport.go`, `syncDirs`/`apply`/`RecoverLocked`: both existing destination directories are now synced before recording completion or removing recovery instructions. However, newly created directories from `MkdirAll` are not durably linked by syncing their parents, so a fresh configuration directory can still disappear after the durable `done` record.

10. **RESOLVED — orphaned staged credentials.**  
    `internal/core/rustimport/rustimport.go`, `RecoverLocked`: unavailable or failed credential deletion now preserves the reference in a `cleanup` journal and returns `ErrCredentialCleanup`. Subsequent recovery retries deletion without repeating file rollback, and the new tests exercise both missing and unavailable keyrings.

11. **PARTIALLY RESOLVED — single-instance protection.**  
    `internal/core/app/run.go`, `Run`, and `rust.go`, `InstanceLock`: the missing-runtime-directory case now has a fallback, and ordinary lock failures stop execution. But sessions sharing one database can select different lock files depending on their runtime environment, so exclusivity is not established across desktop and headless launches.

12. **PARTIALLY RESOLVED — blocked event consumers.**  
    `internal/core/app/run.go`, `Run`: the context supervisor independently requests engine shutdown and exits when the context or engine ends. The reporting goroutine can nevertheless survive `Run` returning, retaining the journal connection after the instance lock is released.

13. **RESOLVED — interrupting an unanswered call.**  
    `internal/core/app/run.go`, `Run`’s `stop` closure, and `cmd/run.go`, `session.run`: cancellation-generated final snapshots no longer install `ErrCall`, leaving the CLI free to return the recorded signal status. `TestStoppingBeforeTheAnswerIsNotAFailure` directly covers cancellation before an answer, though it does not execute the CLI signal path.

14. **PARTIALLY RESOLVED — receipt accuracy.**  
    `internal/g0/scenarios_engine.go`, `phone.stop`/`engineMicrophoneLost`; `internal/platform/audio/call_linux_test.go`, `callThroughTheBinary`; and `internal/core/rustimport/rustimport_test.go`: the receipt correctly qualifies SQLite page-limit exhaustion, simulated process kills, untested power loss and unmeasured hold traffic, while the binary test now counts microphone-loss notices and rejects detected decoy blocks. It still overstates exact closure at `Stopped`—the lab checks after channel closure—and whole-scenario notice uniqueness—the later `quiet`/`ended` waits discard duplicate notices without failing.

## New problems

I found no data race in the normal `post`/`take` mailbox handoff: the slice and `dead` flag are mutex-protected, and late callbacks are serialized. The problems are callback ownership, terminal-state handling and blocking cleanup, not an unprotected slice.

1. **MAJOR — different sessions lock different files.**  
   **File/function:** `internal/core/app/rust.go:26–34`, `InstanceLock`.  
   A desktop launch with `XDG_RUNTIME_DIR` and a headless launch without it can share the same database while successfully acquiring different locks. The second run can finalize the first run’s live history as interrupted; a Rust import can also replace the database beneath the headless run.  
   **Fix:** Use one canonical own-instance lock tied to the persistent database, independent of runtime-directory availability; test mixed environments.

2. **MAJOR — headless startup can bypass an unfinished import.**  
   **File/functions:** `internal/core/app/rust.go:38–51,92–98`, `rustImporter`/`recoverImportLocked`; `run.go`, `Run`.  
   An import interrupted with `XDG_RUNTIME_DIR` set can leave a new database and an old or missing configuration. A later launch without that variable acquires the fallback lock, but recovery silently returns success because constructing the importer requires the Rust runtime directory; startup then uses the inconsistent files.  
   **Fix:** Separate target recovery from source-import prerequisites: recovery does not need the Rust application's runtime lock. Refuse startup if a pending journal cannot be recovered.

3. **MAJOR — configuration import has a check-then-write lock gap.**  
   **File/functions:** `internal/core/app/app.go:164–171`, `Import`; `rust.go:103–115`, `recoverImport`.  
   `Recover` releases its lock before `Import` opens and writes the database, allowing a Rust import to acquire it and begin replacement in between. Worse, an existing lock holder is ignored whenever `Pending()` happens to be false, including the window before that importer writes its journal.  
   **Fix:** Hold the same exclusive lock across recovery, `ReplaceConfig` and store closure; do not use journal absence as permission to write under another holder.

4. **MAJOR — a stalled reporter outlives the protected database session.**  
   **File/function:** `internal/core/app/run.go:142–168,202–215`, `Run`.  
   If `report` blocks during a call, cancellation stops the engine and `Run` returns after `outputGrace`, releasing the instance lock, but `j.close()` remains deferred in the blocked goroutine. Another run can recover its unfinished history, or an import can replace its still-open database; if reporting resumes, the old goroutine continues journaling without ownership.  
   **Fix:** Keep event draining and journaling independent of output, and finish journal writes and close the store before releasing the lock. Give output an explicit cancellation/lifetime contract.

5. **MAJOR — audio-result cleanup can defeat the shutdown deadline.**  
   **File/functions:** `internal/core/engine/actor.go:509–524`, `established`; `actor.go:76–87`, `run`; `engine.go:114–121`, `post`.  
   If audio opening finishes during shutdown, its queued callback calls `audio.Close()` synchronously while finalization holds `late`. A blocking close prevents `Stopped` indefinitely, outside `shutdown()`’s deadlines; a late callback acquiring `late` first can produce the same blockage.  
   **Fix:** Keep potentially blocking disposal off actor finalization and provide cancellable/force-close audio operations. Track outstanding opens and closures rather than equating a timed-out wait with completed cleanup.

6. **MAJOR — callback panics lose owned results and can revive a stopped account.**  
   **File/functions:** `internal/core/engine/actor.go:61–106,825–838`, `run`/`release`; `engine.go`, `take`/`post`.  
   If one callback panics, the remaining callbacks in the batch returned by `take()` are no longer in `e.results` and are abandoned; the deferred pending-results loop likewise skips its remaining callbacks after a panic. Separately, `release` leaves the generation valid, so a successful connect result arriving after this exit can install an endpoint and start account goroutines after `Stopped`.  
   **Fix:** Invalidate account/call ownership before processing exit results, retain ownership of every unprocessed result, and isolate disposal failures per result. Add tests with multiple resource-owning callbacks, an early panic and a late successful connection.

The receipt is substantially more precise, but its “thirteen fixed” summary and exact shutdown wording are not justified by the current implementation and assertions.

Not checked: builds or test execution, network, hardware, omitted dependency behavior, or the receipt’s unavailable execution logs.

Verdict on M1: UNSOUND