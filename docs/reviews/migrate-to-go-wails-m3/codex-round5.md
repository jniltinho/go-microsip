SOUND WITH CHANGES — M3 work and evidence: tasks 5.1–5.7, tray and local address.
UNSOUND — M4 work in the extract.

## Round-4 findings

1. **Resolved.** `UiState.ask` preserves an existing confirmation across links, deletes, close requests and delayed `busy` replies. “a link asks before it calls” now holds the quit reply pending and checks the reported ordering.
2. **Resolved for the reported tray/attention path.** Both hooks now return without waiting for toolkit dispatch. The active-call SIGTERM subtest and supplied log cover that exit path. A separate, narrower toolkit wait remains below.
3. **Partially resolved.** `Phone.Register` serializes normal registration/keyring operations, and the named test checks the resulting password. However, the new timeout wrapper can return before its write finishes, defeating that serialization.
4. **Resolved.** The current packaged run is attributed to artifact and checks commit `1ff7b57`. Its subtests and reporting locations match the supplied harness, including links, both active-call exits and remembered passwords.
5. **Resolved for the caller’s deadline.** `within` includes connection time in how long the caller waits; `TestASilentBusDoesNotHoldTheCaller` and `keyring.log` support the observed five-second return. They do not establish cancellation of the operation.
6. **Resolved.** Both icon audits now use painted stroke/fill information, including stroke opacity. The transparent-stroke negative control is present and recorded.
7. **Resolved.** Request URLs are extracted and classified by their prefix. The foreign URL containing the local address in its query is explicitly checked.

## New findings

### 1. M4 — Major: a timed-out password write can still overwrite a later password

**Files/functions:** `internal/platform/keyring/keyring_linux.go:45–51,108–127,184–216`, `open`, `within`, `Store.Set`; `internal/core/app/phone.go:239–241,320–355`, `keep`, `Register`.

`within` abandons its wait, not the operation. The worker receives no cancellation, and `open` starts another five-second context **after** connecting. `Register` releases its serialization lock when the outer timeout returns, although the password write may still be running.

**Concrete scenario:** an existing reference holds the account password. Save A spends part of its budget connecting, then waits for a keyring response. At five seconds it returns a timeout and reports “for this session only.” The person corrects the password and submits B. B finishes successfully; A subsequently resumes, still inside its later-started context, and replaces B with the older password. The next start uses A. The two submissions can be several seconds apart.

A first-ever timed-out save can instead leave a password stored under a reference that was never published to the database.

**Fix:** use one deadline throughout connection and operation, cancel the underlying work, and prevent an abandoned write from superseding a newer save. Already-submitted mutations need honest “outcome unknown” handling rather than a definite session-only claim. Test a timed-out operation that subsequently resumes and writes.

The current blocking-keyring test does not cover this: its `sync.Once.Do` also makes later fake writes wait, and it never returns a timeout while retaining unfinished work.

### 2. M4 — Minor: notification actions are not authenticated to the notification service

**File/function:** `internal/platform/notify/notify_linux.go:47,93–113`, `Desktop.serve`.

The match restricts interface and path, but not sender. The handler checks the notification ID and action string without checking `s.Sender`.

**Concrete scenario:** a deliberately hostile process running as the same user emits `org.freedesktop.Notifications.ActionInvoked` with a current notification ID and `"answer"`. The application answers that ringing call without anyone pressing its notification. `"reject"` similarly refuses it.

**Fix:** validate the notification service’s unique bus owner, and invalidate old ID mappings when that owner changes. Add a test using a second, non-owner connection; the existing “another notification” test only sends an unknown ID from the legitimate service.

This is **minor under the requested same-user hostile-process calibration**, despite the effect on consent.

### 3. M4 — Minor: `forward` still has a synchronous toolkit-quit path

**Files/functions:** `internal/gui/service.go:153–164`, `App.forward`; `internal/gui/gui.go:154–155,262,350–353`, `Run`.

`a.quit()` still calls `ui.Quit()` synchronously. Wails implements that call through synchronous main-thread dispatch. [Wails `Quit`](https://raw.githubusercontent.com/wailsapp/wails/v3.1.0/v3/pkg/application/application.go), [synchronous dispatch](https://raw.githubusercontent.com/wailsapp/wails/v3.1.0/v3/pkg/application/mainthread.go).

**Concrete scenario:** a page-requested shutdown has set `a.quitting`, but its final events have not arrived. SIGTERM independently ends the toolkit loop. When `Stopped` reaches `forward`, it calls `ui.Quit()` and waits for that departed loop. `Run` subsequently waits for `forwarded`.

The new SIGTERM test starts without a pending page shutdown, so it does not force this ordering.

**Fix:** make the forwarder’s quit request nonblocking and suppress toolkit dispatch after the loop has ended. Test termination while a page shutdown is already pending.

This overlapping-exit scenario is minor under the supplied timing calibration.

### 4. M3 — Minor: transparent foreground colors can pass the “as drawn” audit

**Files/functions:** `frontend/scripts/contrast.js:43–47,84–87`, `over`, `audit`; `frontend/scripts/focus.js:35–37`, ring calculation.

`parse` retains a color’s alpha, but `over` ignores it. Text and outline checks pass element opacity only. The icon path handles paint alpha separately; text and rings do not.

**Concrete scenario:** inject a Light-only rule making the Menu button’s focused outline `rgba(0,0,0,0)`, leaving its width and style unchanged. The ring is invisible, but the audit compares opaque black with the light background and passes it.

**Fix:** include foreground-color alpha in compositing, without counting icon alpha twice, and add this transparent-outline negative control. This is an audit limitation—not evidence of a currently invisible shipped ring.

## Checked and supported

- **Confirmations:** I found no remaining replacement of an outstanding `Confirm` through the supplied production paths. Approval consumes the preserved object before awaiting backend work.
- **Forwarder:** tray refreshes and notification requests use nonblocking sends; attention is dispatched separately. It does not perform keyring or notification-service calls. Finding 3 identifies the remaining direct toolkit wait.
- **Notifier ownership:** `shown`, `buttons` and the connection variable belong to `serve`; public methods only enqueue requests. Sharing the underlying godbus connection is supported by its concurrency contract. [godbus connection documentation](https://raw.githubusercontent.com/godbus/dbus/v5.2.2/conn.go).
- **Notification safety:** action sends cannot block on a full channel. Engine Answer/Reject commands retain the call ID and check that the call still rings. Caller markup is escaped, controls/direction markers covered by the sanitizer are removed, and length is bounded.
- **Registration lock:** it is separate from `p.mu`; command submission precedes the keyring write. Calls and `Close` do not acquire it, so I found no lock cycle with shutdown. A second registration can wait for the preceding save plus its own operation; the five-second bound is not a total latency guarantee for queued registrations.
- **Doctor/import:** for an already-current database, `store.Open` does not recover or terminate call records. Its migration transaction can briefly contend with the phone, but I found no concrete current-schema data-loss path. Import takes the instance lock and deletes the old credential only after successful configuration replacement.
- **M3 tasks:** the token/registry checks, seven-skin capture schedule, bundled-first font declarations, offline/font logs, owner-review record and throwaway-skin exercise support 5.1–5.7, subject to finding 4 and the disclosed visual-inspection limits.
- **Route/tray:** the route test establishes agreement within one DNS answer. VPN audio, protocol-dependent routing, actual tray-host interaction and the native fallback dialog remain explicitly unverified rather than falsely claimed.

### Receipts and attribution

- **M3:** the current AppImage log now matches the harness, including reporting at line 1271. The broader “as drawn” guarantee remains stronger than the alpha handling establishes.
- **Tray/local address:** the historical `a24194c` run is distinguished from the current packaged run. The VPN statement remains registration-only, appropriately.
- **M4:** the keyring log establishes bounded **caller waiting**, not bounded operation lifetime or session-only persistence after timeout. Notification tests establish ID routing, not sender authentication.
- **Revisions:** closing runs explicitly pair artifact/checks at `1ff7b57`; VM runs explicitly pair both at `4a5bd14`; old packaged screenshots are identified as `281c35f`. Earlier exploratory entries such as the throwaway exercise and “Runs at this state” are not individually given separate harness revisions. I found no remaining contradictory packaged-log reuse.
- Windows audio failure and the missing Windows call walkthrough are candidly recorded. Task 5.8 remains unchecked; this verdict does not close it.
- No apparent production credential or personal path surfaced in the reviewed material.

Not checked: no builds, tests or applications were run; omitted images, font binaries, released artifacts and checksums were not independently verified. Stale/excluded graph coverage was replaced with direct source reads.
Risk: timed-out keyring mutations remain the M4 blocker; the other findings are minor under the requested desktop calibration.