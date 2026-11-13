# Codex review of milestone M1, round 1 (2026-11-11)

Reviewed: commit `60cd1a8`, a sanitized extract (`git archive` of `cmd`, `internal/core`, `internal/platform`, `internal/g0`, the QA scripts, the two patched diago files, the change and the M1 receipt; no third-party sources, no fixtures, nothing from `base/`). Command: `codex exec --sandbox read-only --skip-git-repo-check --ephemeral`. The report below is unedited.

## Findings

Paths are relative to `src/`. This is a static review of the single-account, single-call design.

1. **MAJOR — Shutdown can abandon resource-owning results**  
   **Files/functions:** `internal/core/engine/engine.go:90–95`, `actor.go`: `connect`, `dial`, `answer`, `shutdown`.  
   Cleanup of a successfully created endpoint or dialog happens inside a posted callback. Shutdown stops processing callbacks; `post` can discard one or enqueue it into a channel nobody will drain.  
   **Scenario:** Dial completes, but Shutdown wins the actor’s next select before the completion callback. The call still has no attached dialog/media, so shutdown cannot release those returned resources.  
   **Fix:** Keep ownership in the worker until the actor explicitly accepts the result; otherwise close it. Shutdown must also dispose of already-queued results.

2. **MAJOR — The five-second shutdown guarantee is not enforced**  
   **Files/functions:** `internal/core/engine/actor.go:476–494,696–742`: `established`, `shutdown`; `call.go:144–170`: `release`.  
   `Audio.Open` runs synchronously on the actor without cancellation. Shutdown also calls `server.Respond` directly without its deadline. Furthermore, Hangup can consume the entire shutdown budget before audio cleanup starts; the expired wait then permits `Stopped` while cleanup is still running.  
   **Scenario:** An unresponsive audio service prevents the actor from reading Shutdown, or an unanswered BYE consumes five seconds and `Stopped` precedes device closure. The omitted libraries’ exact blocking behavior is **unverified**.  
   **Fix:** Move device opening off the actor, bound response operations, and reserve time for forced local cleanup before reporting `Stopped`.

3. **MAJOR — Shutdown can race an incoming Answer with a second final response**  
   **File/functions:** `internal/core/engine/actor.go:421–449,704–710`: `answer`, `shutdown`.  
   Answer sets `answering=true` but leaves the phase Ringing until its worker reports back. Shutdown ignores `answering` and sends 486 for that phase. Ordering 180 before 200 does not prevent this race.  
   **Scenario:** Shutdown arrives while Answer is sending 200 or waiting for completion. The same INVITE can receive competing 200/486 operations, while the local final snapshot says rejected.  
   **Fix:** Give one operation ownership of the final response. If answering has committed, shutdown must terminate the answered dialog rather than reject it. Whether this also races diago’s internal fields is **unverified**.

4. **MAJOR — Media workers can die without changing the call’s state**  
   **File/function:** `internal/core/engine/call.go:81–139`: `startAudio`.  
   Read/write failures simply return from the affected goroutine. They do not notify the actor, stop the other pump, or release the devices. Most diagnostics are Debug-level, below even the CLI’s verbose setting.  
   **Scenario:** RTP reception returns an error while SIP remains connected. The call stays Established indefinitely but never receives audio again.  
   **Fix:** Report terminal media failures to the actor and explicitly end or mark the call degraded; cancel the companion pump. Test failures independently in each direction.

5. **MAJOR — Transfer completion can be lost**  
   **File/function:** `internal/core/engine/actor.go:647–688`: `transfer`.  
   The bounded notification channel drops every status indiscriminately, including the terminal result. Separately, `watch` can finalize the call before the transfer worker posts success, after which the success callback is ignored.  
   **Scenario:** Progress notifications fill the channel before REFER returns and the final 200 is dropped, producing a false timeout. Alternatively, a PBX sends successful NOTIFY followed immediately by BYE, and the final snapshot remains normal/transfer-pending.  
   **Fix:** Preserve terminal notifications separately from advisory progress and coordinate dialog termination with pending transfer completion.

6. **MAJOR — Jitter catch-up can prematurely declare reordered packets lost**  
   **File/functions:** `third_party/diago/media/rtp_jitter_buffer.go:198–230,441–450`: `ReadRTP`, `resetPlayoutTimer`.  
   Catch-up uses total queued packets, not contiguous playable packets. It therefore accelerates loss decisions as well as successful releases.  
   **Scenario:** After releasing packet 100, packets 102–107 are queued but 101 is slightly delayed. Catch-up immediately declares 101 lost, although it could arrive before its original next playout deadline. Repeated bursts can create avoidable audio loss.  
   **Fix:** Do not advance missing-packet deadlines merely because later packets are buffered. Base catch-up on contiguous backlog and measured lateness; add timed burst/reordering tests.

7. **MAJOR — Existing storage directories invalidate the private-directory assumption**  
   **File/functions:** `internal/core/store/store.go:290–299,463–488`: `Open`, `createPrivateFile`.  
   Existing directories are neither checked nor tightened. The symlink check/open shortcut explicitly relies on a private directory, but `Open` does not establish that condition.  
   **Scenario:** A restored or pre-created application directory is writable by another user/group. That principal can replace database entries or race the check/open despite the database file being 0600. A merely readable 0755 directory is not itself this vulnerability.  
   **Fix:** Validate ownership and reject unsafe writable directories before opening the database, or use a platform-backed safe-open operation.

8. **CRITICAL — Recovery runs without exclusive ownership and can delete previous data**  
   **Files/functions:** `internal/core/rustimport/rustimport.go:339–407,410–420`: `Recover`, `Apply`, `apply`; `internal/core/app/rust.go:65–94`.  
   Recovery runs before acquiring the instance lock; failure recovery runs after `apply` has released it. Even an import preview calls recovery without that lock.  
   **Scenario:** Two recovery processes both observe the database backup. One restores it; the other then removes that restored database and fails to rename the now-consumed backup. The previous database has been deleted. Recovery can also interfere with a live import’s staging files and credential.  
   **Fix:** Hold the same exclusive lock across recovery, import and failure cleanup. Use an internal already-locked recovery helper to avoid recursive locking.

9. **MAJOR — The import commit is not durable across power loss**  
   **File/functions:** `internal/core/rustimport/rustimport.go:293–314,352–387,556–620`: `writeJournal`, `Recover`, `apply`.  
   Journal writes sync the data directory, but configuration renames occur in a separate directory that is never synced. Recovery likewise removes its journal without first durably recording configuration restoration.  
   **Scenario:** The “done” journal becomes durable, power fails, and the configuration-directory changes do not survive. Recovery accepts the imported database while an old or missing configuration remains.  
   **Fix:** Sync every affected directory before recording completion or removing the recovery journal. Test power-loss ordering separately from process termination.

10. **MAJOR — Recovery can permanently orphan a staged credential**  
    **File/function:** `internal/core/rustimport/rustimport.go:376–387`: `Recover`.  
    With a credential-bearing unfinished journal and `Secrets=nil`, recovery skips deletion but removes the journal containing the only cleanup reference.  
    **Scenario:** An import stages a password, then recovery runs without a keyring adapter. Later recovery cannot discover the orphan. This is dormant in the current CLI because credential import is disabled, but contradicts the implemented protocol’s guarantee.  
    **Fix:** Preserve the journal and return an actionable error until credential deletion succeeds, or retain a durable cleanup record.

11. **MAJOR — `run` silently bypasses single-instance protection**  
    **File/function:** `internal/core/app/run.go:61–72`: `Run`.  
    A missing runtime directory skips locking entirely; lock errors other than `ErrLocked` merely warn and continue.  
    **Scenario:** Two headless runs without `XDG_RUNTIME_DIR` open the same history. The second calls `RecoverInterrupted`, finalizing the first run’s live call as interrupted; its eventual `FinishCall` cannot replace that finalized record.  
    **Fix:** Fail closed when exclusive ownership cannot be established, or provide a safe per-user fallback lock location.

12. **MAJOR — A blocked event consumer prevents signal-triggered shutdown**  
    **Files/functions:** `internal/core/app/run.go:124–136`: `Run`; `cmd/run.go:96–129`: `printer`.  
    Reporting runs synchronously before the loop can observe cancellation. The signal goroutine only cancels the context; it does not independently stop the engine.  
    **Scenario:** Output blocks on a stalled pipe during a call. SIGINT cancels the context, but the loop remains inside the writer and the microphone continues transmitting.  
    **Fix:** Supervise cancellation independently of reporting, and make output shutdown bounded without silently discarding retained final engine events.

13. **MINOR — Interrupting an unanswered call returns 1 instead of 130/143**  
    **Files/functions:** `internal/core/app/run.go:118–123,157–160`; `cmd/run.go:67–73`.  
    Cancellation first requests shutdown with no error. The resulting unanswered final snapshot subsequently installs `ErrCall`; the CLI prioritizes that error over the recorded signal.  
    **Scenario:** SIGINT during dialing produces status 1, unlike the established-call interrupt tested by the audio suite.  
    **Fix:** Track intentional cancellation separately and avoid converting its final call snapshot into a new failure; preserve the documented signal status.

14. **MAJOR — Several receipt claims exceed what the tests establish**  
    **Files/functions:** `internal/g0/scenarios_engine.go`, `internal/platform/audio/call_linux_test.go`, `internal/core/rustimport/rustimport_test.go`, `internal/core/store/store_test.go`.  
    Specific unsupported guarantees include:
    - **Device closure before `Stopped`:** `phone.stop` allows another three seconds for streams to close after the event stream ends. Its shutdown timeout is eight seconds, not five.
    - **No audio during hold/mute:** the tone checks only fail above a 50% share. Substantial intermittent leakage would pass; packet transmission is not measured.
    - **Microphone loss reported exactly once:** `quiet` filters call snapshots, not repeated notices. The binary test likewise does not count loss notices and permits up to 5% decoy-microphone blocks.
    - **Crash safety at every point:** injected “kills” return normally and execute deferred store closure and unlocks. Recovery interruption, concurrent recovery and power-loss durability are not tested.
    - **Actual full-disk behavior:** `max_page_count` exercises `SQLITE_FULL`, not filesystem exhaustion during WAL, backup or journal syncing.
    
    **Fix:** Add strict assertions and targeted fault-injection tests, and narrow the receipt’s wording until those guarantees are demonstrated. Reported execution results remain **unverified** in this review.

## Checked and found sound

- The normal actor `end` path guards against duplicate final snapshots; the outbox retains finals and places `Stopped` last when drained.
- TLS uses a fresh certificate pool, hostname verification and TLS 1.2 minimum; the bundle replaces system roots.
- Registrar filtering checks both resolved IP and configured port. Initial plain offers receive 488; successful outgoing plain answers encounter explicit teardown.
- Configuration precedence, value-free parser errors, secret formatting, doctor/export separation and handle-based permission checks are sound in the inspected paths.
- SQL values are parameterized; dynamic account columns are whitelisted. Migrations are transactional and preceded by consistent private backups.
- Rust configuration/database snapshotting reads source files only and opens SQLite on copies, not the source.

Not checked: builds, tests, network or hardware; omitted dependency behavior and raw third-party error redaction remain unverified. Data-loss and shutdown risks require fixes.

Verdict on M1: UNSOUND