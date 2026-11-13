UNSOUND

Static review only: I read the implementations, named checks, both diffs, receipts and supplied logs. I ran no application, build, test or check script.

## Round-1 findings

1. **Resolved:** repeated close is no longer consent. `leave`/`QuitAsked` preserve an acknowledged page’s confirmation path; the native dialog defaults to Cancel. `TestLeavingAsksThePageThatListens` checks repeated acknowledgements, although it does not drive the native dialog.
2. **Partially resolved:** `ending` and `pending` fix the reported duplicate-shutdown race. A different delayed-`Stopped` interleaving still loses a first quit request; see N1.
3. **Partially resolved:** `localFor` and its table test handle disagreement within the addresses already resolved. Later hostname resolution remains uncoupled. The UDP-probe limitation is honestly recorded and defensible as restricted network support, not general route correctness.
4. **Partially resolved:** the loopback regression is removed and tested. Split signalling/media routes can still produce silent calls. The receipt states this truthfully and correctly limits the owner’s VPN report to registration. Defensible as an interim non-ICE limitation, not proof of VPN audio support.
5. **Resolved:** `scale-check.sh` sets both storage keys and checks both effective skin attributes for every measurement.
6. **Partially resolved:** the original missing-outline and `display:none` examples are detected, with negative controls. The new focus sequence misses Menu and Settings, and visibility remains incomplete; see N4–N5.
7. **Partially resolved:** the trace and three runtime checks now require the named font faces/weights and no reported errors. They still do not establish complete rendering. The receipt’s startup-smoke limitation and filename-only unit-test description are truthful and defensible as limited evidence.
8. **Partially resolved:** same-line `color: navy` and named arbitrary-value utilities are caught and have fixtures. Other ordinary literal forms still escape; see N6.
9. **Resolved:** Doctor derives the warning independently of successful account resolution. The added settings-only profile test checks this case.
10. **Resolved for healthy-PBX close-button cleanup:** the third registration adds a PBX-dependent witness, and `unregistered` now accurately describes its narrower observation. Deferring unanswered-PBX bounded-exit acceptance to 6.1 is defensible for M3, but desktop-integration acceptance remains open.

## New and remaining findings

### N1. **Major — a first quit can still be lost**
**`internal/gui/service.go:248–274`, `App.Shutdown`**

Every send error clears `quitting`, although `Phone.Send` can return `engine_failed`, not just queue overload.

**Failing scenario:** the engine has stopped, but its `Stopped` event has not reached `forward`. The user quits; sending Shutdown returns `engine_failed`; `quitting` becomes false while cached `stopped` is still false. When `Stopped` finally arrives, `forward` does not quit. The duplicate-request test does not force this ordering.

**Fix:** distinguish overload from an already-ended engine. Preserve the quit intention on `engine_failed` until `Stopped` arrives. Add a test that delays delivery of `Stopped` before the first shutdown request.

### N2. **Major — the native fallback can end a call without confirmation**
**`internal/gui/gui.go:164–177`, native `leave` closure; `internal/gui/service.go:312–325`, `inCall`/`end`**

`inCall` reads the GUI’s asynchronous event cache, not an engine-atomic condition. A false result immediately permits unconditional Shutdown.

**Failing scenario:** an outgoing call has connected, but journaling/event forwarding is delayed and `a.call` remains nil. The page is unresponsive; a repeated close reaches the native fallback. It skips the question and shuts down the established call without a yes.

**Fix:** conservatively ask when backend state is uncertain, or provide an engine-checked idle-only shutdown that refuses when a call exists. Test with call-event delivery deliberately delayed.

### N3. **Major — route agreement is still only a DNS snapshot**
**`internal/core/sip/endpoint.go:106–155,227–233`, `bindHost`, `localFor`, `NewEndpoint`, `uri`**

The consensus check covers `NewEndpoint`’s lookup, but registration and calls still use `s.Server`, allowing a later lookup to choose a different destination.

**Failing scenario:** the initial DNS answer contains only a VPN-reachable registrar, selecting the VPN source address. The SIP lookup returns a LAN-reachable registrar from another DNS answer. Traffic remains bound to the VPN source, and replies cannot reach it. Every current route-table test can pass.

**Fix:** couple the selected destination and source address for the actual transport, preserving the hostname for TLS verification. Test differing successive DNS answers. This residual limitation is not recorded alongside the protocol-routing limitation.

### N4. **Major — the focus audit closes the menu before auditing it**
**`frontend/scripts/prints.sh:56–60,195–200`, `rings`; `frontend/src/components/AppMenu.vue:27–32`, `onKey`**

`rings` presses Tab. Inside the open menu, Tab explicitly closes that menu.

**Failing scenario:** remove `.menuitem:focus-visible`’s outline. The “menu” audit first closes the menu and then passes on Home controls. The following Enter does not reliably open Settings either. The supplied `focus.txt` records exactly 12 controls for Home, Menu and Settings in every skin, consistent with this wrong-screen sequence.

**Fix:** establish keyboard modality without dismissing the scene, assert the expected screen before and after auditing, and explicitly reopen/select Settings. Add a menu-specific negative control.

### N5. **Major — “in sight” and “as drawn” ignore effective opacity**
**`frontend/scripts/prints.sh:42–51`, `state`; `frontend/scripts/focus.js:18–35`; `frontend/scripts/contrast.js:39–48`**

The state check inspects the SVG’s opacity but not its ancestors. The contrast and focus checks also ignore opacity compositing.

**Failing scenario:** `.badge { opacity: 0 }` makes the call-state labels and icons invisible. Their boxes remain nonzero, the SVG’s own opacity remains 1, and the computed colors retain their original contrast. The audits can pass, including both existing negative controls. Likewise, making a focused control transparent leaves its computed outline intact.

**Fix:** check effective visibility through ancestors and account for transparency when evaluating painted contrast. Add ancestor-opacity and transparent-focus negative cases.

### N6. **Minor — named SVG colors escape the literal-color gate**
**`frontend/src/themes/themes.test.ts:142–186`, `LITERALS` and the literal-color test**

Named-color matching covers declarations and utility classes, not SVG presentation attributes.

**Failing scenario:** a component contains `<path d="…" fill="navy" />`. None of the patterns matches it, so the component can introduce a skin-independent literal color while the named gate passes. Splitting a named-color declaration across lines also escapes the line-by-line scan.

**Fix:** cover presentation attributes and multiline declarations, with negative fixtures for both. Narrow the receipt’s universal enforcement claim until then.

### N7. **Minor — incoming attention races window initialization**
**`internal/gui/gui.go:116–123,153–159`, `Run`/`reveal`; `internal/gui/service.go:116–117`, `forward`**

`forward` starts before `main` is assigned. Its newly added attention callback reads the same variable without synchronization.

**Failing scenario:** a configured account registers quickly and receives an incoming call while native window construction is still underway. The forwarding goroutine reads `main` concurrently with its assignment; the attention request can also be discarded while it is nil.

**Fix:** initialize the window before starting event forwarding, or publish it through an explicit initialization barrier.

### N8. **Minor — an unsuccessful asset scan is treated as a clean scan**
**`build/qa/offline-skins.sh:20–25`, top-level remote-asset scan**

The `if grep …` treats grep’s error status exactly like “no matches.” `set -e` does not make that conditional fail.

**Failing scenario:** invoke the documented script with an existing embedded binary after its source `internal/gui/dist` directory has been removed. The scan errors, but successful window traces can still produce seven passes and exit zero.

**Fix:** require the expected asset files and distinguish grep’s clean-no-match status from an execution/read error.

### N9. **Minor — scaling passes do not establish unclipped controls**
**`frontend/scripts/scale-check.sh:18–23`, `whole`/`check`**

General controls are checked horizontally only. Vertical visibility and overlap are not tested; the additional height checks cover only selected classes.

**Failing scenario:** translate the End call button below the viewport with `transform: translateY(100vh)`. Its horizontal bounds and layout dimensions remain acceptable, and all scaling assertions can pass despite the hangup control being inaccessible.

**Fix:** check essential controls’ vertical visibility and obstruction, accounting for intentional scroll containers. Describe the present result as horizontal/layout checks rather than complete scaled-window acceptance.

### N10. **Minor — Ubuntu remains an explicit exception to the written asset requirement**
**`frontend/src/themes/ubuntu.css:94`, `--font-ui`; `frontend/src/themes/themes.test.ts:394–401`, font test**

The requirement says skins use only bundled assets, but Ubuntu and Ubuntu Sans precede bundled Inter. The test merely requires a bundled family somewhere in the stack.

**Failing scenario:** on a desktop with Ubuntu installed, that unbundled font is used; on the offline build image, Inter is used. The receipt accurately acknowledges this difference, but it does not satisfy the requirement literally.

**Fix:** put Inter first, or explicitly approve and record a system-font exception in the specification. This is a reasonable desktop compatibility choice, but not an exception the current specification contains.

## Receipt claims still too strong

- **M3 receipt, lines 8, 25 and 45:** “77 screens” with every control’s ring drawn overstates the wrong-screen sequence and opacity checks in N4–N5. The 896 count does not establish the claimed coverage.
- **M3 receipt, task 5.1:** the named test does not enforce every literal-color form; see N6.
- **M3 receipt, task 5.4:** startup and loaded-face evidence is appropriately narrowed elsewhere, but complete self-contained compliance still needs the Ubuntu exception reconciled.
- **Tray receipt, line 27:** “only a yes ends a call” is stronger than the cached-state fallback establishes; see N2.
- **Packaged validation:** the supplied AppImage log uses the older harness’s line numbers, including final reporting at line 885 rather than current line 928. It does not demonstrate the new minimized-window, third-registration or per-face checks against a post-review artifact. The tray receipt’s “same through the AppImage” should identify the tested revision and scope explicitly.

## Checked and supported

- The accepted-shutdown duplicate path is serialized; no lock-order cycle is visible between `ending` and `mu`. Attention runs outside `mu`.
- The Linux walkthrough selects each skin, checks identity and font faces, connects and ends calls, and clears the retained number.
- The minimized-window test checks visibility **before** `keys` can reactivate the window.
- The third registration adds meaningful healthy-PBX evidence beyond `Stopped`.
- PowerShell’s single-line command slicing is corrected; every current skin has a nonempty expected-face list. Screenshot failures remain intentionally non-fatal.
- Font URLs are local, licence files are concatenated into notices, and the network log is correctly identified as mock-development evidence.
- Keeping 5.8 unchecked truthfully preserves the Windows-call and automation-method gaps.

Not checked: runtime behavior, omitted font binaries/screenshots, or released artifacts; stale graph metadata was supplemented by direct source reads.  
Risk: quit/confirmation races remain, and green audit totals still overstate accessibility coverage.