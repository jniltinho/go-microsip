SOUND WITH CHANGES — M3 work and evidence: tasks 5.1–5.7, tray and local address.
SOUND WITH CHANGES — M4 work in the extract.

## Round 5 findings

1. **Partially resolved.** The shared deadline, unknown-outcome warning and reference retention address the reported late-starting operation and fresh-reference cases. `TestAnOperationGivenUpOnFindsItsTimeOver` checks expiration; `TestAKeyringThatAnswersLateLeavesNothingAstray` checks both immediate storage outcomes. Neither establishes ordering of unfinished remote writes; see finding 1 below.
2. **Resolved.** `serve` checks the sender against the notification service’s unique name. `TestCallNotificationsOnASessionBus` now sends a real notification’s action from a separate connection and requires it to be ignored.
3. **Resolved.** `Run`’s `quit` starts `ui.Quit` in its own goroutine, so `forward` no longer waits for toolkit termination. The precise overlapping-exit scenario remains untested, as the response acknowledges.
4. **Resolved.** `over` includes foreground-color alpha. The icon call site avoids counting its paint alpha twice. The eleventh control injects a transparent outline; `focus.txt` records its rejection at 1.00:1.

## Findings

### 1. M4 — Minor: the replacement still does not guarantee last-registration password ordering

**Files/functions:** `internal/platform/keyring/keyring_linux.go:44–60,119–146,207–233`, `open`, `withinFor`, `store`; `internal/core/app/phone.go:220–254,323–326`, `keep`, `Register`.

The argument is correct only in its narrower form: a call reaching godbus’s cancellation check with an expired context is not sent. It does **not** follow that everything unsent at the deadline remains unsent. Godbus checks the context, then enters its output-lock/write path without another context check. Expiration between those steps does not retract the message. [Godbus v5.2.2 implementation](https://raw.githubusercontent.com/godbus/dbus/v5.2.2/conn.go).

Furthermore, bus delivery order is not remote completion order: D-Bus explicitly permits concurrent method processing and out-of-order replies. [D-Bus method-call documentation](https://dbus.freedesktop.org/doc/dbus-tutorial.html).

**Concrete order in which an earlier password can still replace a later one:**

1. A sends `CreateItem(..., replace=true)` before its deadline.
2. A’s handler remains pending in a delayed, concurrently processing Secret Service.
3. A times out; `keep` records its reference and `Register` releases its mutex.
4. B subsequently writes the same reference successfully.
5. A’s handler finishes last and replaces B with A.

This is a static counterexample, **not an observed GNOME Keyring failure**. It depends on delayed/out-of-order keyring processing, so it is minor under the requested calibration.

The new context test never sends a D-Bus message. The application fake either stores **before returning** or never stores; neither forces this ordering.

**Fix:** isolate subsequent writes from an uncertain earlier mutation—for example, use distinct, durably tracked references rather than replacing the same item after an uncertain result. Add a test that completes A’s remote mutation after B. Narrow the absolute deadline/order claims meanwhile.

### 2. M4 — Minor: the owner preflight prevents notification-service reactivation

**File/function:** `internal/platform/notify/notify_linux.go:34–58,81–86`, `serving` and `connect`.

After the first successful connection, `connect` returns immediately. If the notification daemon subsequently exits, `serving` asks the bus for its owner, gets no owner, and discards the request. It never reaches another service-directed call that could activate the daemon.

**Concrete scenario:** an activatable notification daemon serves the first call and then exits. Later incoming calls all fail the owner preflight until another application activates that daemon. The softphone’s window remains available, but its desktop notifications silently stop.

Ordinary service-directed calls can trigger D-Bus activation; the owner lookup is instead addressed to the bus itself. [D-Bus activation rules](https://dbus.freedesktop.org/doc/dbus-specification.html).

**Fix:** when the owner is absent, make a bounded activation/capability request before resolving its unique name. Test notification delivery after the service exits and is activated again.

### 3. M3 — Minor: translucent backgrounds still escape the “as drawn” guarantee

**File/functions:** `frontend/scripts/contrast.js:23–34,44–50`, `backgrounds`, `over`.

Foreground transparency is fixed, but `backgrounds` returns the first background with any positive alpha without compositing it over what lies beneath. Contrast then treats its RGB channels as the actual background.

**Concrete scenario:** inject white badge text over `rgba(0,0,0,0.05)` on a white panel. The audit compares white against black and obtains 21:1. The displayed text is almost white-on-white and fails badly. The state-icon check can also pass.

This is a remaining audit limitation, **not evidence of unreadable shipped badges**, and is distinct from the resolved transparent-foreground finding.

**Fix:** composite translucent background layers before calculating contrast and add this negative control; otherwise narrow the receipt’s general “as drawn” transparency claim.

## Receipt assessment

- **M3 receipt:** the broad transparency guarantee exceeds the background handling described in finding 3. The specific transparent-outline correction and eleven recorded negative controls are supported.
- **M4 receipt:** “sends nothing after” the deadline is stronger than godbus’s implementation and the context-only test establish. The response’s assertion that send order guarantees the final password is also unsupported.
- **M4 restart wording:** “asks if it did not [keep the password]” is established for a **fresh reference**. With an existing password A, a timed-out replacement B that never takes effect leaves A available at restart; the application tries A rather than asking.
- **Tray/local-address receipt:** I found no additional overclaim in the reviewed assertions. It appropriately distinguishes route-table tests from VPN registration/audio, and explicitly leaves actual tray-host interaction and the native fallback dialog unverified.
- Current packaged runs and older VM runs are separately attributed. The supplied walkthrough logs contain the named subtests and match the current harness’s reporting locations. That supports consistency, not independent verification of artifact hashes or provenance.

## Checked and supported

- `get`/`read`, `store` and `remove` use the same operation context through their Secret Service calls. There is no second five-second service-call budget after connection.
- Caller waiting includes connection establishment. The silent-bus check supports bounded caller waiting, not termination of the underlying connection worker.
- Timeout warnings no longer assert definite session-only persistence. Fresh references are recorded in both outcomes exercised by the application test.
- Notification requests remain nonblocking at the event-forwarder boundary; the separate-connection sender rejection is meaningful.
- The asynchronous, once-only quit request removes the reported forwarder wait without changing call-confirmation logic.
- The skin registry/token checks, capture schedule, supplied audit records, local font declarations, offline-start checks, owner-review record and throwaway-skin exercise support their stated, bounded M3 purposes.
- Windows audio failure, missing Windows call walkthrough and unchecked task 5.8 remain disclosed. No production credential or personal path surfaced in the reviewed delta and supporting material.

Not checked: no builds, tests or applications were run; omitted images, font binaries and artifacts were not independently verified. Stale/excluded graph results were replaced with direct source reads.  
Risk: uncertain keyring writes still lack a complete ordering guarantee; the remaining findings are minor under the requested desktop calibration.