UNSOUND — tasks 6.4–6.6: failed revocation is blocked in the current owner, but the page still hides a usable bootstrap and clears its warning; the named test misses this path.
UNSOUND — task 6.3 in this extract: notification actions lack session binding, and several Windows checks marked passed are absent from the supplied scripts.

## Round-1 findings

1. **Partially resolved.** `Revoke` and `requestGrant` prevent fresh use through this owner. `TestARevocationThatIsNotStoredStillRefusesTheGrant` covers that, but not a bootstrap consumed by a later service or the page clearing the error.
2. **Resolved.** `Handle` checks `closed`, `Close` clears pending approvals, `DenyAll` terminates, and `track`/`stop` close existing connections and reject late tracking. The two named lifecycle tests cover the reported failures.
3. **Resolved.** `controlledPhone.SetDND` emits the state; the page applies `"dnd"` and toggles from it. The named backend and page tests cover this path.
4. **Resolved.** `ContactsUpTo` and `HistoryUpTo` pass the requested bound into storage. `TestListingsPageBeyondAThousandRecords` covers history beyond 1,000; the contacts path uses the corresponding implementation.
5. **Resolved in the code.** Held startup retains the registration commands; `service` consumes the bootstrap and starts serving before `Begin`. No registration command reaches the engine before consumption. The named test is weaker than its description—see finding 8 below.
6. **Resolved.** `approvalSeen` resets the timer while an incoming-call dialog hides the request. The named page test covers its appearance afterward.
7. **Resolved.** `ReadMessage` requires end-of-input after the JSON value; request-envelope names are checked exactly. The named test includes both trailing delimiters and case variants.
8. **Partially resolved.** Pause, expiry and invalid-capability behavior are now described accurately, but the new instruction to revoke a grant to stop already-authorized DTMF is false.
9. **Resolved.** Generated JSON-mode keys go to standard error; standard output remains the JSON response. The named test checks presence, although it merges the two streams.
10. **Resolved.** The receipt now explicitly distinguishes mock-backend tests and rendered screens from an actual grant-form walkthrough.

## Findings

1. **[6.4–6.6] Major — failed bootstrap revocation still appears completed.**  
   **Files/functions:** `frontend/src/components/AgentAccess.vue:36–40,66–68`, `refresh`/`revoke`; `internal/control/grants.go:313–341`, `Grants`/`Revoke`.

   `revoke` catches the backend error, then unconditionally calls `refresh`, which clears that error after a successful read. Meanwhile, `Grants` hides every locally revoked ID, including an unconsumed bootstrap whose database revocation failed.

   **Scenario:** Export a bootstrap, then revoke it while database writes fail but reads succeed. The row disappears and the warning is cleared. After storage recovers, quit the window and start the service with that file before expiry. `ConsumeBootstrap` still accepts its unrevoked database row; the new owner has no previous owner’s refusal map.

   `TestARevocationThatIsNotStoredStillRefusesTheGrant` tests a session grant through the same owner only. The response README’s claim that the page tells the person what remains usable is therefore stronger than the implementation.

   **Fix:** Preserve the revocation error. Keep failed-to-revoke bootstraps visible as still usable, with a retry action. Add a component test and a later-service consumption test with revocation storage failure.

2. **[6.3] Major — an old toast can authorize a different engine session’s call.**  
   **Files/functions:** `internal/platform/notify/notify_windows.go:9–15,19–23,31–39`, `show`/`serve`; `notify.go:54–69`, `toastXML`/`toastAction`; `internal/core/domain/state.go:67`.

   Notifications persist, their application/activation identities are constant, and their action arguments contain only a call ID. `CallID` is explicitly session-scoped. Nothing rejects a notification from an earlier run.

   **Scenario:** A notification for call 7 remains after that call ends and the application exits. A later run has a ringing call with ID 7. Pressing the old notification’s Answer or Reject produces a command for the new call, although the notification described the old caller. This needs neither a hostile process nor a tight race. The library documents that its callback handles activation from any available notification. [wintoast documentation](https://pkg.go.dev/git.sr.ht/~jackmordaunt/go-toast/v2@v2.0.3/wintoast)

   **Fix:** Bind actions to a fresh run/notification identity and accept them only while their matching ringing-call entry remains active. Invalidate that entry on `Close`. Test an old action against a new session reusing the same call ID.

3. **[6.3] Major — the Windows receipt attributes checks to scripts that do not perform them.**  
   **Files/functions:** `build/vm/desktop.ps1:81–107`; `build/vm/check.ps1:127–131`; receipt Task 6.3 rows and Status.

   After status, history and the session test, `desktop.ps1` ends. It never performs the claimed dial/approval/Escape/operation-outcome sequence, second launch with a link, close-to-tray, or reopening.

   The notification block runs **`ControlTest`**, not `NotifyTest`. It never enables or starts `TestAToastOnThisDesktop`; `$pressed` is never assigned. Counting existing notification history cannot establish that this run submitted a toast.

   **Scenario:** A broken second-instance or tray implementation can survive these scripts. A retained notification can make the notification check succeed even when the new notifier never runs; an empty history can make an otherwise working implementation fail.

   **Fix:** Implement the claimed actions and run the actual notification test, identifying a fresh notification—or withdraw the corresponding pass claims. Record the exact artifact and run: the receipt’s Runs section still says Windows was not rerun and that its recorded executable has no control transport. The omitted last log cannot resolve what these supplied scripts actually check.

4. **[6.4–6.6] Minor — the DND callback introduces opposite mutex ordering.**  
   **Files/functions:** `internal/control/owner.go:298–304,637`, `Handle`/`execute`; `internal/gui/service.go:180–190,747–760`, `Subscribe`/`controlledPhone.SetDND`/`dndChanged`.

   Agent execution holds the owner mutex and then takes the application mutex through `dndChanged`. `Subscribe` holds the application mutex and calls `approvals`, which takes the owner mutex.

   **Scenario:** A page resubscription overlaps a granted DND change: each waits for the other mutex. Control and event forwarding can then remain blocked. This overlap is minor under the requested calibration.

   **Fix:** Remove the opposite lock ordering, preferably delivering the DND callback after releasing the owner mutex, like the existing `changed` notification. Add a synchronized overlap test.

5. **[6.3] Minor — PID-to-token authentication has a reuse gap.**  
   **File/functions:** `internal/control/endpoint_windows.go:39–82`, `userOf`/`peer`.

   The socket query returns a numeric PID; `OpenProcess` subsequently resolves that number. Those are not an atomic identity lookup.

   **Scenario:** If the returned PID is recycled in that interval, the comparison uses the replacement process’s token. A replacement owned by this user can produce a false acceptance; another user’s replacement can produce a false rejection. Holding a handle *after* that replacement does not repair the earlier lookup. Windows documents PID reuse and process-object lifetime. [Microsoft explanation](https://devblogs.microsoft.com/oldnewthing/20110107-00/?p=11803)

   This is not an independently demonstrated bypass of the default private profile directory, and the narrow timing makes it minor here.

   **Fix:** Reject recycled/dead identities using appropriate lifetime validation and a pinned handle, or use socket-bound/token-based authentication such as named-pipe impersonation. Keep directory permissions as an independently enforced boundary.

6. **[6.3] Minor — holding the lock does not make every existing socket-path file disposable.**  
   **File/function:** `internal/control/endpoint_windows.go:111–117`, `Listen`.

   `os.Remove` deletes whatever occupies `control.sock`, without establishing that it is an AF_UNIX socket. The lock establishes exclusion between cooperating owners, not the existing file’s type.

   **Scenario:** A regular diagnostic/data file is left at that pathname. Starting control silently deletes it. `TestAClientReachesTheOwnerOnWindows` explicitly creates a regular file and treats its deletion as stale-socket recovery.

   **Fix:** Inspect the Windows AF_UNIX reparse tag and refuse unrelated files. Test recovery using a genuine stale socket, and separately test refusal of regular files.

7. **[6.4–6.6] Minor — the revised documentation promises DTMF cancellation that does not exist.**  
   **Files/functions:** `docs/agent-cli-mcp.md:53–56`; `internal/control/owner.go:650–670`, `paceDTMF`.

   “Revoke the grant to end those” follows the explanation of ongoing DTMF. Pacing checks the owner and call state, but neither grant revocation nor its expiry.

   **Scenario:** Start a granted 32-digit sequence, then revoke the grant after its first digit. The remaining digits continue.

   **Fix:** Say that revocation prevents future grant use but does not cancel an already-authorized sequence; ending the call interrupts it. No cancellation mechanism is needed merely to correct the document.

8. **[6.4–6.6] Minor — the held-phone test does not establish its full advertised guarantee.**  
   **File/test:** `internal/core/app/phone_test.go:956–965`, `TestAHeldPhoneRegistersOnlyWhenItBegins`.

   Its pre-`Begin` check reads at most one event, then immediately proceeds. It neither observes the whole interval nor counts PBX traffic.

   **Scenario:** An initial `Disabled` event followed by premature registration could pass this check; the subsequent registration is accepted by the post-`Begin` wait. The second `Begin` also has no assertion against another registration.

   **Fix:** Observe the full pre-start interval and assert no PBX registration, including through a rejected-bootstrap service start. The source ordering resolves round-1 finding 5; this test alone is not packet-level evidence for it.

## Checked and found supported

- **Current-owner revocation:** fresh reads and mutations with the refused capability are blocked, even when its database row remains active. Replaying an existing operation does not execute it again.
- **Shutdown:** the closed-owner guard, pending-list clearing, terminating `DenyAll`, and stopped-server tracking address the original lifecycle defects.
- **Parsing and paging:** exact request-field checks and trailing-input rejection are present. Both storage listings receive the larger page/look-ahead bound; the window retains its own 1,000-row bound.
- **Startup:** `Held` withholds the startup commands. In `service`, bootstrap consumption precedes listening and `Begin`; configuration/password inspection does not send a registration command.
- **Windows isolation:** with the expected private profile ACLs, I found no ordinary other-user route to connect, impersonate the owner, or read grant files. Windows enforces permissions on pathname sockets. This protection depends on inherited ACLs—the code does not validate them, and the self-process transport/file tests do not establish cross-user isolation. [Microsoft AF_UNIX security description](https://devblogs.microsoft.com/commandline/af_unix-comes-to-windows/)
- **Windows link registration:** writes are confined to HKCU application/class registration, not the user’s default-handler choice. The executable path is quoted and is not passed through `cmd.exe`: spaces and shell metacharacters in that path are not shell code. Paths containing `%` or `"` skip registration.
- **Toast contents:** caller text is bounded, strips controls/direction characters, and escapes XML text delimiters. Quotes inside the text node do not escape it. `toastAction` accepts only the two named numeric actions and ignores body clicks. A late action still names its original ID, not “the current call”; the cross-session identity problem is finding 2.
- **Evidence boundaries:** the supplied Linux logs support their recorded approval, denial, replay, CLI/MCP and service scenarios. The revised receipt correctly leaves the actual grant-form walkthrough, Windows calls/audio and notification buttons unverified. README responses 1 and 5 need the qualifications above; response 8 needs its remaining documentation correction.
- No apparent real credential or identifiable personal home path was found in the reviewed additions and named logs. Graph freshness checks flagged changed/untracked files; conclusions above use current source reads rather than stale graph positions.

**Not checked:** no builds, tests or applications were run; no screenshots, binaries or omitted Windows log were validated. The engine implementations and toast-library implementation were unavailable in the extract, so downstream runtime behavior was not independently verified.