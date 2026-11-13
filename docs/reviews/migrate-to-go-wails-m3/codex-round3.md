UNSOUND

## Round-2 findings

- **N1 — Resolved.** `App.Shutdown` preserves quit intent on `engine_failed`; `forward` also handles stream closure. `TestAQuitOutlivesALateStopped` explicitly covers both late endings and queue refusal.
- **N2 — Resolved for the native fallback.** `Run` now asks whenever an engine may still run, without consulting cached call state, and defaults to Cancel. The native dialog remains untested, as disclosed. A separate page-side gap remains below.
- **N3 — Not resolved in code; truthfully documented.** The receipt accurately describes the one-DNS-answer limit. `TestTheEndpointBindsWhereTheRegistrarIsReachedFrom` establishes agreement within that answer, not subsequent resolution. Shipping with this limitation is defensible.
- **N4 — Resolved.** `rings` uses Shift, verifies the named screen before and after, and restricts auditing to the open layer. The supplied results distinguish Home’s 12 controls, Menu’s 14 and Settings’ 26; both new negative controls are recorded.
- **N5 — Resolved for the reported ancestor-opacity cases.** Contrast, focus and state checks now include ancestor opacity, with recorded negative controls. This does not establish every aspect of painted visibility; see finding 6.
- **N6 — Resolved for named SVG attributes and multiline declarations.** Both have fixtures, and the source is scanned as a whole. A different literal-color gap remains below.
- **N7 — Resolved.** `Run` creates `main` before opening the phone or starting `forward`, removing the reported incoming-attention initialization race.
- **N8 — Resolved.** `offline-skins.sh` requires built assets and distinguishes grep’s “no match” status from scan failure.
- **N9 — Resolved for the reported below-viewport case.** `scale-check.sh` adds vertical bounds and centre-point obstruction checks outside scrolling containers; the receipt now describes measurements rather than visual acceptance.
- **N10 — Resolved.** Ubuntu names bundled Inter first, and the font test requires the first family—not merely some fallback—to be bundled.

**Severity correction:** round-2 N3 should have been **minor** under this scale: changing DNS answers that cross interfaces are an unusual network case. Its documented limitation does not justify retaining the former first-interface behavior that broke ordinary VPN registration. Round-2 N5’s additional audit-coverage demand also deserved **minor**, absent an actual invisible shipped element. N4 was different: the recorded audit actually inspected the wrong screens.

## New and remaining findings

### 1. Major — the responsive page can still quit an unseen active call
**`frontend/src/state.ts:400–426`, `UiState.dispatch("quit")` / `quitNow`; `internal/gui/service.go:318–335`, `App.leave`**

The page treats `this.call === null` as permission for unconditional Shutdown. That state is also asynchronous. `Phone` journals call events before forwarding them (`internal/core/app/phone.go:150–159`).

**Failing scenario:** an outgoing call connects while its initial snapshots are delayed in journaling/event delivery. The responsive page still has no call. Alt+F4 delivers `"quit"` first; the page sends Shutdown without displaying the question, ending the connected call.

**Fix:** make unconfirmed shutdown idle-only **at the engine**, or conservatively ask when absence of a call cannot be established. Add a test delaying the initial call snapshots, not just `Stopped`.

Consequently, the tray receipt’s general claim that the page asks before ending a call remains stronger than the implementation.

### 2. Major — another link can change what an existing “yes” approves
**`frontend/src/state.ts:324–329,429–438`, `askDial` / `runConfirm`; `frontend/src/App.vue:241–253`, confirmation dialog**

`askDial` replaces `this.confirm` unconditionally. The existing dialog and focused affirmative button remain; its click dispatches generic `"confirm"`, which reads the latest value.

**Failing scenario:** the user reads the question for 1002 and focuses Call. A second link requesting 1003 arrives immediately before Enter. The same focused button now approves 1003, although the user intended to approve 1002. A link can similarly replace a pending Delete confirmation.

**Fix:** keep the displayed question immutable until answered. Defer or discard additional links while confirmation is pending, and bind approval to that particular question. Test two links arriving around one confirmation.

### 3. Major — merely receiving a link destroys unsaved edits
**`frontend/src/state.ts:231–234,324–329`, `closeView` / `askDial`**

Before asking anything, `askDial` closes the current view and clears `contactForm`. Closing Settings also unmounts its component-local form.

**Failing scenario:** the user has typed a new contact, or edited account settings without saving. A link reaches the running instance. The editor disappears and its draft is lost—even if the user presses Cancel.

**Fix:** show the confirmation over the existing view without resetting it. Preserve the draft on cancellation; defer navigation until the user accepts. Add a link-then-Cancel test with an edited contact.

### 4. Minor — bracketed host validation accepts non-IPv6 addresses
**`internal/core/domain/link.go:114–147`, `validLinkHost`**

The bracketed branch checks an alphabet, not IPv6 syntax.

**Failing scenario:** `sip:1002@[1]` and `sip:1002@[:::]` pass and become target `1002`, although neither bracketed host is valid IPv6. The rejection table tests `[zz]`, which exercises only the alphabet restriction.

**Fix:** parse bracketed addresses with `net/netip` and require IPv6; add malformed-address fixtures. The host is discarded afterward, so this does **not** redirect signalling to another server.

### 5. Minor — the link cancellation check can inspect stale state
**`internal/platform/audio/window_linux_test.go:470–493`, `linksInTheWindow`**

After observing the question disappear, the test sleeps and examines `w.call.ID`. Sleeping does not consume `w.lines`; only `w.take` updates that cache.

**Failing scenario:** a regression schedules dialing shortly after Cancel. The new call events accumulate during the sleep, while the assertion still sees the previous call ID. The later drain consumes them but checks only whether another question appeared, so the test can pass.

**Fix:** consume the trace throughout the observation interval and reject every new call ID, including during the later drain. Add a delayed-call negative control.

### 6. Minor — an invisible SVG shape still passes the state audit
**`frontend/scripts/prints.sh:43–47`, `state`; `frontend/scripts/contrast.js:76–80`, SVG audit**

Visibility and contrast are checked on the SVG container; the shape is checked only for existence.

**Failing scenario:** add `.badge path { opacity: 0 }`. The SVG retains its box, opacity and computed stroke, and still contains a path. Both audits pass although the state icon paints nothing. The seven existing negative controls do not cover this case.

**Fix:** check the actual painted shapes’ visibility/opacity, with this negative fixture. Until then, the M3 receipt’s “icon in sight” claim exceeds what the named checks establish. This is an audit limitation, not evidence that the current icons are invisible.

### 7. Minor — uppercase functional colors evade the literal-color gate
**`frontend/src/themes/themes.test.ts:142–157`, `LITERALS`**

The functional-color expression is case-sensitive, unlike CSS function names.

**Failing scenario:** `.example { color: RGB(1, 2, 3); }` in `style.css` matches none of the patterns and passes the gate. It is an ordinary source literal, not a color assembled at runtime.

**Fix:** make that expression case-insensitive and add a fixture. The receipt’s functional-color enforcement claim should remain bounded accordingly.

### 8. Minor — the portable application installs a desktop entry without link support
**`internal/gui/desktop_linux.go:94–103`, `desktopText`; `build/linux/go-microsip.desktop`**

Only the build template gained `%u` and the SIP/TEL MIME types. The entry installed by the running portable application still has neither.

**Failing scenario:** on a desktop without a system entry, starting the AppImage creates its generated launcher. That launcher does not advertise SIP/TEL handling; if associated with a URI, its `Exec` line does not forward the URI.

**Fix:** add the same MIME types and `%u` to `desktopText`, with assertions in `TestInstallDesktop`. This is unfinished integration under unchecked task 6.1, not an M3 acceptance blocker by itself.

### 9. Minor — receipt evidence still mixes revisions and scopes
**`docs/receipts/go-tray-and-route-2026-11-12.md:37`; `docs/receipts/go-m3-skins-2026-11-12.md:50–65`, evidence rows**

Naming artifact commits improves attribution, but does not establish that the corrected harness was used.

- The tray receipt attributes the minimized-window and third-registration checks to the linked AppImage run. That supplied log still has the older harness fingerprint—final reporting at line 885, versus 928 before this round’s added link test. It does not establish those corrected packaged checks.
- The browser network log reports Ubuntu loading no bundled font. That cannot demonstrate the current Ubuntu stylesheet’s Inter-first behavior. Its historical environment is described, but it remains under “Runs at this state.”

**Concrete consequence:** someone can accept corrected packaged cleanup or current Ubuntu asset evidence using records that did not exercise those checks/state.

**Fix:** identify both harness and artifact revisions, attach the corresponding run, or explicitly narrow these records to historical evidence. This does not show that either artifact failed.

## Checked and supported

- The late-`Stopped`, duplicate-shutdown and native conservative-confirmation changes address their reported cases.
- The focus records support 77 audited screens and 1,008 controls, with genuinely different Menu and Settings coverage. The seven injected faults are recorded.
- The ordinary link path passes only a validated user part, revalidates hand-off data, displays it as text, and does not itself dial. `Endpoint.uri` still uses the configured server.
- The new silent-PBX tests require observed zero-expiry REGISTER requests, and the active-call case also requires a BYE and final call snapshot. They test bounded **engine stopping**, not the entire desktop process.
- The Windows lock uses an exclusive, nonwaiting range lock and closes failed handles. Its test covers contention and reacquisition; the VM script propagates its exit status.
- I found no new vacuous-success mistake in the changed PowerShell command slicing or current nonempty font-face lists. The offline scan now fails on missing assets or grep errors.
- The seven-skin contract, Inter-first Ubuntu stack, local font declarations and notices concatenation are supported by the source.
- The supplied extract revealed no non-fixture credential or personal path in the searches performed.
- Keeping 5.8 unchecked honestly preserves the Windows call-walkthrough and automation-method gaps.

Not checked: no application, build, test or check script was run; omitted images, font binaries, artifact hashes and packaged notices were not independently verified.  
Risk: findings 1–3 remain shipping blockers; the documented unusual-network limitation is not one.