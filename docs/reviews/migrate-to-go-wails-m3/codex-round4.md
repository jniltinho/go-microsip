UNSOUND — M3  
UNSOUND — M4

Static review, calibrated to one person using a desktop softphone. No application, build, test or check script was run. Stale graph results were supplemented with direct source reads.

## Round‑3 findings

1. **Resolved.** `ShutdownIfIdle` makes the original stale-call decision inside the actor. `TestAConditionalShutdownIsRefusedDuringACall`, the GUI test and the page test cover the reported path.
2. **Resolved for the reported link replacement.** `askDial` refuses another link while a confirmation exists; the page test covers two links and a pending delete. A different replacement path remains below.
3. **Resolved.** Receiving a link no longer closes the editor. The contact-draft cancellation test covers this; the component structure also preserves Settings.
4. **Resolved.** `validIPv6` rejects the reported malformed bracketed addresses; the added acceptance and rejection fixtures exercise its group-count and IPv4-tail rules.
5. **Resolved.** `watch` consumes trace events throughout both observation intervals and checks for a new call ID instead of inspecting an unchanged cache.
6. **Resolved for the reported `opacity: 0` fixture.** Both audits now detect it, and both negative controls are recorded. Another painted-visibility gap remains below.
7. **Resolved.** The functional-color expression is case-insensitive and includes the uppercase `RGB(...)` fixture.
8. **Partially resolved, deliberately and defensibly.** The generated entry forwards `%u`. Not advertising SIP/TEL handling before consent is reasonable, and the remaining integration work is truthfully recorded. `TestInstallDesktop` checks forwarding.
9. **Partially resolved.** The supplied network record now includes Ubuntu’s Inter request. The packaged-run evidence still has the older harness fingerprint and does not substantiate the claimed reviewed behavior.

## New and remaining findings

### 1. M3 — Major: a delayed quit refusal can change what an existing approval means

**Files/functions:** `frontend/src/state.ts:430–461`, `quitIfIdle` / `runConfirm`; `frontend/src/App.vue:241–253`.

The `busy` reply unconditionally replaces `this.confirm`. The existing confirmation dialog and affirmative button remain mounted; their handler reads the replacement.

**Failing scenario:** an unconfirmed Quit is awaiting its engine reply while call snapshots are delayed. A link arrives and displays “Call 1002”; the user focuses Call. The delayed `busy` reply changes that question to Quit. Enter, intended to approve 1002, now sends unconditional Shutdown and ends the existing call.

The current tests resolve the quit immediately, so they miss this ordering.

**Fix:** preserve an existing question when either quit path wants to ask another one, and bind approval to that question’s identity. Add a test holding the quit promise pending while a link confirmation is displayed.

### 2. M4 — Major: a final call event can leave shutdown waiting on a dead GUI

**Files/functions:** `internal/gui/gui.go:208,239,327–330`, `Run`; `internal/gui/service.go:134–135`, `forward`.

`forward` synchronously rebuilds the tray menu on every final call snapshot. That hook remains active after `ui.Run()` returns, when deferred `phone.Close()` ends any remaining call.

**Failing scenario:** SIGTERM ends the toolkit loop during an active call. Deferred `phone.Close()` produces the final snapshot. `forward` calls `tray.SetMenu`, which waits for GUI-thread execution; the loop has already ended. `Run` then waits forever for `forwarded`.

This is a static inference from the application’s lifecycle and pinned Wails implementation: [`SetMenu` uses synchronous dispatch](https://raw.githubusercontent.com/wailsapp/wails/v3.1.0/v3/pkg/application/systemtray.go), and [`InvokeSync` waits for that dispatch](https://raw.githubusercontent.com/wailsapp/wails/v3.1.0/v3/pkg/application/mainthread.go).

The active-call quit test stops the engine **before** leaving the GUI. The other signal tests signal an idle phone.

**Fix:** cancel/drain tray updates while the GUI loop is alive, and prevent the event forwarder from waiting on GUI work during teardown. Add an active-call SIGTERM process-exit test.

### 3. M4 — Major: overlapping saves can remember the older password

**Files/functions:** `internal/core/app/phone.go:216–245,307–347`, `keep` / `Register`; `frontend/src/components/SettingsView.vue:56–76,187`.

The mutex protects individual accesses to `p.password`, not the registration-and-keyring operation. The Settings form also permits another submission while the first is pending.

**Failing scenario:** an existing reference is present. Save A queues registration with an incorrect password, then pauses before its keyring write. The user corrects it and submits B; B registers and stores the correct password. A resumes and overwrites that same keyring entry with the incorrect password. This session uses B, but the next start reads A and cannot register.

The named password test performs replacement sequentially.

**Fix:** serialize backend registration/password persistence in command order. Add a blocking-keyring test that forces the two writes to finish in the opposite order.

### 4. M3 — Major: the packaged evidence still mixes revisions

**Locations:** `docs/reviews/migrate-to-go-wails-m3/README.md:61`; M3 receipt “Runs at this state” and “Closing M3”; tray receipt’s AppImage evidence; `docs/receipts/go-m3/appimage-call.log`.

The README says the final artifact was driven by its matching harness. The supplied log still reports the walkthrough at line **885**, contains no `links` subtest, and has the older scale-report locations. The supplied newer unpackaged log reports the walkthrough at **1034**; current source reports it at **1166**.

The same AppImage log is also cited under both artifact revisions `5b1d188` and `281c35f`.

**Concrete consequence:** the old successful run can be accepted as evidence for corrected packaged minimized-window, cleanup and font checks that its harness did not perform. This does not establish that the artifact itself failed.

**Fix:** attach a run identified by both artifact and harness revisions, or label this log historical and remove the unsupported current-behavior attribution. This is the unresolved packaged portion of round‑3 finding 9—not a penalty for leaving task 5.8 unchecked.

### 5. M4 — Minor: the five-second keyring deadline starts too late

**File/function:** `internal/platform/secrets/secrets_linux.go:45–51`, `open`.

`dbus.SessionBus()` runs before the timeout context exists. Its connection/authentication/Hello work is therefore outside the advertised whole-operation bound. The pinned [godbus connection code](https://raw.githubusercontent.com/godbus/dbus/v5.2.2/conn.go) confirms that sequence.

**Failing scenario:** a stalled session-bus endpoint accepts a connection but never completes authentication. `Get` or `Set` never reaches the five-second context, and its caller remains blocked.

The missing-bus test uses a nonexistent socket, which fails immediately; it does not test this case.

**Fix:** include connection establishment and authentication in the deadline, using a cancellable private connection with bounded transport setup. Add a silent-handshake fixture. This is minor under the requested calibration because it requires an abnormal local bus.

### 6. M3 — Minor: transparent strokes still pass both icon audits

**Files/functions:** `frontend/scripts/prints.sh:44–48`, `state`; `frontend/scripts/contrast.js:76–86`, SVG audit.

The new shape check inspects `opacity`, display and visibility, but not whether the shape’s stroke or fill paints anything. Contrast still uses the **SVG container’s** stroke.

**Failing scenario:** add `.badge path { stroke-opacity: 0 }`. These badge icons have no fill. They disappear, but shape opacity remains 1 and the SVG retains its contrasting stroke value. Both checks pass.

**Fix:** evaluate the shapes’ effective painted stroke/fill, including their opacity, and add this negative control. No currently invisible shipped icon was established; the defect is in the receipt’s “icon in sight” evidence.

### 7. M3 — Minor: the network check compares substrings, not origins

**File/function:** `frontend/scripts/network-log.sh:20–26`, request classification.

`grep -vc "$base/"` treats any request line containing that text as local.

**Failing scenario:** a request to `https://assets.example/font.ttf?from=http://127.0.0.1:5173/` is counted as inside the page’s origin. With ordinary local font requests also present, the check can pass.

**Fix:** parse each request URL and compare its actual origin with the base URL’s origin. Add this foreign-origin fixture. This weakens the check’s universal claim; it does not show a remote request in the supplied record or weaken the built page’s CSP.

## Receipt assessment

- **M3:** the packaged-revision claim and the universal painted-icon/network-origin claims exceed their evidence, as detailed above. The offline-start limitation, focus-audit scope and unchecked 5.8 are otherwise candid.
- **Tray/local address:** the packaged attribution remains unsupported. The route test establishes agreement within one resolved address set—not VPN audio or later DNS answers—and those limits are accurately disclosed. Tray-menu operation and the native fallback dialog are explicitly unverified.
- **M4:** the whole-operation keyring deadline is overstated. The new `quit during a call` and `the password is remembered` claims have no corresponding subtests in the supplied window log; “Runs” remains empty. I cannot verify those claimed runs from this extract. The engine-only silent-PBX scope and unexecuted Windows Credential Manager adapter are truthfully distinguished.

## Checked and supported

- The conditional-shutdown answer channel has room for its reply; `ShutdownIfIdle` also waits on engine completion. I found no reply-wait deadlock in that path.
- Hooks for attention and recent-history updates are installed before forwarding begins. Trace-hook replacement uses the service mutex.
- Each tray row captures its own number from the same record as its label; I found no loop-variable mistake that substitutes another row’s number.
- New password references use random bytes, not account identity. In the provided paths, typed secrets go to registration and the keyring, not Settings or UI events. Logging avoids binding arguments and Secret Service error bodies.
- The adapter never calls Secret Service `Prompt`; it dismisses returned prompt objects. That matches the [protocol’s prompting model](https://specifications.freedesktop.org/secret-service/latest/prompts.html).
- I found no concrete Windows credential-layout or returned-buffer lifetime defect: the fields match [`CREDENTIALW`](https://learn.microsoft.com/en-us/windows/win32/api/wincred/ns-wincred-credentialw), and the blob is copied before `CredFree`. The lock retains its file handle and closes failed acquisitions.
- The SQLite URI change keeps the Windows drive in the path and escapes URI-significant characters; its added test is present in the diff.
- The supplied audit records support the stated screen/control counts and nine injected faults, subject to the specific limitations above.
- No apparent production credential or personal path was found in the reviewed material.

Not checked: runtime behavior, omitted dependency/store implementations, screenshots, font binaries, artifact hashes or packaged notices.  
Risk: confirmation substitution, password-save ordering and GUI teardown remain blockers; the packaged evidence also needs correction.